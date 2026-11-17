package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/service"
)

// These tests exercise the contract between the API (cancel / retry, as redxtube_app) and the worker
// (as redxtube_worker) on one real database. They need TEST_WORKER_APP_DATABASE_URL as well.

type allowAll struct{}

func (allowAll) Hit(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 1, time.Minute, nil
}
func (allowAll) Peek(context.Context, string) (int64, time.Duration, error) { return 0, 0, nil }
func (allowAll) Reset(context.Context, string) error                        { return nil }

func processingSvc(t *testing.T, e *pgEnv) *service.Processing {
	t.Helper()
	dsn := os.Getenv("TEST_WORKER_APP_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_WORKER_APP_DATABASE_URL not set")
	}
	app, err := pgxpool.New(ctxb, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return service.NewProcessing(repository.NewProcessing(app), allowAll{}, []byte("0123456789abcdef0123456789abcdef"), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func principal(id string, role auth.Role) *auth.Principal {
	return &auth.Principal{UserID: id, Role: role, EmailVerified: true, IsCreator: true}
}

func (e *pgEnv) newUser() string {
	id := uuid.NewString()
	e.exec(`INSERT INTO users (id, username, email, password_hash, display_name, is_creator, is_verified)
VALUES ($1::uuid, $2, $3, '$argon2id$v=19$m=8192,t=1,p=1$c2FsdA$a2V5', 'Other', true, true)`, id, "u_"+id[:8], id[:8]+"@test.local")
	return id
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("want error %q, got %v", code, err)
	}
}

func TestProcessingCancelAuthorizationAndAudit(t *testing.T) {
	e := newPGEnv(t)
	svc := processingSvc(t, e)
	ci := service.ClientInfo{IP: "203.0.113.9", RequestID: "req-1"}
	stranger, mod := e.newUser(), e.newUser()

	// someone else's video: indistinguishable from a video that does not exist
	v1 := e.video(3)
	wantCode(t, svc.Cancel(ctxb, principal(stranger, auth.RoleUser), ci, v1, ""), "target_not_found")
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, v1) != "PROCESSING" {
		t.Fatal("a stranger cancelled someone else's video")
	}
	wantCode(t, svc.Cancel(ctxb, principal(e.user, auth.RoleUser), ci, "not-a-uuid", ""), "target_not_found")

	// the owner can; jobs are cancelled, the video fails, the action is audited
	if err := svc.Cancel(ctxb, principal(e.user, auth.RoleUser), ci, v1, "wrong file"); err != nil {
		t.Fatal(err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, v1) != "FAILED" ||
		e.str(`SELECT status::text FROM video_processing_jobs WHERE video_id = $1::uuid`, v1) != "CANCELLED" {
		t.Fatal("video not FAILED / job not CANCELLED")
	}
	if !strings.HasPrefix(e.str(`SELECT error_message FROM video_processing_jobs WHERE video_id = $1::uuid`, v1), "cancelled: ") {
		t.Fatal("cancel code not stored")
	}
	if e.num(`SELECT count(*) FROM audit_logs WHERE action = 'processing.cancelled' AND target_id = $1 AND actor_user_id = $2::uuid
AND metadata->>'reason' = 'wrong file' AND metadata->>'by' = 'owner'`, v1, e.user) != 1 {
		t.Fatal("cancel not audited with actor, reason and role")
	}
	// cancelling twice is a state error, not a crash
	wantCode(t, svc.Cancel(ctxb, principal(e.user, auth.RoleUser), ci, v1, ""), "invalid_state")

	// a moderator may cancel any video; a video that is not processing cannot be cancelled
	v2 := e.video(3)
	if err := svc.Cancel(ctxb, principal(mod, auth.RoleModerator), ci, v2, ""); err != nil {
		t.Fatal(err)
	}
	if e.num(`SELECT count(*) FROM audit_logs WHERE action = 'processing.cancelled' AND target_id = $1 AND metadata->>'by' = 'staff'`, v2) != 1 {
		t.Fatal("staff cancel not audited as staff")
	}
	v3 := e.video(3)
	e.exec(`UPDATE videos SET status = 'FAILED' WHERE id = $1::uuid`, v3)
	wantCode(t, svc.Cancel(ctxb, principal(e.user, auth.RoleUser), ci, v3, ""), "invalid_state")
	// the reason, when given, is validated
	v4 := e.video(3)
	wantCode(t, svc.Cancel(ctxb, principal(e.user, auth.RoleUser), ci, v4, strings.Repeat("x", 501)), "validation_failed")
}

func TestProcessingRetry(t *testing.T) {
	e := newPGEnv(t)
	svc := processingSvc(t, e)
	ci := service.ClientInfo{IP: "203.0.113.9", RequestID: "req-2"}
	mod := e.newUser()
	staff := principal(mod, auth.RoleModerator)

	failedVideo := func(withSession bool) string {
		vid := e.video(3)
		if withSession {
			e.exec(`INSERT INTO upload_sessions (id, user_id, video_id, storage_key, provider_upload_id, declared_size_bytes, declared_content_type,
  part_size_bytes, part_count, expires_at, status, completed_at)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3, 'p', 1000, 'video/mp4', 5242880, 1, now() + interval '1 day', 'COMPLETED', now())`,
				e.user, vid, "uploads/"+vid+"/original.mp4")
		}
		s := e.store("w")
		j := claimFor(t, s, vid)
		if _, err := s.Fail(ctxb, j, permanent("too_long", "Too long")); err != nil {
			t.Fatal(err)
		}
		return vid
	}

	vid := failedVideo(true)
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "FAILED" {
		t.Fatal("fixture")
	}
	// only staff, and a reason is required
	wantCode(t, svc.Retry(ctxb, principal(e.user, auth.RoleUser), ci, vid, "please"), "forbidden")
	wantCode(t, svc.Retry(ctxb, staff, ci, vid, ""), "validation_failed")

	if err := svc.Retry(ctxb, staff, ci, vid, "limit raised"); err != nil {
		t.Fatal(err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "PROCESSING" {
		t.Fatal("video not back in the pipeline")
	}
	if e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != 1 ||
		e.str(`SELECT status::text FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != "QUEUED" ||
		e.str(`SELECT job_type::text FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != "METADATA" {
		t.Fatal("old jobs were not replaced by one fresh METADATA job")
	}
	if e.num(`SELECT count(*) FROM audit_logs WHERE action = 'processing.retried' AND target_id = $1 AND metadata->>'reason' = 'limit raised'
AND metadata->>'previous_error' LIKE 'too_long:%'`, vid) != 1 {
		t.Fatal("retry not audited with reason and previous error")
	}
	// the worker can claim the new job: the loop is closed
	if j := claimFor(t, e.store("w2"), vid); j.Type != "METADATA" || j.Attempts != 1 {
		t.Fatalf("fresh job: %+v", j)
	}
	// a video that is processing again cannot be retried
	wantCode(t, svc.Retry(ctxb, staff, ci, vid, "again"), "invalid_state")

	// refusals: removed by moderation, upload never completed, unknown video
	removed := failedVideo(true)
	e.exec(`UPDATE videos SET moderation_status = 'REMOVED' WHERE id = $1::uuid`, removed)
	wantCode(t, svc.Retry(ctxb, staff, ci, removed, "no"), "invalid_state")
	noSession := failedVideo(false)
	wantCode(t, svc.Retry(ctxb, staff, ci, noSession, "no"), "invalid_state")
	wantCode(t, svc.Retry(ctxb, staff, ci, uuid.NewString(), "no"), "target_not_found")
}

// The point of cancelling: a running worker stops, its process tree dies, and whatever it still
// tries to write is refused.
func TestCancelKillsARunningJob(t *testing.T) {
	e := newPGEnv(t)
	svc := processingSvc(t, e)
	vid := e.video(3)
	e.exec(`UPDATE video_processing_jobs SET status = 'CANCELLED' WHERE status = 'QUEUED' AND video_id <> $1::uuid`, vid) // other tests' leftovers

	mem := newMem()
	content := []byte("not really a video, the scanner never lets it get that far")
	mem.objs["uploads/"+vid+"/original.mp4"] = content
	e.exec(`UPDATE videos SET file_size_bytes = $2 WHERE id = $1::uuid`, vid, len(content))

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := filepath.Join(dir, "slow-scan.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho $$ > "+pidFile+"\nsleep 120\n"), 0o700)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pg := NewPG(e.worker, "cancel-test")
	pipe := &Pipeline{Store: pg, Storage: mem, Log: log, WorkDir: t.TempDir(), FFmpeg: "ffmpeg", FFprobe: "ffprobe",
		Scanner: &ExecScanner{Args: []string{script, "{file}"}, Timeout: 3 * time.Minute}}
	w := New(Config{Concurrency: 1, PollInterval: 50 * time.Millisecond, HeartbeatEvery: 200 * time.Millisecond, WorkDir: pipe.WorkDir}, pg, pipe, log)
	wctx, stop := context.WithCancel(ctxb)
	done := make(chan struct{})
	go func() { w.Run(wctx); close(done) }()
	defer func() { stop(); <-done }()

	var pid int
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			break
		}
	}
	if pid == 0 {
		t.Fatal("the scanner never started: the worker did not pick the job up")
	}

	cancelledAt := time.Now()
	if err := svc.Cancel(ctxb, principal(e.user, auth.RoleUser), service.ClientInfo{IP: "203.0.113.1"}, vid, ""); err != nil {
		t.Fatal(err)
	}
	alive := func() bool { return syscall.Kill(pid, 0) == nil }
	for alive() && time.Since(cancelledAt) < 10*time.Second {
		time.Sleep(50 * time.Millisecond)
	}
	if alive() {
		t.Fatal("the running process was not killed after the cancel")
	}
	t.Logf("process killed %v after the cancel", time.Since(cancelledAt).Round(time.Millisecond))
	time.Sleep(500 * time.Millisecond) // let the worker finish handling the abort

	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "FAILED" ||
		e.str(`SELECT status::text FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != "CANCELLED" {
		t.Fatal("the worker overwrote the cancel")
	}
	if e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid`, vid) != 0 {
		t.Fatal("a cancel must not notify the creator of a failure")
	}
	if e.num(`SELECT count(*) FROM video_metadata WHERE video_id = $1::uuid`, vid) != 0 {
		t.Fatal("results of a cancelled job were stored")
	}
}

// The failure code the worker writes is what the status endpoint reads: the whole chain of
// "worker fails a job" -> "creator sees a specific reason", against the real schema.
func TestFailureCodeReachesTheStatusQuery(t *testing.T) {
	e := newPGEnv(t)
	dsn := os.Getenv("TEST_WORKER_APP_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_WORKER_APP_DATABASE_URL not set")
	}
	app, err := pgxpool.New(ctxb, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	up := repository.NewUpload(app)
	svc := processingSvc(t, e)

	// a permanent worker failure: the code is stored, the message with paths/ffmpeg text never is
	vid := e.video(3)
	s := e.store("w")
	j := claimFor(t, s, vid)
	je := permanent("too_long", "That video is longer than the maximum of 1 hour(s)")
	je.Err = errors.New("ffmpeg said /work/secret/path.mp4: invalid data")
	if _, err := s.Fail(ctxb, j, je); err != nil {
		t.Fatal(err)
	}
	if stored := e.str(`SELECT error_message FROM video_processing_jobs WHERE video_id = $1::uuid`, vid); strings.Contains(stored, "ffmpeg") || strings.Contains(stored, "/work") {
		t.Fatalf("internal detail reached the database: %q", stored)
	}
	pr, err := up.Progress(ctxb, vid)
	if err != nil || pr.FailCode != "too_long" {
		t.Fatalf("status query: %+v %v", pr, err)
	}

	// a cancellation reports its own code
	v2 := e.video(3)
	if err := svc.Cancel(ctxb, principal(e.user, auth.RoleUser), service.ClientInfo{}, v2, ""); err != nil {
		t.Fatal(err)
	}
	if pr, err := up.Progress(ctxb, v2); err != nil || pr.FailCode != "cancelled" {
		t.Fatalf("cancelled: %+v %v", pr, err)
	}

	// while something is running there is no failure to explain
	v3 := e.video(3)
	if pr, err := up.Progress(ctxb, v3); err != nil || pr.FailCode != "" || pr.Active == nil {
		t.Fatalf("active: %+v %v", pr, err)
	}
}
