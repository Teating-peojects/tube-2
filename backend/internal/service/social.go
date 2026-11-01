package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/pagination"
	"github.com/redxtube/api/internal/repository"
)

const (
	CommentPageSize = 20
	RepliesPageSize = 50
	MaxCommentRunes = 2000
	MaxReportDetail = 1000
	ViewDedupeTTL   = 30 * time.Minute
)

// Social implements reactions, subscriptions, comments, history, watch later,
// playlists, blocking and reports. Every write is rate limited per user and
// every target is re-checked on the server (is the video visible to this caller?).
type Social struct {
	repo    *repository.Social
	cat     *Catalog
	limiter Limiter
	ipKey   []byte
	log     *slog.Logger
	now     func() time.Time
}

func NewSocial(repo *repository.Social, cat *Catalog, limiter Limiter, ipKey []byte, log *slog.Logger) *Social {
	return &Social{repo: repo, cat: cat, limiter: limiter, ipKey: ipKey, log: log, now: time.Now}
}

var (
	errVideoNotFound    = apperr.NotFound("video_not_found", "Video not found")
	errCommentNotFound  = apperr.NotFound("comment_not_found", "Comment not found")
	errPlaylistNotFound = apperr.NotFound("playlist_not_found", "Playlist not found")
	errChannelNotFound  = apperr.NotFound("channel_not_found", "Channel not found")
	errLimitReached     = func(what string) error { return apperr.New(422, "limit_reached", "You've reached the limit for "+what) }
)

// rateLimit counts an action and rejects it past max. It fails closed.
func rateLimit(ctx context.Context, l Limiter, log *slog.Logger, key string, max int64, window time.Duration) error {
	n, ttl, err := l.Hit(ctx, key, window)
	if err != nil {
		log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
		return errUnavailable
	}
	if n > max {
		return apperr.TooManyRequests(ttl)
	}
	return nil
}

func (s *Social) limit(ctx context.Context, key string, max int64, window time.Duration) error {
	return rateLimit(ctx, s.limiter, s.log, key, max, window)
}

func ptr(s string) *string { return &s }

func uidPtr(p *auth.Principal) *string {
	if p == nil {
		return nil
	}
	return &p.UserID
}

// access returns the video if this viewer may open it, else 404.
func (s *Social) access(ctx context.Context, videoID string, p *auth.Principal) (*repository.VideoAccess, error) {
	if !IsUUID(videoID) {
		return nil, errVideoNotFound
	}
	a, err := s.repo.VideoAccess(ctx, videoID, uidPtr(p))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if a == nil {
		return nil, errVideoNotFound
	}
	return a, nil
}

// ---------- cursors ----------

func (s *Social) keyedPage(kind, cursor string, fetch func(after *repository.KeyedAfter, limit int) ([]repository.KeyedVideoRow, error)) ([]repository.KeyedVideoRow, *string, error) {
	var after *repository.KeyedAfter
	if cursor != "" {
		var c keyCursor
		if pagination.Decode(cursor, &c) != nil || c.K != kind || !IsUUID(c.I) {
			return nil, nil, badCursor()
		}
		after = &repository.KeyedAfter{Time: time.UnixMicro(c.T).UTC(), ID: c.I}
	}
	rows, err := fetch(after, FeedPageSize+1)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	var next *string
	if len(rows) > FeedPageSize {
		rows = rows[:FeedPageSize]
		last := rows[len(rows)-1]
		n, err := pagination.Encode(keyCursor{K: kind, T: last.Key.UnixMicro(), I: last.ID})
		if err != nil {
			return nil, nil, apperr.Internal(err)
		}
		next = &n
	}
	return rows, next, nil
}

func (s *Social) videoPage(rows []repository.KeyedVideoRow, next *string) model.Page[model.Video] {
	items := make([]model.Video, 0, len(rows))
	for _, r := range rows {
		items = append(items, s.cat.video(r.VideoRow))
	}
	return model.Page[model.Video]{Items: items, NextCursor: next}
}

// ---------- reactions ----------

// SetReaction sets "like"/"dislike" or clears it (nil). Idempotent.
func (s *Social) SetReaction(ctx context.Context, p *auth.Principal, videoID string, reaction *string) error {
	if reaction != nil && *reaction != "like" && *reaction != "dislike" {
		return apperr.Validation(map[string]string{"reaction": "must be like, dislike or null"})
	}
	if !IsUUID(videoID) {
		return errVideoNotFound
	}
	if err := s.limit(ctx, "react:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	if reaction == nil {
		if err := s.repo.ClearReaction(ctx, videoID, p.UserID); err != nil {
			return apperr.Internal(err)
		}
		return nil
	}
	if _, err := s.access(ctx, videoID, p); err != nil {
		return err
	}
	if err := s.repo.SetReaction(ctx, videoID, p.UserID, strings.ToUpper(*reaction)); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// ---------- subscriptions ----------

func (s *Social) SetSubscription(ctx context.Context, p *auth.Principal, channelID string, subscribed *bool) error {
	if subscribed == nil {
		return apperr.Validation(map[string]string{"subscribed": "required"})
	}
	if !IsUUID(channelID) {
		return errChannelNotFound
	}
	if channelID == p.UserID {
		return apperr.New(422, "cannot_subscribe_to_self", "You can't subscribe to your own channel")
	}
	if err := s.limit(ctx, "sub:"+p.UserID, 30, time.Minute); err != nil {
		return err
	}
	if !*subscribed {
		if err := s.repo.Unsubscribe(ctx, p.UserID, channelID); err != nil {
			return apperr.Internal(err)
		}
		return nil
	}
	ok, err := s.repo.ActiveUser(ctx, channelID)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errChannelNotFound
	}
	if _, err := s.repo.Subscribe(ctx, p.UserID, channelID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Social) SubscribedChannels(ctx context.Context, p *auth.Principal) ([]model.ChannelResult, error) {
	rows, err := s.repo.SubscribedChannels(ctx, p.UserID, 200)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]model.ChannelResult, 0, len(rows))
	for _, r := range rows {
		c := s.cat.channelResult(r)
		c.IsSubscribed = true
		out = append(out, c)
	}
	return out, nil
}

func (s *Social) SubscriptionFeed(ctx context.Context, p *auth.Principal, cursor string) (model.Page[model.Video], error) {
	var after *repository.Keyset
	if cursor != "" {
		var c keyCursor
		if pagination.Decode(cursor, &c) != nil || c.K != "subfeed" || !IsUUID(c.I) {
			return model.Page[model.Video]{}, badCursor()
		}
		after = &repository.Keyset{Time: time.UnixMicro(c.T).UTC(), ID: c.I}
	}
	rows, err := s.repo.SubscriptionFeed(ctx, p.UserID, after, FeedPageSize+1)
	if err != nil {
		return model.Page[model.Video]{}, apperr.Internal(err)
	}
	page := model.Page[model.Video]{}
	if len(rows) > FeedPageSize {
		rows = rows[:FeedPageSize]
		last := rows[len(rows)-1]
		n, err := pagination.Encode(keyCursor{K: "subfeed", T: last.PublishedAt.UnixMicro(), I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &n
	}
	page.Items = s.cat.videos(rows)
	return page, nil
}

// ---------- comments ----------

var manyNewlines = regexp.MustCompile(`\n{3,}`)

func (s *Social) commentDTO(r repository.CommentRow) model.Comment {
	return model.Comment{
		ID: r.ID, Text: r.Text, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339), Likes: int(r.Likes), Liked: r.Liked,
		ReplyCount: int(r.ReplyCount), Pinned: r.Pinned,
		Author: model.CommentAuthor{ID: r.AuthorID, Name: r.AuthorName, AvatarURL: s.cat.urls.Asset(r.AvatarKey), IsCreator: r.IsCreator},
	}
}

func (s *Social) comments(rows []repository.CommentRow) []model.Comment {
	out := make([]model.Comment, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.commentDTO(r))
	}
	return out
}

// Comments lists top-level comments. The pinned comment leads the first page.
func (s *Social) Comments(ctx context.Context, videoID, sort, cursor string) (model.Page[model.Comment], error) {
	p := auth.FromContext(ctx)
	a, err := s.access(ctx, videoID, p)
	if err != nil {
		return model.Page[model.Comment]{}, err
	}
	if sort == "" {
		sort = "top"
	}
	if sort != "top" && sort != "new" {
		return model.Page[model.Comment]{}, apperr.Validation(map[string]string{"sort": "invalid"})
	}
	page := model.Page[model.Comment]{Items: []model.Comment{}}
	if a.CommentsMode == "OFF" {
		return page, nil
	}
	kind := "c:" + sort
	q := repository.CommentListQuery{VideoID: videoID, Viewer: uidPtr(p), Sort: sort, Limit: CommentPageSize + 1}
	if cursor != "" {
		var c keyCursor
		if pagination.Decode(cursor, &c) != nil || c.K != kind || !IsUUID(c.I) {
			return page, badCursor()
		}
		q.After = &repository.CommentKey{Time: time.UnixMicro(c.T).UTC(), Likes: c.N, ID: c.I}
	}
	var items []model.Comment
	if cursor == "" {
		pinned, err := s.repo.PinnedComment(ctx, videoID, uidPtr(p))
		if err != nil {
			return page, apperr.Internal(err)
		}
		if pinned != nil {
			items = append(items, s.commentDTO(*pinned))
		}
	}
	rows, err := s.repo.TopLevelComments(ctx, q)
	if err != nil {
		return page, apperr.Internal(err)
	}
	if len(rows) > CommentPageSize {
		rows = rows[:CommentPageSize]
		last := rows[len(rows)-1]
		n, err := pagination.Encode(keyCursor{K: kind, T: last.CreatedAt.UnixMicro(), N: int64(last.Likes), I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &n
	}
	page.Items = append(items, s.comments(rows)...)
	if page.Items == nil {
		page.Items = []model.Comment{}
	}
	return page, nil
}

// Replies lists the replies of a top-level comment, oldest first.
func (s *Social) Replies(ctx context.Context, commentID, cursor string) (model.Page[model.Comment], error) {
	p := auth.FromContext(ctx)
	page := model.Page[model.Comment]{Items: []model.Comment{}}
	if !IsUUID(commentID) {
		return page, errCommentNotFound
	}
	ref, err := s.repo.CommentRef(ctx, commentID)
	if err != nil {
		return page, apperr.Internal(err)
	}
	if ref == nil || ref.Status != "VISIBLE" {
		return page, errCommentNotFound
	}
	a, err := s.access(ctx, ref.VideoID, p)
	if err != nil {
		return page, errCommentNotFound
	}
	if a.CommentsMode == "OFF" {
		return page, nil
	}
	var after *repository.CommentKey
	if cursor != "" {
		var c keyCursor
		if pagination.Decode(cursor, &c) != nil || c.K != "r" || !IsUUID(c.I) {
			return page, badCursor()
		}
		after = &repository.CommentKey{Time: time.UnixMicro(c.T).UTC(), ID: c.I}
	}
	rows, err := s.repo.Replies(ctx, commentID, uidPtr(p), after, RepliesPageSize+1)
	if err != nil {
		return page, apperr.Internal(err)
	}
	if len(rows) > RepliesPageSize {
		rows = rows[:RepliesPageSize]
		last := rows[len(rows)-1]
		n, err := pagination.Encode(keyCursor{K: "r", T: last.CreatedAt.UnixMicro(), I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &n
	}
	page.Items = s.comments(rows)
	return page, nil
}

// CreateComment posts a comment or reply. Replies to replies attach to the top-level comment.
func (s *Social) CreateComment(ctx context.Context, p *auth.Principal, videoID, text string, parentID *string) (model.Comment, error) {
	text = manyNewlines.ReplaceAllString(cleanText(text, true), "\n\n")
	if n := utf8.RuneCountInString(text); n < 1 || n > MaxCommentRunes {
		return model.Comment{}, apperr.Validation(map[string]string{"text": "1-2000 characters"})
	}
	if parentID != nil && !IsUUID(*parentID) {
		return model.Comment{}, apperr.Validation(map[string]string{"parentId": "invalid"})
	}
	a, err := s.access(ctx, videoID, p)
	if err != nil {
		return model.Comment{}, err
	}
	if a.CommentsMode == "OFF" {
		return model.Comment{}, apperr.Forbidden("comments_disabled", "Comments are turned off for this video")
	}

	params := repository.CreateCommentParams{ID: auth.NewUUIDv7(), VideoID: videoID, AuthorID: p.UserID, Body: text, Status: "VISIBLE", VideoCreatorID: a.CreatorID}
	if parentID != nil {
		ref, err := s.repo.CommentRef(ctx, *parentID)
		if err != nil {
			return model.Comment{}, apperr.Internal(err)
		}
		if ref == nil || ref.VideoID != videoID || ref.Status != "VISIBLE" {
			return model.Comment{}, errCommentNotFound
		}
		top := ref.ID
		if ref.ParentID != nil {
			top = *ref.ParentID
		}
		params.ParentID, params.ParentAuthorID = &top, &ref.AuthorID
	}

	if err := s.limit(ctx, "comment:min:"+p.UserID, 10, time.Minute); err != nil {
		return model.Comment{}, err
	}
	if err := s.limit(ctx, "comment:hr:"+p.UserID, 100, time.Hour); err != nil {
		return model.Comment{}, err
	}
	// the same words on the same video within 5 minutes are a double-submit or spam
	dup := short(p.UserID + "|" + videoID + "|" + strings.ToLower(text))
	if n, _, err := s.limiter.Hit(ctx, "comment:dup:"+dup, 5*time.Minute); err != nil {
		s.log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
		return model.Comment{}, errUnavailable
	} else if n > 1 {
		return model.Comment{}, apperr.Conflict("duplicate_comment", "You already posted that")
	}

	if a.CommentsMode == "REVIEW" && p.UserID != a.CreatorID {
		params.Status = "UNDER_REVIEW" // waits for the creator's approval
	}
	row, err := s.repo.CreateComment(ctx, params)
	if err != nil {
		return model.Comment{}, apperr.Internal(err)
	}
	return s.commentDTO(row), nil
}

// commentTarget loads a live comment and checks the caller may see its video.
func (s *Social) commentTarget(ctx context.Context, p *auth.Principal, id string) (*repository.CommentRef, error) {
	if !IsUUID(id) {
		return nil, errCommentNotFound
	}
	ref, err := s.repo.CommentRef(ctx, id)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if ref == nil {
		return nil, errCommentNotFound
	}
	return ref, nil
}

func (s *Social) LikeComment(ctx context.Context, p *auth.Principal, id string, liked *bool) error {
	if liked == nil {
		return apperr.Validation(map[string]string{"liked": "required"})
	}
	ref, err := s.commentTarget(ctx, p, id)
	if err != nil {
		return err
	}
	if ref.Status != "VISIBLE" {
		return errCommentNotFound
	}
	if _, err := s.access(ctx, ref.VideoID, p); err != nil {
		return errCommentNotFound
	}
	if err := s.limit(ctx, "clike:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	if err := s.repo.SetCommentLike(ctx, id, p.UserID, *liked); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// DeleteComment: the author, the video's creator and moderators may delete.
func (s *Social) DeleteComment(ctx context.Context, p *auth.Principal, id string, ci ClientInfo) error {
	ref, err := s.commentTarget(ctx, p, id)
	if err != nil {
		return err
	}
	isAuthor := ref.AuthorID == p.UserID
	if !isAuthor && ref.VideoCreatorID != p.UserID && !p.Role.AtLeast(auth.RoleModerator) {
		return apperr.Forbidden("forbidden", "You can't delete this comment")
	}
	if err := s.limit(ctx, "cdel:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	var a *repository.AuditEntry
	if !isAuthor { // removing someone else's comment is a moderation act and is logged
		target := "comment"
		a = &repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: "comment.delete", TargetType: &target, TargetID: &ref.ID, IPHash: auth.HashIP(s.ipKey, ci.IP), RequestID: ci.RequestID}
	}
	if err := s.repo.DeleteComment(ctx, *ref, a); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// PinComment: only the video's creator (or a moderator) can pin; one pinned comment per video.
func (s *Social) PinComment(ctx context.Context, p *auth.Principal, id string, pinned *bool) error {
	if pinned == nil {
		return apperr.Validation(map[string]string{"pinned": "required"})
	}
	ref, err := s.commentTarget(ctx, p, id)
	if err != nil {
		return err
	}
	if ref.VideoCreatorID != p.UserID && !p.Role.AtLeast(auth.RoleModerator) {
		return apperr.Forbidden("forbidden", "Only the video's creator can pin comments")
	}
	ok, err := s.repo.PinComment(ctx, *ref, *pinned)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return apperr.New(422, "not_pinnable", "Only published top-level comments can be pinned")
	}
	return nil
}

// ---------- views, progress, history ----------

var botRe = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|facebookexternalhit|preview|headless|python-requests|curl/|wget|go-http-client|httpclient|monitor|scrapy`)

func isBot(ua string) bool { return strings.TrimSpace(ua) == "" || botRe.MatchString(ua) }

func deviceOf(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "ipad") || strings.Contains(l, "tablet") || (strings.Contains(l, "android") && !strings.Contains(l, "mobile")):
		return "tablet"
	case strings.Contains(l, "mobi") || strings.Contains(l, "iphone") || strings.Contains(l, "android"):
		return "mobile"
	case strings.Contains(l, "smart-tv") || strings.Contains(l, "smarttv") || strings.Contains(l, "appletv"):
		return "tv"
	case l == "":
		return "other"
	}
	return "desktop"
}

// traffic sources the schema knows; anything else is recorded as OTHER
var viewSources = map[string]string{"home": "HOME", "search": "SEARCH", "subscriptions": "SUBSCRIPTIONS", "external": "EXTERNAL", "direct": "DIRECT",
	"trending": "OTHER", "channel": "OTHER", "playlist": "OTHER"}

// RecordView counts a view at most once per viewer per 30 minutes (refreshing
// the page is not a new view). Bots and the creator's own views are ignored.
// It never errors to the client for abuse cases: excess events are just dropped.
func (s *Social) RecordView(ctx context.Context, p *auth.Principal, videoID, source string, ci ClientInfo) error {
	a, err := s.access(ctx, videoID, p)
	if err != nil {
		return err
	}
	if isBot(ci.UserAgent) || (p != nil && p.UserID == a.CreatorID) {
		return nil
	}
	trafficSource, known := viewSources[source]
	if !known {
		trafficSource = "DIRECT"
	}
	n, _, err := s.limiter.Hit(ctx, "view:ip:"+ci.IP, time.Minute)
	if err != nil {
		s.log.ErrorContext(ctx, "rate limiter unavailable (view not counted)", "err", err.Error())
		return nil
	}
	if n > 120 {
		return nil
	}
	m := hmac.New(sha256.New, s.ipKey)
	m.Write([]byte("viewer|" + ci.IP + "|" + ci.UserAgent))
	who := "a:" + short(string(m.Sum(nil)))
	if p != nil {
		who = "u:" + p.UserID
	}
	first, _, err := s.limiter.Hit(ctx, "view:"+videoID+":"+who, ViewDedupeTTL)
	if err != nil {
		s.log.ErrorContext(ctx, "rate limiter unavailable (view not counted)", "err", err.Error())
		return nil
	}
	if first == 1 {
		// ip_hash rotates daily (HMAC of the IP with a per-day key): enough to spot abuse
		// within a day, useless for tracking a person across days
		day := hmac.New(sha256.New, s.ipKey)
		day.Write([]byte("view-day|" + s.now().UTC().Format("2006-01-02")))
		ev := repository.ViewEvent{VideoID: videoID, SessionID: auth.NewUUIDv7(), Device: strings.ToUpper(deviceOf(ci.UserAgent)), Source: trafficSource,
			IPHash: auth.HashIP(day.Sum(nil), ci.IP)}
		if p != nil {
			ev.ViewerID = p.UserID
		}
		if c := countryPtr(ci.Country); c != nil {
			ev.Country = *c
		}
		if err := s.repo.RecordView(ctx, ev); err != nil {
			return apperr.Internal(err)
		}
	}
	if p != nil {
		if save, err := s.repo.SavesHistory(ctx, p.UserID); err == nil && save {
			if err := s.repo.TouchHistory(ctx, p.UserID, videoID); err != nil {
				s.log.WarnContext(ctx, "history not saved", "err", err.Error())
			}
		}
	}
	return nil
}

// Progress stores the resume position and adds watched seconds (<=120 per call).
func (s *Social) Progress(ctx context.Context, p *auth.Principal, videoID string, position, watched int) error {
	if position < 0 || watched < 0 {
		return apperr.Validation(map[string]string{"positionSec": "must not be negative"})
	}
	a, err := s.access(ctx, videoID, p)
	if err != nil {
		return err
	}
	if err := s.limit(ctx, "progress:"+p.UserID, 30, time.Minute); err != nil {
		return err
	}
	if save, err := s.repo.SavesHistory(ctx, p.UserID); err != nil {
		return apperr.Internal(err)
	} else if !save {
		return nil // the user turned watch history off
	}
	const maxWatchedPerCall = 120
	if watched > maxWatchedPerCall {
		watched = maxWatchedPerCall
	}
	completed := false
	if a.DurationSec != nil && *a.DurationSec > 0 {
		if position > int(*a.DurationSec) {
			position = int(*a.DurationSec)
		}
		completed = float64(position) >= 0.9*float64(*a.DurationSec)
	} else if position > 86400 {
		position = 86400
	}
	if err := s.repo.AddProgress(ctx, p.UserID, videoID, int32(position), int32(watched), completed); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Social) History(ctx context.Context, p *auth.Principal, cursor string) (model.Page[model.HistoryItem], error) {
	rows, next, err := s.keyedPage("history", cursor, func(after *repository.KeyedAfter, limit int) ([]repository.KeyedVideoRow, error) {
		return s.repo.History(ctx, p.UserID, after, limit)
	})
	if err != nil {
		return model.Page[model.HistoryItem]{}, err
	}
	items := make([]model.HistoryItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, model.HistoryItem{Video: s.cat.video(r.VideoRow), WatchedAt: r.Key.UTC().Format(time.RFC3339), ProgressSec: int(r.Progress)})
	}
	return model.Page[model.HistoryItem]{Items: items, NextCursor: next}, nil
}

func (s *Social) DeleteHistoryItem(ctx context.Context, p *auth.Principal, videoID string) error {
	if !IsUUID(videoID) {
		return errVideoNotFound
	}
	if err := s.limit(ctx, "histdel:"+p.UserID, 120, time.Minute); err != nil {
		return err
	}
	if err := s.repo.DeleteHistoryItem(ctx, p.UserID, videoID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Social) ClearHistory(ctx context.Context, p *auth.Principal) error {
	if err := s.limit(ctx, "histdel:"+p.UserID, 120, time.Minute); err != nil {
		return err
	}
	if err := s.repo.ClearHistory(ctx, p.UserID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// ---------- watch later / liked ----------

func (s *Social) WatchLater(ctx context.Context, p *auth.Principal, cursor string) (model.Page[model.Video], error) {
	rows, next, err := s.keyedPage("watchlater", cursor, func(after *repository.KeyedAfter, limit int) ([]repository.KeyedVideoRow, error) {
		return s.repo.WatchLater(ctx, p.UserID, after, limit)
	})
	if err != nil {
		return model.Page[model.Video]{}, err
	}
	return s.videoPage(rows, next), nil
}

func (s *Social) WatchLaterIDs(ctx context.Context, p *auth.Principal) ([]string, error) {
	ids, err := s.repo.WatchLaterIDs(ctx, p.UserID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return ids, nil
}

func (s *Social) AddWatchLater(ctx context.Context, p *auth.Principal, videoID string) error {
	if _, err := s.access(ctx, videoID, p); err != nil {
		return err
	}
	if err := s.limit(ctx, "wl:"+p.UserID, 120, time.Minute); err != nil {
		return err
	}
	_, full, err := s.repo.AddWatchLater(ctx, p.UserID, videoID)
	if err != nil {
		return apperr.Internal(err)
	}
	if full {
		return errLimitReached("Watch later")
	}
	return nil
}

func (s *Social) RemoveWatchLater(ctx context.Context, p *auth.Principal, videoID string) error {
	if !IsUUID(videoID) {
		return errVideoNotFound
	}
	if err := s.limit(ctx, "wl:"+p.UserID, 120, time.Minute); err != nil {
		return err
	}
	if err := s.repo.RemoveWatchLater(ctx, p.UserID, videoID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Social) Liked(ctx context.Context, p *auth.Principal, cursor string) (model.Page[model.Video], error) {
	rows, next, err := s.keyedPage("liked", cursor, func(after *repository.KeyedAfter, limit int) ([]repository.KeyedVideoRow, error) {
		return s.repo.Liked(ctx, p.UserID, after, limit)
	})
	if err != nil {
		return model.Page[model.Video]{}, err
	}
	return s.videoPage(rows, next), nil
}

// ---------- not interested / block ----------

func (s *Social) NotInterested(ctx context.Context, p *auth.Principal, videoID string) error {
	if _, err := s.access(ctx, videoID, p); err != nil {
		return err
	}
	if err := s.limit(ctx, "notint:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	if err := s.repo.NotInterested(ctx, p.UserID, videoID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *Social) SetBlock(ctx context.Context, p *auth.Principal, channelID string, blocked bool) error {
	if !IsUUID(channelID) {
		return errChannelNotFound
	}
	if channelID == p.UserID {
		return apperr.New(422, "cannot_block_self", "You can't block yourself")
	}
	if err := s.limit(ctx, "block:"+p.UserID, 30, time.Minute); err != nil {
		return err
	}
	if !blocked {
		if err := s.repo.Unblock(ctx, p.UserID, channelID); err != nil {
			return apperr.Internal(err)
		}
		return nil
	}
	ok, err := s.repo.ActiveUser(ctx, channelID)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errChannelNotFound
	}
	if err := s.repo.Block(ctx, p.UserID, channelID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// ---------- playlists ----------

func cleanTitle(s string) string { return strings.Join(strings.Fields(cleanText(s, false)), " ") }

func parseVisibility(v string) (string, bool) {
	switch strings.ToLower(v) {
	case "public":
		return "PUBLIC", true
	case "private":
		return "PRIVATE", true
	}
	return "", false
}

func (s *Social) playlistDTO(r repository.PlaylistRow) model.Playlist {
	return model.Playlist{ID: r.ID, Title: r.Title, Description: r.Description, Visibility: strings.ToLower(r.Visibility),
		VideoCount: int(r.VideoCount), ThumbnailURL: s.cat.urls.Asset(r.ThumbKey), OwnerName: r.OwnerName, UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339)}
}

func (s *Social) MyPlaylists(ctx context.Context, p *auth.Principal, videoID string) ([]model.PlaylistWithMembership, error) {
	var vid *string
	if videoID != "" {
		if !IsUUID(videoID) {
			return nil, apperr.Validation(map[string]string{"videoId": "invalid"})
		}
		vid = &videoID
	}
	rows, err := s.repo.MyPlaylists(ctx, p.UserID, vid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]model.PlaylistWithMembership, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.PlaylistWithMembership{Playlist: s.playlistDTO(r.PlaylistRow), Contains: r.Contains})
	}
	return out, nil
}

func (s *Social) CreatePlaylist(ctx context.Context, p *auth.Principal, title, visibility string, videoID *string) (model.Playlist, error) {
	fields := map[string]string{}
	title = cleanTitle(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > 100 {
		fields["title"] = "1-100 characters"
	}
	vis, ok := parseVisibility(visibility)
	if !ok {
		fields["visibility"] = "must be public or private"
	}
	if len(fields) > 0 {
		return model.Playlist{}, apperr.Validation(fields)
	}
	if videoID != nil {
		if _, err := s.access(ctx, *videoID, p); err != nil {
			return model.Playlist{}, err
		}
	}
	if err := s.limit(ctx, "plwrite:"+p.UserID, 60, time.Minute); err != nil {
		return model.Playlist{}, err
	}
	id := auth.NewUUIDv7()
	full, err := s.repo.CreatePlaylist(ctx, id, p.UserID, title, vis, videoID)
	if err != nil {
		return model.Playlist{}, apperr.Internal(err)
	}
	if full {
		return model.Playlist{}, errLimitReached("playlists")
	}
	d, err := s.repo.Playlist(ctx, id, &p.UserID)
	if err != nil || d == nil {
		return model.Playlist{}, apperr.Internal(err)
	}
	return s.playlistDTO(d.PlaylistRow), nil
}

func (s *Social) GetPlaylist(ctx context.Context, id string) (model.PlaylistDetail, error) {
	p := auth.FromContext(ctx)
	if !IsUUID(id) {
		return model.PlaylistDetail{}, errPlaylistNotFound
	}
	d, err := s.repo.Playlist(ctx, id, uidPtr(p))
	if err != nil {
		return model.PlaylistDetail{}, apperr.Internal(err)
	}
	if d == nil {
		return model.PlaylistDetail{}, errPlaylistNotFound
	}
	vids, err := s.repo.PlaylistVideos(ctx, id, uidPtr(p))
	if err != nil {
		return model.PlaylistDetail{}, apperr.Internal(err)
	}
	return model.PlaylistDetail{Playlist: s.playlistDTO(d.PlaylistRow), IsOwner: p != nil && p.UserID == d.OwnerID, Videos: s.cat.videos(vids)}, nil
}

type PlaylistPatch struct{ Title, Description, Visibility *string }

func (s *Social) UpdatePlaylist(ctx context.Context, p *auth.Principal, id string, in PlaylistPatch) error {
	if !IsUUID(id) {
		return errPlaylistNotFound
	}
	fields := map[string]string{}
	var title, desc, vis *string
	if in.Title != nil {
		t := cleanTitle(*in.Title)
		if n := utf8.RuneCountInString(t); n < 1 || n > 100 {
			fields["title"] = "1-100 characters"
		}
		title = &t
	}
	if in.Description != nil {
		d := cleanText(*in.Description, true)
		if utf8.RuneCountInString(d) > 1000 {
			fields["description"] = "At most 1000 characters"
		}
		desc = &d
	}
	if in.Visibility != nil {
		v, ok := parseVisibility(*in.Visibility)
		if !ok {
			fields["visibility"] = "must be public or private"
		}
		vis = &v
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	if err := s.limit(ctx, "plwrite:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	ok, err := s.repo.UpdatePlaylist(ctx, id, p.UserID, title, desc, vis)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errPlaylistNotFound
	}
	return nil
}

func (s *Social) DeletePlaylist(ctx context.Context, p *auth.Principal, id string) error {
	if !IsUUID(id) {
		return errPlaylistNotFound
	}
	if err := s.limit(ctx, "plwrite:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	ok, err := s.repo.DeletePlaylist(ctx, id, p.UserID)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errPlaylistNotFound
	}
	return nil
}

func (s *Social) playlistOutcome(o repository.PlaylistOutcome) error {
	switch o {
	case repository.PlaylistNotFound:
		return errPlaylistNotFound
	case repository.PlaylistFull:
		return errLimitReached("videos in a playlist")
	case repository.PlaylistMismatch:
		return apperr.New(422, "invalid_order", "The order must contain exactly the videos in the playlist")
	}
	return nil
}

func (s *Social) AddPlaylistVideo(ctx context.Context, p *auth.Principal, id, videoID string) error {
	if !IsUUID(id) {
		return errPlaylistNotFound
	}
	if _, err := s.access(ctx, videoID, p); err != nil {
		return err
	}
	if err := s.limit(ctx, "plwrite:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	o, err := s.repo.AddPlaylistVideo(ctx, id, p.UserID, videoID)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.playlistOutcome(o)
}

func (s *Social) RemovePlaylistVideo(ctx context.Context, p *auth.Principal, id, videoID string) error {
	if !IsUUID(id) {
		return errPlaylistNotFound
	}
	if !IsUUID(videoID) {
		return errVideoNotFound
	}
	if err := s.limit(ctx, "plwrite:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	o, err := s.repo.RemovePlaylistVideo(ctx, id, p.UserID, videoID)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.playlistOutcome(o)
}

func (s *Social) ReorderPlaylist(ctx context.Context, p *auth.Principal, id string, videoIDs []string) error {
	if !IsUUID(id) {
		return errPlaylistNotFound
	}
	bad := apperr.New(422, "invalid_order", "The order must contain exactly the videos in the playlist")
	if len(videoIDs) > repository.MaxPlaylistVideos {
		return bad
	}
	seen := map[string]bool{}
	for i, v := range videoIDs {
		v = strings.ToLower(v)
		if !IsUUID(v) || seen[v] {
			return bad
		}
		seen[v], videoIDs[i] = true, v
	}
	if err := s.limit(ctx, "plwrite:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	o, err := s.repo.ReorderPlaylist(ctx, id, p.UserID, videoIDs)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.playlistOutcome(o)
}

// ---------- reports ----------

type ReportInput struct{ TargetType, TargetID, Reason, Details string }

// frontend reason ids -> report_reason enum. A database trigger sets the
// priority (underage / non-consensual / illegal content are urgent).
var reportReasons = map[string]string{
	"non_consensual": "NON_CONSENSUAL_CONTENT",
	"underage":       "UNDERAGE_CONTENT",
	"illegal":        "ILLEGAL_CONTENT",
	"copyright":      "COPYRIGHT",
	"harassment":     "HARASSMENT",
	"spam":           "SPAM",
	"other":          "OTHER",
}

// Report files a report for the moderation queue. Urgent categories get priority 0.
func (s *Social) Report(ctx context.Context, p *auth.Principal, in ReportInput) error {
	fields := map[string]string{}
	tt := strings.ToLower(in.TargetType)
	if !map[string]bool{"video": true, "comment": true, "channel": true, "user": true}[tt] {
		fields["targetType"] = "must be video, comment, channel or user"
	}
	reasonEnum, ok := reportReasons[strings.ToLower(in.Reason)]
	if !ok {
		fields["reason"] = "invalid"
	}
	details := cleanText(in.Details, true)
	if utf8.RuneCountInString(details) > MaxReportDetail {
		fields["details"] = "At most 1000 characters"
	}
	if strings.ToLower(in.Reason) == "other" && details == "" {
		fields["details"] = "Please tell us what's wrong"
	}
	if !IsUUID(in.TargetID) {
		fields["targetId"] = "invalid"
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	targetType := strings.ToUpper(tt)
	notFound := apperr.NotFound("target_not_found", "That content no longer exists")

	// the reporter must be able to see the target (no probing for hidden ids)
	switch tt {
	case "video":
		if _, err := s.access(ctx, in.TargetID, p); err != nil {
			return notFound
		}
	case "comment":
		ref, err := s.repo.CommentRef(ctx, in.TargetID)
		if err != nil {
			return apperr.Internal(err)
		}
		if ref == nil {
			return notFound
		}
		if _, err := s.access(ctx, ref.VideoID, p); err != nil {
			return notFound
		}
	default:
		if in.TargetID == p.UserID {
			return apperr.New(422, "cannot_report_self", "You can't report yourself")
		}
		ok, err := s.repo.ActiveUser(ctx, in.TargetID)
		if err != nil {
			return apperr.Internal(err)
		}
		if !ok {
			return notFound
		}
	}
	if err := s.limit(ctx, "report:"+p.UserID, 10, time.Hour); err != nil {
		return err
	}
	if tt == "channel" {
		targetType = "USER" // channels are users
	}
	err := s.repo.CreateReport(ctx, repository.ReportInput{
		ReporterID: p.UserID, TargetType: targetType, TargetID: in.TargetID, Reason: reasonEnum, Details: details,
	})
	if err == repository.ErrAlreadyReported {
		return apperr.Conflict("already_reported", "You've already reported this")
	}
	if err != nil {
		return apperr.Internal(err)
	}
	return nil
}
