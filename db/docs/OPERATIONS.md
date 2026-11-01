# REDXTUBE database operations

## 1. Migrations
* Tool: **golang-migrate** (`db/migrations/NNNNNN_name.up.sql` / `.down.sql`). Run from CI/deploy with the
  `redxtube_migrator` role, **never from the API process**.
* Rules: one logical change per migration; never edit an applied migration; every `up` has a `down`.
  Down migrations are for dev/CI. In production prefer **roll-forward** fixes (data migrations are not reversible).
* The initial migrations create indexes together with the (empty) tables. For **live tables** add indexes in their own
  migration file containing a single statement `CREATE INDEX CONCURRENTLY …` (it cannot run inside a transaction).
* Safe schema changes on big tables: add nullable column → backfill in batches → add `CHECK … NOT VALID` →
  `VALIDATE CONSTRAINT` → set `NOT NULL`. Set `lock_timeout` before DDL.
* CI: run `make check`, then apply all migrations up, seed, and down on a throw-away database.

## 2. Roles and connection security
| Role | Used by | Can |
|---|---|---|
| `redxtube_migrator` | CI/deploy | owns objects, runs DDL |
| `redxtube_app` | API | DML on all tables; **no** UPDATE/DELETE on `audit_logs`, `moderation_actions` |
| `redxtube_worker` | FFmpeg/analytics workers | DML except credential/identity tables; `SELECT` on a few `users` columns |
| `redxtube_maint` | scheduled jobs | `EXECUTE` the purge/partition functions only |
| `redxtube_readonly` | BI / replicas | `SELECT`, no secrets (no sessions, tokens, password hashes) |

Checklist (tick before launch):
- [ ] Roles created by `db/bootstrap/roles.sql`; **passwords come from the secret manager**, rotated, never in git/images.
- [ ] `password_encryption = scram-sha-256`; `pg_hba.conf` has **no `trust`**; superuser reachable only from a bastion/VPN.
- [ ] TLS on every connection (`sslmode=verify-full`); database in a private subnet, security group allows only API/worker/maintenance hosts.
- [ ] Disk, WAL and **backups encrypted** (KMS); backup bucket in another account/region with object lock.
- [ ] All queries parameterised (pgx `$1…`); no string-built SQL; dynamic sort/filter columns come from an allow-list.
- [ ] Per-role `statement_timeout`, `lock_timeout`, `idle_in_transaction_session_timeout` (set by the bootstrap).
- [ ] Connection pooling (PgBouncer in transaction mode, or pgxpool sized ≈ 2–4 × cores); with PgBouncer use pgx `QueryExecModeCacheDescribe` or simple protocol.
- [ ] `log_min_duration_statement`, `log_lock_waits`, `pg_stat_statements` on; `pgaudit` (role/DDL) if you need audit trails of DB admins.
- [ ] Password hashing parameters (argon2id) and the HMAC **pepper** live outside the database.
- [ ] Application code never logs SQL parameters containing tokens, hashes or emails.
- [ ] Separate non-prod database; **no production data in dev/staging** (seed data only).
- [ ] Row-level security is not used; authorisation is in the API. Add RLS only if you introduce direct DB access for third parties.

## 3. Data retention
All purge functions delete at most `limit` rows per call; the scheduler loops until it returns 0 (keeps locks and WAL small).
Policies below are **defaults for you and your counsel to confirm** (legal holds, CSAM/NCII evidence rules and local law may
require longer retention of specific records outside this database).

| Data | Keep | Mechanism |
|---|---|---|
| Sessions | until expiry/revoke + 7 days | `purge_expired_sessions()` hourly |
| Verification / reset tokens | until expiry/use + 1 day | `purge_expired_verification_tokens()` hourly |
| Search history | 90 days, ≤ 100 per user | `purge_old_search_history()` daily (+ trim on insert) |
| Notifications | read: 90 days; any: 365 days | `purge_old_notifications()` daily |
| Raw `video_views` | ~90 days | `drop_old_partitions('video_views', 3)` monthly |
| Daily analytics rollups | 25 months, then monthly roll-up | scheduled job (add when needed) |
| `audit_logs` | 2 years (extend for security/regulatory needs) | `purge_old_audit_logs()` daily; archive to cold storage first |
| Reports / moderation actions | per counsel (suggest ≥ 3 years after resolution) | archive job, then delete |
| Watch history | until the user clears it; trim to the newest 10 000 | on write / daily |
| Upload sessions | 7 days after terminal state | daily; S3 lifecycle rule also aborts incomplete multipart uploads after 3 days |
| Processing jobs | completed 30 days, failed 90 days | daily |
| Deleted accounts | 30-day grace, then purge | flow below |
| IP data | prefix in sessions (session lifetime), salted hash in views (≤ 90 days) | automatic with the above |

### Account deletion flow
1. **Request:** `UPDATE users SET status='DELETED', deleted_at=now()`; revoke sessions; in the same transaction soft-delete
   the user's videos/comments (`deleted_at`/status) so nothing remains public (counter triggers update automatically).
2. **Grace period (30 days):** the user can cancel (restore status and `deleted_at = NULL`).
3. **Purge job:** skip users that still have open/unarchived `reports` or `moderation_actions` pointing at them
   (`ON DELETE RESTRICT` enforces this). Otherwise: a worker deletes the user's objects in storage (originals, HLS,
   thumbnails, avatar); then `DELETE FROM videos WHERE creator_id = $1;` then `DELETE FROM users WHERE id = $1;` (cascades
   profile, settings, sessions, history, reactions, playlists, comments…). `audit_logs.actor_user_id` becomes NULL.
   Finally anonymise recent events: `UPDATE video_views SET user_id = NULL WHERE user_id = $1 AND created_at > now() - interval '90 days';`
   (partition-pruned).

## 4. Backup and recovery
**Definitions.** *RPO* (recovery point objective) = how much recent data you can afford to lose.
*RTO* (recovery time objective) = how long the service may be down before it is restored.

| Scenario | RPO target | RTO target | How |
|---|---|---|---|
| Instance / AZ failure | ≈ 0 (synchronous standby) | ≤ 5 min | managed Multi-AZ or Patroni with a synchronous replica, automatic failover |
| Bad deploy / accidental delete / corruption | ≤ 5 min | ≤ 1–2 h (size dependent) | **point-in-time recovery** from base backup + archived WAL |
| Region loss / ransomware | ≤ 15 min | ≤ 4 h | cross-region copy of backups/WAL (object-lock bucket), restore into a new cluster |

These are **targets to validate, not guarantees**: your real RTO is whatever the monthly restore test measures.

Strategy
* **Continuous WAL archiving** (`archive_mode=on`, `archive_timeout=60s`) + **base backups**: weekly full, daily differential/incremental, with **pgBackRest** or **WAL-G** (or the managed service's PITR).
* **Retention:** PITR window 14 days; daily backups 35 days; monthly backups 12 months (adjust to policy/cost).
* **Encryption + isolation:** backups encrypted with KMS, stored in a different account/region, object lock/immutability, restricted delete rights.
* **Restore testing (the part most teams skip):** monthly automated job restores the latest backup to a scratch instance, replays WAL to a random timestamp, then checks `schema_migrations` version, row counts of core tables, a sample of FK integrity queries, and `make check`-equivalent invariants; it records the elapsed time (= measured RTO) and alerts on failure. Do a full disaster-recovery drill quarterly.
* **Object storage is separate:** videos live in object storage, not in these backups. Enable bucket versioning, lifecycle rules and cross-region replication; a database restore cannot bring files back.
* **Redis** is not a source of truth; it is rebuilt from PostgreSQL (counters, trending) after loss.

## 5. Scaling roadmap (do not build ahead of need)
| Signal | Do this |
|---|---|
| Single instance comfortable (thousands of users, ≤ ~1 M videos, ~10 M comments) | PostgreSQL primary + PgBouncer + Redis cache. Nothing else. |
| Primary CPU > 60 % sustained, or heavy read traffic | add **read replicas** (route feeds, search, comments, Studio reads); keep writes, auth and read-your-writes flows on the primary |
| `notifications`, `audit_logs`, or daily-analytics tables > ~100–200 M rows | **range-partition** them by month (same pattern as `video_views`) |
| Raw events > ~1 B/month, or dashboards slow | **separate analytics store** (ClickHouse/Timescale/BigQuery); keep only rollups in PostgreSQL |
| Search p95 > ~200 ms at ~2 M+ documents, or you need typo tolerance/facets | **OpenSearch/Meilisearch** fed from `video_search` |
| Several consumers need the same events (analytics, recommendations, notifications) at ~5 k events/s+ | **event streaming** (Kafka/Redpanda/NATS JetStream) between API and consumers |
| Warehouse/BI queries hurt production | nightly export to a **data warehouse**; BI never touches the primary |
| Subscriptions feed slow for users with many subscriptions | per-user timeline in Redis (fan-out on write for small creators, on read for large ones) |
| Primary write throughput exhausted after all of the above | vertical scale first; **sharding** is a last resort (shard by `creator_id`/`user_id`) and rarely needed |

## 6. Redis vs PostgreSQL
| PostgreSQL (source of truth) | Redis (rebuildable, TTL'd) |
|---|---|
| users, profiles, settings, age-verification outcomes | session lookup cache (≈ 60 s) keyed by token hash — the session row stays in PostgreSQL |
| videos, variants, thumbnails, captions, upload sessions | rate limiting (login, upload init, comments, anonymous reports) |
| comments, reactions, subscriptions, playlists, history | **job queues** (FFmpeg/transcode), pub/sub for realtime topics |
| notifications, reports, moderation actions, audit log | **short-lived counters**: view `INCR`, unique-viewer HyperLogLog, unread badge |
| daily analytics aggregates, final counters | trending / home / category caches, suggestion caches |
| processing-job records (durable state) | OTP/verification attempt throttling, idempotency keys, temporary upload state |

Rules: Redis may be flushed at any time without data loss that matters; anything the user would be upset to lose is written to PostgreSQL first. The job queue is a delivery mechanism — the **durable record** is `video_processing_jobs` (workers can also claim jobs straight from PostgreSQL with `FOR UPDATE SKIP LOCKED` on `video_jobs_queue`).

## 7. Monitoring
Alert on: replication lag; missing next-month `video_views` partition; non-empty counter-drift report; `video_jobs_stale` rows;
long-running/idle-in-transaction sessions; autovacuum lag on `video_stats`/`notifications`; disk and WAL growth; slow
queries (`pg_stat_statements`); failed restore test; sudden growth of `reports_queue`.

## 8. Scheduled jobs (suggested)
| Frequency | Job |
|---|---|
| every 30 s | flush Redis view/watch-time counters → `video_stats` + `video_daily_analytics` |
| every 5 min | roll up today's `creator_daily_*`; re-queue stale jobs (`heartbeat_at`); expire upload sessions (and abort S3 multipart uploads) |
| hourly | purge sessions and verification tokens |
| daily | purge search history/notifications/audit logs/old jobs; counter drift check + `recount_*`; refresh `video_search` for renamed creators |
| monthly | `ensure_monthly_partitions('video_views', 3)`, `drop_old_partitions('video_views', 3)`; restore test |

## 9. Uploads, storage and background jobs (B5 / B8 / B9)
- **Storage**: set `STORAGE_*` (see `backend/.env.example`). Keep the bucket private. Public delivery (CDN) should expose only `thumbs/`, `captions/` and `hls/`, never `uploads/`.
- **Bucket CORS** (the browser PUTs parts directly): allow your web origin, methods `PUT GET HEAD`, allowed headers `*`, **expose header `ETag`**. Without the ETag header the browser cannot complete multipart uploads.
- **Lifecycle rules**: add a rule that aborts incomplete multipart uploads after 2 days (the API janitor aborts expired sessions too, this is a safety net).
- **Janitor** (inside every API instance, once a minute): closes expired upload sessions and aborts their multipart uploads; calls `expire_user_suspensions()`. Both are safe to run on many instances at once (`SKIP LOCKED`).
- **Realtime** needs Redis pub/sub on the same Redis as the rate limiter (channel `redx:rt`).
- **Not built**: the FFmpeg worker (B6). Until it exists, uploaded videos stay `PROCESSING`.
- **Hash blocklist**: the media worker must write `videos.content_sha256` (SHA-256 of the original) after hashing; the trigger does the rest.
