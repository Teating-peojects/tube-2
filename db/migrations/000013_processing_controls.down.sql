DROP INDEX IF EXISTS videos_failed_cleanup;
ALTER TABLE videos
  DROP CONSTRAINT IF EXISTS videos_processing_profile_len,
  DROP COLUMN IF EXISTS outputs_cleaned_at,
  DROP COLUMN IF EXISTS processing_profile;
-- PostgreSQL cannot remove an enum value. 'CANCELLED' stays in job_status; rows that use it are folded
-- into FAILED so older code never meets a value it does not know.
UPDATE video_processing_jobs SET status = 'FAILED' WHERE status::text = 'CANCELLED';
