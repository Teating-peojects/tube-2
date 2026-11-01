package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/realtime"
	"github.com/redxtube/api/internal/service"
	"github.com/redxtube/api/internal/ws"
)

// VideoOwner answers whether a user owns a video (gate for the video:<id> topic).
type VideoOwner interface {
	OwnsVideo(ctx context.Context, userID, videoID string) (bool, error)
}

// Realtime upgrades authenticated requests to a WebSocket. The socket only carries
// "something changed" pings; the browser re-fetches the data through the normal API.
// Protocol: client sends {"action":"subscribe"|"unsubscribe","topic":"..."}; server sends {"topic","data"}.
type Realtime struct {
	Hub     *ws.Hub
	Owner   VideoOwner
	Origins []string
	Log     *slog.Logger
}

func (h Realtime) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ws", middleware.RequireAuth(h.serve))
}

type clientMsg struct {
	Action string `json:"action"`
	Topic  string `json:"topic"`
}

func (h Realtime) allowed(ctx context.Context, userID, topic string) bool {
	switch {
	case topic == realtime.TopicNotifications:
		return true
	case strings.HasPrefix(topic, realtime.TopicVideoPrefix):
		id := strings.TrimPrefix(topic, realtime.TopicVideoPrefix)
		if !service.IsUUID(id) {
			return false
		}
		ok, err := h.Owner.OwnsVideo(ctx, userID, id)
		return err == nil && ok
	}
	return false
}

func (h Realtime) serve(w http.ResponseWriter, r *http.Request) {
	p := me(r)
	conn, err := ws.Upgrade(w, r, h.Origins, ws.Options{})
	if err != nil {
		return // Upgrade already answered the HTTP request
	}
	c := ws.NewClient(p.UserID, conn)
	if err := h.Hub.Add(c); err != nil {
		if errors.Is(err, ws.ErrTooManyConns) {
			conn.CloseWith(ws.ClosePolicy)
		} else {
			conn.Close()
		}
		return
	}
	defer func() { h.Hub.Remove(c); c.Close() }()
	go c.WriteLoop(30 * time.Second)

	ctx := context.WithoutCancel(r.Context()) // r.Context() ends after hijack; the loop runs until the socket closes
	for {
		raw, err := conn.ReadText()
		if err != nil {
			return
		}
		var m clientMsg
		if json.Unmarshal(raw, &m) != nil {
			conn.CloseWith(ws.CloseProtocol)
			return
		}
		switch m.Action {
		case "subscribe":
			vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			ok := h.allowed(vctx, p.UserID, m.Topic)
			cancel()
			if !ok || !c.Subscribe(m.Topic) {
				continue // silently ignored: a client learns nothing about other people's videos
			}
		case "unsubscribe":
			c.Unsubscribe(m.Topic)
		}
	}
}
