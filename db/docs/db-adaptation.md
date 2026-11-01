# Backend <-> your database package (db/docs, db/)

The backend was rebuilt on **your** schema (`db/migrations`, 9 migrations, 37 tables). Nothing of the earlier
prototype schema remains. `db/docs/` holds your DESIGN / DATA_DICTIONARY / ERD / OPERATIONS documents unchanged.

## What was verified against a real PostgreSQL 16 (your package had never been run)
| Check | Result |
|---|---|
| roles bootstrap (`db/bootstrap/roles.sql`), run twice | clean, idempotent |
| 9 migrations up -> down -> up | clean, no leftovers after full rollback |
| `db/ops/restore_smoke.sql` | passes |
| `db/tools/dbcheck.py` | OK |
| `db/queries/examples.sql` (45 statements, via PREPARE) | 44 ok, **1 bug fixed** (see below) |
| your Go `backend/internal/db/models` + `repo` | compile and vet clean |
| API integration tests (133) running as `redxtube_app` | all pass, so every query works under your real GRANTs |

## Problems found in the package (fixed in this copy, please port them to your repo)
1. **`db/seeds/dev_seed.sql` failed** at the playlist_videos insert: `ON CONFLICT DO NOTHING` cannot infer a
   DEFERRABLE unique constraint. Now `ON CONFLICT (playlist_id, video_id) DO NOTHING`.
2. **`db/queries/examples.sql` #44** (creator daily rollup) was missing `a.date` in `GROUP BY`.
3. **Migrations must run as `redxtube_migrator`, but your Makefile ran them as `postgres`.** `roles.sql` grants the API
   role its rights with `ALTER DEFAULT PRIVILEGES FOR ROLE redxtube_migrator`, which only covers objects that role
   creates. Run as a superuser, every API query fails with `permission denied for table users`. The Makefile/compose
   here run migrations as `redxtube_migrator` (`db/scripts/migrate.sh`, `db/scripts/test-db.sh`, `backend/docker-compose.yml`).
4. **Categories are reference data but exist only in the dev seed.** A fresh production database has none, and a
   video cannot be published without a category. Added `db/seeds/reference_data.sql` (idempotent); run it in every
   environment after the migrations (compose does, via the `reference-data` service).

## How the API maps onto the schema
| Frontend concept | Schema |
|---|---|
| user / channel | `users` (+ `user_profiles` and `user_settings`, created by triggers). Channel id = user id |
| email verified | `users.is_verified`; `status` PENDING -> ACTIVE on first verification |
| `isCreator` | `users.is_creator`; staff powers use `role` |
| session | `sessions.session_token_hash` (SHA-256 of the cookie), `ip_prefix` (/24 or /56), sliding `expires_at`, 90-day cap from `created_at` |
| email code / reset link | `verification_tokens`: HMAC(purpose, user, secret), `attempts/max_attempts`. Reset link = `<token id>.<secret>` |
| 18+ gate | signed cookie for visitors (no row); `age_verifications` row (VERIFIED, SELF_DECLARATION) per account at registration |
| audit | `audit_logs(actor_user_id, action, target_*, metadata{request_id, role}, ip_hash)`, HMAC of the IP only |
| video "public" | `status='PUBLISHED' AND visibility='PUBLIC' AND moderation_status='OK' AND deleted_at IS NULL`, creator ACTIVE |
| video counters | `video_stats` (triggers keep likes/dislikes/comments; the API updates `view_count`, `watch_time_seconds`) |
| shorts / videos | `video_type` SHORT / LONG |
| quality badge | `LEAST(video_metadata.width, height)`: >= 720 HD, >= 2160 4K |
| stream URL | `CDN_URL + hls_master_key` (B7 turns this into signed URLs) |
| search | `video_search.document` (title, description, tags, creator) + partial-title `ILIKE`; `refresh_video_search()` must run when a video's text changes (upload flow, B5) |
| comments | `comments` with `video_creator_id` (creator badge), status VISIBLE / UNDER_REVIEW (creator review mode) / DELETED. Counters and reply counts are trigger-maintained, so the API never touches them |
| views | `video_views` (partitioned, daily-rotating HMAC `ip_hash`, enums for device and source), `video_daily_analytics` |
| reports | `reports` with one FK column per target; a trigger sets `priority`; unique partial indexes give `409 already_reported` |
| notifications | rows are written at event time (new subscriber, comment, reply), gated by `user_settings`; read APIs and WebSocket come in B8 |
| blocking | `blocked_users`, `not_interested` filter feeds, trending, search, popular channels (not a channel's own page) |

## Deviations and open decisions (please decide)
- **Verified badge**: the schema has no "verified creator" flag (`is_verified` means *email verified*). The API shows
  the badge for `is_creator AND is_verified`, i.e. every creator with a confirmed email. A real badge needs a column
  (for example `user_profiles.is_verified_creator`).
- **No live streaming tables**: `/live`, `?type=live` and `kind=live` exist and return empty lists; `isLive` is false.
- **Anonymous age gate stores nothing** (your `age_verifications.user_id` is NOT NULL). The signed cookie is the proof.
- **UNLISTED videos** open by link but never appear in any list, including inside someone's playlist (playlist counts
  and thumbnails only consider PUBLIC videos and the viewer's own).
- **Report evidence**: your schema keeps evidence in place (targets are FK-protected) instead of snapshotting it, so
  nothing is copied at report time. Report `details` map to `reports.description`.
- **`view_count` is incremented per counted view** (one UPDATE on `video_stats`). Your `examples.sql` describes the
  better design (Redis INCR drained every ~30 s); switch when one video gets hot (B8).
- **Frontend upload limits** already match your constraints (LONG <= 5 GiB, SHORT <= 200 MiB). The API enforces the
  200 MiB limit for SHORT once the duration is known (B5).
