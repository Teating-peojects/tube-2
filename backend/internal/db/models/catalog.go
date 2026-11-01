package models

import (
	"time"

	"github.com/google/uuid"
)

type Category struct {
	ID                  int16     `db:"id"`
	Name                string    `db:"name"`
	Slug                string    `db:"slug"`
	Description         *string   `db:"description"`
	ThumbnailStorageKey *string   `db:"thumbnail_storage_key"`
	IsActive            bool      `db:"is_active"`
	SortOrder           int32     `db:"sort_order"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

type Tag struct {
	ID         int64     `db:"id"`
	Name       string    `db:"name"`
	Slug       string    `db:"slug"`
	UsageCount int32     `db:"usage_count"`
	CreatedAt  time.Time `db:"created_at"`
}

type Video struct {
	ID                 uuid.UUID        `db:"id"`
	CreatorID          uuid.UUID        `db:"creator_id"`
	Title              string           `db:"title"`
	Slug               *string          `db:"slug"`
	Description        string           `db:"description"`
	VideoType          *VideoType       `db:"video_type"` // nil until the METADATA job has run
	Visibility         VideoVisibility  `db:"visibility"`
	Status             VideoStatus      `db:"status"`
	ModerationStatus   ModerationStatus `db:"moderation_status"`
	DurationSeconds    *int32           `db:"duration_seconds"`
	FileSizeBytes      *int64           `db:"file_size_bytes"`
	OriginalFilename   *string          `db:"original_filename"`
	OriginalStorageKey *string          `db:"original_storage_key"`
	HLSMasterKey       *string          `db:"hls_master_key"`
	CategoryID         *int16           `db:"category_id"`
	Language           *string          `db:"language"`
	AgeRestricted      bool             `db:"age_restricted"`
	CommentsMode       CommentMode      `db:"comments_mode"`
	PublishedAt        *time.Time       `db:"published_at"`
	CreatedAt          time.Time        `db:"created_at"`
	UpdatedAt          time.Time        `db:"updated_at"`
	DeletedAt          *time.Time       `db:"deleted_at"`
}

// IsPubliclyListed mirrors the SQL predicate used by every discovery query and partial index.
func (v Video) IsPubliclyListed() bool {
	return v.Status == VideoPublished && v.Visibility == VisibilityPublic &&
		v.ModerationStatus == ModerationOK && v.DeletedAt == nil
}

type VideoStats struct {
	VideoID          uuid.UUID `db:"video_id"`
	ViewCount        int64     `db:"view_count"`
	LikeCount        int32     `db:"like_count"`
	DislikeCount     int32     `db:"dislike_count"`
	CommentCount     int32     `db:"comment_count"`
	WatchTimeSeconds int64     `db:"watch_time_seconds"`
	UpdatedAt        time.Time `db:"updated_at"`
}

type VideoTag struct {
	VideoID uuid.UUID `db:"video_id"`
	TagID   int64     `db:"tag_id"`
}

type VideoMetadata struct {
	VideoID     uuid.UUID `db:"video_id"`
	Width       int32     `db:"width"`
	Height      int32     `db:"height"`
	FPS         *float64  `db:"fps"`
	VideoCodec  *string   `db:"video_codec"`
	AudioCodec  *string   `db:"audio_codec"`
	Bitrate     *int64    `db:"bitrate"`
	Container   *string   `db:"container"`
	AspectRatio *float64  `db:"aspect_ratio"` // generated column: read-only
	HasAudio    bool      `db:"has_audio"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type VideoVariant struct {
	ID            uuid.UUID       `db:"id"`
	VideoID       uuid.UUID       `db:"video_id"`
	Resolution    VideoResolution `db:"resolution"`
	Width         *int32          `db:"width"`
	Height        *int32          `db:"height"`
	Bitrate       *int32          `db:"bitrate"`
	Codec         *string         `db:"codec"`
	StorageKey    string          `db:"storage_key"`
	ManifestPath  *string         `db:"manifest_path"`
	FileSizeBytes *int64          `db:"file_size_bytes"`
	Status        VariantStatus   `db:"status"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
}

type VideoThumbnail struct {
	ID         uuid.UUID     `db:"id"`
	VideoID    uuid.UUID     `db:"video_id"`
	StorageKey string        `db:"storage_key"`
	Width      *int32        `db:"width"`
	Height     *int32        `db:"height"`
	Type       ThumbnailType `db:"type"`
	Source     AssetSource   `db:"source"`
	Status     AssetStatus   `db:"status"`
	IsPrimary  bool          `db:"is_primary"`
	CreatedAt  time.Time     `db:"created_at"`
}

type VideoCaption struct {
	ID         uuid.UUID   `db:"id"`
	VideoID    uuid.UUID   `db:"video_id"`
	Language   string      `db:"language"`
	Label      *string     `db:"label"`
	StorageKey string      `db:"storage_key"`
	Status     AssetStatus `db:"status"`
	CreatedAt  time.Time   `db:"created_at"`
}

type UploadSession struct {
	ID                  uuid.UUID    `db:"id"`
	UserID              uuid.UUID    `db:"user_id"`
	VideoID             uuid.UUID    `db:"video_id"`
	StorageKey          string       `db:"storage_key"`
	ProviderUploadID    string       `db:"provider_upload_id"`
	DeclaredSizeBytes   int64        `db:"declared_size_bytes"`
	DeclaredContentType string       `db:"declared_content_type"`
	PartSizeBytes       int32        `db:"part_size_bytes"`
	PartCount           int32        `db:"part_count"`
	Status              UploadStatus `db:"status"`
	ExpiresAt           time.Time    `db:"expires_at"`
	CreatedAt           time.Time    `db:"created_at"`
	UpdatedAt           time.Time    `db:"updated_at"`
	CompletedAt         *time.Time   `db:"completed_at"`
}

type ProcessingJob struct {
	ID           uuid.UUID  `db:"id"`
	VideoID      uuid.UUID  `db:"video_id"`
	JobType      JobType    `db:"job_type"`
	Status       JobStatus  `db:"status"`
	Attempts     int16      `db:"attempts"`
	MaxAttempts  int16      `db:"max_attempts"`
	ErrorMessage *string    `db:"error_message"`
	WorkerID     *string    `db:"worker_id"`
	HeartbeatAt  *time.Time `db:"heartbeat_at"`
	ScheduledAt  time.Time  `db:"scheduled_at"`
	StartedAt    *time.Time `db:"started_at"`
	CompletedAt  *time.Time `db:"completed_at"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}
