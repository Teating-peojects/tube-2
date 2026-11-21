package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/repository"
)

// Service-level tests for the processing controls (status, cancel, retry). They were missing: a mutation that
// let any signed-in user read any video's processing status passed the whole suite.

const procVideo = "0190aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"

type procCall struct {
	videoID, actor string
	staff          bool
	entry          repository.AuditEntry
}

type fakeProc struct {
	info      repository.ProcInfo
	infoErr   error
	actionErr error
	cancels   []procCall
	retries   []procCall
}

func (f *fakeProc) Info(context.Context, string) (repository.ProcInfo, error) {
	return f.info, f.infoErr
}
func (f *fakeProc) Cancel(_ context.Context, id, actor string, staff bool, a repository.AuditEntry) error {
	f.cancels = append(f.cancels, procCall{id, actor, staff, a})
	return f.actionErr
}
func (f *fakeProc) Retry(_ context.Context, id string, a repository.AuditEntry) error {
	f.retries = append(f.retries, procCall{videoID: id, entry: a})
	return f.actionErr
}

// procLimiter counts hits per key and can be told to fail.
type procLimiter struct {
	hits map[string]int64
	err  error
}

func (l *procLimiter) Hit(_ context.Context, key string, _ time.Duration) (int64, time.Duration, error) {
	if l.err != nil {
		return 0, 0, l.err
	}
	if l.hits == nil {
		l.hits = map[string]int64{}
	}
	l.hits[key]++
	return l.hits[key], time.Minute, nil
}
func (l *procLimiter) Peek(context.Context, string) (int64, time.Duration, error) { return 0, 0, nil }
func (l *procLimiter) Reset(context.Context, string) error                        { return nil }

func newProc(f *fakeProc, l *procLimiter) *Processing {
	return NewProcessing(f, l, []byte("k"), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func user(id string, role auth.Role) *auth.Principal { return &auth.Principal{UserID: id, Role: role} }

func isMissing(err error) bool { return errors.Is(err, errTargetMissing) }

func TestProcessingStatusIsOnlyForTheOwnerOrStaff(t *testing.T) {
	ctx := context.Background()
	base := repository.ProcInfo{CreatorID: "owner", VideoStatus: "PROCESSING", Moderation: "OK"}
	for _, c := range []struct {
		name    string
		p       *auth.Principal
		mut     func(*repository.ProcInfo)
		visible bool
	}{
		{"owner", user("owner", auth.RoleCreator), nil, true},
		{"a signed-in stranger", user("stranger", auth.RoleCreator), nil, false},
		{"a plain user", user("stranger", auth.RoleUser), nil, false},
		{"a moderator", user("mod", auth.RoleModerator), nil, true},
		{"an admin", user("adm", auth.RoleAdmin), nil, true},
		{"the owner of a deleted video", user("owner", auth.RoleCreator), func(v *repository.ProcInfo) { v.Deleted = true }, false},
		{"staff for a deleted video", user("mod", auth.RoleModerator), func(v *repository.ProcInfo) { v.Deleted = true }, false},
	} {
		info := base
		if c.mut != nil {
			c.mut(&info)
		}
		_, err := newProc(&fakeProc{info: info}, &procLimiter{}).ProcessingInfo(ctx, c.p, procVideo)
		switch {
		case c.visible && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case !c.visible && !isMissing(err):
			t.Errorf("%s: want target_not_found (exactly like a video that does not exist), got %v", c.name, err)
		}
	}
	s := newProc(&fakeProc{info: base}, &procLimiter{})
	if _, err := s.ProcessingInfo(ctx, user("owner", auth.RoleCreator), "not-a-uuid"); !isMissing(err) {
		t.Errorf("a malformed id: %v", err)
	}
	s = newProc(&fakeProc{infoErr: repository.ErrTargetNotFound}, &procLimiter{})
	if _, err := s.ProcessingInfo(ctx, user("owner", auth.RoleCreator), procVideo); !isMissing(err) {
		t.Errorf("an unknown video: %v", err)
	}
}

func TestProcessingStatusMapping(t *testing.T) {
	ctx := context.Background()
	owner := user("owner", auth.RoleCreator)
	staff := user("mod", auth.RoleModerator)
	type want struct {
		status, stage, code string
		progress            int
		cancel, retryOwner  bool
		retryStaff          bool
	}
	for _, c := range []struct {
		name string
		in   repository.ProcInfo
		want want
	}{
		{"removed wins over published", repository.ProcInfo{VideoStatus: "PUBLISHED", Moderation: "REMOVED"}, want{status: "removed", code: "removed"}},
		{"published", repository.ProcInfo{VideoStatus: "PUBLISHED", Moderation: "OK"}, want{status: "published", progress: 100}},
		{"ready", repository.ProcInfo{VideoStatus: "READY", Moderation: "OK"}, want{status: "ready", progress: 100}},
		{"failed with a code", repository.ProcInfo{VideoStatus: "FAILED", Moderation: "OK", FailCode: "invalid_file"}, want{status: "failed", code: "invalid_file", retryStaff: true}},
		{"cancelled", repository.ProcInfo{VideoStatus: "FAILED", Moderation: "OK", FailCode: "cancelled"}, want{status: "cancelled", code: "cancelled", retryStaff: true}},
		{"queued metadata", repository.ProcInfo{VideoStatus: "PROCESSING", Moderation: "OK", ActiveJob: "METADATA", ActiveStatus: "QUEUED"}, want{status: "queued", stage: "inspecting", cancel: true}},
		{"encoding in progress", repository.ProcInfo{VideoStatus: "PROCESSING", Moderation: "OK", ActiveJob: "TRANSCODE", ActiveStatus: "PROCESSING", JobsDone: 2}, want{status: "processing", stage: "encoding", progress: 50, cancel: true}},
		{"progress never reaches 100 before the video is ready", repository.ProcInfo{VideoStatus: "PROCESSING", Moderation: "OK", ActiveJob: "HLS", ActiveStatus: "PROCESSING", JobsDone: 4}, want{status: "processing", stage: "packaging", progress: 99, cancel: true}},
		{"still uploading", repository.ProcInfo{VideoStatus: "UPLOADING", Moderation: "OK"}, want{status: "queued"}},
	} {
		in := c.in
		in.CreatorID = "owner"
		for who, p := range map[string]*auth.Principal{"owner": owner, "staff": staff} {
			got, err := newProc(&fakeProc{info: in}, &procLimiter{}).ProcessingInfo(ctx, p, procVideo)
			if err != nil {
				t.Fatalf("%s/%s: %v", c.name, who, err)
			}
			wantRetry := c.want.retryOwner
			if who == "staff" {
				wantRetry = c.want.retryStaff
			}
			if got.Status != c.want.status || got.Stage != c.want.stage || got.ErrorCode != c.want.code || got.Progress != c.want.progress ||
				got.CanCancel != c.want.cancel || got.CanRetry != wantRetry {
				t.Errorf("%s/%s: got %+v, want %+v (retry %v)", c.name, who, got, c.want, wantRetry)
			}
			if (got.ErrorCode == "") != (got.Message == "") {
				t.Errorf("%s/%s: a message goes with an error code and nothing else: code %q message %q", c.name, who, got.ErrorCode, got.Message)
			}
			if got.Renditions == nil {
				t.Errorf("%s/%s: renditions must serialise as [] not null", c.name, who)
			}
		}
	}
	got, _ := newProc(&fakeProc{info: repository.ProcInfo{CreatorID: "owner", VideoStatus: "FAILED", FailCode: "invalid_file"}}, &procLimiter{}).ProcessingInfo(ctx, owner, procVideo)
	if got.Message != failureMessage("invalid_file") || strings.Contains(got.Message, "invalid_file") {
		t.Errorf("the creator sees a sentence, not the code: %q", got.Message)
	}
}

func TestProcessingCancel(t *testing.T) {
	ctx := context.Background()
	ci := ClientInfo{IP: "203.0.113.5", RequestID: "req-1"}

	f := &fakeProc{}
	if err := newProc(f, &procLimiter{}).Cancel(ctx, user("owner", auth.RoleCreator), ci, procVideo, "  changed my mind "); err != nil {
		t.Fatal(err)
	}
	c := f.cancels[0]
	if c.staff || c.actor != "owner" || c.videoID != procVideo || c.entry.Action != "processing.cancelled" || c.entry.Meta["by"] != "owner" || c.entry.Meta["reason"] != "changed my mind" {
		t.Fatalf("owner cancel: %+v", c)
	}
	if len(c.entry.IPHash) == 0 || bytes.Contains(c.entry.IPHash, []byte("203.0.113.5")) {
		t.Fatalf("the audit entry must carry a hash of the IP, never the IP: %x", c.entry.IPHash)
	}

	f = &fakeProc{}
	if err := newProc(f, &procLimiter{}).Cancel(ctx, user("mod", auth.RoleModerator), ci, procVideo, ""); err != nil {
		t.Fatal(err)
	}
	if c := f.cancels[0]; !c.staff || c.entry.Meta["by"] != "staff" || c.entry.Meta["reason"] != nil {
		t.Fatalf("staff cancel without a reason: %+v", c)
	}

	f = &fakeProc{}
	s := newProc(f, &procLimiter{})
	if err := s.Cancel(ctx, user("owner", auth.RoleCreator), ci, "not-a-uuid", ""); !isMissing(err) {
		t.Errorf("malformed id: %v", err)
	}
	if err := s.Cancel(ctx, user("owner", auth.RoleCreator), ci, procVideo, strings.Repeat("x", 501)); err == nil {
		t.Error("a 501-character reason must be refused")
	}
	if len(f.cancels) != 0 {
		t.Errorf("a refused request must not reach the store: %d calls", len(f.cancels))
	}

	l := &procLimiter{}
	s = newProc(&fakeProc{}, l)
	for i := 1; i <= 20; i++ {
		if err := s.Cancel(ctx, user("owner", auth.RoleCreator), ci, procVideo, ""); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	var ae *apperr.Error
	if err := s.Cancel(ctx, user("owner", auth.RoleCreator), ci, procVideo, ""); !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests {
		t.Errorf("the 21st cancel within the window must be rate limited: %v", err)
	}
	if err := newProc(&fakeProc{}, &procLimiter{err: errors.New("redis down")}).Cancel(ctx, user("owner", auth.RoleCreator), ci, procVideo, ""); !errors.Is(err, errUnavailable) {
		t.Errorf("a broken limiter must fail closed: %v", err)
	}
	if err := newProc(&fakeProc{actionErr: repository.ErrTargetNotFound}, &procLimiter{}).Cancel(ctx, user("owner", auth.RoleCreator), ci, procVideo, ""); !isMissing(err) {
		t.Errorf("an unknown or foreign video: %v", err)
	}
}

func TestProcessingRetryIsForStaffWithAReason(t *testing.T) {
	ctx := context.Background()
	ci := ClientInfo{IP: "203.0.113.5"}
	f := &fakeProc{}
	s := newProc(f, &procLimiter{})
	for _, p := range []*auth.Principal{user("owner", auth.RoleCreator), user("u", auth.RoleUser)} {
		err := s.Retry(ctx, p, ci, procVideo, "please")
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Errorf("%s must be forbidden to retry: %v", p.Role, err)
		}
	}
	if len(f.retries) != 0 {
		t.Fatalf("a forbidden retry reached the store")
	}
	if err := s.Retry(ctx, user("mod", auth.RoleModerator), ci, procVideo, "   "); err == nil {
		t.Error("a retry without a reason must be refused")
	}
	if err := s.Retry(ctx, user("mod", auth.RoleModerator), ci, "nope", "again"); !isMissing(err) {
		t.Errorf("malformed id: %v", err)
	}
	if err := s.Retry(ctx, user("mod", auth.RoleModerator), ci, procVideo, "disk was full"); err != nil {
		t.Fatal(err)
	}
	if len(f.retries) != 1 || f.retries[0].entry.Action != "processing.retried" || f.retries[0].entry.Meta["reason"] != "disk was full" || f.retries[0].videoID != procVideo {
		t.Fatalf("%+v", f.retries)
	}
	l := &procLimiter{}
	s = newProc(&fakeProc{}, l)
	for i := 1; i <= 30; i++ {
		if err := s.Retry(ctx, user("mod", auth.RoleModerator), ci, procVideo, "again"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	var ae *apperr.Error
	if err := s.Retry(ctx, user("mod", auth.RoleModerator), ci, procVideo, "again"); !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests {
		t.Errorf("the 31st retry within the window must be rate limited: %v", err)
	}
}
