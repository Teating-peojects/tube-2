-- 000010 upload consent, content-hash blocklist, suspension expiry (B5 upload + B9 moderation).

-- ---------- 18+ / consent / rights declaration ----------
-- One row per published video: the uploader declared that everyone appearing is 18+, consented and that
-- they hold the rights. Append-only for the API (see the REVOKE below); evidence-first like reports.
CREATE TABLE upload_consents (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  video_id          uuid NOT NULL REFERENCES videos (id) ON DELETE RESTRICT,
  user_id           uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  statement_version smallint NOT NULL,                       -- which wording of the declaration the uploader saw
  ip_hash           bytea,                                   -- HMAC of the IP, never the IP itself
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT upload_consents_video_key UNIQUE (video_id),
  CONSTRAINT upload_consents_version CHECK (statement_version > 0),
  CONSTRAINT upload_consents_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32)
);
CREATE INDEX upload_consents_by_user ON upload_consents (user_id, created_at DESC);

-- ---------- content-hash blocklist ----------
-- SHA-256 of ORIGINAL files that must never be published again (confirmed illegal content, takedowns).
-- The media worker stores videos.content_sha256 after hashing the upload; the trigger below removes a
-- video whose hash is on the list, so the rule is enforced by the database no matter who writes the hash.
CREATE TABLE blocked_hashes (
  sha256     bytea PRIMARY KEY,
  reason     text NOT NULL,
  added_by   uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT blocked_hashes_len CHECK (octet_length(sha256) = 32),
  CONSTRAINT blocked_hashes_reason_len CHECK (char_length(btrim(reason)) BETWEEN 1 AND 200)
);

ALTER TABLE videos
  ADD COLUMN content_sha256 bytea,
  ADD CONSTRAINT videos_content_sha256_len CHECK (content_sha256 IS NULL OR octet_length(content_sha256) = 32);
CREATE INDEX videos_by_content_hash ON videos (content_sha256) WHERE content_sha256 IS NOT NULL;

CREATE FUNCTION trg_videos_hash_blocklist() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.content_sha256 IS NOT NULL AND EXISTS (SELECT 1 FROM blocked_hashes h WHERE h.sha256 = NEW.content_sha256) THEN
    NEW.moderation_status := 'REMOVED';
    INSERT INTO audit_logs (actor_user_id, action, target_type, target_id)
    VALUES (NULL, 'moderation.hash_match', 'video', NEW.id::text);
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER videos_hash_blocklist BEFORE INSERT OR UPDATE OF content_sha256 ON videos
  FOR EACH ROW EXECUTE FUNCTION trg_videos_hash_blocklist();

-- ---------- temporary suspensions ----------
-- A suspension with expires_at is lifted by this function (the API runs it every minute). Only the LATEST
-- suspend/ban action of an account counts: a later ban, or a later open-ended suspension, is never undone.
CREATE FUNCTION expire_user_suspensions(p_limit integer DEFAULT 1000) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE
  r record;
  n integer := 0;
BEGIN
  FOR r IN
    SELECT u.id FROM users u
    WHERE u.status = 'SUSPENDED' AND u.deleted_at IS NULL
      AND COALESCE((
        SELECT a.action = 'USER_SUSPENDED' AND a.expires_at IS NOT NULL AND a.expires_at <= now()
        FROM moderation_actions a
        WHERE a.target_user_id = u.id AND a.action IN ('USER_SUSPENDED', 'USER_BANNED')
        ORDER BY a.created_at DESC, a.id DESC LIMIT 1
      ), false)
    LIMIT p_limit
    FOR UPDATE OF u SKIP LOCKED
  LOOP
    UPDATE users SET status = 'ACTIVE' WHERE id = r.id;
    INSERT INTO audit_logs (actor_user_id, action, target_type, target_id)
    VALUES (NULL, 'moderation.suspension_expired', 'user', r.id::text);
    n := n + 1;
  END LOOP;
  RETURN n;
END $$;
REVOKE ALL ON FUNCTION expire_user_suspensions(integer) FROM PUBLIC;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_app') THEN
    REVOKE UPDATE, DELETE, TRUNCATE ON upload_consents FROM redxtube_app;
    GRANT EXECUTE ON FUNCTION expire_user_suspensions(integer) TO redxtube_app;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_worker') THEN
    REVOKE UPDATE, DELETE, TRUNCATE ON upload_consents FROM redxtube_worker;
    REVOKE ALL ON upload_consents, blocked_hashes FROM redxtube_worker;
    GRANT SELECT ON blocked_hashes TO redxtube_worker;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_maint') THEN
    GRANT EXECUTE ON FUNCTION expire_user_suspensions(integer) TO redxtube_maint;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_readonly') THEN
    REVOKE ALL ON blocked_hashes FROM redxtube_readonly;   -- the list itself is sensitive
  END IF;
END $$;
