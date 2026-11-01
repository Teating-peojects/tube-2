// Package service holds business rules for the read side of the platform:
// input validation, category routing, pagination and shaping rows into the
// JSON models. It depends on a Store interface, so it is unit-testable.
package service

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/pagination"
	"github.com/redxtube/api/internal/repository"
)

const (
	FeedPageSize    = 12
	SearchPageSize  = 10
	TrendingMax     = 100  // trending lists are bounded; B8 caches the ranking in Redis
	SearchMaxOffset = 1000 // deep paging into ranked search is not supported
	PopularChannels = 12
	MaxQueryRunes   = 100
)

var (
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	slugRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
)

func IsUUID(s string) bool { return uuidRe.MatchString(s) }

// Store is implemented by repository.Catalog.
type Store interface {
	ListVideos(ctx context.Context, q repository.VideoListQuery) ([]repository.VideoRow, error)
	Trending(ctx context.Context, tab string, viewer *string, offset, limit int) ([]repository.VideoRow, error)
	VideoDetail(ctx context.Context, id string, viewerID *string) (*repository.VideoDetailRow, error)
	Sitemap(ctx context.Context, limit int) ([]repository.SitemapRow, error)
	PopularChannels(ctx context.Context, viewer *string, limit int) ([]repository.ChannelRow, error)
	Channel(ctx context.Context, id string) (*repository.ChannelDetailRow, error)
	ChannelPlaylists(ctx context.Context, channelID string, limit int) ([]repository.PlaylistRow, error)
	SearchVideos(ctx context.Context, q repository.SearchVideosQuery) ([]repository.VideoRow, int, error)
	SearchChannels(ctx context.Context, viewer *string, q string, limit int) ([]repository.ChannelRow, error)
	SearchPlaylists(ctx context.Context, q string, limit int) ([]repository.PlaylistRow, error)
	SuggestTitles(ctx context.Context, q string, limit int) ([]string, error)
	SuggestChannels(ctx context.Context, q string, limit int) ([]string, error)
}

// ViewerData supplies per-viewer facts (subscribed? reacted?). Optional: when
// nil (unit tests, anonymous-only setups) those fields keep their defaults.
type ViewerData interface {
	SubscribedAmong(ctx context.Context, viewer string, ids []string) (map[string]bool, error)
	ReactionFor(ctx context.Context, videoID, userID string) (string, error)
}

type Catalog struct {
	store  Store
	urls   media.URLs
	now    func() time.Time
	viewer ViewerData
}

func NewCatalog(store Store, urls media.URLs) *Catalog {
	return &Catalog{store: store, urls: urls, now: time.Now}
}

// WithViewerData enables isSubscribed / reaction for signed-in viewers.
func (s *Catalog) WithViewerData(v ViewerData) *Catalog { s.viewer = v; return s }

// viewerID returns the signed-in user's id (nil when anonymous).
func viewerID(ctx context.Context) *string {
	if p := auth.FromContext(ctx); p != nil {
		return &p.UserID
	}
	return nil
}

// markSubscribed sets IsSubscribed on channel summaries for the signed-in viewer (one query).
func (s *Catalog) markSubscribed(ctx context.Context, chans ...*model.ChannelSummary) error {
	v := viewerID(ctx)
	if v == nil || s.viewer == nil || len(chans) == 0 {
		return nil
	}
	ids := make([]string, len(chans))
	for i, c := range chans {
		ids[i] = c.ID
	}
	subs, err := s.viewer.SubscribedAmong(ctx, *v, ids)
	if err != nil {
		return apperr.Internal(err)
	}
	for _, c := range chans {
		c.IsSubscribed = subs[c.ID]
	}
	return nil
}

// ---------- cursors ----------

// keyCursor is the keyset position; K ties a cursor to the listing it came from.
type keyCursor struct {
	K string `json:"k"`
	T int64  `json:"t,omitempty"` // unix micro (latest order)
	N int64  `json:"n,omitempty"` // view count (popular order)
	I string `json:"i,omitempty"`
}

type offsetCursor struct {
	K string `json:"k"`
	O int    `json:"o"`
}

func badCursor() *apperr.Error { return apperr.BadRequest("invalid_cursor", "Invalid cursor") }

func strPtr(s string) *string { return &s }

// ---------- shaping ----------

func (s *Catalog) video(r repository.VideoRow) model.Video {
	dur := 0
	if r.DurationSec != nil {
		dur = int(*r.DurationSec)
	}
	return model.Video{
		ID: r.ID, Title: r.Title, ThumbnailURL: s.urls.Asset(r.ThumbKey), DurationSec: dur,
		Quality: media.Quality(r.ShortSide), Views: r.Views, PublishedAt: r.PublishedAt.UTC().Format(time.RFC3339),
		IsLive:  r.IsLive,
		Creator: model.Creator{ID: r.CreatorID, Name: r.CreatorName, AvatarURL: s.urls.Asset(r.AvatarKey), Verified: r.Verified},
	}
}

func (s *Catalog) videos(rows []repository.VideoRow) []model.Video {
	out := make([]model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.video(r))
	}
	return out
}

func (s *Catalog) channelResult(r repository.ChannelRow) model.ChannelResult {
	return model.ChannelResult{
		ChannelSummary: model.ChannelSummary{
			Creator:     model.Creator{ID: r.ID, Name: r.Name, AvatarURL: s.urls.Asset(r.AvatarKey), Verified: r.Verified},
			Subscribers: r.Subscribers, // IsSubscribed is filled in once sessions exist (B3/B4)
		},
		VideoCount: int(r.VideoCount), Description: r.Bio,
	}
}

func (s *Catalog) playlistSummary(r repository.PlaylistRow) model.PlaylistSummary {
	return model.PlaylistSummary{ID: r.ID, Title: r.Title, VideoCount: int(r.VideoCount), ThumbnailURL: s.urls.Asset(r.ThumbKey), CreatorName: r.OwnerName}
}

// ---------- feeds ----------

// Videos serves GET /videos. category is the chip value (lowercased by the frontend).
func (s *Catalog) Videos(ctx context.Context, category, cursor string) (model.Page[model.Video], error) {
	category = strings.ToLower(strings.TrimSpace(category))
	switch category {
	case "", "all":
		return s.listPage(ctx, repository.VideoListQuery{Order: repository.OrderLatest, Kind: "LONG"}, cursor)
	case "new":
		return s.listPage(ctx, repository.VideoListQuery{Order: repository.OrderLatest, Kind: "LONG"}, cursor)
	case "popular":
		return s.listPage(ctx, repository.VideoListQuery{Order: repository.OrderPopular, Kind: "LONG"}, cursor)
	case "trending":
		return s.Trending(ctx, "now", cursor)
	case "live":
		return s.Live(ctx, cursor)
	case "shorts":
		return s.Shorts(ctx, cursor)
	}
	if !slugRe.MatchString(category) {
		return model.Page[model.Video]{}, apperr.Validation(map[string]string{"category": "invalid"})
	}
	return s.listPage(ctx, repository.VideoListQuery{Order: repository.OrderLatest, Kind: "LONG", CategorySlug: category}, cursor)
}

func (s *Catalog) Shorts(ctx context.Context, cursor string) (model.Page[model.Video], error) {
	return s.listPage(ctx, repository.VideoListQuery{Order: repository.OrderLatest, Kind: "SHORT"}, cursor)
}

func (s *Catalog) Live(ctx context.Context, cursor string) (model.Page[model.Video], error) {
	t := true
	return s.listPage(ctx, repository.VideoListQuery{Order: repository.OrderLatest, Live: &t}, cursor)
}

// listPage runs a keyset-paginated listing: fetch limit+1, trim, build next cursor.
func (s *Catalog) listPage(ctx context.Context, q repository.VideoListQuery, cursor string) (model.Page[model.Video], error) {
	order := string(q.Order)
	if cursor != "" {
		var c keyCursor
		if pagination.Decode(cursor, &c) != nil || c.K != order || !IsUUID(c.I) {
			return model.Page[model.Video]{}, badCursor()
		}
		q.After = &repository.Keyset{Time: time.UnixMicro(c.T).UTC(), Num: c.N, ID: c.I}
	}
	q.Limit = FeedPageSize + 1
	q.Viewer = viewerID(ctx)
	rows, err := s.store.ListVideos(ctx, q)
	if err != nil {
		return model.Page[model.Video]{}, apperr.Internal(err)
	}
	page := model.Page[model.Video]{}
	if len(rows) > FeedPageSize {
		rows = rows[:FeedPageSize]
		last := rows[len(rows)-1]
		next, err := pagination.Encode(keyCursor{K: order, T: last.PublishedAt.UnixMicro(), N: last.Views, I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	page.Items = s.videos(rows)
	return page, nil
}

var trendingTabs = map[string]bool{"now": true, "most-viewed": true, "fast-growing": true, "recent": true}

// Trending serves GET /trending. Lists are bounded (TrendingMax) and offset-paged.
func (s *Catalog) Trending(ctx context.Context, tab, cursor string) (model.Page[model.Video], error) {
	if tab == "" {
		tab = "now"
	}
	if !trendingTabs[tab] {
		return model.Page[model.Video]{}, apperr.Validation(map[string]string{"tab": "invalid"})
	}
	key := "trending:" + tab
	offset := 0
	if cursor != "" {
		var c offsetCursor
		if pagination.Decode(cursor, &c) != nil || c.K != key || c.O < 0 || c.O >= TrendingMax || c.O%FeedPageSize != 0 {
			return model.Page[model.Video]{}, badCursor()
		}
		offset = c.O
	}
	fetch := FeedPageSize + 1
	if remaining := TrendingMax - offset; remaining < fetch {
		fetch = remaining
	}
	rows, err := s.store.Trending(ctx, tab, viewerID(ctx), offset, fetch)
	if err != nil {
		return model.Page[model.Video]{}, apperr.Internal(err)
	}
	page := model.Page[model.Video]{}
	// The window is bounded: never emit more than TrendingMax items overall,
	// regardless of how many rows the store returned.
	size := FeedPageSize
	if remaining := TrendingMax - offset; remaining < size {
		size = remaining
	}
	if len(rows) > size {
		hasMore := offset+size < TrendingMax
		rows = rows[:size]
		if hasMore {
			next, err := pagination.Encode(offsetCursor{K: key, O: offset + FeedPageSize})
			if err != nil {
				return page, apperr.Internal(err)
			}
			page.NextCursor = &next
		}
	}
	page.Items = s.videos(rows)
	return page, nil
}

// ---------- video detail ----------

// VideoDetail serves GET /videos/{id}. viewerID is nil for anonymous callers
// (sessions arrive in B3); it lets owners open their own PRIVATE videos.
func (s *Catalog) VideoDetail(ctx context.Context, id string, viewer *string) (model.VideoDetail, error) {
	notFound := apperr.NotFound("video_not_found", "Video not found")
	if !IsUUID(id) {
		return model.VideoDetail{}, notFound
	}
	d, err := s.store.VideoDetail(ctx, id, viewer)
	if err != nil {
		return model.VideoDetail{}, apperr.Internal(err)
	}
	if d == nil {
		return model.VideoDetail{}, notFound
	}
	v := s.video(d.VideoRow)
	caps := make([]model.Caption, 0, len(d.Captions))
	for _, c := range d.Captions {
		if u := s.urls.AssetString(c.Key); u != nil {
			caps = append(caps, model.Caption{Lang: c.Lang, Label: c.Label, URL: *u})
		}
	}
	stream := s.urls.Asset(d.HLSKey) // B7 replaces this with signed/tokenized playback URLs
	out := model.VideoDetail{
		ID: v.ID, Title: v.Title, ThumbnailURL: v.ThumbnailURL, DurationSec: v.DurationSec, Quality: v.Quality,
		Views: v.Views, PublishedAt: v.PublishedAt, IsLive: v.IsLive,
		Creator:     model.ChannelSummary{Creator: v.Creator, Subscribers: d.Subscribers},
		Description: d.Description, StreamURL: stream, PosterURL: v.ThumbnailURL, Captions: caps,
		Likes: int(d.Likes), Dislikes: int(d.Dislikes), Reaction: nil, CommentCount: int(d.CommentCount),
	}
	// personalise for the signed-in viewer (the principal travels in ctx)
	if err := s.markSubscribed(ctx, &out.Creator); err != nil {
		return model.VideoDetail{}, err
	}
	if me := viewerID(ctx); me != nil && s.viewer != nil {
		r, err := s.viewer.ReactionFor(ctx, d.ID, *me)
		if err != nil {
			return model.VideoDetail{}, apperr.Internal(err)
		}
		if r != "" {
			out.Reaction = &r
		}
	}
	return out, nil
}

// ---------- channels ----------

func (s *Catalog) PopularCreators(ctx context.Context) ([]model.ChannelResult, error) {
	rows, err := s.store.PopularChannels(ctx, viewerID(ctx), PopularChannels)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]model.ChannelResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.channelResult(r))
	}
	if err := s.markChannelResults(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Catalog) markChannelResults(ctx context.Context, list []model.ChannelResult) error {
	ptrs := make([]*model.ChannelSummary, len(list))
	for i := range list {
		ptrs[i] = &list[i].ChannelSummary
	}
	return s.markSubscribed(ctx, ptrs...)
}

func (s *Catalog) Channel(ctx context.Context, id string) (model.ChannelDetail, error) {
	notFound := apperr.NotFound("channel_not_found", "Channel not found")
	if !IsUUID(id) {
		return model.ChannelDetail{}, notFound
	}
	d, err := s.store.Channel(ctx, id)
	if err != nil {
		return model.ChannelDetail{}, apperr.Internal(err)
	}
	if d == nil {
		return model.ChannelDetail{}, notFound
	}
	out := model.ChannelDetail{
		ChannelResult: s.channelResult(d.ChannelRow),
		BannerURL:     s.urls.Asset(d.BannerKey),
		JoinedAt:      d.JoinedAt.UTC().Format(time.RFC3339),
		TotalViews:    d.TotalViews,
		Links:         buildLinks(d.Website, d.SocialLinks),
	}
	if err := s.markSubscribed(ctx, &out.ChannelSummary); err != nil {
		return model.ChannelDetail{}, err
	}
	return out, nil
}

// buildLinks exposes only http(s) URLs: stored values are never trusted for
// schemes like javascript:. social_links is an array of {"label","url"}.
func buildLinks(website *string, social []byte) []model.Link {
	links := []model.Link{}
	add := func(label, raw string) {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 300 || len(links) >= 8 {
			return
		}
		if label = strings.TrimSpace(label); label == "" || utf8.RuneCountInString(label) > 30 {
			return
		}
		links = append(links, model.Link{Label: label, URL: u.String()})
	}
	if website != nil {
		add("Website", *website)
	}
	var items []struct {
		Label string `json:"label"`
		URL   string `json:"url"`
	}
	if len(social) > 0 && json.Unmarshal(social, &items) == nil {
		for _, it := range items {
			add(it.Label, it.URL)
		}
	}
	return links
}

// ChannelVideos serves GET /channels/{id}/videos?kind=&sort=&cursor=.
func (s *Catalog) ChannelVideos(ctx context.Context, id, kind, sort, cursor string) (model.Page[model.Video], error) {
	if !IsUUID(id) {
		return model.Page[model.Video]{}, apperr.NotFound("channel_not_found", "Channel not found")
	}
	if kind == "" {
		kind = "videos"
	}
	if sort == "" {
		sort = "latest"
	}
	fields := map[string]string{}
	q := repository.VideoListQuery{CreatorID: id}
	f := false
	t := true
	switch kind {
	case "videos":
		q.Kind, q.Live = "LONG", &f
	case "shorts":
		q.Kind = "SHORT"
	case "live":
		q.Live = &t
	default:
		fields["kind"] = "invalid"
	}
	switch sort {
	case "latest":
		q.Order = repository.OrderLatest
	case "popular":
		q.Order = repository.OrderPopular
	default:
		fields["sort"] = "invalid"
	}
	if len(fields) > 0 {
		return model.Page[model.Video]{}, apperr.Validation(fields)
	}
	return s.listPage(ctx, q, cursor)
}

func (s *Catalog) ChannelPlaylists(ctx context.Context, id string) ([]model.Playlist, error) {
	if !IsUUID(id) {
		return nil, apperr.NotFound("channel_not_found", "Channel not found")
	}
	rows, err := s.store.ChannelPlaylists(ctx, id, 50)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]model.Playlist, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.Playlist{
			ID: r.ID, Title: r.Title, Description: r.Description, Visibility: strings.ToLower(r.Visibility),
			VideoCount: int(r.VideoCount), ThumbnailURL: s.urls.Asset(r.ThumbKey), OwnerName: r.OwnerName,
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// ---------- search ----------

type SearchParams struct{ Q, Sort, Type, Duration, Date string }

// normalizeQuery makes user text safe for PostgreSQL: invalid UTF-8 and NUL
// bytes make the driver return an error (a 500 for a simple bad request), so
// they are removed. Control characters become spaces; whitespace is collapsed.
func normalizeQuery(q string) string {
	q = strings.ToValidUTF8(q, "")
	q = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, q)
	q = strings.Join(strings.Fields(q), " ")
	if utf8.RuneCountInString(q) > MaxQueryRunes {
		q = string([]rune(q)[:MaxQueryRunes])
	}
	return q
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Search serves GET /search. Channel and playlist matches come first (a few
// for "all", every match for their own tabs), then ranked videos.
func (s *Catalog) Search(ctx context.Context, p SearchParams, cursor string) (model.SearchPage, error) {
	defaults := map[*string]string{&p.Sort: "relevance", &p.Type: "all", &p.Duration: "any", &p.Date: "any"}
	for ptr, def := range defaults {
		if *ptr == "" {
			*ptr = def
		}
	}
	fields := map[string]string{}
	if !oneOf(p.Sort, "relevance", "latest", "views") {
		fields["sort"] = "invalid"
	}
	if !oneOf(p.Type, "all", "videos", "channels", "playlists", "live") {
		fields["type"] = "invalid"
	}
	if !oneOf(p.Duration, "any", "short", "medium", "long") {
		fields["duration"] = "invalid"
	}
	if !oneOf(p.Date, "any", "today", "week", "month", "year") {
		fields["date"] = "invalid"
	}
	if len(fields) > 0 {
		return model.SearchPage{}, apperr.Validation(fields)
	}

	empty := model.SearchPage{Items: []model.SearchResult{}}
	q := normalizeQuery(p.Q)
	offset := 0
	if cursor != "" {
		var c offsetCursor
		if pagination.Decode(cursor, &c) != nil || c.K != "search" || c.O < 0 || c.O > SearchMaxOffset {
			return empty, badCursor()
		}
		offset = c.O
	}
	if q == "" {
		return empty, nil
	}

	var nonVideo []model.SearchResult
	if p.Type == "all" || p.Type == "channels" {
		limit := 2
		if p.Type == "channels" {
			limit = 50
		}
		rows, err := s.store.SearchChannels(ctx, viewerID(ctx), q, limit)
		if err != nil {
			return empty, apperr.Internal(err)
		}
		chans := make([]model.ChannelResult, 0, len(rows))
		for _, r := range rows {
			chans = append(chans, s.channelResult(r))
		}
		if err := s.markChannelResults(ctx, chans); err != nil {
			return empty, err
		}
		for i := range chans {
			nonVideo = append(nonVideo, model.SearchResult{Kind: "channel", Channel: &chans[i]})
		}
	}
	if p.Type == "all" || p.Type == "playlists" {
		limit := 1
		if p.Type == "playlists" {
			limit = 50
		}
		rows, err := s.store.SearchPlaylists(ctx, q, limit)
		if err != nil {
			return empty, apperr.Internal(err)
		}
		for _, r := range rows {
			pl := s.playlistSummary(r)
			nonVideo = append(nonVideo, model.SearchResult{Kind: "playlist", Playlist: &pl})
		}
	}

	items := []model.SearchResult{}
	if offset < len(nonVideo) {
		end := offset + SearchPageSize
		if end > len(nonVideo) {
			end = len(nonVideo)
		}
		items = append(items, nonVideo[offset:end]...)
	}
	total := len(nonVideo)

	if p.Type != "channels" && p.Type != "playlists" {
		vq := repository.SearchVideosQuery{Viewer: viewerID(ctx), Q: q, Sort: p.Sort, LiveOnly: p.Type == "live"}
		switch p.Duration {
		case "short":
			vq.MaxDur = intPtr(239)
		case "medium":
			vq.MinDur, vq.MaxDur = intPtr(240), intPtr(1200)
		case "long":
			vq.MinDur = intPtr(1201)
		}
		if span, ok := map[string]time.Duration{"today": 24 * time.Hour, "week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour, "year": 365 * 24 * time.Hour}[p.Date]; ok {
			since := s.now().Add(-span)
			vq.Since = &since
		}
		vq.Offset = offset - len(nonVideo)
		if vq.Offset < 0 {
			vq.Offset = 0
		}
		vq.Limit = SearchPageSize - len(items)
		rows, videoTotal, err := s.store.SearchVideos(ctx, vq)
		if err != nil {
			return empty, apperr.Internal(err)
		}
		for _, r := range rows {
			v := s.video(r)
			items = append(items, model.SearchResult{Kind: "video", Video: &v})
		}
		total += videoTotal
	}

	page := model.SearchPage{Items: items, Total: total}
	if next := offset + SearchPageSize; next < total && next <= SearchMaxOffset {
		c, err := pagination.Encode(offsetCursor{K: "search", O: next})
		if err != nil {
			return empty, apperr.Internal(err)
		}
		page.NextCursor = &c
	}
	return page, nil
}

func intPtr(n int) *int { return &n }

// Suggest serves GET /search/suggestions (max 8, deduplicated, prefix matches first).
func (s *Catalog) Suggest(ctx context.Context, q string) ([]string, error) {
	q = normalizeQuery(q)
	if utf8.RuneCountInString(q) < 2 {
		return []string{}, nil
	}
	titles, err := s.store.SuggestTitles(ctx, q, 8)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	names, err := s.store.SuggestChannels(ctx, q, 4)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	lq := strings.ToLower(q)
	seen := map[string]bool{}
	var prefix, rest []string
	for _, c := range append(names, titles...) {
		key := strings.ToLower(c)
		if seen[key] {
			continue
		}
		seen[key] = true
		if strings.HasPrefix(key, lq) {
			prefix = append(prefix, c)
		} else {
			rest = append(rest, c)
		}
	}
	out := append(prefix, rest...)
	if len(out) > 8 {
		out = out[:8]
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// ---------- static ----------

// ReportReasons are the options in the report dialog. Ids match the DB enum (lowercased).
func (s *Catalog) ReportReasons() []model.ReportReason {
	return []model.ReportReason{
		{ID: "non_consensual", Label: "Non-consensual content", Description: "Shared without the consent of the people shown"},
		{ID: "underage", Label: "Underage / minor concern", Description: "Anyone who may be under 18"},
		{ID: "illegal", Label: "Illegal content", Description: "Content that breaks the law"},
		{ID: "copyright", Label: "Copyright", Description: "Uses my work without permission"},
		{ID: "harassment", Label: "Harassment", Description: "Targets or threatens a person"},
		{ID: "spam", Label: "Spam or scam", Description: "Misleading, repetitive or deceptive"},
		{ID: "other", Label: "Other"},
	}
}

// Sitemap serves GET /sitemap/videos (PUBLIC videos only; UNLISTED stays out).
func (s *Catalog) Sitemap(ctx context.Context, limit int) ([]model.SitemapEntry, error) {
	if limit < 1 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.store.Sitemap(ctx, limit)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]model.SitemapEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.SitemapEntry{ID: r.ID, UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339)})
	}
	return out, nil
}
