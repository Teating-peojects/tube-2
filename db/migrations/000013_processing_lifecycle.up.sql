-- B6.2: job lifecycle columns for the processing worker (see processing/docs/B6_DESIGN_ADR.md).
-- Cancellation is a marker, not a new enum value: PostgreSQL cannot drop enum values, which would break up/down/up.
ALTER TABLE video_processing_jobs
  ADD COLUMN processing_version smallint    NOT NULL DEFAULT 1,
  ADD COLUMN stage               text,
  ADD COLUMN progress            smallint    NOT NULL DEFAULT 0,
  ADD COLUMN error_code          text,
  ADD COLUMN cancel_requested_at timestamptz,
  ADD CONSTRAINT video_jobs_version  CHECK (processing_version > 0),
  ADD CONSTRAINT video_jobs_stage    CHECK (stage IS NULL OR stage ~ '^[a-z][a-z_]{0,39}$'),
  ADD CONSTRAINT video_jobs_progress CHECK (progress BETWEEN 0 AND 100),
  ADD CONSTRAINT video_jobs_error_code CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z_]{0,39}$');
