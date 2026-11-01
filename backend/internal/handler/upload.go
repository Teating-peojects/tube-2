package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// Upload serves the creator upload flow. Every route needs a signed-in user with
// a verified email. The video bytes never reach the API: see service.Upload.
type Upload struct {
	Svc        *service.Upload
	TrustProxy bool
}

func (h Upload) info(r *http.Request) service.ClientInfo {
	return service.ClientInfo{IP: auth.ClientIP(r, h.TrustProxy), UserAgent: r.UserAgent(), RequestID: httpx.RequestIDFrom(r.Context()), Country: r.Header.Get("CF-IPCountry")}
}

func (h Upload) Routes(mux *http.ServeMux) {
	const v = "/api/v1"
	verified := func(f httpx.HandlerFunc) http.HandlerFunc { return middleware.RequireVerified(httpx.Handle(f)) }

	mux.HandleFunc("POST "+v+"/upload/init", verified(h.init))
	mux.HandleFunc("POST "+v+"/upload/{id}/complete", verified(h.complete))
	mux.HandleFunc("DELETE "+v+"/upload/{id}", verified(h.abort))
	mux.HandleFunc("GET "+v+"/upload/{id}/status", verified(h.status))
	mux.HandleFunc("POST "+v+"/upload/{id}/assets", verified(h.assets))
	mux.HandleFunc("PATCH "+v+"/videos/{id}/metadata", verified(h.saveDraft))
	mux.HandleFunc("POST "+v+"/videos/{id}/publish", verified(h.publish))
}

func (h Upload) init(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Filename    string `json:"filename"`
		Size        int64  `json:"size"`
		ContentType string `json:"contentType"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	out, err := h.Svc.Init(r.Context(), me(r), h.info(r), service.InitInput{Filename: in.Filename, Size: in.Size, ContentType: in.ContentType})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (h Upload) complete(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Parts []service.PartIn `json:"parts"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Complete(r.Context(), me(r), h.info(r), r.PathValue("id"), in.Parts); err != nil {
		return err
	}
	return done(w)
}

func (h Upload) abort(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Abort(r.Context(), me(r), h.info(r), r.PathValue("id")); err != nil {
		return err
	}
	return done(w)
}

func (h Upload) status(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Status(r.Context(), me(r), r.PathValue("id"))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h Upload) assets(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Kind        string `json:"kind"`
		ContentType string `json:"contentType"`
		Size        int64  `json:"size"`
		Lang        string `json:"lang"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	out, err := h.Svc.Asset(r.Context(), me(r), h.info(r), r.PathValue("id"), service.AssetInput{Kind: in.Kind, ContentType: in.ContentType, Size: in.Size, Lang: in.Lang})
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (h Upload) saveDraft(w http.ResponseWriter, r *http.Request) error {
	var in service.MetaInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.SaveDraft(r.Context(), me(r), h.info(r), r.PathValue("id"), in); err != nil {
		return err
	}
	return done(w)
}

func (h Upload) publish(w http.ResponseWriter, r *http.Request) error {
	var in service.MetaInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Publish(r.Context(), me(r), h.info(r), r.PathValue("id"), in); err != nil {
		return err
	}
	return done(w)
}
