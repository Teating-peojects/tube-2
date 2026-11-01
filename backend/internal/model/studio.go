package model

// JSON shapes for upload, notifications, Studio and moderation. They mirror the
// frontend's src/types/index.ts (Upload & Studio, notifications); change both
// sides together.

// ---------- upload ----------

type UploadPart struct {
	PartNumber int    `json:"partNumber"`
	URL        string `json:"url"`
}

// UploadInit tells the browser where to PUT each part of the file directly.
type UploadInit struct {
	UploadID string       `json:"uploadId"`
	VideoID  string       `json:"videoId"`
	PartSize int64        `json:"partSize"`
	Parts    []UploadPart `json:"parts"`
}

type UploadStatus struct {
	Status     string   `json:"status"` // processing | encoding | ready | published | failed
	Progress   int      `json:"progress"`
	Thumbnails []string `json:"thumbnails"`
	Error      *string  `json:"error,omitempty"`
}

type AssetInit struct {
	AssetID string `json:"assetId"`
	URL     string `json:"url"`
}

// ---------- notifications ----------

type Notification struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // new_subscriber | new_comment | comment_reply | creator_upload | system | processing_completed | processing_failed
	Title     string `json:"title"`
	Body      string `json:"body"`
	Href      string `json:"href"`
	CreatedAt string `json:"createdAt"`
	Read      bool   `json:"read"`
}

type UnreadCount struct {
	Count int `json:"count"`
}

// ---------- Studio ----------

type SeriesPoint struct {
	Date  string  `json:"date"` // YYYY-MM-DD (UTC)
	Value float64 `json:"value"`
}

type Stat struct {
	Label string `json:"label"`
	Value int    `json:"value"` // a share of the total, in percent
}

type StudioTotals struct {
	Views            int64    `json:"views"`
	WatchTimeHours   float64  `json:"watchTimeHours"`
	Subscribers      int64    `json:"subscribers"`
	SubscribersDelta int64    `json:"subscribersDelta"`
	Revenue          *float64 `json:"revenue"` // null: there is no monetisation
}

type StudioTopVideo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Views int64  `json:"views"`
}

type StudioOverview struct {
	Period    string           `json:"period"`
	Totals    StudioTotals     `json:"totals"`
	Views     []SeriesPoint    `json:"views"`
	TopVideos []StudioTopVideo `json:"topVideos"`
}

type StudioAudience struct {
	Countries []Stat `json:"countries"`
	Devices   []Stat `json:"devices"`
}

type StudioEngagement struct {
	Likes              int64 `json:"likes"`
	Comments           int64 `json:"comments"`
	Shares             int64 `json:"shares"`
	AvgViewDurationSec int64 `json:"avgViewDurationSec"`
}

type StudioAnalytics struct {
	Period     string           `json:"period"`
	Views      []SeriesPoint    `json:"views"`
	WatchTime  []SeriesPoint    `json:"watchTime"` // hours
	Audience   StudioAudience   `json:"audience"`
	Traffic    []Stat           `json:"traffic"`
	Engagement StudioEngagement `json:"engagement"`
}

type StudioVideo struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	DurationSec  int     `json:"durationSec"`
	Status       string  `json:"status"`     // published | draft | processing | failed
	Visibility   string  `json:"visibility"` // public | unlisted | private
	CreatedAt    string  `json:"createdAt"`
	Views        int64   `json:"views"`
	Comments     int64   `json:"comments"`
	Likes        int64   `json:"likes"`
}

type StudioComment struct {
	ID         string `json:"id"`
	VideoID    string `json:"videoId"`
	VideoTitle string `json:"videoTitle"`
	Author     string `json:"author"`
	Text       string `json:"text"`
	CreatedAt  string `json:"createdAt"`
	Status     string `json:"status"` // published | held
}
