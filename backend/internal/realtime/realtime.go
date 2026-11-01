// Package realtime connects API instances to browsers' WebSockets. Any instance (or the
// media worker) publishes "something changed for user X on topic T" to Redis; every
// instance receives it and hands it to the sockets it holds for that user.
//
// Events carry no sensitive content on purpose: the browser reacts by re-fetching the
// real data through the normal, authorised API.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redxtube/api/internal/ws"
)

const channel = "redx:rt"

// Topics a client may subscribe to.
const (
	TopicNotifications = "notifications"
	TopicVideoPrefix   = "video:" // video:<id>, owner only
)

type event struct {
	User  string          `json:"u"`
	Topic string          `json:"t"`
	Data  json.RawMessage `json:"d,omitempty"`
}

// Bus publishes events through Redis.
type Bus struct {
	rdb *redis.Client
	log *slog.Logger
}

func NewBus(rdb *redis.Client, log *slog.Logger) *Bus { return &Bus{rdb: rdb, log: log} }

// Publish never fails the caller's request: realtime is a convenience, polling still works.
func (b *Bus) Publish(ctx context.Context, userID, topic string, data json.RawMessage) {
	msg, err := json.Marshal(event{User: userID, Topic: topic, Data: data})
	if err != nil {
		return
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := b.rdb.Publish(pctx, channel, msg).Err(); err != nil {
		b.log.WarnContext(ctx, "realtime publish failed", "err", err.Error())
	}
}

// Notify tells a user's open sockets that their notifications changed (service.Notifier).
func (b *Bus) Notify(ctx context.Context, userID string) {
	b.Publish(ctx, userID, TopicNotifications, nil)
}

// Run delivers published events to the local hub until ctx ends. It reconnects after errors.
func (b *Bus) Run(ctx context.Context, hub *ws.Hub) {
	for ctx.Err() == nil {
		b.runOnce(ctx, hub)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *Bus) runOnce(ctx context.Context, hub *ws.Hub) {
	sub := b.rdb.Subscribe(ctx, channel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			var e event
			if json.Unmarshal([]byte(m.Payload), &e) != nil || e.User == "" || e.Topic == "" {
				continue
			}
			hub.Deliver(e.User, e.Topic, e.Data)
		}
	}
}
