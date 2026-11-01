# REDXTUBE data dictionary

> **Generated** from `db/migrations/*.up.sql` by `db/tools/gen_dictionary.py` (`make dictionary`). Do not edit by hand.

39 tables. Column notes come from the comments in the migrations.

## Tables

- [`users`](#users) — Accounts. One row per person; credentials, role, lifecycle status. Soft-deleted via `deleted_at`.
- [`user_profiles`](#user_profiles) — Public channel profile (bio, images, links) plus cached `subscriber_count` / `video_count`.
- [`user_settings`](#user_settings) — Per-user preferences: notifications, privacy flags, playback, history switches.
- [`sessions`](#sessions) — Login sessions. Only token hashes are stored; IP is truncated to a prefix.
- [`verification_tokens`](#verification_tokens) — One-time tokens for email verification, phone OTP and password reset (HMAC-hashed, single use).
- [`age_verifications`](#age_verifications) — Outcome of 18+ verification (status/method/provider reference). No identity documents.
- [`categories`](#categories) — Curated video categories.
- [`tags`](#tags) — Normalised tags with a cached `usage_count`.
- [`videos`](#videos) — Core video record: metadata, lifecycle `status`, `visibility`, `moderation_status`, storage keys (no binary data).
- [`video_stats`](#video_stats) — Hot counters (views, likes, dislikes, comments, watch time) kept off the wide `videos` row.
- [`video_tags`](#video_tags) — Many-to-many link between videos and tags.
- [`video_metadata`](#video_metadata) — Technical facts about the source file (resolution, codec, fps, bitrate). Enforces max 1080p.
- [`video_variants`](#video_variants) — Transcoded HLS renditions (360p–1080p) and their processing status.
- [`video_thumbnails`](#video_thumbnails) — Generated and custom thumbnails; at most one primary per video.
- [`video_captions`](#video_captions) — Subtitle tracks (WebVTT) per language.
- [`upload_sessions`](#upload_sessions) — Multipart upload bookkeeping (provider upload id, part size/count, expiry).
- [`video_processing_jobs`](#video_processing_jobs) — Durable record of FFmpeg pipeline jobs (queue state, attempts, heartbeat).
- [`video_search`](#video_search) — Denormalised full-text document per video (title, tags, creator, description) with a GIN index.
- [`watch_history`](#watch_history) — Per-user playback position and last-watched time (History, Continue watching).
- [`video_reactions`](#video_reactions) — One like/dislike per user per video.
- [`comments`](#comments) — Threaded comments (one reply level), moderation status, cached like/reply counts.
- [`comment_reactions`](#comment_reactions) — One reaction per user per comment.
- [`subscriptions`](#subscriptions) — Free follow relationship: subscriber -> creator.
- [`playlists`](#playlists) — User playlists with visibility; soft-deleted.
- [`playlist_videos`](#playlist_videos) — Ordered playlist items (`position`, reorderable in one statement).
- [`watch_later`](#watch_later) — Per-user "Watch later" list.
- [`blocked_users`](#blocked_users) — Creators/users a user has blocked.
- [`not_interested`](#not_interested) — Videos a user asked to see less of.
- [`notifications`](#notifications) — In-app notifications; title/body are built at read time from type + payload.
- [`search_history`](#search_history) — Recent searches per user (capped, expiring).
- [`reports`](#reports) — User reports against a video, comment or user; evidence-preserving (RESTRICT FKs).
- [`moderation_actions`](#moderation_actions) — Append-only log of moderator decisions.
- [`audit_logs`](#audit_logs) — Append-only security/admin audit trail (no secrets, hashed IP).
- [`video_views`](#video_views) — Raw view events, partitioned by month, retained ~90 days. No FKs by design.
- [`video_daily_analytics`](#video_daily_analytics) — Per-video daily rollup (views, watch time, likes, comments, unique viewers).
- [`creator_daily_analytics`](#creator_daily_analytics) — Per-creator daily rollup for Studio overview.
- [`creator_daily_breakdown`](#creator_daily_breakdown) — Per-creator daily views by country / device / traffic source.
- [`upload_consents`](#upload_consents) — Append-only record that the uploader declared everyone is 18+, consented and rights are held (one per published video).
- [`blocked_hashes`](#blocked_hashes) — SHA-256 of original files that must never be published again; a trigger removes matching videos.

## users

Accounts. One row per person; credentials, role, lifecycle status. Soft-deleted via `deleted_at`.

*Created in `000002_identity.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | generate UUIDv7 in Go for index locality **PK** |
| `username` | `citext` | no |  |  |  |
| `email` | `citext` | no |  |  |  |
| `phone` | `text` | yes |  |  |  |
| `password_hash` | `text` | no |  |  | argon2id (preferred) or bcrypt, never plaintext |
| `display_name` | `text` | no |  |  |  |
| `role` | `user_role` | no | `'USER'` |  |  |
| `status` | `user_status` | no | `'PENDING'` |  |  |
| `is_verified` | `boolean` | no | `false` |  | cached: email verified |
| `is_creator` | `boolean` | no | `false` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |
| `last_login_at` | `timestamptz` | yes |  |  |  |
| `deleted_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `users_username_key` — `CONSTRAINT users_username_key UNIQUE (username)`
- `users_email_key` — `CONSTRAINT users_email_key UNIQUE (email)`
- `users_phone_key` — `CONSTRAINT users_phone_key UNIQUE (phone)`
- `users_username_format` — `CONSTRAINT users_username_format CHECK (username ~ '^[A-Za-z0-9_]{3,24}$')`
- `users_email_format` — `CONSTRAINT users_email_format CHECK (char_length(email) <= 254 AND email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')`
- `users_phone_e164` — `CONSTRAINT users_phone_e164 CHECK (phone IS NULL OR phone ~ '^\+[1-9][0-9]{6,14}$')`
- `users_password_hash_format` — `CONSTRAINT users_password_hash_format CHECK (password_hash ~ '^\$(argon2id\|2[aby])\$')`
- `users_display_name_len` — `CONSTRAINT users_display_name_len CHECK (char_length(btrim(display_name)) BETWEEN 2 AND 40)`
- `users_creator_flag` — `CONSTRAINT users_creator_flag CHECK (role <> 'CREATOR' OR is_creator)`
- `users_deleted_consistency` — `CONSTRAINT users_deleted_consistency CHECK ((status = 'DELETED') = (deleted_at IS NOT NULL))`

**Indexes**

- `users_attention` — btree (`status, created_at DESC`) WHERE `status <> 'ACTIVE'`
- `users_display_name_trgm` — gin (`display_name gin_trgm_ops`) WHERE `is_creator AND status = 'ACTIVE'`

**Triggers**

- `users_create_children` — AFTER INSERT
- `users_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## user_profiles

Public channel profile (bio, images, links) plus cached `subscriber_count` / `video_count`.

*Created in `000002_identity.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` | **PK** |
| `bio` | `text` | no | `''` |  |  |
| `avatar_storage_key` | `text` | yes |  |  |  |
| `banner_storage_key` | `text` | yes |  |  |  |
| `website` | `text` | yes |  |  |  |
| `location` | `text` | yes |  |  |  |
| `social_links` | `jsonb` | no | `'[]'::jsonb` |  | [{ "label": "...", "url": "https://..." }] |
| `subscriber_count` | `integer` | no | `0` |  | cached, maintained by trigger (see 000007) |
| `video_count` | `integer` | no | `0` |  | cached: public published videos |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `user_profiles_bio_len` — `CONSTRAINT user_profiles_bio_len CHECK (char_length(bio) <= 300)`
- `user_profiles_website` — `CONSTRAINT user_profiles_website CHECK (website IS NULL OR (char_length(website) <= 300 AND website ~* '^https?://'))`
- `user_profiles_location_len` — `CONSTRAINT user_profiles_location_len CHECK (location IS NULL OR char_length(location) <= 100)`
- `user_profiles_social_links` — `CONSTRAINT user_profiles_social_links CHECK (jsonb_typeof(social_links) = 'array' AND jsonb_array_length(social_links) <= 10)`
- `user_profiles_counters` — `CONSTRAINT user_profiles_counters CHECK (subscriber_count >= 0 AND video_count >= 0)`

## user_settings

Per-user preferences: notifications, privacy flags, playback, history switches.

*Created in `000002_identity.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` | **PK** |
| `language` | `text` | no | `'en'` |  |  |
| `theme` | `theme_pref` | no | `'DARK'` |  |  |
| `autoplay` | `boolean` | no | `true` |  |  |
| `email_notifications` | `boolean` | no | `false` |  |  |
| `push_notifications` | `boolean` | no | `true` |  |  |
| `comment_notifications` | `boolean` | no | `true` |  | replies + comments on my videos |
| `subscription_notifications` | `boolean` | no | `true` |  | new videos from creators I follow |
| `subscriber_notifications` | `boolean` | no | `true` |  | someone subscribed to me |
| `system_notifications` | `boolean` | no | `true` |  |  |
| `show_subscriptions` | `boolean` | no | `false` |  | privacy |
| `show_liked_videos` | `boolean` | no | `false` |  | privacy |
| `save_watch_history` | `boolean` | no | `true` |  | privacy (also gates writes to watch_history) |
| `save_search_history` | `boolean` | no | `true` |  | privacy (gates writes to search_history) |
| `extra` | `jsonb` | no | `'{}'::jsonb` |  | future flags without a migration |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `user_settings_language` — `CONSTRAINT user_settings_language CHECK (language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$')`
- `user_settings_extra` — `CONSTRAINT user_settings_extra CHECK (jsonb_typeof(extra) = 'object' AND octet_length(extra::text) <= 4096)`

**Triggers**

- `user_settings_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## sessions

Login sessions. Only token hashes are stored; IP is truncated to a prefix.

*Created in `000002_identity.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `session_token_hash` | `bytea` | no |  |  | SHA-256 of a 256-bit random token |
| `refresh_token_hash` | `bytea` | yes |  |  |  |
| `ip_prefix` | `cidr` | yes |  |  | truncated (/24 IPv4, /56 IPv6): data minimisation |
| `user_agent` | `text` | yes |  |  |  |
| `device_info` | `jsonb` | no | `'{}'::jsonb` |  |  |
| `expires_at` | `timestamptz` | no |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `last_used_at` | `timestamptz` | no | `now()` |  |  |
| `revoked_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `sessions_token_key` — `CONSTRAINT sessions_token_key UNIQUE (session_token_hash)`
- `sessions_refresh_key` — `CONSTRAINT sessions_refresh_key UNIQUE (refresh_token_hash)`
- `sessions_token_len` — `CONSTRAINT sessions_token_len CHECK (octet_length(session_token_hash) = 32)`
- `sessions_refresh_len` — `CONSTRAINT sessions_refresh_len CHECK (refresh_token_hash IS NULL OR octet_length(refresh_token_hash) = 32)`
- `sessions_expiry` — `CONSTRAINT sessions_expiry CHECK (expires_at > created_at)`
- `sessions_ip_prefix` — `CONSTRAINT sessions_ip_prefix CHECK ( ip_prefix IS NULL OR (family(ip_prefix) = 4 AND masklen(ip_prefix) <= 24) OR (family(ip_prefix) = 6 AND masklen(ip_prefix) <= 56) )`
- `sessions_ua_len` — `CONSTRAINT sessions_ua_len CHECK (user_agent IS NULL OR char_length(user_agent) <= 512)`

**Indexes**

- `sessions_active_by_user` — btree (`user_id, last_used_at DESC`) WHERE `revoked_at IS NULL`
- `sessions_expires_at` — btree (`expires_at`)

## verification_tokens

One-time tokens for email verification, phone OTP and password reset (HMAC-hashed, single use).

*Created in `000002_identity.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `purpose` | `verification_purpose` | no |  |  |  |
| `token_hash` | `bytea` | no |  |  |  |
| `target` | `text` | yes |  |  | email/phone being verified (supports change of address) |
| `attempts` | `smallint` | no | `0` |  |  |
| `max_attempts` | `smallint` | no | `5` |  |  |
| `expires_at` | `timestamptz` | no |  |  |  |
| `consumed_at` | `timestamptz` | yes |  |  | set exactly once, atomically (see queries) |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `verification_tokens_hash_len` — `CONSTRAINT verification_tokens_hash_len CHECK (octet_length(token_hash) = 32)`
- `verification_tokens_attempts` — `CONSTRAINT verification_tokens_attempts CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts)`
- `verification_tokens_expiry` — `CONSTRAINT verification_tokens_expiry CHECK (expires_at > created_at)`
- `verification_tokens_target` — `CONSTRAINT verification_tokens_target CHECK (purpose = 'PASSWORD_RESET' OR target IS NOT NULL)`

**Indexes**

- `verification_tokens_open` — btree (`user_id, purpose, created_at DESC`) WHERE `consumed_at IS NULL`
- `verification_tokens_expires_at` — btree (`expires_at`)

## age_verifications

Outcome of 18+ verification (status/method/provider reference). No identity documents.

*Created in `000002_identity.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `status` | `age_verification_status` | no | `'PENDING'` |  |  |
| `method` | `age_verification_method` | no |  |  |  |
| `provider` | `text` | yes |  |  |  |
| `provider_reference` | `text` | yes |  |  |  |
| `verified_at` | `timestamptz` | yes |  |  |  |
| `expires_at` | `timestamptz` | yes |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `age_verifications_verified` — `CONSTRAINT age_verifications_verified CHECK (status <> 'VERIFIED' OR verified_at IS NOT NULL)`
- `age_verifications_provider` — `CONSTRAINT age_verifications_provider CHECK (method NOT IN ('THIRD_PARTY', 'DOCUMENT') OR provider IS NOT NULL)`
- `age_verifications_ref_len` — `CONSTRAINT age_verifications_ref_len CHECK (provider_reference IS NULL OR char_length(provider_reference) <= 200)`
- `age_verifications_expiry` — `CONSTRAINT age_verifications_expiry CHECK (expires_at IS NULL OR verified_at IS NULL OR expires_at > verified_at)`

**Indexes**

- `age_verifications_one_pending` — UNIQUE btree (`user_id`) WHERE `status = 'PENDING'`
- `age_verifications_latest` — btree (`user_id, created_at DESC`)

**Triggers**

- `age_verifications_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## categories

Curated video categories.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `smallint` | no |  |  | **PK** |
| `name` | `citext` | no |  |  |  |
| `slug` | `text` | no |  |  |  |
| `description` | `text` | yes |  |  |  |
| `thumbnail_storage_key` | `text` | yes |  |  |  |
| `is_active` | `boolean` | no | `true` |  |  |
| `sort_order` | `integer` | no | `0` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `categories_name_key` — `CONSTRAINT categories_name_key UNIQUE (name)`
- `categories_slug_key` — `CONSTRAINT categories_slug_key UNIQUE (slug)`
- `categories_slug_format` — `CONSTRAINT categories_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')`
- `categories_name_len` — `CONSTRAINT categories_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 50)`

**Indexes**

- `categories_active_order` — btree (`sort_order, name`) WHERE `is_active`

**Triggers**

- `categories_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## tags

Normalised tags with a cached `usage_count`.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `bigint` | no |  |  | **PK** |
| `name` | `text` | no |  |  | display form |
| `slug` | `text` | no |  |  | normalised, lower-case, unique |
| `usage_count` | `integer` | no | `0` |  | cached, maintained by trigger |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `tags_slug_key` — `CONSTRAINT tags_slug_key UNIQUE (slug)`
- `tags_slug_format` — `CONSTRAINT tags_slug_format CHECK (slug = lower(slug) AND char_length(slug) BETWEEN 1 AND 30 AND slug !~ '[[:space:]]')`
- `tags_name_len` — `CONSTRAINT tags_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 30)`
- `tags_usage_count` — `CONSTRAINT tags_usage_count CHECK (usage_count >= 0)`

**Indexes**

- `tags_slug_prefix` — btree (`slug text_pattern_ops`)

## videos

Core video record: metadata, lifecycle `status`, `visibility`, `moderation_status`, storage keys (no binary data).

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `creator_id` | `uuid` | no |  | `users(id) ON DELETE RESTRICT` |  |
| `title` | `text` | no |  |  |  |
| `slug` | `text` | yes |  |  | cosmetic SEO text; lookups use id |
| `description` | `text` | no | `''` |  |  |
| `video_type` | `video_type` | yes |  |  | NULL until the METADATA job measured the duration |
| `visibility` | `video_visibility` | no | `'PRIVATE'` |  |  |
| `status` | `video_status` | no | `'UPLOADING'` |  |  |
| `moderation_status` | `moderation_status` | no | `'OK'` |  |  |
| `duration_seconds` | `integer` | yes |  |  |  |
| `file_size_bytes` | `bigint` | yes |  |  |  |
| `original_filename` | `text` | yes |  |  |  |
| `original_storage_key` | `text` | yes |  |  |  |
| `hls_master_key` | `text` | yes |  |  | master .m3u8 key, set when the HLS job finishes |
| `category_id` | `smallint` | yes |  | `categories(id)` |  |
| `language` | `text` | yes |  |  |  |
| `age_restricted` | `boolean` | no | `true` |  |  |
| `comments_mode` | `comment_mode` | no | `'ON'` |  |  |
| `published_at` | `timestamptz` | yes |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |
| `deleted_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `videos_id_creator_key` — `CONSTRAINT videos_id_creator_key UNIQUE (id, creator_id)`
- `videos_title_len` — `CONSTRAINT videos_title_len CHECK (char_length(btrim(title)) BETWEEN 1 AND 100)`
- `videos_description_len` — `CONSTRAINT videos_description_len CHECK (char_length(description) <= 5000)`
- `videos_slug_format` — `CONSTRAINT videos_slug_format CHECK (slug IS NULL OR slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')`
- `videos_language_format` — `CONSTRAINT videos_language_format CHECK (language IS NULL OR language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$')`
- `videos_duration_range` — `CONSTRAINT videos_duration_range CHECK (duration_seconds IS NULL OR (duration_seconds > 0 AND duration_seconds <= 86400))`
- `videos_type_matches_duration` — `CONSTRAINT videos_type_matches_duration CHECK ( video_type IS NULL OR duration_seconds IS NULL OR (video_type = 'SHORT' AND duration_seconds <= 60) OR (video_type = 'LONG' AND duration_seconds > 60) )`
- `videos_size_limits` — `CONSTRAINT videos_size_limits CHECK ( file_size_bytes IS NULL OR (file_size_bytes > 0 AND file_size_bytes <= 5368709120 AND (video_type IS DISTINCT FROM 'SHORT' OR file_size_bytes <= 209715200)) )`
- `videos_ready_requirements` — `CONSTRAINT videos_ready_requirements CHECK ( status NOT IN ('READY', 'PUBLISHED') OR (video_type IS NOT NULL AND duration_seconds IS NOT NULL AND hls_master_key IS NOT NULL) )`
- `videos_published_requirements` — `CONSTRAINT videos_published_requirements CHECK ( status <> 'PUBLISHED' OR (published_at IS NOT NULL AND category_id IS NOT NULL) )`
- `videos_published_at_only_when_published` — `CONSTRAINT videos_published_at_only_when_published CHECK (published_at IS NULL OR status = 'PUBLISHED')`

**Indexes**

- `videos_public_recent` — btree (`published_at DESC, id DESC`) WHERE `status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL`
- `videos_public_category` — btree (`category_id, published_at DESC, id DESC`) WHERE `status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL`
- `videos_public_creator` — btree (`creator_id, published_at DESC, id DESC`) WHERE `status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL`
- `videos_public_shorts` — btree (`published_at DESC, id DESC`) WHERE `status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL AND video_type = 'SHORT'`
- `videos_public_title_trgm` — gin (`title gin_trgm_ops`) WHERE `status = 'PUBLISHED' AND visibility = 'PUBLIC' AND moderation_status = 'OK' AND deleted_at IS NULL`
- `videos_by_creator` — btree (`creator_id, created_at DESC, id DESC`) WHERE `deleted_at IS NULL`
- `videos_in_flight` — btree (`created_at`) WHERE `status IN ('UPLOADING', 'PROCESSING')`
- `videos_moderated` — btree (`moderation_status, updated_at DESC`) WHERE `moderation_status <> 'OK'`
- `videos_by_content_hash` — btree (`content_sha256`) WHERE `content_sha256 IS NOT NULL`

**Triggers**

- `videos_create_stats` — AFTER INSERT
- `videos_immutable_creator` — BEFORE UPDATE OF creator_id
- `videos_profile_video_count` — AFTER INSERT OR DELETE OR UPDATE OF status, visibility, moderation_status, deleted_at
- `videos_hash_blocklist` — BEFORE INSERT OR UPDATE OF content_sha256
- `videos_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## video_stats

Hot counters (views, likes, dislikes, comments, watch time) kept off the wide `videos` row.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` | **PK** |
| `view_count` | `bigint` | no | `0` |  | flushed in batches from Redis |
| `like_count` | `integer` | no | `0` |  | trigger-maintained |
| `dislike_count` | `integer` | no | `0` |  | trigger-maintained |
| `comment_count` | `integer` | no | `0` |  | trigger-maintained (visible comments + replies) |
| `watch_time_seconds` | `bigint` | no | `0` |  | flushed in batches |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `video_stats_non_negative` — `CONSTRAINT video_stats_non_negative CHECK ( view_count >= 0 AND like_count >= 0 AND dislike_count >= 0 AND comment_count >= 0 AND watch_time_seconds >= 0 )`

## video_tags

Many-to-many link between videos and tags.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `tag_id` | `bigint` | no |  | `tags(id) ON DELETE CASCADE` |  |

**Constraints**

- `PRIMARY KEY (video_id, tag_id)`

**Indexes**

- `video_tags_by_tag` — btree (`tag_id, video_id`)

**Triggers**

- `video_tags_count` — AFTER INSERT OR DELETE

## video_metadata

Technical facts about the source file (resolution, codec, fps, bitrate). Enforces max 1080p.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` | **PK** |
| `width` | `integer` | no |  |  |  |
| `height` | `integer` | no |  |  |  |
| `fps` | `numeric(6, 3)` | yes |  |  |  |
| `video_codec` | `text` | yes |  |  |  |
| `audio_codec` | `text` | yes |  |  |  |
| `bitrate` | `bigint` | yes |  |  |  |
| `container` | `text` | yes |  |  |  |
| `aspect_ratio` | `numeric(10, 5)` | yes |  |  | *generated:* `(width::numeric / NULLIF(height, 0))` |
| `has_audio` | `boolean` | no | `true` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `video_metadata_dimensions` — `CONSTRAINT video_metadata_dimensions CHECK (width > 0 AND height > 0)`
- `video_metadata_max_1080p` — `CONSTRAINT video_metadata_max_1080p CHECK (LEAST(width, height) <= 1080 AND GREATEST(width, height) <= 1920)`
- `video_metadata_fps` — `CONSTRAINT video_metadata_fps CHECK (fps IS NULL OR (fps > 0 AND fps <= 240))`
- `video_metadata_bitrate` — `CONSTRAINT video_metadata_bitrate CHECK (bitrate IS NULL OR bitrate > 0)`

**Triggers**

- `video_metadata_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## video_variants

Transcoded HLS renditions (360p–1080p) and their processing status.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `resolution` | `video_resolution` | no |  |  |  |
| `width` | `integer` | yes |  |  |  |
| `height` | `integer` | yes |  |  |  |
| `bitrate` | `integer` | yes |  |  |  |
| `codec` | `text` | yes |  |  |  |
| `storage_key` | `text` | no |  |  | prefix/directory of the HLS rendition |
| `manifest_path` | `text` | yes |  |  | rendition playlist, relative to storage_key |
| `file_size_bytes` | `bigint` | yes |  |  |  |
| `status` | `variant_status` | no | `'PENDING'` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `video_variants_video_resolution_key` — `CONSTRAINT video_variants_video_resolution_key UNIQUE (video_id, resolution)`
- `video_variants_ready` — `CONSTRAINT video_variants_ready CHECK ( status <> 'READY' OR (manifest_path IS NOT NULL AND width IS NOT NULL AND height IS NOT NULL AND bitrate IS NOT NULL) )`
- `video_variants_dimensions` — `CONSTRAINT video_variants_dimensions CHECK ((width IS NULL OR width > 0) AND (height IS NULL OR height > 0))`
- `video_variants_sizes` — `CONSTRAINT video_variants_sizes CHECK ((bitrate IS NULL OR bitrate > 0) AND (file_size_bytes IS NULL OR file_size_bytes >= 0))`

**Triggers**

- `video_variants_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## video_thumbnails

Generated and custom thumbnails; at most one primary per video.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `storage_key` | `text` | no |  |  |  |
| `width` | `integer` | yes |  |  |  |
| `height` | `integer` | yes |  |  |  |
| `type` | `thumbnail_type` | no | `'THUMBNAIL'` |  |  |
| `source` | `asset_source` | no | `'GENERATED'` |  |  |
| `status` | `asset_status` | no | `'READY'` |  | CUSTOM uploads start as PENDING |
| `is_primary` | `boolean` | no | `false` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `video_thumbnails_dimensions` — `CONSTRAINT video_thumbnails_dimensions CHECK ((width IS NULL OR width > 0) AND (height IS NULL OR height > 0))`
- `video_thumbnails_primary_ready` — `CONSTRAINT video_thumbnails_primary_ready CHECK (NOT is_primary OR status = 'READY')`

**Indexes**

- `video_thumbnails_one_primary` — UNIQUE btree (`video_id`) WHERE `is_primary`
- `video_thumbnails_by_video` — btree (`video_id, type`)

## video_captions

Subtitle tracks (WebVTT) per language.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `language` | `text` | no |  |  |  |
| `label` | `text` | yes |  |  |  |
| `storage_key` | `text` | no |  |  |  |
| `status` | `asset_status` | no | `'PENDING'` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `video_captions_video_language_key` — `CONSTRAINT video_captions_video_language_key UNIQUE (video_id, language)`
- `video_captions_language` — `CONSTRAINT video_captions_language CHECK (language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$')`
- `video_captions_label_len` — `CONSTRAINT video_captions_label_len CHECK (label IS NULL OR char_length(label) <= 50)`

## upload_sessions

Multipart upload bookkeeping (provider upload id, part size/count, expiry).

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `storage_key` | `text` | no |  |  |  |
| `provider_upload_id` | `text` | no |  |  | e.g. S3 multipart UploadId |
| `declared_size_bytes` | `bigint` | no |  |  |  |
| `declared_content_type` | `text` | no |  |  |  |
| `part_size_bytes` | `integer` | no |  |  |  |
| `part_count` | `integer` | no |  |  |  |
| `status` | `upload_status` | no | `'INITIATED'` |  |  |
| `expires_at` | `timestamptz` | no |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |
| `completed_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `upload_sessions_video_key` — `CONSTRAINT upload_sessions_video_key UNIQUE (video_id)`
- `upload_sessions_size` — `CONSTRAINT upload_sessions_size CHECK (declared_size_bytes > 0 AND declared_size_bytes <= 5368709120)`
- `upload_sessions_part_size` — `CONSTRAINT upload_sessions_part_size CHECK (part_size_bytes >= 5242880)`
- `upload_sessions_part_count` — `CONSTRAINT upload_sessions_part_count CHECK (part_count BETWEEN 1 AND 10000)`
- `upload_sessions_expiry` — `CONSTRAINT upload_sessions_expiry CHECK (expires_at > created_at)`
- `upload_sessions_completed` — `CONSTRAINT upload_sessions_completed CHECK ((status = 'COMPLETED') = (completed_at IS NOT NULL))`

**Indexes**

- `upload_sessions_open_by_user` — btree (`user_id`) WHERE `status IN ('INITIATED', 'COMPLETING')`
- `upload_sessions_expiring` — btree (`expires_at`) WHERE `status IN ('INITIATED', 'COMPLETING')`

**Triggers**

- `upload_sessions_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## video_processing_jobs

Durable record of FFmpeg pipeline jobs (queue state, attempts, heartbeat).

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `job_type` | `job_type` | no |  |  |  |
| `status` | `job_status` | no | `'QUEUED'` |  |  |
| `attempts` | `smallint` | no | `0` |  |  |
| `max_attempts` | `smallint` | no | `3` |  |  |
| `error_message` | `text` | yes |  |  |  |
| `worker_id` | `text` | yes |  |  |  |
| `heartbeat_at` | `timestamptz` | yes |  |  | workers refresh this; stale rows are re-queued |
| `scheduled_at` | `timestamptz` | no | `now()` |  |  |
| `started_at` | `timestamptz` | yes |  |  |  |
| `completed_at` | `timestamptz` | yes |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `video_jobs_attempts` — `CONSTRAINT video_jobs_attempts CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts)`
- `video_jobs_error_len` — `CONSTRAINT video_jobs_error_len CHECK (error_message IS NULL OR char_length(error_message) <= 2000)`
- `video_jobs_processing_started` — `CONSTRAINT video_jobs_processing_started CHECK (status <> 'PROCESSING' OR started_at IS NOT NULL)`
- `video_jobs_completed` — `CONSTRAINT video_jobs_completed CHECK (status <> 'COMPLETED' OR completed_at IS NOT NULL)`

**Indexes**

- `video_jobs_one_active` — UNIQUE btree (`video_id, job_type`) WHERE `status IN ('QUEUED', 'PROCESSING')`
- `video_jobs_queue` — btree (`scheduled_at`) WHERE `status = 'QUEUED'`
- `video_jobs_stale` — btree (`heartbeat_at`) WHERE `status = 'PROCESSING'`
- `video_jobs_by_video` — btree (`video_id, created_at DESC`)

**Triggers**

- `video_processing_jobs_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## video_search

Denormalised full-text document per video (title, tags, creator, description) with a GIN index.

*Created in `000003_catalog.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` | **PK** |
| `document` | `tsvector` | no |  |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Indexes**

- `video_search_document` — gin (`document`)

## watch_history

Per-user playback position and last-watched time (History, Continue watching).

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `last_position_seconds` | `integer` | no | `0` |  |  |
| `watch_duration_seconds` | `integer` | no | `0` |  |  |
| `completed` | `boolean` | no | `false` |  |  |
| `last_watched_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (user_id, video_id)`
- `watch_history_non_negative` — `CONSTRAINT watch_history_non_negative CHECK (last_position_seconds >= 0 AND watch_duration_seconds >= 0)`

**Indexes**

- `watch_history_recent` — btree (`user_id, last_watched_at DESC, video_id DESC`)
- `watch_history_continue` — btree (`user_id, last_watched_at DESC`) WHERE `completed = false AND last_position_seconds > 0`
- `watch_history_by_video` — btree (`video_id`)

## video_reactions

One like/dislike per user per video.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `reaction` | `reaction_type` | no |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (user_id, video_id)`

**Indexes**

- `video_reactions_by_video` — btree (`video_id, reaction`)
- `video_reactions_liked` — btree (`user_id, created_at DESC, video_id`) WHERE `reaction = 'LIKE'`

**Triggers**

- `video_reactions_count` — AFTER INSERT OR DELETE OR UPDATE OF reaction
- `video_reactions_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## comments

Threaded comments (one reply level), moderation status, cached like/reply counts.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `video_id` | `uuid` | no |  |  |  |
| `video_creator_id` | `uuid` | no |  |  | denormalised on purpose: Studio inbox query (kept consistent by FK below) |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `parent_comment_id` | `uuid` | yes |  |  |  |
| `content` | `text` | no |  |  |  |
| `status` | `comment_status` | no | `'VISIBLE'` |  |  |
| `is_pinned` | `boolean` | no | `false` |  |  |
| `like_count` | `integer` | no | `0` |  | trigger-maintained |
| `reply_count` | `integer` | no | `0` |  | trigger-maintained (visible replies) |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |
| `deleted_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `comments_video_fk` — `CONSTRAINT comments_video_fk FOREIGN KEY (video_id, video_creator_id)`
- `comments_id_video_key` — `CONSTRAINT comments_id_video_key UNIQUE (id, video_id)`
- `comments_parent_fk` — `CONSTRAINT comments_parent_fk FOREIGN KEY (parent_comment_id, video_id)`
- `comments_content_len` — `CONSTRAINT comments_content_len CHECK (char_length(btrim(content)) BETWEEN 1 AND 2000)`
- `comments_pin_top_level` — `CONSTRAINT comments_pin_top_level CHECK (NOT is_pinned OR parent_comment_id IS NULL)`
- `comments_counters` — `CONSTRAINT comments_counters CHECK (like_count >= 0 AND reply_count >= 0)`
- `comments_deleted_consistency` — `CONSTRAINT comments_deleted_consistency CHECK ((status = 'DELETED') = (deleted_at IS NOT NULL))`

**Indexes**

- `comments_video_new` — btree (`video_id, created_at DESC, id DESC`) WHERE `parent_comment_id IS NULL AND status = 'VISIBLE'`
- `comments_video_top` — btree (`video_id, like_count DESC, created_at DESC, id DESC`) WHERE `parent_comment_id IS NULL AND status = 'VISIBLE'`
- `comments_one_pinned` — UNIQUE btree (`video_id`) WHERE `is_pinned`
- `comments_replies` — btree (`parent_comment_id, created_at, id`) WHERE `parent_comment_id IS NOT NULL`
- `comments_by_user` — btree (`user_id, created_at DESC`)
- `comments_by_video` — btree (`video_id`)
- `comments_creator_inbox` — btree (`video_creator_id, created_at DESC, id DESC`) WHERE `status IN ('VISIBLE', 'UNDER_REVIEW')`
- `comments_creator_review` — btree (`video_creator_id, created_at DESC, id DESC`) WHERE `status = 'UNDER_REVIEW'`

**Triggers**

- `comments_counts` — AFTER INSERT OR DELETE OR UPDATE OF status, deleted_at
- `comments_depth` — BEFORE INSERT

## comment_reactions

One reaction per user per comment.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `comment_id` | `uuid` | no |  | `comments(id) ON DELETE CASCADE` |  |
| `reaction` | `reaction_type` | no | `'LIKE'` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (user_id, comment_id)`

**Indexes**

- `comment_reactions_by_comment` — btree (`comment_id`)

**Triggers**

- `comment_reactions_count` — AFTER INSERT OR DELETE OR UPDATE OF reaction

## subscriptions

Free follow relationship: subscriber -> creator.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `subscriber_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `creator_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (subscriber_id, creator_id)`
- `subscriptions_no_self` — `CONSTRAINT subscriptions_no_self CHECK (subscriber_id <> creator_id)`

**Indexes**

- `subscriptions_by_creator` — btree (`creator_id, created_at DESC`)
- `subscriptions_by_subscriber` — btree (`subscriber_id, created_at DESC`)

**Triggers**

- `subscriptions_count` — AFTER INSERT OR DELETE

## playlists

User playlists with visibility; soft-deleted.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `title` | `text` | no |  |  |  |
| `description` | `text` | no | `''` |  |  |
| `visibility` | `playlist_visibility` | no | `'PRIVATE'` |  |  |
| `thumbnail_video_id` | `uuid` | yes |  | `videos(id) ON DELETE SET NULL` | optional override; default = first video |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |
| `deleted_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `playlists_title_len` — `CONSTRAINT playlists_title_len CHECK (char_length(btrim(title)) BETWEEN 1 AND 100)`
- `playlists_description_len` — `CONSTRAINT playlists_description_len CHECK (char_length(description) <= 1000)`

**Indexes**

- `playlists_by_user` — btree (`user_id, updated_at DESC, id DESC`) WHERE `deleted_at IS NULL`
- `playlists_public_by_user` — btree (`user_id, updated_at DESC, id DESC`) WHERE `visibility = 'PUBLIC' AND deleted_at IS NULL`
- `playlists_public_title_trgm` — gin (`title gin_trgm_ops`) WHERE `visibility = 'PUBLIC' AND deleted_at IS NULL`
- `playlists_thumbnail_video` — btree (`thumbnail_video_id`) WHERE `thumbnail_video_id IS NOT NULL`

**Triggers**

- `playlists_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## playlist_videos

Ordered playlist items (`position`, reorderable in one statement).

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `playlist_id` | `uuid` | no |  | `playlists(id) ON DELETE CASCADE` |  |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `position` | `integer` | no |  |  |  |
| `added_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (playlist_id, video_id)`
- `playlist_videos_position_non_negative` — `CONSTRAINT playlist_videos_position_non_negative CHECK (position >= 0)`
- `playlist_videos_position_key` — `CONSTRAINT playlist_videos_position_key UNIQUE (playlist_id, position) DEFERRABLE INITIALLY DEFERRED`

**Indexes**

- `playlist_videos_by_video` — btree (`video_id`)

## watch_later

Per-user "Watch later" list.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (user_id, video_id)`

**Indexes**

- `watch_later_recent` — btree (`user_id, created_at DESC, video_id DESC`)
- `watch_later_by_video` — btree (`video_id`)

## blocked_users

Creators/users a user has blocked.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `blocked_user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (user_id, blocked_user_id)`
- `blocked_users_no_self` — `CONSTRAINT blocked_users_no_self CHECK (user_id <> blocked_user_id)`

**Indexes**

- `blocked_users_by_blocked` — btree (`blocked_user_id`)

## not_interested

Videos a user asked to see less of.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (user_id, video_id)`

**Indexes**

- `not_interested_by_video` — btree (`video_id`)

## notifications

In-app notifications; title/body are built at read time from type + payload.

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `bigint` | no |  |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `type` | `notification_type` | no |  |  |  |
| `actor_user_id` | `uuid` | yes |  | `users(id) ON DELETE SET NULL` |  |
| `video_id` | `uuid` | yes |  | `videos(id) ON DELETE CASCADE` |  |
| `comment_id` | `uuid` | yes |  | `comments(id) ON DELETE CASCADE` |  |
| `payload` | `jsonb` | no | `'{}'::jsonb` |  | small, type-specific extras; title/body are built at read time |
| `read_at` | `timestamptz` | yes |  |  |  |
| `is_read` | `boolean` | no |  |  | derived: can never disagree with read_at *generated:* `(read_at IS NOT NULL)` |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `notifications_payload` — `CONSTRAINT notifications_payload CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4096)`

**Indexes**

- `notifications_unread` — btree (`user_id, created_at DESC, id DESC`) WHERE `read_at IS NULL`
- `notifications_by_user` — btree (`user_id, created_at DESC, id DESC`)
- `notifications_by_video` — btree (`video_id`) WHERE `video_id IS NOT NULL`
- `notifications_by_comment` — btree (`comment_id`) WHERE `comment_id IS NOT NULL`
- `notifications_by_actor` — btree (`actor_user_id`) WHERE `actor_user_id IS NOT NULL`

## search_history

Recent searches per user (capped, expiring).

*Created in `000004_engagement.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `bigint` | no |  |  | **PK** |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `query` | `citext` | no |  |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `search_history_user_query_key` — `CONSTRAINT search_history_user_query_key UNIQUE (user_id, query)`
- `search_history_query_len` — `CONSTRAINT search_history_query_len CHECK (char_length(btrim(query)) BETWEEN 1 AND 100)`

**Indexes**

- `search_history_recent` — btree (`user_id, created_at DESC`)
- `search_history_created_at` — btree (`created_at`)

## reports

User reports against a video, comment or user; evidence-preserving (RESTRICT FKs).

*Created in `000005_safety.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `reporter_user_id` | `uuid` | yes |  | `users(id) ON DELETE SET NULL` | NULL = anonymous reporter (rate-limit in Redis) |
| `target_type` | `report_target` | no |  |  |  |
| `target_video_id` | `uuid` | yes |  | `videos(id) ON DELETE RESTRICT` |  |
| `target_comment_id` | `uuid` | yes |  | `comments(id) ON DELETE RESTRICT` |  |
| `target_user_id` | `uuid` | yes |  | `users(id) ON DELETE RESTRICT` | also covers "report a channel/creator" |
| `reason` | `report_reason` | no |  |  |  |
| `description` | `text` | yes |  |  |  |
| `status` | `report_status` | no | `'PENDING'` |  |  |
| `priority` | `smallint` | no | `1` |  | 0 = urgent (set by trigger from reason) |
| `assigned_to` | `uuid` | yes |  | `users(id) ON DELETE SET NULL` |  |
| `resolved_by` | `uuid` | yes |  | `users(id) ON DELETE SET NULL` |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |
| `updated_at` | `timestamptz` | no | `now()` |  |  |
| `resolved_at` | `timestamptz` | yes |  |  |  |

**Constraints**

- `reports_one_target` — `CONSTRAINT reports_one_target CHECK ( (target_video_id IS NOT NULL)::int + (target_comment_id IS NOT NULL)::int + (target_user_id IS NOT NULL)::int = 1 )`
- `reports_target_type_matches` — `CONSTRAINT reports_target_type_matches CHECK ( (target_type = 'VIDEO') = (target_video_id IS NOT NULL) AND (target_type = 'COMMENT') = (target_comment_id IS NOT NULL) AND (target_type = 'USER') = (target_user_id IS NOT NULL) )`
- `reports_description_len` — `CONSTRAINT reports_description_len CHECK (description IS NULL OR char_length(description) <= 1000)`
- `reports_other_needs_description` — `CONSTRAINT reports_other_needs_description CHECK (reason <> 'OTHER' OR char_length(btrim(coalesce(description, ''))) > 0)`
- `reports_resolved_consistency` — `CONSTRAINT reports_resolved_consistency CHECK ((status IN ('RESOLVED', 'REJECTED')) = (resolved_at IS NOT NULL))`
- `reports_priority_range` — `CONSTRAINT reports_priority_range CHECK (priority BETWEEN 0 AND 2)`

**Indexes**

- `reports_queue` — btree (`priority, created_at`) WHERE `status IN ('PENDING', 'UNDER_REVIEW')`
- `reports_by_video` — btree (`target_video_id`) WHERE `target_video_id IS NOT NULL`
- `reports_by_comment` — btree (`target_comment_id`) WHERE `target_comment_id IS NOT NULL`
- `reports_by_target_user` — btree (`target_user_id`) WHERE `target_user_id IS NOT NULL`
- `reports_by_reporter` — btree (`reporter_user_id, created_at DESC`) WHERE `reporter_user_id IS NOT NULL`
- `reports_open_video_once` — UNIQUE btree (`reporter_user_id, target_video_id`) WHERE `target_video_id IS NOT NULL AND reporter_user_id IS NOT NULL AND status IN ('PENDING', 'UNDER_REVIEW')`
- `reports_open_comment_once` — UNIQUE btree (`reporter_user_id, target_comment_id`) WHERE `target_comment_id IS NOT NULL AND reporter_user_id IS NOT NULL AND status IN ('PENDING', 'UNDER_REVIEW')`
- `reports_open_user_once` — UNIQUE btree (`reporter_user_id, target_user_id`) WHERE `target_user_id IS NOT NULL AND reporter_user_id IS NOT NULL AND status IN ('PENDING', 'UNDER_REVIEW')`

**Triggers**

- `reports_priority` — BEFORE INSERT
- `reports_set_updated_at` — BEFORE UPDATE (keeps updated_at current)

## moderation_actions

Append-only log of moderator decisions.

*Created in `000005_safety.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `uuid` | no | `gen_random_uuid()` |  | **PK** |
| `moderator_id` | `uuid` | no |  | `users(id) ON DELETE RESTRICT` |  |
| `report_id` | `uuid` | yes |  | `reports(id) ON DELETE SET NULL` |  |
| `target_type` | `report_target` | no |  |  |  |
| `target_video_id` | `uuid` | yes |  | `videos(id) ON DELETE RESTRICT` |  |
| `target_comment_id` | `uuid` | yes |  | `comments(id) ON DELETE RESTRICT` |  |
| `target_user_id` | `uuid` | yes |  | `users(id) ON DELETE RESTRICT` |  |
| `action` | `moderation_action_type` | no |  |  |  |
| `reason` | `text` | no |  |  |  |
| `notes` | `text` | yes |  |  |  |
| `expires_at` | `timestamptz` | yes |  |  | temporary suspensions / restrictions |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `moderation_one_target` — `CONSTRAINT moderation_one_target CHECK ( (target_video_id IS NOT NULL)::int + (target_comment_id IS NOT NULL)::int + (target_user_id IS NOT NULL)::int = 1 )`
- `moderation_target_type_matches` — `CONSTRAINT moderation_target_type_matches CHECK ( (target_type = 'VIDEO') = (target_video_id IS NOT NULL) AND (target_type = 'COMMENT') = (target_comment_id IS NOT NULL) AND (target_type = 'USER') = (target_user_id IS NOT NULL) )`
- `moderation_action_target` — `CONSTRAINT moderation_action_target CHECK ( (action NOT IN ('USER_SUSPENDED', 'USER_BANNED') OR target_type = 'USER') AND (action <> 'COMMENT_REMOVED' OR target_type = 'COMMENT') AND (action <> 'VIDEO_RESTRICTED' OR target_type = `
- `moderation_reason_len` — `CONSTRAINT moderation_reason_len CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500)`
- `moderation_notes_len` — `CONSTRAINT moderation_notes_len CHECK (notes IS NULL OR char_length(notes) <= 5000)`

**Indexes**

- `moderation_by_video` — btree (`target_video_id, created_at DESC`) WHERE `target_video_id IS NOT NULL`
- `moderation_by_comment` — btree (`target_comment_id, created_at DESC`) WHERE `target_comment_id IS NOT NULL`
- `moderation_by_user` — btree (`target_user_id, created_at DESC`) WHERE `target_user_id IS NOT NULL`
- `moderation_by_moderator` — btree (`moderator_id, created_at DESC`)
- `moderation_by_report` — btree (`report_id`) WHERE `report_id IS NOT NULL`

## audit_logs

Append-only security/admin audit trail (no secrets, hashed IP).

*Created in `000005_safety.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `bigint` | no |  |  | **PK** |
| `actor_user_id` | `uuid` | yes |  | `users(id) ON DELETE SET NULL` |  |
| `action` | `text` | no |  |  | dotted verb, e.g. 'user.role_changed' |
| `target_type` | `text` | yes |  |  |  |
| `target_id` | `text` | yes |  |  |  |
| `metadata` | `jsonb` | no | `'{}'::jsonb` |  |  |
| `ip_hash` | `bytea` | yes |  |  | HMAC of the IP, never the IP itself |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `audit_logs_action_format` — `CONSTRAINT audit_logs_action_format CHECK (action ~ '^[a-z][a-z0-9_.]{2,63}$')`
- `audit_logs_metadata` — `CONSTRAINT audit_logs_metadata CHECK (jsonb_typeof(metadata) = 'object' AND octet_length(metadata::text) <= 16384)`
- `audit_logs_no_secrets` — `CONSTRAINT audit_logs_no_secrets CHECK (NOT (metadata ?\| ARRAY['password', 'password_hash', 'token', 'secret', 'authorization', 'api_key']))`
- `audit_logs_ip_hash_len` — `CONSTRAINT audit_logs_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32)`

**Indexes**

- `audit_logs_by_actor` — btree (`actor_user_id, created_at DESC`) WHERE `actor_user_id IS NOT NULL`
- `audit_logs_by_target` — btree (`target_type, target_id, created_at DESC`) WHERE `target_id IS NOT NULL`
- `audit_logs_created_brin` — brin (`created_at`)

## video_views

Raw view events, partitioned by month, retained ~90 days. No FKs by design.

*Created in `000006_analytics.up.sql`* — **PARTITION BY RANGE (created_at**

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `bigint` | no | `nextval('video_views_id_seq')` |  |  |
| `video_id` | `uuid` | no |  |  | no FK on purpose: high-volume event table, integrity enforced by the writer |
| `user_id` | `uuid` | yes |  |  | no FK on purpose: account deletion must not rewrite billions of rows |
| `session_id` | `uuid` | yes |  |  | random viewer/session key, not an FK |
| `watch_duration_seconds` | `integer` | no | `0` |  |  |
| `completion_percentage` | `numeric(5, 2)` | no | `0` |  |  |
| `ip_hash` | `bytea` | yes |  |  | HMAC(daily-rotating salt, ip): cannot be reversed or tracked across days |
| `country_code` | `char(2)` | yes |  |  |  |
| `device_type` | `device_type` | no | `'OTHER'` |  |  |
| `traffic_source` | `traffic_source` | no | `'OTHER'` |  |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `PRIMARY KEY (id, created_at)`
- `video_views_duration` — `CONSTRAINT video_views_duration CHECK (watch_duration_seconds >= 0)`
- `video_views_completion` — `CONSTRAINT video_views_completion CHECK (completion_percentage BETWEEN 0 AND 100)`
- `video_views_ip_hash_len` — `CONSTRAINT video_views_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32)`
- `video_views_country` — `CONSTRAINT video_views_country CHECK (country_code IS NULL OR country_code ~ '^[A-Z]{2}$')`

**Indexes**

- `video_views_by_video` — btree (`video_id, created_at`)

## video_daily_analytics

Per-video daily rollup (views, watch time, likes, comments, unique viewers).

*Created in `000006_analytics.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE CASCADE` |  |
| `date` | `date` | no |  |  | UTC day |
| `views` | `integer` | no | `0` |  |  |
| `watch_time_seconds` | `bigint` | no | `0` |  |  |
| `likes` | `integer` | no | `0` |  | new likes that day |
| `comments` | `integer` | no | `0` |  | new comments that day |
| `unique_viewers` | `integer` | no | `0` |  | from Redis HyperLogLog at day close (not additive) |

**Constraints**

- `PRIMARY KEY (video_id, date)`
- `video_daily_non_negative` — `CONSTRAINT video_daily_non_negative CHECK ( views >= 0 AND watch_time_seconds >= 0 AND likes >= 0 AND comments >= 0 AND unique_viewers >= 0 )`

**Indexes**

- `video_daily_trending` — btree (`date, views DESC`)

## creator_daily_analytics

Per-creator daily rollup for Studio overview.

*Created in `000006_analytics.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `creator_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `date` | `date` | no |  |  |  |
| `views` | `bigint` | no | `0` |  |  |
| `watch_time_seconds` | `bigint` | no | `0` |  |  |
| `likes` | `integer` | no | `0` |  |  |
| `comments` | `integer` | no | `0` |  |  |
| `new_subscribers` | `integer` | no | `0` |  |  |
| `video_uploads` | `integer` | no | `0` |  |  |

**Constraints**

- `PRIMARY KEY (creator_id, date)`
- `creator_daily_non_negative` — `CONSTRAINT creator_daily_non_negative CHECK ( views >= 0 AND watch_time_seconds >= 0 AND likes >= 0 AND comments >= 0 AND new_subscribers >= 0 AND video_uploads >= 0 )`

## creator_daily_breakdown

Per-creator daily views by country / device / traffic source.

*Created in `000006_analytics.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `creator_id` | `uuid` | no |  | `users(id) ON DELETE CASCADE` |  |
| `date` | `date` | no |  |  |  |
| `dimension` | `breakdown_dimension` | no |  |  |  |
| `key` | `text` | no |  |  | 'US', 'MOBILE', 'SEARCH', ... |
| `views` | `bigint` | no | `0` |  |  |

**Constraints**

- `PRIMARY KEY (creator_id, date, dimension, key)`
- `creator_breakdown_views` — `CONSTRAINT creator_breakdown_views CHECK (views >= 0)`
- `creator_breakdown_key_len` — `CONSTRAINT creator_breakdown_key_len CHECK (char_length(key) BETWEEN 1 AND 32)`

## upload_consents

Append-only record that the uploader declared everyone is 18+, consented and rights are held (one per published video).

*Created in `000010_upload_moderation.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `id` | `bigint` | no |  |  | **PK** |
| `video_id` | `uuid` | no |  | `videos(id) ON DELETE RESTRICT` |  |
| `user_id` | `uuid` | no |  | `users(id) ON DELETE RESTRICT` |  |
| `statement_version` | `smallint` | no |  |  | which wording of the declaration the uploader saw |
| `ip_hash` | `bytea` | yes |  |  | HMAC of the IP, never the IP itself |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `upload_consents_video_key` — `CONSTRAINT upload_consents_video_key UNIQUE (video_id)`
- `upload_consents_version` — `CONSTRAINT upload_consents_version CHECK (statement_version > 0)`
- `upload_consents_ip_hash_len` — `CONSTRAINT upload_consents_ip_hash_len CHECK (ip_hash IS NULL OR octet_length(ip_hash) = 32)`

**Indexes**

- `upload_consents_by_user` — btree (`user_id, created_at DESC`)

## blocked_hashes

SHA-256 of original files that must never be published again; a trigger removes matching videos.

*Created in `000010_upload_moderation.up.sql`*

| Column | Type | Null | Default | References | Notes |
|---|---|---|---|---|---|
| `sha256` | `bytea` | no |  |  | **PK** |
| `reason` | `text` | no |  |  |  |
| `added_by` | `uuid` | yes |  | `users(id) ON DELETE SET NULL` |  |
| `created_at` | `timestamptz` | no | `now()` |  |  |

**Constraints**

- `blocked_hashes_len` — `CONSTRAINT blocked_hashes_len CHECK (octet_length(sha256) = 32)`
- `blocked_hashes_reason_len` — `CONSTRAINT blocked_hashes_reason_len CHECK (char_length(btrim(reason)) BETWEEN 1 AND 200)`
