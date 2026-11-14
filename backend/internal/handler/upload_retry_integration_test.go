package handler_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/service"
	"github.com/redxtube/api/internal/storage"
)

// The upload retry flow against the real schema (migration 000011) with the real repository. Object storage is
// stubbed: only multipart creation/abort and presigning are needed, any other call would panic on the nil interface.

type retryLimiter struct{}

func (retryLimiter) Hit(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 1, time.Minute, nil
}
func (retryLimiter) Peek(context.Context, string) (int64, time.Duration, error) { return 0, 0, nil }
func (retryLimiter) Reset(context.Context, string) error                        { return nil }

type retryNotifier struct{}

func (retryNotifier) Notify(context.Context, string) {}

type retryStore struct {
	storage.API // everything else is unused here
	mu          sync.Mutex
	created     int
	aborted     []string // provider upload ids
}

func (s *retryStore) CreateMultipart(context.Context, string, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created++
	return fmt.Sprintf("prov-%d", s.created), nil
}
func (s *retryStore) AbortMultipart(_ context.Context, _, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aborted = append(s.aborted, providerID)
	return nil
}
func (s *retryStore) PresignUploadPart(key, id string, n int, _ time.Duration) string {
	return fmt.Sprintf("https://s3.test/%s?upload=%s&part=%d", key, id, n)
}
func (s *retryStore) counts() (created, aborted int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.created, len(s.aborted)
}

func TestUploadRetryAgainstRealDatabase(t *testing.T) {
	e := setup(t)
	e.creator(1, "Retry Studio", "ACTIVE", false, 0)
	e.creator(2, "Other Studio", "ACTIVE", false, 0)
	store := &retryStore{}
	svc := service.NewUpload(repository.NewUpload(e.app), store, retryLimiter{}, []byte("0123456789abcdef0123456789abcdef"),
		media.New("https://cdn.test"), retryNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	owner := &auth.Principal{UserID: cid(1), EmailVerified: true, IsCreator: true}
	stranger := &auth.Principal{UserID: cid(2), EmailVerified: true, IsCreator: true}
	ci := service.ClientInfo{IP: "203.0.113.9", RequestID: "r1"}
	code := func(err error) string {
		if err == nil {
			return ""
		}
		return apperr.From(err).Code
	}

	type state struct {
		status, videoStatus string
		retries             int
		deleted             bool
	}
	read := func(uploadID string) state {
		t.Helper()
		var s state
		if err := e.pool.QueryRow(ctx, `SELECT s.status::text, s.retry_count, v.status::text, v.deleted_at IS NOT NULL
FROM upload_sessions s JOIN videos v ON v.id = s.video_id WHERE s.id = $1::uuid`, uploadID).Scan(&s.status, &s.retries, &s.videoStatus, &s.deleted); err != nil {
			t.Fatal(err)
		}
		return s
	}
	expire := func(uploadID string) {
		t.Helper()
		e.exec(`UPDATE upload_sessions SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE id = $1::uuid`, uploadID)
		if n, err := svc.ExpireStale(ctx); err != nil || n != 1 {
			t.Fatalf("janitor expired %d sessions, err=%v", n, err)
		}
	}
	newUpload := func() service.InitInput { return service.InitInput{Filename: "clip.mp4", Size: 40 << 20} } // 3 parts of 16 MiB

	in, err := svc.Init(ctx, owner, ci, newUpload())
	if err != nil {
		t.Fatal(err)
	}

	// 1. an upload that is still open cannot be retried
	if _, err := svc.Retry(ctx, owner, ci, in.UploadID); code(err) != "upload_active" {
		t.Fatalf("open upload: %q", code(err))
	}

	// 2. the janitor expires it: the draft video is discarded, the status tells the browser it can retry
	expire(in.UploadID)
	if got := read(in.UploadID); got != (state{"EXPIRED", "FAILED", 0, true}) {
		t.Fatalf("after expiry: %+v", got)
	}
	st, err := svc.Status(ctx, owner, in.UploadID)
	if err != nil || st.Status != "failed" || !st.Retryable {
		t.Fatalf("status of an expired upload: %+v err=%v", st, err)
	}

	// 3. another user cannot touch it
	if _, err := svc.Retry(ctx, stranger, ci, in.UploadID); code(err) != "upload_not_found" {
		t.Fatalf("stranger: %q", code(err))
	}
	if got := read(in.UploadID); got.status != "EXPIRED" || got.retries != 0 {
		t.Fatalf("a stranger changed the session: %+v", got)
	}

	// 4. the owner reopens it: same ids, fresh multipart, draft restored, one audit row
	out, err := svc.Retry(ctx, owner, ci, in.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	if out.UploadID != in.UploadID || out.VideoID != in.VideoID || len(out.Parts) != 3 || out.PartSize != 16<<20 {
		t.Fatalf("retry response: %+v", out)
	}
	if got := read(in.UploadID); got != (state{"INITIATED", "UPLOADING", 1, false}) {
		t.Fatalf("after retry: %+v", got)
	}
	var provider string
	var remaining time.Duration
	var lastRetry *time.Time
	if err := e.pool.QueryRow(ctx, `SELECT provider_upload_id, expires_at - now(), last_retry_at FROM upload_sessions WHERE id = $1::uuid`, in.UploadID).Scan(&provider, &remaining, &lastRetry); err != nil {
		t.Fatal(err)
	}
	if provider != "prov-2" || remaining < 23*time.Hour || lastRetry == nil {
		t.Fatalf("provider=%q remaining=%v lastRetry=%v", provider, remaining, lastRetry)
	}
	if n := e.countRowsAdmin(`SELECT count(*) FROM audit_logs WHERE action = 'upload.retry' AND target_id::text = $1`, in.VideoID); n != 1 {
		t.Fatalf("audit rows: %d", n)
	}
	st, err = svc.Status(ctx, owner, in.UploadID)
	if err != nil || st.Retryable || st.Status != "processing" {
		t.Fatalf("status of a reopened upload: %+v err=%v", st, err)
	}
	if _, err := svc.Retry(ctx, owner, ci, in.UploadID); code(err) != "upload_active" {
		t.Fatalf("second retry while open: %q", code(err))
	}

	// 5. the budget: 3 reopenings in total, then it is final
	expire(in.UploadID)
	for i := 2; i <= 3; i++ {
		if _, err := svc.Retry(ctx, owner, ci, in.UploadID); err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		expire(in.UploadID)
	}
	if _, err := svc.Retry(ctx, owner, ci, in.UploadID); code(err) != "retry_limit" {
		t.Fatalf("fourth retry: %q", code(err))
	}
	if got := read(in.UploadID); got != (state{"EXPIRED", "FAILED", 3, true}) {
		t.Fatalf("after the budget is spent: %+v", got)
	}
	if st, _ = svc.Status(ctx, owner, in.UploadID); st.Retryable {
		t.Fatalf("a spent budget must not be advertised as retryable: %+v", st)
	}

	// 6. two simultaneous retries: exactly one wins, the loser's new multipart is aborted
	race, err := svc.Init(ctx, owner, ci, newUpload())
	if err != nil {
		t.Fatal(err)
	}
	expire(race.UploadID)
	c0, a0 := store.counts()
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Retry(ctx, owner, ci, race.UploadID)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch code(err) {
		case "":
			wins++
		case "not_retryable", "upload_active": // lost at the database, or read the session after the winner reopened it
		default:
			t.Fatalf("unexpected error from the race: %v", err)
		}
	}
	c1, a1 := store.counts()
	if live := (c1 - c0) - (a1 - a0); wins != 1 || live != 1 {
		t.Fatalf("wins=%d, multiparts created=%d aborted=%d: exactly one live new multipart expected", wins, c1-c0, a1-a0)
	}
	if got := read(race.UploadID); got != (state{"INITIATED", "UPLOADING", 1, false}) {
		t.Fatalf("after the race: %+v", got)
	}

	// 6b. the exact race window at the repository: two callers hold the same stale EXPIRED session
	repo := repository.NewUpload(e.app)
	stale, err := svc.Init(ctx, owner, ci, newUpload())
	if err != nil {
		t.Fatal(err)
	}
	expire(stale.UploadID)
	sess, err := repo.UploadByID(ctx, stale.UploadID, owner.UserID)
	if err != nil || sess == nil || sess.Status != "EXPIRED" || sess.RetryCount != 0 {
		t.Fatalf("session: %+v err=%v", sess, err)
	}
	targetType := "video"
	audit := repository.AuditEntry{ActorID: &owner.UserID, Action: "upload.retry", TargetType: &targetType, TargetID: &sess.VideoID}
	first, err1 := repo.ReviveUpload(ctx, sess, "prov-a", time.Now().Add(time.Hour), 3, audit)
	second, err2 := repo.ReviveUpload(ctx, sess, "prov-b", time.Now().Add(time.Hour), 3, audit)
	if err1 != nil || err2 != nil || !first || second {
		t.Fatalf("first=%v/%v second=%v/%v: only the first revive may win", first, err1, second, err2)
	}
	if got := read(stale.UploadID); got != (state{"INITIATED", "UPLOADING", 1, false}) {
		t.Fatalf("after the double revive: %+v", got)
	}
	var prov string
	e.pool.QueryRow(ctx, `SELECT provider_upload_id FROM upload_sessions WHERE id = $1::uuid`, stale.UploadID).Scan(&prov)
	if prov != "prov-a" || e.countRowsAdmin(`SELECT count(*) FROM audit_logs WHERE action = 'upload.retry' AND target_id::text = $1`, stale.VideoID) != 1 {
		t.Fatalf("the loser must change nothing: provider=%q", prov)
	}
	// a spent budget is refused by the database itself, whatever the service believes
	expire(stale.UploadID)
	sess, _ = repo.UploadByID(ctx, stale.UploadID, owner.UserID)
	if ok, err := repo.ReviveUpload(ctx, sess, "prov-c", time.Now().Add(time.Hour), 1, audit); err != nil || ok {
		t.Fatalf("budget of 1 already used: ok=%v err=%v", ok, err)
	}

	// 7. a reopened upload can still be cancelled, and a cancelled one is final
	if err := svc.Abort(ctx, owner, ci, race.UploadID); err != nil {
		t.Fatal(err)
	}
	if got := read(race.UploadID); got.status != "ABORTED" || got.videoStatus != "FAILED" || !got.deleted {
		t.Fatalf("after cancel: %+v", got)
	}
	if _, err := svc.Retry(ctx, owner, ci, race.UploadID); code(err) != "not_retryable" {
		t.Fatalf("retry of a cancelled upload: %q", code(err))
	}

	// 8. a video removed by moderation is never brought back
	mod, err := svc.Init(ctx, owner, ci, newUpload())
	if err != nil {
		t.Fatal(err)
	}
	expire(mod.UploadID)
	e.exec(`UPDATE videos SET moderation_status = 'REMOVED' WHERE id = $1::uuid`, mod.VideoID)
	_, a2 := store.counts()
	if _, err := svc.Retry(ctx, owner, ci, mod.UploadID); code(err) != "not_retryable" {
		t.Fatalf("moderated video: %q", code(err))
	}
	if got := read(mod.UploadID); got != (state{"EXPIRED", "FAILED", 0, true}) {
		t.Fatalf("a moderated video was restored: %+v", got)
	}
	if _, a3 := store.counts(); a3-a2 != 1 {
		t.Fatalf("the multipart created for the refused retry must be aborted (aborted %d)", a3-a2)
	}
}

// countRowsAdmin runs a single-count query on the admin connection.
func (e *env) countRowsAdmin(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
