// Package server wires routes and middleware. Feature routes are registered
// under /api/v1 in later phases (see docs/api-contract.md).
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/config"
	"github.com/redxtube/api/internal/handler"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
)

// Deps are the dependencies the HTTP layer needs.
type Deps struct {
	Cfg    config.Config
	Log    *slog.Logger
	Checks map[string]handler.Pinger
	// Catalog serves the read-only discovery API; nil disables it (tests, early phases).
	Catalog *handler.Catalog
	// Auth serves accounts, sessions and the age gate; nil disables auth (and the age gate on the catalog).
	Auth *handler.Auth
	// Social serves reactions, comments, playlists, history, reports (needs Auth for sessions).
	Social *handler.Social
	// Upload serves the creator upload flow (needs Auth for sessions); nil disables it.
	Upload *handler.Upload
	// Notifications, Studio and Realtime need Auth for sessions; nil disables each.
	Notifications *handler.Notifications
	Studio        *handler.Studio
	Realtime      *handler.Realtime
	// Admin serves moderation and administration (needs Auth); nil disables it.
	Admin *handler.Admin
	// Authn resolves the session cookie on every request (required when Auth is set).
	Authn httpx.Middleware
}

// New builds the root http.Handler.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()

	health := handler.Health{Checks: d.Checks, Timeout: 2 * time.Second, Log: d.Log}
	mux.HandleFunc("GET /health", health.Live)
	mux.HandleFunc("GET /ready", health.Ready)

	if d.Auth != nil {
		d.Auth.Routes(mux)
	}
	var gate handler.Gate
	if d.Auth != nil {
		gate = handler.Gate(middleware.RequireAge(d.Auth.Svc.AgeSigner(), d.Auth.Cookies.Age))
	}
	if d.Catalog != nil {
		d.Catalog.Routes(mux, gate)
	}
	if d.Social != nil {
		d.Social.Routes(mux, gate)
	}
	if d.Upload != nil {
		d.Upload.Routes(mux)
	}
	if d.Notifications != nil {
		d.Notifications.Routes(mux)
	}
	if d.Studio != nil {
		d.Studio.Routes(mux)
	}
	if d.Realtime != nil {
		d.Realtime.Routes(mux)
	}
	if d.Admin != nil {
		d.Admin.Routes(mux)
	}

	mux.HandleFunc("/", httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return apperr.NotFound("not_found", "Resource not found")
	}))

	chain := []httpx.Middleware{
		httpx.RequestID(),
		httpx.Logger(d.Log),
		httpx.Recover(d.Log),
		httpx.SecurityHeaders(d.Cfg.IsProduction()),
		httpx.CORS(d.Cfg.CORSOrigins),
		middleware.CSRFOrigin(d.Cfg.CORSOrigins),
		httpx.BodyLimit(d.Cfg.MaxBodyBytes),
	}
	if d.Authn != nil {
		chain = append(chain, d.Authn)
	}
	return httpx.Chain(mux, chain...)
}
