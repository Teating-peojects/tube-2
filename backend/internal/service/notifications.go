package service

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/pagination"
	"github.com/redxtube/api/internal/repository"
)

const (
	NotificationPageSize = 20
	maxReadIDs           = 100
)

// NotificationStore is implemented by repository.Notifications (and by fakes in tests).
type NotificationStore interface {
	List(ctx context.Context, userID string, after *repository.Keyset, unreadOnly bool, limit int) ([]repository.NotificationRow, error)
	Unread(ctx context.Context, userID string) (int, error)
	MarkRead(ctx context.Context, userID string, ids []int64, all bool) error
}

type Notifications struct {
	repo    NotificationStore
	limiter Limiter
	log     *slog.Logger
}

func NewNotifications(repo NotificationStore, limiter Limiter, log *slog.Logger) *Notifications {
	return &Notifications{repo: repo, limiter: limiter, log: log}
}

type notifCursor struct {
	T int64 `json:"t"`
	N int64 `json:"n"`
}

func name(p *string) string {
	if p == nil || strings.TrimSpace(*p) == "" {
		return "Someone"
	}
	return *p
}

func orEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// shape builds the text and link of one notice from live data. The wire types are the
// ones the frontend knows (see NotificationType in web/src/types).
func shapeNotification(n repository.NotificationRow) model.Notification {
	out := model.Notification{ID: strconv.FormatInt(n.ID, 10), CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339), Read: n.Read, Href: "/notifications"}
	who, video := name(n.ActorName), orEmpty(n.VideoTitle)
	watch := ""
	if n.VideoID != nil && IsUUID(*n.VideoID) {
		watch = "/watch/" + *n.VideoID
	}
	switch n.Type {
	case "NEW_SUBSCRIBER":
		out.Type, out.Title, out.Body = "new_subscriber", who+" subscribed to your channel", ""
		if n.ActorID != nil && IsUUID(*n.ActorID) {
			out.Href = "/channel/" + *n.ActorID
		}
	case "NEW_VIDEO":
		out.Type, out.Title, out.Body, out.Href = "creator_upload", who+" uploaded a new video", video, orDefault(watch, out.Href)
	case "COMMENT":
		out.Type, out.Title, out.Body, out.Href = "new_comment", who+" commented on your video", video, orDefault(watch, out.Href)
	case "COMMENT_REPLY":
		out.Type, out.Title, out.Body, out.Href = "comment_reply", who+" replied to your comment", video, orDefault(watch, out.Href)
	case "VIDEO_READY":
		out.Type, out.Title, out.Body, out.Href = "processing_completed", "Your video is ready", video, "/studio/videos"
	case "VIDEO_FAILED":
		out.Type, out.Title, out.Body, out.Href = "processing_failed", "We couldn't process your video", video, "/studio/videos"
	default: // MODERATION, SECURITY, SYSTEM
		out.Type = "system"
		out.Title, out.Body = clip(n.PayloadTitle, 120), clip(n.PayloadBody, 300)
		if out.Title == "" {
			out.Title = map[string]string{"MODERATION": "Update from moderation", "SECURITY": "Security notice"}[n.Type]
		}
		if out.Title == "" {
			out.Title = "Notice from REDXTUBE"
		}
	}
	return out
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func clip(s string, max int) string {
	if r := []rune(strings.TrimSpace(s)); len(r) > max {
		return string(r[:max])
	} else {
		return string(r)
	}
}

func (s *Notifications) List(ctx context.Context, p *auth.Principal, cursor string, unreadOnly bool) (model.Page[model.Notification], error) {
	var after *repository.Keyset
	if cursor != "" {
		var c notifCursor
		if pagination.Decode(cursor, &c) != nil || c.T <= 0 || c.N <= 0 {
			return model.Page[model.Notification]{}, badCursor()
		}
		after = &repository.Keyset{Time: time.UnixMicro(c.T).UTC(), Num: c.N}
	}
	rows, err := s.repo.List(ctx, p.UserID, after, unreadOnly, NotificationPageSize+1)
	if err != nil {
		return model.Page[model.Notification]{}, apperr.Internal(err)
	}
	page := model.Page[model.Notification]{Items: []model.Notification{}}
	if len(rows) > NotificationPageSize {
		rows = rows[:NotificationPageSize]
		last := rows[len(rows)-1]
		next, err := pagination.Encode(notifCursor{T: last.CreatedAt.UnixMicro(), N: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, shapeNotification(r))
	}
	return page, nil
}

func (s *Notifications) Unread(ctx context.Context, p *auth.Principal) (model.UnreadCount, error) {
	n, err := s.repo.Unread(ctx, p.UserID)
	if err != nil {
		return model.UnreadCount{}, apperr.Internal(err)
	}
	return model.UnreadCount{Count: n}, nil
}

// MarkRead marks the listed notices (or all) as read for the caller only.
func (s *Notifications) MarkRead(ctx context.Context, p *auth.Principal, ids []string, all bool) error {
	if !all && len(ids) == 0 {
		return apperr.Validation(map[string]string{"ids": "Send ids or all=true"})
	}
	if len(ids) > maxReadIDs {
		return apperr.Validation(map[string]string{"ids": "At most 100 ids"})
	}
	nums := make([]int64, 0, len(ids))
	for _, id := range ids {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 {
			return apperr.Validation(map[string]string{"ids": "Invalid id"})
		}
		nums = append(nums, n)
	}
	if err := s.repo.MarkRead(ctx, p.UserID, nums, all); err != nil {
		return apperr.Internal(err)
	}
	return nil
}
