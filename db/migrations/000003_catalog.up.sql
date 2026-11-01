-- 000003 catalog: categories, tags, videos and everything the processing pipeline produces.
-- Binary data lives in object storage; this schema holds metadata and storage KEYS only
-- (URLs are built from CDN_BASE_URL + key, so a CDN change never needs a migration).

CREATE TABLE categories (
  id                   smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name                 citext NOT NULL,
  slug                 text NOT NULL,
  description          text,
  thumbnail_storage_key text,
  is_active            boolean NOT NULL DEFAULT true,
  sort_order           integer NOT NULL DEFAULT 0,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT categories_name_key UNIQUE (name),
  CONSTRAINT categories_slug_key UNIQUE (slug),
  CONSTRAINT categories_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  CONSTRAINT categories_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 50)
);
CREATE INDEX categories_active_order ON categories (sort_order, name) WHERE is_active;

CREATE TABLE tags (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name        text NOT NULL,                                  -- display form
  slug        text NOT NULL,                                  -- normalised, lower-case, unique
  usage_count integer NOT NULL DEFAULT 0,                     -- cached, maintained by trigger
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT tags_slug_key UNIQUE (slug),
  CONSTRAINT tags_slug_format CHECK (slug = lower(slug) AND char_length(slug) BETWEEN 1 AND 30 AND slug !~ '[[:space:]]'),
  CONSTRAINT tags_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 30),
  CONSTRAINT tags_usage_count CHECK (usage_count >= 0)
);
CREATE INDEX tags_slug_prefix ON tags (slug text_pattern_ops);   -- tag autocomplete

CREATE TABLE videos (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  creator_id          uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  title               text NOT NULL,
  slug                text,                                   -- cosmetic SEO text; lookups use id
  description         text NOT NULL DEFAULT '',
  video_type          video_type,                             -- NULL until the METADATA job measured the duration
  visibility          video_visibility NOT NULL DEFAULT 'PRIVATE',
  status              video_status NOT NULL DEFAULT 'UPLOADING',
  moderation_status   moderation_status NOT NULL DEFAULT 'OK',
  duration_seconds    integer,
  file_size_bytes     bigint,
  original_filename   text,
  original_storage_key text,
  hls_master_key      text,                                   -- master .m3u8 key, set when the HLS job finishes
  category_id         smallint REFERENCES categories (id),
  language            text,
  age_restricted      boolean NOT NULL DEFAULT true,
  comments_mode       comment_mode NOT NULL DEFAULT 'ON',
  published_at        timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  deleted_at          timestamptz,
  -- Target of the composite FK from comments (keeps comments.video_creator_id consistent by construction).
  CONSTRAINT videos_id_creator_key UNIQUE (id, creator_id),
  CONSTRAINT videos_title_len CHECK (char_length(btrim(title)) BETWEEN 1 AND 100),
  CONSTRAINT videos_description_len CHECK (char_length(description) <= 5000),
  CONSTRAINT videos_slug_format CHECK (slug IS NULL OR slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  CONSTRAINT videos_language_format CHECK (language IS NULL OR language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
  CONSTRAINT videos_duration_range CHECK (duration_seconds IS NULL OR (duration_seconds > 0 AND duration_seconds <= 86400)),
  -- SHORT <= 60 s, LONG > 60 s. Also enforce these in the API (do not rely on the DB error alone).
  CONSTRAINT videos_type_matches_duration CHECK (
    video_type IS NULL OR duration_seconds IS NULL
    OR (video_type = 'SHORT' AND duration_seconds <= 60)
    OR (video_type = 'LONG' AND duration_seconds > 60)
  ),
  -- Upload limits: LONG <= 5 GiB, SHORT <= 200 MiB.
  CONSTRAINT videos_size_limits CHECK (
    file_size_bytes IS NULL
    OR (file_size_bytes > 0 AND file_size_bytes <= 5368709120
        AND (video_type IS DISTINCT FROM 'SHORT' OR file_size_bytes <= 209715200))
  ),
  CONSTRAINT videos_ready_requirements CHECK (
    status NOT IN ('READY', 'PUBLISHED')
    OR (video_type IS NOT NULL AND duration_seconds IS NOT NULL AND hls_master_key IS NOT NULL)
  ),
  CONSTRAINT videos_published_requirements CHECK (
    status <> 'PUBLISHED' OR (published_at IS NOT NULL AND category_id IS NOT NULL)
  ),
  CONSTRAINT videos_published_at_only_when_published CHECK (published_at IS NULL OR status = 'PUBLISHED')
);

-- Public listing predicate used by every discovery query:
--   status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL
-- Partial indexes keep these hot indexes small (private/draft/removed rows are excluded) and
-- support keyset pagination on (published_at DESC, id DESC). Standalone indexes on low-cardinality
-- columns (status, visibility) are intentionally NOT created: the planner would not use them.
CREATE INDEX videos_public_recent ON videos (published_at DESC, id DESC)
  WHERE status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL;
CREATE INDEX videos_public_category ON videos (category_id, published_at DESC, id DESC)
  WHERE status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL;
CREATE INDEX videos_public_creator ON videos (creator_id, published_at DESC, id DESC)
  WHERE status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL;
CREATE INDEX videos_public_shorts ON videos (published_at DESC, id DESC)
  WHERE status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL AND video_type = 'SHORT';
CREATE INDEX videos_public_title_trgm ON videos USING gin (title gin_trgm_ops)
  WHERE status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL;
-- Creator Studio: all of my videos in any state (also serves the FK on creator_id).
CREATE INDEX videos_by_creator ON videos (creator_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
-- Janitor: stuck uploads / processing.
CREATE INDEX videos_in_flight ON videos (created_at) WHERE status IN ('UPLOADING', 'PROCESSING');
-- Moderation: restricted / removed content.
CREATE INDEX videos_moderated ON videos (moderation_status, updated_at DESC) WHERE moderation_status <> 'OK';

-- Plain-SQL view for readability. Queries through it still use the partial indexes above.
CREATE VIEW public_videos AS
  SELECT * FROM videos
  WHERE status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL;

-- Hot counters live in their own narrow row so a like/view never rewrites the wide videos row.
CREATE TABLE video_stats (
  video_id           uuid PRIMARY KEY REFERENCES videos (id) ON DELETE CASCADE,
  view_count         bigint NOT NULL DEFAULT 0,               -- flushed in batches from Redis
  like_count         integer NOT NULL DEFAULT 0,              -- trigger-maintained
  dislike_count      integer NOT NULL DEFAULT 0,              -- trigger-maintained
  comment_count      integer NOT NULL DEFAULT 0,              -- trigger-maintained (visible comments + replies)
  watch_time_seconds bigint NOT NULL DEFAULT 0,               -- flushed in batches
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT video_stats_non_negative CHECK (
    view_count >= 0 AND like_count >= 0 AND dislike_count >= 0 AND comment_count >= 0 AND watch_time_seconds >= 0
  )
);

CREATE TABLE video_tags (
  video_id uuid   NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  tag_id   bigint NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
  PRIMARY KEY (video_id, tag_id)
);
CREATE INDEX video_tags_by_tag ON video_tags (tag_id, video_id);

CREATE TABLE video_metadata (
  video_id     uuid PRIMARY KEY REFERENCES videos (id) ON DELETE CASCADE,
  width        integer NOT NULL,
  height       integer NOT NULL,
  fps          numeric(6, 3),
  video_codec  text,
  audio_codec  text,
  bitrate      bigint,
  container    text,
  aspect_ratio numeric(10, 5) GENERATED ALWAYS AS (width::numeric / NULLIF(height, 0)) STORED,
  has_audio    boolean NOT NULL DEFAULT true,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT video_metadata_dimensions CHECK (width > 0 AND height > 0),
  -- "Max 1080p" means the short side is <= 1080 and the long side <= 1920 (1920x1080 or 1080x1920).
  CONSTRAINT video_metadata_max_1080p CHECK (LEAST(width, height) <= 1080 AND GREATEST(width, height) <= 1920),
  CONSTRAINT video_metadata_fps CHECK (fps IS NULL OR (fps > 0 AND fps <= 240)),
  CONSTRAINT video_metadata_bitrate CHECK (bitrate IS NULL OR bitrate > 0)
);

CREATE TABLE video_variants (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  video_id         uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  resolution       video_resolution NOT NULL,
  width            integer,
  height           integer,
  bitrate          integer,
  codec            text,
  storage_key      text NOT NULL,                             -- prefix/directory of the HLS rendition
  manifest_path    text,                                      -- rendition playlist, relative to storage_key
  file_size_bytes  bigint,
  status           variant_status NOT NULL DEFAULT 'PENDING',
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT video_variants_video_resolution_key UNIQUE (video_id, resolution),
  CONSTRAINT video_variants_ready CHECK (
    status <> 'READY' OR (manifest_path IS NOT NULL AND width IS NOT NULL AND height IS NOT NULL AND bitrate IS NOT NULL)
  ),
  CONSTRAINT video_variants_dimensions CHECK ((width IS NULL OR width > 0) AND (height IS NULL OR height > 0)),
  CONSTRAINT video_variants_sizes CHECK ((bitrate IS NULL OR bitrate > 0) AND (file_size_bytes IS NULL OR file_size_bytes >= 0))
);

CREATE TABLE video_thumbnails (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  video_id    uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  storage_key text NOT NULL,
  width       integer,
  height      integer,
  type        thumbnail_type NOT NULL DEFAULT 'THUMBNAIL',
  source      asset_source NOT NULL DEFAULT 'GENERATED',
  status      asset_status NOT NULL DEFAULT 'READY',          -- CUSTOM uploads start as PENDING
  is_primary  boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT video_thumbnails_dimensions CHECK ((width IS NULL OR width > 0) AND (height IS NULL OR height > 0)),
  CONSTRAINT video_thumbnails_primary_ready CHECK (NOT is_primary OR status = 'READY')
);
CREATE UNIQUE INDEX video_thumbnails_one_primary ON video_thumbnails (video_id) WHERE is_primary;  -- at most one primary
CREATE INDEX video_thumbnails_by_video ON video_thumbnails (video_id, type);

-- Subtitles. The worker converts uploaded .srt to .vtt, so storage_key always points to a WebVTT file.
CREATE TABLE video_captions (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  video_id    uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  language    text NOT NULL,
  label       text,
  storage_key text NOT NULL,
  status      asset_status NOT NULL DEFAULT 'PENDING',
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT video_captions_video_language_key UNIQUE (video_id, language),
  CONSTRAINT video_captions_language CHECK (language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$'),
  CONSTRAINT video_captions_label_len CHECK (label IS NULL OR char_length(label) <= 50)
);

-- Multipart upload bookkeeping. Presigned URLs are NOT stored (they are derived on demand);
-- part ETags are sent by the client at completion and verified against object storage.
CREATE TABLE upload_sessions (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id             uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  video_id            uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  storage_key         text NOT NULL,
  provider_upload_id  text NOT NULL,                          -- e.g. S3 multipart UploadId
  declared_size_bytes bigint NOT NULL,
  declared_content_type text NOT NULL,
  part_size_bytes     integer NOT NULL,
  part_count          integer NOT NULL,
  status              upload_status NOT NULL DEFAULT 'INITIATED',
  expires_at          timestamptz NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  completed_at        timestamptz,
  CONSTRAINT upload_sessions_video_key UNIQUE (video_id),
  CONSTRAINT upload_sessions_size CHECK (declared_size_bytes > 0 AND declared_size_bytes <= 5368709120),
  CONSTRAINT upload_sessions_part_size CHECK (part_size_bytes >= 5242880),     -- S3 minimum part size (5 MiB)
  CONSTRAINT upload_sessions_part_count CHECK (part_count BETWEEN 1 AND 10000), -- S3 maximum part count
  CONSTRAINT upload_sessions_expiry CHECK (expires_at > created_at),
  CONSTRAINT upload_sessions_completed CHECK ((status = 'COMPLETED') = (completed_at IS NOT NULL))
);
CREATE INDEX upload_sessions_open_by_user ON upload_sessions (user_id) WHERE status IN ('INITIATED', 'COMPLETING');
CREATE INDEX upload_sessions_expiring ON upload_sessions (expires_at) WHERE status IN ('INITIATED', 'COMPLETING');

CREATE TABLE video_processing_jobs (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  video_id      uuid NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
  job_type      job_type NOT NULL,
  status        job_status NOT NULL DEFAULT 'QUEUED',
  attempts      smallint NOT NULL DEFAULT 0,
  max_attempts  smallint NOT NULL DEFAULT 3,
  error_message text,
  worker_id     text,
  heartbeat_at  timestamptz,                                  -- workers refresh this; stale rows are re-queued
  scheduled_at  timestamptz NOT NULL DEFAULT now(),
  started_at    timestamptz,
  completed_at  timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT video_jobs_attempts CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts),
  CONSTRAINT video_jobs_error_len CHECK (error_message IS NULL OR char_length(error_message) <= 2000),
  CONSTRAINT video_jobs_processing_started CHECK (status <> 'PROCESSING' OR started_at IS NOT NULL),
  CONSTRAINT video_jobs_completed CHECK (status <> 'COMPLETED' OR completed_at IS NOT NULL)
);
-- At most one active job per (video, type): duplicate enqueues fail instead of double-processing.
CREATE UNIQUE INDEX video_jobs_one_active ON video_processing_jobs (video_id, job_type) WHERE status IN ('QUEUED', 'PROCESSING');
CREATE INDEX video_jobs_queue ON video_processing_jobs (scheduled_at) WHERE status = 'QUEUED';        -- FOR UPDATE SKIP LOCKED
CREATE INDEX video_jobs_stale ON video_processing_jobs (heartbeat_at) WHERE status = 'PROCESSING';
CREATE INDEX video_jobs_by_video ON video_processing_jobs (video_id, created_at DESC);

-- Denormalised search document, one row per video. It is the single thing a future OpenSearch /
-- Meilisearch indexer needs to read, and it keeps the GIN index off the hot videos table.
-- Refreshed by refresh_video_search() (see 000009) when a video is published/edited or its tags change.
CREATE TABLE video_search (
  video_id   uuid PRIMARY KEY REFERENCES videos (id) ON DELETE CASCADE,
  document   tsvector NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX video_search_document ON video_search USING gin (document);
