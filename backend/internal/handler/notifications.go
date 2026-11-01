package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// Notifications serves the signed-in user's inbox.
type Notifications struct{ Svc *service.Notifications }

func (h Notifications) Routes(mux *http.ServeMux) {
	const v = "/api/v1/notifications"
	auth_ := func(f httpx.HandlerFunc) http.HandlerFunc { return middleware.RequireAuth(httpx.Handle(f)) }
	mux.HandleFunc("GET "+v, auth_(h.list))
	mux.HandleFunc("GET "+v+"/unread-count", auth_(h.unread))
	mux.HandleFunc("POST "+v+"/read", auth_(h.read))
}

func (h Notifications) list(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.List(r.Context(), me(r), r.URL.Query().Get("cursor"), r.URL.Query().Get("unread") == "1")
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h Notifications) unread(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Unread(r.Context(), me(r))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h Notifications) read(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.MarkRead(r.Context(), me(r), in.IDs, in.All); err != nil {
		return err
	}
	return done(w)
}
