package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
)

var discard = slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

func TestErrorSafeBody(t *testing.T) {
	h := Handle(func(w http.ResponseWriter, r *http.Request) error {
		return apperr.Internal(errors.New("pq: password authentication failed for user admin"))
	})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", "/", nil))

	if rr.Code != 500 {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "password") || strings.Contains(body, "pq:") {
		t.Fatalf("internal detail leaked: %s", body)
	}
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out["code"] != "internal" {
		t.Fatalf("code = %v", out["code"])
	}
}

func TestErrorCodeAndFields(t *testing.T) {
	h := Handle(func(w http.ResponseWriter, r *http.Request) error {
		return apperr.Validation(map[string]string{"email": "invalid"})
	})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("POST", "/", nil))
	if rr.Code != 422 || !strings.Contains(rr.Body.String(), `"code":"validation_failed"`) || !strings.Contains(rr.Body.String(), `"email":"invalid"`) {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestRateLimitedSetsRetryAfter(t *testing.T) {
	rr := httptest.NewRecorder()
	Error(rr, httptest.NewRequest("GET", "/", nil), apperr.TooManyRequests(90*time.Second+time.Millisecond))
	if rr.Code != 429 || rr.Header().Get("Retry-After") != "91" {
		t.Fatalf("got %d retry-after=%q (want rounded-up seconds)", rr.Code, rr.Header().Get("Retry-After"))
	}
	rr = httptest.NewRecorder()
	Error(rr, httptest.NewRequest("GET", "/", nil), apperr.TooManyRequests(0))
	if rr.Header().Get("Retry-After") != "30" {
		t.Fatalf("default retry-after = %q", rr.Header().Get("Retry-After"))
	}
}

func TestDecodeJSONStrict(t *testing.T) {
	type in struct {
		A string `json:"a"`
	}
	var v in
	if err := DecodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{"a":"x"}`)), &v); err != nil || v.A != "x" {
		t.Fatalf("valid body rejected: %v", err)
	}
	for _, body := range []string{`{"a":"x","b":1}`, `{"a":"x"} {"a":"y"}`, `nope`} {
		if err := DecodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(body)), &in{}); err == nil {
			t.Errorf("body %q should be rejected", body)
		}
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }), RequestID())

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if seen == "" || rr.Header().Get("X-Request-ID") != seen {
		t.Fatalf("generated id missing: %q", seen)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "abc12345-trace")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "abc12345-trace" {
		t.Fatalf("valid inbound id not kept: %q", seen)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "bad id\nwith newline")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen == "bad id\nwith newline" {
		t.Fatal("unsafe inbound id accepted")
	}
}

func TestRecover(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }), RequestID(), Recover(discard))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != 500 || strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), SecurityHeaders(true))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	for _, k := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy", "Strict-Transport-Security", "Cache-Control"} {
		if rr.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
	rr = httptest.NewRecorder()
	Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), SecurityHeaders(false)).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must be off outside production")
	}
}

func TestCORS(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := Chain(ok, CORS([]string{"https://redxtube.com"}))

	// allowed origin
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://redxtube.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://redxtube.com" || rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("allowed origin not echoed: %v", rr.Header())
	}
	if !strings.Contains(rr.Header().Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Fatal("Retry-After must be exposed to the frontend")
	}

	// foreign origin gets no CORS headers
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin must not be allowed")
	}

	// preflight
	req = httptest.NewRequest("OPTIONS", "/api/v1/videos", nil)
	req.Header.Set("Origin", "https://redxtube.com")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 204 || !strings.Contains(rr.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("preflight failed: %d %v", rr.Code, rr.Header())
	}

	// preflight from foreign origin is refused
	req = httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("foreign preflight = %d", rr.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	h := Chain(Handle(func(w http.ResponseWriter, r *http.Request) error {
		var v map[string]any
		return DecodeJSON(r, &v)
	}), BodyLimit(16))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/", strings.NewReader(`{"a":"`+strings.Repeat("x", 100)+`"}`)))
	if rr.Code != 413 {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestLoggerWrapperSupportsHijack(t *testing.T) {
	var ok bool
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = w.(http.Hijacker)
	}), Logger(discard))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !ok {
		t.Fatal("WebSocket upgrades need the logging wrapper to implement http.Hijacker")
	}
}
