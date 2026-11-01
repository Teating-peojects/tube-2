-- 000007 triggers: only where they protect an invariant that application code could forget.
--  * 1:1 child rows always exist          * cached counters change in the SAME transaction as their source row
--  * immutable ownership                  * comment threads stay one level deep
--  * updated_at is always right
-- Counter updates use GREATEST(x - 1, 0) so a drifted counter can never abort a user action;
-- the nightly reconcile job (000009) repairs drift.

-- ---------- 1:1 children ----------
CREATE FUNCTION trg_users_create_children() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO user_profiles (user_id) VALUES (NEW.id);
  INSERT INTO user_settings (user_id) VALUES (NEW.id);
  RETURN NULL;
END $$;
CREATE TRIGGER users_create_children AFTER INSERT ON users
  FOR EACH ROW EXECUTE FUNCTION trg_users_create_children();

CREATE FUNCTION trg_videos_create_stats() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO video_stats (video_id) VALUES (NEW.id);
  RETURN NULL;
END $$;
CREATE TRIGGER videos_create_stats AFTER INSERT ON videos
  FOR EACH ROW EXECUTE FUNCTION trg_videos_create_stats();

-- ---------- ownership never changes (comments.video_creator_id depends on it) ----------
CREATE FUNCTION trg_videos_immutable_creator() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.creator_id <> OLD.creator_id THEN
    RAISE EXCEPTION 'videos.creator_id is immutable' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER videos_immutable_creator BEFORE UPDATE OF creator_id ON videos
  FOR EACH ROW EXECUTE FUNCTION trg_videos_immutable_creator();

-- ---------- user_profiles.video_count = number of PUBLIC published videos ----------
CREATE FUNCTION video_counts_as_public(s video_status, v video_visibility, m moderation_status, d timestamptz)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT s = 'PUBLISHED' AND v = 'PUBLIC' AND m = 'OK' AND d IS NULL
$$;

CREATE FUNCTION trg_videos_profile_video_count() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  was_public boolean := false;
  is_public  boolean := false;
  owner_id   uuid;
BEGIN
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    was_public := video_counts_as_public(OLD.status, OLD.visibility, OLD.moderation_status, OLD.deleted_at);
    owner_id := OLD.creator_id;
  END IF;
  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    is_public := video_counts_as_public(NEW.status, NEW.visibility, NEW.moderation_status, NEW.deleted_at);
    owner_id := NEW.creator_id;
  END IF;
  IF is_public AND NOT was_public THEN
    UPDATE user_profiles SET video_count = video_count + 1 WHERE user_id = owner_id;
  ELSIF was_public AND NOT is_public THEN
    UPDATE user_profiles SET video_count = GREATEST(video_count - 1, 0) WHERE user_id = owner_id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER videos_profile_video_count
  AFTER INSERT OR DELETE OR UPDATE OF status, visibility, moderation_status, deleted_at ON videos
  FOR EACH ROW EXECUTE FUNCTION trg_videos_profile_video_count();

-- ---------- subscriber_count ----------
CREATE FUNCTION trg_subscriptions_count() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    UPDATE user_profiles SET subscriber_count = subscriber_count + 1 WHERE user_id = NEW.creator_id;
  ELSE
    UPDATE user_profiles SET subscriber_count = GREATEST(subscriber_count - 1, 0) WHERE user_id = OLD.creator_id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER subscriptions_count AFTER INSERT OR DELETE ON subscriptions
  FOR EACH ROW EXECUTE FUNCTION trg_subscriptions_count();

-- ---------- video like / dislike counters ----------
CREATE FUNCTION bump_video_reaction(p_video uuid, p_reaction reaction_type, p_delta integer)
RETURNS void LANGUAGE sql AS $$
  UPDATE video_stats
  SET like_count    = GREATEST(like_count    + CASE WHEN p_reaction = 'LIKE'    THEN p_delta ELSE 0 END, 0),
      dislike_count = GREATEST(dislike_count + CASE WHEN p_reaction = 'DISLIKE' THEN p_delta ELSE 0 END, 0),
      updated_at    = now()
  WHERE video_id = p_video
$$;

CREATE FUNCTION trg_video_reactions_count() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM bump_video_reaction(NEW.video_id, NEW.reaction, 1);
  ELSIF TG_OP = 'DELETE' THEN
    PERFORM bump_video_reaction(OLD.video_id, OLD.reaction, -1);
  ELSIF NEW.reaction <> OLD.reaction THEN
    PERFORM bump_video_reaction(OLD.video_id, OLD.reaction, -1);
    PERFORM bump_video_reaction(NEW.video_id, NEW.reaction, 1);
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER video_reactions_count AFTER INSERT OR DELETE OR UPDATE OF reaction ON video_reactions
  FOR EACH ROW EXECUTE FUNCTION trg_video_reactions_count();

-- ---------- comments: visible-count on the video and reply_count on the parent ----------
CREATE FUNCTION comment_is_counted(s comment_status, d timestamptz)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT s = 'VISIBLE' AND d IS NULL
$$;

CREATE FUNCTION trg_comments_counts() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  was_counted boolean := false;
  now_counted boolean := false;
  delta       integer;
  vid         uuid;
  parent      uuid;
BEGIN
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    was_counted := comment_is_counted(OLD.status, OLD.deleted_at);
  END IF;
  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    now_counted := comment_is_counted(NEW.status, NEW.deleted_at);
  END IF;
  delta := now_counted::int - was_counted::int;
  IF delta = 0 THEN
    RETURN NULL;
  END IF;
  IF TG_OP = 'DELETE' THEN
    vid := OLD.video_id;  parent := OLD.parent_comment_id;
  ELSE
    vid := NEW.video_id;  parent := NEW.parent_comment_id;
  END IF;
  UPDATE video_stats SET comment_count = GREATEST(comment_count + delta, 0), updated_at = now() WHERE video_id = vid;
  IF parent IS NOT NULL THEN
    UPDATE comments SET reply_count = GREATEST(reply_count + delta, 0) WHERE id = parent;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER comments_counts AFTER INSERT OR DELETE OR UPDATE OF status, deleted_at ON comments
  FOR EACH ROW EXECUTE FUNCTION trg_comments_counts();

-- Threads are exactly one level deep: replies target top-level comments only.
CREATE FUNCTION trg_comments_depth() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.parent_comment_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM comments p WHERE p.id = NEW.parent_comment_id AND p.parent_comment_id IS NOT NULL
  ) THEN
    RAISE EXCEPTION 'replies can only target top-level comments' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER comments_depth BEFORE INSERT ON comments
  FOR EACH ROW EXECUTE FUNCTION trg_comments_depth();

-- ---------- comment likes ----------
CREATE FUNCTION trg_comment_reactions_count() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.reaction = 'LIKE' THEN
      UPDATE comments SET like_count = like_count + 1 WHERE id = NEW.comment_id;
    END IF;
  ELSIF TG_OP = 'DELETE' THEN
    IF OLD.reaction = 'LIKE' THEN
      UPDATE comments SET like_count = GREATEST(like_count - 1, 0) WHERE id = OLD.comment_id;
    END IF;
  ELSIF NEW.reaction <> OLD.reaction THEN
    IF NEW.reaction = 'LIKE' THEN
      UPDATE comments SET like_count = like_count + 1 WHERE id = NEW.comment_id;
    ELSE
      UPDATE comments SET like_count = GREATEST(like_count - 1, 0) WHERE id = NEW.comment_id;
    END IF;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER comment_reactions_count AFTER INSERT OR DELETE OR UPDATE OF reaction ON comment_reactions
  FOR EACH ROW EXECUTE FUNCTION trg_comment_reactions_count();

-- ---------- tag usage ----------
CREATE FUNCTION trg_video_tags_count() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    UPDATE tags SET usage_count = usage_count + 1 WHERE id = NEW.tag_id;
  ELSE
    UPDATE tags SET usage_count = GREATEST(usage_count - 1, 0) WHERE id = OLD.tag_id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER video_tags_count AFTER INSERT OR DELETE ON video_tags
  FOR EACH ROW EXECUTE FUNCTION trg_video_tags_count();

-- ---------- report priority (urgent categories jump the moderator queue) ----------
CREATE FUNCTION trg_reports_priority() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.priority := CASE
    WHEN NEW.reason IN ('UNDERAGE_CONTENT', 'NON_CONSENSUAL_CONTENT', 'ILLEGAL_CONTENT') THEN 0
    ELSE 1
  END;
  RETURN NEW;
END $$;
CREATE TRIGGER reports_priority BEFORE INSERT ON reports
  FOR EACH ROW EXECUTE FUNCTION trg_reports_priority();

-- ---------- updated_at ----------
-- (user_profiles and comments are excluded: their rows are also touched by counter triggers, and
--  updated_at there should mean "the owner edited it", which the application sets.)
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'users', 'user_settings', 'age_verifications', 'categories', 'videos', 'video_metadata',
    'video_variants', 'upload_sessions', 'video_processing_jobs', 'video_reactions', 'playlists', 'reports'
  ] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()', t || '_set_updated_at', t);
  END LOOP;
END $$;

-- ---------- storage tuning for rows that are updated constantly ----------
-- Counter updates touch no indexed column, so leaving free space in each page lets Postgres do HOT updates
-- (no index maintenance) and aggressive autovacuum keeps dead tuples from piling up.
ALTER TABLE video_stats   SET (fillfactor = 80, autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.05);
ALTER TABLE user_profiles SET (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.05);
