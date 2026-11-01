package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// ---------- age gate ----------

func TestAgeGateIsEnforcedByTheServer(t *testing.T) {
	e := setupAuth(t)
	e.creator(1, "Gate Studio", "ACTIVE", false, 1)
	e.video(vopt{n: 1, creator: 1, title: "gated", published: recent, views: 1})
	c := e.client()

	// content is closed without a valid signed cookie
	for _, p := range []string{"/api/v1/videos", "/api/v1/videos/" + vid(1), "/api/v1/search?q=gated", "/api/v1/trending", "/api/v1/channels/" + cid(1), "/api/v1/shorts", "/api/v1/live", "/api/v1/search/suggestions?q=ga", "/api/v1/channels/popular"} {
		c.get(p).wantErr(t, 403, "age_verification_required")
	}
	// ids-only / static endpoints stay open
	c.mustCode(c.get("/api/v1/reports/reasons"), 200)
	c.mustCode(c.get("/api/v1/sitemap/videos"), 200)

	// the frontend's old client-side cookie, and forged signed cookies, are worthless
	c.jar["xh_age_ok"] = &http.Cookie{Name: "xh_age_ok", Value: "1"}
	c.get("/api/v1/videos").wantErr(t, 403, "age_verification_required")
	c.jar["xh_age"] = &http.Cookie{Name: "xh_age", Value: "1"}
	c.get("/api/v1/videos").wantErr(t, 403, "age_verification_required")
	c.jar["xh_age"] = &http.Cookie{Name: "xh_age", Value: "v1.0a1b2c3d-0000-4000-8000-000000000001.99999999999.AAAAAAAA"}
	c.get("/api/v1/videos").wantErr(t, 403, "age_verification_required")
	delete(c.jar, "xh_age")
	delete(c.jar, "xh_age_ok")

	if r := c.get("/api/v1/age/status"); r.code != 200 || r.m["verified"] != false {
		t.Fatalf("status before: %s", r.body)
	}
	// must explicitly confirm
	c.post("/api/v1/age/verify", map[string]any{"confirmed": false}).wantErr(t, 422, "validation_failed")
	c.post("/api/v1/age/verify", map[string]any{}).wantErr(t, 422, "validation_failed")

	r := c.mustCode(c.post("/api/v1/age/verify", map[string]any{"confirmed": true}), 204)
	signed, marker := r.cookie("xh_age"), r.cookie("xh_age_ok")
	if signed == nil || !signed.HttpOnly || !signed.Secure || signed.SameSite != http.SameSiteLaxMode || signed.Domain != "redxtube.test" || signed.Path != "/" {
		t.Fatalf("signed age cookie flags wrong: %+v", signed)
	}
	if marker == nil || marker.HttpOnly || marker.Value != "1" || !marker.Secure {
		t.Fatalf("marker cookie wrong: %+v", marker)
	}
	if !strings.HasPrefix(signed.Value, "v1.") || signed.MaxAge < 29*86400 {
		t.Fatalf("cookie value/expiry: %q %d", signed.Value, signed.MaxAge)
	}

	c.mustCode(c.get("/api/v1/videos"), 200)
	c.mustCode(c.get("/api/v1/videos/"+vid(1)), 200)
	if r := c.get("/api/v1/age/status"); r.m["verified"] != true {
		t.Fatalf("status after: %s", r.body)
	}
	// anonymous visitors are not written to the database (age_verifications is per account;
	// the signed cookie is the proof). Re-verifying just extends the cookie.
	r2 := c.mustCode(c.post("/api/v1/age/verify", map[string]any{"confirmed": true}), 204)
	if r2.cookie("xh_age") == nil || r2.cookie("xh_age_ok") == nil {
		t.Fatal("re-verify must re-issue the cookie")
	}
	if n := e.countRows(`SELECT count(*) FROM age_verifications`); n != 0 {
		t.Fatalf("no database rows are created for anonymous visitors, got %d", n)
	}
}

func TestAgeVerifyIsRateLimited(t *testing.T) {
	e := setupAuth(t)
	c := e.client()
	for i := 0; i < 30; i++ {
		c.mustCode(c.post("/api/v1/age/verify", map[string]any{"confirmed": true}), 204)
	}
	r := c.post("/api/v1/age/verify", map[string]any{"confirmed": true})
	r.wantErr(t, 429, "rate_limited")
	if r.hdr.Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
}

// ---------- registration & email verification ----------

func TestRegistrationRequiresAgeGateAndValidInput(t *testing.T) {
	e := setupAuth(t)
	c := e.client()
	c.register("a@example.com", "alice_x", strongPass).wantErr(t, 403, "age_verification_required")
	c.passAgeGate()

	r := c.register("not-an-email", "a b", "short")
	r.wantErr(t, 422, "validation_failed")
	fields := r.m["fields"].(map[string]any)
	for _, f := range []string{"email", "username", "password"} {
		if fields[f] == nil {
			t.Errorf("missing field error for %s: %v", f, fields)
		}
	}
	for name, in := range map[string][3]string{
		"reserved username": {"r1@example.com", "Admin", strongPass},
		"username too long": {"r2@example.com", strings.Repeat("a", 25), strongPass},
		"email with name":   {"Bob <bob@example.com>", "bob_ok", strongPass},
		"email no dot":      {"bob@localhost", "bob_ok", strongPass},
		"common password":   {"r3@example.com", "bob_ok", "Password123"},
		"password=username": {"r4@example.com", "bobsmith12", "bobsmith12"},
	} {
		if r := c.register(in[0], in[1], in[2]); r.code != 422 {
			t.Errorf("%s: got %d %s", name, r.code, r.body)
		}
	}
	if r := c.post("/api/v1/auth/register", map[string]any{"email": "x@example.com", "username": "xxxxx", "password": strongPass, "isAdmin": true}); r.code != 400 || r.str("code") != "invalid_json" {
		t.Fatalf("unknown fields must be rejected: %d %s", r.code, r.body)
	}
	if n := e.countRows(`SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("no user may exist after rejected registrations, got %d", n)
	}
}

func TestRegisterThenVerifyEmailFlow(t *testing.T) {
	e := setupAuth(t)
	c := e.client()
	c.passAgeGate()
	username := uniq("Maya")
	email := strings.ToLower(username) + "@Example.com"

	r := c.register(email, username, strongPass)
	r.mustBe(t, 201)
	if r.m["emailVerified"] != false || r.m["isCreator"] != false || r.str("email") != strings.ToLower(email) || r.str("username") != username || r.str("displayName") != username {
		t.Fatalf("user body: %s", r.body)
	}
	if r.m["password"] != nil || r.m["passwordHash"] != nil {
		t.Fatal("password material must never be returned")
	}
	sc := r.cookie("xh_session")
	if sc == nil || !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode || sc.Domain != "redxtube.test" || sc.MaxAge < 29*86400 || len(sc.Value) != 43 {
		t.Fatalf("session cookie flags wrong: %+v", sc)
	}

	// storage: only hashes. No plaintext password, session token or code in the database.
	var hash string
	var tokenHash []byte
	id := e.userID(strings.ToLower(email))
	e.scan([]any{&hash}, `SELECT password_hash FROM users WHERE id = $1::uuid`, id)
	e.scan([]any{&tokenHash}, `SELECT session_token_hash FROM sessions WHERE user_id = $1::uuid`, id)
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, strongPass) {
		t.Fatalf("password not stored as argon2id: %s", hash)
	}
	if string(tokenHash) != string(sha(sc.Value)) || strings.Contains(string(tokenHash), sc.Value) {
		t.Fatal("session token must be stored only as its SHA-256")
	}
	code := e.out.code(strings.ToLower(email))
	if len(code) != 6 {
		t.Fatalf("no 6-digit code mailed: %v", e.out.subjects(strings.ToLower(email)))
	}
	var otp []byte
	e.scan([]any{&otp}, `SELECT token_hash FROM verification_tokens WHERE user_id = $1::uuid AND purpose = 'EMAIL_VERIFY'`, id)
	if len(otp) != 32 || strings.Contains(string(otp), code) {
		t.Fatal("OTP must be stored as a keyed digest, never the code")
	}
	if n := e.countRows(`SELECT count(*) FROM age_verifications WHERE user_id = $1::uuid AND method = 'SELF_DECLARATION' AND status = 'VERIFIED'`, id); n != 1 {
		t.Fatalf("account must keep its own age record, got %d", n)
	}

	// duplicates (case-insensitive) conflict without saying which field
	d := e.client()
	d.passAgeGate()
	d.register(strings.ToUpper(email), uniq("other"), strongPass).wantErr(t, 409, "conflict")
	d.register(uniq("o")+"@example.com", strings.ToUpper(username), strongPass).wantErr(t, 409, "conflict")

	// unverified session works for /me
	me := c.get("/api/v1/auth/me")
	me.mustBe(t, 200)
	if me.m["emailVerified"] != false {
		t.Fatalf("me: %s", me.body)
	}

	// wrong code, malformed code
	c.post("/api/v1/auth/verify", map[string]any{"code": "12ab56"}).wantErr(t, 400, "invalid_code")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	c.post("/api/v1/auth/verify", map[string]any{"code": wrong}).wantErr(t, 400, "invalid_code")

	// correct code verifies
	v := c.post("/api/v1/auth/verify", map[string]any{"code": code})
	v.mustBe(t, 200)
	if v.m["emailVerified"] != true {
		t.Fatalf("verify body: %s", v.body)
	}
	if c.get("/api/v1/auth/me").m["emailVerified"] != true {
		t.Fatal("me must reflect verification")
	}
	// the code is single-use and verification is idempotent
	c.post("/api/v1/auth/verify", map[string]any{"code": code}).mustBe(t, 200)
	if n := e.countRows(`SELECT count(*) FROM verification_tokens WHERE user_id = $1::uuid AND consumed_at IS NULL`, id); n != 0 {
		t.Fatalf("code must be consumed, %d still live", n)
	}
}

func (r *resp) mustBe(t *testing.T, status int) {
	t.Helper()
	if r.code != status {
		t.Fatalf("got %d, want %d: %s", r.code, status, r.body)
	}
}

func TestEmailCodeLockoutAndResend(t *testing.T) {
	e := setupAuth(t)
	c, email, _ := e.newUser()
	first := e.out.code(email)
	wrong := "000000"
	if first == wrong {
		wrong = "999999"
	}
	for i := 1; i <= 4; i++ {
		c.post("/api/v1/auth/verify", map[string]any{"code": wrong}).wantErr(t, 400, "invalid_code")
	}
	// the 5th wrong guess locks the code
	r := c.post("/api/v1/auth/verify", map[string]any{"code": wrong})
	r.wantErr(t, 429, "rate_limited")
	if r.hdr.Get("Retry-After") == "" {
		t.Fatal("lockout must send Retry-After")
	}
	// even the right code is refused once locked: guessing cannot be finished by luck
	c.post("/api/v1/auth/verify", map[string]any{"code": first}).wantErr(t, 429, "rate_limited")

	// resending issues a new code, resets attempts and kills the old one
	c.mustCode(c.post("/api/v1/auth/verify/resend", nil), 204)
	second := e.out.code(email)
	if second == "" || len(e.out.to(email)) != 2 {
		t.Fatalf("resend must send exactly one more email, got %d", len(e.out.to(email)))
	}
	if second != first {
		c.post("/api/v1/auth/verify", map[string]any{"code": first}).wantErr(t, 400, "invalid_code")
	}
	c.mustCode(c.post("/api/v1/auth/verify", map[string]any{"code": second}), 200)

	// resend is a no-op for verified users
	before := len(e.out.to(email))
	c.mustCode(c.post("/api/v1/auth/verify/resend", nil), 204)
	if len(e.out.to(email)) != before {
		t.Fatal("verified users must not be mailed again")
	}
}

func TestResendCooldownAndHourlyCap(t *testing.T) {
	e := setupAuth(t)
	c, email, _ := e.newUser()
	c.mustCode(c.post("/api/v1/auth/verify/resend", nil), 204)
	r := c.post("/api/v1/auth/verify/resend", nil)
	r.wantErr(t, 429, "rate_limited")
	if r.hdr.Get("Retry-After") == "" || len(e.out.to(email)) != 2 {
		t.Fatalf("cooldown must block the second resend (mails=%d)", len(e.out.to(email)))
	}
}

func TestExpiredEmailCodeIsRejected(t *testing.T) {
	e := setupAuth(t)
	c, email, _ := e.newUser()
	code := e.out.code(email)
	e.exec(`UPDATE verification_tokens SET created_at = now() - interval '1 hour', expires_at = now() - interval '1 second' WHERE user_id = $1::uuid`, e.userID(email))
	c.post("/api/v1/auth/verify", map[string]any{"code": code}).wantErr(t, 400, "invalid_code")
}

func TestRegistrationIsRateLimitedPerIP(t *testing.T) {
	e := setupAuth(t)
	c := e.client()
	c.passAgeGate()
	var last *resp
	for i := 0; i < 11; i++ {
		last = c.register(uniq("n")+"@example.com", uniq("nu"), strongPass)
		time.Sleep(2 * time.Millisecond)
	}
	last.wantErr(t, 429, "rate_limited")
	if last.hdr.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

// ---------- login ----------

func TestLoginFlow(t *testing.T) {
	e := setupAuth(t)
	_, email, username := e.verifiedUser()

	// unknown account and wrong password are indistinguishable
	c := e.client()
	wrong := c.login(email, "Wrong-Password-1")
	unknown := c.login("nobody-"+uniq("x")+"@example.com", "Wrong-Password-1")
	wrong.wantErr(t, 401, "invalid_credentials")
	unknown.wantErr(t, 401, "invalid_credentials")
	if string(wrong.body) != string(unknown.body) {
		t.Fatalf("responses differ: %s vs %s", wrong.body, unknown.body)
	}
	if c.session() != "" {
		t.Fatal("failed login must not set a session")
	}
	for _, bad := range []map[string]any{{"identifier": "", "password": "x"}, {"identifier": email, "password": ""}, {"identifier": strings.Repeat("a", 300), "password": "x"}} {
		if r := c.post("/api/v1/auth/login", bad); r.code != 401 && r.code != 400 {
			t.Errorf("bad login body -> %d", r.code)
		}
	}

	// success by email (any case) and by username, new session token each time
	r1 := c.login(strings.ToUpper(email), strongPass)
	r1.mustBe(t, 200)
	tok1 := c.session()
	if r1.m["emailVerified"] != true || r1.str("username") != username || len(tok1) != 43 {
		t.Fatalf("login body: %s", r1.body)
	}
	c2 := e.client()
	c2.login(username, strongPass).mustBe(t, 200)
	if c2.session() == tok1 || c2.session() == "" {
		t.Fatal("every login must mint a fresh session token (no fixation)")
	}
	// a client that sends an old token still gets a brand-new one
	c.jar["xh_session"] = &http.Cookie{Name: "xh_session", Value: "attacker-chosen-token-value"}
	c.login(email, strongPass).mustBe(t, 200)
	if c.session() == "attacker-chosen-token-value" {
		t.Fatal("session fixation: server accepted a client-chosen token")
	}
	if e.countRows(`SELECT count(*) FROM audit_logs WHERE action = 'auth.login' AND actor_user_id = $1::uuid`, e.userID(email)) < 3 {
		t.Fatal("logins must be audited")
	}
}

func TestLoginBeforeVerificationStillSetsSession(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.newUser()
	c := e.client()
	r := c.login(email, strongPass)
	r.wantErr(t, 403, "verification_required")
	if r.cookie("xh_session") == nil || c.session() == "" {
		t.Fatal("the verify screen needs the session cookie even when login answers 403")
	}
	c.get("/api/v1/auth/me").mustBe(t, 200)
	c.mustCode(c.post("/api/v1/auth/verify", map[string]any{"code": e.out.code(email)}), 200)
}

func TestLoginLockoutIsPerIdentifier(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	_, other, _ := e.verifiedUser()
	c := e.client()
	for i := 0; i < 5; i++ {
		c.login(email, "Wrong-Password-1").wantErr(t, 401, "invalid_credentials")
	}
	// locked even with the correct password: brute force cannot succeed by luck
	r := c.login(email, strongPass)
	r.wantErr(t, 429, "rate_limited")
	if r.hdr.Get("Retry-After") == "" {
		t.Fatal("lockout must send Retry-After")
	}
	// a different client IP is still locked for that account...
	e.client().login(email, strongPass).wantErr(t, 429, "rate_limited")
	// ...but other accounts are unaffected
	e.client().login(other, strongPass).mustBe(t, 200)
}

func TestSuccessfulLoginClearsFailureCounter(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	c := e.client()
	for round := 0; round < 3; round++ {
		for i := 0; i < 4; i++ {
			c.login(email, "Wrong-Password-1").wantErr(t, 401, "invalid_credentials")
		}
		c.login(email, strongPass).mustBe(t, 200)
	}
}

func TestSuspendedAndBrokenAccounts(t *testing.T) {
	e := setupAuth(t)
	live, email, _ := e.verifiedUser()
	e.exec(`UPDATE users SET status = 'SUSPENDED' WHERE email = $1`, email)
	// existing sessions stop working at once
	live.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	// and the account cannot log in, but only after the password was proven (no status oracle)
	c := e.client()
	c.login(email, "Wrong-Password-1").wantErr(t, 401, "invalid_credentials")
	r := c.login(email, strongPass)
	r.wantErr(t, 403, "account_suspended")
	if c.session() != "" {
		t.Fatal("suspended accounts must not get a session")
	}
	e.exec(`UPDATE users SET status = 'BANNED' WHERE email = $1`, email)
	c.login(email, strongPass).wantErr(t, 403, "account_suspended")

	// accounts with an unusable stored hash are just "invalid credentials", never a 500
	e.creator(1, "Seeded", "ACTIVE", false, 1)
	e.exec(`UPDATE users SET password_hash = '$argon2id$garbage' WHERE id = $1::uuid`, cid(1))
	c.login("creator_1", strongPass).wantErr(t, 401, "invalid_credentials")
}

func TestLoginUpgradesWeakPasswordHash(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	weak, _ := auth.NewHasherWithParams(4*1024, 1, 1, 2).Hash(context.Background(), strongPass)
	e.exec(`UPDATE users SET password_hash = $2 WHERE email = $1`, email, weak)
	e.client().login(email, strongPass).mustBe(t, 200)
	var now string
	e.scan([]any{&now}, `SELECT password_hash FROM users WHERE email = $1`, email)
	if !strings.Contains(now, "m=8192,t=1,p=1") || now == weak {
		t.Fatalf("hash was not upgraded: %s", now)
	}
	e.client().login(email, strongPass).mustBe(t, 200) // still works after the upgrade
}

func TestLoginRestoresAgeCookieForKnownAccounts(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	fresh := e.client() // new device: no age cookie
	fresh.get("/api/v1/videos").wantErr(t, 403, "age_verification_required")
	r := fresh.login(email, strongPass)
	r.mustBe(t, 200)
	if r.cookie("xh_age") == nil || r.cookie("xh_age_ok") == nil {
		t.Fatal("an account that already proved 18+ gets its age cookie back at login")
	}
	fresh.get("/api/v1/videos").mustBe(t, 200)
}

// ---------- sessions ----------

func TestLogoutRevokesTheSession(t *testing.T) {
	e := setupAuth(t)
	c, _, _ := e.verifiedUser()
	stolen := c.session()
	c.get("/api/v1/auth/me").mustBe(t, 200)
	r := c.post("/api/v1/auth/logout", nil)
	r.mustBe(t, 204)
	if ck := r.cookie("xh_session"); ck == nil || ck.MaxAge >= 0 {
		t.Fatalf("logout must clear the cookie: %+v", ck)
	}
	c.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	// replaying the old token after logout fails: it is revoked server-side, not just cleared in the browser
	replay := e.client()
	replay.jar["xh_session"] = &http.Cookie{Name: "xh_session", Value: stolen}
	replay.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	// logout without a session is harmless
	e.client().post("/api/v1/auth/logout", nil).mustBe(t, 204)
}

func TestLogoutAllRevokesEveryDevice(t *testing.T) {
	e := setupAuth(t)
	a, email, _ := e.verifiedUser()
	b := e.client()
	b.login(email, strongPass).mustBe(t, 200)
	other := e.client()
	other.login(email, strongPass).mustBe(t, 200)
	a.post("/api/v1/auth/logout-all", nil).mustBe(t, 204)
	for name, c := range map[string]*client{"a": a, "b": b, "other": other} {
		if r := c.get("/api/v1/auth/me"); r.code != 401 {
			t.Errorf("device %s still signed in after logout-all", name)
		}
	}
	if e.countRows(`SELECT count(*) FROM audit_logs WHERE action = 'auth.logout_all' AND actor_user_id = $1::uuid`, e.userID(email)) != 1 {
		t.Fatal("logout-all must be audited")
	}
}

func TestSessionExpiryTamperingAndSlidingRefresh(t *testing.T) {
	e := setupAuth(t)
	c, email, _ := e.verifiedUser()
	id := e.userID(email)

	// garbage cookie: anonymous, and the browser cookie is cleared
	g := e.client()
	g.jar["xh_session"] = &http.Cookie{Name: "xh_session", Value: strings.Repeat("A", 43)}
	r := g.get("/api/v1/auth/me")
	r.wantErr(t, 401, "unauthenticated")
	if ck := r.cookie("xh_session"); ck == nil || ck.MaxAge >= 0 {
		t.Fatal("stale cookie should be cleared")
	}
	huge := e.client()
	huge.jar["xh_session"] = &http.Cookie{Name: "xh_session", Value: strings.Repeat("A", 5000)}
	huge.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")

	// sliding refresh: idle for 2h -> expiry moves forward and the cookie is re-issued
	e.exec(`UPDATE sessions SET last_used_at = now() - interval '2 hours', expires_at = now() + interval '1 day' WHERE user_id = $1::uuid`, id)
	r = c.get("/api/v1/auth/me")
	r.mustBe(t, 200)
	if ck := r.cookie("xh_session"); ck == nil || ck.MaxAge < 29*86400 {
		t.Fatalf("cookie must be renewed with the session: %+v", ck)
	}
	var days float64
	e.scan([]any{&days}, `SELECT extract(epoch FROM expires_at - now()) / 86400 FROM sessions WHERE user_id = $1::uuid`, id)
	if days < 29 {
		t.Fatalf("session expiry not extended: %.1f days", days)
	}
	// a recently used session is not rewritten on every request
	if r := c.get("/api/v1/auth/me"); r.cookie("xh_session") != nil {
		t.Fatal("no cookie churn for recently refreshed sessions")
	}
	// the absolute cap (created_at + 90 days) is never exceeded
	e.exec(`UPDATE sessions SET last_used_at = now() - interval '2 hours', created_at = now() - interval '88 days', expires_at = now() + interval '1 day' WHERE user_id = $1::uuid`, id)
	c.get("/api/v1/auth/me").mustBe(t, 200)
	e.scan([]any{&days}, `SELECT extract(epoch FROM expires_at - now()) / 86400 FROM sessions WHERE user_id = $1::uuid`, id)
	if days > 2.01 {
		t.Fatalf("sliding refresh crossed the absolute expiry: %.2f days", days)
	}
	// expired / past absolute expiry
	e.exec(`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE user_id = $1::uuid`, id)
	c.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	d, email2, _ := e.verifiedUser()
	e.exec(`UPDATE sessions SET created_at = now() - interval '91 days', expires_at = now() + interval '1 day' WHERE user_id = $1::uuid`, e.userID(email2))
	d.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
}

func TestProtectedEndpointsRejectAnonymousCallers(t *testing.T) {
	e := setupAuth(t)
	c := e.client()
	for _, ep := range [][2]string{
		{"GET", "/api/v1/auth/me"}, {"POST", "/api/v1/auth/verify"}, {"POST", "/api/v1/auth/verify/resend"},
		{"POST", "/api/v1/auth/change-password"}, {"POST", "/api/v1/auth/logout-all"},
		{"PATCH", "/api/v1/account"}, {"POST", "/api/v1/account/delete"},
		{"GET", "/api/v1/settings"}, {"PATCH", "/api/v1/settings"},
	} {
		r := c.do(ep[0], ep[1], map[string]any{})
		if r.code != 401 || r.str("code") != "unauthenticated" {
			t.Errorf("%s %s -> %d %s (want 401 unauthenticated)", ep[0], ep[1], r.code, r.body)
		}
	}
}

// ---------- forgot / reset / change password ----------

func TestForgotAndResetPassword(t *testing.T) {
	e := setupAuth(t)
	victim, email, username := e.verifiedUser()
	other := e.client()
	other.login(email, strongPass).mustBe(t, 200)
	c := e.client()

	// no account enumeration: identical answer, no mail for unknown/malformed addresses
	for _, addr := range []string{"ghost-" + uniq("g") + "@example.com", "not-an-email", ""} {
		c.mustCode(c.post("/api/v1/auth/forgot-password", map[string]any{"email": addr}), 204)
	}
	if n := e.out.count(); n != 1 { // just the registration code
		t.Fatalf("unexpected mail for unknown addresses: %d", n)
	}
	c.mustCode(c.post("/api/v1/auth/forgot-password", map[string]any{"email": strings.ToUpper(email)}), 204)
	token := e.out.resetToken(email)
	tokenID, secret, _ := strings.Cut(token, ".")
	if len(tokenID) != 36 || len(secret) != 43 {
		t.Fatalf("reset link must carry <token id>.<secret>: %q (mail subjects %v)", token, e.out.subjects(email))
	}
	if !strings.Contains(e.out.to(email)[1].Text, appOrigin+"/reset-password?token=") {
		t.Fatalf("link must point at the frontend: %s", e.out.to(email)[1].Text)
	}
	var stored []byte
	var rowID string
	e.scan([]any{&rowID, &stored}, `SELECT id::text, token_hash FROM verification_tokens WHERE purpose = 'PASSWORD_RESET' AND user_id = $1::uuid`, e.userID(email))
	if rowID != tokenID || len(stored) != 32 || strings.Contains(string(stored), secret) || string(stored) == string(sha(secret)) {
		t.Fatal("only a keyed HMAC bound to purpose and user may be stored, never the secret or its plain hash")
	}

	// bad inputs
	c.post("/api/v1/auth/reset-password", map[string]any{"token": "short", "password": "New-Password-123"}).wantErr(t, 400, "invalid_token")
	c.post("/api/v1/auth/reset-password", map[string]any{"token": strings.Repeat("A", 43), "password": "New-Password-123"}).wantErr(t, 400, "invalid_token")
	c.post("/api/v1/auth/reset-password", map[string]any{"token": tokenID + "." + strings.Repeat("A", 43), "password": "New-Password-123"}).wantErr(t, 400, "invalid_token")          // right row, wrong secret
	c.post("/api/v1/auth/reset-password", map[string]any{"token": "00000000-0000-7000-8000-000000000000." + secret, "password": "New-Password-123"}).wantErr(t, 400, "invalid_token") // right secret, wrong row
	c.post("/api/v1/auth/reset-password", map[string]any{"token": token, "password": "weak"}).wantErr(t, 422, "validation_failed")
	if e.countRows(`SELECT count(*) FROM verification_tokens WHERE purpose='PASSWORD_RESET' AND consumed_at IS NULL AND user_id = $1::uuid`, e.userID(email)) != 1 {
		t.Fatal("a rejected reset must not burn the token")
	}

	// success: all sessions die, old password dies, new works
	c.mustCode(c.post("/api/v1/auth/reset-password", map[string]any{"token": token, "password": "New-Password-123"}), 204)
	victim.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	other.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	e.client().login(username, strongPass).wantErr(t, 401, "invalid_credentials")
	e.client().login(username, "New-Password-123").mustBe(t, 200)
	// single use
	c.post("/api/v1/auth/reset-password", map[string]any{"token": token, "password": "Another-Pass-456"}).wantErr(t, 400, "invalid_token")
	e.client().login(username, "Another-Pass-456").wantErr(t, 401, "invalid_credentials")
	// the owner is told
	if subs := e.out.subjects(email); subs[len(subs)-1] != "Your REDXTUBE password was changed" {
		t.Fatalf("security notice missing: %v", subs)
	}
	if e.countRows(`SELECT count(*) FROM audit_logs WHERE action = 'auth.password_reset' AND actor_user_id = $1::uuid`, e.userID(email)) != 1 {
		t.Fatal("reset must be audited")
	}
}

func TestResetTokenExpiresAndNewRequestReplacesOld(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	c := e.client()
	c.mustCode(c.post("/api/v1/auth/forgot-password", map[string]any{"email": email}), 204)
	first := e.out.resetToken(email)
	c.mustCode(c.post("/api/v1/auth/forgot-password", map[string]any{"email": email}), 204)
	second := e.out.resetToken(email)
	if first == second {
		t.Fatal("tokens must be unique")
	}
	c.post("/api/v1/auth/reset-password", map[string]any{"token": first, "password": "New-Password-123"}).wantErr(t, 400, "invalid_token")
	e.exec(`UPDATE verification_tokens SET created_at = now() - interval '1 hour', expires_at = now() - interval '1 second' WHERE purpose = 'PASSWORD_RESET' AND user_id = $1::uuid AND consumed_at IS NULL`, e.userID(email))
	c.post("/api/v1/auth/reset-password", map[string]any{"token": second, "password": "New-Password-123"}).wantErr(t, 400, "invalid_token")
}

func TestForgotPasswordLimits(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	c := e.client()
	for i := 0; i < 5; i++ {
		c.mustCode(c.post("/api/v1/auth/forgot-password", map[string]any{"email": email}), 204)
	}
	// one address is mailed at most 3 times an hour, silently (still 204)
	if n := len(e.out.to(email)) - 1; n != 3 {
		t.Fatalf("reset mails sent: %d, want 3", n)
	}
	// an IP flood hits a visible 429
	f := e.client()
	var last *resp
	for i := 0; i < 11; i++ {
		last = f.post("/api/v1/auth/forgot-password", map[string]any{"email": uniq("z") + "@example.com"})
	}
	last.wantErr(t, 429, "rate_limited")
	if last.hdr.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestChangePassword(t *testing.T) {
	e := setupAuth(t)
	c, email, username := e.verifiedUser()
	other := e.client()
	other.login(email, strongPass).mustBe(t, 200)

	c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": "Wrong-Password-1", "newPassword": "New-Password-123"}).wantErr(t, 403, "invalid_credentials")
	c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": strongPass, "newPassword": "weak"}).wantErr(t, 422, "validation_failed")
	c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": strongPass, "newPassword": strongPass}).wantErr(t, 422, "validation_failed")
	c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": strongPass, "newPassword": username}).wantErr(t, 422, "validation_failed")

	c.mustCode(c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": strongPass, "newPassword": "New-Password-123"}), 204)
	c.get("/api/v1/auth/me").mustBe(t, 200) // this device stays signed in
	other.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	e.client().login(email, strongPass).wantErr(t, 401, "invalid_credentials")
	e.client().login(email, "New-Password-123").mustBe(t, 200)
	if subs := e.out.subjects(email); subs[len(subs)-1] != "Your REDXTUBE password was changed" {
		t.Fatalf("security notice missing: %v", subs)
	}
}

func TestChangePasswordGuessingIsLimited(t *testing.T) {
	e := setupAuth(t)
	c, _, _ := e.verifiedUser()
	for i := 0; i < 5; i++ {
		c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": "Wrong-Password-1", "newPassword": "New-Password-123"}).wantErr(t, 403, "invalid_credentials")
	}
	r := c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": strongPass, "newPassword": "New-Password-123"})
	r.wantErr(t, 429, "rate_limited")
	if r.hdr.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

// ---------- account, settings, deletion ----------

func TestUpdateAccount(t *testing.T) {
	e := setupAuth(t)
	c, _, _ := e.verifiedUser()
	for name, in := range map[string]map[string]any{
		"name too short": {"displayName": "x", "bio": ""},
		"name too long":  {"displayName": strings.Repeat("n", 41), "bio": ""},
		"bio too long":   {"displayName": "Maya", "bio": strings.Repeat("b", 301)},
		"blank name":     {"displayName": "   \t ", "bio": ""},
	} {
		if r := c.patch("/api/v1/account", in); r.code != 422 {
			t.Errorf("%s -> %d %s", name, r.code, r.body)
		}
	}
	r := c.patch("/api/v1/account", map[string]any{"displayName": "  Maya\u0000   Rose \u202e ", "bio": "Line one\nLine two\u0007"})
	r.mustBe(t, 200)
	if r.str("displayName") != "Maya Rose" || r.str("bio") != "Line one\nLine two" {
		t.Fatalf("control and bidi-override characters must be stripped: %q / %q", r.str("displayName"), r.str("bio"))
	}
	// Bengali needs ZWNJ/ZWJ, so those are preserved
	bn := "রাহুল\u200c আহমেদ"
	if r := c.patch("/api/v1/account", map[string]any{"displayName": bn, "bio": ""}); r.code != 200 || r.str("displayName") != bn {
		t.Fatalf("Bengali names must survive intact: %d %q", r.code, r.str("displayName"))
	}
	c.mustCode(c.patch("/api/v1/account", map[string]any{"displayName": "Maya Rose", "bio": "Line one\nLine two"}), 200)
	if c.get("/api/v1/auth/me").str("bio") != "Line one\nLine two" {
		t.Fatal("profile update must persist")
	}
	if r := c.post("/api/v1/auth/me", nil); r.code == 200 {
		t.Fatal("unexpected route")
	}
	if r := c.patch("/api/v1/account", map[string]any{"displayName": "Maya", "bio": "", "role": "ADMIN"}); r.code != 400 {
		t.Fatalf("clients cannot send extra fields like role: %d", r.code)
	}
}

func TestSettings(t *testing.T) {
	e := setupAuth(t)
	c, _, _ := e.verifiedUser()
	r := c.get("/api/v1/settings")
	r.mustBe(t, 200)
	p, n := r.m["privacy"].(map[string]any), r.m["notifications"].(map[string]any)
	if p["showSubscriptions"] != false || p["showLikedVideos"] != false || p["saveWatchHistory"] != true {
		t.Fatalf("privacy defaults: %v", p)
	}
	if n["newVideo"] != true || n["replies"] != true || n["newSubscriber"] != true || n["system"] != true || n["email"] != false {
		t.Fatalf("notification defaults (email must be opt-in): %v", n)
	}
	// partial nested patch changes only what was sent
	c.mustCode(c.patch("/api/v1/settings", map[string]any{"privacy": map[string]any{"showLikedVideos": true}}), 204)
	c.mustCode(c.patch("/api/v1/settings", map[string]any{"notifications": map[string]any{"email": true, "replies": false}}), 204)
	r = c.get("/api/v1/settings")
	p, n = r.m["privacy"].(map[string]any), r.m["notifications"].(map[string]any)
	if p["showLikedVideos"] != true || p["saveWatchHistory"] != true || p["showSubscriptions"] != false || n["email"] != true || n["replies"] != false || n["newVideo"] != true {
		t.Fatalf("patch result: %s", r.body)
	}
	c.mustCode(c.patch("/api/v1/settings", map[string]any{}), 204) // empty patch is fine
	for name, bad := range map[string]any{
		"unknown group": map[string]any{"admin": map[string]any{"x": true}},
		"unknown key":   map[string]any{"privacy": map[string]any{"isAdmin": true}},
		"wrong type":    map[string]any{"privacy": map[string]any{"showLikedVideos": "yes"}},
	} {
		if r := c.patch("/api/v1/settings", bad); r.code != 400 || r.str("code") != "invalid_json" {
			t.Errorf("%s -> %d %s", name, r.code, r.body)
		}
	}
	// settings are per user
	d, _, _ := e.verifiedUser()
	if d.get("/api/v1/settings").m["privacy"].(map[string]any)["showLikedVideos"] != false {
		t.Fatal("settings leaked between users")
	}
}

func TestDeleteAccount(t *testing.T) {
	e := setupAuth(t)
	c, email, username := e.verifiedUser()
	id := e.userID(email)
	// the user owns a public video and some personal data
	e.insertVideo(vspec{id: vid(77), creator: id, title: "my video", dur: 100})
	c.mustCode(c.patch("/api/v1/settings", map[string]any{"privacy": map[string]any{"showLikedVideos": true}}), 204)
	c.mustCode(c.patch("/api/v1/account", map[string]any{"displayName": "Maya Rose", "bio": "private bio"}), 200)
	reader := e.client()
	reader.passAgeGate()
	reader.mustCode(reader.get("/api/v1/videos/"+vid(77)), 200)

	c.post("/api/v1/account/delete", map[string]any{"password": "Wrong-Password-1"}).wantErr(t, 403, "invalid_credentials")
	c.get("/api/v1/auth/me").mustBe(t, 200)
	r := c.post("/api/v1/account/delete", map[string]any{"password": strongPass})
	r.mustBe(t, 204)
	if ck := r.cookie("xh_session"); ck == nil || ck.MaxAge >= 0 {
		t.Fatal("deleting must clear the session cookie")
	}
	c.get("/api/v1/auth/me").wantErr(t, 401, "unauthenticated")
	e.client().login(email, strongPass).wantErr(t, 401, "invalid_credentials")
	e.client().login(username, strongPass).wantErr(t, 401, "invalid_credentials")

	// the creator's content disappears and personal data is erased
	reader.get("/api/v1/videos/"+vid(77)).wantErr(t, 404, "video_not_found")
	reader.get("/api/v1/channels/"+id).wantErr(t, 404, "channel_not_found")
	var name, bio string
	e.scan([]any{&name, &bio}, `SELECT u.display_name, p.bio FROM users u JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1::uuid`, id)
	if name != "Deleted user" || bio != "" {
		t.Fatalf("profile not wiped: %q %q", name, bio)
	}
	var em string
	e.scan([]any{&em}, `SELECT email::text FROM users WHERE id = $1::uuid`, id)
	if strings.Contains(em, "example.com") || strings.Contains(em, username) {
		t.Fatalf("email not anonymised: %s", em)
	}
	for tbl, col := range map[string]string{"sessions": "user_id", "verification_tokens": "user_id", "watch_history": "user_id", "watch_later": "user_id", "subscriptions": "subscriber_id"} {
		if n := e.countRows(`SELECT count(*) FROM `+tbl+` WHERE `+col+` = $1::uuid`, id); n != 0 {
			t.Errorf("%s still has %d rows", tbl, n)
		}
	}
	if e.countRows(`SELECT count(*) FROM audit_logs WHERE action = 'account.delete' AND actor_user_id = $1::uuid`, id) != 1 {
		t.Fatal("deletion must be audited")
	}
	// the email and username are free again
	n := e.client()
	n.passAgeGate()
	n.register(email, username, strongPass).mustBe(t, 201)
}

// ---------- CSRF, roles, failure modes, logging, audit ----------

func TestCSRFOriginChecks(t *testing.T) {
	e := setupAuth(t)
	_, email, _ := e.verifiedUser()
	c := e.client()
	c.origin = "https://evil.example"
	c.login(email, strongPass).wantErr(t, 403, "csrf_blocked")
	c.origin = "http://redxtube.test" // wrong scheme
	c.login(email, strongPass).wantErr(t, 403, "csrf_blocked")
	c.origin = appOrigin + ":8443"
	c.login(email, strongPass).wantErr(t, 403, "csrf_blocked")
	c.origin = "null"
	c.login(email, strongPass).wantErr(t, 403, "csrf_blocked")
	if c.session() != "" {
		t.Fatal("blocked requests must not create sessions")
	}
	c.origin = appOrigin
	c.login(email, strongPass).mustBe(t, 200)

	// no Origin (curl, mobile, server-to-server) is allowed unless the browser says cross-site
	n := e.client()
	n.origin = ""
	n.login(email, strongPass).mustBe(t, 200)
	n2 := e.client()
	n2.origin = ""
	n2.headers["Sec-Fetch-Site"] = "cross-site"
	n2.login(email, strongPass).wantErr(t, 403, "csrf_blocked")
	n2.headers["Sec-Fetch-Site"] = "same-site"
	n2.login(email, strongPass).mustBe(t, 200)
	// reads are never blocked
	e2 := e.client()
	e2.origin = "https://evil.example"
	e2.mustCode(e2.get("/api/v1/reports/reasons"), 200)
}

func TestRoleGuardsUseDatabaseRolesOnly(t *testing.T) {
	e := setupAuth(t)
	c, email, _ := e.verifiedUser()
	unverified, _, _ := e.newUser()

	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }
	mux.HandleFunc("GET /creator", middleware.RequireRole(auth.RoleCreator)(ok))
	mux.HandleFunc("GET /mod", middleware.RequireRole(auth.RoleModerator)(ok))
	mux.HandleFunc("GET /admin", middleware.RequireRole(auth.RoleAdmin)(ok))
	mux.HandleFunc("GET /verified", middleware.RequireVerified(ok))
	mux.HandleFunc("GET /any", middleware.RequireAuth(ok))
	h := httpx.Chain(mux, httpx.RequestID(), middleware.Authenticate(e.svc, e.cookies))
	for _, cl := range []*client{c, unverified, e.client()} {
		cl.h = h
	}
	anon := e.client()
	anon.h = h

	anon.get("/any").wantErr(t, 401, "unauthenticated")
	anon.get("/admin").wantErr(t, 401, "unauthenticated")
	unverified.get("/any").mustBe(t, 200)
	unverified.get("/verified").wantErr(t, 403, "verification_required")
	unverified.get("/creator").wantErr(t, 403, "verification_required")

	c.get("/verified").mustBe(t, 200)
	c.get("/creator").wantErr(t, 403, "forbidden")
	c.get("/admin").wantErr(t, 403, "forbidden")
	// nothing the client sends can raise its role
	c.headers["X-Role"] = "ADMIN"
	c.headers["X-User-Role"] = "SUPER_ADMIN"
	c.jar["role"] = &http.Cookie{Name: "role", Value: "ADMIN"}
	c.get("/admin").wantErr(t, 403, "forbidden")

	// the role is read from the database on each request: promotions and demotions are immediate
	e.exec(`UPDATE users SET role = 'MODERATOR' WHERE email = $1`, email)
	c.get("/mod").mustBe(t, 200)
	c.get("/creator").mustBe(t, 200)
	c.get("/admin").wantErr(t, 403, "forbidden")
	e.exec(`UPDATE users SET role = 'ADMIN' WHERE email = $1`, email)
	c.get("/admin").mustBe(t, 200)
	if me := c.get("/any"); me.code != 200 {
		t.Fatal("unexpected")
	}
	e.exec(`UPDATE users SET role = 'USER' WHERE email = $1`, email)
	c.get("/admin").wantErr(t, 403, "forbidden")
}

type brokenLimiter struct{}

func (brokenLimiter) Hit(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 0, 0, errors.New("redis: connection refused")
}
func (brokenLimiter) Peek(context.Context, string) (int64, time.Duration, error) {
	return 0, 0, errors.New("redis: connection refused")
}
func (brokenLimiter) Reset(context.Context, string) error {
	return errors.New("redis: connection refused")
}

var _ service.Limiter = brokenLimiter{}

func TestAuthFailsClosedWhenRateLimiterIsDown(t *testing.T) {
	good := setupAuth(t)
	_, email, _ := good.verifiedUser()
	e := setupAuthWith(t, brokenLimiter{})
	_ = email
	c := e.client()
	// age/register/login/forgot must refuse rather than run without brute-force protection
	c.post("/api/v1/age/verify", map[string]any{"confirmed": true}).wantErr(t, 503, "service_unavailable")
	c.login("someone@example.com", strongPass).wantErr(t, 503, "service_unavailable")
	c.post("/api/v1/auth/forgot-password", map[string]any{"email": "someone@example.com"}).wantErr(t, 503, "service_unavailable")
	if strings.Contains(string(c.login("a@example.com", "x").body), "redis") {
		t.Fatal("infrastructure details leaked to the client")
	}
}

func TestSecretsNeverAppearInLogs(t *testing.T) {
	e := setupAuth(t)
	c, email, username := e.newUser()
	code := e.out.code(email)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	c.post("/api/v1/auth/verify", map[string]any{"code": wrong})
	c.post("/api/v1/auth/verify", map[string]any{"code": code}).mustBe(t, 200)
	e.client().login(email, "Wrong-Password-1")
	e.client().login(email, strongPass).mustBe(t, 200)
	e.client().post("/api/v1/auth/forgot-password", map[string]any{"email": email})
	token := e.out.resetToken(email)
	e.client().post("/api/v1/auth/reset-password", map[string]any{"token": token, "password": "New-Password-123"}).mustBe(t, 204)
	c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": "x", "newPassword": "y"})
	e.client().post("/api/v1/auth/login", map[string]any{"identifier": email, "password": 12345}) // malformed body

	logs := e.logs.String()
	if !strings.Contains(logs, "http_request") {
		t.Fatal("expected request logs")
	}
	for name, secret := range map[string]string{
		"password": strongPass, "wrong password": "Wrong-Password-1", "new password": "New-Password-123",
		"email code": code, "reset token": token, "session token": c.session(), "email address": email, "username": username,
	} {
		if secret != "" && strings.Contains(logs, secret) {
			t.Errorf("%s leaked into the logs", name)
		}
	}
}

func TestAuditTrail(t *testing.T) {
	e := setupAuth(t)
	c, email, _ := e.verifiedUser()
	id := e.userID(email)
	e.client().login(email, strongPass).mustBe(t, 200)
	c.mustCode(c.post("/api/v1/auth/change-password", map[string]any{"currentPassword": strongPass, "newPassword": "New-Password-123"}), 204)
	for _, action := range []string{"auth.register", "auth.email_verified", "auth.login", "auth.password_change"} {
		if e.countRows(`SELECT count(*) FROM audit_logs WHERE actor_user_id = $1::uuid AND action = $2 AND metadata ? 'request_id' AND ip_hash IS NOT NULL`, id, action) < 1 {
			t.Errorf("missing audit entry %s (with request id and a hashed ip)", action)
		}
	}
	var meta string
	e.scan([]any{&meta}, `SELECT string_agg(metadata::text, '') FROM audit_logs WHERE actor_user_id = $1::uuid`, id)
	if strings.Contains(meta, strongPass) || strings.Contains(meta, "New-Password-123") {
		t.Fatal("audit metadata must not hold secrets")
	}
	// the trail cannot be edited
	if _, err := e.pool.Exec(context.Background(), `UPDATE audit_logs SET action = 'x' WHERE actor_user_id = $1::uuid`, id); err == nil {
		t.Fatal("audit_logs must be append-only")
	}
}

func TestRequestBodyLimitsAndMalformedJSON(t *testing.T) {
	e := setupAuth(t)
	c := e.client()
	c.passAgeGate()
	huge := strings.Repeat("a", 2<<20)
	c.post("/api/v1/auth/login", map[string]any{"identifier": huge, "password": "x"}).wantErr(t, 413, "too_large")
	r := c.do("POST", "/api/v1/auth/login", nil)
	r.wantErr(t, 400, "invalid_json")
}
