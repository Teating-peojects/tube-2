package service

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/pagination"
	"github.com/redxtube/api/internal/repository"
)

const StudioPageSize = 20

// StudioStore is implemented by repository.Studio (and by fakes in tests).
type StudioStore interface {
	Overview(ctx context.Context, creatorID string, days int) (repository.StudioOverviewRows, error)
	Analytics(ctx context.Context, creatorID string, days int) (repository.AnalyticsRows, error)
	Videos(ctx context.Context, creatorID, filter string, after *repository.Keyset, limit int) ([]repository.StudioVideoRow, error)
	SetVisibility(ctx context.Context, creatorID, videoID, visibility string, a repository.AuditEntry) (bool, error)
	DeleteVideo(ctx context.Context, creatorID, videoID string, a repository.AuditEntry) (bool, error)
	Comments(ctx context.Context, creatorID, filter string, after *repository.Keyset, limit int) ([]repository.StudioCommentRow, error)
	ApproveComment(ctx context.Context, creatorID, commentID string, a repository.AuditEntry) (bool, error)
}

type Studio struct {
	repo    StudioStore
	limiter Limiter
	ipKey   []byte
	urls    media.URLs
	log     *slog.Logger
}

func NewStudio(repo StudioStore, limiter Limiter, ipKey []byte, urls media.URLs, log *slog.Logger) *Studio {
	return &Studio{repo: repo, limiter: limiter, ipKey: ipKey, urls: urls, log: log}
}

var errNoVideo = apperr.NotFound("video_not_found", "Video not found")

func periodDays(p string) (string, int, error) {
	switch p {
	case "", "28d":
		return "28d", 28, nil
	case "7d":
		return "7d", 7, nil
	case "90d":
		return "90d", 90, nil
	}
	return "", 0, apperr.Validation(map[string]string{"period": "must be 7d, 28d or 90d"})
}

func points(days []repository.DayPoint, f func(repository.DayPoint) float64) []model.SeriesPoint {
	out := make([]model.SeriesPoint, 0, len(days))
	for _, d := range days {
		out = append(out, model.SeriesPoint{Date: d.Date, Value: f(d)})
	}
	return out
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// shares turns counts into whole percentages that add up to 100 (largest remainder).
func shares(rows []repository.Count, label func(string) string) []model.Stat {
	var total int64
	for _, r := range rows {
		total += r.N
	}
	out := make([]model.Stat, 0, len(rows))
	if total == 0 {
		return out
	}
	type rem struct {
		i int
		f float64
	}
	sum, rems := 0, make([]rem, 0, len(rows))
	for i, r := range rows {
		exact := float64(r.N) * 100 / float64(total)
		w := int(exact)
		sum += w
		out = append(out, model.Stat{Label: label(r.Label), Value: w})
		rems = append(rems, rem{i, exact - float64(w)})
	}
	for left := 100 - sum; left > 0; left-- {
		best := 0
		for i := range rems {
			if rems[i].f > rems[best].f {
				best = i
			}
		}
		out[rems[best].i].Value++
		rems[best].f = -1
	}
	return out
}

func title(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", " "))
	if s == "" {
		return "Other"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func country(s string) string {
	if s == "" {
		return "Unknown"
	}
	return s
}

func (s *Studio) Overview(ctx context.Context, p *auth.Principal, period string) (model.StudioOverview, error) {
	label, days, err := periodDays(period)
	if err != nil {
		return model.StudioOverview{}, err
	}
	o, err := s.repo.Overview(ctx, p.UserID, days)
	if err != nil {
		return model.StudioOverview{}, apperr.Internal(err)
	}
	var views, secs int64
	for _, d := range o.Days {
		views += d.Views
		secs += d.Secs
	}
	top := make([]model.StudioTopVideo, 0, len(o.Top))
	for _, t := range o.Top {
		top = append(top, model.StudioTopVideo{ID: t.ID, Title: t.Title, Views: t.Views})
	}
	return model.StudioOverview{
		Period: label,
		Totals: model.StudioTotals{Views: views, WatchTimeHours: round1(float64(secs) / 3600), Subscribers: o.Subscribers, SubscribersDelta: o.SubsInPeriod},
		Views:  points(o.Days, func(d repository.DayPoint) float64 { return float64(d.Views) }), TopVideos: top,
	}, nil
}

func (s *Studio) Analytics(ctx context.Context, p *auth.Principal, period string) (model.StudioAnalytics, error) {
	label, days, err := periodDays(period)
	if err != nil {
		return model.StudioAnalytics{}, err
	}
	a, err := s.repo.Analytics(ctx, p.UserID, days)
	if err != nil {
		return model.StudioAnalytics{}, apperr.Internal(err)
	}
	return model.StudioAnalytics{
		Period:    label,
		Views:     points(a.Days, func(d repository.DayPoint) float64 { return float64(d.Views) }),
		WatchTime: points(a.Days, func(d repository.DayPoint) float64 { return round1(float64(d.Secs) / 3600) }),
		Audience:  model.StudioAudience{Countries: shares(a.Countries, country), Devices: shares(a.Devices, title)},
		Traffic:   shares(a.Traffic, title),
		// there is no share feature yet, so shares is honestly zero
		Engagement: model.StudioEngagement{Likes: a.Likes, Comments: a.Comments, AvgViewDurationSec: a.AvgSecs},
	}, nil
}

var studioFilters = map[string]bool{"all": true, "published": true, "draft": true, "processing": true, "private": true, "unlisted": true}

func studioStatus(s string) string {
	switch s {
	case "PUBLISHED":
		return "published"
	case "READY":
		return "draft"
	case "FAILED":
		return "failed"
	}
	return "processing"
}

type pageCursor struct {
	T int64  `json:"t"`
	I string `json:"i"`
}

func decodePage(cursor string) (*repository.Keyset, error) {
	if cursor == "" {
		return nil, nil
	}
	var c pageCursor
	if pagination.Decode(cursor, &c) != nil || c.T <= 0 || !IsUUID(c.I) {
		return nil, badCursor()
	}
	return &repository.Keyset{Time: time.UnixMicro(c.T).UTC(), ID: c.I}, nil
}

func (s *Studio) Videos(ctx context.Context, p *auth.Principal, filter, cursor string) (model.Page[model.StudioVideo], error) {
	if filter == "" {
		filter = "all"
	}
	if !studioFilters[filter] {
		return model.Page[model.StudioVideo]{}, apperr.Validation(map[string]string{"filter": "Unknown filter"})
	}
	after, err := decodePage(cursor)
	if err != nil {
		return model.Page[model.StudioVideo]{}, err
	}
	rows, err := s.repo.Videos(ctx, p.UserID, filter, after, StudioPageSize+1)
	if err != nil {
		return model.Page[model.StudioVideo]{}, apperr.Internal(err)
	}
	page := model.Page[model.StudioVideo]{Items: []model.StudioVideo{}}
	if len(rows) > StudioPageSize {
		rows = rows[:StudioPageSize]
		last := rows[len(rows)-1]
		next, err := pagination.Encode(pageCursor{T: last.CreatedAt.UnixMicro(), I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, model.StudioVideo{ID: r.ID, Title: r.Title, ThumbnailURL: s.urls.Asset(r.ThumbKey), DurationSec: r.DurationSec,
			Status: studioStatus(r.Status), Visibility: strings.ToLower(r.Visibility), CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
			Views: r.Views, Comments: r.Comments, Likes: r.Likes})
	}
	return page, nil
}

func (s *Studio) audit(p *auth.Principal, ci ClientInfo, action, target, id string, meta map[string]any) repository.AuditEntry {
	return repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: action, TargetType: &target, TargetID: &id,
		IPHash: auth.HashIP(s.ipKey, ci.IP), RequestID: ci.RequestID, Meta: meta}
}

func (s *Studio) SetVisibility(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID, visibility string) error {
	if !IsUUID(videoID) {
		return errNoVideo
	}
	v := strings.ToUpper(visibility)
	if v != "PUBLIC" && v != "UNLISTED" && v != "PRIVATE" {
		return apperr.Validation(map[string]string{"visibility": "must be public, unlisted or private"})
	}
	if err := rateLimit(ctx, s.limiter, s.log, "studio:write:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	ok, err := s.repo.SetVisibility(ctx, p.UserID, videoID, v, s.audit(p, ci, "video.visibility", "video", videoID, map[string]any{"visibility": v}))
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errNoVideo
	}
	return nil
}

func (s *Studio) DeleteVideo(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID string) error {
	if !IsUUID(videoID) {
		return errNoVideo
	}
	if err := rateLimit(ctx, s.limiter, s.log, "studio:write:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	ok, err := s.repo.DeleteVideo(ctx, p.UserID, videoID, s.audit(p, ci, "video.delete", "video", videoID, nil))
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errNoVideo
	}
	return nil
}

func (s *Studio) Comments(ctx context.Context, p *auth.Principal, filter, cursor string) (model.Page[model.StudioComment], error) {
	if filter == "" {
		filter = "all"
	}
	if filter != "all" && filter != "held" {
		return model.Page[model.StudioComment]{}, apperr.Validation(map[string]string{"filter": "must be all or held"})
	}
	after, err := decodePage(cursor)
	if err != nil {
		return model.Page[model.StudioComment]{}, err
	}
	rows, err := s.repo.Comments(ctx, p.UserID, filter, after, StudioPageSize+1)
	if err != nil {
		return model.Page[model.StudioComment]{}, apperr.Internal(err)
	}
	page := model.Page[model.StudioComment]{Items: []model.StudioComment{}}
	if len(rows) > StudioPageSize {
		rows = rows[:StudioPageSize]
		last := rows[len(rows)-1]
		next, err := pagination.Encode(pageCursor{T: last.CreatedAt.UnixMicro(), I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		st := "published"
		if r.Status == "UNDER_REVIEW" {
			st = "held"
		}
		page.Items = append(page.Items, model.StudioComment{ID: r.ID, VideoID: r.VideoID, VideoTitle: r.VideoTitle, Author: r.Author, Text: r.Body,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339), Status: st})
	}
	return page, nil
}

func (s *Studio) ApproveComment(ctx context.Context, p *auth.Principal, ci ClientInfo, commentID string) error {
	if !IsUUID(commentID) {
		return apperr.NotFound("comment_not_found", "Comment not found")
	}
	if err := rateLimit(ctx, s.limiter, s.log, "studio:write:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	ok, err := s.repo.ApproveComment(ctx, p.UserID, commentID, s.audit(p, ci, "comment.approve", "comment", commentID, nil))
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return apperr.NotFound("comment_not_found", "Comment not found")
	}
	return nil
}
