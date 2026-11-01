package service

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/repository"
)

type fakeNotifs struct {
	rows       []repository.NotificationRow
	gotIDs     []int64
	gotAll     bool
	gotUnread  bool
	markCalled bool
}

func (f *fakeNotifs) List(_ context.Context, _ string, _ *repository.Keyset, unread bool, limit int) ([]repository.NotificationRow, error) {
	f.gotUnread = unread
	if len(f.rows) > limit {
		return f.rows[:limit], nil
	}
	return f.rows, nil
}
func (f *fakeNotifs) Unread(context.Context, string) (int, error) { return 3, nil }
func (f *fakeNotifs) MarkRead(_ context.Context, _ string, ids []int64, all bool) error {
	f.markCalled, f.gotIDs, f.gotAll = true, ids, all
	return nil
}

func sp(s string) *string { return &s }

func TestShapeNotification(t *testing.T) {
	vid := "22222222-2222-7222-8222-222222222222"
	act := "11111111-1111-7111-8111-111111111111"
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		row       repository.NotificationRow
		typ, href string
		titleHas  string
	}{
		{repository.NotificationRow{Type: "NEW_SUBSCRIBER", ActorID: &act, ActorName: sp("Ann")}, "new_subscriber", "/channel/" + act, "Ann subscribed"},
		{repository.NotificationRow{Type: "NEW_VIDEO", VideoID: &vid, VideoTitle: sp("T"), ActorName: sp("Ann")}, "creator_upload", "/watch/" + vid, "uploaded"},
		{repository.NotificationRow{Type: "COMMENT", VideoID: &vid}, "new_comment", "/watch/" + vid, "Someone commented"},
		{repository.NotificationRow{Type: "COMMENT_REPLY", VideoID: &vid}, "comment_reply", "/watch/" + vid, "replied"},
		{repository.NotificationRow{Type: "VIDEO_READY"}, "processing_completed", "/studio/videos", "ready"},
		{repository.NotificationRow{Type: "VIDEO_FAILED"}, "processing_failed", "/studio/videos", "couldn't"},
		{repository.NotificationRow{Type: "SYSTEM", PayloadTitle: "Hi"}, "system", "/notifications", "Hi"},
		{repository.NotificationRow{Type: "SECURITY"}, "system", "/notifications", "Security"},
	}
	for _, c := range cases {
		c.row.ID, c.row.CreatedAt = 7, at
		got := shapeNotification(c.row)
		if got.Type != c.typ || got.Href != c.href || !contains(got.Title, c.titleHas) || got.ID != "7" || got.CreatedAt != "2026-01-02T03:04:05Z" {
			t.Errorf("%s -> %+v", c.row.Type, got)
		}
	}
	// a malicious actor-supplied value can never become a link target
	bad := "javascript:alert(1)"
	if got := shapeNotification(repository.NotificationRow{Type: "NEW_SUBSCRIBER", ActorID: &bad}); got.Href != "/notifications" {
		t.Errorf("href %q", got.Href)
	}
}

func TestNotificationsListAndRead(t *testing.T) {
	f := &fakeNotifs{}
	for i := 0; i < NotificationPageSize+1; i++ {
		f.rows = append(f.rows, repository.NotificationRow{ID: int64(100 - i), Type: "SYSTEM", CreatedAt: time.Now()})
	}
	svc := NewNotifications(f, &okLimiter{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	page, err := svc.List(context.Background(), testUser, "", true)
	if err != nil || len(page.Items) != NotificationPageSize || page.NextCursor == nil || !f.gotUnread {
		t.Fatalf("page=%d next=%v err=%v unread=%v", len(page.Items), page.NextCursor, err, f.gotUnread)
	}
	if _, err := svc.List(context.Background(), testUser, *page.NextCursor, false); err != nil {
		t.Errorf("own cursor rejected: %v", err)
	}
	if _, err := svc.List(context.Background(), testUser, "garbage", false); ecode(t, err) != "invalid_cursor" {
		t.Errorf("garbage cursor: %v", err)
	}

	for name, args := range map[string]struct {
		ids []string
		all bool
	}{"nothing": {}, "not a number": {ids: []string{"abc"}}, "negative": {ids: []string{"-1"}}, "too many": {ids: make([]string, 101)}} {
		f.markCalled = false
		if err := svc.MarkRead(context.Background(), testUser, args.ids, args.all); ecode(t, err) != "validation_failed" || f.markCalled {
			t.Errorf("%s: %v called=%v", name, err, f.markCalled)
		}
	}
	if err := svc.MarkRead(context.Background(), testUser, []string{"5", "6"}, false); err != nil || len(f.gotIDs) != 2 {
		t.Errorf("mark: %v %v", err, f.gotIDs)
	}
	if err := svc.MarkRead(context.Background(), testUser, nil, true); err != nil || !f.gotAll {
		t.Errorf("mark all: %v", err)
	}
}

func TestSharesAddUpTo100(t *testing.T) {
	cases := [][]repository.Count{
		{{Label: "a", N: 1}, {Label: "b", N: 1}, {Label: "c", N: 1}},
		{{Label: "a", N: 2}, {Label: "b", N: 1}},
		{{Label: "BD", N: 97}, {Label: "US", N: 2}, {Label: "", N: 1}},
		{{Label: "x", N: 5}},
	}
	for i, rows := range cases {
		sum := 0
		for _, s := range shares(rows, title) {
			sum += s.Value
		}
		if sum != 100 {
			t.Errorf("case %d sums to %d", i, sum)
		}
	}
	if got := shares(nil, title); got == nil || len(got) != 0 {
		t.Errorf("empty must be an empty list, got %v", got)
	}
	if title("MOBILE") != "Mobile" || title("") != "Other" || title("TRAFFIC_X") != "Traffic x" || country("") != "Unknown" {
		t.Error("labels")
	}
}

func TestPeriodAndFilterValidation(t *testing.T) {
	for in, want := range map[string]int{"": 28, "7d": 7, "28d": 28, "90d": 90} {
		if _, d, err := periodDays(in); err != nil || d != want {
			t.Errorf("%q -> %d %v", in, d, err)
		}
	}
	if _, _, err := periodDays("1y"); ecode(t, err) != "validation_failed" {
		t.Error("period 1y accepted")
	}
	svc := NewStudio(nil, &okLimiter{}, []byte("k"), media.New(""), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := svc.Videos(context.Background(), testUser, "everything", ""); ecode(t, err) != "validation_failed" {
		t.Error("filter accepted")
	}
	if _, err := svc.Comments(context.Background(), testUser, "spam", ""); ecode(t, err) != "validation_failed" {
		t.Error("comment filter accepted")
	}
	if _, err := svc.Videos(context.Background(), testUser, "all", "!!"); ecode(t, err) != "invalid_cursor" {
		t.Error("bad cursor accepted")
	}
	if err := svc.SetVisibility(context.Background(), testUser, testCI, "not-uuid", "public"); ecode(t, err) != "video_not_found" {
		t.Error("bad id")
	}
	if err := svc.SetVisibility(context.Background(), testUser, testCI, vidID, "friends"); ecode(t, err) != "validation_failed" {
		t.Error("bad visibility")
	}
}

func TestStudioStatusMapping(t *testing.T) {
	for in, want := range map[string]string{"PUBLISHED": "published", "READY": "draft", "FAILED": "failed", "PROCESSING": "processing", "UPLOADING": "processing"} {
		if got := studioStatus(in); got != want {
			t.Errorf("%s -> %s", in, got)
		}
	}
}
