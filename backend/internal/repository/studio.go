package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Studio is the creator dashboard: numbers and lists about the caller's OWN videos.
// Every query is scoped by creator_id, so a creator can never see or change anyone else's data.
// Figures are computed from the event tables for the requested window (not from rollups).
type Studio struct{ pool *pgxpool.Pool }

func NewStudio(pool *pgxpool.Pool) *Studio { return &Studio{pool: pool} }

const (
	// $1 creator, $2 number of days. One row per UTC day, zero-filled.
	qStudioViewsSeries = `SELECT d::date::text, COALESCE(x.views, 0)::bigint, COALESCE(x.secs, 0)::bigint
FROM generate_series((now() AT TIME ZONE 'UTC')::date - ($2::int - 1), (now() AT TIME ZONE 'UTC')::date, interval '1 day') d
LEFT JOIN (
  SELECT (w.created_at AT TIME ZONE 'UTC')::date AS day, count(*) AS views, sum(w.watch_duration_seconds) AS secs
  FROM video_views w JOIN videos v ON v.id = w.video_id AND v.creator_id = $1::uuid
  WHERE w.created_at >= ((now() AT TIME ZONE 'UTC')::date - ($2::int - 1))::timestamp AT TIME ZONE 'UTC'
  GROUP BY 1) x ON x.day = d::date
ORDER BY d`

	qStudioTopVideos = `SELECT v.id::text, v.title, count(*)::bigint AS views
FROM video_views w JOIN videos v ON v.id = w.video_id AND v.creator_id = $1::uuid AND v.deleted_at IS NULL
WHERE w.created_at >= now() - make_interval(days => $2::int)
GROUP BY v.id, v.title ORDER BY views DESC, v.id LIMIT 5`

	qStudioSubs = `SELECT count(*)::bigint, count(*) FILTER (WHERE created_at >= now() - make_interval(days => $2::int))::bigint
FROM subscriptions WHERE creator_id = $1::uuid`

	// $3 = 'COUNTRY' | 'DEVICE' | 'TRAFFIC'
	qStudioBreakdown = `SELECT label, n::bigint FROM (
  SELECT CASE $3::text WHEN 'COUNTRY' THEN COALESCE(w.country_code::text, '')
                       WHEN 'DEVICE' THEN w.device_type::text ELSE w.traffic_source::text END AS label, count(*) AS n
  FROM video_views w JOIN videos v ON v.id = w.video_id AND v.creator_id = $1::uuid
  WHERE w.created_at >= now() - make_interval(days => $2::int) GROUP BY 1) t
ORDER BY n DESC, label LIMIT 6`

	qStudioEngagement = `SELECT
  (SELECT count(*) FROM video_reactions r JOIN videos v ON v.id = r.video_id AND v.creator_id = $1::uuid
    WHERE r.reaction = 'LIKE' AND r.created_at >= now() - make_interval(days => $2::int))::bigint,
  (SELECT count(*) FROM comments c WHERE c.video_creator_id = $1::uuid AND c.status = 'VISIBLE'
    AND c.created_at >= now() - make_interval(days => $2::int))::bigint,
  (SELECT COALESCE(avg(w.watch_duration_seconds), 0)::bigint FROM video_views w JOIN videos v ON v.id = w.video_id AND v.creator_id = $1::uuid
    WHERE w.created_at >= now() - make_interval(days => $2::int))`

	qStudioVideos = `SELECT v.id::text, v.title, t.storage_key, COALESCE(v.duration_seconds, 0), v.status::text, v.visibility::text,
  v.created_at, vs.view_count, vs.comment_count::bigint, vs.like_count::bigint
FROM videos v
JOIN video_stats vs ON vs.video_id = v.id
LEFT JOIN video_thumbnails t ON t.video_id = v.id AND t.is_primary AND t.status = 'READY'
WHERE v.creator_id = $1::uuid AND v.deleted_at IS NULL
  AND CASE $2::text
        WHEN 'published'  THEN v.status = 'PUBLISHED'
        WHEN 'draft'      THEN v.status IN ('READY', 'FAILED')
        WHEN 'processing' THEN v.status IN ('UPLOADING', 'PROCESSING')
        WHEN 'private'    THEN v.status = 'PUBLISHED' AND v.visibility = 'PRIVATE'
        WHEN 'unlisted'   THEN v.status = 'PUBLISHED' AND v.visibility = 'UNLISTED'
        ELSE true END
  AND ($3::timestamptz IS NULL OR (v.created_at, v.id) < ($3::timestamptz, $4::uuid))
ORDER BY v.created_at DESC, v.id DESC LIMIT $5::int`

	qStudioSetVisibility = `UPDATE videos SET visibility = $3::video_visibility, updated_at = now()
WHERE id = $1::uuid AND creator_id = $2::uuid AND deleted_at IS NULL AND moderation_status <> 'REMOVED'`

	qStudioDelete = `UPDATE videos SET deleted_at = now(), updated_at = now() WHERE id = $1::uuid AND creator_id = $2::uuid AND deleted_at IS NULL`

	qStudioComments = `SELECT c.id::text, c.video_id::text, v.title, u.display_name, c.content, c.created_at, c.status::text
FROM comments c
JOIN videos v ON v.id = c.video_id
JOIN users u ON u.id = c.user_id
WHERE c.video_creator_id = $1::uuid AND v.deleted_at IS NULL
  AND CASE $2::text WHEN 'held' THEN c.status = 'UNDER_REVIEW' ELSE c.status IN ('VISIBLE', 'UNDER_REVIEW') END
  AND ($3::timestamptz IS NULL OR (c.created_at, c.id) < ($3::timestamptz, $4::uuid))
ORDER BY c.created_at DESC, c.id DESC LIMIT $5::int`

	qStudioApprove = `UPDATE comments SET status = 'VISIBLE', updated_at = now()
WHERE id = $1::uuid AND video_creator_id = $2::uuid AND status = 'UNDER_REVIEW'`
)

type DayPoint struct {
	Date  string
	Views int64
	Secs  int64
}

type TopVideo struct {
	ID, Title string
	Views     int64
}

type Count struct {
	Label string
	N     int64
}

type StudioOverviewRows struct {
	Days                      []DayPoint
	Top                       []TopVideo
	Subscribers, SubsInPeriod int64
}

func (r *Studio) series(ctx context.Context, creatorID string, days int) ([]DayPoint, error) {
	rows, err := r.pool.Query(ctx, qStudioViewsSeries, creatorID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayPoint
	for rows.Next() {
		var p DayPoint
		if err := rows.Scan(&p.Date, &p.Views, &p.Secs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Studio) Overview(ctx context.Context, creatorID string, days int) (StudioOverviewRows, error) {
	var o StudioOverviewRows
	var err error
	if o.Days, err = r.series(ctx, creatorID, days); err != nil {
		return o, err
	}
	rows, err := r.pool.Query(ctx, qStudioTopVideos, creatorID, days)
	if err != nil {
		return o, err
	}
	for rows.Next() {
		var t TopVideo
		if err = rows.Scan(&t.ID, &t.Title, &t.Views); err != nil {
			rows.Close()
			return o, err
		}
		o.Top = append(o.Top, t)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return o, err
	}
	err = r.pool.QueryRow(ctx, qStudioSubs, creatorID, days).Scan(&o.Subscribers, &o.SubsInPeriod)
	return o, err
}

type AnalyticsRows struct {
	Days                        []DayPoint
	Countries, Devices, Traffic []Count
	Likes, Comments, AvgSecs    int64
}

func (r *Studio) breakdown(ctx context.Context, creatorID string, days int, dim string) ([]Count, error) {
	rows, err := r.pool.Query(ctx, qStudioBreakdown, creatorID, days, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Label, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Studio) Analytics(ctx context.Context, creatorID string, days int) (AnalyticsRows, error) {
	var a AnalyticsRows
	var err error
	if a.Days, err = r.series(ctx, creatorID, days); err != nil {
		return a, err
	}
	if a.Countries, err = r.breakdown(ctx, creatorID, days, "COUNTRY"); err != nil {
		return a, err
	}
	if a.Devices, err = r.breakdown(ctx, creatorID, days, "DEVICE"); err != nil {
		return a, err
	}
	if a.Traffic, err = r.breakdown(ctx, creatorID, days, "TRAFFIC"); err != nil {
		return a, err
	}
	err = r.pool.QueryRow(ctx, qStudioEngagement, creatorID, days).Scan(&a.Likes, &a.Comments, &a.AvgSecs)
	return a, err
}

type StudioVideoRow struct {
	ID, Title              string
	ThumbKey               *string
	DurationSec            int
	Status, Visibility     string
	CreatedAt              time.Time
	Views, Comments, Likes int64
}

func (r *Studio) Videos(ctx context.Context, creatorID, filter string, after *Keyset, limit int) ([]StudioVideoRow, error) {
	var ts *time.Time
	var id *string
	if after != nil {
		ts, id = &after.Time, &after.ID
	}
	rows, err := r.pool.Query(ctx, qStudioVideos, creatorID, filter, ts, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StudioVideoRow
	for rows.Next() {
		var v StudioVideoRow
		if err := rows.Scan(&v.ID, &v.Title, &v.ThumbKey, &v.DurationSec, &v.Status, &v.Visibility, &v.CreatedAt, &v.Views, &v.Comments, &v.Likes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetVisibility changes a video's visibility; false when it is not the caller's (or is removed/deleted).
func (r *Studio) SetVisibility(ctx context.Context, creatorID, videoID, visibility string, a AuditEntry) (bool, error) {
	return r.change(ctx, qStudioSetVisibility, a, videoID, creatorID, visibility)
}

// DeleteVideo soft-deletes the caller's video (storage is cleaned up by the maintenance job).
func (r *Studio) DeleteVideo(ctx context.Context, creatorID, videoID string, a AuditEntry) (bool, error) {
	return r.change(ctx, qStudioDelete, a, videoID, creatorID)
}

// ApproveComment publishes a comment held for review on the caller's video.
func (r *Studio) ApproveComment(ctx context.Context, creatorID, commentID string, a AuditEntry) (bool, error) {
	return r.change(ctx, qStudioApprove, a, commentID, creatorID)
}

// change runs one UPDATE and its audit row in a transaction; the audit is written only if a row changed.
func (r *Studio) change(ctx context.Context, sql string, a AuditEntry, args ...any) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if err = audit(ctx, tx, a); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

type StudioCommentRow struct {
	ID, VideoID, VideoTitle, Author, Body, Status string
	CreatedAt                                     time.Time
}

func (r *Studio) Comments(ctx context.Context, creatorID, filter string, after *Keyset, limit int) ([]StudioCommentRow, error) {
	var ts *time.Time
	var id *string
	if after != nil {
		ts, id = &after.Time, &after.ID
	}
	rows, err := r.pool.Query(ctx, qStudioComments, creatorID, filter, ts, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StudioCommentRow
	for rows.Next() {
		var c StudioCommentRow
		if err := rows.Scan(&c.ID, &c.VideoID, &c.VideoTitle, &c.Author, &c.Body, &c.CreatedAt, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
