package handler_test

// Test harness for the auth integration tests: real PostgreSQL + Redis, a
// fake mailer that records outgoing mail, a cookie-jar client and log capture.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/config"
	"github.com/redxtube/api/internal/handler"
	appmail "github.com/redxtube/api/internal/mail"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/ratelimit"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/server"
	"github.com/redxtube/api/internal/service"
)

const (
	appOrigin  = "https://redxtube.test"
	strongPass = "Sup3r-Secret-Pass"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type outbox struct {
	mu   sync.Mutex
	msgs []appmail.Message
}

func (o *outbox) Send(_ context.Context, m appmail.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.msgs = append(o.msgs, m)
	return nil
}

func (o *outbox) to(email string) []appmail.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []appmail.Message
	for _, m := range o.msgs {
		if m.To == email {
			out = append(out, m)
		}
	}
	return out
}

func (o *outbox) count() int { o.mu.Lock(); defer o.mu.Unlock(); return len(o.msgs) }

var (
	codeRe  = regexp.MustCompile(`\b(\d{6})\b`)
	tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_.-]+)`)
)

// code returns the newest 6-digit code mailed to email ("" if none).
func (o *outbox) code(email string) string {
	ms := o.to(email)
	for i := len(ms) - 1; i >= 0; i-- {
		if strings.Contains(ms[i].Subject, "verification code") {
			return codeRe.FindString(ms[i].Text)
		}
	}
	return ""
}

func (o *outbox) resetToken(email string) string {
	ms := o.to(email)
	for i := len(ms) - 1; i >= 0; i-- {
		if m := tokenRe.FindStringSubmatch(ms[i].Text); m != nil {
			return m[1]
		}
	}
	return ""
}

func (o *outbox) subjects(email string) []string {
	var s []string
	for _, m := range o.to(email) {
		s = append(s, m.Subject)
	}
	return s
}

type authEnv struct {
	*env    // catalogue fixtures (creator, video) share the same database
	t       *testing.T
	pool    *pgxpool.Pool
	h       http.Handler
	out     *outbox
	logs    *syncBuf
	svc     *service.Auth
	cookies middleware.Cookies
	hasher  *auth.Hasher
	ipSeq   int
	run     string
}

func lightHasher() *auth.Hasher { return auth.NewHasherWithParams(8*1024, 1, 1, 8) }

func setupAuth(t *testing.T) *authEnv { return setupAuthWith(t, nil) }

// setupAuthWith lets a test replace the rate limiter (e.g. a broken Redis).
func setupAuthWith(t *testing.T, limiter service.Limiter) *authEnv {
	t.Helper()
	base := setup(t) // skips unless TEST_DATABASE_URL is set; wipes catalogue tables
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.Close() })

	run := fmt.Sprintf("%d", time.Now().UnixNano())
	if limiter == nil {
		limiter = ratelimit.New(rdb, "test:"+run+":")
	}
	logs := &syncBuf{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	out := &outbox{}
	hasher := lightHasher()
	urls := media.New("https://cdn.test")
	svc := service.NewAuth(repository.NewAuth(base.app), hasher, limiter, out, []byte(strings.Repeat("s3cr3t", 8)), urls, appOrigin, log)
	cookies := middleware.Cookies{Session: "xh_session", Age: "xh_age", AgeMark: "xh_age_ok", Domain: ".redxtube.test", Secure: true}
	socialRepo := repository.NewSocial(base.app)
	catalogSvc := service.NewCatalog(repository.NewCatalog(base.app), urls).WithViewerData(socialRepo)
	socialSvc := service.NewSocial(socialRepo, catalogSvc, limiter, auth.DeriveKeys([]byte(strings.Repeat("s3cr3t", 8))).IP, log)
	h := server.New(server.Deps{
		Cfg:     config.Config{Env: "development", MaxBodyBytes: 1 << 20, CORSOrigins: []string{appOrigin}},
		Log:     log,
		Auth:    &handler.Auth{Svc: svc, Cookies: cookies},
		Authn:   middleware.Authenticate(svc, cookies),
		Catalog: &handler.Catalog{Svc: catalogSvc},
		Social:  &handler.Social{Svc: socialSvc},
	})
	return &authEnv{env: base, t: t, pool: base.pool, h: h, out: out, logs: logs, svc: svc, cookies: cookies, hasher: hasher, run: run}
}

func (e *authEnv) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec: %v\n%s", err, sql)
	}
}

func (e *authEnv) scan(dst []any, sql string, args ...any) {
	e.t.Helper()
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(dst...); err != nil {
		e.t.Fatalf("scan: %v\n%s", err, sql)
	}
}

func (e *authEnv) countRows(sql string, args ...any) int {
	e.t.Helper()
	var n int
	e.scan([]any{&n}, sql, args...)
	return n
}

// ---------- client ----------

type client struct {
	e       *authEnv
	jar     map[string]*http.Cookie
	ip      string
	origin  string
	h       http.Handler // optional override (used by the role-guard test)
	headers map[string]string
}

func (e *authEnv) client() *client {
	e.ipSeq++
	return &client{e: e, jar: map[string]*http.Cookie{}, ip: fmt.Sprintf("10.%d.%d.%d", 20+e.ipSeq/60000, (e.ipSeq/250)%250, e.ipSeq%250+1), origin: appOrigin, headers: map[string]string{"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) RedxtubeTest/1.0"}}
}

// sameIP makes another client that shares this client's IP (and nothing else).
func (c *client) sameIP() *client {
	n := c.e.client()
	n.ip = c.ip
	return n
}

type resp struct {
	code int
	hdr  http.Header
	body []byte
	m    map[string]any
	cks  []*http.Cookie
}

func (r *resp) str(key string) string { s, _ := r.m[key].(string); return s }

func (r *resp) cookie(name string) *http.Cookie {
	for _, c := range r.cks {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (c *client) do(method, path string, body any) *resp {
	c.e.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = c.ip + ":4242"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.origin != "" && method != "GET" {
		req.Header.Set("Origin", c.origin)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for _, ck := range c.jar {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	h := c.e.h
	if c.h != nil {
		h = c.h
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	out := &resp{code: rr.Code, hdr: rr.Header(), body: rr.Body.Bytes()}
	_ = json.Unmarshal(out.body, &out.m)
	out.cks = (&http.Response{Header: rr.Header()}).Cookies()
	for _, ck := range out.cks {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.jar, ck.Name)
		} else {
			c.jar[ck.Name] = ck
		}
	}
	return out
}

func (c *client) get(path string) *resp             { c.e.t.Helper(); return c.do("GET", path, nil) }
func (c *client) post(path string, body any) *resp  { c.e.t.Helper(); return c.do("POST", path, body) }
func (c *client) patch(path string, body any) *resp { c.e.t.Helper(); return c.do("PATCH", path, body) }
func (c *client) session() string {
	if ck := c.jar["xh_session"]; ck != nil {
		return ck.Value
	}
	return ""
}

// ---------- scenario helpers ----------

func (c *client) passAgeGate() {
	c.e.t.Helper()
	if r := c.post("/api/v1/age/verify", map[string]any{"confirmed": true}); r.code != 204 {
		c.e.t.Fatalf("age verify: %d %s", r.code, r.body)
	}
}

func (c *client) register(email, username, password string) *resp {
	c.e.t.Helper()
	return c.post("/api/v1/auth/register", map[string]any{"email": email, "username": username, "password": password})
}

func (c *client) login(identifier, password string) *resp {
	c.e.t.Helper()
	return c.post("/api/v1/auth/login", map[string]any{"identifier": identifier, "password": password})
}

func uniq(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%1_000_000_000)
}

// newUser registers (unverified) and returns the logged-in client plus credentials.
func (e *authEnv) newUser() (c *client, email, username string) {
	e.t.Helper()
	c = e.client()
	c.passAgeGate()
	username = uniq("user")
	email = username + "@example.com"
	if r := c.register(email, username, strongPass); r.code != 201 {
		e.t.Fatalf("register: %d %s", r.code, r.body)
	}
	return c, email, username
}

// verifiedUser registers and completes email verification.
func (e *authEnv) verifiedUser() (c *client, email, username string) {
	e.t.Helper()
	c, email, username = e.newUser()
	code := e.out.code(email)
	if code == "" {
		e.t.Fatal("no verification email was sent")
	}
	if r := c.post("/api/v1/auth/verify", map[string]any{"code": code}); r.code != 200 {
		e.t.Fatalf("verify: %d %s", r.code, r.body)
	}
	return c, email, username
}

func (e *authEnv) userID(email string) string {
	e.t.Helper()
	var id string
	e.scan([]any{&id}, `SELECT id::text FROM users WHERE email = $1`, email)
	return id
}

func sha(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func (c *client) mustCode(r *resp, want int) *resp {
	c.e.t.Helper()
	if r.code != want {
		c.e.t.Fatalf("status %d, want %d: %s", r.code, want, r.body)
	}
	return r
}

func (r *resp) wantErr(t *testing.T, status int, code string) {
	t.Helper()
	if r.code != status || r.str("code") != code {
		t.Fatalf("got %d %s, want %d %s", r.code, r.body, status, code)
	}
}

var _ = url.QueryEscape

func (c *client) put(path string, body any) *resp { c.e.t.Helper(); return c.do("PUT", path, body) }
func (c *client) del(path string) *resp           { c.e.t.Helper(); return c.do("DELETE", path, nil) }

// viewer is an anonymous visitor who passed the age gate.
func (e *authEnv) viewer() *client {
	c := e.client()
	c.passAgeGate()
	return c
}

// vo is the short form used by social tests; see vspec for the semantics.
type vo struct {
	vis, status, kind, mode, published string
	dur, views                         int
	deleted                            bool
}

// addVideo inserts a published video for a real account and returns its id.
func (e *authEnv) addVideo(creatorID, title string, o vo) string {
	e.t.Helper()
	return e.insertVideo(vspec{creator: creatorID, title: title, kind: o.kind, status: o.status, visibility: o.vis, mode: o.mode, published: o.published, dur: o.dur, views: o.views, deleted: o.deleted})
}

// channel makes a verified account that owns videos; returns its client, id and name.
func (e *authEnv) channel() (c *client, id string) {
	e.t.Helper()
	c, email, _ := e.verifiedUser()
	return c, e.userID(email)
}

func (r *resp) items() []map[string]any {
	var out []map[string]any
	if a, ok := r.m["items"].([]any); ok {
		for _, x := range a {
			out = append(out, x.(map[string]any))
		}
	}
	return out
}

func (r *resp) itemIDs() []string {
	var out []string
	for _, it := range r.items() {
		if id, ok := it["id"].(string); ok {
			out = append(out, id)
		} else if v, ok := it["video"].(map[string]any); ok {
			out = append(out, v["id"].(string))
		}
	}
	return out
}

func (r *resp) next() string { s, _ := r.m["nextCursor"].(string); return s }

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// walkAll follows nextCursor and returns every item id.
func (c *client) walkAll(path string) []string {
	c.e.t.Helper()
	var all []string
	cursor := ""
	for i := 0; i < 100; i++ {
		p := path
		if cursor != "" {
			sep := "?"
			if strings.Contains(p, "?") {
				sep = "&"
			}
			p += sep + "cursor=" + url.QueryEscape(cursor)
		}
		r := c.get(p)
		if r.code != 200 {
			c.e.t.Fatalf("GET %s = %d %s", p, r.code, r.body)
		}
		all = append(all, r.itemIDs()...)
		if cursor = r.next(); cursor == "" {
			return all
		}
	}
	c.e.t.Fatal("pagination did not terminate")
	return nil
}

func (e *authEnv) videoCounts(id string) (likes, dislikes, comments int, views int64) {
	e.t.Helper()
	e.scan([]any{&likes, &dislikes, &comments, &views}, `SELECT like_count, dislike_count, comment_count, view_count FROM video_stats WHERE video_id = $1::uuid`, id)
	return
}

// notificationCount counts notifications by the frontend-facing name used in the tests
// (NEW_COMMENT is the schema's COMMENT type).
func (e *authEnv) notificationCount(userID, typ string) int {
	e.t.Helper()
	if typ == "NEW_COMMENT" {
		typ = "COMMENT"
	}
	return e.countRows(`SELECT count(*) FROM notifications WHERE user_id = $1::uuid AND type = $2::notification_type`, userID, typ)
}

func sleepMs(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }

func jsonInto(b []byte, dst any) error { return json.Unmarshal(b, dst) }

// bulkVideos inserts n published videos for a creator in one statement (no thumbnails),
// newest first, and returns their ids.
func (e *authEnv) bulkVideos(creatorID string, n int) []string {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `INSERT INTO videos (id, creator_id, title, video_type, status, visibility, moderation_status, comments_mode, category_id, duration_seconds, published_at, hls_master_key)
SELECT gen_random_uuid(), $1::uuid, 'bulk ' || g, 'LONG', 'PUBLISHED', 'PUBLIC', 'OK', 'ON', (SELECT id FROM categories WHERE slug = 'music'), 300, now() - (g * interval '1 minute'), 'k/' || g
FROM generate_series(1, $2::int) g RETURNING id::text`, creatorID, n)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			e.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}
