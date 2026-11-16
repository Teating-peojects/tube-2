package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// Processing serves the processing controls (Phase 6):
//
//	POST /api/v1/studio/videos/{id}/processing/cancel   creator (own video) or MODERATOR+; body {"reason"} optional
//	POST /api/v1/admin/videos/{id}/processing/retry     MODERATOR+; body {"reason"} required
//
// Paths follow the existing split: creator actions under /studio, staff actions under /admin.
type Processing struct {
	Svc        *service.Processing
	TrustProxy bool
}

func (h Processing) info(r *http.Request) service.ClientInfo {
	return service.ClientInfo{IP: auth.ClientIP(r, h.TrustProxy), UserAgent: r.UserAgent(), RequestID: httpx.RequestIDFrom(r.Context())}
}

func (h Processing) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/studio/videos/{id}/processing/cancel", middleware.RequireVerified(httpx.Handle(h.cancel)))
	mux.HandleFunc("POST /api/v1/admin/videos/{id}/processing/retry", middleware.RequireRole(auth.RoleModerator)(httpx.Handle(h.retry)))
}

func (h Processing) cancel(w http.ResponseWriter, r *http.Request) error {
	// the body is optional for creators: an empty request means "no reason given"
	var in struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &in); err != nil {
			return err
		}
	}
	if err := h.Svc.Cancel(r.Context(), me(r), h.info(r), r.PathValue("id"), in.Reason); err != nil {
		return err
	}
	return done(w)
}

func (h Processing) retry(w http.ResponseWriter, r *http.Request) error {
	reason, err := reasonBody(r)
	if err != nil {
		return err
	}
	if err := h.Svc.Retry(r.Context(), me(r), h.info(r), r.PathValue("id"), reason); err != nil {
		return err
	}
	return done(w)
}
