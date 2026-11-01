package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type WatchHistory struct {
	UserID               uuid.UUID `db:"user_id"`
	VideoID              uuid.UUID `db:"video_id"`
	LastPositionSeconds  int32     `db:"last_position_seconds"`
	WatchDurationSeconds int32     `db:"watch_duration_seconds"`
	Completed            bool      `db:"completed"`
	LastWatchedAt        time.Time `db:"last_watched_at"`
}

type VideoReaction struct {
	UserID    uuid.UUID    `db:"user_id"`
	VideoID   uuid.UUID    `db:"video_id"`
	Reaction  ReactionType `db:"reaction"`
	CreatedAt time.Time    `db:"created_at"`
	UpdatedAt time.Time    `db:"updated_at"`
}

type Comment struct {
	ID              uuid.UUID     `db:"id"`
	VideoID         uuid.UUID     `db:"video_id"`
	VideoCreatorID  uuid.UUID     `db:"video_creator_id"` // set from videos.creator_id, enforced by FK
	UserID          uuid.UUID     `db:"user_id"`
	ParentCommentID *uuid.UUID    `db:"parent_comment_id"`
	Content         string        `db:"content"` // plain text; escape on output, never render as HTML
	Status          CommentStatus `db:"status"`
	IsPinned        bool          `db:"is_pinned"`
	LikeCount       int32         `db:"like_count"`
	ReplyCount      int32         `db:"reply_count"`
	CreatedAt       time.Time     `db:"created_at"`
	UpdatedAt       time.Time     `db:"updated_at"`
	DeletedAt       *time.Time    `db:"deleted_at"`
}

type CommentReaction struct {
	UserID    uuid.UUID    `db:"user_id"`
	CommentID uuid.UUID    `db:"comment_id"`
	Reaction  ReactionType `db:"reaction"`
	CreatedAt time.Time    `db:"created_at"`
}

type Subscription struct {
	SubscriberID uuid.UUID `db:"subscriber_id"`
	CreatorID    uuid.UUID `db:"creator_id"`
	CreatedAt    time.Time `db:"created_at"`
}

type Playlist struct {
	ID               uuid.UUID          `db:"id"`
	UserID           uuid.UUID          `db:"user_id"`
	Title            string             `db:"title"`
	Description      string             `db:"description"`
	Visibility       PlaylistVisibility `db:"visibility"`
	ThumbnailVideoID *uuid.UUID         `db:"thumbnail_video_id"`
	CreatedAt        time.Time          `db:"created_at"`
	UpdatedAt        time.Time          `db:"updated_at"`
	DeletedAt        *time.Time         `db:"deleted_at"`
}

type PlaylistVideo struct {
	PlaylistID uuid.UUID `db:"playlist_id"`
	VideoID    uuid.UUID `db:"video_id"`
	Position   int32     `db:"position"`
	AddedAt    time.Time `db:"added_at"`
}

type WatchLater struct {
	UserID    uuid.UUID `db:"user_id"`
	VideoID   uuid.UUID `db:"video_id"`
	CreatedAt time.Time `db:"created_at"`
}

type BlockedUser struct {
	UserID        uuid.UUID `db:"user_id"`
	BlockedUserID uuid.UUID `db:"blocked_user_id"`
	CreatedAt     time.Time `db:"created_at"`
}

type NotInterested struct {
	UserID    uuid.UUID `db:"user_id"`
	VideoID   uuid.UUID `db:"video_id"`
	CreatedAt time.Time `db:"created_at"`
}

type Notification struct {
	ID          int64            `db:"id"`
	UserID      uuid.UUID        `db:"user_id"`
	Type        NotificationType `db:"type"`
	ActorUserID *uuid.UUID       `db:"actor_user_id"`
	VideoID     *uuid.UUID       `db:"video_id"`
	CommentID   *uuid.UUID       `db:"comment_id"`
	Payload     json.RawMessage  `db:"payload"`
	ReadAt      *time.Time       `db:"read_at"`
	IsRead      bool             `db:"is_read"` // generated column: read-only
	CreatedAt   time.Time        `db:"created_at"`
}

type SearchHistory struct {
	ID        int64     `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	Query     string    `db:"query"`
	CreatedAt time.Time `db:"created_at"`
}
