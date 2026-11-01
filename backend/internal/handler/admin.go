package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// Admin serves moderation (MODERATOR+), administration (ADMIN+) and role changes (SUPER_ADMIN).
// Routes are role-gated here and the service re-checks what depends on the target.
type Admin struct {
	Svc        *service.Admin
	TrustProxy bool
}

func (h Admin) info(r *http.Request) service.ClientInfo {
	return service.ClientInfo{IP: auth.ClientIP(r, h.TrustProxy), UserAgent: r.UserAgent(), RequestID: httpx.RequestIDFrom(r.Context())}
}

func (h Admin) Routes(mux *http.ServeMux) {
	const v = "/api/v1/admin"
	role := func(min auth.Role, f httpx.HandlerFunc) http.HandlerFunc {
		return middleware.RequireRole(min)(httpx.Handle(f))
	}
	mod := func(f httpx.HandlerFunc) http.HandlerFunc { return role(auth.RoleModerator, f) }
	adm := func(f httpx.HandlerFunc) http.HandlerFunc { return role(auth.RoleAdmin, f) }

	mux.HandleFunc("GET "+v+"/reports", mod(h.reports))
	mux.HandleFunc("POST "+v+"/reports/{id}/assign", mod(h.assign))
	mux.HandleFunc("POST "+v+"/reports/{id}/resolve", mod(h.resolve))
	mux.HandleFunc("POST "+v+"/actions", mod(h.act))
	mux.HandleFunc("POST "+v+"/videos/{id}/restore", mod(h.restoreVideo))
	mux.HandleFunc("POST "+v+"/users/{id}/restore", mod(h.restoreUser))

	mux.HandleFunc("GET "+v+"/users", adm(h.users))
	mux.HandleFunc("GET "+v+"/audit", adm(h.audit))
	mux.HandleFunc("GET "+v+"/blocked-hashes", adm(h.hashes))
	mux.HandleFunc("POST "+v+"/blocked-hashes", adm(h.blockHash))
	mux.HandleFunc("DELETE "+v+"/blocked-hashes/{sha256}", adm(h.unblockHash))
	mux.HandleFunc("PUT "+v+"/users/{id}/role", role(auth.RoleSuperAdmin, h.setRole))
}

type actionBody struct {
	Action string  `json:"action"`
	Reason string  `json:"reason"`
	Notes  *string `json:"notes"`
	Days   *int    `json:"days"`
}

func (b actionBody) input() service.ActionInput {
	return service.ActionInput{Action: b.Action, Reason: b.Reason, Notes: b.Notes, Days: b.Days}
}

func (h Admin) reports(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Reports(r.Context(), q.Get("status"), q.Get("cursor"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Admin) assign(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.AssignReport(r.Context(), me(r), h.info(r), r.PathValue("id")); err != nil {
		return err
	}
	return done(w)
}

func (h Admin) resolve(w http.ResponseWriter, r *http.Request) error {
	var in actionBody
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.ResolveReport(r.Context(), me(r), h.info(r), r.PathValue("id"), in.input()); err != nil {
		return err
	}
	return done(w)
}

func (h Admin) act(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		actionBody
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Act(r.Context(), me(r), h.info(r), nil, in.TargetType, in.TargetID, in.input()); err != nil {
		return err
	}
	return done(w)
}

func reasonBody(r *http.Request) (string, error) {
	var in struct {
		Reason string `json:"reason"`
	}
	err := httpx.DecodeJSON(r, &in)
	return in.Reason, err
}

func (h Admin) restoreVideo(w http.ResponseWriter, r *http.Request) error {
	reason, err := reasonBody(r)
	if err != nil {
		return err
	}
	if err := h.Svc.RestoreVideo(r.Context(), me(r), h.info(r), r.PathValue("id"), reason); err != nil {
		return err
	}
	return done(w)
}

func (h Admin) restoreUser(w http.ResponseWriter, r *http.Request) error {
	reason, err := reasonBody(r)
	if err != nil {
		return err
	}
	if err := h.Svc.RestoreUser(r.Context(), me(r), h.info(r), r.PathValue("id"), reason); err != nil {
		return err
	}
	return done(w)
}

func (h Admin) users(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Users(r.Context(), q.Get("q"), q.Get("status"), q.Get("cursor"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Admin) audit(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Audit(r.Context(), q.Get("action"), q.Get("actor"), q.Get("cursor"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Admin) hashes(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.BlockedHashes(r.Context())
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Admin) blockHash(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		SHA256 string `json:"sha256"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	out, err := h.Svc.BlockHash(r.Context(), me(r), h.info(r), in.SHA256, in.Reason)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (h Admin) unblockHash(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.UnblockHash(r.Context(), me(r), h.info(r), r.PathValue("sha256")); err != nil {
		return err
	}
	return done(w)
}

func (h Admin) setRole(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Role string `json:"role"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.SetRole(r.Context(), me(r), h.info(r), r.PathValue("id"), in.Role); err != nil {
		return err
	}
	return done(w)
}
