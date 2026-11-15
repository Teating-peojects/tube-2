-- 000012 upload storage cleanup (Phase 5 / D): bookkeeping for the janitor that frees the object storage of
-- closed upload sessions (EXPIRED / ABORTED). storage_cleaned_at marks a session whose multipart upload and
-- objects were verified gone; failures are retried with a growing delay (cleanup_attempts, cleanup_next_at).
ALTER TABLE upload_sessions
  ADD COLUMN storage_cleaned_at timestamptz,
  ADD COLUMN cleanup_attempts   smallint NOT NULL DEFAULT 0,
  ADD COLUMN cleanup_next_at    timestamptz,
  ADD CONSTRAINT upload_sessions_cleanup_attempts CHECK (cleanup_attempts >= 0);

-- sessions still waiting for their storage to be freed
CREATE INDEX upload_sessions_pending_cleanup ON upload_sessions (updated_at)
  WHERE status IN ('EXPIRED', 'ABORTED') AND storage_cleaned_at IS NULL;
-- expired sessions whose retry window may have ended
CREATE INDEX upload_sessions_expired_by_deadline ON upload_sessions (expires_at) WHERE status = 'EXPIRED';
