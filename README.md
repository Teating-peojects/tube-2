# REDXTUBE

Monorepo for the REDXTUBE 18+ video platform.

```
web/        Next.js frontend (package `redxtube-web`)
backend/    Go REST API (stdlib net/http, pgx, Redis, Argon2id, Resend)
db/         PostgreSQL 16 schema: migrations, bootstrap, seeds, ops, docs, checks
security/   security design docs + staff/role tooling
```

Stack: Go 1.22, PostgreSQL 16 (pgx), Redis 7 (go-redis), Argon2id, Resend (email), FFmpeg worker + S3-compatible storage (later phases), Docker, Next.js 15.

Contracts and design:
- API contract: [backend/docs/api-contract.md](backend/docs/api-contract.md)
- Security design (sessions, age gate, rate limits, CSRF): [security/auth.md](security/auth.md)
- Database design, dictionary, ERD, operations: [db/docs/](db/docs)
- How the API uses the database: [db/docs/db-adaptation.md](db/docs/db-adaptation.md)

## Run (Docker)
```bash
cd backend
cp .env.example .env          # set the passwords and AUTH_SECRET (openssl rand -base64 48)
docker compose up --build     # postgres -> roles -> migrations (as redxtube_migrator) -> categories -> api
curl localhost:8080/ready     # {"checks":{"postgres":"ok","redis":"ok"},"status":"ok"}
```
Compose lives in `backend/` and uses the `redxtube` project name; it mounts `../db/*` read-only.

The API connects as the least-privilege `redxtube_app` role; migrations run as `redxtube_migrator`. Never run
migrations as a superuser (the API role would get no privileges). First build runs `go mod tidy`; commit the
generated `go.sum` afterwards (`make tidy`).

Frontend:
```bash
cd web
cp .env.example .env.local    # NEXT_PUBLIC_USE_MOCK=false, NEXT_PUBLIC_API_URL=http://localhost:8080
npm install
npm run dev                   # http://localhost:3000
```

## Sample data (development only)
`db/seeds/dev_seed.sql` (creators, videos, playlists). Reference data (categories) is **not** dev-only: it runs
automatically in Docker, and must be applied in every environment after the migrations.

```bash
cd backend
docker compose exec -T postgres psql -U redxtube_migrator -d redxtube < ../db/seeds/dev_seed.sql
```

## Run (without Docker)
```bash
# one-time: roles + database (superuser), then migrations as the migrator
psql "$SUPERUSER_URL" -v dbname=redxtube -f db/bootstrap/roles.sql     # then ALTER ROLE ... PASSWORD ...
DATABASE_URL=postgres://redxtube_migrator:...@localhost:5432/redxtube db/scripts/migrate.sh up
psql "$MIGRATOR_URL" -f db/seeds/reference_data.sql

cd backend
export DATABASE_URL=postgres://redxtube_app:...@localhost:5432/redxtube REDIS_URL=redis://:...@localhost:6379/0 CORS_ALLOWED_ORIGINS=http://localhost:3000
go run ./cmd/api
```

## Test
```bash
cd backend
go test ./...                 # unit tests; integration tests are skipped without the variables below
ADMIN_URL=postgres://postgres:secret@127.0.0.1:5432/postgres ../db/scripts/test-db.sh   # builds redxtube_test, prints the exports
go test -race ./...           # unit + integration (real PostgreSQL as redxtube_app, real Redis)
```
`TEST_ADMIN_DATABASE_URL` (superuser, fixtures and wipe), `TEST_DATABASE_URL` (the API's `redxtube_app` role) and
`TEST_REDIS_URL`; both database URLs must point at a database whose name ends in `_test` (the tests wipe it).

Static database checks (no server needed): `python3 db/tools/dbcheck.py`.

## Staff accounts
There is no API to create admins or moderators. Register normally, then:
```bash
DATABASE_URL=... security/scripts/promote.sh you@example.com ADMIN
```

## Layout
```
web/                        Next.js frontend (App Router, Tailwind, TanStack Query)
backend/
  cmd/api                   entrypoint (config -> db/redis -> server -> graceful shutdown)
  cmd/devpass               dev helper for password hashes
  internal/config           env-only configuration + validation
  internal/apperr           safe application errors (code, message, fields; cause is log-only)
  internal/httpx            JSON/error helpers, middleware (request id, logging, recover, CORS, headers, body limit)
  internal/auth             Argon2id, OTP/HMAC, signed age cookie, roles, client IP
  internal/ratelimit        Redis atomic counters
  internal/mail             Resend client, async queue with retries, email templates
  internal/middleware       session auth, role guards, age gate, CSRF origin check
  internal/handler          thin HTTP handlers
  internal/service          business rules (validation, routing, pagination, shaping)
  internal/repository       all SQL (pgx) against db/migrations, returns plain rows
  internal/model            JSON shapes returned to the frontend
  internal/pagination       opaque cursor codec
  internal/media            storage key -> CDN URL, quality badge
  internal/server           routes + middleware chain
  internal/db|cache         PostgreSQL pool / Redis client
  docs/api-contract.md      HTTP contract
  docker-compose.yml        local stack (project `redxtube`)
  Dockerfile, Makefile, .env.example
db/
  migrations                SQL migrations
  bootstrap                 one-time roles/database bootstrap
  seeds                     reference data + development seed
  ops                       operational SQL (purge/retention jobs)
  queries                   example/reporting queries
  deferred                  design-only SQL for later phases
  docs                      design, data dictionary, ERD, operations, adaptation notes
  scripts                   migrate.sh, test-db.sh
  tools                     dbcheck.py, gen_dictionary.py
security/
  auth.md                   auth/session/age-gate/rate-limit design
  scripts/promote.sh        grant staff roles (audited)
```
Handler -> Service -> Repository -> Database. No business logic in handlers.

## Status
| Phase | State |
|---|---|
| B0 Foundation | done: config, errors, middleware, /health, /ready |
| B1 Database | done: 9 migrations, 37 tables, roles, triggers verified on PostgreSQL 16; 4 package bugs fixed ([db/docs/db-adaptation.md](db/docs/db-adaptation.md)) |
| B2 Read APIs | done: feed, detail, search + suggestions, trending, shorts, channels, playlists, report reasons, sitemap (`/live` is empty: no live tables) |
| B3 Auth + age gate | done: [security/auth.md](security/auth.md) |
| B4 Social | done: reactions, subscriptions, comments (pin, replies, review mode), views/progress/history, watch later, liked, playlists, block / not interested, reports |
| B5 Upload | done: direct-to-storage multipart, server-side verification, drafts, thumbnails, captions, consent record, publish |
| B6 FFmpeg-HLS worker | **not built**: nothing moves an uploaded video from `PROCESSING` to `READY`, so a real upload cannot be published yet |
| B7 Playback (signed HLS) | not wired: `internal/playback` exists but is unused |
| B8 Notifications + Studio + realtime | done: inbox, unread count, Studio overview/analytics/videos/comments, WebSocket over Redis pub/sub |
| B9 Moderation + admin | done (API only, no UI): report queue, actions, restore, users, audit, hash blocklist, auto-restrict |
| B10 Hardening | partly: upload verification, consent, hash blocklist trigger, suspension expiry, CI for Go. Still open: worker sandboxing, malware/CSAM scanning integration, load tests |
