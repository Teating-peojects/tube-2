package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Notifications reads and marks a user's inbox. Rows are written elsewhere
// (subscriptions, comments, publishing); titles and links are built at read time
// from live data, so a renamed video or a deleted account never leaves stale text.
type Notifications struct{ pool *pgxpool.Pool }

func NewNotifications(pool *pgxpool.Pool) *Notifications { return &Notifications{pool: pool} }

// Likes are counted on the video itself and never shown as notices. A notice about
// a video disappears when the video is deleted, removed, or (for new uploads) no longer public.
const notifVisible = `n.type NOT IN ('VIDEO_LIKE', 'COMMENT_LIKE')
  AND (n.video_id IS NULL OR (v.deleted_at IS NULL AND v.moderation_status <> 'REMOVED'
       AND (n.type <> 'NEW_VIDEO' OR (v.status = 'PUBLISHED' AND v.visibility = 'PUBLIC'))))`

const (
	qNotifList = `SELECT n.id, n.type::text, n.actor_user_id::text, a.display_name, n.video_id::text, v.title,
  COALESCE(n.payload->>'title', ''), COALESCE(n.payload->>'body', ''), n.is_read, n.created_at
FROM notifications n
LEFT JOIN users a ON a.id = n.actor_user_id AND a.deleted_at IS NULL
LEFT JOIN videos v ON v.id = n.video_id
WHERE n.user_id = $1::uuid AND ` + notifVisible + `
  AND ($2::timestamptz IS NULL OR (n.created_at, n.id) < ($2::timestamptz, $3::bigint))
  AND (NOT $4::boolean OR n.read_at IS NULL)
ORDER BY n.created_at DESC, n.id DESC LIMIT $5::int`

	qNotifUnread = `SELECT count(*)::int FROM (
  SELECT 1 FROM notifications n LEFT JOIN videos v ON v.id = n.video_id
  WHERE n.user_id = $1::uuid AND n.read_at IS NULL AND ` + notifVisible + ` LIMIT 100) x`

	qNotifReadIDs = `UPDATE notifications SET read_at = now() WHERE user_id = $1::uuid AND read_at IS NULL AND id = ANY($2::bigint[])`
	qNotifReadAll = `UPDATE notifications SET read_at = now() WHERE user_id = $1::uuid AND read_at IS NULL`
)

type NotificationRow struct {
	ID                                      int64
	Type                                    string
	ActorID, ActorName, VideoID, VideoTitle *string
	PayloadTitle, PayloadBody               string
	Read                                    bool
	CreatedAt                               time.Time
}

// List returns one keyset page, newest first. after = nil starts from the top.
func (r *Notifications) List(ctx context.Context, userID string, after *Keyset, unreadOnly bool, limit int) ([]NotificationRow, error) {
	var ts *time.Time
	var id int64
	if after != nil {
		ts, id = &after.Time, after.Num
	}
	rows, err := r.pool.Query(ctx, qNotifList, userID, ts, id, unreadOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationRow
	for rows.Next() {
		var n NotificationRow
		if err := rows.Scan(&n.ID, &n.Type, &n.ActorID, &n.ActorName, &n.VideoID, &n.VideoTitle, &n.PayloadTitle, &n.PayloadBody, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Unread counts unread notices, capped at 100 (the badge shows "99+").
func (r *Notifications) Unread(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, qNotifUnread, userID).Scan(&n)
	return n, err
}

// MarkRead marks the given notices (or all of them) read; it only ever touches the caller's own rows.
func (r *Notifications) MarkRead(ctx context.Context, userID string, ids []int64, all bool) error {
	var err error
	if all {
		_, err = r.pool.Exec(ctx, qNotifReadAll, userID)
	} else if len(ids) > 0 {
		_, err = r.pool.Exec(ctx, qNotifReadIDs, userID, ids)
	}
	return err
}

const qOwnsVideo = `SELECT EXISTS (SELECT 1 FROM videos WHERE id = $1::uuid AND creator_id = $2::uuid AND deleted_at IS NULL)`

// OwnsVideo reports whether userID is the creator of videoID (gate for the video:<id> realtime topic).
func (r *Notifications) OwnsVideo(ctx context.Context, userID, videoID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, qOwnsVideo, videoID, userID).Scan(&ok)
	return ok, err
}
