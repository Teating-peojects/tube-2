package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Report struct {
	ID              uuid.UUID    `db:"id"`
	ReporterUserID  *uuid.UUID   `db:"reporter_user_id"` // nil = anonymous reporter
	TargetType      ReportTarget `db:"target_type"`
	TargetVideoID   *uuid.UUID   `db:"target_video_id"`
	TargetCommentID *uuid.UUID   `db:"target_comment_id"`
	TargetUserID    *uuid.UUID   `db:"target_user_id"`
	Reason          ReportReason `db:"reason"`
	Description     *string      `db:"description"`
	Status          ReportStatus `db:"status"`
	Priority        int16        `db:"priority"` // set by trigger; 0 = urgent
	AssignedTo      *uuid.UUID   `db:"assigned_to"`
	ResolvedBy      *uuid.UUID   `db:"resolved_by"`
	CreatedAt       time.Time    `db:"created_at"`
	UpdatedAt       time.Time    `db:"updated_at"`
	ResolvedAt      *time.Time   `db:"resolved_at"`
}

// ModerationAction rows are append-only. Never expose an Update/Delete method for them.
type ModerationAction struct {
	ID              uuid.UUID            `db:"id"`
	ModeratorID     uuid.UUID            `db:"moderator_id"`
	ReportID        *uuid.UUID           `db:"report_id"`
	TargetType      ReportTarget         `db:"target_type"`
	TargetVideoID   *uuid.UUID           `db:"target_video_id"`
	TargetCommentID *uuid.UUID           `db:"target_comment_id"`
	TargetUserID    *uuid.UUID           `db:"target_user_id"`
	Action          ModerationActionType `db:"action"`
	Reason          string               `db:"reason"`
	Notes           *string              `db:"notes"`
	ExpiresAt       *time.Time           `db:"expires_at"`
	CreatedAt       time.Time            `db:"created_at"`
}

// AuditLog rows are append-only and must never contain secrets (the DB rejects obvious secret keys).
type AuditLog struct {
	ID          int64           `db:"id"`
	ActorUserID *uuid.UUID      `db:"actor_user_id"`
	Action      string          `db:"action"`
	TargetType  *string         `db:"target_type"`
	TargetID    *string         `db:"target_id"`
	Metadata    json.RawMessage `db:"metadata"`
	IPHash      []byte          `db:"ip_hash"`
	CreatedAt   time.Time       `db:"created_at"`
}

type VideoView struct {
	ID                   int64         `db:"id"`
	VideoID              uuid.UUID     `db:"video_id"`
	UserID               *uuid.UUID    `db:"user_id"`
	SessionID            *uuid.UUID    `db:"session_id"`
	WatchDurationSeconds int32         `db:"watch_duration_seconds"`
	CompletionPercentage float64       `db:"completion_percentage"`
	IPHash               []byte        `db:"ip_hash"`
	CountryCode          *string       `db:"country_code"`
	DeviceType           DeviceType    `db:"device_type"`
	TrafficSource        TrafficSource `db:"traffic_source"`
	CreatedAt            time.Time     `db:"created_at"`
}

type VideoDailyAnalytics struct {
	VideoID          uuid.UUID `db:"video_id"`
	Date             time.Time `db:"date"` // UTC day
	Views            int32     `db:"views"`
	WatchTimeSeconds int64     `db:"watch_time_seconds"`
	Likes            int32     `db:"likes"`
	Comments         int32     `db:"comments"`
	UniqueViewers    int32     `db:"unique_viewers"`
}

type CreatorDailyAnalytics struct {
	CreatorID        uuid.UUID `db:"creator_id"`
	Date             time.Time `db:"date"`
	Views            int64     `db:"views"`
	WatchTimeSeconds int64     `db:"watch_time_seconds"`
	Likes            int32     `db:"likes"`
	Comments         int32     `db:"comments"`
	NewSubscribers   int32     `db:"new_subscribers"`
	VideoUploads     int32     `db:"video_uploads"`
}

type CreatorDailyBreakdown struct {
	CreatorID uuid.UUID          `db:"creator_id"`
	Date      time.Time          `db:"date"`
	Dimension BreakdownDimension `db:"dimension"`
	Key       string             `db:"key"`
	Views     int64              `db:"views"`
}
