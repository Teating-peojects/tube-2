-- Example queries for the API. Parameters are $1, $2, ... (use pgx; never string-concatenate SQL).
-- Pagination is KEYSET-based (stable and index-friendly); never use OFFSET on large tables.

-- ============ DISCOVERY (all use the partial "public" indexes) ============
-- Home feed, first page / next page (cursor = last row's published_at, id)
SELECT v.id, v.title, v.duration_seconds, v.published_at, u.id AS creator_id, u.display_name, u.is_verified,
       s.view_count, p.thumb_key
FROM public_videos v
JOIN users u        ON u.id = v.creator_id
JOIN video_stats s  ON s.video_id = v.id
LEFT JOIN LATERAL (SELECT storage_key AS thumb_key FROM video_thumbnails t WHERE t.video_id = v.id AND t.is_primary) p ON true
WHERE ($1::timestamptz IS NULL OR (v.published_at, v.id) < ($1, $2::uuid))
  AND NOT EXISTS (SELECT 1 FROM blocked_users b WHERE b.user_id = $3::uuid AND b.blocked_user_id = v.creator_id)
ORDER BY v.published_at DESC, v.id DESC
LIMIT 24;

-- Category feed
SELECT v.id, v.title, v.published_at FROM public_videos v
WHERE v.category_id = $1 AND ($2::timestamptz IS NULL OR (v.published_at, v.id) < ($2, $3::uuid))
ORDER BY v.published_at DESC, v.id DESC LIMIT 24;

-- Shorts feed
SELECT v.id, v.title, v.published_at FROM public_videos v
WHERE v.video_type = 'SHORT' AND ($1::timestamptz IS NULL OR (v.published_at, v.id) < ($1, $2::uuid))
ORDER BY v.published_at DESC, v.id DESC LIMIT 6;

-- Trending: views over the last 7 days (cache the result in Redis for ~5 minutes)
SELECT a.video_id, sum(a.views) AS views
FROM video_daily_analytics a
JOIN public_videos v ON v.id = a.video_id
WHERE a.date >= (now() AT TIME ZONE 'UTC')::date - 7
GROUP BY a.video_id ORDER BY views DESC LIMIT 30;

-- Subscriptions feed (fine up to a few hundred subscriptions; beyond that use a Redis timeline)
SELECT v.id, v.title, v.published_at FROM public_videos v
WHERE v.creator_id IN (SELECT creator_id FROM subscriptions WHERE subscriber_id = $1)
  AND ($2::timestamptz IS NULL OR (v.published_at, v.id) < ($2, $3::uuid))
ORDER BY v.published_at DESC, v.id DESC LIMIT 24;

-- ============ SEARCH ============
-- Videos: weighted full-text rank + filters (type/duration/date), keyset on (rank, id) or simple LIMIT for page 1
SELECT v.id, v.title, ts_rank_cd(vs.document, q.query) AS rank
FROM video_search vs
JOIN public_videos v ON v.id = vs.video_id
CROSS JOIN (SELECT websearch_to_tsquery('simple', $1) AS query) q
WHERE vs.document @@ q.query
  AND ($2::text IS NULL OR ($2 = 'short' AND v.duration_seconds < 240) OR ($2 = 'medium' AND v.duration_seconds BETWEEN 240 AND 1200) OR ($2 = 'long' AND v.duration_seconds > 1200))
ORDER BY rank DESC, v.published_at DESC LIMIT 20;

-- Autocomplete suggestions (trigram prefix/typo tolerant)
SELECT title FROM public_videos WHERE title ILIKE $1 || '%' OR title % $1 ORDER BY similarity(title, $1) DESC LIMIT 6;

-- Channels
SELECT u.id, u.display_name, p.subscriber_count FROM users u JOIN user_profiles p ON p.user_id = u.id
WHERE u.is_creator AND u.status = 'ACTIVE' AND u.display_name % $1 ORDER BY similarity(u.display_name, $1) DESC LIMIT 10;

-- Remember a search (respect user_settings.save_search_history)
INSERT INTO search_history (user_id, query) VALUES ($1, $2)
ON CONFLICT (user_id, query) DO UPDATE SET created_at = now();

-- ============ WATCH PAGE ============
SELECT v.*, s.view_count, s.like_count, s.dislike_count, s.comment_count,
       p.subscriber_count, u.display_name, u.is_verified,
       (SELECT reaction FROM video_reactions r WHERE r.user_id = $2 AND r.video_id = v.id) AS my_reaction,
       EXISTS (SELECT 1 FROM subscriptions x WHERE x.subscriber_id = $2 AND x.creator_id = v.creator_id) AS subscribed
FROM videos v
JOIN video_stats s ON s.video_id = v.id
JOIN users u ON u.id = v.creator_id
JOIN user_profiles p ON p.user_id = u.id
WHERE v.id = $1 AND v.deleted_at IS NULL AND v.status = 'PUBLISHED' AND v.moderation_status <> 'REMOVED'
  AND (v.visibility <> 'PRIVATE' OR v.creator_id = $2);

-- React (upsert; counters follow by trigger) / remove
INSERT INTO video_reactions (user_id, video_id, reaction) VALUES ($1, $2, $3)
ON CONFLICT (user_id, video_id) DO UPDATE SET reaction = EXCLUDED.reaction WHERE video_reactions.reaction <> EXCLUDED.reaction;
DELETE FROM video_reactions WHERE user_id = $1 AND video_id = $2;

-- Subscribe / unsubscribe (idempotent)
INSERT INTO subscriptions (subscriber_id, creator_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;
DELETE FROM subscriptions WHERE subscriber_id = $1 AND creator_id = $2;

-- ============ COMMENTS ============
-- Top-level, newest first (page cursor = created_at, id). Sort "top": ORDER BY like_count DESC, created_at DESC, id DESC.
SELECT c.id, c.content, c.created_at, c.like_count, c.reply_count, u.display_name, (c.user_id = c.video_creator_id) AS is_creator
FROM comments c JOIN users u ON u.id = c.user_id
WHERE c.video_id = $1 AND c.parent_comment_id IS NULL AND c.status = 'VISIBLE'
  AND ($2::timestamptz IS NULL OR (c.created_at, c.id) < ($2, $3::uuid))
ORDER BY c.created_at DESC, c.id DESC LIMIT 20;
-- Pinned comment (served separately, at most one)
SELECT * FROM comments WHERE video_id = $1 AND is_pinned AND status = 'VISIBLE';
-- Replies
SELECT c.id, c.content, c.created_at, u.display_name FROM comments c JOIN users u ON u.id = c.user_id
WHERE c.parent_comment_id = $1 AND c.status = 'VISIBLE' ORDER BY c.created_at, c.id LIMIT 50;
-- Create: status depends on videos.comments_mode (ON -> VISIBLE, REVIEW -> UNDER_REVIEW, OFF -> reject in the API)
INSERT INTO comments (video_id, video_creator_id, user_id, parent_comment_id, content, status)
SELECT v.id, v.creator_id, $2, $3, $4, CASE v.comments_mode WHEN 'REVIEW' THEN 'UNDER_REVIEW'::comment_status ELSE 'VISIBLE'::comment_status END
FROM videos v WHERE v.id = $1 AND v.comments_mode <> 'OFF' RETURNING id;
-- Studio: approve a held comment
UPDATE comments SET status = 'VISIBLE' WHERE id = $1 AND video_creator_id = $2 AND status = 'UNDER_REVIEW';
-- Studio inbox
SELECT c.id, c.content, c.status, c.created_at, v.title FROM comments c JOIN videos v ON v.id = c.video_id
WHERE c.video_creator_id = $1 AND c.status IN ('VISIBLE', 'UNDER_REVIEW') ORDER BY c.created_at DESC, c.id DESC LIMIT 20;

-- ============ LIBRARY ============
-- Save playback progress (respect user_settings.save_watch_history)
INSERT INTO watch_history (user_id, video_id, last_position_seconds, watch_duration_seconds, completed, last_watched_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (user_id, video_id) DO UPDATE SET
  last_position_seconds = EXCLUDED.last_position_seconds,
  watch_duration_seconds = watch_history.watch_duration_seconds + EXCLUDED.watch_duration_seconds,
  completed = watch_history.completed OR EXCLUDED.completed,
  last_watched_at = now();
-- History page / continue watching
SELECT h.video_id, h.last_position_seconds, h.last_watched_at FROM watch_history h
WHERE h.user_id = $1 AND ($2::timestamptz IS NULL OR (h.last_watched_at, h.video_id) < ($2, $3::uuid))
ORDER BY h.last_watched_at DESC, h.video_id DESC LIMIT 20;
SELECT video_id FROM watch_history WHERE user_id = $1 AND completed = false AND last_position_seconds > 0 ORDER BY last_watched_at DESC LIMIT 6;
-- Watch later: toggle + ids
INSERT INTO watch_later (user_id, video_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;
DELETE FROM watch_later WHERE user_id = $1 AND video_id = $2;
SELECT video_id FROM watch_later WHERE user_id = $1;
-- Playlist: append at the end, reorder in ONE statement (position uniqueness is deferred), remove
INSERT INTO playlist_videos (playlist_id, video_id, position)
SELECT $1, $2, COALESCE(MAX(position) + 1, 0) FROM playlist_videos WHERE playlist_id = $1
ON CONFLICT DO NOTHING;
UPDATE playlist_videos pv SET position = (o.ord - 1)
FROM unnest($2::uuid[]) WITH ORDINALITY AS o(video_id, ord)
WHERE pv.playlist_id = $1 AND pv.video_id = o.video_id;
-- Which of my playlists contain this video (Save menu)
SELECT p.id, p.title, EXISTS (SELECT 1 FROM playlist_videos pv WHERE pv.playlist_id = p.id AND pv.video_id = $2) AS contains
FROM playlists p WHERE p.user_id = $1 AND p.deleted_at IS NULL ORDER BY p.updated_at DESC;

-- ============ NOTIFICATIONS ============
SELECT id, type, actor_user_id, video_id, comment_id, payload, is_read, created_at FROM notifications
WHERE user_id = $1 AND ($2::bigint IS NULL OR id < $2) ORDER BY created_at DESC, id DESC LIMIT 20;
SELECT count(*) FROM (SELECT 1 FROM notifications WHERE user_id = $1 AND read_at IS NULL LIMIT 100) x;   -- capped badge count
UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL AND (cardinality($2::bigint[]) = 0 OR id = ANY($2));

-- ============ AUTH ============
-- Session lookup by token hash (every authenticated request; cache the result in Redis for ~60 s)
SELECT s.id, s.user_id, u.role, u.status FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.session_token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.deleted_at IS NULL;
-- Consume a verification token exactly once (atomic: concurrent attempts, replays and expiry all fail)
UPDATE verification_tokens SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND expires_at > now() AND attempts < max_attempts AND token_hash = $2
RETURNING user_id, purpose, target;
-- Count a failed attempt
UPDATE verification_tokens SET attempts = attempts + 1 WHERE id = $1 AND consumed_at IS NULL AND attempts < max_attempts;
-- Revoke every session of a user (password change / "log out everywhere")
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- ============ SAFETY ============
-- Report (409 "already reported" = unique violation on reports_open_*_once)
INSERT INTO reports (reporter_user_id, target_type, target_video_id, reason, description) VALUES ($1, 'VIDEO', $2, $3, $4) RETURNING id;
-- Moderator queue (urgent first)
SELECT id, target_type, reason, priority, created_at FROM reports WHERE status IN ('PENDING', 'UNDER_REVIEW') ORDER BY priority, created_at LIMIT 50;
-- Action + state change in ONE transaction (the DB cannot do this for you)
--   INSERT INTO moderation_actions (...) VALUES (...);
--   UPDATE videos SET moderation_status = 'REMOVED' WHERE id = $1;
--   UPDATE reports SET status = 'RESOLVED', resolved_at = now(), resolved_by = $2 WHERE id = $3;
--   INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, metadata) VALUES (...);

-- ============ STUDIO ============
SELECT v.id, v.title, v.status, v.visibility, v.created_at, s.view_count, s.comment_count, s.like_count
FROM videos v JOIN video_stats s ON s.video_id = v.id
WHERE v.creator_id = $1 AND v.deleted_at IS NULL AND ($2::text = 'all' OR ($2 = 'private' AND v.visibility = 'PRIVATE') OR ($2 = 'unlisted' AND v.visibility = 'UNLISTED')
      OR ($2 = 'processing' AND v.status IN ('UPLOADING', 'PROCESSING')) OR ($2 = 'draft' AND v.status IN ('READY', 'FAILED'))
      OR ($2 = 'published' AND v.status = 'PUBLISHED' AND v.visibility = 'PUBLIC'))
ORDER BY v.created_at DESC, v.id DESC LIMIT 20;
-- Overview chart
SELECT date, views, watch_time_seconds FROM creator_daily_analytics WHERE creator_id = $1 AND date >= $2 ORDER BY date;
-- Audience / traffic panels
SELECT key, sum(views) AS views FROM creator_daily_breakdown WHERE creator_id = $1 AND dimension = $2 AND date >= $3 GROUP BY key ORDER BY views DESC LIMIT 5;

-- ============ ANALYTICS WRITE PATH ============
-- 1. Redis INCR per view; every ~30 s a worker drains the counters and flushes in one statement:
UPDATE video_stats s SET view_count = s.view_count + d.views, watch_time_seconds = s.watch_time_seconds + d.watch, updated_at = now()
FROM unnest($1::uuid[], $2::bigint[], $3::bigint[]) AS d(video_id, views, watch) WHERE s.video_id = d.video_id;
INSERT INTO video_daily_analytics (video_id, date, views, watch_time_seconds)
SELECT d.video_id, (now() AT TIME ZONE 'UTC')::date, d.views, d.watch FROM unnest($1::uuid[], $2::bigint[], $3::bigint[]) AS d(video_id, views, watch)
ON CONFLICT (video_id, date) DO UPDATE SET views = video_daily_analytics.views + EXCLUDED.views,
  watch_time_seconds = video_daily_analytics.watch_time_seconds + EXCLUDED.watch_time_seconds;
-- 2. Creator rollup, recomputed for "today" every few minutes from the video rows (idempotent, no drift)
INSERT INTO creator_daily_analytics (creator_id, date, views, watch_time_seconds)
SELECT v.creator_id, a.date, sum(a.views), sum(a.watch_time_seconds) FROM video_daily_analytics a JOIN videos v ON v.id = a.video_id
WHERE a.date = $1 GROUP BY v.creator_id, a.date
ON CONFLICT (creator_id, date) DO UPDATE SET views = EXCLUDED.views, watch_time_seconds = EXCLUDED.watch_time_seconds;

-- ============ MAINTENANCE ============
-- Drift check (run nightly; alert if it returns rows, then call recount_video_counters)
SELECT s.video_id, s.like_count, (SELECT count(*) FROM video_reactions r WHERE r.video_id = s.video_id AND r.reaction = 'LIKE') AS actual_likes
FROM video_stats s WHERE s.like_count <> (SELECT count(*) FROM video_reactions r WHERE r.video_id = s.video_id AND r.reaction = 'LIKE') LIMIT 100;
-- Retention (loop until 0): SELECT purge_expired_sessions(10000); SELECT purge_old_search_history(10000, 90); ...
-- Monthly: SELECT ensure_monthly_partitions('video_views', 3); SELECT drop_old_partitions('video_views', 3);
