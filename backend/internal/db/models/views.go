package models

import (
	"time"

	"github.com/google/uuid"
)

// Read models: joined shapes returned by list/detail endpoints (match the frontend contract).

type VideoCard struct {
	ID              uuid.UUID `db:"id"`
	Title           string    `db:"title"`
	DurationSeconds int32     `db:"duration_seconds"`
	PublishedAt     time.Time `db:"published_at"`
	ViewCount       int64     `db:"view_count"`
	ThumbnailKey    *string   `db:"thumb_key"`
	CreatorID       uuid.UUID `db:"creator_id"`
	CreatorName     string    `db:"display_name"`
	CreatorVerified bool      `db:"is_verified"`
	TopResolution   *string   `db:"top_resolution"` // "1080p" -> HD badge
}

type VideoDetail struct {
	Video
	ViewCount       int64         `db:"view_count"`
	LikeCount       int32         `db:"like_count"`
	DislikeCount    int32         `db:"dislike_count"`
	CommentCount    int32         `db:"comment_count"`
	SubscriberCount int32         `db:"subscriber_count"`
	CreatorName     string        `db:"display_name"`
	CreatorVerified bool          `db:"is_verified"`
	MyReaction      *ReactionType `db:"my_reaction"`
	Subscribed      bool          `db:"subscribed"`
}

type CommentView struct {
	ID         uuid.UUID `db:"id"`
	Content    string    `db:"content"`
	CreatedAt  time.Time `db:"created_at"`
	LikeCount  int32     `db:"like_count"`
	ReplyCount int32     `db:"reply_count"`
	IsPinned   bool      `db:"is_pinned"`
	AuthorID   uuid.UUID `db:"user_id"`
	AuthorName string    `db:"display_name"`
	IsCreator  bool      `db:"is_creator"`
	LikedByMe  bool      `db:"liked_by_me"`
}

type ChannelSummary struct {
	ID              uuid.UUID `db:"id"`
	DisplayName     string    `db:"display_name"`
	IsVerified      bool      `db:"is_verified"`
	AvatarKey       *string   `db:"avatar_storage_key"`
	SubscriberCount int32     `db:"subscriber_count"`
	VideoCount      int32     `db:"video_count"`
	Subscribed      bool      `db:"subscribed"`
}
