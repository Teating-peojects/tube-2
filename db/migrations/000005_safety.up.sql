-- 000005 safety: reports, moderation actions, audit log.
-- Evidence-first: target FKs are RESTRICT, so reported content/users cannot be hard-deleted until the
-- reports are archived on purpose (see docs/OPERATIONS.md, "Deleted accounts").

CREATE TABLE reports (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  reporter_user_id uuid REFERENCES users (id) ON DELETE SET NULL,   -- NULL = anonymous reporter (rate-limit in Redis)
  target_type      report_target NOT NULL,
  target_video_id   uuid REFERENCES videos (id) ON DELETE RESTRICT,
  target_comment_id uuid REFERENCES comments (id) ON DELETE RESTRICT,
  target_user_id    uuid REFERENCES users (id) ON DELETE RESTRICT,  -- also covers "report a channel/creator"
  reason           report_reason NOT NULL,
  description      text,
  status           report_status NOT NULL DEFAULT 'PENDING',
  priority         smallint NOT NULL DEFAULT 1,                     -- 0 = urgent (set by trigger from reason)
  assigned_to      uuid REFERENCES users (id) ON DELETE SET NULL,
  resolved_by      uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  resolved_at      timestamptz,
  -- Exactly one target column is set, and target_type says which.
  CONSTRAINT reports_one_target CHECK (
    (target_video_id IS NOT NULL)::int + (target_comment_id IS NOT NULL)::int + (target_user_id IS NOT NULL)::int = 1
  ),
  CONSTRAINT reports_target_type_matches CHECK (
    (target_type = 'VIDEO') = (target_video_id IS NOT NULL)
    AND (target_type = 'COMMENT') = (target_comment_id IS NOT NULL)
    AND (target_type = 'USER') = (target_user_id IS NOT NULL)
  ),
  CONSTRAINT reports_description_len CHECK (description IS NULL OR char_length(description) <= 1000),
  CONSTRAINT reports_other_needs_description CHECK (reason <> 'OTHER' OR char_length(btrim(coalesce(description, ''))) > 0),
  CONSTRAINT reports_resolved_consistency CHECK ((status IN ('RESOLVED', 'REJECTED')) = (resolved_at IS NOT NULL)),
  CONSTRAINT reports_priority_range CHECK (priority BETWEEN 0 AND 2)
);
-- Moderator queue: urgent first, then oldest first.
CREATE INDEX reports_queue ON reports (priority, created_at) WHERE status IN ('PENDING', 'UNDER_REVIEW');
CREATE INDEX reports_by_video ON reports (target_video_id) WHERE target_video_id IS NOT NULL;
CREATE INDEX reports_by_comment ON reports (target_comment_id) WHERE target_comment_id IS NOT NULL;
CREATE INDEX reports_by_target_user ON reports (target_user_id) WHERE target_user_id IS NOT NULL;
CREATE INDEX reports_by_reporter ON reports (reporter_user_id, created_at DESC) WHERE reporter_user_id IS NOT NULL;
-- "Already reported" (HTTP 409): one OPEN report per reporter per target.
CREATE UNIQUE INDEX reports_open_video_once ON reports (reporter_user_id, target_video_id)
  WHERE target_video_id IS NOT NULL AND reporter_user_id IS NOT NULL AND status IN ('PENDING', 'UNDER_REVIEW');
CREATE UNIQUE INDEX reports_open_comment_once ON reports (reporter_user_id, target_comment_id)
  WHERE target_comment_id IS NOT NULL AND reporter_user_id IS NOT NULL AND status IN ('PENDING', 'UNDER_REVIEW');
CREATE UNIQUE INDEX reports_open_user_once ON reports (reporter_user_id, target_user_id)
  WHERE target_user_id IS NOT NULL AND reporter_user_id IS NOT NULL AND status IN ('PENDING', 'UNDER_REVIEW');

-- Append-only (UPDATE/DELETE are revoked from application roles in 000008).
CREATE TABLE moderation_actions (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  moderator_id      uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  report_id         uuid REFERENCES reports (id) ON DELETE SET NULL,
  target_type       report_target NOT NULL,
  target_video_id   uuid REFERENCES videos (id) ON DELETE RESTRICT,
  target_comment_id uuid REFERENCES comments (id) ON DELETE RESTRICT,
  target_user_id    uuid REFERENCES users (id) ON DELETE RESTRICT,
  action            moderation_action_type NOT NULL,
  reason            text NOT NULL,
  notes             text,
  expires_at        timestamptz,                                    -- temporary suspensions / restrictions
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT moderation_one_target CHECK (
    (target_video_id IS NOT NULL)::int + (target_comment_id IS NOT NULL)::int + (target_user_id IS NOT NULL)::int = 1
  ),
  CONSTRAINT moderation_target_type_matches CHECK (
    (target_type = 'VIDEO') = (target_video_id IS NOT NULL)
    AND (target_type = 'COMMENT') = (target_comment_id IS NOT NULL)
    AND (target_type = 'USER') = (target_user_id IS NOT NULL)
  ),
  CONSTRAINT moderation_action_target CHECK (
    (action NOT IN ('USER_SUSPENDED', 'USER_BANNED') OR target_type = 'USER')
    AND (action <> 'COMMENT_REMOVED' OR target_type = 'COMMENT')
    AND (action <> 'VIDEO_RESTRICTED' OR target_type = 'VIDEO')
    AND (action <> 'CONTENT_REMOVED' OR target_type IN ('VIDEO', 'COMMENT'))
  ),
  CONSTRAINT moderation_reason_len CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT moderation_notes_len CHECK (notes IS NULL OR char_length(notes) <= 5000)
);
CREATE INDEX moderation_by_video ON moderation_actions (target_video_id, created_at DESC) WHERE target_video_id IS NOT NULL;
CREATE INDEX moderation_by_comment ON moderation_actions (target_comment_id, created_at DESC) WHERE target_comment_id IS NOT NULL;
CREATE INDEX moderation_by_user ON moderation_actions (target_user_id, created_at DESC) WHERE target_user_id IS NOT NULL;
CREATE INDEX moderation_by_moderator ON moderation_actions (moderator_id, created_at DESC);
CREATE INDEX moderation_by_report ON moderation_actions (report_id) WHERE report_id IS NOT NULL;

-- Append-only. No FK on target_id on purpose: audit records must outlive the things they describe.
CREATE TABLE audit_logs (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  action        text NOT NULL,                                      -- dotted verb, e.g. 'user.role_changed'
  target_type   text,
  target_id     text,
  metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
  ip_hash       bytea,                                              -- HMAC of the IP, never the IP itself
  created_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT audit_logs_action_format CHECK (action ~ '^[a-z][a-z0-9_.]{2,63}$'),
  CONSTRAINT audit_logs_metadata CHECK (jsonb_typeof(metadata) = 'object' AND octet_length(metadata::text) <= 16384),
  -- Guard rail: top-level keys that must never carry secrets.
  CONSTRAINT audit_logs_no_secrets CHECK (NOT (metadata ?| ARRAY['password', 'password_hash', 'token', 'secret', 'authorization', 'api_key'])),
  CONSTRAINT audit_logs_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32)
);
CREATE INDEX audit_logs_by_actor ON audit_logs (actor_user_id, created_at DESC) WHERE actor_user_id IS NOT NULL;
CREATE INDEX audit_logs_by_target ON audit_logs (target_type, target_id, created_at DESC) WHERE target_id IS NOT NULL;
CREATE INDEX audit_logs_created_brin ON audit_logs USING brin (created_at);   -- append-only => perfectly correlated; tiny
