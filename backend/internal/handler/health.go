// Package handler holds thin HTTP handlers. Business logic lives in services.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/redxtube/api/internal/httpx"
)

// Pinger is satisfied by *pgxpool.Pool and cache.Pinger.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health serves liveness (/health) and readiness (/ready).
type Health struct {
	Checks  map[string]Pinger // name -> dependency
	Timeout time.Duration     // per-check timeout
	Log     *slog.Logger
}

// Live reports that the process is up. It never touches dependencies, so an
// orchestrator won't restart a healthy API just because Postgres is down.
func (h Health) Live(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready reports whether dependencies are reachable. Error details are logged,
// never returned to the caller.
func (h Health) Ready(w http.ResponseWriter, r *http.Request) {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	results := make(map[string]string, len(h.Checks))
	healthy := true
	for name, p := range h.Checks {
		if err := p.Ping(ctx); err != nil {
			healthy = false
			results[name] = "error"
			if h.Log != nil {
				h.Log.WarnContext(r.Context(), "readiness check failed", "dependency", name, "err", err.Error())
			}
			continue
		}
		results[name] = "ok"
	}

	status, code := "ok", http.StatusOK
	if !healthy {
		status, code = "unavailable", http.StatusServiceUnavailable
	}
	httpx.JSON(w, code, map[string]any{"status": status, "checks": results})
}
