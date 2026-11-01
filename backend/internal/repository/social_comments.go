package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- comments ----------

type CommentRow struct {
	ID, Text, AuthorID, AuthorName string
	AvatarKey                      *string
	CreatedAt                      time.Time
	Likes, ReplyCount              int32
	Liked, Pinned, IsCreator       bool
}

// $2 is always the viewer (nullable uuid) in comment queries. The creator badge
// uses comments.video_creator_id, so no join to videos is needed.
const commentCols = `c.id::text, c.content, c.created_at, c.like_count, c.reply_count, c.is_pinned, c.user_id::text, au.display_name, p.avatar_storage_key,
  (c.user_id = c.video_creator_id), (cl.user_id IS NOT NULL)`

const commentFrom = `FROM comments c
  JOIN users au ON au.id = c.user_id AND au.status NOT IN ('SUSPENDED', 'BANNED')
  JOIN user_profiles p ON p.user_id = c.user_id
  LEFT JOIN comment_reactions cl ON cl.comment_id = c.id AND cl.user_id = $2::uuid AND cl.reaction = 'LIKE'`

func scanComment(r pgx.Row) (CommentRow, error) {
	var c CommentRow
	err := r.Scan(&c.ID, &c.Text, &c.CreatedAt, &c.Likes, &c.ReplyCount, &c.Pinned, &c.AuthorID, &c.AuthorName, &c.AvatarKey, &c.IsCreator, &c.Liked)
	return c, err
}

func collectComments(rows pgx.Rows) ([]CommentRow, error) {
	defer rows.Close()
	out := []CommentRow{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type CommentKey struct {
	Time  time.Time // "new" order and replies
	Likes int64     // "top" order
	ID    string
}

type CommentListQuery struct {
	VideoID string
	Viewer  *string
	Sort    string // "new" | "top"
	After   *CommentKey
	Limit   int
}

const visibleComment = `c.status = 'VISIBLE' AND c.deleted_at IS NULL`

// PinnedComment returns the video's pinned comment, if any.
func (r *Social) PinnedComment(ctx context.Context, videoID string, viewer *string) (*CommentRow, error) {
	c, err := scanComment(r.pool.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+`
WHERE c.video_id = $1::uuid AND c.is_pinned AND c.parent_comment_id IS NULL AND `+visibleComment, videoID, viewer))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

// TopLevelComments lists visible top-level comments (the pinned one is served separately).
func (r *Social) TopLevelComments(ctx context.Context, q CommentListQuery) ([]CommentRow, error) {
	args := []any{q.VideoID, q.Viewer}
	where := `c.video_id = $1::uuid AND c.parent_comment_id IS NULL AND ` + visibleComment + ` AND NOT c.is_pinned`
	order := `c.created_at DESC, c.id DESC`
	if q.Sort == "top" {
		order = `c.like_count DESC, c.id DESC`
		if q.After != nil {
			args = append(args, q.After.Likes, q.After.ID)
			where += ` AND (c.like_count, c.id) < ($3, $4::uuid)`
		}
	} else if q.After != nil {
		args = append(args, q.After.Time, q.After.ID)
		where += ` AND (c.created_at, c.id) < ($3, $4::uuid)`
	}
	args = append(args, q.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+commentCols+` `+commentFrom+` WHERE `+where+` ORDER BY `+order+` LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	return collectComments(rows)
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return digits[n : n+1]
	}
	return itoa(n/10) + digits[n%10:n%10+1]
}

// Replies lists replies oldest first.
func (r *Social) Replies(ctx context.Context, parentID string, viewer *string, after *CommentKey, limit int) ([]CommentRow, error) {
	args := []any{parentID, viewer}
	where := `c.parent_comment_id = $1::uuid AND ` + visibleComment
	if after != nil {
		args = append(args, after.Time, after.ID)
		where += ` AND (c.created_at, c.id) > ($3, $4::uuid)`
	}
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, `SELECT `+commentCols+` `+commentFrom+` WHERE `+where+` ORDER BY c.created_at ASC, c.id ASC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	return collectComments(rows)
}

type CommentRef struct {
	ID, VideoID, AuthorID, VideoCreatorID, Status string
	ParentID                                      *string
	Pinned                                        bool
}

// CommentRef finds a live comment regardless of video visibility (authors,
// creators and moderators must be able to delete after a video is hidden).
func (r *Social) CommentRef(ctx context.Context, id string) (*CommentRef, error) {
	var c CommentRef
	err := r.pool.QueryRow(ctx, `SELECT c.id::text, c.video_id::text, c.user_id::text, c.video_creator_id::text, c.status::text, c.parent_comment_id::text, c.is_pinned
FROM comments c WHERE c.id = $1::uuid AND c.deleted_at IS NULL`, id).
		Scan(&c.ID, &c.VideoID, &c.AuthorID, &c.VideoCreatorID, &c.Status, &c.ParentID, &c.Pinned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

type CreateCommentParams struct {
	ID, VideoID, AuthorID, Body, Status string // Status: VISIBLE | UNDER_REVIEW
	ParentID                            *string
	VideoCreatorID                      string
	ParentAuthorID                      *string
}

// CreateComment inserts the comment and queues notifications for the creator /
// the person replied to, in one transaction. Counters are updated by triggers.
func (r *Social) CreateComment(ctx context.Context, p CreateCommentParams) (CommentRow, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CommentRow{}, err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `INSERT INTO comments (id, video_id, video_creator_id, user_id, parent_comment_id, content, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7::comment_status)`,
		p.ID, p.VideoID, p.VideoCreatorID, p.AuthorID, p.ParentID, p.Body, p.Status); err != nil {
		return CommentRow{}, err
	}
	if p.Status == "VISIBLE" {
		const notify = `INSERT INTO notifications (user_id, type, actor_user_id, video_id, comment_id)
SELECT $1::uuid, $2::notification_type, $3::uuid, $4::uuid, $5::uuid
WHERE $1::uuid <> $3::uuid AND COALESCE((SELECT comment_notifications FROM user_settings WHERE user_id = $1::uuid), true)`
		if p.ParentAuthorID != nil {
			if _, err = tx.Exec(ctx, notify, *p.ParentAuthorID, "COMMENT_REPLY", p.AuthorID, p.VideoID, p.ID); err != nil {
				return CommentRow{}, err
			}
		}
		if p.ParentAuthorID == nil || *p.ParentAuthorID != p.VideoCreatorID {
			if _, err = tx.Exec(ctx, notify, p.VideoCreatorID, "COMMENT", p.AuthorID, p.VideoID, p.ID); err != nil {
				return CommentRow{}, err
			}
		}
	}
	// read back through the same query the list uses, so the response matches exactly
	row, err := scanComment(tx.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+` WHERE c.id = $1::uuid`, p.ID, p.AuthorID))
	if err != nil {
		return CommentRow{}, err
	}
	return row, tx.Commit(ctx)
}

func (r *Social) SetCommentLike(ctx context.Context, commentID, userID string, liked bool) error {
	if liked {
		_, err := r.pool.Exec(ctx, `INSERT INTO comment_reactions (user_id, comment_id, reaction) VALUES ($1::uuid, $2::uuid, 'LIKE') ON CONFLICT DO NOTHING`, userID, commentID)
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM comment_reactions WHERE comment_id = $1::uuid AND user_id = $2::uuid`, commentID, userID)
	return err
}

// DeleteComment soft-deletes a comment (and its replies when it is top-level).
// Triggers correct the video and parent counters.
func (r *Social) DeleteComment(ctx context.Context, c CommentRef, a *AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE comments SET status = 'DELETED', deleted_at = now(), is_pinned = false
WHERE (id = $1::uuid OR parent_comment_id = $1::uuid) AND status <> 'DELETED'`, c.ID); err != nil {
		return err
	}
	if a != nil {
		if err = audit(ctx, tx, *a); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// PinComment makes one top-level, visible comment the pinned one (or unpins).
func (r *Social) PinComment(ctx context.Context, c CommentRef, pinned bool) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if pinned {
		if _, err = tx.Exec(ctx, `UPDATE comments SET is_pinned = false WHERE video_id = $1::uuid AND is_pinned AND id <> $2::uuid`, c.VideoID, c.ID); err != nil {
			return false, err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE comments SET is_pinned = $2 WHERE id = $1::uuid AND parent_comment_id IS NULL AND status = 'VISIBLE'`, c.ID, pinned)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, tx.Commit(ctx)
}

// ---------- playlists ----------

const (
	MaxPlaylists      = 100
	MaxPlaylistVideos = 200
)

type PlaylistOutcome int

const (
	PlaylistOK PlaylistOutcome = iota
	PlaylistNotFound
	PlaylistFull
	PlaylistMismatch
)

type MyPlaylistRow struct {
	PlaylistRow
	Contains bool
}

// MyPlaylists lists the user's playlists. With videoID, Contains says whether the video is in each.
func (r *Social) MyPlaylists(ctx context.Context, userID string, videoID *string) ([]MyPlaylistRow, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+playlistCols("$1")+`,
  COALESCE($2::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM playlist_videos x WHERE x.playlist_id = pl.id AND x.video_id = $2::uuid), false)
FROM playlists pl JOIN users ou ON ou.id = pl.user_id
WHERE pl.user_id = $1::uuid AND pl.deleted_at IS NULL ORDER BY pl.updated_at DESC, pl.id DESC LIMIT $3`, userID, videoID, MaxPlaylists)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyPlaylistRow{}
	for rows.Next() {
		var m MyPlaylistRow
		if err := rows.Scan(&m.ID, &m.Title, &m.Description, &m.Visibility, &m.VideoCount, &m.ThumbKey, &m.OwnerName, &m.UpdatedAt, &m.Contains); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type PlaylistDetailRow struct {
	PlaylistRow
	OwnerID string
}

// Playlist returns a playlist the viewer may open: their own, or any PUBLIC/UNLISTED one.
func (r *Social) Playlist(ctx context.Context, id string, viewer *string) (*PlaylistDetailRow, error) {
	var d PlaylistDetailRow
	err := r.pool.QueryRow(ctx, `SELECT `+playlistCols("$2")+`, pl.user_id::text
FROM playlists pl JOIN users ou ON ou.id = pl.user_id
WHERE pl.id = $1::uuid AND pl.deleted_at IS NULL AND ou.deleted_at IS NULL AND (pl.user_id = $2::uuid OR (pl.visibility IN ('PUBLIC', 'UNLISTED') AND ou.status = 'ACTIVE'))`, id, viewer).
		Scan(&d.ID, &d.Title, &d.Description, &d.Visibility, &d.VideoCount, &d.ThumbKey, &d.OwnerName, &d.UpdatedAt, &d.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

// PlaylistVideos lists the videos in order, limited to ones listable for the viewer.
func (r *Social) PlaylistVideos(ctx context.Context, id string, viewer *string) ([]VideoRow, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+videoCols+` FROM playlist_videos pv JOIN videos v ON v.id = pv.video_id
  JOIN users u ON u.id = v.creator_id JOIN user_profiles p ON p.user_id = u.id JOIN video_stats vs ON vs.video_id = v.id
  LEFT JOIN video_metadata vm ON vm.video_id = v.id
  LEFT JOIN video_thumbnails t ON t.video_id = v.id AND t.is_primary AND t.status = 'READY'
WHERE pv.playlist_id = $1::uuid AND `+listable("$2")+` ORDER BY pv.position LIMIT $3`, id, viewer, MaxPlaylistVideos)
	if err != nil {
		return nil, err
	}
	return collectVideos(rows)
}

// CreatePlaylist enforces the per-user cap under a row lock. full=true when the cap is hit.
func (r *Social) CreatePlaylist(ctx context.Context, id, ownerID, title, visibility string, videoID *string) (full bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1::uuid FOR UPDATE`, ownerID); err != nil {
		return false, err
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM playlists WHERE user_id = $1::uuid AND deleted_at IS NULL`, ownerID).Scan(&n); err != nil {
		return false, err
	}
	if n >= MaxPlaylists {
		return true, nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO playlists (id, user_id, title, visibility) VALUES ($1::uuid, $2::uuid, $3, $4::playlist_visibility)`, id, ownerID, title, visibility); err != nil {
		return false, err
	}
	if videoID != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO playlist_videos (playlist_id, video_id, position) VALUES ($1::uuid, $2::uuid, 0)`, id, *videoID); err != nil {
			return false, err
		}
	}
	return false, tx.Commit(ctx)
}

func (r *Social) UpdatePlaylist(ctx context.Context, id, ownerID string, title, description, visibility *string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE playlists SET title = COALESCE($3, title), description = COALESCE($4, description), visibility = COALESCE($5::playlist_visibility, visibility)
WHERE id = $1::uuid AND user_id = $2::uuid AND deleted_at IS NULL`, id, ownerID, title, description, visibility)
	return tag.RowsAffected() == 1, err
}

func (r *Social) DeletePlaylist(ctx context.Context, id, ownerID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE playlists SET deleted_at = now() WHERE id = $1::uuid AND user_id = $2::uuid AND deleted_at IS NULL`, id, ownerID)
	return tag.RowsAffected() == 1, err
}

// lockPlaylist takes a row lock on an owned playlist so positions stay consistent.
func lockPlaylist(ctx context.Context, tx pgx.Tx, id, ownerID string) (bool, error) {
	var x int
	err := tx.QueryRow(ctx, `SELECT 1 FROM playlists WHERE id = $1::uuid AND user_id = $2::uuid AND deleted_at IS NULL FOR UPDATE`, id, ownerID).Scan(&x)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Social) AddPlaylistVideo(ctx context.Context, id, ownerID, videoID string) (PlaylistOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PlaylistNotFound, err
	}
	defer tx.Rollback(ctx)
	if ok, err := lockPlaylist(ctx, tx, id, ownerID); err != nil || !ok {
		return PlaylistNotFound, err
	}
	var exists bool
	var n, next int
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM playlist_videos WHERE playlist_id = $1::uuid AND video_id = $2::uuid), count(*), COALESCE(max(position) + 1, 0)
FROM playlist_videos WHERE playlist_id = $1::uuid`, id, videoID).Scan(&exists, &n, &next); err != nil {
		return PlaylistNotFound, err
	}
	if exists {
		return PlaylistOK, tx.Commit(ctx)
	}
	if n >= MaxPlaylistVideos {
		return PlaylistFull, nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO playlist_videos (playlist_id, video_id, position) VALUES ($1::uuid, $2::uuid, $3)`, id, videoID, next); err != nil {
		return PlaylistNotFound, err
	}
	if _, err = tx.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1::uuid`, id); err != nil {
		return PlaylistNotFound, err
	}
	return PlaylistOK, tx.Commit(ctx)
}

func (r *Social) RemovePlaylistVideo(ctx context.Context, id, ownerID, videoID string) (PlaylistOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PlaylistNotFound, err
	}
	defer tx.Rollback(ctx)
	if ok, err := lockPlaylist(ctx, tx, id, ownerID); err != nil || !ok {
		return PlaylistNotFound, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM playlist_videos WHERE playlist_id = $1::uuid AND video_id = $2::uuid`, id, videoID)
	if err != nil {
		return PlaylistNotFound, err
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, `UPDATE playlist_videos pv SET position = r.rn - 1 FROM (
  SELECT video_id, row_number() OVER (ORDER BY position) AS rn FROM playlist_videos WHERE playlist_id = $1::uuid) r
WHERE pv.playlist_id = $1::uuid AND pv.video_id = r.video_id`, id); err != nil {
			return PlaylistNotFound, err
		}
		if _, err = tx.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1::uuid`, id); err != nil {
			return PlaylistNotFound, err
		}
	}
	return PlaylistOK, tx.Commit(ctx)
}

// ReorderPlaylist applies a new order. videoIDs must be exactly the playlist's current set.
func (r *Social) ReorderPlaylist(ctx context.Context, id, ownerID string, videoIDs []string) (PlaylistOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PlaylistNotFound, err
	}
	defer tx.Rollback(ctx)
	if ok, err := lockPlaylist(ctx, tx, id, ownerID); err != nil || !ok {
		return PlaylistNotFound, err
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM playlist_videos WHERE playlist_id = $1::uuid) = cardinality($2::uuid[])
  AND NOT EXISTS (SELECT 1 FROM playlist_videos WHERE playlist_id = $1::uuid AND NOT (video_id = ANY($2::uuid[])))`, id, videoIDs).Scan(&same); err != nil {
		return PlaylistNotFound, err
	}
	if !same {
		return PlaylistMismatch, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE playlist_videos pv SET position = o.ord - 1
FROM unnest($2::uuid[]) WITH ORDINALITY AS o(video_id, ord) WHERE pv.playlist_id = $1::uuid AND pv.video_id = o.video_id`, id, videoIDs); err != nil {
		return PlaylistNotFound, err
	}
	if _, err = tx.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1::uuid`, id); err != nil {
		return PlaylistNotFound, err
	}
	return PlaylistOK, tx.Commit(ctx)
}
