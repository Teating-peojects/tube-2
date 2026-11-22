package handler_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/metrics"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/service"
)

// freedPrefixes is the number of per-video prefixes service.Upload frees: uploads/, thumbs/, captions/ and hls/.
const freedPrefixes = 4

// The storage sweeper against the real schema (migrations 000011/000012) and the real repository, with stubbed
// object storage. Covers: what is freed for EXPIRED vs ABORTED sessions, revival resetting the bookkeeping,
// failure backoff without starvation, finalizing expired uploads, and leaving open uploads alone.
func TestUploadStorageCleanupAgainstRealDatabase(t *testing.T) {
	e := setup(t)
	e.creator(1, "Cleanup Studio", "ACTIVE", false, 0)
	store := &retryStore{}
	counters := metrics.New()
	mk := func(l service.UploadLimits) *service.Upload {
		return service.NewUpload(repository.NewUpload(e.app), store, retryLimiter{}, []byte("0123456789abcdef0123456789abcdef"),
			media.New("https://cdn.test"), retryNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil))).WithLimits(l).WithMetrics(counters)
	}
	limits := service.DefaultUploadLimits()
	limits.CleanupDelay, limits.ExpiredRetention = 0, 3*time.Hour // the helper below expires sessions 1h ago: inside the window
	svc := mk(limits)
	patient := limits
	patient.CleanupDelay = time.Hour
	svcPatient := mk(patient)

	ctx := context.Background()
	owner := &auth.Principal{UserID: cid(1), EmailVerified: true, IsCreator: true}
	ci := service.ClientInfo{IP: "203.0.113.9", RequestID: "r1"}
	code := func(err error) string {
		if err == nil {
			return ""
		}
		return apperr.From(err).Code
	}
	init := func() service.InitInput { return service.InitInput{Filename: "clip.mp4", Size: 40 << 20} }

	type row struct {
		status   string
		cleaned  bool
		attempts int
		nextAt   *time.Time
	}
	read := func(uploadID string) row {
		t.Helper()
		var r row
		var cleaned *time.Time
		if err := e.pool.QueryRow(ctx, `SELECT status::text, storage_cleaned_at, cleanup_attempts, cleanup_next_at FROM upload_sessions WHERE id = $1::uuid`,
			uploadID).Scan(&r.status, &cleaned, &r.attempts, &r.nextAt); err != nil {
			t.Fatal(err)
		}
		r.cleaned = cleaned != nil
		return r
	}
	expire := func(uploadID string) {
		t.Helper()
		e.exec(`UPDATE upload_sessions SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE id = $1::uuid`, uploadID)
		if n, err := svc.ExpireStale(ctx); err != nil || n != 1 {
			t.Fatalf("janitor expired %d sessions, err=%v", n, err)
		}
	}
	prefixesFor := func(videoID string) int {
		store.mu.Lock()
		defer store.mu.Unlock()
		n := 0
		for _, p := range store.prefixes {
			if strings.Contains(p, videoID) {
				n++
			}
		}
		return n
	}
	sweep := func(s *service.Upload) service.SweepResult {
		t.Helper()
		r, err := s.SweepStorage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// an open upload that must never be touched
	open, err := svc.Init(ctx, owner, ci, init())
	if err != nil {
		t.Fatal(err)
	}

	// 1. EXPIRED: only the multipart upload is freed, the key stays (a retry reuses it); the delay is honoured
	u1, err := svc.Init(ctx, owner, ci, init())
	if err != nil {
		t.Fatal(err)
	}
	expire(u1.UploadID)
	if r := sweep(svcPatient); r != (service.SweepResult{}) {
		t.Fatalf("closed for less than the delay, nothing may happen: %+v", r)
	}
	if r := sweep(svc); r.Cleaned != 1 || r.Failed != 0 {
		t.Fatalf("expired session: %+v", r)
	}
	if got := read(u1.UploadID); got != (row{"EXPIRED", true, 0, nil}) || prefixesFor(u1.VideoID) != 0 {
		t.Fatalf("expired session after the sweep: %+v prefixes=%d", got, prefixesFor(u1.VideoID))
	}
	if r := sweep(svc); r != (service.SweepResult{}) {
		t.Fatalf("a cleaned session must not be picked again: %+v", r)
	}

	// 2. a retry brings the bookkeeping back to "storage in use"
	if _, err := svc.Retry(ctx, owner, ci, u1.UploadID); err != nil {
		t.Fatal(err)
	}
	if got := read(u1.UploadID); got != (row{"INITIATED", false, 0, nil}) {
		t.Fatalf("after retry: %+v", got)
	}
	if r := sweep(svc); r != (service.SweepResult{}) {
		t.Fatalf("a reopened upload must be left alone: %+v", r)
	}

	// 3. cancelled: the multipart upload and every object of the video (original, thumbnails, captions, renditions) go
	if err := svc.Abort(ctx, owner, ci, u1.UploadID); err != nil {
		t.Fatal(err)
	}
	if r := sweep(svc); r.Cleaned != 1 {
		t.Fatalf("aborted session: %+v", r)
	}
	if got := read(u1.UploadID); got.status != "ABORTED" || !got.cleaned || prefixesFor(u1.VideoID) != freedPrefixes {
		t.Fatalf("aborted session after the sweep: %+v prefixes=%d", got, prefixesFor(u1.VideoID))
	}

	// 3b. the race the provider upload id guards against: the sweeper holds an EXPIRED row, a retry revives the
	// session and it expires AGAIN (EXPIRED once more, but with a new multipart upload) before the sweeper reports.
	// The stale result must not mark the new multipart upload as freed.
	repo := repository.NewUpload(e.app)
	racy, err := svc.Init(ctx, owner, ci, init())
	if err != nil {
		t.Fatal(err)
	}
	expire(racy.UploadID)
	pending, err := repo.ClosedUploads(ctx, 0, 50)
	if err != nil || len(pending) != 1 || pending[0].ID != racy.UploadID {
		t.Fatalf("pending cleanups: %+v %v", pending, err)
	}
	if _, err := svc.Retry(ctx, owner, ci, racy.UploadID); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.MarkCleaned(ctx, pending[0]); err != nil || ok { // while revived (INITIATED)
		t.Fatalf("a stale result must not mark a revived session as cleaned: ok=%v err=%v", ok, err)
	}
	expire(racy.UploadID) // EXPIRED again, now holding the second multipart upload
	if ok, err := repo.MarkCleaned(ctx, pending[0]); err != nil || ok {
		t.Fatalf("a stale result must not mark the NEW multipart upload as freed: ok=%v err=%v", ok, err)
	}
	if n, err := repo.CleanupFailed(ctx, pending[0]); err != nil || n != 0 {
		t.Fatalf("a stale failure must not back off the new multipart upload: attempts=%d err=%v", n, err)
	}
	if got := read(racy.UploadID); got != (row{"EXPIRED", false, 0, nil}) {
		t.Fatalf("session changed by a stale sweeper: %+v", got)
	}
	if r := sweep(svc); r.Cleaned != 1 { // the real sweep frees the second multipart upload
		t.Fatalf("sweep after the race: %+v", r)
	}
	if got := read(racy.UploadID); got != (row{"EXPIRED", true, 0, nil}) {
		t.Fatalf("after the real sweep: %+v", got)
	}

	// 4. failures back off, and a failing row does not starve the next one
	bad, err := svc.Init(ctx, owner, ci, init())
	if err != nil {
		t.Fatal(err)
	}
	good, err := svc.Init(ctx, owner, ci, init())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{bad.UploadID, good.UploadID} {
		if err := svc.Abort(ctx, owner, ci, id); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	store.failVideo = bad.VideoID
	store.mu.Unlock()
	if r := sweep(svc); r.Failed != 1 || r.Cleaned != 1 {
		t.Fatalf("one failing row must not stop the other: %+v", r)
	}
	b := read(bad.UploadID)
	if b.cleaned || b.attempts != 1 || b.nextAt == nil || !b.nextAt.After(time.Now()) || !read(good.UploadID).cleaned {
		t.Fatalf("after the failed pass: bad=%+v good=%+v", b, read(good.UploadID))
	}
	if r := sweep(svc); r != (service.SweepResult{}) {
		t.Fatalf("a failed row must wait out its backoff: %+v", r)
	}
	e.exec(`UPDATE upload_sessions SET cleanup_next_at = now() - interval '1 second' WHERE id = $1::uuid`, bad.UploadID)
	if r := sweep(svc); r.Failed != 1 {
		t.Fatalf("still failing: %+v", r)
	}
	if got := read(bad.UploadID); got.attempts != 2 || got.cleaned {
		t.Fatalf("second failure: %+v", got)
	}
	var minutes float64
	if err := e.pool.QueryRow(ctx, `SELECT extract(epoch FROM cleanup_next_at - now()) / 60 FROM upload_sessions WHERE id = $1::uuid`, bad.UploadID).Scan(&minutes); err != nil || minutes < 1 || minutes > 2.1 {
		t.Fatalf("second backoff should be about 2 minutes, got %.2f (%v)", minutes, err)
	}
	store.mu.Lock()
	store.failVideo = ""
	store.mu.Unlock()
	e.exec(`UPDATE upload_sessions SET cleanup_next_at = now() - interval '1 second' WHERE id = $1::uuid`, bad.UploadID)
	if r := sweep(svc); r.Cleaned != 1 || r.Failed != 0 {
		t.Fatalf("storage recovered: %+v", r)
	}
	if got := read(bad.UploadID); !got.cleaned || got.nextAt != nil || prefixesFor(bad.VideoID) != freedPrefixes {
		t.Fatalf("after recovery: %+v prefixes=%d", got, prefixesFor(bad.VideoID))
	}

	// 5. an expired upload past its retry window is final: not retryable any more, storage deleted
	old, err := svc.Init(ctx, owner, ci, init())
	if err != nil {
		t.Fatal(err)
	}
	expire(old.UploadID)
	e.exec(`UPDATE upload_sessions SET created_at = now() - interval '6 hours', expires_at = now() - interval '4 hours' WHERE id = $1::uuid`, old.UploadID)
	if st, err := svc.Status(ctx, owner, old.UploadID); err != nil || st.Retryable {
		t.Fatalf("past the window the status must not advertise a retry: %+v %v", st, err)
	}
	if _, err := svc.Retry(ctx, owner, ci, old.UploadID); code(err) != "not_retryable" {
		t.Fatalf("retry past the window: %q", code(err))
	}
	if r := sweep(svc); r.Finalized != 1 || r.Cleaned != 1 {
		t.Fatalf("finalize pass: %+v", r)
	}
	if got := read(old.UploadID); got.status != "ABORTED" || !got.cleaned || prefixesFor(old.VideoID) != freedPrefixes {
		t.Fatalf("finalized session: %+v prefixes=%d", got, prefixesFor(old.VideoID))
	}

	// 6. the open upload was never touched, and the counters saw the whole story
	if got := read(open.UploadID); got != (row{"INITIATED", false, 0, nil}) || prefixesFor(open.VideoID) != 0 {
		t.Fatalf("the open upload was touched: %+v", got)
	}
	for name, want := range map[string]int64{"cleanup_sessions": 6, "cleanup_failed": 2, "upload_expired_finalized": 1, "upload_retry": 2, "cleanup_objects_deleted": 4 * freedPrefixes} {
		if got := counters.Get(name); got != want {
			values, _ := counters.Snapshot()
			t.Errorf("%s = %d, want %d (all: %v)", name, got, want, values)
		}
	}
}
