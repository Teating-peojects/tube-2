package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PG is the PostgreSQL Store. The job queue is video_processing_jobs (claimed with
// FOR UPDATE SKIP LOCKED, kept alive by heartbeat_at). Every statement that finishes or fails a job
// is guarded by "status = PROCESSING AND worker_id = me", so a worker that lost its lease (it was
// presumed dead and the job re-queued) can never overwrite the new owner's result.
type PG struct {
	pool     *pgxpool.Pool
	workerID string
}

func NewPG(pool *pgxpool.Pool, workerID string) *PG { return &PG{pool: pool, workerID: workerID} }

const (
	qClaim = `UPDATE video_processing_jobs j
SET status = 'PROCESSING', attempts = j.attempts + 1, worker_id = $1, started_at = now(), heartbeat_at = now(),
    error_message = NULL, updated_at = now()
WHERE j.id = (SELECT q.id FROM video_processing_jobs q
              WHERE q.status = 'QUEUED' AND q.scheduled_at <= now() AND q.attempts < q.max_attempts
              ORDER BY q.scheduled_at, q.created_at FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING j.id::text, j.video_id::text, j.job_type::text, j.attempts::int, j.max_attempts::int,
  GREATEST(EXTRACT(EPOCH FROM (now() - j.scheduled_at)), 0)::float8`

	qHeartbeat = `UPDATE video_processing_jobs SET heartbeat_at = now() WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING'`
	qRelease   = `UPDATE video_processing_jobs SET status = 'QUEUED', attempts = GREATEST(attempts - 1, 0), worker_id = NULL,
  heartbeat_at = NULL, scheduled_at = now(), updated_at = now()
WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING'`
	qRetry = `UPDATE video_processing_jobs SET status = 'QUEUED', error_message = $3, worker_id = NULL, heartbeat_at = NULL,
  scheduled_at = now() + make_interval(secs => $4::int), updated_at = now()
WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING'`
	qJobFailed = `UPDATE video_processing_jobs SET status = 'FAILED', error_message = $3, heartbeat_at = NULL, updated_at = now()
WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING'`
	qJobDone = `UPDATE video_processing_jobs SET status = 'COMPLETED', completed_at = now(), heartbeat_at = NULL, updated_at = now()
WHERE id = $1::uuid AND worker_id = $2 AND status = 'PROCESSING'`
	qEnqueue = `INSERT INTO video_processing_jobs (video_id, job_type) VALUES ($1::uuid, $2::job_type)
ON CONFLICT (video_id, job_type) WHERE status IN ('QUEUED', 'PROCESSING') DO NOTHING`
	qVideoFailed = `UPDATE videos SET status = 'FAILED', updated_at = now()
WHERE id = $1::uuid AND status IN ('UPLOADING', 'PROCESSING') RETURNING creator_id::text`
	qNotify = `INSERT INTO notifications (user_id, type, video_id, payload) VALUES ($1::uuid, $2::notification_type, $3::uuid, $4::jsonb)`

	qStale = `WITH stale AS (
  UPDATE video_processing_jobs SET
    status = CASE WHEN attempts < max_attempts THEN 'QUEUED'::job_status ELSE 'FAILED'::job_status END,
    error_message = 'worker_lost: no heartbeat', worker_id = NULL, heartbeat_at = NULL, scheduled_at = now(), updated_at = now()
  WHERE status = 'PROCESSING' AND heartbeat_at < now() - make_interval(secs => $1::int)
  RETURNING video_id, status
), failed AS (
  UPDATE videos v SET status = 'FAILED', updated_at = now() FROM stale s
  WHERE v.id = s.video_id AND s.status = 'FAILED' AND v.status IN ('UPLOADING', 'PROCESSING')
  RETURNING v.creator_id, v.id
), note AS (
  INSERT INTO notifications (user_id, type, video_id, payload)
  SELECT creator_id, 'VIDEO_FAILED', id, '{"code":"worker_lost"}'::jsonb FROM failed
)
SELECT count(*)::int FROM stale`

	qVideo = `SELECT creator_id::text, status::text, moderation_status::text, COALESCE(original_storage_key, ''),
  COALESCE(file_size_bytes, 0), deleted_at IS NOT NULL FROM videos WHERE id = $1::uuid`
	qMeta = `SELECT m.width, m.height, COALESCE(m.fps, 0)::float8, m.has_audio, COALESCE(v.duration_seconds, 0)
FROM video_metadata m JOIN videos v ON v.id = m.video_id WHERE m.video_id = $1::uuid`

	qUpsertMeta = `INSERT INTO video_metadata (video_id, width, height, fps, video_codec, audio_codec, bitrate, container, has_audio)
VALUES ($1::uuid, $2, $3, $4::float8::numeric(6,3), $5, $6, $7, $8, $9)
ON CONFLICT (video_id) DO UPDATE SET width = EXCLUDED.width, height = EXCLUDED.height, fps = EXCLUDED.fps,
  video_codec = EXCLUDED.video_codec, audio_codec = EXCLUDED.audio_codec, bitrate = EXCLUDED.bitrate,
  container = EXCLUDED.container, has_audio = EXCLUDED.has_audio, updated_at = now()`
	// Writing content_sha256 fires the blocklist trigger: a match flips moderation_status to REMOVED.
	qVideoMeta = `UPDATE videos SET video_type = $2::video_type, duration_seconds = $3, content_sha256 = $4, updated_at = now()
WHERE id = $1::uuid AND status = 'PROCESSING' AND deleted_at IS NULL RETURNING moderation_status::text`

	qDelGenThumbs = `DELETE FROM video_thumbnails WHERE video_id = $1::uuid AND source = 'GENERATED' RETURNING storage_key`
	qInsThumb     = `INSERT INTO video_thumbnails (video_id, storage_key, width, height, type, source, status, is_primary)
VALUES ($1::uuid, $2, $3, $4, 'THUMBNAIL', 'GENERATED', 'READY', $5)`

	qUpsertVariant = `INSERT INTO video_variants (video_id, resolution, width, height, bitrate, codec, storage_key, manifest_path, file_size_bytes, status)
VALUES ($1::uuid, $2::video_resolution, $3, $4, $5, 'h264', $6, 'index.m3u8', $7, 'READY')
ON CONFLICT (video_id, resolution) DO UPDATE SET width = EXCLUDED.width, height = EXCLUDED.height, bitrate = EXCLUDED.bitrate,
  storage_key = EXCLUDED.storage_key, manifest_path = EXCLUDED.manifest_path, file_size_bytes = EXCLUDED.file_size_bytes,
  status = 'READY', updated_at = now()`
	qVariants = `SELECT resolution::text FROM video_variants WHERE video_id = $1::uuid AND status = 'READY'`
	qReady    = `UPDATE videos SET status = 'READY', hls_master_key = $2, processing_profile = $3, outputs_cleaned_at = NULL, updated_at = now()
WHERE id = $1::uuid AND status = 'PROCESSING' AND deleted_at IS NULL AND moderation_status <> 'REMOVED' RETURNING creator_id::text`
)

func (s *PG) Claim(ctx context.Context) (*Job, error) {
	var j Job
	err := s.pool.QueryRow(ctx, qClaim, s.workerID).Scan(&j.ID, &j.VideoID, &j.Type, &j.Attempts, &j.MaxAttempts, &j.WaitSec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *PG) leased(ctx context.Context, q string, args ...any) error {
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (s *PG) Heartbeat(ctx context.Context, j Job) error {
	return s.leased(ctx, qHeartbeat, j.ID, s.workerID)
}

func (s *PG) Release(ctx context.Context, j Job) error {
	return s.leased(ctx, qRelease, j.ID, s.workerID)
}

func (s *PG) RequeueStale(ctx context.Context, after time.Duration) (int, error) {
	var n int
	return n, s.pool.QueryRow(ctx, qStale, int(after.Seconds())).Scan(&n)
}

// errText is what lands in video_processing_jobs.error_message: a stable code and a user-safe
// sentence, nothing else. The underlying error (ffmpeg output, paths, storage detail) goes to the
// worker log only, so it can never leak through an API.
func errText(je *JobError) string {
	t := je.Code + ": " + je.Message
	if len(t) > 500 {
		t = t[:500]
	}
	return t
}

// Fail retries a transient error with backoff, or ends the job (and the video) for good.
func (s *PG) Fail(ctx context.Context, j Job, je *JobError) (FailResult, error) {
	if !je.Permanent && j.Attempts < j.MaxAttempts {
		return FailResult{}, s.leased(ctx, qRetry, j.ID, s.workerID, errText(je), int(backoff(j.Attempts).Seconds()))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return FailResult{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, qJobFailed, j.ID, s.workerID, errText(je))
	if err != nil {
		return FailResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return FailResult{}, ErrLeaseLost
	}
	res := FailResult{Final: true}
	err = tx.QueryRow(ctx, qVideoFailed, j.VideoID).Scan(&res.CreatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		res.CreatorID = ""
	} else if err != nil {
		return FailResult{}, err
	}
	if res.CreatorID != "" && !je.Silent {
		if _, err = tx.Exec(ctx, qNotify, res.CreatorID, "VIDEO_FAILED", j.VideoID, fmt.Sprintf(`{"code":%q}`, je.Code)); err != nil {
			return FailResult{}, err
		}
	}
	if je.Silent {
		res.CreatorID = ""
	}
	return res, tx.Commit(ctx)
}

func (s *PG) Video(ctx context.Context, id string) (VideoRow, error) {
	var v VideoRow
	var deleted bool
	err := s.pool.QueryRow(ctx, qVideo, id).Scan(&v.CreatorID, &v.Status, &v.Moderation, &v.OriginalKey, &v.Size, &deleted)
	if errors.Is(err, pgx.ErrNoRows) || deleted {
		return v, ErrVideoGone
	}
	return v, err
}

func (s *PG) Meta(ctx context.Context, id string) (MetaRow, error) {
	var m MetaRow
	err := s.pool.QueryRow(ctx, qMeta, id).Scan(&m.Width, &m.Height, &m.FPS, &m.HasAudio, &m.Duration)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, transient("metadata_missing", errors.New("video_metadata row not found"))
	}
	return m, err
}

// done completes the job and enqueues the next step inside the caller's transaction.
func (s *PG) done(ctx context.Context, tx pgx.Tx, j Job, next string) error {
	tag, err := tx.Exec(ctx, qJobDone, j.ID, s.workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	if next != "" {
		_, err = tx.Exec(ctx, qEnqueue, j.VideoID, next)
	}
	return err
}

func (s *PG) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PG) SaveMetadata(ctx context.Context, j Job, m MetadataResult) (blocked bool, err error) {
	p := m.Probe
	var fps any
	if p.FPS > 0 && p.FPS <= 240 {
		fps = p.FPS
	}
	var bitrate any
	if p.Bitrate > 0 {
		bitrate = p.Bitrate
	}
	var audio any
	if p.AudioCodec != "" {
		audio = p.AudioCodec
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, qUpsertMeta, j.VideoID, p.Width, p.Height, fps, p.VideoCodec, audio, bitrate, p.Container, p.HasAudio); err != nil {
			return err
		}
		var moderation string
		err := tx.QueryRow(ctx, qVideoMeta, j.VideoID, m.VideoType, m.Seconds, m.SHA256).Scan(&moderation)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVideoGone
		}
		if err != nil {
			return err
		}
		if moderation == "REMOVED" {
			blocked = true // commit the hash and the REMOVED status; the caller fails the job
			return nil
		}
		return s.done(ctx, tx, j, "THUMBNAIL")
	})
	return blocked, err
}

func (s *PG) ReplaceThumbnails(ctx context.Context, j Job, items []ThumbOut, primary int) (old []string, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		old = old[:0]
		rows, err := tx.Query(ctx, qDelGenThumbs, j.VideoID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return err
			}
			old = append(old, k)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i, t := range items {
			if _, err := tx.Exec(ctx, qInsThumb, j.VideoID, t.Key, t.W, t.H, i == primary); err != nil {
				return err
			}
		}
		return s.done(ctx, tx, j, "TRANSCODE")
	})
	return old, err
}

func (s *PG) SaveVariant(ctx context.Context, videoID string, v VariantOut) error {
	_, err := s.pool.Exec(ctx, qUpsertVariant, videoID, v.R.Name, v.R.W, v.R.H, v.R.bandwidth(), v.Key, v.Bytes)
	return err
}

func (s *PG) FinishTranscode(ctx context.Context, j Job) error {
	return s.inTx(ctx, func(tx pgx.Tx) error { return s.done(ctx, tx, j, "HLS") })
}

func (s *PG) Variants(ctx context.Context, videoID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, qVariants, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PG) FinishHLS(ctx context.Context, j Job, masterKey, profile string) (creator string, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, qReady, j.VideoID, masterKey, profile).Scan(&creator)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVideoGone
		}
		if err != nil {
			return err
		}
		if err := s.done(ctx, tx, j, ""); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, qNotify, creator, "VIDEO_READY", j.VideoID, "{}")
		return err
	})
	return creator, err
}

// ClaimFailedForCleanup is one statement, so two workers never claim the same video.
func (s *PG) ClaimFailedForCleanup(ctx context.Context, olderThan time.Duration, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `UPDATE videos SET outputs_cleaned_at = now()
WHERE id IN (
  SELECT v.id FROM videos v
  WHERE v.status = 'FAILED' AND v.outputs_cleaned_at IS NULL AND v.deleted_at IS NULL
    AND v.updated_at < now() - make_interval(secs => $1::int)
    AND NOT EXISTS (SELECT 1 FROM video_processing_jobs j WHERE j.video_id = v.id AND j.status IN ('QUEUED', 'PROCESSING'))
  ORDER BY v.updated_at LIMIT $2 FOR UPDATE SKIP LOCKED)
RETURNING id::text`, int(olderThan.Seconds()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *PG) ReleaseCleanup(ctx context.Context, videoID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE videos SET outputs_cleaned_at = NULL WHERE id = $1::uuid AND status = 'FAILED'`, videoID)
	return err
}

// RemoveVideo marks the video REMOVED and audits it in one transaction (the same effect the
// blocked-hash trigger has). The original stays in storage untouched: what happens to evidence is a
// legal decision, not the worker's.
func (s *PG) RemoveVideo(ctx context.Context, j Job, reason string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE videos SET moderation_status = 'REMOVED', updated_at = now()
WHERE id = $1::uuid AND deleted_at IS NULL`, j.VideoID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrVideoGone
		}
		_, err = tx.Exec(ctx, `INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, metadata)
VALUES (NULL, 'moderation.scan_match', 'video', $1, jsonb_build_object('detail', $2::text))`, j.VideoID, reason)
		return err
	})
}

var _ Store = (*PG)(nil)
