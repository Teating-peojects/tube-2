package handler

import (
	"context"
	"testing"

	"github.com/redxtube/api/internal/realtime"
)

type ownerStub struct{ owns bool }

func (o ownerStub) OwnsVideo(context.Context, string, string) (bool, error) { return o.owns, nil }

func TestRealtimeTopicAuthorization(t *testing.T) {
	vid := "22222222-2222-7222-8222-222222222222"
	mine, theirs := Realtime{Owner: ownerStub{true}}, Realtime{Owner: ownerStub{false}}
	if !mine.allowed(context.Background(), "u", realtime.TopicNotifications) {
		t.Error("own notifications must be allowed")
	}
	if !mine.allowed(context.Background(), "u", realtime.TopicVideoPrefix+vid) {
		t.Error("owner may follow their video")
	}
	for _, topic := range []string{realtime.TopicVideoPrefix + vid, realtime.TopicVideoPrefix + "x", "video:", "admin", "notifications:other", ""} {
		if theirs.allowed(context.Background(), "u", topic) {
			t.Errorf("topic %q must be refused", topic)
		}
	}
}
