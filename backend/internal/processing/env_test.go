package processing_test

// Integration tests for the job lifecycle. They need the same database as the handler tests:
//
//	TEST_ADMIN_DATABASE_URL  superuser DSN of a database whose name ends in _test (fixtures, assertions, wipe)
//
// The Store under test connects through a pool that runs `SET ROLE redxtube_worker`, so every query is also checked
// against the real GRANTs of the worker role. Without the variable the tests are skipped.

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redxtube/api/internal/processing"
)

const (
	creatorID       = "00000000-0000-0000-0000-0000000000a1"
	placeholderHash = "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5" // never matches a password
)

type tenv struct {
	t     *testing.T
	admin *pgxpool.Pool
	store *processing.Store
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setup(t *testing.T) *tenv {
	t.Helper()
	dsn := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		t.Fatalf("refusing to run: database name must end in _test (got %q)", u.Path)
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	// Same advisory lock as internal/handler: packages share this database and run in parallel.
	lock, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, `SELECT pg_advisory_lock(746100)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lock.Exec(context.Background(), `SELECT pg_advisory_unlock(746100)`)
		lock.Release()
	})

	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatalf("the admin DSN must be a superuser: %v", err)
	}
	var tables string
	if err := conn.QueryRow(ctx, `SELECT string_agg(format('%I', c.relname), ', ') FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relispartition AND c.relname NOT IN ('categories') AND c.relname NOT LIKE 'schema_migrations%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `TRUNCATE `+tables+` RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	_, _ = conn.Exec(ctx, `RESET session_replication_role`)
	conn.Release()

	if _, err := admin.Exec(ctx, `INSERT INTO users (id, username, email, password_hash, display_name, status, is_creator, is_verified)
VALUES ($1::uuid, 'proc_creator', 'proc@test.local', $2, 'Proc Studio', 'ACTIVE', true, true)`, creatorID, placeholderHash); err != nil {
		t.Fatal(err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 24
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, `SET ROLE redxtube_worker`)
		return err
	}
	wp, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wp.Close)
	return &tenv{t: t, admin: admin, store: processing.NewStore(wp)}
}

func (e *tenv) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.admin.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

// video inserts a video with the given pipeline status and returns its id.
func (e *tenv) video(status string) string {
	e.t.Helper()
	var id string
	if err := e.admin.QueryRow(context.Background(), `INSERT INTO videos (creator_id, title, status) VALUES ($1::uuid, 'Test video', $2::video_status) RETURNING id::text`,
		creatorID, status).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// job queues a job of the given type and attempt budget for a video and returns its id.
func (e *tenv) job(videoID, typ string, maxAttempts int) string {
	e.t.Helper()
	var id string
	if err := e.admin.QueryRow(context.Background(), `INSERT INTO video_processing_jobs (video_id, job_type, max_attempts) VALUES ($1::uuid, $2::job_type, $3) RETURNING id::text`,
		videoID, typ, maxAttempts).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// queued is a PROCESSING video with one queued METADATA job (3 attempts): the state Phase 5 leaves behind.
func (e *tenv) queued() (videoID, jobID string) {
	e.t.Helper()
	videoID = e.video("PROCESSING")
	return videoID, e.job(videoID, "METADATA", 3)
}

type row struct {
	Status, ErrorCode, WorkerID, Stage string
	Attempts, Progress                 int
	Cancel, HasCompleted               bool
	ScheduledInFuture                  bool
}

func (e *tenv) row(jobID string) row {
	e.t.Helper()
	var r row
	var ec, w, st *string
	err := e.admin.QueryRow(context.Background(), `SELECT status::text, error_code, worker_id, stage, attempts, progress,
  cancel_requested_at IS NOT NULL, completed_at IS NOT NULL, scheduled_at > now() + interval '5 seconds'
FROM video_processing_jobs WHERE id = $1::uuid`, jobID).Scan(&r.Status, &ec, &w, &st, &r.Attempts, &r.Progress, &r.Cancel, &r.HasCompleted, &r.ScheduledInFuture)
	if err != nil {
		e.t.Fatal(err)
	}
	if ec != nil {
		r.ErrorCode = *ec
	}
	if w != nil {
		r.WorkerID = *w
	}
	if st != nil {
		r.Stage = *st
	}
	return r
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", d, what)
}
