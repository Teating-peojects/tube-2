package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Upload holds the SQL for the creator upload flow: multipart upload sessions,
// draft metadata, thumbnails/captions and publishing. Every statement is a
// package constant (q...) so db/tools/prepare_queries.py can check all of them
// against a real database.
type Upload struct{ pool *pgxpool.Pool }

func NewUpload(pool *pgxpool.Pool) *Upload { return &Upload{pool: pool} }

var (
	ErrNotReady         = errors.New("repository: video is not ready to publish")
	ErrAlreadyPublished = errors.New("repository: video is already published")
	ErrModerated        = errors.New("repository: video was removed by moderation")
	ErrCategoryRequired = errors.New("repository: category required")
	ErrShortTooLarge    = errors.New("repository: short video exceeds 200 MiB")
	ErrVideoNotFound    = errors.New("repository: video not found")
)

// dbq is satisfied by both the pool and a transaction.
type dbq interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const (
	qOpenUploads   = `SELECT count(*)::int FROM upload_sessions WHERE user_id = $1::uuid AND status IN ('INITIATED', 'COMPLETING') AND expires_at > now()`
	qEnsureCreator = `UPDATE users SET is_creator = true WHERE id = $1::uuid AND NOT is_creator`
	qInsertVideo   = `INSERT INTO videos (id, creator_id, title, original_filename, original_storage_key, file_size_bytes)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::bigint)`
	qInsertUpload = `INSERT INTO upload_sessions (id, user_id, video_id, storage_key, provider_upload_id, declared_size_bytes, declared_content_type, part_size_bytes, part_count, expires_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::bigint, $7, $8::int, $9::int, $10::timestamptz)`

	uploadCols = `s.id::text, s.user_id::text, s.video_id::text, s.storage_key, s.provider_upload_id, s.declared_content_type, s.status::text,
  s.declared_size_bytes, s.part_size_bytes, s.part_count, s.expires_at, v.status::text, v.deleted_at IS NOT NULL`
	qUploadByID = `SELECT ` + uploadCols + ` FROM upload_sessions s JOIN videos v ON v.id = s.video_id WHERE s.id = $1::uuid AND s.user_id = $2::uuid`

	qBeginComplete  = `UPDATE upload_sessions SET status = 'COMPLETING', updated_at = now() WHERE id = $1::uuid AND status = 'INITIATED' AND expires_at > now()`
	qRevertComplete = `UPDATE upload_sessions SET status = 'INITIATED', updated_at = now() WHERE id = $1::uuid AND status = 'COMPLETING'`
	qFinishUpload   = `UPDATE upload_sessions SET status = 'COMPLETED', completed_at = now(), updated_at = now() WHERE id = $1::uuid AND status = 'COMPLETING'`
	qVideoProcess   = `UPDATE videos SET status = 'PROCESSING', updated_at = now() WHERE id = $1::uuid AND status = 'UPLOADING'`
	qEnqueueMeta    = `INSERT INTO video_processing_jobs (video_id, job_type) VALUES ($1::uuid, 'METADATA')
ON CONFLICT (video_id, job_type) WHERE status IN ('QUEUED', 'PROCESSING') DO NOTHING`
	qAbortSession = `UPDATE upload_sessions SET status = 'ABORTED', updated_at = now() WHERE id = $1::uuid AND status IN ('INITIATED', 'COMPLETING', 'COMPLETED')`
	qDiscardVideo = `UPDATE videos SET status = 'FAILED', deleted_at = now(), updated_at = now() WHERE id = $1::uuid AND status <> 'PUBLISHED' AND deleted_at IS NULL`

	qExpireUploads = `UPDATE upload_sessions s SET status = 'EXPIRED', updated_at = now()
WHERE s.id IN (SELECT i.id FROM upload_sessions i WHERE i.status IN ('INITIATED', 'COMPLETING') AND i.expires_at < now()
               ORDER BY i.expires_at LIMIT $1::int FOR UPDATE SKIP LOCKED)
RETURNING s.id::text, s.video_id::text, s.storage_key, s.provider_upload_id`

	qJobCounts = `SELECT
  (SELECT count(*) FROM video_processing_jobs j WHERE j.video_id = $1::uuid AND j.status = 'COMPLETED')::int,
  (SELECT count(*) FROM video_processing_jobs j WHERE j.video_id = $1::uuid AND j.status = 'FAILED')::int,
  (SELECT j.job_type::text FROM video_processing_jobs j WHERE j.video_id = $1::uuid AND j.status IN ('QUEUED', 'PROCESSING') ORDER BY j.created_at LIMIT 1)`
	qGeneratedThumbs = `SELECT id::text, storage_key FROM video_thumbnails WHERE video_id = $1::uuid AND source = 'GENERATED' AND status = 'READY' ORDER BY created_at, id LIMIT 6`
	qVideoModeration = `SELECT moderation_status::text FROM videos WHERE id = $1::uuid`

	qInsertThumb   = `INSERT INTO video_thumbnails (video_id, storage_key, type, source, status) VALUES ($1::uuid, $2, 'THUMBNAIL', 'CUSTOM', 'PENDING') RETURNING id::text`
	qCountThumbs   = `SELECT count(*)::int FROM video_thumbnails WHERE video_id = $1::uuid AND source = 'CUSTOM'`
	qUpsertCaption = `INSERT INTO video_captions (video_id, language, label, storage_key, status) VALUES ($1::uuid, $2, $3, $4, 'PENDING')
ON CONFLICT (video_id, language) DO UPDATE SET storage_key = EXCLUDED.storage_key, label = EXCLUDED.label, status = 'PENDING' RETURNING id::text`
	qCountCaptions = `SELECT count(*)::int FROM video_captions WHERE video_id = $1::uuid`
	qCaptionExists = `SELECT EXISTS (SELECT 1 FROM video_captions WHERE video_id = $1::uuid AND language = $2)`
	qThumbByID     = `SELECT storage_key FROM video_thumbnails WHERE id = $1::uuid AND video_id = $2::uuid AND source = 'CUSTOM'`
	qCaptionByID   = `SELECT language, storage_key FROM video_captions WHERE id = $1::uuid AND video_id = $2::uuid`

	qApplyMeta = `UPDATE videos SET title = COALESCE($3, title), description = COALESCE($4, description),
  category_id = COALESCE($5::smallint, category_id), language = COALESCE($6, language),
  visibility = COALESCE($7::video_visibility, visibility), comments_mode = COALESCE($8::comment_mode, comments_mode),
  age_restricted = COALESCE($9::boolean, age_restricted), updated_at = now()
WHERE id = $1::uuid AND creator_id = $2::uuid AND deleted_at IS NULL AND moderation_status <> 'REMOVED'`
	qCategoryID = `SELECT id::int FROM categories WHERE is_active AND (name = $1::citext OR slug = lower($1))`
	qUpsertTag  = `INSERT INTO tags (name, slug) VALUES ($1, $2) ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug RETURNING id`
	qDropTags   = `DELETE FROM video_tags WHERE video_id = $1::uuid AND NOT (tag_id = ANY($2::bigint[]))`
	qAddTags    = `INSERT INTO video_tags (video_id, tag_id) SELECT $1::uuid, t FROM unnest($2::bigint[]) AS t ON CONFLICT DO NOTHING`
	qVideoState = `SELECT status::text, moderation_status::text, category_id::int, video_type::text, file_size_bytes
FROM videos WHERE id = $1::uuid AND creator_id = $2::uuid AND deleted_at IS NULL FOR UPDATE`
	qClearPrimary   = `UPDATE video_thumbnails SET is_primary = false WHERE video_id = $1::uuid AND is_primary`
	qSetPrimary     = `UPDATE video_thumbnails SET is_primary = true, status = 'READY' WHERE id = $2::uuid AND video_id = $1::uuid`
	qCaptionReady   = `UPDATE video_captions SET status = 'READY', storage_key = $3 WHERE id = $2::uuid AND video_id = $1::uuid`
	qPublish        = `UPDATE videos SET status = 'PUBLISHED', published_at = now(), updated_at = now() WHERE id = $1::uuid AND status = 'READY' AND moderation_status = 'OK' RETURNING visibility::text`
	qInsertConsent  = `INSERT INTO upload_consents (video_id, user_id, statement_version, ip_hash) VALUES ($1::uuid, $2::uuid, $3::smallint, $4)`
	qRefreshSearch  = `SELECT refresh_video_search($1::uuid)`
	qVideoPublished = `SELECT status = 'PUBLISHED' FROM videos WHERE id = $1::uuid`
	qFanOutNewVideo = `INSERT INTO notifications (user_id, type, actor_user_id, video_id)
SELECT s.subscriber_id, 'NEW_VIDEO', $2::uuid, $1::uuid
FROM subscriptions s
JOIN users u ON u.id = s.subscriber_id AND u.status = 'ACTIVE' AND u.deleted_at IS NULL
LEFT JOIN user_settings us ON us.user_id = s.subscriber_id
WHERE s.creator_id = $2::uuid AND COALESCE(us.subscription_notifications, true)
ORDER BY s.created_at DESC LIMIT $3::int
RETURNING user_id::text`
)

// ---------- sessions ----------

type UploadSession struct {
	ID, UserID, VideoID, StorageKey, ProviderUploadID, ContentType, Status, VideoStatus string
	DeclaredSize                                                                        int64
	PartSize, PartCount                                                                 int32
	ExpiresAt                                                                           time.Time
	VideoDeleted                                                                        bool
}

func scanUpload(row pgx.Row) (*UploadSession, error) {
	var s UploadSession
	err := row.Scan(&s.ID, &s.UserID, &s.VideoID, &s.StorageKey, &s.ProviderUploadID, &s.ContentType, &s.Status,
		&s.DeclaredSize, &s.PartSize, &s.PartCount, &s.ExpiresAt, &s.VideoStatus, &s.VideoDeleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// OpenUploads counts the caller's unfinished, unexpired upload sessions.
func (r *Upload) OpenUploads(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, qOpenUploads, userID).Scan(&n)
	return n, err
}

type UploadInit struct {
	UploadID, VideoID, UserID, Title, Filename, StorageKey, ProviderUploadID, ContentType string
	Size                                                                                  int64
	PartSize, PartCount                                                                   int32
	ExpiresAt                                                                             time.Time
	Audit                                                                                 AuditEntry
}

// CreateUpload records the draft video and its upload session. Whoever uploads
// becomes a creator (users.is_creator), as on every channel-based platform.
func (r *Upload) CreateUpload(ctx context.Context, n UploadInit) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, qEnsureCreator, n.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, qInsertVideo, n.VideoID, n.UserID, n.Title, n.Filename, n.StorageKey, n.Size); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, qInsertUpload, n.UploadID, n.UserID, n.VideoID, n.StorageKey, n.ProviderUploadID, n.Size, n.ContentType, n.PartSize, n.PartCount, n.ExpiresAt); err != nil {
		return err
	}
	if err = audit(ctx, tx, n.Audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UploadByID returns the caller's own session, or nil.
func (r *Upload) UploadByID(ctx context.Context, id, userID string) (*UploadSession, error) {
	return scanUpload(r.pool.QueryRow(ctx, qUploadByID, id, userID))
}

// BeginComplete moves INITIATED -> COMPLETING; false means another request got there first (or it expired).
func (r *Upload) BeginComplete(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, qBeginComplete, id)
	return tag.RowsAffected() == 1, err
}

// RevertComplete lets the client retry after a transient storage failure.
func (r *Upload) RevertComplete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, qRevertComplete, id)
	return err
}

// FinishComplete marks the upload done and queues the processing pipeline.
func (r *Upload) FinishComplete(ctx context.Context, s *UploadSession, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, qFinishUpload, s.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, qVideoProcess, s.VideoID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, qEnqueueMeta, s.VideoID); err != nil {
		return err
	}
	if err = audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DiscardUpload aborts the session and hides the unpublished draft video.
func (r *Upload) DiscardUpload(ctx context.Context, s *UploadSession, a *AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, qAbortSession, s.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, qDiscardVideo, s.VideoID); err != nil {
		return err
	}
	if a != nil {
		if err = audit(ctx, tx, *a); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type ExpiredUpload struct{ ID, VideoID, StorageKey, ProviderUploadID string }

// ExpireUploads marks sessions past their deadline and discards their drafts.
// SKIP LOCKED makes it safe to run on every API instance at once.
func (r *Upload) ExpireUploads(ctx context.Context, limit int) ([]ExpiredUpload, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, qExpireUploads, limit)
	if err != nil {
		return nil, err
	}
	var out []ExpiredUpload
	for rows.Next() {
		var e ExpiredUpload
		if err = rows.Scan(&e.ID, &e.VideoID, &e.StorageKey, &e.ProviderUploadID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, e := range out {
		if _, err = tx.Exec(ctx, qDiscardVideo, e.VideoID); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit(ctx)
}

// ---------- processing status ----------

type Progress struct {
	Done, Failed int
	Active       *string // job type being worked on, or nil
	Moderation   string
	Thumbs       []ThumbRow
}

type ThumbRow struct{ ID, Key string }

// Progress summarises the media pipeline for one video.
func (r *Upload) Progress(ctx context.Context, videoID string) (Progress, error) {
	var p Progress
	if err := r.pool.QueryRow(ctx, qJobCounts, videoID).Scan(&p.Done, &p.Failed, &p.Active); err != nil {
		return p, err
	}
	if err := r.pool.QueryRow(ctx, qVideoModeration, videoID).Scan(&p.Moderation); err != nil {
		return p, err
	}
	th, err := r.GeneratedThumbs(ctx, videoID)
	p.Thumbs = th
	return p, err
}

// GeneratedThumbs lists the thumbnails the worker produced.
func (r *Upload) GeneratedThumbs(ctx context.Context, videoID string) ([]ThumbRow, error) {
	rows, err := r.pool.Query(ctx, qGeneratedThumbs, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThumbRow
	for rows.Next() {
		var t ThumbRow
		if err := rows.Scan(&t.ID, &t.Key); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------- assets ----------

func (r *Upload) CountThumbs(ctx context.Context, videoID string) (int, error) {
	var n int
	return n, r.pool.QueryRow(ctx, qCountThumbs, videoID).Scan(&n)
}

func (r *Upload) AddThumb(ctx context.Context, videoID, key string) (string, error) {
	var id string
	return id, r.pool.QueryRow(ctx, qInsertThumb, videoID, key).Scan(&id)
}

func (r *Upload) CountCaptions(ctx context.Context, videoID string) (int, error) {
	var n int
	return n, r.pool.QueryRow(ctx, qCountCaptions, videoID).Scan(&n)
}

func (r *Upload) CaptionExists(ctx context.Context, videoID, lang string) (bool, error) {
	var ok bool
	return ok, r.pool.QueryRow(ctx, qCaptionExists, videoID, lang).Scan(&ok)
}

func (r *Upload) AddCaption(ctx context.Context, videoID, lang, label, key string) (string, error) {
	var id string
	return id, r.pool.QueryRow(ctx, qUpsertCaption, videoID, lang, label, key).Scan(&id)
}

// ThumbKey returns the storage key of a custom thumbnail of this video ("" when unknown).
func (r *Upload) ThumbKey(ctx context.Context, videoID, id string) (string, error) {
	var k string
	err := r.pool.QueryRow(ctx, qThumbByID, id, videoID).Scan(&k)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return k, err
}

// CaptionKey returns the language and storage key of a caption of this video.
func (r *Upload) CaptionKey(ctx context.Context, videoID, id string) (lang, key string, err error) {
	err = r.pool.QueryRow(ctx, qCaptionByID, id, videoID).Scan(&lang, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return
}

// ---------- metadata & publishing ----------

type TagIn struct{ Name, Slug string }

// MetaPatch is a partial update; nil fields are left alone.
type MetaPatch struct {
	Title, Description, Language *string
	CategoryID                   *int32
	Visibility, CommentsMode     *string // enum labels (PUBLIC..., ON...)
	AgeRestricted                *bool
	Tags                         *[]TagIn
}

// CategoryID resolves a category by name or slug; 0 when unknown.
func (r *Upload) CategoryID(ctx context.Context, name string) (int32, error) {
	var id int32
	err := r.pool.QueryRow(ctx, qCategoryID, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func applyMeta(ctx context.Context, q dbq, videoID, userID string, m MetaPatch) error {
	tag, err := q.Exec(ctx, qApplyMeta, videoID, userID, m.Title, m.Description, m.CategoryID, m.Language, m.Visibility, m.CommentsMode, m.AgeRestricted)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrVideoNotFound
	}
	if m.Tags == nil {
		return nil
	}
	ids := make([]int64, 0, len(*m.Tags))
	for _, t := range *m.Tags {
		var id int64
		if err := q.QueryRow(ctx, qUpsertTag, t.Name, t.Slug).Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if _, err := q.Exec(ctx, qDropTags, videoID, ids); err != nil {
		return err
	}
	_, err = q.Exec(ctx, qAddTags, videoID, ids)
	return err
}

// UpdateMetadata saves draft (or live) metadata for the creator's own video.
func (r *Upload) UpdateMetadata(ctx context.Context, videoID, userID string, m MetaPatch, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = applyMeta(ctx, tx, videoID, userID, m); err != nil {
		return err
	}
	var live bool
	if err = tx.QueryRow(ctx, qVideoPublished, videoID).Scan(&live); err != nil {
		return err
	}
	if live { // keep search in step with the edited title/description/tags
		if _, err = tx.Exec(ctx, qRefreshSearch, videoID); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type CaptionReady struct{ ID, Key string }

type PublishParams struct {
	VideoID, UserID string
	Patch           MetaPatch
	PrimaryThumbID  string // custom upload or generated thumbnail to use; "" keeps the current one
	Captions        []CaptionReady
	ConsentVersion  int16
	IPHash          []byte
	Audit           AuditEntry
}

// Publish applies the final metadata and makes the video live in ONE transaction,
// together with the uploader's consent record. The row is locked, so a double
// click or two tabs cannot publish twice.
func (r *Upload) Publish(ctx context.Context, p PublishParams) (visibility string, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var status, moderation string
	var category *int32
	var vtype *string
	var size *int64
	err = tx.QueryRow(ctx, qVideoState, p.VideoID, p.UserID).Scan(&status, &moderation, &category, &vtype, &size)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrVideoNotFound
	}
	if err != nil {
		return "", err
	}
	switch {
	case moderation == "REMOVED":
		return "", ErrModerated
	case status == "PUBLISHED":
		return "", ErrAlreadyPublished
	case status != "READY":
		return "", ErrNotReady
	}
	if vtype != nil && *vtype == "SHORT" && size != nil && *size > 200<<20 {
		return "", ErrShortTooLarge
	}
	if category == nil && p.Patch.CategoryID == nil {
		return "", ErrCategoryRequired
	}
	if err = applyMeta(ctx, tx, p.VideoID, p.UserID, p.Patch); err != nil {
		return "", err
	}
	if p.PrimaryThumbID != "" {
		if _, err = tx.Exec(ctx, qClearPrimary, p.VideoID); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, qSetPrimary, p.VideoID, p.PrimaryThumbID); err != nil {
			return "", err
		}
	}
	for _, c := range p.Captions {
		if _, err = tx.Exec(ctx, qCaptionReady, p.VideoID, c.ID, c.Key); err != nil {
			return "", err
		}
	}
	err = tx.QueryRow(ctx, qPublish, p.VideoID).Scan(&visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotReady
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, qInsertConsent, p.VideoID, p.UserID, p.ConsentVersion, p.IPHash); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, qRefreshSearch, p.VideoID); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, p.Audit); err != nil {
		return "", err
	}
	return visibility, tx.Commit(ctx)
}

// FanOutNewVideo writes "new video" notifications for a creator's subscribers
// (at most limit; bigger audiences use the subscriptions feed instead) and
// returns who got one, so the caller can push a realtime event.
func (r *Upload) FanOutNewVideo(ctx context.Context, videoID, creatorID string, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, qFanOutNewVideo, videoID, creatorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
