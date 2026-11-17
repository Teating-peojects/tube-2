package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/storage"
)

// ---------- fakes ----------

type okLimiter struct{ n int64 }

func (l *okLimiter) Hit(context.Context, string, time.Duration) (int64, time.Duration, error) {
	l.n++
	return 1, time.Minute, nil
}
func (l *okLimiter) Peek(context.Context, string) (int64, time.Duration, error) { return 0, 0, nil }
func (l *okLimiter) Reset(context.Context, string) error                        { return nil }

type fakeStorage struct {
	mu        sync.Mutex
	objects   map[string][]byte
	parts     []storage.Part
	aborted   []string
	deleted   []string
	completed bool
	size      int64 // object size after completion
	head      []byte
	created   int   // CreateMultipart calls
	createErr error // makes CreateMultipart fail

	abortedIDs   []string // provider upload ids passed to AbortMultipart
	abortFailKey string   // AbortMultipart fails for this key
	prefixes     []string // DeletePrefix calls
	prefixErr    error    // DeletePrefix fails
}

func (f *fakeStorage) CreateMultipart(context.Context, string, string) (string, error) {
	f.created++
	if f.createErr != nil {
		return "", f.createErr
	}
	return "prov-" + string(rune('0'+f.created)), nil
}
func (f *fakeStorage) PresignUploadPart(key, id string, n int, _ time.Duration) string {
	return "https://s3.test/" + key + "?part=" + string(rune('0'+n))
}
func (f *fakeStorage) ListParts(context.Context, string, string) ([]storage.Part, error) {
	return f.parts, nil
}
func (f *fakeStorage) CompleteMultipart(context.Context, string, string, []storage.Part) error {
	f.completed = true
	return nil
}
func (f *fakeStorage) AbortMultipart(_ context.Context, key, id string) error {
	if key == f.abortFailKey {
		return errors.New("s3 down")
	}
	f.aborted = append(f.aborted, key)
	f.abortedIDs = append(f.abortedIDs, id)
	return nil
}
func (f *fakeStorage) PresignPut(key, _ string, _ int64, _ time.Duration) string {
	return "https://s3.test/put/" + key
}
func (f *fakeStorage) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.objects[key]; ok {
		return storage.ObjectInfo{Size: int64(len(b))}, nil
	}
	if f.completed {
		return storage.ObjectInfo{Size: f.size}, nil
	}
	return storage.ObjectInfo{}, storage.ErrNotFound
}
func (f *fakeStorage) GetRange(_ context.Context, key string, s, e int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return f.head, nil
	}
	if e >= int64(len(b)) {
		e = int64(len(b)) - 1
	}
	return b[s : e+1], nil
}
func (f *fakeStorage) Download(context.Context, string, io.Writer) (int64, error) { return 0, nil }
func (f *fakeStorage) Put(context.Context, string, string, string, io.Reader, int64) error {
	return nil
}
func (f *fakeStorage) PutBytes(_ context.Context, key, _, _ string, b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = b
	return nil
}
func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}
func (f *fakeStorage) DeletePrefix(_ context.Context, prefix string) (int, error) {
	if f.prefixErr != nil {
		return 0, f.prefixErr
	}
	f.prefixes = append(f.prefixes, prefix)
	return 1, nil
}

type fakeUploads struct {
	progress  repository.Progress // returned by Progress when Moderation is set
	sess      *repository.UploadSession
	open      int
	created   *repository.UploadInit
	begun     bool
	reverted  bool
	finished  bool
	discarded bool
	pub       *repository.PublishParams
	pubErr    error
	pubVis    string
	fanned    bool
	thumbs    map[string]string // id -> key
	caps      map[string][2]string

	revives    int
	revived    revive
	reviveLost bool // another caller won the race / budget spent
	reviveErr  error

	closed         []repository.ClosedUpload
	closedDelay    time.Duration
	marked         []string
	failed         []string
	failedAttempts int
	finalize       int
	finalizeCalls  int
	finalizeKeep   time.Duration
}

type revive struct {
	provider string
	expires  time.Time
	max      int
	audit    repository.AuditEntry
}

func (f *fakeUploads) OpenUploads(context.Context, string) (int, error) { return f.open, nil }
func (f *fakeUploads) CreateUpload(_ context.Context, n repository.UploadInit) error {
	f.created = &n
	return nil
}
func (f *fakeUploads) UploadByID(context.Context, string, string) (*repository.UploadSession, error) {
	return f.sess, nil
}
func (f *fakeUploads) BeginComplete(context.Context, string) (bool, error) {
	if f.sess.Status != "INITIATED" {
		return false, nil
	}
	f.begun = true
	return true, nil
}
func (f *fakeUploads) RevertComplete(context.Context, string) error { f.reverted = true; return nil }
func (f *fakeUploads) FinishComplete(context.Context, *repository.UploadSession, repository.AuditEntry) error {
	f.finished = true
	return nil
}
func (f *fakeUploads) DiscardUpload(context.Context, *repository.UploadSession, *repository.AuditEntry) error {
	f.discarded = true
	return nil
}
func (f *fakeUploads) ClosedUploads(_ context.Context, delay time.Duration, _ int) ([]repository.ClosedUpload, error) {
	f.closedDelay = delay
	return f.closed, nil
}
func (f *fakeUploads) MarkCleaned(_ context.Context, c repository.ClosedUpload) (bool, error) {
	f.marked = append(f.marked, c.ID)
	return true, nil
}
func (f *fakeUploads) CleanupFailed(_ context.Context, c repository.ClosedUpload) (int, error) {
	f.failed = append(f.failed, c.ID)
	return f.failedAttempts, nil
}
func (f *fakeUploads) FinalizeExpired(_ context.Context, keep time.Duration, _ int) (int, error) {
	f.finalizeCalls++
	f.finalizeKeep = keep
	return f.finalize, nil
}
func (f *fakeUploads) ReviveUpload(_ context.Context, _ *repository.UploadSession, provider string, expires time.Time, max int, a repository.AuditEntry) (bool, error) {
	f.revives++
	f.revived = revive{provider, expires, max, a}
	return !f.reviveLost, f.reviveErr
}
func (f *fakeUploads) ExpireUploads(context.Context, int) ([]repository.ExpiredUpload, error) {
	return []repository.ExpiredUpload{{ID: "u1", StorageKey: "k", ProviderUploadID: "p"}}, nil
}
func (f *fakeUploads) Progress(context.Context, string) (repository.Progress, error) {
	if f.progress.Moderation != "" {
		return f.progress, nil
	}
	return repository.Progress{Moderation: "ACTIVE"}, nil
}
func (f *fakeUploads) GeneratedThumbs(context.Context, string) ([]repository.ThumbRow, error) {
	return nil, nil
}
func (f *fakeUploads) CountThumbs(context.Context, string) (int, error)   { return 0, nil }
func (f *fakeUploads) CountCaptions(context.Context, string) (int, error) { return 0, nil }
func (f *fakeUploads) AddThumb(context.Context, string, string) (string, error) {
	return auth.NewUUIDv7(), nil
}
func (f *fakeUploads) CaptionExists(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakeUploads) AddCaption(context.Context, string, string, string, string) (string, error) {
	return auth.NewUUIDv7(), nil
}
func (f *fakeUploads) ThumbKey(_ context.Context, _, id string) (string, error) {
	return f.thumbs[id], nil
}
func (f *fakeUploads) CaptionKey(_ context.Context, _, id string) (string, string, error) {
	c := f.caps[id]
	return c[0], c[1], nil
}
func (f *fakeUploads) CategoryID(context.Context, string) (int32, error) { return 3, nil }
func (f *fakeUploads) UpdateMetadata(context.Context, string, string, repository.MetaPatch, repository.AuditEntry) error {
	return nil
}
func (f *fakeUploads) Publish(_ context.Context, p repository.PublishParams) (string, error) {
	f.pub = &p
	return f.pubVis, f.pubErr
}
func (f *fakeUploads) FanOutNewVideo(context.Context, string, string, int) ([]string, error) {
	f.fanned = true
	return []string{"a", "b"}, nil
}

type countNotifier struct{ ids []string }

func (c *countNotifier) Notify(_ context.Context, id string) { c.ids = append(c.ids, id) }

// ---------- helpers ----------

var (
	testUser = &auth.Principal{UserID: "11111111-1111-7111-8111-111111111111", Role: auth.RoleUser, EmailVerified: true}
	testCI   = ClientInfo{IP: "203.0.113.9", RequestID: "r1"}
	vidID    = "22222222-2222-7222-8222-222222222222"
	upID     = "33333333-3333-7333-8333-333333333333"
)

func newUploadSvc(db *fakeUploads, st storage.API) (*Upload, *countNotifier) {
	n := &countNotifier{}
	return NewUpload(db, st, &okLimiter{}, []byte("0123456789abcdef0123456789abcdef"), media.New("https://cdn.test"), n,
		slog.New(slog.NewTextHandler(io.Discard, nil))), n
}

func ecode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	return apperr.From(err).Code
}

func mp4Head() []byte { return append([]byte{0, 0, 0, 0x20}, []byte("ftypisom0000")...) }

func session(status string, size int64) *repository.UploadSession {
	return &repository.UploadSession{ID: upID, UserID: testUser.UserID, VideoID: vidID, StorageKey: "uploads/" + vidID + "/original.mp4",
		ProviderUploadID: "prov-1", Status: status, VideoStatus: "PROCESSING", DeclaredSize: size,
		PartSize: UploadPartSize, PartCount: int32((size + UploadPartSize - 1) / UploadPartSize), ExpiresAt: time.Now().Add(time.Hour)}
}

// ---------- init ----------

func TestInitValidation(t *testing.T) {
	cases := []struct {
		name string
		in   InitInput
	}{
		{"bad extension", InitInput{Filename: "x.exe", Size: 10}},
		{"avi is no longer accepted", InitInput{Filename: "x.avi", Size: 10}},
		{"empty", InitInput{Filename: "x.mp4", Size: 0}},
		{"too big", InitInput{Filename: "x.mp4", Size: MaxUploadBytes + 1}},
		{"not video", InitInput{Filename: "x.mp4", Size: 10, ContentType: "text/html"}},
	}
	for _, c := range cases {
		db := &fakeUploads{}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		_, err := svc.Init(context.Background(), testUser, testCI, c.in)
		if ecode(t, err) != "validation_failed" || db.created != nil {
			t.Errorf("%s: code=%q created=%v", c.name, ecode(t, err), db.created != nil)
		}
	}
}

func TestInitPartsAndKey(t *testing.T) {
	db := &fakeUploads{}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	out, err := svc.Init(context.Background(), testUser, testCI, InitInput{Filename: `C:\fake\my clip.final.MP4`, Size: 2*UploadPartSize + 1, ContentType: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Parts) != 3 || out.PartSize != UploadPartSize {
		t.Fatalf("parts=%d partSize=%d", len(out.Parts), out.PartSize)
	}
	if db.created.Title != "my clip.final" || db.created.Filename != "my clip.final.MP4" {
		t.Errorf("title/filename: %q / %q", db.created.Title, db.created.Filename)
	}
	if want := "uploads/" + out.VideoID + "/original.mp4"; db.created.StorageKey != want {
		t.Errorf("key %q want %q", db.created.StorageKey, want)
	}
	if db.created.ContentType != "video/mp4" {
		t.Errorf("content type must come from the extension, got %q", db.created.ContentType)
	}
}

func expired(retries int16) *repository.UploadSession {
	s := session("EXPIRED", 40<<20)
	s.VideoStatus, s.VideoDeleted, s.RetryCount, s.ContentType = "FAILED", true, retries, "video/mp4"
	return s
}

func TestRetryReopensAnExpiredUpload(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	db, st := &fakeUploads{sess: expired(1)}, &fakeStorage{}
	svc, _ := newUploadSvc(db, st)
	svc.now = func() time.Time { return now }
	out, err := svc.Retry(context.Background(), testUser, testCI, upID)
	if err != nil {
		t.Fatal(err)
	}
	if out.UploadID != upID || out.VideoID != vidID || out.PartSize != UploadPartSize || len(out.Parts) != 3 {
		t.Errorf("same ids and part layout expected, got %+v", out)
	}
	if db.revives != 1 || db.revived.provider != "prov-1" || db.revived.max != MaxUploadRetries || !db.revived.expires.Equal(now.Add(UploadTTL)) {
		t.Errorf("revive call wrong: %d %+v", db.revives, db.revived)
	}
	if a := db.revived.audit; a.Action != "upload.retry" || a.TargetID == nil || *a.TargetID != vidID || a.Meta["retry"] != 2 {
		t.Errorf("audit wrong: %+v", a)
	}
	if len(st.aborted) != 0 {
		t.Errorf("the new multipart must be kept: %v", st.aborted)
	}
}

func TestRetryOnlyForExpiredUploads(t *testing.T) {
	for status, want := range map[string]string{"INITIATED": "upload_active", "COMPLETING": "upload_active", "COMPLETED": "not_retryable", "ABORTED": "not_retryable"} {
		db, st := &fakeUploads{sess: session(status, 40<<20)}, &fakeStorage{}
		svc, _ := newUploadSvc(db, st)
		_, err := svc.Retry(context.Background(), testUser, testCI, upID)
		if ecode(t, err) != want || st.created != 0 || db.revives != 0 {
			t.Errorf("%s: code=%q created=%d revives=%d, want %q and no side effects", status, ecode(t, err), st.created, db.revives, want)
		}
	}
}

func TestRetryBudget(t *testing.T) {
	for name, c := range map[string]struct {
		retries int16
		max     int
	}{"spent": {3, 3}, "beyond": {5, 3}, "disabled": {0, 0}} {
		db, st := &fakeUploads{sess: expired(c.retries)}, &fakeStorage{}
		svc, _ := newUploadSvc(db, st)
		l := DefaultUploadLimits()
		l.MaxRetries = c.max
		svc.WithLimits(l)
		_, err := svc.Retry(context.Background(), testUser, testCI, upID)
		if ecode(t, err) != "retry_limit" || st.created != 0 || db.revives != 0 {
			t.Errorf("%s: code=%q created=%d revives=%d", name, ecode(t, err), st.created, db.revives)
		}
	}
	// one retry left
	db := &fakeUploads{sess: expired(2)}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if _, err := svc.Retry(context.Background(), testUser, testCI, upID); err != nil {
		t.Errorf("2 of 3 used, one left: %v", err)
	}
}

func TestRetryLosesTheRaceAndCleansUp(t *testing.T) {
	db, st := &fakeUploads{sess: expired(0), reviveLost: true}, &fakeStorage{}
	svc, _ := newUploadSvc(db, st)
	_, err := svc.Retry(context.Background(), testUser, testCI, upID)
	if ecode(t, err) != "not_retryable" || len(st.aborted) != 1 {
		t.Errorf("code=%q aborted=%v: the multipart created for the lost race must be aborted", ecode(t, err), st.aborted)
	}
	db, st = &fakeUploads{sess: expired(0), reviveErr: errors.New("db down")}, &fakeStorage{}
	svc, _ = newUploadSvc(db, st)
	_, err = svc.Retry(context.Background(), testUser, testCI, upID)
	if apperr.From(err).Status != http.StatusInternalServerError || len(st.aborted) != 1 {
		t.Errorf("db error: status=%d aborted=%v", apperr.From(err).Status, st.aborted)
	}
}

func TestRetryNeedsStorageAndRespectsCaps(t *testing.T) {
	db, st := &fakeUploads{sess: expired(0)}, &fakeStorage{createErr: errors.New("s3 down")}
	svc, _ := newUploadSvc(db, st)
	_, err := svc.Retry(context.Background(), testUser, testCI, upID)
	if ecode(t, err) != "storage_unavailable" || db.revives != 0 {
		t.Errorf("storage down: code=%q revives=%d (the database must not change)", ecode(t, err), db.revives)
	}
	db = &fakeUploads{sess: expired(0), open: MaxOpenUploads}
	svc, _ = newUploadSvc(db, &fakeStorage{})
	if _, err = svc.Retry(context.Background(), testUser, testCI, upID); ecode(t, err) != "limit_reached" || db.revives != 0 {
		t.Errorf("open-upload cap: %q", ecode(t, err))
	}
	off, _ := newUploadSvc(&fakeUploads{sess: expired(0)}, nil)
	if _, err = off.Retry(context.Background(), testUser, testCI, upID); ecode(t, err) != "uploads_disabled" {
		t.Errorf("uploads off: %q", ecode(t, err))
	}
	_, err = svc.Retry(context.Background(), testUser, testCI, "not-a-uuid")
	if ecode(t, err) != "upload_not_found" {
		t.Errorf("bad id: %q", ecode(t, err))
	}
}

func TestStatusMarksExpiredUploadsRetryable(t *testing.T) {
	for _, c := range []struct {
		status    string
		retries   int16
		retryable bool
	}{{"EXPIRED", 0, true}, {"EXPIRED", 2, true}, {"EXPIRED", 3, false}, {"ABORTED", 0, false}} {
		s := session(c.status, 40<<20)
		s.VideoDeleted, s.RetryCount = true, c.retries
		svc, _ := newUploadSvc(&fakeUploads{sess: s}, &fakeStorage{})
		out, err := svc.Status(context.Background(), testUser, upID)
		if err != nil || out.Status != "failed" || out.Retryable != c.retryable || out.Error == nil {
			t.Errorf("%s/%d: %+v err=%v, want failed, retryable=%v", c.status, c.retries, out, err, c.retryable)
		}
	}
}

type testCounter struct {
	mu sync.Mutex
	m  map[string]int64
}

func (c *testCounter) Add(name string, n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]int64{}
	}
	c.m[name] += n
}
func (c *testCounter) get(name string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[name]
}

func closedRow(id, status, videoStatus string) repository.ClosedUpload {
	return repository.ClosedUpload{ID: id, VideoID: vidID, StorageKey: "uploads/" + vidID + "/" + id + ".mp4", ProviderUploadID: "prov-" + id, Status: status, VideoStatus: videoStatus}
}

func TestSweepFreesAbortedStorage(t *testing.T) {
	ctr := &testCounter{}
	db, st := &fakeUploads{closed: []repository.ClosedUpload{closedRow("a", "ABORTED", "FAILED")}}, &fakeStorage{}
	svc, _ := newUploadSvc(db, st)
	svc.WithMetrics(ctr)
	res, err := svc.SweepStorage(context.Background())
	if err != nil || res.Cleaned != 1 || res.Failed != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(st.abortedIDs) != 1 || st.abortedIDs[0] != "prov-a" {
		t.Errorf("the multipart upload must be aborted by its provider id: %v", st.abortedIDs)
	}
	want := []string{"uploads/" + vidID + "/", "thumbs/" + vidID + "/", "captions/" + vidID + "/"}
	if strings.Join(st.prefixes, ",") != strings.Join(want, ",") {
		t.Errorf("deleted prefixes %v, want %v", st.prefixes, want)
	}
	if len(db.marked) != 1 || db.marked[0] != "a" || len(db.failed) != 0 {
		t.Errorf("marked=%v failed=%v", db.marked, db.failed)
	}
	if ctr.get("cleanup_sessions") != 1 || ctr.get("cleanup_objects_deleted") != 3 {
		t.Errorf("counters: %v", ctr.m)
	}
}

func TestSweepKeepsTheKeyOfAnExpiredUpload(t *testing.T) {
	// A retry reuses the storage key, so an EXPIRED session only loses its multipart upload.
	db, st := &fakeUploads{closed: []repository.ClosedUpload{closedRow("e", "EXPIRED", "FAILED")}}, &fakeStorage{}
	svc, _ := newUploadSvc(db, st)
	res, _ := svc.SweepStorage(context.Background())
	if res.Cleaned != 1 || len(st.abortedIDs) != 1 || len(st.prefixes) != 0 {
		t.Errorf("cleaned=%d aborted=%v prefixes=%v: nothing but the multipart may be touched", res.Cleaned, st.abortedIDs, st.prefixes)
	}
}

func TestSweepFailureIsRecordedAndDoesNotStarveOthers(t *testing.T) {
	bad, good := closedRow("bad", "ABORTED", "FAILED"), closedRow("good", "ABORTED", "FAILED")
	db := &fakeUploads{closed: []repository.ClosedUpload{bad, good}, failedAttempts: 2}
	st := &fakeStorage{abortFailKey: bad.StorageKey}
	ctr := &testCounter{}
	svc, _ := newUploadSvc(db, st)
	svc.WithMetrics(ctr)
	res, err := svc.SweepStorage(context.Background())
	if err != nil || res.Failed != 1 || res.Cleaned != 1 {
		t.Fatalf("%+v %v: one failure must not stop the pass", res, err)
	}
	if len(db.failed) != 1 || db.failed[0] != "bad" || len(db.marked) != 1 || db.marked[0] != "good" {
		t.Errorf("failed=%v marked=%v", db.failed, db.marked)
	}
	if ctr.get("cleanup_failed") != 1 {
		t.Errorf("counters: %v", ctr.m)
	}
	// deleting objects can fail too: the session must stay unmarked
	db = &fakeUploads{closed: []repository.ClosedUpload{bad}}
	svc, _ = newUploadSvc(db, &fakeStorage{prefixErr: errors.New("s3 down")})
	if res, _ = svc.SweepStorage(context.Background()); res.Failed != 1 || len(db.marked) != 0 || len(db.failed) != 1 {
		t.Errorf("delete failure: %+v marked=%v failed=%v", res, db.marked, db.failed)
	}
}

func TestSweepNeverDeletesAPublishedVideo(t *testing.T) {
	ctr := &testCounter{}
	db, st := &fakeUploads{closed: []repository.ClosedUpload{closedRow("p", "ABORTED", "PUBLISHED")}}, &fakeStorage{}
	svc, _ := newUploadSvc(db, st)
	svc.WithMetrics(ctr)
	res, _ := svc.SweepStorage(context.Background())
	if len(st.prefixes) != 0 || res.Cleaned != 1 || ctr.get("cleanup_skipped_published") != 1 {
		t.Errorf("prefixes=%v cleaned=%d counters=%v", st.prefixes, res.Cleaned, ctr.m)
	}
	// a malformed video id must never be turned into a delete prefix
	badID := closedRow("x", "ABORTED", "FAILED")
	badID.VideoID = "../../uploads"
	db, st = &fakeUploads{closed: []repository.ClosedUpload{badID}}, &fakeStorage{}
	svc, _ = newUploadSvc(db, st)
	if res, _ = svc.SweepStorage(context.Background()); len(st.prefixes) != 0 || res.Failed != 1 {
		t.Errorf("malformed id: prefixes=%v %+v", st.prefixes, res)
	}
}

func TestSweepUsesTheConfiguredTimes(t *testing.T) {
	db := &fakeUploads{finalize: 4}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	l := DefaultUploadLimits()
	l.ExpiredRetention, l.CleanupDelay = 48*time.Hour, 3*time.Minute
	svc.WithLimits(l)
	res, err := svc.SweepStorage(context.Background())
	if err != nil || res.Finalized != 4 || db.finalizeKeep != 48*time.Hour || db.closedDelay != 3*time.Minute {
		t.Errorf("%+v %v keep=%v delay=%v", res, err, db.finalizeKeep, db.closedDelay)
	}
	// without object storage nothing can be verified: no database work, nothing marked
	db = &fakeUploads{finalize: 4, closed: []repository.ClosedUpload{closedRow("a", "ABORTED", "FAILED")}}
	off, _ := newUploadSvc(db, nil)
	if res, err = off.SweepStorage(context.Background()); err != nil || res != (SweepResult{}) || db.finalizeCalls != 0 || len(db.marked) != 0 {
		t.Errorf("uploads off: %+v %v calls=%d marked=%v", res, err, db.finalizeCalls, db.marked)
	}
}

func TestRetryWindow(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		deadlineAgo time.Duration
		open        bool
	}{"just expired": {time.Hour, true}, "six days ago": {6 * 24 * time.Hour, true}, "eight days ago": {8 * 24 * time.Hour, false}} {
		s := expired(0)
		s.ExpiresAt = now.Add(-c.deadlineAgo)
		db := &fakeUploads{sess: s}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		svc.now = func() time.Time { return now }
		st, err := svc.Status(context.Background(), testUser, upID)
		if err != nil || st.Retryable != c.open {
			t.Errorf("%s: status retryable=%v err=%v, want %v", name, st.Retryable, err, c.open)
		}
		_, err = svc.Retry(context.Background(), testUser, testCI, upID)
		if c.open && err != nil || !c.open && (ecode(t, err) != "not_retryable" || db.revives != 0) {
			t.Errorf("%s: retry err=%v revives=%d", name, err, db.revives)
		}
	}
}

func TestCountersFollowTheUploadLifecycle(t *testing.T) {
	ctr := &testCounter{}
	new := func(db *fakeUploads, st *fakeStorage) *Upload {
		svc, _ := newUploadSvc(db, st)
		return svc.WithMetrics(ctr)
	}
	ctx := context.Background()
	if _, err := new(&fakeUploads{}, &fakeStorage{}).Init(ctx, testUser, testCI, InitInput{Filename: "a.mp4", Size: 10}); err != nil {
		t.Fatal(err)
	}
	size := int64(UploadPartSize + 100)
	have, in := goodParts(size)
	if err := new(&fakeUploads{sess: session("INITIATED", size)}, &fakeStorage{parts: have, size: size, head: mp4Head()}).Complete(ctx, testUser, testCI, upID, in); err != nil {
		t.Fatal(err)
	}
	new(&fakeUploads{sess: session("INITIATED", 100)}, &fakeStorage{parts: func() []storage.Part { p, _ := goodParts(100); return p }(), size: 100, head: []byte("<html>no</html>")}).
		Complete(ctx, testUser, testCI, upID, func() []PartIn { _, i := goodParts(100); return i }())
	if err := new(&fakeUploads{sess: session("INITIATED", 100)}, &fakeStorage{}).Abort(ctx, testUser, testCI, upID); err != nil {
		t.Fatal(err)
	}
	if _, err := new(&fakeUploads{sess: expired(0)}, &fakeStorage{}).Retry(ctx, testUser, testCI, upID); err != nil {
		t.Fatal(err)
	}
	if _, err := new(&fakeUploads{}, &fakeStorage{}).ExpireStale(ctx); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int64{"upload_init": 1, "upload_complete": 1, "upload_rejected_invalid_file": 1, "upload_aborted": 1, "upload_retry": 1, "upload_expired": 1} {
		if got := ctr.get(name); got != want {
			t.Errorf("%s = %d, want %d (all: %v)", name, got, want, ctr.m)
		}
	}
}

func TestInitAcceptsAllowedFormats(t *testing.T) {
	for ext, want := range map[string]string{".mp4": "video/mp4", ".m4v": "video/x-m4v", ".MOV": "video/quicktime", ".mkv": "video/x-matroska", ".webm": "video/webm"} {
		db := &fakeUploads{}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if _, err := svc.Init(context.Background(), testUser, testCI, InitInput{Filename: "clip" + ext, Size: 10}); err != nil {
			t.Errorf("%s rejected: %v", ext, err)
		} else if db.created.ContentType != want {
			t.Errorf("%s: content type %q, want %q", ext, db.created.ContentType, want)
		}
	}
}

func TestDefaultUploadLimitsMatchConstants(t *testing.T) {
	d := DefaultUploadLimits()
	if d.ExpiredRetention != ExpiredRetention || d.CleanupDelay != CleanupDelay {
		t.Fatalf("cleanup defaults drifted: %+v", d)
	}
	if d.MaxBytes != MaxUploadBytes || d.PartSize != UploadPartSize || d.MaxOpen != MaxOpenUploads || d.SessionTTL != UploadTTL || d.MaxRetries != MaxUploadRetries {
		t.Fatalf("defaults drifted from the constants: %+v", d)
	}
	if d.presignPartTTL() != PresignPartTTL || d.presignPartTTL() >= d.SessionTTL {
		t.Errorf("presign TTL %v must equal %v and stay below the session %v", d.presignPartTTL(), PresignPartTTL, d.SessionTTL)
	}
}

func TestInitUsesConfiguredLimits(t *testing.T) {
	lim := UploadLimits{MaxBytes: 50 << 20, PartSize: 5 << 20, MaxOpen: 1, SessionTTL: 2 * time.Hour}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	newSvc := func(db *fakeUploads) *Upload {
		svc, _ := newUploadSvc(db, &fakeStorage{})
		svc.WithLimits(lim).now = func() time.Time { return now }
		return svc
	}

	db := &fakeUploads{}
	out, err := newSvc(db).Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 12 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Parts) != 3 || out.PartSize != 5<<20 || db.created.PartSize != 5<<20 || db.created.PartCount != 3 {
		t.Errorf("parts=%d partSize=%d stored=%d/%d, want 3 parts of 5 MiB", len(out.Parts), out.PartSize, db.created.PartSize, db.created.PartCount)
	}
	if !db.created.ExpiresAt.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("expires %v, want %v", db.created.ExpiresAt, now.Add(2*time.Hour))
	}

	db = &fakeUploads{}
	_, err = newSvc(db).Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 51 << 20})
	if ecode(t, err) != "validation_failed" || db.created != nil {
		t.Errorf("over the configured maximum: code=%q created=%v", ecode(t, err), db.created != nil)
	}
	if got := apperr.From(err).Fields["size"]; !strings.Contains(got, "50 MiB") {
		t.Errorf("size message %q should state the configured maximum", got)
	}

	_, err = newSvc(&fakeUploads{open: 1}).Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 5})
	if ecode(t, err) != "limit_reached" {
		t.Errorf("configured open-upload cap: %q", ecode(t, err))
	}
	// The default cap (5) still allows one open upload.
	def, _ := newUploadSvc(&fakeUploads{open: 1}, &fakeStorage{})
	if _, err := def.Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 5}); err != nil {
		t.Errorf("default limits: %v", err)
	}
}

func TestSizeText(t *testing.T) {
	for in, want := range map[int64]string{5 << 30: "5 GiB", 200 << 20: "200 MiB", 1536 << 20: "1536 MiB", 500: "500 bytes"} {
		if got := sizeText(in); got != want {
			t.Errorf("sizeText(%d)=%q want %q", in, got, want)
		}
	}
}

func TestInitLimitsAndDisabled(t *testing.T) {
	db := &fakeUploads{open: MaxOpenUploads}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	_, err := svc.Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 5})
	if ecode(t, err) != "limit_reached" {
		t.Errorf("open cap: %q", ecode(t, err))
	}
	off, _ := newUploadSvc(&fakeUploads{}, nil)
	_, err = off.Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 5})
	if apperr.From(err).Status != http.StatusServiceUnavailable || ecode(t, err) != "uploads_disabled" {
		t.Errorf("disabled: %v", err)
	}
}

// ---------- complete ----------

func goodParts(size int64) ([]storage.Part, []PartIn) {
	n := int((size + UploadPartSize - 1) / UploadPartSize)
	var have []storage.Part
	var in []PartIn
	for i := 1; i <= n; i++ {
		sz := int64(UploadPartSize)
		if i == n {
			sz = size - int64(n-1)*UploadPartSize
		}
		have = append(have, storage.Part{Number: i, ETag: `"e` + string(rune('0'+i)) + `"`, Size: sz})
		in = append(in, PartIn{PartNumber: i, ETag: "e" + string(rune('0'+i))})
	}
	return have, in
}

func TestCompleteHappyPath(t *testing.T) {
	size := int64(UploadPartSize + 100)
	have, in := goodParts(size)
	st := &fakeStorage{parts: have, size: size, head: mp4Head()}
	db := &fakeUploads{sess: session("INITIATED", size)}
	svc, _ := newUploadSvc(db, st)
	if err := svc.Complete(context.Background(), testUser, testCI, upID, in); err != nil {
		t.Fatal(err)
	}
	if !st.completed || !db.finished || db.reverted || db.discarded {
		t.Errorf("completed=%v finished=%v reverted=%v discarded=%v", st.completed, db.finished, db.reverted, db.discarded)
	}
}

func TestCompleteIsIdempotent(t *testing.T) {
	db := &fakeUploads{sess: session("COMPLETED", 10)}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if err := svc.Complete(context.Background(), testUser, testCI, upID, []PartIn{{1, "x"}}); err != nil {
		t.Fatal(err)
	}
	if db.begun {
		t.Error("a completed upload must not be completed again")
	}
}

func TestCompletePartsMismatch(t *testing.T) {
	size := int64(UploadPartSize + 100)
	have, in := goodParts(size)
	cases := map[string]func(){
		"missing part": func() { have = have[:1] },
		"wrong etag":   func() { have[0].ETag = `"other"` },
		"wrong size":   func() { have[1].Size = 7 },
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		have, in = goodParts(size)
		cases[name]()
		st := &fakeStorage{parts: have, size: size, head: mp4Head()}
		db := &fakeUploads{sess: session("INITIATED", size)}
		svc, _ := newUploadSvc(db, st)
		err := svc.Complete(context.Background(), testUser, testCI, upID, in)
		if ecode(t, err) != "parts_mismatch" || !db.reverted || st.completed || db.finished {
			t.Errorf("%s: code=%q reverted=%v completed=%v finished=%v", name, ecode(t, err), db.reverted, st.completed, db.finished)
		}
	}
}

func TestCompleteRejectsBadPartList(t *testing.T) {
	size := int64(UploadPartSize + 100)
	for name, in := range map[string][]PartIn{
		"too few":   {{1, "a"}},
		"duplicate": {{1, "a"}, {1, "b"}},
		"range":     {{1, "a"}, {3, "b"}},
		"no etag":   {{1, "a"}, {2, ""}},
	} {
		db := &fakeUploads{sess: session("INITIATED", size)}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if err := svc.Complete(context.Background(), testUser, testCI, upID, in); ecode(t, err) != "validation_failed" || db.begun {
			t.Errorf("%s: %q begun=%v", name, ecode(t, err), db.begun)
		}
	}
}

func TestCompleteRejectsNonVideoAndSizeMismatch(t *testing.T) {
	size := int64(100)
	have, in := goodParts(size)

	st := &fakeStorage{parts: have, size: size, head: []byte("<html>not a video at all</html>")}
	db := &fakeUploads{sess: session("INITIATED", size)}
	svc, _ := newUploadSvc(db, st)
	err := svc.Complete(context.Background(), testUser, testCI, upID, in)
	if ecode(t, err) != "invalid_file" || !db.discarded || len(st.deleted) != 1 || db.finished {
		t.Errorf("non-video: %q discarded=%v deleted=%v", ecode(t, err), db.discarded, st.deleted)
	}

	st = &fakeStorage{parts: have, size: size + 1, head: mp4Head()}
	db = &fakeUploads{sess: session("INITIATED", size)}
	svc, _ = newUploadSvc(db, st)
	err = svc.Complete(context.Background(), testUser, testCI, upID, in)
	if ecode(t, err) != "size_mismatch" || !db.discarded {
		t.Errorf("size: %q", ecode(t, err))
	}
}

func TestCompleteInProgressAndClosed(t *testing.T) {
	for status, want := range map[string]string{"COMPLETING": "upload_in_progress", "ABORTED": "upload_closed", "EXPIRED": "upload_closed"} {
		db := &fakeUploads{sess: session(status, 10)}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if err := svc.Complete(context.Background(), testUser, testCI, upID, []PartIn{{1, "x"}}); ecode(t, err) != want {
			t.Errorf("%s: %q", status, ecode(t, err))
		}
	}
	db := &fakeUploads{}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if err := svc.Complete(context.Background(), testUser, testCI, "not-a-uuid", nil); ecode(t, err) != "upload_not_found" {
		t.Errorf("bad id: %q", ecode(t, err))
	}
	db.sess = nil
	if err := svc.Complete(context.Background(), testUser, testCI, upID, nil); ecode(t, err) != "upload_not_found" {
		t.Errorf("unknown/other user's upload: %q", ecode(t, err))
	}
}

func TestValidContainer(t *testing.T) {
	ok := [][]byte{mp4Head(), {0x1A, 0x45, 0xDF, 0xA3, 1, 2}}
	for i, b := range ok {
		if !validContainer(b) {
			t.Errorf("valid #%d rejected", i)
		}
	}
	for i, b := range [][]byte{nil, []byte("MZ\x90\x00"), []byte("GIF89a......."), []byte("RIFF\x00\x00\x00\x00WAVEfmt "), []byte("RIFF\x00\x00\x00\x00AVI LIST")} {
		if validContainer(b) {
			t.Errorf("invalid #%d accepted", i)
		}
	}
}

// ---------- abort / status ----------

func TestAbort(t *testing.T) {
	st := &fakeStorage{}
	db := &fakeUploads{sess: session("INITIATED", 10)}
	svc, _ := newUploadSvc(db, st)
	if err := svc.Abort(context.Background(), testUser, testCI, upID); err != nil {
		t.Fatal(err)
	}
	if len(st.aborted) != 1 || !db.discarded {
		t.Errorf("aborted=%v discarded=%v", st.aborted, db.discarded)
	}

	db = &fakeUploads{sess: session("COMPLETED", 10)}
	db.sess.VideoStatus = "PUBLISHED"
	svc, _ = newUploadSvc(db, st)
	if err := svc.Abort(context.Background(), testUser, testCI, upID); ecode(t, err) != "already_published" || db.discarded {
		t.Errorf("published: %q", ecode(t, err))
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		sess        string
		video       string
		wantStatus  string
		wantErrText bool
	}{
		{"INITIATED", "PROCESSING", "processing", false},
		{"COMPLETED", "PROCESSING", "processing", false},
		{"COMPLETED", "READY", "ready", false},
		{"COMPLETED", "PUBLISHED", "published", false},
		{"COMPLETED", "FAILED", "failed", true},
		{"EXPIRED", "PROCESSING", "failed", true},
	}
	for _, c := range cases {
		db := &fakeUploads{sess: session(c.sess, 10)}
		db.sess.VideoStatus = c.video
		svc, _ := newUploadSvc(db, &fakeStorage{})
		out, err := svc.Status(context.Background(), testUser, upID)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != c.wantStatus || (out.Error != nil) != c.wantErrText || out.Thumbnails == nil {
			t.Errorf("%s/%s -> %+v", c.sess, c.video, out)
		}
	}
}

// ---------- assets ----------

func TestAssetValidation(t *testing.T) {
	svc, _ := newUploadSvc(&fakeUploads{sess: session("COMPLETED", 10)}, &fakeStorage{})
	bad := []AssetInput{
		{Kind: "thumbnail", ContentType: "image/gif", Size: 100},
		{Kind: "thumbnail", ContentType: "image/png", Size: MaxThumbBytes + 1},
		{Kind: "caption", ContentType: "text/vtt", Size: 100, Lang: "EN US"},
		{Kind: "caption", ContentType: "text/html", Size: 100, Lang: "en"},
		{Kind: "video", ContentType: "video/mp4", Size: 5},
	}
	for i, in := range bad {
		if _, err := svc.Asset(context.Background(), testUser, testCI, upID, in); ecode(t, err) != "validation_failed" {
			t.Errorf("bad #%d: %q", i, ecode(t, err))
		}
	}
	out, err := svc.Asset(context.Background(), testUser, testCI, upID, AssetInput{Kind: "caption", ContentType: "text/vtt", Size: 100, Lang: "bn"})
	if err != nil || out.AssetID == "" || out.URL == "" {
		t.Errorf("good caption: %+v %v", out, err)
	}
}

// ---------- publish ----------

func yes() *bool           { b := true; return &b }
func str(s string) *string { return &s }

func TestPublishRequiresConsent(t *testing.T) {
	db := &fakeUploads{}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	for _, c := range []*bool{nil, new(bool)} {
		err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: c})
		if ecode(t, err) != "validation_failed" || db.pub != nil {
			t.Errorf("consent=%v -> %q published=%v", c, ecode(t, err), db.pub != nil)
		}
	}
}

func TestPublishErrorMapping(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		repository.ErrNotReady:         {409, "not_ready"},
		repository.ErrAlreadyPublished: {409, "already_published"},
		repository.ErrModerated:        {403, "video_removed"},
		repository.ErrShortTooLarge:    {422, "file_too_large_for_short"},
		repository.ErrVideoNotFound:    {404, "video_not_found"},
	}
	for in, want := range cases {
		db := &fakeUploads{pubErr: in}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes()})
		if e := apperr.From(err); e.Status != want.status || e.Code != want.code {
			t.Errorf("%v -> %d %s", in, e.Status, e.Code)
		}
	}
	db := &fakeUploads{pubErr: errors.New("boom")}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes()}); apperr.From(err).Status != 500 {
		t.Errorf("unknown error must be 500: %v", err)
	}
}

func TestPublishPublicFansOut(t *testing.T) {
	for vis, wantFan := range map[string]bool{"PUBLIC": true, "UNLISTED": false, "PRIVATE": false} {
		db := &fakeUploads{pubVis: vis}
		svc, n := newUploadSvc(db, &fakeStorage{})
		if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Title: str("Hello")}); err != nil {
			t.Fatal(err)
		}
		if db.fanned != wantFan || (len(n.ids) == 2) != wantFan {
			t.Errorf("%s: fanned=%v notified=%v", vis, db.fanned, n.ids)
		}
		if db.pub.ConsentVersion != ConsentVersion || len(db.pub.IPHash) != 32 || *db.pub.Patch.Title != "Hello" {
			t.Errorf("%s: params %+v", vis, db.pub)
		}
	}
}

func TestPublishValidatesMetadata(t *testing.T) {
	long := make([]rune, 101)
	for i := range long {
		long[i] = 'a'
	}
	tags := []string{"ok", "bad,tag"}
	vis := "everyone"
	cases := []MetaInput{
		{Consent: yes(), Title: str("")},
		{Consent: yes(), Title: str(string(long))},
		{Consent: yes(), Tags: &tags},
		{Consent: yes(), Visibility: &vis},
	}
	for i, in := range cases {
		db := &fakeUploads{}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if err := svc.Publish(context.Background(), testUser, testCI, vidID, in); ecode(t, err) != "validation_failed" || db.pub != nil {
			t.Errorf("case %d: %q", i, ecode(t, err))
		}
	}
}

func TestPublishChecksThumbnailImage(t *testing.T) {
	thumbID := auth.NewUUIDv7()
	key := "thumbs/" + vidID + "/x.png"
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0}

	st := &fakeStorage{objects: map[string][]byte{key: []byte("<script>alert(1)</script>")}}
	db := &fakeUploads{thumbs: map[string]string{thumbID: key}}
	svc, _ := newUploadSvc(db, st)
	err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Thumbnail: str(thumbID)})
	if ecode(t, err) != "validation_failed" || db.pub != nil || len(st.deleted) != 1 {
		t.Errorf("fake image: %q deleted=%v", ecode(t, err), st.deleted)
	}

	st = &fakeStorage{objects: map[string][]byte{key: png}}
	db = &fakeUploads{thumbs: map[string]string{thumbID: key}, pubVis: "PRIVATE"}
	svc, _ = newUploadSvc(db, st)
	if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Thumbnail: str(thumbID)}); err != nil {
		t.Fatal(err)
	}
	if db.pub.PrimaryThumbID != thumbID {
		t.Errorf("primary thumb %q", db.pub.PrimaryThumbID)
	}
}

func TestPublishConvertsCaptionsToVTT(t *testing.T) {
	capID := auth.NewUUIDv7()
	key := "captions/" + vidID + "/en.srt"
	srt := "1\n00:00:01,000 --> 00:00:02,000\nHello\n"
	st := &fakeStorage{objects: map[string][]byte{key: []byte(srt)}}
	db := &fakeUploads{caps: map[string][2]string{capID: {"en", key}}, pubVis: "PRIVATE"}
	svc, _ := newUploadSvc(db, st)
	if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Captions: []CaptionRef{{Lang: "en", AssetID: capID}}}); err != nil {
		t.Fatal(err)
	}
	final := "captions/" + vidID + "/en.vtt"
	got := string(st.objects[final])
	if len(got) < 6 || got[:6] != "WEBVTT" || !contains(got, "00:00:01.000 --> 00:00:02.000") {
		t.Errorf("not converted: %q", got)
	}
	if len(db.pub.Captions) != 1 || db.pub.Captions[0].Key != final {
		t.Errorf("captions %+v", db.pub.Captions)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------- janitor / helpers ----------

func TestExpireStaleAbortsMultipart(t *testing.T) {
	st := &fakeStorage{}
	svc, _ := newUploadSvc(&fakeUploads{}, st)
	n, err := svc.ExpireStale(context.Background())
	if err != nil || n != 1 || len(st.aborted) != 1 {
		t.Errorf("n=%d err=%v aborted=%v", n, err, st.aborted)
	}
}

func TestTagSlugAndTitle(t *testing.T) {
	if got := tagSlug("  Hello   World_Foo "); got != "hello-world-foo" {
		t.Errorf("slug %q", got)
	}
	if got := titleFromFilename("noext"); got == "" {
		t.Error("empty title")
	}
	if got := titleFromFilename(".mp4"); got != "Untitled" {
		t.Errorf("got %q", got)
	}
}

func TestStatusExplainsWhyProcessingFailed(t *testing.T) {
	cases := []struct{ code, want string }{
		{"too_long", "longer than the maximum allowed length"},
		{"resolution_too_high", "1080p"},
		{"unsupported_codec", "codec"},
		{"cancelled", "Processing was cancelled."},
		{"", genericProcessingFailure},
		{"ffmpeg_failed", genericProcessingFailure}, // internal conditions are never described to the client
		{"scanner_unavailable", genericProcessingFailure},
		{"blocked_content/../etc", genericProcessingFailure}, // unknown or hostile codes fall back to the generic sentence
	}
	for _, c := range cases {
		db := &fakeUploads{sess: session("COMPLETED", 10), progress: repository.Progress{Moderation: "ACTIVE", FailCode: c.code}}
		db.sess.VideoStatus = "FAILED"
		svc, _ := newUploadSvc(db, &fakeStorage{})
		out, err := svc.Status(context.Background(), testUser, upID)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != "failed" || out.Error == nil || !strings.Contains(*out.Error, c.want) {
			t.Errorf("code %q -> %+v, want text containing %q", c.code, out, c.want)
		}
	}
}

func TestEveryWorkerFailureCodeHasAUserText(t *testing.T) {
	// the permanent codes the worker can write (internal/worker); keep this list in sync with probe/policy.go
	for _, code := range []string{"too_long", "too_large", "short_too_large", "resolution_too_high", "resolution_too_low", "frame_rate_too_high",
		"unsupported_container", "unsupported_codec", "no_video_stream", "invalid_file", "invalid_duration", "too_many_streams",
		"size_mismatch", "original_missing", "cancelled"} {
		if failureMessage(code) == genericProcessingFailure {
			t.Errorf("code %q has no specific text", code)
		}
	}
}
