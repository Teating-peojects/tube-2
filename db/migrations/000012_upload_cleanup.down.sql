DROP INDEX IF EXISTS upload_sessions_expired_by_deadline;
DROP INDEX IF EXISTS upload_sessions_pending_cleanup;
ALTER TABLE upload_sessions
  DROP CONSTRAINT IF EXISTS upload_sessions_cleanup_attempts,
  DROP COLUMN IF EXISTS cleanup_next_at,
  DROP COLUMN IF EXISTS cleanup_attempts,
  DROP COLUMN IF EXISTS storage_cleaned_at;
