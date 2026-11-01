package handler

import (
	"net/http"
	"strconv"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/service"
)

// Catalog exposes the read-only discovery endpoints. Handlers only parse
// input and write output; rules live in the service.
type Catalog struct{ Svc *service.Catalog }

// Gate wraps a handler with a precondition (the age gate). nil means open.
type Gate func(http.HandlerFunc) http.HandlerFunc

// Routes registers the discovery API. Everything that serves content goes
// through gate (age verification); report reasons and the sitemap (ids only)
// stay open.
func (h Catalog) Routes(mux *http.ServeMux, gate Gate) {
	const v = "/api/v1"
	g := func(f httpx.HandlerFunc) http.HandlerFunc {
		if gate == nil {
			return httpx.Handle(f)
		}
		return gate(httpx.Handle(f))
	}
	mux.HandleFunc("GET "+v+"/videos", g(h.videos))
	mux.HandleFunc("GET "+v+"/videos/{id}", g(h.video))
	mux.HandleFunc("GET "+v+"/shorts", g(h.shorts))
	mux.HandleFunc("GET "+v+"/live", g(h.live))
	mux.HandleFunc("GET "+v+"/trending", g(h.trending))
	mux.HandleFunc("GET "+v+"/search", g(h.search))
	mux.HandleFunc("GET "+v+"/search/suggestions", g(h.suggestions))
	mux.HandleFunc("GET "+v+"/channels/popular", g(h.popular))
	mux.HandleFunc("GET "+v+"/channels/{id}", g(h.channel))
	mux.HandleFunc("GET "+v+"/channels/{id}/videos", g(h.channelVideos))
	mux.HandleFunc("GET "+v+"/channels/{id}/playlists", g(h.channelPlaylists))
	mux.HandleFunc("GET "+v+"/reports/reasons", httpx.Handle(h.reasons))
	mux.HandleFunc("GET "+v+"/sitemap/videos", httpx.Handle(h.sitemap))
}

func (h Catalog) videos(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	page, err := h.Svc.Videos(r.Context(), q.Get("category"), q.Get("cursor"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}

func (h Catalog) video(w http.ResponseWriter, r *http.Request) error {
	var viewer *string // signed-in owners may open their own PRIVATE videos
	if p := auth.FromContext(r.Context()); p != nil {
		viewer = &p.UserID
	}
	d, err := h.Svc.VideoDetail(r.Context(), r.PathValue("id"), viewer)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, d)
	return nil
}

func (h Catalog) shorts(w http.ResponseWriter, r *http.Request) error {
	page, err := h.Svc.Shorts(r.Context(), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}

func (h Catalog) live(w http.ResponseWriter, r *http.Request) error {
	page, err := h.Svc.Live(r.Context(), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}

func (h Catalog) trending(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	page, err := h.Svc.Trending(r.Context(), q.Get("tab"), q.Get("cursor"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}

func (h Catalog) search(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	page, err := h.Svc.Search(r.Context(), service.SearchParams{
		Q: q.Get("q"), Sort: q.Get("sort"), Type: q.Get("type"), Duration: q.Get("duration"), Date: q.Get("date"),
	}, q.Get("cursor"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}

func (h Catalog) suggestions(w http.ResponseWriter, r *http.Request) error {
	s, err := h.Svc.Suggest(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string][]string{"suggestions": s})
	return nil
}

func (h Catalog) popular(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.PopularCreators(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h Catalog) channel(w http.ResponseWriter, r *http.Request) error {
	d, err := h.Svc.Channel(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, d)
	return nil
}

func (h Catalog) channelVideos(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	page, err := h.Svc.ChannelVideos(r.Context(), r.PathValue("id"), q.Get("kind"), q.Get("sort"), q.Get("cursor"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}

func (h Catalog) channelPlaylists(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.ChannelPlaylists(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h Catalog) reasons(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httpx.JSON(w, http.StatusOK, h.Svc.ReportReasons())
	return nil
}

func (h Catalog) sitemap(w http.ResponseWriter, r *http.Request) error {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.Svc.Sitemap(r.Context(), limit)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.JSON(w, http.StatusOK, out)
	return nil
}
