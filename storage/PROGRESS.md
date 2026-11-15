# Phase 5 progress

| Phase | Scope | Status | Verified by |
|---|---|---|---|
| A | Audit and baseline | **DONE** | Real runs: Go 200/200 with PostgreSQL 16 + Redis 7 + `-race`; web 63/63; dbcheck OK |
| B | Config and validation | **DONE** (code) — MinIO/compose not verified | Go 206/206 with PostgreSQL 16 + Redis 7 + `-race`; web 64/64, tsc and lint clean (`baseline/phaseB-*`) |
| C | Retry and state | **DONE** (backend, real PostgreSQL) — frontend wiring is Phase E | Go 213/213 with PostgreSQL 16 + Redis 7 + `-race`; migration 000011 up/down/up; dbcheck OK (11 migrations); mutation-checked (`baseline/phaseC-*`) |
| D | Cleanup and reliability | **DONE** (code) — metrics have no scrape endpoint; real MinIO not verified | Go 226/226 with PostgreSQL 16 + Redis 7 + `-race`; migration 000012 up/down/up; dbcheck OK (12 migrations); mutation-checked (`baseline/phaseD-*`) |
| E | Frontend | **DONE** (unit level) — e2e/real browser not run | Vitest 105/105 (+41), tsc and lint clean, mutation-checked (`baseline/phaseE-*`) |
| F | Integration tests | Not started | — |
| G | OpenAPI, docs, report | Not started | — |

Overall Phase 5: **PARTIALLY COMPLETE** (see `docs/PHASE_5_AUDIT.md`).

## Decisions from the owner (2026-11-14)
- No creator approval step: any signed-in, email-verified user may upload; the first upload makes them a creator
  (existing behaviour, unchanged). Uploads still never auto-publish: publish needs READY + consent declaration.
- `.avi` is dropped. Allowed formats: `.mp4 .m4v .mov .mkv .webm` (all have a magic-byte check: `ftyp` or EBML).
- Per-request age gate on upload routes: not requested, not changed (age is verified at registration).

## Phase E — what changed
Details: `docs/PHASE_5_FRONTEND.md`. Only `web/` (and docs) changed; no backend change.
- `lib/uploader.ts`: stall watchdog in `xhrPut` (60 s without progress = network error), `browserOnline`/`waitForOnline`, offline gate in `uploadParts` (waits without using attempts, resumes by itself), no retry for 4xx other than 408/429.
- `features/upload/useUpload.ts` (rewritten): generation guard fixes the cancel-while-preparing race; one automatic recovery through `POST /upload/:id/retry`, with fallback to abort-then-new-session; error classification (`classify`) with per-code messages and `canRetry`; auto-resume on `online`; `beforeunload` sets `returnValue`; reset never aborts a finished upload.
- `UploadProgress.tsx`: offline state, "Choose another file" when retrying cannot help, 44 px buttons. `UploadView.tsx` passes the new props.
- `services/upload.ts` + mock + `config/endpoints.ts` + `types`: `retryUpload`, `endpoints.upload.retry`, `ServerUploadStatus.retryable`.
- Tests (+41): uploader (offline gate, 4xx vs transient, `waitForOnline`, `xhrPut` with a fake XHR: ETag, status, stall, progress re-arming, abort), `UploadProgress` (+4), `useUpload` (20: happy path, cancel races, recovery paths, server refusals, connection loss, `beforeunload`, reset).
- `web/README.md` updated.

Behaviour notes / risks:
- A reopened (expired) upload re-sends all parts: a 5 GiB upload restarts from zero. A reload still cannot resume (no file handle).
- Not run: Playwright e2e, `next build`, a real browser against a real API/bucket.

## Phase D — what changed
Details and operating notes: `docs/PHASE_5_CLEANUP.md`.
- `db/migrations/000012_upload_cleanup.{up,down}.sql`: `storage_cleaned_at`, `cleanup_attempts`, `cleanup_next_at` + two partial indexes (reversible).
- `repository/upload.go`: `ClosedUploads`, `MarkCleaned`, `CleanupFailed` (backoff), `FinalizeExpired`; `ReviveUpload` resets the bookkeeping.
- `service/upload.go`: `SweepStorage`/`freeStorage` (EXPIRED frees the multipart only, ABORTED also deletes the video's objects, PUBLISHED is never touched), retry window (`ExpiredRetention`), `Counter` + counters at every lifecycle step.
- `internal/metrics` (new): in-process counters; the janitor logs them every 10 minutes.
- `storage.Client.Ping` (HEAD bucket); `handler.Health.Optional`: `/ready` reports storage as `degraded` but stays 200; wired in `cmd/api/main.go`; the janitor runs the sweep every minute.
- Config: `UPLOAD_EXPIRED_RETENTION` (1h..2160h, default 168h), `UPLOAD_CLEANUP_DELAY` (1m..24h, default 10m); `docker-compose.yml`, `.env.example`, `README.md` (`/ready`), `api-contract.md` updated.
- Tests (+13): storage Ping, metrics (2, one concurrent), health (optional vs required), config, service (7: sweep outcomes, failure/starvation, published and malformed-id guards, configured times, retry window, counters), and `upload_cleanup_integration_test.go` on the real schema. Mutation checks: removing any of the EXPIRED-keeps-key rule, the failure backoff, the published guard, the revive reset, or the provider-id guard (in `MarkCleaned` and `CleanupFailed`) makes a test fail.

Behaviour notes / risks:
- Existing closed sessions (before this migration) have `storage_cleaned_at IS NULL`, so the first sweeps work through that backlog (50 per minute per instance); every step is idempotent. Old ABORTED sessions get their `uploads/`, `thumbs/`, `captions/` prefixes deleted: intended, but it is a one-time bulk delete in production.
- The retry window is new behaviour: an upload that expired more than 7 days ago can no longer be retried (it was unlimited in Phase C).
- Not verified: real MinIO/S3, `docker compose`, bucket lifecycle rules (documented only).

## Phase C — what changed
Retry means: an upload whose session **expired** before it was finished (status `EXPIRED`) is reopened with a fresh
multipart upload; the draft video keeps its id and metadata. Cancelled or rejected uploads (`ABORTED`) are final.

- `db/migrations/000011_upload_retry.{up,down}.sql`: `upload_sessions.retry_count`, `last_retry_at`, CHECK 0..100. Reversible
  (applied, reverted, re-applied and reverted twice on PostgreSQL 16; the API role reads the new columns).
- `backend/internal/repository/upload.go`: `ReviveUpload` — one transaction: session EXPIRED -> INITIATED (new provider
  upload id, new deadline, `retry_count + 1`) and its video FAILED/deleted -> UPLOADING, plus the audit row. Returns false and
  changes nothing if another caller already revived it, the budget is spent, or the video is moderated/published.
- `backend/internal/service/upload.go`: `Retry`, `presignParts` (shared with Init), `UploadLimits.MaxRetries`, `Status`
  now returns `retryable: true` for an expired upload with budget left.
- `backend/internal/handler/upload.go`: `POST /api/v1/upload/{id}/retry` -> 200, same body as init.
- `backend/internal/config/config.go`, `cmd/api/main.go`, `docker-compose.yml`, `.env.example`: `UPLOAD_MAX_RETRIES` (0..10, default 3, 0 = off).
- `backend/internal/model/studio.go`: `UploadStatus.retryable` (omitted when false, so existing responses are unchanged).
- `backend/docs/api-contract.md`: retry endpoint, `retryable`, new error codes; also fixes the `.avi` mention Phase B left behind.
- Tests: service unit (+6), config (extended), `internal/handler/upload_retry_integration_test.go` (real schema + real
  repository, stubbed object storage): lifecycle, ownership, budget, concurrent retries, the exact race window at the
  repository, cancel after retry, moderated video. Breaking the moderation guard or the budget guard in the SQL makes it fail.

Behaviour notes / risks:
- The browser must upload the **same file** again: size and part layout are fixed by the session. A different file ends in `422 parts_mismatch`/`size_mismatch` at complete.
- Errors: `409 upload_active`, `409 not_retryable`, `409 retry_limit`, `422 limit_reached` (open-upload cap), `429` rate limit (20 retries/hour), `502 storage_unavailable`, `404 upload_not_found` (also for other users' uploads).
- If the new multipart cannot be recorded (lost race, database error) it is aborted straight away. An old multipart whose abort failed in the janitor is not touched here (orphan sweep is Phase D).
- Not wired in the web app yet: no retry button or API client call (Phase E). Not verified: real object storage, `docker compose`.

## Phase B — what changed
Outside storage/ (these must compile into the API/web modules):
- `backend/internal/service/upload.go`: `UploadLimits` + `WithLimits`, limits read from the struct instead of
  constants (defaults identical), `.avi` removed, `.m4v` added, AVI signature case removed, size message states the
  configured maximum.
- `backend/internal/config/config.go`: `UploadConfig`, env `UPLOAD_MAX_BYTES` (1 MiB..5 GiB), `UPLOAD_PART_SIZE_BYTES`
  (5 MiB..1 GiB), `UPLOAD_MAX_OPEN` (1..50), `UPLOAD_SESSION_TTL` (1h..168h); invalid values fail at startup naming
  the variable. Bounds match the DB checks (declared size <= 5 GiB, part size >= 5 MiB).
- `backend/cmd/api/main.go` (wires the limits), `backend/docker-compose.yml` and `backend/.env.example` (the four variables).
- `web/src/config/upload.ts`: extension list.
- Tests: `service/upload_test.go` (+5, 2 edited), `config/config_test.go` (+2), `web/src/config/upload.test.ts` (+1).

Behaviour notes / risks:
- With defaults nothing changes except the allowlist. Presigned part URLs live for half the session TTL (12h by default, as before).
- An `.avi` upload started before this change and completed after it is rejected at `complete` (signature no longer
  accepted). Sessions live 24h, so the window is short; already-processed videos are unaffected.
- The frontend takes the part size from the init response, so a changed `UPLOAD_PART_SIZE_BYTES` needs no web change.
  The web `maxBytes` constant (5 GiB) is only a client-side hint; lowering `UPLOAD_MAX_BYTES` is enforced by the API.
- Not verified: `docker compose` passing the new variables, real MinIO.

## Not verifiable in this environment
Real MinIO/S3, `docker compose up`, Playwright e2e, `next build` (Google Fonts unreachable).
