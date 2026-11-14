-- 000011 upload retry (Phase 5 / C): an upload session whose deadline passed (status EXPIRED) can be
-- reopened a limited number of times with a fresh multipart upload. The counter is the retry budget.
ALTER TABLE upload_sessions
  ADD COLUMN retry_count   smallint NOT NULL DEFAULT 0,
  ADD COLUMN last_retry_at timestamptz,
  ADD CONSTRAINT upload_sessions_retry_count CHECK (retry_count BETWEEN 0 AND 100);
