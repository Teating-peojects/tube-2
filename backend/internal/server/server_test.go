package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redxtube/api/internal/config"
	"github.com/redxtube/api/internal/handler"
)

type fake struct{ err error }

func (f fake) Ping(context.Context) error { return f.err }

func newTestServer(checks map[string]handler.Pinger) http.Handler {
	return New(Deps{
		Cfg:    config.Config{Env: "development", MaxBodyBytes: 1 << 20, CORSOrigins: []string{"http://localhost:3000"}},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Checks: checks,
	})
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
	return rr
}

func TestHealthIsAlwaysOK(t *testing.T) {
	h := newTestServer(map[string]handler.Pinger{"postgres": fake{errors.New("down")}})
	if rr := get(h, "/health"); rr.Code != 200 {
		t.Fatalf("/health = %d (liveness must not depend on dependencies)", rr.Code)
	}
}

func TestReady(t *testing.T) {
	ok := newTestServer(map[string]handler.Pinger{"postgres": fake{}, "redis": fake{}})
	if rr := get(ok, "/ready"); rr.Code != 200 {
		t.Fatalf("ready = %d", rr.Code)
	}

	bad := newTestServer(map[string]handler.Pinger{"postgres": fake{}, "redis": fake{errors.New("dial tcp 10.0.0.5:6379: refused")}})
	rr := get(bad, "/ready")
	if rr.Code != 503 {
		t.Fatalf("ready = %d, want 503", rr.Code)
	}
	var out struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out.Status != "unavailable" || out.Checks["redis"] != "error" || out.Checks["postgres"] != "ok" {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
	if rr.Body.String() != "" && (contains(rr.Body.String(), "10.0.0.5") || contains(rr.Body.String(), "refused")) {
		t.Fatalf("dependency error leaked: %s", rr.Body.String())
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

func TestNotFoundIsJSONWithCode(t *testing.T) {
	rr := get(newTestServer(nil), "/nope")
	if rr.Code != 404 || !contains(rr.Body.String(), `"code":"not_found"`) {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-Request-ID") == "" || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("middleware chain not applied")
	}
}

func TestStreamRouteSkipsSessionLookup(t *testing.T) {
	var authnCalls int
	authn := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authnCalls++
			next.ServeHTTP(w, r)
		})
	}
	var gotToken, gotKey string
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken, gotKey = r.PathValue("token"), r.PathValue("key")
		w.WriteHeader(http.StatusNoContent)
	})
	h := New(Deps{
		Cfg:    config.Config{Env: "development", MaxBodyBytes: 1 << 20},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stream: stream, Authn: authn,
	})
	if rr := get(h, "/stream/123.abc/hls/v/720p/seg_00001.ts"); rr.Code != 204 || gotToken != "123.abc" || gotKey != "hls/v/720p/seg_00001.ts" {
		t.Fatalf("stream route: %d token=%q key=%q", rr.Code, gotToken, gotKey)
	}
	if authnCalls != 0 {
		t.Fatalf("session lookup ran for a stream request (%d)", authnCalls)
	}
	get(h, "/health")
	if authnCalls != 1 {
		t.Fatalf("authn must still run for normal requests, ran %d times", authnCalls)
	}
	// disabled stream: the path is just a 404
	if rr := get(newTestServer(nil), "/stream/a/b"); rr.Code != 404 {
		t.Errorf("stream disabled: %d", rr.Code)
	}
}
