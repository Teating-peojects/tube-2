# Phase 5 implementation plan

Principles: extend, do not rewrite. Keep existing routes, enums and `STORAGE_*` names (mapping in the audit).
Every phase ends with real test runs and an update of `PROGRESS.md`. "Edits outside storage/" lists the only places
that must change in `backend/` or `web/` (they must compile into those modules).

## Phase A — Audit and baseline  (status: DONE)
- Output: this folder, audit, baseline runs and evidence (`baseline/`).
- Edits outside storage/: none.

## Phase B — Configuration and validation  (status: DONE, see PROGRESS.md)
- Move limits to config with validated defaults: part size, max video size, upload session TTL, max retries,
  max open uploads, presign expiry. Names follow `STORAGE_*` / `UPLOAD_*`; defaults equal today's constants so
  behaviour does not change unless set.
- Decide and document: `.avi` (keep or drop), MKV, max duration (BLOCKED until the worker), checksum (SHA-256 is
  computed by the worker; keep ETag verification here).
- Decide the age/creator questions from the audit (per-request age gate on upload routes? creator approval?).
- Edits outside storage/: `backend/internal/config`, `backend/internal/service/upload.go`, `backend/.env.example`.
- Tests: config validation, limits applied.

## Phase C — Retry and state
- Migration `000011` (reversible, non-destructive): retry counter and last-failure fields on `upload_sessions`.
- `POST /api/v1/upload/{id}/retry`: owner only, only for failed/expired-but-recoverable states, capped, audited,
  issues fresh presigned URLs without creating a second video row.
- Status response exposes a safe failure reason.
- Edits outside storage/: `db/migrations`, `backend/internal/{handler,service,repository}/upload*.go`, `docs/api-contract.md`.
- Tests: retry limit, wrong owner, wrong state, duplicate call, concurrent calls.

## Phase D — Cleanup and reliability
- Janitor extension: sweep orphan objects/multiparts with no live session, cancelled/failed objects past a
  configurable retention; idempotent, retryable, structured logs; never touches objects of COMPLETED/published videos.
- Storage connectivity check exposed without making `/ready` depend on it (optional dependency).
- Counters for init / success / failure / verification failure / expired / cleanup failure.
- Edits outside storage/: `backend/internal/service/upload.go`, `backend/cmd/api/main.go`, `handler/health.go`.

## Phase E — Frontend
- Verify and fill: cancel, retry, speed/ETA, `beforeunload` guard, network-loss handling, mobile layout.
- Edits outside storage/: `web/src/features/upload`, `web/src/lib/uploader.ts`, tests.

## Phase F — Integration tests
- Upload HTTP-level tests against real PostgreSQL (init -> complete -> status -> abort/retry) with a fake storage backend.
- MinIO integration test, gated by env vars, plus a CI job with a MinIO service container. Cannot be executed in
  this sandbox (no Docker/MinIO): it will be written, and its status reported as NOT RUN unless you run it.
- Cleanup and failed-upload recovery tests.

## Phase G — OpenAPI, docs, completion report
- `storage/openapi.yaml` for the upload endpoints (existing paths), plus the remaining `PHASE_5_*` documents
  (architecture, API, security, testing, deployment) and `PHASE_5_COMPLETION_REPORT.md` with honest labels.
- Housekeeping findings F1-F3 (exec bits, test-db.sh stdout, doc counts) are proposed as a separate small change.

## Out of scope (stays in B6/B7)
FFmpeg/HLS worker, ffprobe duration check, signed playback, moderation UI, malware/CSAM scanning integration.
Phase 5 only guarantees a clean hand-off: uploaded videos stay private and a processing job row exists.
