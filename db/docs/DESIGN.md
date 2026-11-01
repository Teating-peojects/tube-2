# REDXTUBE database design

PostgreSQL 16+, 37 tables, 9 versioned migrations. Binary media never touches the database: only metadata and
**storage keys** are stored (public URLs are built as `CDN_BASE_URL + key`, so a CDN change needs no migration).

> **Status of this work.** The SQL has been checked with `db/tools/dbcheck.py` (every FK, index column, enum literal,
> trigger, seed row and down-migration is cross-checked, and a mutation test proves the checker catches deliberate
> breakage), but it has **not been executed against a real PostgreSQL server** (none was available while writing it).
> Run `cd backend && docker compose up --build` once on a scratch database before relying on it.

## 1. How the schema was derived

The brief said to analyse the frontend/backend requirements first. Every screen and API call of the finished frontend
was mapped to tables and indexes:

| Frontend feature | Tables | Hot query → index |
|---|---|---|
| Home / Explore / category chips | `videos`, `video_stats`, `video_thumbnails`, `categories` | newest public: `videos_public_recent`; by category: `videos_public_category` |
| Shorts feed | `videos` (`video_type='SHORT'`) | `videos_public_shorts` |
| Watch page, player (HLS), quality menu | `videos`, `video_variants`, `video_captions`, `video_stats` | PK lookup; `hls_master_key` + variants |
| Like / dislike | `video_reactions` (+ counters in `video_stats`) | PK `(user_id, video_id)` |
| Comments: top/new, replies, pinned, creator badge, "held for review" | `comments`, `comment_reactions` | `comments_video_new`, `comments_video_top`, `comments_replies`, `comments_one_pinned` |
| Subscribe, Subscriptions page + feed | `subscriptions`, `user_profiles.subscriber_count` | PK; `subscriptions_by_subscriber`; feed via `videos_public_creator` |
| Watch later, History, Continue watching | `watch_later`, `watch_history` | `watch_later_recent`, `watch_history_recent`, `watch_history_continue` |
| Liked videos | `video_reactions` | `video_reactions_liked` |
| Playlists, Save menu, reorder | `playlists`, `playlist_videos` | `playlists_by_user`, `playlist_videos_by_video`, deferred unique `position` |
| Search, suggestions, recent searches | `video_search`, trigram indexes, `search_history` | GIN on `document`; `videos_public_title_trgm`; `users_display_name_trgm` |
| Trending (4 tabs), Popular creators | `video_daily_analytics`, `creator_daily_analytics` (+ Redis cache) | `video_daily_trending` |
| Channel (banner, links, tabs) | `user_profiles`, `videos` | `videos_public_creator` |
| Upload (init → parts → complete → status → publish, drafts, thumbnails, captions) | `upload_sessions`, `videos`, `video_processing_jobs`, `video_thumbnails`, `video_captions` | `upload_sessions_open_by_user`, `video_jobs_queue` |
| Studio overview / analytics / videos / comments | `creator_daily_*`, `videos`, `comments` | PK ranges; `videos_by_creator`; `comments_creator_inbox`, `comments_creator_review` |
| Notifications + unread badge | `notifications` | `notifications_unread`, `notifications_by_user` |
| Report / Not interested / Block | `reports`, `not_interested`, `blocked_users` | `reports_queue`, `reports_open_*_once` (409 "already reported") |
| Settings (privacy, notifications, playback) | `user_settings` | PK |
| Auth: login, sessions, verify, reset, 18+ gate | `users`, `sessions`, `verification_tokens`, `age_verifications` | unique hash indexes, `sessions_active_by_user` |

**Deliberately not stored:** private messages (the brief's data-minimisation rule; the frontend `/messages` link has no
backend), identity documents, raw IP addresses, card data.
**Deferred (design only, `db/deferred/`):** monetisation (`subscription_plans`, `creator_subscriptions`, `transactions`,
`payouts`) and live streaming (`live_streams`). The core schema needs no change when they are added.

## 2. Where this design differs from the brief (and why)

| # | Brief | Delivered | Reason |
|---|---|---|---|
| 1 | role `CREATOR` and `is_creator` | both kept, plus `CHECK (role <> 'CREATOR' OR is_creator)` | two sources of truth can disagree. **Authorise uploads with `is_creator`**, staff features with `role`. A moderator can also be a creator. |
| 2 | `user_settings.privacy_settings` JSONB | typed boolean columns + `extra jsonb` | the flags are fixed, typed, and some gate writes (`save_watch_history`). JSONB kept only for future flags. |
| 3 | 3 tables: email/phone verification, password reset | one `verification_tokens` with `purpose` | identical shape; one code path, one purge job. |
| 4 | `sessions.ip_address` | `ip_prefix cidr` (CHECK: ≤ /24 IPv4, ≤ /56 IPv6) | data minimisation is enforced by the database, not by hope. |
| 5 | `videos.allow_comments boolean` | `comments_mode` ON / REVIEW / OFF | the frontend has "hold comments for review". |
| 6 | videos: no moderation field | `moderation_status` (OK/RESTRICTED/REMOVED), `hls_master_key` | lifecycle (`status`) and moderation are separate axes; the player needs the master playlist key. |
| 7 | `video_type` NOT NULL | nullable until the METADATA job measures the duration; required once READY/PUBLISHED | the type is derived from duration, which is unknown at upload start. |
| 8 | reports/moderation `target_type` incl. `CREATOR`; polymorphic `target_id` | types `VIDEO/COMMENT/USER`; three nullable FK columns + CHECK "exactly one" | a creator *is* a user; real FKs keep referential integrity. |
| 9 | `creator_daily_analytics.id` | PK `(creator_id, date)` | a surrogate key would never be used. |
| 10 | `notifications.is_read` | generated column from `read_at` | cannot disagree with `read_at`. Added types `VIDEO_FAILED`, `SYSTEM` (frontend needs them). |
| 11 | — | **added** `upload_sessions`, `video_captions`, `video_stats`, `video_search`, `not_interested`, `creator_daily_breakdown`, `video_views.traffic_source` | required by the frontend (multipart upload, subtitles, Studio audience/traffic panels, "Not interested"); `video_stats`/`video_search` keep hot/wide data off the `videos` row. |
| 12 | FKs everywhere | none on `video_views` | high-volume event table; integrity enforced by the writer; deleting an account must not rewrite billions of rows. |
| 13 | UUID ids | UUID for major entities; `smallint` categories, `bigint identity` for tags, notifications, search history, audit log | smaller indexes on high-volume tables. Generate **UUIDv7 in Go** (time-ordered → better B-tree locality); `gen_random_uuid()` is only the fallback default. |
| 14 | `videos.slug` | kept, **not unique** | titles repeat; lookups use `id`. Use `/watch/{id}/{slug}` for SEO if wanted. |
| 15 | `subscriptions_plans` | `subscription_plans` (deferred) | typo fixed. Free "follow" = `subscriptions`; paid membership = `creator_subscriptions`. |

## 3. Key decisions

### Identifiers, text, enums
* `citext` for `username`, `email`, category `name`, search queries → case-insensitive uniqueness with plain `UNIQUE`.
* Native enums for stable state machines. Adding a value later is `ALTER TYPE … ADD VALUE` (cannot be removed or used in
  the same transaction); use `text + CHECK` if a set changes often.
* All timestamps `timestamptz` (UTC). The bootstrap sets `timezone = 'UTC'` on the database.

### Visibility and soft delete
A video is **publicly listed** iff `status='PUBLISHED' AND visibility='PUBLIC' AND moderation_status='OK' AND deleted_at IS NULL`.
That predicate is the `WHERE` of every discovery partial index and of the `public_videos` view. Always query through the
view (or repeat the predicate) so the planner can use the small partial indexes; **never** expose rows with `deleted_at`.
Standalone indexes on `status`/`visibility` are intentionally absent (low cardinality, the planner would ignore them).
UNLISTED/PRIVATE videos are fetched by id with an ownership check.

### 1:1 child rows are created by triggers
`users → user_profiles + user_settings`, `videos → video_stats`. A missing row would otherwise be a latent bug.

### Counters (brief §34)
| Counter | Stored in | Updated by | Why |
|---|---|---|---|
| `subscriber_count`, `video_count` | `user_profiles` | triggers, same transaction | read on every channel/watch page; `COUNT(*)` over millions of subscriptions is too slow |
| `like_count`, `dislike_count`, `comment_count` | `video_stats` | triggers | consistent by construction; separate narrow row, `fillfactor 80` → HOT updates |
| comment `like_count`, `reply_count` | `comments` | triggers | |
| tag `usage_count` | `tags` | trigger | |
| `view_count`, `watch_time_seconds` | `video_stats` | Redis `INCR`, flushed in batches every ~30 s | a trigger per view would make one hot row the bottleneck |
| unique viewers | `video_daily_analytics` | Redis HyperLogLog, written at day close | not additive across days |
| unread notifications | Redis counter, PG capped `COUNT` fallback | app | badge needs only "0…99+" |
| playlist size, my watch-later size | none — `COUNT(*)` | | small, index-backed sets |

**Keeping counters honest:** triggers change the counter in the *same transaction* as the source row, so they cannot
diverge through the API. Decrements use `GREATEST(x-1, 0)` so a drifted counter never aborts a user action;
`CHECK (… >= 0)` is the last line of defence. A nightly drift query (see `db/queries/examples.sql`) plus
`recount_video_counters()`, `recount_creator_counters()`, `recount_comment_counters()`, `recount_tag_usage()` repair drift.
**When a single row becomes hot** (a viral video gets hundreds of likes/second) move that counter to Redis and flush
like views; nothing else changes.

### Views and analytics (brief §14, §27, §28)
```
player beacon ─► Redis INCR + HLL ─► (every ~30 s) batch UPDATE video_stats + upsert video_daily_analytics
              └► raw event row ───► video_views (monthly partitions, kept ~90 days)
nightly/intraday: video_daily_analytics ─► creator_daily_analytics, creator_daily_breakdown
Studio/trending read ONLY the daily rollups.
```
* `video_views` is `PARTITION BY RANGE (created_at)` by month: retention is `DROP` of an old partition (instant, no bloat).
  `ensure_monthly_partitions('video_views', 3)` must run monthly (the migration creates current + 3 months; alert if
  the next month's partition is missing — inserts into a missing range fail, so view-event writes must never block playback).
* Rollup writes are additive upserts (`ON CONFLICT … DO UPDATE SET views = views + EXCLUDED.views`). Make the *job* idempotent
  (store the last processed window) rather than the row.
* **Move to a dedicated analytics store** (ClickHouse / TimescaleDB / BigQuery) when raw events exceed roughly
  1 B rows/month or dashboards slow down. The write path already goes through an event record, so that is a change in one
  worker, not in the API.
* `ip_hash` = HMAC(daily-rotating salt, ip): cannot be reversed and cannot track a person across days.

### Search (brief §32)
`video_search(video_id, document tsvector)` is a denormalised document (title **A**, tags **B**, creator name **C**,
description **D**) with a GIN index, refreshed by `refresh_video_search(video_id)` on publish/edit/tag change (and a
periodic job for renamed creators). The `simple` text-search config is used so Bengali/Hindi/etc. work; switch to
per-language configs if you need stemming. Autocomplete and channel/playlist search use `pg_trgm`.
`video_search` is exactly what an OpenSearch/Meilisearch indexer needs: read rows where `updated_at > last_sync`.
Move to OpenSearch when p95 search latency exceeds ~200 ms on ~2 M+ documents, or when you need typo tolerance/facets at scale.

### Comments
One self-referencing table. Composite FKs guarantee that a reply belongs to the **same video** as its parent and that
`video_creator_id` equals the real creator, without triggers. A small trigger keeps threads **one level deep**
(replies target top-level comments only) — this matches the frontend and keeps queries simple. `video_creator_id` is a
deliberate, FK-protected denormalisation: the Studio inbox (`comments on MY videos`) would otherwise join every video.
Pagination is keyset-based. "Top" sorts by `like_count`; at very large scale consider a time-decayed score.

### Playlists
`UNIQUE (playlist_id, position) DEFERRABLE INITIALLY DEFERRED` lets a reorder be one `UPDATE … FROM unnest(…) WITH ORDINALITY`
without temporary positions. Append uses `MAX(position)+1`; a concurrent append hits the unique constraint → retry.

### Safety (reports, moderation, audit)
* **Evidence first:** target FKs are `ON DELETE RESTRICT`, so reported content/users cannot be hard-deleted until the
  reports are archived on purpose.
* `reports_open_*_once` partial unique indexes give the frontend its "already reported" (HTTP 409). Anonymous reporters
  (`reporter_user_id IS NULL`) cannot be de-duplicated by the database — rate-limit them in Redis.
* A trigger sets `priority = 0` for UNDERAGE / NON_CONSENSUAL / ILLEGAL so they jump the moderator queue.
* `moderation_actions` and `audit_logs` are **append-only** (UPDATE/DELETE/TRUNCATE revoked from the app and worker roles).
  Moderation state changes (`videos.moderation_status`, `reports.status`) must be written in the **same transaction** as the
  action row and an audit row — the database cannot do that for you.
* `audit_logs` rejects obvious secret keys at top level of `metadata` and stores only an HMAC of the IP.

### Privacy by design
Sessions keep a truncated IP prefix; views keep a salted hash; no documents or birth dates are stored for age checks
(`age_verifications` holds the outcome and an opaque provider reference only); search history is capped and expires;
password hashes must look like argon2id/bcrypt (a CHECK rejects anything else, so plaintext cannot be inserted by accident).
Verification tokens are HMAC-hashed and consumed with one atomic `UPDATE … WHERE consumed_at IS NULL AND expires_at > now()`.

## 4. Pre-SQL review (brief "Important implementation rule")
1. **Requirements analysed** → section 1. 2. **Entities** → 37 tables. 3. **Removed:** payments/live (deferred), private
messages, `CREATOR` report target, surrogate ids that would never be read. 4. **Relationships** → `ERD.md`.
5. **Indexes** designed per query (section 1). Every FK child column that can be hit by a cascade/restrict has a
supporting index; indexes that exist only for that reason are commented as "FK". 6. **Normalisation / duplicated data:**
the only denormalised values are `comments.video_creator_id` (FK-protected), `users.is_verified` (cached, set when the
token is consumed), the counters above, and `video_search.document` — each with a stated reason. 7. **Circular
dependencies:** none (the only cycle-like reference is `comments → comments`, intentional). 8. **Scalability risks found
and handled:** hot counter rows (views → Redis), unbounded event table (partitions), NEW_VIDEO notification fan-out
(do not fan out for large audiences — compute on read), cascade scans (FK indexes), search GIN churn (separate table).
9. **Security/privacy** → section 3 and `OPERATIONS.md`.

## 5. Things the application MUST still enforce
The DB enforces limits as a safety net; the API must validate first and return friendly errors.
* SHORT ≤ 60 s / ≤ 200 MiB, LONG > 60 s / ≤ 5 GiB, max 1080p (short side ≤ 1080, long side ≤ 1920).
  A violating `UPDATE` raises `check_violation` (`videos_type_matches_duration`, `videos_size_limits`,
  `video_metadata_max_1080p`) — mark the video `FAILED` with a reason instead of letting the error surface.
* Max 15 tags/video, max 5000 playlist items, per-user upload concurrency, rate limits (Redis).
* Authorisation (who may edit/delete/moderate) — never trust the client.

## 6. Frontend alignment items
* `src/config/upload.ts` allows 8 GB; the database limit is **5 GiB** (and 200 MiB for shorts). Update the frontend.
* Notification ids are `bigint`; the API should return them as strings (JSON-safe).
* Frontend comment "held" = `UNDER_REVIEW`; "Approve" = set `VISIBLE`.
* Frontend `Block creator` = `blocked_users`; `Not interested` = `not_interested`.
* `/messages` and the Live page have no tables yet (see deferred designs); return an empty list for `GET /live`.
