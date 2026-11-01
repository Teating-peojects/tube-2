package handler_test

// Shared fixtures for the integration tests (catalogue, auth, social). They run
// against a real PostgreSQL database built from db/migrations:
//
//	createdb redxtube_test, then bootstrap roles and apply migrations (see README)
//	TEST_ADMIN_DATABASE_URL=postgres://<superuser>@host/redxtube_test   fixtures + wipe (needs superuser)
//	TEST_DATABASE_URL=postgres://redxtube_app:...@host/redxtube_test     what the API itself uses
//	TEST_REDIS_URL=redis://localhost:6379/0                            auth/social tests
//
// The API under test connects as the least-privilege redxtube_app role, so every
// query is also checked against the real GRANTs. The tests WIPE the data, so
// both URLs must point at a database whose name ends in "_test".
// Without TEST_DATABASE_URL the tests are skipped.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/config"
	"github.com/redxtube/api/internal/handler"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/server"
	"github.com/redxtube/api/internal/service"
)

type env struct {
	t    *testing.T
	pool *pgxpool.Pool // admin (fixtures, assertions, wipe)
	app  *pgxpool.Pool // the API's own least-privilege connection
	h    http.Handler
}

func mustTestDB(t *testing.T, dsn string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		t.Fatalf("refusing to run: database name must end in _test (got %q)", u.Path)
	}
}

func setup(t *testing.T) *env {
	t.Helper()
	appDSN := os.Getenv("TEST_DATABASE_URL")
	if appDSN == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	adminDSN := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if adminDSN == "" {
		t.Skip("TEST_ADMIN_DATABASE_URL not set (superuser DSN used for fixtures and wiping)")
	}
	mustTestDB(t, appDSN)
	mustTestDB(t, adminDSN)
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	app, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)

	// Wipe everything except reference data. audit_logs and moderation_actions are
	// append-only (a trigger blocks TRUNCATE), so triggers are bypassed for the wipe.
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
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

	svc := service.NewCatalog(repository.NewCatalog(app), media.New("https://cdn.test"))
	h := server.New(server.Deps{
		Cfg:     config.Config{Env: "development", MaxBodyBytes: 1 << 20},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Catalog: &handler.Catalog{Svc: svc},
	})
	return &env{t: t, pool: admin, app: app, h: h}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec failed: %v\n%s", err, sql)
	}
}

func cid(n int) string { return fmt.Sprintf("c0000000-0000-7000-8000-%012d", n) }
func vid(n int) string { return fmt.Sprintf("d0000000-0000-7000-8000-%012d", n) }

const placeholderHash = "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5" // never matches a password

// creator inserts a user (fixtures can't log in). status: ACTIVE|SUSPENDED|BANNED; deleted makes it a DELETED account.
// The profile and settings rows are created by database triggers.
func (e *env) creator(n int, name, status string, deleted bool, subs int) {
	if deleted {
		status = "DELETED"
	}
	if len([]rune(name)) < 2 {
		name += " Studio" // display_name must be 2-40 characters
	}
	e.exec(`INSERT INTO users (id, username, email, password_hash, display_name, status, is_creator, is_verified, deleted_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6::user_status, true, true, CASE WHEN $6 = 'DELETED' THEN now() END)`,
		cid(n), fmt.Sprintf("creator_%d", n), fmt.Sprintf("c%d@test.local", n), placeholderHash, name, status)
	e.exec(`UPDATE user_profiles SET subscriber_count = $2 WHERE user_id = $1::uuid`, cid(n), subs)
}

// vspec describes a video fixture. status: "" or READY = published; PROCESSING = a draft;
// REMOVED = published but removed by moderation. kind: VIDEO (default, LONG) or SHORT.
type vspec struct {
	id, creator, title, kind, status, visibility, mode, category, published string
	dur, views                                                              int
	deleted                                                                 bool
}

func (e *env) insertVideo(v vspec) string {
	e.t.Helper()
	if v.id == "" {
		v.id = auth.NewUUIDv7()
	}
	vtype := "LONG"
	if v.kind == "SHORT" {
		vtype = "SHORT"
	}
	if v.dur == 0 {
		v.dur = 300
		if vtype == "SHORT" {
			v.dur = 30
		}
	}
	if v.visibility == "" {
		v.visibility = "PUBLIC"
	}
	if v.mode == "" {
		v.mode = "ON"
	}
	if v.mode == "OPEN" {
		v.mode = "ON"
	}
	if v.mode == "HELD" {
		v.mode = "REVIEW"
	}
	if v.category == "" {
		v.category = "music"
	}
	status, moderation, pub, hls := "PUBLISHED", "OK", "now() - interval '1 day'", "'videos/'||$1::text||'/hls/master.m3u8'"
	if v.published != "" {
		pub = v.published
	}
	switch v.status {
	case "PROCESSING":
		status, pub, hls = "PROCESSING", "NULL", "NULL"
	case "REMOVED":
		moderation = "REMOVED"
	}
	e.exec(fmt.Sprintf(`INSERT INTO videos (id, creator_id, title, video_type, status, visibility, moderation_status, comments_mode, category_id, duration_seconds, published_at, hls_master_key, deleted_at)
VALUES ($1::uuid, $2::uuid, $3, $4::video_type, $5::video_status, $6::video_visibility, $7::moderation_status, $8::comment_mode,
  (SELECT id FROM categories WHERE slug = $9), $10, %s, %s, CASE WHEN $11 THEN now() END)`, pub, hls),
		v.id, v.creator, v.title, vtype, status, v.visibility, moderation, v.mode, v.category, v.dur, v.deleted)
	e.exec(`UPDATE video_stats SET view_count = $2 WHERE video_id = $1::uuid`, v.id, v.views)
	e.exec(`INSERT INTO video_thumbnails (video_id, storage_key, width, height, is_primary, status) VALUES ($1::uuid, $2, 640, 360, true, 'READY')`, v.id, "t/"+v.id+".jpg")
	e.exec(`INSERT INTO video_metadata (video_id, width, height) VALUES ($1::uuid, 1280, 720)`, v.id)
	e.exec(`SELECT refresh_video_search($1::uuid)`, v.id)
	return v.id
}

type vopt struct {
	n                                                    int
	creator                                              int
	title, kind, status, visibility, published, category string
	views                                                int64
	dur                                                  int
	deleted                                              bool
}

// video inserts a fixture with a fixed id (vid(n)) owned by creator(n).
func (e *env) video(o vopt) {
	e.t.Helper()
	e.insertVideo(vspec{id: vid(o.n), creator: cid(o.creator), title: o.title, kind: o.kind, status: o.status, visibility: o.visibility,
		category: o.category, published: o.published, dur: o.dur, views: int(o.views), deleted: o.deleted})
}

func contextBG() context.Context { return context.Background() }
