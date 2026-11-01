-- 000009 functions: search document refresh, counter reconciliation, retention helpers.

-- ---------- search ----------
-- Weights: title A, tags B, creator name C, description D. 'simple' config = no English stemming, so
-- Bengali/Hindi/other languages work. Switch to a per-language config later if you need stemming.
CREATE FUNCTION refresh_video_search(p_video uuid) RETURNS void LANGUAGE sql AS $$
  INSERT INTO video_search (video_id, document, updated_at)
  SELECT v.id,
         setweight(to_tsvector('simple', v.title), 'A')
         || setweight(to_tsvector('simple', coalesce((
              SELECT string_agg(t.name, ' ') FROM video_tags vt JOIN tags t ON t.id = vt.tag_id WHERE vt.video_id = v.id
            ), '')), 'B')
         || setweight(to_tsvector('simple', u.display_name || ' ' || u.username::text), 'C')
         || setweight(to_tsvector('simple', v.description), 'D'),
         now()
  FROM videos v JOIN users u ON u.id = v.creator_id
  WHERE v.id = p_video
  ON CONFLICT (video_id) DO UPDATE SET document = EXCLUDED.document, updated_at = EXCLUDED.updated_at
$$;

-- ---------- counter reconciliation (nightly job; also usable ad hoc) ----------
CREATE FUNCTION recount_video_counters(p_video uuid) RETURNS void LANGUAGE sql AS $$
  UPDATE video_stats s
  SET like_count    = (SELECT count(*) FROM video_reactions r WHERE r.video_id = p_video AND r.reaction = 'LIKE'),
      dislike_count = (SELECT count(*) FROM video_reactions r WHERE r.video_id = p_video AND r.reaction = 'DISLIKE'),
      comment_count = (SELECT count(*) FROM comments c WHERE c.video_id = p_video AND c.status = 'VISIBLE' AND c.deleted_at IS NULL),
      updated_at    = now()
  WHERE s.video_id = p_video
$$;

CREATE FUNCTION recount_creator_counters(p_user uuid) RETURNS void LANGUAGE sql AS $$
  UPDATE user_profiles p
  SET subscriber_count = (SELECT count(*) FROM subscriptions s WHERE s.creator_id = p_user),
      video_count = (
        SELECT count(*) FROM videos v
        WHERE v.creator_id = p_user AND v.status = 'PUBLISHED' AND v.visibility = 'PUBLIC'
          AND v.moderation_status = 'OK' AND v.deleted_at IS NULL
      )
  WHERE p.user_id = p_user
$$;

CREATE FUNCTION recount_comment_counters(p_comment uuid) RETURNS void LANGUAGE sql AS $$
  UPDATE comments c
  SET like_count = (SELECT count(*) FROM comment_reactions r WHERE r.comment_id = p_comment AND r.reaction = 'LIKE'),
      reply_count = (SELECT count(*) FROM comments x WHERE x.parent_comment_id = p_comment AND x.status = 'VISIBLE' AND x.deleted_at IS NULL)
  WHERE c.id = p_comment
$$;

CREATE FUNCTION recount_tag_usage() RETURNS void LANGUAGE sql AS $$
  UPDATE tags t
  SET usage_count = (SELECT count(*) FROM video_tags vt WHERE vt.tag_id = t.id)
  WHERE t.usage_count IS DISTINCT FROM (SELECT count(*) FROM video_tags vt WHERE vt.tag_id = t.id)
$$;

-- ---------- retention (each call deletes at most p_limit rows; the scheduler loops until it returns 0) ----------
CREATE FUNCTION purge_expired_sessions(p_limit integer DEFAULT 10000) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE n integer;
BEGIN
  DELETE FROM sessions WHERE ctid IN (
    SELECT ctid FROM sessions
    WHERE expires_at < now() - interval '7 days' OR revoked_at < now() - interval '7 days'
    LIMIT p_limit);
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END $$;

CREATE FUNCTION purge_expired_verification_tokens(p_limit integer DEFAULT 10000) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE n integer;
BEGIN
  DELETE FROM verification_tokens WHERE ctid IN (
    SELECT ctid FROM verification_tokens
    WHERE expires_at < now() - interval '1 day' OR consumed_at < now() - interval '1 day'
    LIMIT p_limit);
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END $$;

CREATE FUNCTION purge_old_search_history(p_limit integer DEFAULT 10000, p_days integer DEFAULT 90) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE n integer;
BEGIN
  DELETE FROM search_history WHERE ctid IN (
    SELECT ctid FROM search_history WHERE created_at < now() - make_interval(days => p_days) LIMIT p_limit);
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END $$;

-- Read notifications after 90 days, unread after 365 days.
CREATE FUNCTION purge_old_notifications(p_limit integer DEFAULT 10000) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE n integer;
BEGIN
  DELETE FROM notifications WHERE id IN (
    SELECT id FROM notifications
    WHERE (read_at IS NOT NULL AND created_at < now() - interval '90 days')
       OR created_at < now() - interval '365 days'
    LIMIT p_limit);
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END $$;

-- Audit logs are append-only for the app; only this definer function (granted to the maintenance role) can age them out.
CREATE FUNCTION purge_old_audit_logs(p_limit integer DEFAULT 10000, p_days integer DEFAULT 730) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE n integer;
BEGIN
  DELETE FROM audit_logs WHERE id IN (
    SELECT id FROM audit_logs WHERE created_at < now() - make_interval(days => p_days) LIMIT p_limit);
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END $$;

-- Only the maintenance role may run the SECURITY DEFINER purge functions or partition management.
REVOKE ALL ON FUNCTION purge_expired_sessions(integer), purge_expired_verification_tokens(integer),
  purge_old_search_history(integer, integer), purge_old_notifications(integer), purge_old_audit_logs(integer, integer),
  ensure_monthly_partitions(regclass, integer), drop_old_partitions(regclass, integer) FROM PUBLIC;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_maint') THEN
    GRANT EXECUTE ON FUNCTION purge_expired_sessions(integer), purge_expired_verification_tokens(integer),
      purge_old_search_history(integer, integer), purge_old_notifications(integer), purge_old_audit_logs(integer, integer),
      ensure_monthly_partitions(regclass, integer), drop_old_partitions(regclass, integer) TO redxtube_maint;
  END IF;
END $$;
