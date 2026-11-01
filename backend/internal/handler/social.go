package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/service"
)

// Social serves reactions, subscriptions, comments, views/history, watch later,
// playlists, blocking and reports.
//
// Access rules:
//   - anything that changes public counts or content (reactions, subscribing,
//     commenting, playlists) needs a signed-in user with a verified email;
//   - personal data (history, watch later, blocking, reports) needs a session;
//   - endpoints that return videos or comments also sit behind the age gate.
type Social struct {
	Svc        *service.Social
	TrustProxy bool
}

func (h Social) info(r *http.Request) service.ClientInfo {
	return service.ClientInfo{IP: auth.ClientIP(r, h.TrustProxy), UserAgent: r.UserAgent(), RequestID: httpx.RequestIDFrom(r.Context()), Country: r.Header.Get("CF-IPCountry")}
}

func me(r *http.Request) *auth.Principal { return auth.FromContext(r.Context()) }

func (h Social) Routes(mux *http.ServeMux, gate Gate) {
	const v = "/api/v1"
	age := func(f http.HandlerFunc) http.HandlerFunc {
		if gate == nil {
			return f
		}
		return gate(f)
	}
	auth_ := func(f httpx.HandlerFunc) http.HandlerFunc { return middleware.RequireAuth(httpx.Handle(f)) }
	verified := func(f httpx.HandlerFunc) http.HandlerFunc { return middleware.RequireVerified(httpx.Handle(f)) }
	open := func(f httpx.HandlerFunc) http.HandlerFunc { return httpx.Handle(f) }

	mux.HandleFunc("PUT "+v+"/videos/{id}/reaction", verified(h.reaction))
	mux.HandleFunc("PUT "+v+"/channels/{id}/subscription", verified(h.subscription))
	mux.HandleFunc("GET "+v+"/subscriptions/channels", age(auth_(h.subscribedChannels)))
	mux.HandleFunc("GET "+v+"/subscriptions/feed", age(auth_(h.subscriptionFeed)))

	mux.HandleFunc("GET "+v+"/videos/{id}/comments", age(open(h.comments)))
	mux.HandleFunc("POST "+v+"/videos/{id}/comments", verified(h.createComment))
	mux.HandleFunc("GET "+v+"/comments/{id}/replies", age(open(h.replies)))
	mux.HandleFunc("PUT "+v+"/comments/{id}/like", verified(h.likeComment))
	mux.HandleFunc("PUT "+v+"/comments/{id}/pin", verified(h.pinComment))
	mux.HandleFunc("DELETE "+v+"/comments/{id}", auth_(h.deleteComment))

	mux.HandleFunc("POST "+v+"/videos/{id}/view", age(open(h.view)))
	mux.HandleFunc("POST "+v+"/videos/{id}/progress", auth_(h.progress))
	mux.HandleFunc("GET "+v+"/history", age(auth_(h.history)))
	mux.HandleFunc("DELETE "+v+"/history", auth_(h.clearHistory))
	mux.HandleFunc("DELETE "+v+"/history/{videoId}", auth_(h.deleteHistoryItem))

	mux.HandleFunc("GET "+v+"/watch-later", age(auth_(h.watchLater)))
	mux.HandleFunc("GET "+v+"/watch-later/ids", auth_(h.watchLaterIDs))
	mux.HandleFunc("PUT "+v+"/watch-later/{videoId}", auth_(h.addWatchLater))
	mux.HandleFunc("DELETE "+v+"/watch-later/{videoId}", auth_(h.removeWatchLater))
	mux.HandleFunc("GET "+v+"/liked", age(auth_(h.liked)))

	mux.HandleFunc("GET "+v+"/playlists", age(auth_(h.myPlaylists)))
	mux.HandleFunc("POST "+v+"/playlists", verified(h.createPlaylist))
	mux.HandleFunc("GET "+v+"/playlists/{id}", age(open(h.playlist)))
	mux.HandleFunc("PATCH "+v+"/playlists/{id}", verified(h.updatePlaylist))
	mux.HandleFunc("DELETE "+v+"/playlists/{id}", verified(h.deletePlaylist))
	mux.HandleFunc("PUT "+v+"/playlists/{id}/videos/{videoId}", verified(h.addPlaylistVideo))
	mux.HandleFunc("DELETE "+v+"/playlists/{id}/videos/{videoId}", verified(h.removePlaylistVideo))
	mux.HandleFunc("PUT "+v+"/playlists/{id}/order", verified(h.reorderPlaylist))

	mux.HandleFunc("POST "+v+"/videos/{id}/not-interested", auth_(h.notInterested))
	mux.HandleFunc("PUT "+v+"/channels/{id}/block", auth_(h.block))
	mux.HandleFunc("DELETE "+v+"/channels/{id}/block", auth_(h.unblock))
	mux.HandleFunc("POST "+v+"/reports", auth_(h.report))
}

func ok(w http.ResponseWriter, v any) error { httpx.JSON(w, http.StatusOK, v); return nil }

func done(w http.ResponseWriter) error { httpx.NoContent(w); return nil }

// ---------- reactions & subscriptions ----------

func (h Social) reaction(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Reaction *string `json:"reaction"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.SetReaction(r.Context(), me(r), r.PathValue("id"), in.Reaction); err != nil {
		return err
	}
	return done(w)
}

func (h Social) subscription(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Subscribed *bool `json:"subscribed"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.SetSubscription(r.Context(), me(r), r.PathValue("id"), in.Subscribed); err != nil {
		return err
	}
	return done(w)
}

func (h Social) subscribedChannels(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.SubscribedChannels(r.Context(), me(r))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) subscriptionFeed(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.SubscriptionFeed(r.Context(), me(r), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

// ---------- comments ----------

func (h Social) comments(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Comments(r.Context(), r.PathValue("id"), q.Get("sort"), q.Get("cursor"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) createComment(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Text     string  `json:"text"`
		ParentID *string `json:"parentId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	c, err := h.Svc.CreateComment(r.Context(), me(r), r.PathValue("id"), in.Text, in.ParentID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

func (h Social) replies(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Replies(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) likeComment(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Liked *bool `json:"liked"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.LikeComment(r.Context(), me(r), r.PathValue("id"), in.Liked); err != nil {
		return err
	}
	return done(w)
}

func (h Social) pinComment(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Pinned *bool `json:"pinned"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.PinComment(r.Context(), me(r), r.PathValue("id"), in.Pinned); err != nil {
		return err
	}
	return done(w)
}

func (h Social) deleteComment(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.DeleteComment(r.Context(), me(r), r.PathValue("id"), h.info(r)); err != nil {
		return err
	}
	return done(w)
}

// ---------- views & history ----------

func (h Social) view(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Source string `json:"source"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &in); err != nil {
			return err
		}
	}
	if err := h.Svc.RecordView(r.Context(), me(r), r.PathValue("id"), in.Source, h.info(r)); err != nil {
		return err
	}
	return done(w)
}

func (h Social) progress(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		PositionSec int `json:"positionSec"`
		WatchedSec  int `json:"watchedSec"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Progress(r.Context(), me(r), r.PathValue("id"), in.PositionSec, in.WatchedSec); err != nil {
		return err
	}
	return done(w)
}

func (h Social) history(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.History(r.Context(), me(r), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) clearHistory(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.ClearHistory(r.Context(), me(r)); err != nil {
		return err
	}
	return done(w)
}

func (h Social) deleteHistoryItem(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.DeleteHistoryItem(r.Context(), me(r), r.PathValue("videoId")); err != nil {
		return err
	}
	return done(w)
}

// ---------- watch later & liked ----------

func (h Social) watchLater(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.WatchLater(r.Context(), me(r), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) watchLaterIDs(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.WatchLaterIDs(r.Context(), me(r))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) addWatchLater(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.AddWatchLater(r.Context(), me(r), r.PathValue("videoId")); err != nil {
		return err
	}
	return done(w)
}

func (h Social) removeWatchLater(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.RemoveWatchLater(r.Context(), me(r), r.PathValue("videoId")); err != nil {
		return err
	}
	return done(w)
}

func (h Social) liked(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Liked(r.Context(), me(r), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

// ---------- playlists ----------

func (h Social) myPlaylists(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.MyPlaylists(r.Context(), me(r), r.URL.Query().Get("videoId"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) createPlaylist(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Title      string  `json:"title"`
		Visibility string  `json:"visibility"`
		VideoID    *string `json:"videoId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if in.Visibility == "" {
		in.Visibility = "private"
	}
	p, err := h.Svc.CreatePlaylist(r.Context(), me(r), in.Title, in.Visibility, in.VideoID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

func (h Social) playlist(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.GetPlaylist(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, out)
}

func (h Social) updatePlaylist(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Visibility  *string `json:"visibility"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.UpdatePlaylist(r.Context(), me(r), r.PathValue("id"), service.PlaylistPatch{Title: in.Title, Description: in.Description, Visibility: in.Visibility}); err != nil {
		return err
	}
	return done(w)
}

func (h Social) deletePlaylist(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.DeletePlaylist(r.Context(), me(r), r.PathValue("id")); err != nil {
		return err
	}
	return done(w)
}

func (h Social) addPlaylistVideo(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.AddPlaylistVideo(r.Context(), me(r), r.PathValue("id"), r.PathValue("videoId")); err != nil {
		return err
	}
	return done(w)
}

func (h Social) removePlaylistVideo(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.RemovePlaylistVideo(r.Context(), me(r), r.PathValue("id"), r.PathValue("videoId")); err != nil {
		return err
	}
	return done(w)
}

func (h Social) reorderPlaylist(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		VideoIDs []string `json:"videoIds"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.ReorderPlaylist(r.Context(), me(r), r.PathValue("id"), in.VideoIDs); err != nil {
		return err
	}
	return done(w)
}

// ---------- safety ----------

func (h Social) notInterested(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.NotInterested(r.Context(), me(r), r.PathValue("id")); err != nil {
		return err
	}
	return done(w)
}

func (h Social) block(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.SetBlock(r.Context(), me(r), r.PathValue("id"), true); err != nil {
		return err
	}
	return done(w)
}

func (h Social) unblock(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.SetBlock(r.Context(), me(r), r.PathValue("id"), false); err != nil {
		return err
	}
	return done(w)
}

func (h Social) report(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Reason     string `json:"reason"`
		Details    string `json:"details"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Report(r.Context(), me(r), service.ReportInput{TargetType: in.TargetType, TargetID: in.TargetID, Reason: in.Reason, Details: in.Details}); err != nil {
		return err
	}
	return done(w)
}
