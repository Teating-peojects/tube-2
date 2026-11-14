ALTER TABLE upload_sessions
  DROP CONSTRAINT IF EXISTS upload_sessions_retry_count,
  DROP COLUMN IF EXISTS last_retry_at,
  DROP COLUMN IF EXISTS retry_count;
