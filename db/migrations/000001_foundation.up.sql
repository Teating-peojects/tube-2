-- 000001 foundation: extensions, enum types, shared helper functions.
-- All timestamps are timestamptz (stored as UTC). Run the DB with timezone = 'UTC'.

CREATE EXTENSION IF NOT EXISTS citext;    -- case-insensitive username / email
CREATE EXTENSION IF NOT EXISTS pg_trgm;   -- autocomplete + fuzzy channel/playlist search

-- ---- identity ----
CREATE TYPE user_role AS ENUM ('USER', 'CREATOR', 'MODERATOR', 'ADMIN', 'SUPER_ADMIN');
CREATE TYPE user_status AS ENUM ('ACTIVE', 'SUSPENDED', 'BANNED', 'DELETED', 'PENDING');
CREATE TYPE theme_pref AS ENUM ('DARK', 'LIGHT', 'SYSTEM');
CREATE TYPE verification_purpose AS ENUM ('EMAIL_VERIFY', 'PHONE_VERIFY', 'PASSWORD_RESET');
CREATE TYPE age_verification_status AS ENUM ('PENDING', 'VERIFIED', 'FAILED', 'EXPIRED', 'REJECTED');
CREATE TYPE age_verification_method AS ENUM ('SELF_DECLARATION', 'THIRD_PARTY', 'DOCUMENT', 'OTHER');

-- ---- video catalog ----
CREATE TYPE video_type AS ENUM ('SHORT', 'LONG');
CREATE TYPE video_visibility AS ENUM ('PUBLIC', 'UNLISTED', 'PRIVATE');
-- Lifecycle of the upload/processing pipeline. Moderation is a separate axis (moderation_status).
CREATE TYPE video_status AS ENUM ('UPLOADING', 'PROCESSING', 'READY', 'PUBLISHED', 'FAILED');
CREATE TYPE moderation_status AS ENUM ('OK', 'RESTRICTED', 'REMOVED');
CREATE TYPE comment_mode AS ENUM ('ON', 'REVIEW', 'OFF');
CREATE TYPE video_resolution AS ENUM ('360p', '480p', '720p', '1080p');
CREATE TYPE variant_status AS ENUM ('PENDING', 'PROCESSING', 'READY', 'FAILED');
CREATE TYPE thumbnail_type AS ENUM ('POSTER', 'THUMBNAIL', 'PREVIEW');
CREATE TYPE asset_source AS ENUM ('GENERATED', 'CUSTOM');
CREATE TYPE asset_status AS ENUM ('PENDING', 'READY');
CREATE TYPE job_type AS ENUM ('METADATA', 'TRANSCODE', 'HLS', 'THUMBNAIL');
CREATE TYPE job_status AS ENUM ('QUEUED', 'PROCESSING', 'COMPLETED', 'FAILED');
CREATE TYPE upload_status AS ENUM ('INITIATED', 'COMPLETING', 'COMPLETED', 'ABORTED', 'EXPIRED');

-- ---- engagement ----
CREATE TYPE reaction_type AS ENUM ('LIKE', 'DISLIKE');
CREATE TYPE comment_status AS ENUM ('VISIBLE', 'HIDDEN', 'DELETED', 'UNDER_REVIEW');
CREATE TYPE playlist_visibility AS ENUM ('PUBLIC', 'UNLISTED', 'PRIVATE');
CREATE TYPE notification_type AS ENUM (
  'NEW_SUBSCRIBER', 'NEW_VIDEO', 'COMMENT', 'COMMENT_REPLY', 'VIDEO_LIKE', 'COMMENT_LIKE',
  'VIDEO_READY', 'VIDEO_FAILED', 'MODERATION', 'SECURITY', 'SYSTEM'
);

-- ---- safety ----
CREATE TYPE report_target AS ENUM ('VIDEO', 'COMMENT', 'USER');
CREATE TYPE report_reason AS ENUM (
  'UNDERAGE_CONTENT', 'NON_CONSENSUAL_CONTENT', 'COPYRIGHT', 'HARASSMENT', 'SPAM', 'ILLEGAL_CONTENT', 'OTHER'
);
CREATE TYPE report_status AS ENUM ('PENDING', 'UNDER_REVIEW', 'RESOLVED', 'REJECTED');
CREATE TYPE moderation_action_type AS ENUM (
  'WARNING', 'CONTENT_REMOVED', 'VIDEO_RESTRICTED', 'USER_SUSPENDED', 'USER_BANNED', 'COMMENT_REMOVED'
);

-- ---- analytics ----
CREATE TYPE device_type AS ENUM ('MOBILE', 'DESKTOP', 'TABLET', 'TV', 'OTHER');
CREATE TYPE traffic_source AS ENUM ('HOME', 'SEARCH', 'SUBSCRIPTIONS', 'EXTERNAL', 'DIRECT', 'OTHER');
CREATE TYPE breakdown_dimension AS ENUM ('COUNTRY', 'DEVICE', 'TRAFFIC_SOURCE');

-- ---- helpers ----
CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END $$;
