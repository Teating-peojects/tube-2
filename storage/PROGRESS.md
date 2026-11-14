# Phase 5 progress

| Phase | Scope | Status | Verified by |
|---|---|---|---|
| A | Audit and baseline | **DONE** | Real runs: Go 200/200 with PostgreSQL 16 + Redis 7 + `-race`; web 63/63; dbcheck OK |
| B | Config and validation | **DONE** (code) — MinIO/compose not verified | Go 206/206 with PostgreSQL 16 + Redis 7 + `-race`; web 64/64, tsc and lint clean (`baseline/phaseB-*`) |
| C | Retry and state | **DONE** (backend, real PostgreSQL) — frontend wiring is Phase E | Go 213/213 with PostgreSQL 16 + Redis 7 + `-race`; migration 000011 up/down/up; dbcheck OK (11 migrations); mutation-checked (`baseline/phaseC-*`) |
| D | Cleanup and reliability | Not started | — |
| E | Frontend | Not started | — |
| F | Integration tests | Not started | — |
| G | OpenAPI, docs, report | Not started | — |

Overall Phase 5: **PARTIALLY COMPLETE** (see `docs/PHASE_5_AUDIT.md`).

## Decisions from the owner (2026-11-14)
- No creator approval step: any signed-in, email-verified user may upload; the first upload makes them a creator
  (existing behaviour, unchanged). Uploads still never auto-publish: publish needs READY + consent declaration.
- `.avi` is dropped. Allowed formats: `.mp4 .m4v .mov .mkv .webm` (all have a magic-byte check: `ftyp` or EBML).
- Per-request age gate on upload routes: not requested, not changed (age is verified at registration).

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
