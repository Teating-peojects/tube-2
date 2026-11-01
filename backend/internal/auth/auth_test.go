package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// light parameters keep tests fast; production uses NewHasher (64 MiB, t=3).
func testHasher() *Hasher { return NewHasherWithParams(8*1024, 1, 1, 4) }

func TestPasswordHashAndVerify(t *testing.T) {
	h, ctx := testHasher(), context.Background()
	enc, err := h.Hash(ctx, "Correct-Horse-9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=8192,t=1,p=1$") || strings.Contains(enc, "Correct-Horse") {
		t.Fatalf("unexpected encoding: %s", enc)
	}
	if ok, rehash, err := h.Verify(ctx, "Correct-Horse-9", enc); !ok || rehash || err != nil {
		t.Fatalf("valid password rejected: %v %v %v", ok, rehash, err)
	}
	if ok, _, _ := h.Verify(ctx, "correct-horse-9", enc); ok {
		t.Fatal("wrong password accepted")
	}
	enc2, _ := h.Hash(ctx, "Correct-Horse-9")
	if enc == enc2 {
		t.Fatal("salts must be random")
	}
}

func TestVerifyRejectsMalformedAndHostileHashes(t *testing.T) {
	h, ctx := testHasher(), context.Background()
	for _, bad := range []string{
		"", "plaintext", "$argon2id$v=19$m=8192,t=1,p=1$abc",
		"$bcrypt$v=19$m=8192,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=999999999,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5", // memory bomb
		"$argon2id$v=19$m=8192,t=9999,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",   // CPU bomb
		"$argon2id$v=19$m=8192,t=1,p=1$!!!$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=18$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
	} {
		if ok, _, err := h.Verify(ctx, "x", bad); ok || err == nil {
			t.Errorf("hash %q: ok=%v err=%v", bad, ok, err)
		}
	}
}

func TestNeedsRehashWhenParametersWeaker(t *testing.T) {
	ctx := context.Background()
	old := NewHasherWithParams(8*1024, 1, 1, 2)
	enc, _ := old.Hash(ctx, "Some-Password-1")
	upgraded := NewHasherWithParams(16*1024, 2, 1, 2)
	ok, rehash, err := upgraded.Verify(ctx, "Some-Password-1", enc)
	if !ok || !rehash || err != nil {
		t.Fatalf("ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestHashRejectsHugePasswords(t *testing.T) {
	h, ctx := testHasher(), context.Background()
	if _, err := h.Hash(ctx, strings.Repeat("a", 5000)); err == nil {
		t.Fatal("must refuse to hash a 5000-byte password (DoS)")
	}
	enc, _ := h.Hash(ctx, "ok-Password-1")
	if ok, _, _ := h.Verify(ctx, strings.Repeat("a", 5000), enc); ok {
		t.Fatal("huge password accepted")
	}
}

func TestHasherConcurrencyLimitHonoursContext(t *testing.T) {
	h := NewHasherWithParams(8*1024, 1, 1, 1)
	h.sem <- struct{}{} // occupy the only slot
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "Password-12345"); err == nil {
		t.Fatal("expected context error when no slot is free")
	}
}

func TestBurnTakesComparableTime(t *testing.T) {
	h, ctx := NewHasherWithParams(32*1024, 2, 1, 2), context.Background()
	enc, _ := h.Hash(ctx, "Real-Password-1")
	start := time.Now()
	h.Verify(ctx, "wrong", enc)
	real := time.Since(start)
	start = time.Now()
	h.Burn(ctx, "wrong")
	burn := time.Since(start)
	if burn < real/3 || burn > real*3 {
		t.Fatalf("Burn (%v) should cost about the same as a real verify (%v)", burn, real)
	}
}

func TestValidatePassword(t *testing.T) {
	ok := []string{"Tr0ub4dor-and-3", "correcthorse9", "pässwörter-2025x", strings.Repeat("a1", 64)}
	for _, p := range ok {
		if r := ValidatePassword(p, "alice", "alice@example.com"); r != "" {
			t.Errorf("%q rejected: %s", p, r)
		}
	}
	bad := map[string]string{
		"short1":                    "at least 10",
		strings.Repeat("a1", 65):    "at most 128",
		"onlyletterslongenough":     "number",
		"1234567890123":             "letter",
		"Password123":               "too common",
		"alice12345":                "username",
		"alice\x00password1":        "Invalid",
		"bad\xffbytes12345":         "Invalid",
		"aliceexample1@example.com": "",
	}
	for p, want := range bad {
		r := ValidatePassword(p, "alice12345", "aliceexample1@example.com")
		if want == "" {
			continue
		}
		if !strings.Contains(r, want) {
			t.Errorf("%q: got %q want %q", p, r, want)
		}
	}
	if r := ValidatePassword("someone1234", "x", "someone1234@example.com"); !strings.Contains(r, "email") {
		t.Errorf("email local part as password must be rejected, got %q", r)
	}
}

func TestOTP(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 3000; i++ {
		c := GenerateOTP()
		if len(c) != 6 || strings.Trim(c, "0123456789") != "" {
			t.Fatalf("bad otp %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 2500 {
		t.Fatalf("otp looks non-random: %d distinct of 3000", len(seen))
	}
	k := DeriveKeys([]byte(strings.Repeat("k", 40)))
	a := OTPDigest(k.OTP, "user-1", "123456")
	if !Equal(a, OTPDigest(k.OTP, "user-1", "123456")) {
		t.Fatal("digest must be deterministic")
	}
	if Equal(a, OTPDigest(k.OTP, "user-2", "123456")) {
		t.Fatal("digest must be bound to the user")
	}
	if Equal(a, OTPDigest(k.OTP, "user-1", "123457")) {
		t.Fatal("digest must depend on the code")
	}
	if Equal(a, OTPDigest(k.Age, "user-1", "123456")) {
		t.Fatal("keys must differ per purpose")
	}
}

func TestRandomTokenAndHash(t *testing.T) {
	a, b := RandomToken(32), RandomToken(32)
	if a == b || len(a) != 43 || strings.ContainsAny(a, "+/=") {
		t.Fatalf("bad tokens %q %q", a, b)
	}
	if len(HashToken(a)) != 32 || Equal(HashToken(a), HashToken(b)) {
		t.Fatal("hash must be 32 bytes and distinct")
	}
}

const uid = "0a1b2c3d-0000-4000-8000-000000000001"

func TestAgeCookieSignVerify(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s := NewAgeSigner([]byte(strings.Repeat("s", 32)))
	s.now = func() time.Time { return now }

	tok, exp := s.Issue(uid, 30*24*time.Hour)
	if !exp.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expiry %v", exp)
	}
	if sub, ok := s.Verify(tok); !ok || sub != uid {
		t.Fatalf("valid token rejected (%v %q)", ok, sub)
	}

	parts := strings.Split(tok, ".")
	tampered := map[string]string{
		"subject":  "v1.0a1b2c3d-0000-4000-8000-000000000002." + parts[2] + "." + parts[3],
		"expiry":   "v1." + parts[1] + ".99999999999." + parts[3],
		"mac":      "v1." + parts[1] + "." + parts[2] + ".AAAA",
		"version":  "v2." + parts[1] + "." + parts[2] + "." + parts[3],
		"plain 1":  "1",
		"empty":    "",
		"extra":    tok + ".x",
		"bad uuid": "v1.not-a-uuid." + parts[2] + "." + parts[3],
		"bad exp":  "v1." + parts[1] + ".abc." + parts[3],
	}
	for name, v := range tampered {
		if _, ok := s.Verify(v); ok {
			t.Errorf("%s: tampered token accepted", name)
		}
	}
	other := NewAgeSigner([]byte(strings.Repeat("z", 32)))
	other.now = s.now
	if _, ok := other.Verify(tok); ok {
		t.Fatal("token signed with another key accepted")
	}
	s.now = func() time.Time { return now.Add(31 * 24 * time.Hour) }
	if _, ok := s.Verify(tok); ok {
		t.Fatal("expired token accepted")
	}
}

func TestRoles(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleModerator) || RoleUser.AtLeast(RoleCreator) || !RoleSuperAdmin.AtLeast(RoleAdmin) || !RoleUser.AtLeast(RoleUser) {
		t.Fatal("role ranking wrong")
	}
	if Role("ROOT").AtLeast(RoleUser) || RoleAdmin.AtLeast(Role("ROOT")) || Role("").Valid() {
		t.Fatal("unknown roles must never pass")
	}
}

func TestClientIP(t *testing.T) {
	req := func(remote string, hdr map[string]string) *httptestReq {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return &httptestReq{r}
	}
	spoof := map[string]string{"CF-Connecting-IP": "9.9.9.9", "X-Forwarded-For": "1.1.1.1, 2.2.2.2"}
	if got := ClientIP(req("203.0.113.7:5555", spoof).r, false); got != "203.0.113.7" {
		t.Fatalf("headers must be ignored when proxy is not trusted: %s", got)
	}
	if got := ClientIP(req("10.0.0.1:1", spoof).r, true); got != "9.9.9.9" {
		t.Fatalf("CF header should win: %s", got)
	}
	if got := ClientIP(req("10.0.0.1:1", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.4"}).r, true); got != "198.51.100.4" {
		t.Fatalf("right-most XFF entry expected: %s", got)
	}
	if got := ClientIP(req("10.0.0.1:1", map[string]string{"X-Forwarded-For": "garbage"}).r, true); got != "10.0.0.1" {
		t.Fatalf("garbage XFF falls back to peer: %s", got)
	}
	if got := ClientIP(req("[2001:db8::1]:443", nil).r, false); got != "2001:db8::1" {
		t.Fatalf("ipv6: %s", got)
	}
	if string(HashIP([]byte("k"), "1.2.3.4")) == "1.2.3.4" || len(HashIP([]byte("k"), "1.2.3.4")) != 32 {
		t.Fatal("ip must be hashed")
	}
}

type httptestReq struct{ r *http.Request }
