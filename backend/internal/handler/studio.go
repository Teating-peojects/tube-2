package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// Studio is the creator dashboard. Everything is scoped to the caller's own videos.
type Studio struct {
	Svc        *service.Studio
	TrustProxy bool
}

func (h Studio) info(r *http.Request) service.ClientInfo {
	return service.ClientInfo{IP: auth.ClientIP(r, h.TrustProxy), UserAgent: r.UserAgent(), RequestID: httpx.RequestIDFrom(r.Context())}
}

func (h Studio) Routes(mux *http.ServeMux) {
	const v = "/api/v1/studio"
	verified := func(f httpx.HandlerFunc) http.HandlerFunc { return middleware.RequireVerified(httpx.Handle(f)) }
	mux.HandleFunc("GET "+v+"/overview", verified(h.overview))
	mux.HandleFunc("GET "+v+"/analytics", verified(h.analytics))
	mux.HandleFunc("GET "+v+"/videos", verified(h.videos))
	mux.HandleFunc("PATCH "+v+"/videos/{id}", verified(h.setVisibility))
	mux.HandleFunc("DELETE "+v+"/videos/{id}", verified(h.deleteVideo))
	mux.HandleFunc("GET "+v+"/comments", verified(h.comments))
	mux.HandleFunc("POST "+v+"/comments/{id}/approve", verified(h.approve))
}

func private(w http.ResponseWriter, v any) error {
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, v)
	return nil
}

func (h Studio) overview(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Overview(r.Context(), me(r), r.URL.Query().Get("period"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Studio) analytics(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Analytics(r.Context(), me(r), r.URL.Query().Get("period"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Studio) videos(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Videos(r.Context(), me(r), q.Get("filter"), q.Get("cursor"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Studio) setVisibility(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Visibility string `json:"visibility"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.SetVisibility(r.Context(), me(r), h.info(r), r.PathValue("id"), in.Visibility); err != nil {
		return err
	}
	return done(w)
}

func (h Studio) deleteVideo(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.DeleteVideo(r.Context(), me(r), h.info(r), r.PathValue("id")); err != nil {
		return err
	}
	return done(w)
}

func (h Studio) comments(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Comments(r.Context(), me(r), q.Get("filter"), q.Get("cursor"))
	if err != nil {
		return err
	}
	return private(w, out)
}

func (h Studio) approve(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.ApproveComment(r.Context(), me(r), h.info(r), r.PathValue("id")); err != nil {
		return err
	}
	return done(w)
}
