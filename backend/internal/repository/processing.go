package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Processing controls a video's processing pipeline from the API (Phase 6): cancel while it runs,
// re-queue after it failed. Both lock the video row, change video and jobs in one transaction and write
// the audit entry in that same transaction, so an action can never exist without its evidence.
type Processing struct{ pool *pgxpool.Pool }

func NewProcessing(pool *pgxpool.Pool) *Processing { return &Processing{pool: pool} }

const (
	qProcVideoLock = `SELECT creator_id::text, status::text, moderation_status::text, deleted_at IS NOT NULL, COALESCE(original_storage_key, '')
FROM videos WHERE id = $1::uuid FOR UPDATE`
	// The worker notices the lost lease at its next heartbeat and kills ffmpeg (process group); any
	// result it still tries to write is refused because the job is no longer PROCESSING.
	qProcCancelJobs = `UPDATE video_processing_jobs SET status = 'CANCELLED', error_message = 'cancelled: Processing was cancelled',
  heartbeat_at = NULL, updated_at = now() WHERE video_id = $1::uuid AND status IN ('QUEUED', 'PROCESSING')`
	qProcSetStatus  = `UPDATE videos SET status = $2::video_status, updated_at = now() WHERE id = $1::uuid`
	qProcSessionOK  = `SELECT EXISTS (SELECT 1 FROM upload_sessions WHERE video_id = $1::uuid AND status = 'COMPLETED')`
	qProcLastError  = `SELECT COALESCE(error_message, '') FROM video_processing_jobs WHERE video_id = $1::uuid AND status IN ('FAILED', 'CANCELLED') ORDER BY updated_at DESC LIMIT 1`
	qProcDeleteJobs = `DELETE FROM video_processing_jobs WHERE video_id = $1::uuid`
	qProcRequeue    = `UPDATE videos SET status = 'PROCESSING', outputs_cleaned_at = NULL, updated_at = now() WHERE id = $1::uuid`
	qProcNewJob     = `INSERT INTO video_processing_jobs (video_id, job_type) VALUES ($1::uuid, 'METADATA')`
)

type procVideo struct {
	creator, status, moderation, original string
	deleted                               bool
}

func (r *Processing) lock(ctx context.Context, tx pgx.Tx, videoID string) (procVideo, error) {
	var v procVideo
	err := tx.QueryRow(ctx, qProcVideoLock, videoID).Scan(&v.creator, &v.status, &v.moderation, &v.deleted, &v.original)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrTargetNotFound
	}
	return v, err
}

// Cancel stops a video that is being processed. The creator may cancel their own video; staff may
// cancel any (staff=true). Anyone else gets ErrTargetNotFound, so another creator's video id is never
// confirmed to exist. Only a PROCESSING video can be cancelled (ErrBadState otherwise).
func (r *Processing) Cancel(ctx context.Context, videoID, actorID string, staff bool, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	v, err := r.lock(ctx, tx, videoID)
	if err != nil {
		return err
	}
	if v.deleted || (!staff && v.creator != actorID) {
		return ErrTargetNotFound
	}
	if v.status != "PROCESSING" {
		return ErrBadState
	}
	tag, err := tx.Exec(ctx, qProcCancelJobs, videoID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qProcSetStatus, videoID, "FAILED"); err != nil {
		return err
	}
	a.Meta = withMeta(a.Meta, "jobs_cancelled", tag.RowsAffected())
	if err := audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Retry puts a FAILED video back into the pipeline from the first step. It needs a finished upload
// whose original is still in storage, and refuses videos removed by moderation (restore them first).
// Old job rows are replaced so the progress shown to the creator starts from zero; the previous failure
// goes into the audit entry.
func (r *Processing) Retry(ctx context.Context, videoID string, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	v, err := r.lock(ctx, tx, videoID)
	if err != nil {
		return err
	}
	if v.deleted {
		return ErrTargetNotFound
	}
	if v.status != "FAILED" || v.moderation == "REMOVED" || v.original == "" {
		return ErrBadState
	}
	var ok bool
	if err := tx.QueryRow(ctx, qProcSessionOK, videoID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrBadState // the upload never completed: there is nothing to process
	}
	var prev string
	if err := tx.QueryRow(ctx, qProcLastError, videoID).Scan(&prev); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(ctx, qProcDeleteJobs, videoID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qProcRequeue, videoID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qProcNewJob, videoID); err != nil {
		return err
	}
	a.Meta = withMeta(a.Meta, "previous_error", prev)
	if err := audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func withMeta(m map[string]any, k string, v any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for a, b := range m {
		out[a] = b
	}
	out[k] = v
	return out
}

// ProcInfo is the raw state behind GET /videos/{id}/processing.
type ProcInfo struct {
	CreatorID, VideoStatus, Moderation, Profile string
	Deleted                                     bool
	UpdatedAt                                   time.Time
	JobsDone                                    int
	ActiveJob                                   string // job type that is queued or running, "" if none
	ActiveStatus                                string // QUEUED or PROCESSING when ActiveJob is set
	FailCode                                    string // stable code of the latest failed or cancelled job
	Renditions                                  []string
	Thumbnails                                  int
}

// Info reads everything the processing view needs. Authorization is the caller's job (service layer).
func (r *Processing) Info(ctx context.Context, videoID string) (ProcInfo, error) {
	var v ProcInfo
	err := r.pool.QueryRow(ctx, `SELECT creator_id::text, status::text, moderation_status::text, COALESCE(processing_profile, ''),
  deleted_at IS NOT NULL, updated_at FROM videos WHERE id = $1::uuid`, videoID).
		Scan(&v.CreatorID, &v.VideoStatus, &v.Moderation, &v.Profile, &v.Deleted, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrTargetNotFound
	}
	if err != nil {
		return v, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND status = 'COMPLETED')::int,
  COALESCE((SELECT job_type::text FROM video_processing_jobs WHERE video_id = $1::uuid AND status IN ('QUEUED', 'PROCESSING') ORDER BY created_at LIMIT 1), ''),
  COALESCE((SELECT status::text FROM video_processing_jobs WHERE video_id = $1::uuid AND status IN ('QUEUED', 'PROCESSING') ORDER BY created_at LIMIT 1), ''),
  COALESCE((SELECT split_part(COALESCE(error_message, ''), ':', 1) FROM video_processing_jobs
            WHERE video_id = $1::uuid AND status IN ('FAILED', 'CANCELLED') ORDER BY updated_at DESC LIMIT 1), ''),
  (SELECT count(*) FROM video_thumbnails WHERE video_id = $1::uuid AND status = 'READY')::int`, videoID).
		Scan(&v.JobsDone, &v.ActiveJob, &v.ActiveStatus, &v.FailCode, &v.Thumbnails); err != nil {
		return v, err
	}
	rows, err := r.pool.Query(ctx, `SELECT resolution::text FROM video_variants WHERE video_id = $1::uuid AND status = 'READY' ORDER BY height`, videoID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return v, err
		}
		v.Renditions = append(v.Renditions, name)
	}
	return v, rows.Err()
}
