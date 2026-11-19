package processing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the job queue on PostgreSQL. Every state change goes through CanTransition and is fenced by
// worker_id, so a stale worker that wakes up late can never overwrite a job another worker has claimed.
// All times come from the database clock (now()), never from the worker's machine.
type Store struct{ pool *pgxpool.Pool }

// NewStore returns a Store over a pool. The worker must connect as the redxtube_worker role.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const returningJob = `RETURNING j.id::text, j.video_id::text, j.job_type::text, j.attempts, j.max_attempts,
  j.processing_version, j.cancel_requested_at, COALESCE(j.worker_id, '')`

const qClaim = `WITH c AS (
  SELECT j.id FROM video_processing_jobs j
  JOIN videos v ON v.id = j.video_id
  WHERE j.status = 'QUEUED' AND j.scheduled_at <= now() AND j.attempts < j.max_attempts
    AND j.job_type::text = ANY($2::text[])
    AND v.status = 'PROCESSING' AND v.deleted_at IS NULL
  ORDER BY j.scheduled_at, j.created_at
  FOR UPDATE OF j SKIP LOCKED LIMIT 1)
UPDATE video_processing_jobs j SET status = 'PROCESSING', worker_id = $1, started_at = now(), heartbeat_at = now(),
  attempts = j.attempts + 1, stage = NULL, progress = 0, error_code = NULL, updated_at = now()
FROM c WHERE j.id = c.id ` + returningJob

func scanJob(row pgx.Row) (*Job, error) {
	var j Job
	if err := row.Scan(&j.ID, &j.VideoID, &j.Type, &j.Attempts, &j.MaxAttempts, &j.ProcessingVersion, &j.CancelRequestedAt, &j.WorkerID); err != nil {
		return nil, err
	}
	return &j, nil
}

// Claim leases the oldest runnable job of one of the given types to workerID, or returns (nil, nil) when there is
// none. Jobs of videos that are deleted or no longer PROCESSING are never claimed (see FailOrphans).
// Claiming consumes one attempt, so a job that keeps killing its worker cannot loop forever.
func (s *Store) Claim(ctx context.Context, workerID string, types []string) (*Job, error) {
	if workerID == "" || len(types) == 0 {
		return nil, errors.New("processing: Claim needs a worker id and at least one job type")
	}
	j, err := scanJob(s.pool.QueryRow(ctx, qClaim, workerID, types))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// Heartbeat refreshes the lease and records stage and progress (progress never goes backwards). It returns whether
// cancellation was requested. ErrLeaseLost means the job is no longer this worker's: stop working on it.
func (s *Store) Heartbeat(ctx context.Context, jobID, workerID, stage string, progress int) (cancelRequested bool, err error) {
	var st *string
	if stage != "" {
		st = &stage
	}
	progress = max(0, min(100, progress))
	var cancelAt *time.Time
	err = s.pool.QueryRow(ctx, `UPDATE video_processing_jobs SET heartbeat_at = now(), updated_at = now(),
  stage = COALESCE($3, stage), progress = GREATEST(progress, $4::smallint)
WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING' RETURNING cancel_requested_at`, jobID, workerID, st, progress).Scan(&cancelAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrLeaseLost
	}
	return cancelAt != nil, err
}

// Complete marks the job COMPLETED. Calling it again for an already completed job of the same worker is a no-op.
func (s *Store) Complete(ctx context.Context, jobID, workerID string) error {
	if !CanTransition(Processing, Completed) {
		return ErrInvalidTransition
	}
	tag, err := s.pool.Exec(ctx, `UPDATE video_processing_jobs SET status = 'COMPLETED', completed_at = now(), progress = 100,
  heartbeat_at = now(), error_code = NULL, updated_at = now()
WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING'`, jobID, workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var status, owner string
	err = s.pool.QueryRow(ctx, `SELECT status::text, COALESCE(worker_id, '') FROM video_processing_jobs WHERE id = $1::uuid`, jobID).Scan(&status, &owner)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	case status == Completed && owner == workerID:
		return nil // idempotent re-delivery
	case status == Processing || owner != workerID:
		return ErrLeaseLost
	default:
		return ErrInvalidTransition
	}
}

// lock reads one job FOR UPDATE inside tx.
func lock(ctx context.Context, tx pgx.Tx, jobID string) (status, worker string, attempts, maxAttempts int, cancelAt *time.Time, err error) {
	err = tx.QueryRow(ctx, `SELECT status::text, COALESCE(worker_id, ''), attempts, max_attempts, cancel_requested_at
FROM video_processing_jobs WHERE id = $1::uuid FOR UPDATE`, jobID).Scan(&status, &worker, &attempts, &maxAttempts, &cancelAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const qToQueued = `UPDATE video_processing_jobs SET status = 'QUEUED', worker_id = NULL, started_at = NULL, heartbeat_at = NULL,
  stage = NULL, progress = 0, scheduled_at = now() + make_interval(secs => $2::float8), attempts = GREATEST(attempts - $3::int, 0),
  error_code = $4, updated_at = now() WHERE id = $1::uuid`
const qToFailed = `UPDATE video_processing_jobs SET status = 'FAILED', error_code = $2, completed_at = NULL, updated_at = now() WHERE id = $1::uuid`

// Fail ends one attempt of a job this worker holds. A transient failure re-queues it after `delay` (the caller picks
// it with Backoff) until max_attempts is reached; a permanent failure, an exhausted budget or a pending cancel
// request makes it FAILED. code must be a safe code. The resulting status is returned.
func (s *Store) Fail(ctx context.Context, jobID, workerID, code string, permanent bool, delay time.Duration) (string, error) {
	if code == "" {
		code = CodeInternal
	}
	var out string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		status, owner, attempts, maxAttempts, cancelAt, err := lock(ctx, tx, jobID)
		if err != nil {
			return err
		}
		if status != Processing || owner != workerID {
			return ErrLeaseLost
		}
		switch {
		case cancelAt != nil:
			code, permanent = CodeCancelled, true
		case attempts >= maxAttempts:
			permanent = true
		}
		out = Queued
		if permanent {
			out = Failed
		}
		if !CanTransition(Processing, out) {
			return ErrInvalidTransition
		}
		if permanent {
			_, err = tx.Exec(ctx, qToFailed, jobID, code)
		} else {
			_, err = tx.Exec(ctx, qToQueued, jobID, delay.Seconds(), 0, code)
		}
		return err
	})
	return out, err
}

// Release hands a job back without blaming it (graceful shutdown): it is re-queued at once and the attempt that the
// claim consumed is refunded. A job with a pending cancel request becomes FAILED(cancelled) instead.
func (s *Store) Release(ctx context.Context, jobID, workerID string) (string, error) {
	var out string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		status, owner, _, _, cancelAt, err := lock(ctx, tx, jobID)
		if err != nil {
			return err
		}
		if status != Processing || owner != workerID {
			return ErrLeaseLost
		}
		if cancelAt != nil {
			out = Failed
			_, err = tx.Exec(ctx, qToFailed, jobID, CodeCancelled)
			return err
		}
		out = Queued
		_, err = tx.Exec(ctx, qToQueued, jobID, 0.0, 1, nil)
		return err
	})
	return out, err
}

// Cancel stops the processing of a video: queued jobs become FAILED(cancelled) at once, running jobs get a cancel
// marker that their worker sees on its next heartbeat. It returns how many jobs were affected.
func (s *Store) Cancel(ctx context.Context, videoID string) (queued, running int, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if !CanTransition(Queued, Failed) {
			return ErrInvalidTransition
		}
		t1, err := tx.Exec(ctx, `UPDATE video_processing_jobs SET status = 'FAILED', error_code = 'cancelled', updated_at = now()
WHERE video_id = $1::uuid AND status = 'QUEUED'`, videoID)
		if err != nil {
			return err
		}
		t2, err := tx.Exec(ctx, `UPDATE video_processing_jobs SET cancel_requested_at = COALESCE(cancel_requested_at, now()), updated_at = now()
WHERE video_id = $1::uuid AND status = 'PROCESSING'`, videoID)
		queued, running = int(t1.RowsAffected()), int(t2.RowsAffected())
		return err
	})
	return
}

// RecoverStale re-queues running jobs whose lease (last heartbeat) is older than `lease`. A job that already used all
// its attempts becomes FAILED(lease_expired); one with a cancel request becomes FAILED(cancelled).
func (s *Store) RecoverStale(ctx context.Context, lease time.Duration, limit int) (requeued, failed int, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text, attempts, max_attempts, cancel_requested_at FROM video_processing_jobs
WHERE status = 'PROCESSING' AND heartbeat_at < now() - make_interval(secs => $1::float8)
ORDER BY heartbeat_at LIMIT $2 FOR UPDATE SKIP LOCKED`, lease.Seconds(), limit)
		if err != nil {
			return err
		}
		type stale struct {
			id          string
			att, maxAtt int
			cancel      *time.Time
		}
		var list []stale
		for rows.Next() {
			var r stale
			if err := rows.Scan(&r.id, &r.att, &r.maxAtt, &r.cancel); err != nil {
				return err
			}
			list = append(list, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, r := range list {
			switch {
			case r.cancel != nil:
				_, err = tx.Exec(ctx, qToFailed, r.id, CodeCancelled)
				failed++
			case r.att >= r.maxAtt:
				_, err = tx.Exec(ctx, qToFailed, r.id, CodeLeaseExpired)
				failed++
			default:
				_, err = tx.Exec(ctx, qToQueued, r.id, 0.0, 0, CodeLeaseExpired)
				requeued++
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return
}

// FailOrphans closes queued jobs of videos that were deleted or left PROCESSING (FAILED, video_gone), and asks
// running jobs of such videos to stop.
func (s *Store) FailOrphans(ctx context.Context) (failed, stopRequested int, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		t1, err := tx.Exec(ctx, `UPDATE video_processing_jobs j SET status = 'FAILED', error_code = 'video_gone', updated_at = now()
FROM videos v WHERE v.id = j.video_id AND j.status = 'QUEUED' AND (v.deleted_at IS NOT NULL OR v.status <> 'PROCESSING')`)
		if err != nil {
			return err
		}
		t2, err := tx.Exec(ctx, `UPDATE video_processing_jobs j SET cancel_requested_at = now(), updated_at = now()
FROM videos v WHERE v.id = j.video_id AND j.status = 'PROCESSING' AND j.cancel_requested_at IS NULL
  AND (v.deleted_at IS NOT NULL OR v.status <> 'PROCESSING')`)
		failed, stopRequested = int(t1.RowsAffected()), int(t2.RowsAffected())
		return err
	})
	return
}

func (j *Job) String() string {
	return fmt.Sprintf("job %s (%s, video %s, attempt %d/%d)", j.ID, j.Type, j.VideoID, j.Attempts, j.MaxAttempts)
}
