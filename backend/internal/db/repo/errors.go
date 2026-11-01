// Package repo defines the persistence interfaces the Go services depend on. Implement them with pgx
// (or generate with sqlc); keep SQL out of handlers and services.
package repo

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")  // unique violation (23505)
	ErrReference = errors.New("reference") // foreign key violation (23503)
	ErrInvalid   = errors.New("invalid")   // check / not-null violation (23514, 23502)
	ErrRateLimit = errors.New("rate limited")
	ErrForbidden = errors.New("forbidden")
)

// Constraint names the API maps to specific client errors (see migrations).
const (
	ConstraintUsername       = "users_username_key"
	ConstraintEmail          = "users_email_key"
	ConstraintPhone          = "users_phone_key"
	ConstraintReportOpenVid  = "reports_open_video_once"   // -> 409 already_reported
	ConstraintReportOpenCom  = "reports_open_comment_once" // -> 409 already_reported
	ConstraintReportOpenUser = "reports_open_user_once"    // -> 409 already_reported
	ConstraintVideoDuration  = "videos_type_matches_duration"
	ConstraintVideoSize      = "videos_size_limits"
	ConstraintMetadata1080p  = "video_metadata_max_1080p"
	ConstraintActiveJob      = "video_jobs_one_active"
	ConstraintOnePinned      = "comments_one_pinned"
)

// MapError turns pgx/Postgres errors into the sentinel errors above while keeping the original error
// in the chain (Go 1.20+ multi-%w), so ConstraintOf and errors.As still work.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return fmt.Errorf("%w: %w", ErrConflict, err)
		case "23503":
			return fmt.Errorf("%w: %w", ErrReference, err)
		case "23514", "23502", "22001", "22P02":
			return fmt.Errorf("%w: %w", ErrInvalid, err)
		}
	}
	return err
}

// ConstraintOf returns the violated constraint name ("" if the error is not a constraint violation).
// Example: errors.Is(err, ErrConflict) && ConstraintOf(err) == ConstraintEmail  ->  "email already registered".
func ConstraintOf(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.ConstraintName
	}
	return ""
}
