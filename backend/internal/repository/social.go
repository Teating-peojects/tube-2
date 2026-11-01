package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Social holds the SQL for reactions, subscriptions, comments, history,
// watch later, playlists, blocking and reports. Counters (likes, comment and
// reply counts, subscriber counts) are maintained by database triggers, so
// nothing here updates them by hand.
type Social struct{ pool *pgxpool.Pool }

func NewSocial(pool *pgxpool.Pool) *Social { return &Social{pool: pool} }

// watchable: a video the viewer may open. PUBLIC and UNLISTED published videos
// by id; a creator's own published video in any visibility. Needs the aliases
// v (videos) and u (the creator's users row).
func watchable(viewerArg string) string {
	return `v.status = 'PUBLISHED' AND v.published_at IS NOT NULL AND v.deleted_at IS NULL AND v.moderation_status <> 'REMOVED' AND ` + activeCreator +
		` AND ((v.visibility IN ('PUBLIC', 'UNLISTED') AND v.moderation_status = 'OK') OR v.creator_id = ` + viewerArg + `::uuid)`
}

// listable: videos that may be *shown in lists* to the viewer: published PUBLIC
// videos (and the viewer's own). UNLISTED videos open by link but never appear
// in a listing, including inside someone else's playlist.
func listable(viewerArg string) string {
	return `v.status = 'PUBLISHED' AND v.published_at IS NOT NULL AND v.deleted_at IS NULL AND v.moderation_status <> 'REMOVED' AND ` + activeCreator +
		` AND ((v.visibility = 'PUBLIC' AND v.moderation_status = 'OK') OR v.creator_id = ` + viewerArg + `::uuid)`
}

// ---------- shared lookups ----------

type VideoAccess struct {
	ID, CreatorID, CommentsMode, Visibility string
	DurationSec                             *int32
}

// VideoAccess returns nil when the video does not exist or the viewer may not open it.
func (r *Social) VideoAccess(ctx context.Context, videoID string, viewerID *string) (*VideoAccess, error) {
	var a VideoAccess
	err := r.pool.QueryRow(ctx, `SELECT v.id::text, v.creator_id::text, v.comments_mode::text, v.visibility::text, v.duration_seconds
FROM videos v JOIN users u ON u.id = v.creator_id WHERE v.id = $1::uuid AND `+watchable("$2"), videoID, viewerID).
		Scan(&a.ID, &a.CreatorID, &a.CommentsMode, &a.Visibility, &a.DurationSec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// ActiveUser reports whether the channel/user exists and is in good standing.
func (r *Social) ActiveUser(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u WHERE u.id = $1::uuid AND `+activeCreator+`)`, id).Scan(&ok)
	return ok, err
}

// ---------- reactions ----------

func (r *Social) SetReaction(ctx context.Context, videoID, userID, reaction string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO video_reactions (user_id, video_id, reaction) VALUES ($1::uuid, $2::uuid, $3::reaction_type)
ON CONFLICT (user_id, video_id) DO UPDATE SET reaction = EXCLUDED.reaction, created_at = now() WHERE video_reactions.reaction <> EXCLUDED.reaction`,
		userID, videoID, reaction)
	return err
}

func (r *Social) ClearReaction(ctx context.Context, videoID, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM video_reactions WHERE video_id = $1::uuid AND user_id = $2::uuid`, videoID, userID)
	return err
}

// ReactionFor returns "like", "dislike" or "" for the viewer.
func (r *Social) ReactionFor(ctx context.Context, videoID, userID string) (string, error) {
	var s string
	err := r.pool.QueryRow(ctx, `SELECT lower(reaction::text) FROM video_reactions WHERE video_id = $1::uuid AND user_id = $2::uuid`, videoID, userID).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return s, err
}

// ---------- subscriptions ----------

// Subscribe returns true when a new subscription was created.
func (r *Social) Subscribe(ctx context.Context, subscriber, channel string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO subscriptions (subscriber_id, creator_id) VALUES ($1::uuid, $2::uuid) ON CONFLICT DO NOTHING`, subscriber, channel)
	if err != nil {
		return false, err
	}
	created := tag.RowsAffected() == 1
	if created {
		// one "new subscriber" notice per person per day: toggling cannot spam the creator
		if _, err = tx.Exec(ctx, `INSERT INTO notifications (user_id, type, actor_user_id)
SELECT $2::uuid, 'NEW_SUBSCRIBER', $1::uuid
WHERE COALESCE((SELECT subscriber_notifications FROM user_settings WHERE user_id = $2::uuid), true)
  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.user_id = $2::uuid AND n.type = 'NEW_SUBSCRIBER' AND n.actor_user_id = $1::uuid AND n.created_at > now() - interval '24 hours')`,
			subscriber, channel); err != nil {
			return false, err
		}
	}
	return created, tx.Commit(ctx)
}

func (r *Social) Unsubscribe(ctx context.Context, subscriber, channel string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM subscriptions WHERE subscriber_id = $1::uuid AND creator_id = $2::uuid`, subscriber, channel)
	return err
}

// SubscribedAmong returns which of the channel ids the viewer subscribes to.
func (r *Social) SubscribedAmong(ctx context.Context, viewer string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT creator_id::text FROM subscriptions WHERE subscriber_id = $1::uuid AND creator_id = ANY($2::uuid[])`, viewer, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (r *Social) SubscribedChannels(ctx context.Context, userID string, limit int) ([]ChannelRow, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+channelCols+` FROM subscriptions s JOIN users u ON u.id = s.creator_id JOIN user_profiles p ON p.user_id = u.id
WHERE s.subscriber_id = $1::uuid AND `+activeCreator+` ORDER BY lower(u.display_name), u.id LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return collectChannels(rows)
}

// KeyedVideoRow is a video plus the sort key of the join table it was listed through.
type KeyedVideoRow struct {
	VideoRow
	Key      time.Time
	Progress int32 // history only
}

func collectKeyed(rows pgx.Rows, withProgress bool) ([]KeyedVideoRow, error) {
	defer rows.Close()
	out := []KeyedVideoRow{}
	for rows.Next() {
		var k KeyedVideoRow
		var err error
		if withProgress {
			k.VideoRow, err = scanVideo(rows, &k.Key, &k.Progress)
		} else {
			k.VideoRow, err = scanVideo(rows, &k.Key)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

type KeyedAfter struct {
	Time time.Time
	ID   string
}

// listVia lists videos through a per-user join table with keyset pagination.
func (r *Social) listVia(ctx context.Context, joinTable, userCol, keyCol, extraCols, extraWhere string, userID string, after *KeyedAfter, limit int) (pgx.Rows, error) {
	args := []any{userID, limit}
	keyset := ""
	if after != nil {
		keyset = ` AND (j.` + keyCol + `, j.video_id) < ($3, $4::uuid)`
		args = append(args, after.Time, after.ID)
	}
	sql := `SELECT ` + videoCols + `, j.` + keyCol + extraCols + ` FROM ` + joinTable + ` j JOIN videos v ON v.id = j.video_id
  JOIN users u ON u.id = v.creator_id JOIN user_profiles p ON p.user_id = u.id JOIN video_stats vs ON vs.video_id = v.id
  LEFT JOIN video_metadata vm ON vm.video_id = v.id
  LEFT JOIN video_thumbnails t ON t.video_id = v.id AND t.is_primary AND t.status = 'READY'
WHERE j.` + userCol + ` = $1::uuid` + extraWhere + ` AND ` + watchable("$1") + keyset + `
ORDER BY j.` + keyCol + ` DESC, j.video_id DESC LIMIT $2`
	return r.pool.Query(ctx, sql, args...)
}

func (r *Social) WatchLater(ctx context.Context, userID string, after *KeyedAfter, limit int) ([]KeyedVideoRow, error) {
	rows, err := r.listVia(ctx, "watch_later", "user_id", "created_at", "", "", userID, after, limit)
	if err != nil {
		return nil, err
	}
	return collectKeyed(rows, false)
}

func (r *Social) Liked(ctx context.Context, userID string, after *KeyedAfter, limit int) ([]KeyedVideoRow, error) {
	rows, err := r.listVia(ctx, "video_reactions", "user_id", "created_at", "", " AND j.reaction = 'LIKE'", userID, after, limit)
	if err != nil {
		return nil, err
	}
	return collectKeyed(rows, false)
}

func (r *Social) History(ctx context.Context, userID string, after *KeyedAfter, limit int) ([]KeyedVideoRow, error) {
	rows, err := r.listVia(ctx, "watch_history", "user_id", "last_watched_at", ", j.last_position_seconds", "", userID, after, limit)
	if err != nil {
		return nil, err
	}
	return collectKeyed(rows, true)
}

// SubscriptionFeed lists new public videos from subscribed channels.
func (r *Social) SubscriptionFeed(ctx context.Context, userID string, after *Keyset, limit int) ([]VideoRow, error) {
	args := []any{userID, limit}
	keyset := ""
	if after != nil {
		keyset = ` AND (v.published_at, v.id) < ($3, $4::uuid)`
		args = append(args, after.Time, after.ID)
	}
	rows, err := r.pool.Query(ctx, `SELECT `+videoCols+` `+videoFrom+`
WHERE v.creator_id IN (SELECT creator_id FROM subscriptions WHERE subscriber_id = $1::uuid)
  AND `+publicVideo+` AND `+activeCreator+` AND v.video_type = 'LONG'`+keyset+`
ORDER BY v.published_at DESC, v.id DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	return collectVideos(rows)
}

// ---------- watch later ----------

const MaxWatchLater = 500

// AddWatchLater adds a video the viewer can open. full=true when the cap is hit.
func (r *Social) AddWatchLater(ctx context.Context, userID, videoID string) (added bool, full bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)
	// lock the user's row so two parallel adds cannot both slip past the cap
	if _, err = tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1::uuid FOR UPDATE`, userID); err != nil {
		return false, false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM watch_later WHERE user_id = $1::uuid AND video_id = $2::uuid)`, userID, videoID).Scan(&exists); err != nil {
		return false, false, err
	}
	if exists {
		return false, false, tx.Commit(ctx)
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM watch_later WHERE user_id = $1::uuid`, userID).Scan(&n); err != nil {
		return false, false, err
	}
	if n >= MaxWatchLater {
		return false, true, nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO watch_later (user_id, video_id) VALUES ($1::uuid, $2::uuid)`, userID, videoID); err != nil {
		return false, false, err
	}
	return true, false, tx.Commit(ctx)
}

func (r *Social) RemoveWatchLater(ctx context.Context, userID, videoID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM watch_later WHERE user_id = $1::uuid AND video_id = $2::uuid`, userID, videoID)
	return err
}

func (r *Social) WatchLaterIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT video_id::text FROM watch_later WHERE user_id = $1::uuid ORDER BY created_at DESC, video_id DESC LIMIT $2`, userID, MaxWatchLater)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------- views, progress, history ----------

type ViewEvent struct {
	VideoID, ViewerID, SessionID, Country, Device, Source string
	IPHash                                                []byte
}

// RecordView stores a counted view and bumps the counters. view_count is
// updated per view for now; docs/db-adaptation.md covers moving it to a Redis
// counter flushed in batches once a single video gets hot.
func (r *Social) RecordView(ctx context.Context, e ViewEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var viewer, country any
	if e.ViewerID != "" {
		viewer = e.ViewerID
	}
	if e.Country != "" {
		country = e.Country
	}
	if _, err = tx.Exec(ctx, `INSERT INTO video_views (video_id, user_id, session_id, ip_hash, country_code, device_type, traffic_source)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::device_type, $7::traffic_source)`,
		e.VideoID, viewer, e.SessionID, e.IPHash, country, e.Device, e.Source); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE video_stats SET view_count = view_count + 1 WHERE video_id = $1::uuid`, e.VideoID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO video_daily_analytics (video_id, date, views) VALUES ($1::uuid, (now() AT TIME ZONE 'UTC')::date, 1)
ON CONFLICT (video_id, date) DO UPDATE SET views = video_daily_analytics.views + 1`, e.VideoID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Social) SavesHistory(ctx context.Context, userID string) (bool, error) {
	var save bool
	err := r.pool.QueryRow(ctx, `SELECT COALESCE((SELECT save_watch_history FROM user_settings WHERE user_id = $1::uuid), true)`, userID).Scan(&save)
	return save, err
}

// TouchHistory marks a video as watched now without changing progress.
func (r *Social) TouchHistory(ctx context.Context, userID, videoID string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO watch_history (user_id, video_id) VALUES ($1::uuid, $2::uuid)
ON CONFLICT (user_id, video_id) DO UPDATE SET last_watched_at = now()`, userID, videoID)
	return err
}

// AddProgress records the resume position and adds watched seconds.
func (r *Social) AddProgress(ctx context.Context, userID, videoID string, position, watched int32, completed bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO watch_history (user_id, video_id, last_position_seconds, watch_duration_seconds, completed) VALUES ($1::uuid, $2::uuid, $3, $4, $5)
ON CONFLICT (user_id, video_id) DO UPDATE SET last_position_seconds = EXCLUDED.last_position_seconds,
  watch_duration_seconds = watch_history.watch_duration_seconds + EXCLUDED.watch_duration_seconds,
  completed = watch_history.completed OR EXCLUDED.completed, last_watched_at = now()`, userID, videoID, position, watched, completed); err != nil {
		return err
	}
	if watched > 0 {
		if _, err = tx.Exec(ctx, `UPDATE video_stats SET watch_time_seconds = watch_time_seconds + $2 WHERE video_id = $1::uuid`, videoID, watched); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO video_daily_analytics (video_id, date, watch_time_seconds) VALUES ($1::uuid, (now() AT TIME ZONE 'UTC')::date, $2)
ON CONFLICT (video_id, date) DO UPDATE SET watch_time_seconds = video_daily_analytics.watch_time_seconds + EXCLUDED.watch_time_seconds`, videoID, watched); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Social) DeleteHistoryItem(ctx context.Context, userID, videoID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM watch_history WHERE user_id = $1::uuid AND video_id = $2::uuid`, userID, videoID)
	return err
}

func (r *Social) ClearHistory(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM watch_history WHERE user_id = $1::uuid`, userID)
	return err
}

// ---------- not interested / block ----------

func (r *Social) NotInterested(ctx context.Context, userID, videoID string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO not_interested (user_id, video_id) VALUES ($1::uuid, $2::uuid) ON CONFLICT DO NOTHING`, userID, videoID)
	return err
}

// Block hides a channel from the blocker's feeds and removes any subscription.
func (r *Social) Block(ctx context.Context, blocker, blocked string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO blocked_users (user_id, blocked_user_id) VALUES ($1::uuid, $2::uuid) ON CONFLICT DO NOTHING`, blocker, blocked); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM subscriptions WHERE subscriber_id = $1::uuid AND creator_id = $2::uuid`, blocker, blocked); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Social) Unblock(ctx context.Context, blocker, blocked string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM blocked_users WHERE user_id = $1::uuid AND blocked_user_id = $2::uuid`, blocker, blocked)
	return err
}

// ---------- reports ----------

var ErrAlreadyReported = errors.New("already reported")

type ReportInput struct {
	ReporterID, TargetType, TargetID, Reason, Details string
}

// CreateReport files a report. The schema keeps evidence in place (targets are
// FK-protected from deletion) and a trigger sets the priority from the reason.
// A reporter can only have one open report per target (unique partial indexes).
func (r *Social) CreateReport(ctx context.Context, in ReportInput) error {
	var details any
	if in.Details != "" {
		details = in.Details
	}
	var video, comment, user any
	switch in.TargetType {
	case "VIDEO":
		video = in.TargetID
	case "COMMENT":
		comment = in.TargetID
	default:
		user = in.TargetID
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var priority int
	err = tx.QueryRow(ctx, `INSERT INTO reports (reporter_user_id, target_type, target_video_id, target_comment_id, target_user_id, reason, description)
VALUES ($1::uuid, $2::report_target, $3::uuid, $4::uuid, $5::uuid, $6::report_reason, $7) RETURNING priority::int`,
		in.ReporterID, in.TargetType, video, comment, user, in.Reason, details).Scan(&priority)
	if isUnique(err) {
		return ErrAlreadyReported
	}
	if err != nil {
		return err
	}
	if in.TargetType == "VIDEO" && priority == 0 {
		if err = autoRestrict(ctx, tx, in.TargetID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// AutoRestrictReporters is how many independent, established accounts must file an URGENT
// report (underage / non-consensual / illegal) before a video is hidden pending review.
// Only verified accounts older than AutoRestrictMinAge count, so throwaway accounts cannot
// take down someone's video. Restricting is reversible (a moderator restores it).
const (
	AutoRestrictReporters = 3
	qAutoRestrictCount    = `SELECT count(DISTINCT r.reporter_user_id) FROM reports r JOIN users u ON u.id = r.reporter_user_id
WHERE r.target_video_id = $1::uuid AND r.priority = 0 AND r.status IN ('PENDING', 'UNDER_REVIEW')
  AND u.is_verified AND u.status = 'ACTIVE' AND u.created_at < now() - interval '3 days'`
	qAutoRestrict = `UPDATE videos SET moderation_status = 'RESTRICTED', updated_at = now() WHERE id = $1::uuid AND moderation_status = 'OK'`
)

func autoRestrict(ctx context.Context, tx pgx.Tx, videoID string) error {
	var n int
	if err := tx.QueryRow(ctx, qAutoRestrictCount, videoID).Scan(&n); err != nil {
		return err
	}
	if n < AutoRestrictReporters {
		return nil
	}
	tag, err := tx.Exec(ctx, qAutoRestrict, videoID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, metadata)
VALUES (NULL, 'moderation.auto_restrict', 'video', $1, jsonb_build_object('urgent_reporters', $2::int))`, videoID, n)
	return err
}
