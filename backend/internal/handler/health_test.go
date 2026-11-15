package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redxtube/api/internal/handler"
)

type healthPinger struct{ err error }

func (p healthPinger) Ping(context.Context) error { return p.err }

func ready(t *testing.T, h handler.Health) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return rec.Code, body
}

func TestReadyOptionalDependenciesNeverFailReadiness(t *testing.T) {
	down := errors.New("dial tcp: connection refused")

	// all healthy: unchanged shape, no "degraded" key
	code, body := ready(t, handler.Health{Checks: map[string]handler.Pinger{"postgres": healthPinger{}}, Optional: map[string]handler.Pinger{"storage": healthPinger{}}})
	if code != 200 || body["status"] != "ok" || body["checks"].(map[string]any)["storage"] != "ok" {
		t.Fatalf("healthy: %d %v", code, body)
	}
	if _, has := body["degraded"]; has {
		t.Fatalf("nothing is degraded: %v", body)
	}

	// storage down: still 200 and in rotation, but reported
	code, body = ready(t, handler.Health{Checks: map[string]handler.Pinger{"postgres": healthPinger{}}, Optional: map[string]handler.Pinger{"storage": healthPinger{down}}})
	deg, _ := body["degraded"].([]any)
	if code != 200 || body["status"] != "ok" || len(deg) != 1 || deg[0] != "storage" || body["checks"].(map[string]any)["storage"] != "error" {
		t.Fatalf("storage down must degrade, not fail: %d %v", code, body)
	}
	if raw, _ := json.Marshal(body); containsAny(string(raw), "connection refused", "dial tcp") {
		t.Fatalf("error details leaked: %s", raw)
	}

	// a required dependency still fails readiness, whatever the optional ones say
	code, body = ready(t, handler.Health{Checks: map[string]handler.Pinger{"postgres": healthPinger{down}}, Optional: map[string]handler.Pinger{"storage": healthPinger{}}})
	if code != 503 || body["status"] != "unavailable" {
		t.Fatalf("required down: %d %v", code, body)
	}

	// no optional dependencies configured (uploads off): same as before
	if code, body = ready(t, handler.Health{Checks: map[string]handler.Pinger{"postgres": healthPinger{}}}); code != 200 || body["status"] != "ok" {
		t.Fatalf("no optional: %d %v", code, body)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
