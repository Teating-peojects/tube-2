DROP FUNCTION IF EXISTS set_updated_at();
DROP TYPE IF EXISTS breakdown_dimension, traffic_source, device_type;
DROP TYPE IF EXISTS moderation_action_type, report_status, report_reason, report_target;
DROP TYPE IF EXISTS notification_type, playlist_visibility, comment_status, reaction_type;
DROP TYPE IF EXISTS upload_status, job_status, job_type, asset_status, asset_source, thumbnail_type;
DROP TYPE IF EXISTS variant_status, video_resolution, comment_mode, moderation_status, video_status, video_visibility, video_type;
DROP TYPE IF EXISTS age_verification_method, age_verification_status, verification_purpose, theme_pref, user_status, user_role;
-- Extensions (citext, pg_trgm) are shared and intentionally left installed.
