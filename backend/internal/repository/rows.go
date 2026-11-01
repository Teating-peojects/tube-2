// Package repository contains all SQL, written against the schema in db/migrations.
// It returns plain rows (storage keys, timestamps); URL building and JSON shaping
// happen in the service layer.
package repository

import "time"

// Every public query uses exactly this predicate so the partial indexes apply.
// UNLISTED/PRIVATE/draft/restricted/removed videos and inactive creators are never listed.
const publicVideo = `v.status = 'PUBLISHED' AND v.visibility = 'PUBLIC' AND v.moderation_status = 'OK' AND v.published_at IS NOT NULL AND v.deleted_at IS NULL`
const activeCreator = `u.status = 'ACTIVE' AND u.deleted_at IS NULL`

type VideoRow struct {
	ID          string
	Title       string
	DurationSec *int32
	ShortSide   *int32 // LEAST(width, height) of the source: drives the SD/HD badge
	Views       int64
	PublishedAt time.Time
	IsLive      bool // always false: live streaming has no tables yet
	CreatorID   string
	CreatorName string
	AvatarKey   *string
	Verified    bool
	ThumbKey    *string
}

type CaptionRow struct {
	Lang, Label, Key string
}

type VideoDetailRow struct {
	VideoRow
	Description  string
	Likes        int32
	Dislikes     int32
	CommentCount int32
	Subscribers  int64
	HLSKey       *string // storage key of the master playlist (nil until processed)
	Captions     []CaptionRow
}

type ChannelRow struct {
	ID          string
	Name        string
	AvatarKey   *string
	Verified    bool
	Subscribers int64
	Bio         string
	VideoCount  int64
}

type ChannelDetailRow struct {
	ChannelRow
	BannerKey   *string
	JoinedAt    time.Time
	TotalViews  int64
	Website     *string
	SocialLinks []byte // raw jsonb: [{"label": "...", "url": "https://..."}]
}

type PlaylistRow struct {
	ID          string
	Title       string
	Description string
	Visibility  string
	VideoCount  int64
	ThumbKey    *string
	OwnerName   string
	UpdatedAt   time.Time
}

type SitemapRow struct {
	ID        string
	UpdatedAt time.Time
}

// Keyset is the position of the last row of the previous page.
type Keyset struct {
	Time time.Time // latest order
	Num  int64     // popular order
	ID   string
}

type Order string

const (
	OrderLatest  Order = "latest"
	OrderPopular Order = "popular"
)

type VideoListQuery struct {
	Order        Order
	Kind         string // "LONG" | "SHORT" | "" (any)
	Live         *bool  // live streaming is not built: true yields nothing, false/nil is a no-op
	CategorySlug string
	CreatorID    string  // validated uuid or ""
	Viewer       *string // signed-in viewer: hides blocked channels and "not interested" videos (not on a channel's own page)
	After        *Keyset
	Limit        int
}

type SearchVideosQuery struct {
	Viewer   *string
	Q        string
	Sort     string // relevance | latest | views
	LiveOnly bool
	MinDur   *int
	MaxDur   *int
	Since    *time.Time
	Offset   int
	Limit    int
}
