// Package model holds the JSON shapes returned to the frontend. They mirror
// the frontend's src/types/index.ts exactly; change both sides together.
package model

type Creator struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
	Verified  bool    `json:"verified"`
}

type Video struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	DurationSec  int     `json:"durationSec"`
	Quality      string  `json:"quality"` // SD | HD | 4K
	Views        int64   `json:"views"`
	PublishedAt  string  `json:"publishedAt"` // RFC 3339
	IsLive       bool    `json:"isLive"`
	Creator      Creator `json:"creator"`
}

// Page is the cursor-paginated list wrapper ({items, nextCursor}).
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

type ChannelSummary struct {
	Creator
	Subscribers  int64 `json:"subscribers"`
	IsSubscribed bool  `json:"isSubscribed"`
}

type Caption struct {
	Lang  string `json:"lang"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type VideoDetail struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	ThumbnailURL *string        `json:"thumbnailUrl"`
	DurationSec  int            `json:"durationSec"`
	Quality      string         `json:"quality"`
	Views        int64          `json:"views"`
	PublishedAt  string         `json:"publishedAt"`
	IsLive       bool           `json:"isLive"`
	Creator      ChannelSummary `json:"creator"`
	Description  string         `json:"description"`
	StreamURL    *string        `json:"streamUrl"`
	PosterURL    *string        `json:"posterUrl"`
	Captions     []Caption      `json:"captions"`
	Likes        int            `json:"likes"`
	Dislikes     int            `json:"dislikes"`
	Reaction     *string        `json:"reaction"` // "like" | "dislike" | null
	CommentCount int            `json:"commentCount"`
}

type ChannelResult struct {
	ChannelSummary
	VideoCount  int    `json:"videoCount"`
	Description string `json:"description"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type ChannelDetail struct {
	ChannelResult
	BannerURL  *string `json:"bannerUrl"`
	JoinedAt   string  `json:"joinedAt"`
	TotalViews int64   `json:"totalViews"`
	Links      []Link  `json:"links"`
}

type PlaylistSummary struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	VideoCount   int     `json:"videoCount"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	CreatorName  string  `json:"creatorName"`
}

type Playlist struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Visibility   string  `json:"visibility"` // public | private
	VideoCount   int     `json:"videoCount"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	OwnerName    string  `json:"ownerName"`
	UpdatedAt    string  `json:"updatedAt"`
}

type SearchResult struct {
	Kind     string           `json:"kind"` // video | channel | playlist
	Video    *Video           `json:"video,omitempty"`
	Channel  *ChannelResult   `json:"channel,omitempty"`
	Playlist *PlaylistSummary `json:"playlist,omitempty"`
}

type SearchPage struct {
	Items      []SearchResult `json:"items"`
	NextCursor *string        `json:"nextCursor"`
	Total      int            `json:"total"`
}

type ReportReason struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type SitemapEntry struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updatedAt"`
}

// ---------- accounts ----------

type User struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	DisplayName   string  `json:"displayName"`
	Email         string  `json:"email"`
	AvatarURL     *string `json:"avatarUrl"`
	EmailVerified bool    `json:"emailVerified"`
	IsCreator     bool    `json:"isCreator"`
	JoinedAt      string  `json:"joinedAt"`
	Bio           string  `json:"bio"`
}

type PrivacySettings struct {
	ShowSubscriptions bool `json:"showSubscriptions"`
	ShowLikedVideos   bool `json:"showLikedVideos"`
	SaveWatchHistory  bool `json:"saveWatchHistory"`
}

type NotificationSettings struct {
	NewVideo      bool `json:"newVideo"`
	Replies       bool `json:"replies"`
	NewSubscriber bool `json:"newSubscriber"`
	System        bool `json:"system"`
	Email         bool `json:"email"`
}

type Settings struct {
	Privacy       PrivacySettings      `json:"privacy"`
	Notifications NotificationSettings `json:"notifications"`
}

// SettingsPatch is the partial, nested update body: unknown keys are rejected.
type SettingsPatch struct {
	Privacy *struct {
		ShowSubscriptions *bool `json:"showSubscriptions"`
		ShowLikedVideos   *bool `json:"showLikedVideos"`
		SaveWatchHistory  *bool `json:"saveWatchHistory"`
	} `json:"privacy"`
	Notifications *struct {
		NewVideo      *bool `json:"newVideo"`
		Replies       *bool `json:"replies"`
		NewSubscriber *bool `json:"newSubscriber"`
		System        *bool `json:"system"`
		Email         *bool `json:"email"`
	} `json:"notifications"`
}

// ---------- social ----------

type CommentAuthor struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
	IsCreator bool    `json:"isCreator"` // the author is the video's creator ("Creator" badge)
}

type Comment struct {
	ID         string        `json:"id"`
	Author     CommentAuthor `json:"author"`
	Text       string        `json:"text"` // plain text; the frontend renders it as text, never HTML
	CreatedAt  string        `json:"createdAt"`
	Likes      int           `json:"likes"`
	Liked      bool          `json:"liked"`
	ReplyCount int           `json:"replyCount"`
	Pinned     bool          `json:"pinned"`
}

type HistoryItem struct {
	Video       Video  `json:"video"`
	WatchedAt   string `json:"watchedAt"`
	ProgressSec int    `json:"progressSec"`
}

type PlaylistWithMembership struct {
	Playlist
	Contains bool `json:"contains"`
}

type PlaylistDetail struct {
	Playlist
	IsOwner bool    `json:"isOwner"`
	Videos  []Video `json:"videos"`
}
