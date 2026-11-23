-- 000013 processing controls (Phase 6): cancellable jobs, traceable encoding profile, and a marker that
-- tells the worker's janitor which FAILED videos still have partial HLS output to remove.

-- A cancelled job is neither FAILED (an error) nor COMPLETED. Added outside any use in this file, so it
-- also works inside the migration transaction (PostgreSQL 12+).
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'CANCELLED';

ALTER TABLE videos
  ADD COLUMN processing_profile text,          -- encoding recipe version that produced the renditions, e.g. h264-aac-hls-v1
  ADD COLUMN outputs_cleaned_at timestamptz,   -- set when the janitor removed the partial output of a FAILED video
  ADD CONSTRAINT videos_processing_profile_len CHECK (processing_profile IS NULL OR char_length(processing_profile) BETWEEN 1 AND 64);

-- The janitor scans only failed videos that still need cleaning.
CREATE INDEX videos_failed_cleanup ON videos (updated_at)
  WHERE status = 'FAILED' AND outputs_cleaned_at IS NULL AND deleted_at IS NULL;
