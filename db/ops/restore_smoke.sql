-- Restore smoke test. Run against a freshly RESTORED copy (never against production) after every restore test:
--   psql "$RESTORED_URL" -v ON_ERROR_STOP=1 -f db/ops/restore_smoke.sql
-- Hard failures RAISE EXCEPTION (non-zero exit). Softer findings RAISE WARNING. Facts RAISE NOTICE.
DO $$
DECLARE
  v_version bigint;
  v_dirty   boolean;
  n         bigint;
  newest    timestamptz;
  t         text;
BEGIN
  -- 1. Migration state (golang-migrate keeps one row in schema_migrations)
  SELECT version, dirty INTO v_version, v_dirty FROM schema_migrations;
  IF v_version IS NULL THEN RAISE EXCEPTION 'schema_migrations is empty: not a REDXTUBE database?'; END IF;
  IF v_dirty THEN RAISE EXCEPTION 'migration % is marked dirty', v_version; END IF;
  RAISE NOTICE 'schema version: %', v_version;

  -- 2. Every table exists, and a quick look at the volume (informational: a new environment may be empty)
  FOREACH t IN ARRAY ARRAY['users', 'user_profiles', 'user_settings', 'sessions', 'verification_tokens', 'age_verifications', 'categories', 'tags', 'videos', 'video_stats', 'video_tags', 'video_metadata', 'video_variants', 'video_thumbnails', 'video_captions', 'upload_sessions', 'video_processing_jobs', 'video_search', 'watch_history', 'video_reactions', 'comments', 'comment_reactions', 'subscriptions', 'playlists', 'playlist_videos', 'watch_later', 'blocked_users', 'not_interested', 'notifications', 'search_history', 'reports', 'moderation_actions', 'audit_logs', 'video_views', 'video_daily_analytics', 'creator_daily_analytics', 'creator_daily_breakdown'] LOOP
    IF to_regclass('public.' || t) IS NULL THEN RAISE EXCEPTION 'missing table %', t; END IF;
  END LOOP;
  RAISE NOTICE 'users=% videos=% comments=%', (SELECT count(*) FROM users), (SELECT count(*) FROM videos), (SELECT count(*) FROM comments);

  -- 3. Credential hygiene survived the restore (the CHECK should make this impossible)
  SELECT count(*) INTO n FROM users WHERE password_hash !~ '^\$(argon2id|2[aby])\$';
  IF n > 0 THEN RAISE EXCEPTION '% users have a password_hash that is not argon2id/bcrypt', n; END IF;

  -- 4. Constraints are still validated (a restore must not leave NOT VALID constraints behind)
  SELECT count(*) INTO n FROM pg_constraint WHERE NOT convalidated;
  IF n > 0 THEN RAISE EXCEPTION '% constraints are not validated', n; END IF;

  -- 5. Partitions for the current and next month exist (otherwise view-event inserts will fail)
  SELECT count(*) INTO n FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
  WHERE i.inhparent = 'video_views'::regclass
    AND c.relname IN (
      'video_views_y' || to_char(now() AT TIME ZONE 'UTC', 'YYYY') || 'm' || to_char(now() AT TIME ZONE 'UTC', 'MM'),
      'video_views_y' || to_char((now() AT TIME ZONE 'UTC') + interval '1 month', 'YYYY') || 'm' || to_char((now() AT TIME ZONE 'UTC') + interval '1 month', 'MM'));
  IF n < 2 THEN RAISE WARNING 'missing video_views partition(s) for this/next month: run ensure_monthly_partitions()'; END IF;


  -- 6. Counter drift on a 1 % sample of videos (soft: repair with recount_video_counters())
  SELECT count(*) INTO n FROM (
    SELECT s.video_id FROM video_stats AS s TABLESAMPLE SYSTEM (1)
    WHERE s.like_count <> (SELECT count(*) FROM video_reactions r WHERE r.video_id = s.video_id AND r.reaction = 'LIKE')
    LIMIT 50) x;
  IF n > 0 THEN RAISE WARNING 'like_count drift found on % sampled videos', n; END IF;

  -- 7. Every video has its stats row; every user has profile + settings
  SELECT count(*) INTO n FROM videos v WHERE NOT EXISTS (SELECT 1 FROM video_stats s WHERE s.video_id = v.id);
  IF n > 0 THEN RAISE EXCEPTION '% videos have no video_stats row', n; END IF;
  SELECT count(*) INTO n FROM users u WHERE NOT EXISTS (SELECT 1 FROM user_profiles p WHERE p.user_id = u.id)
                                          OR NOT EXISTS (SELECT 1 FROM user_settings s WHERE s.user_id = u.id);
  IF n > 0 THEN RAISE EXCEPTION '% users are missing a profile or settings row', n; END IF;

  -- 8. Most recent write timestamps: compare with "now" to MEASURE the achieved RPO of this restore
  SELECT greatest(
           (SELECT max(created_at) FROM audit_logs),
           (SELECT max(created_at) FROM comments),
           (SELECT max(created_at) FROM notifications),
           (SELECT max(created_at) FROM sessions)) INTO newest;
  RAISE NOTICE 'newest write found: % (compare with the recovery target time to compute the achieved RPO)', newest;
  RAISE NOTICE 'restore smoke test finished';
END $$;
