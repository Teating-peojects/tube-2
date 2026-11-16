# Phase 5 audit — Video Upload & Storage

Date: 2026-11-14. Method: read the code and docs, then ran the checks listed in `baseline/REPRODUCE.md`.
Labels: **VERIFIED** = a test or real run exercised it today. **IMPLEMENTED (unit)** = covered by unit tests with
mocks or an in-process fake S3 only. **PARTIAL**, **MISSING**, **BLOCKED**.

## 1. Context
The backend phase called B5 already implements most of Phase 5 under different names. The phase document
(written as a prompt) predates it. Existing contracts win (`instruction.txt` rule 6, `backend/docs/api-contract.md`
says the frontend is the source of truth), so this audit maps the document onto what exists.

### Naming map (document -> existing code; existing names are kept)
| Phase 5 document | Existing |
|---|---|
| `POST /api/v1/uploads/init` | `POST /api/v1/upload/init` |
| `POST /uploads/{id}/complete` | `POST /upload/{id}/complete` |
| `GET /uploads/{id}` | `GET /upload/{id}/status` |
| `DELETE /uploads/{id}` | `DELETE /upload/{id}` |
| `POST /uploads/{id}/retry` | none |
| upload states PENDING/UPLOADING/VERIFYING/UPLOADED/FAILED/CANCELLED/EXPIRED | INITIATED/COMPLETING/COMPLETED/ABORTED/EXPIRED (`upload_status` enum) |
| `S3_*` env variables | `STORAGE_*` (ENDPOINT, PUBLIC_ENDPOINT, REGION, ACCESS_KEY, SECRET_KEY, BUCKET, PATH_STYLE) |
| `MAX_VIDEO_SIZE_BYTES` etc. | Go constants (see section 2) |

## 2. Requirement matrix
| # | Requirement (doc section) | Status | Notes / evidence |
|---|---|---|---|
| 5.1 | Init: auth, validation, upload id, random key, DB record, presigned URLs | IMPLEMENTED (unit) | `TestInitValidation`, `TestInitPartsAndKey`, `TestInitLimitsAndDisabled`. Key is `uploads/<uuidv7>/original<ext>` (unpredictable video id) |
| 5.2 | Complete: ownership, exists, size, no double-complete, transactional | IMPLEMENTED (unit) | `TestCompleteHappyPath/IsIdempotent/PartsMismatch/...`. Verifies the client part list against `ListParts` (sizes + ETags) |
| 5.3 | Status endpoint, ownership checked | IMPLEMENTED (unit) | `TestStatusMapping`; progress comes from `videos`/jobs state |
| 5.4 | Cancel, safe cleanup | IMPLEMENTED (unit) | `TestAbort` |
| 5.5 | Retry endpoint, retry limit, audit trail | **MISSING** | Only `RevertComplete` (transient storage failure lets the client call complete again). No `retry_count`, no limit, no endpoint |
| 6 | Frontend upload UI (drop zone, progress, cancel, retry, speed/ETA, states) | PARTIAL / NOT VERIFIED | `web/src/features/upload/*`, `lib/uploader.ts`. Unit tests pass (uploader 6, UploadProgress 4, TagInput 3, config 3). Not checked yet: speed/ETA, `beforeunload` guard, e2e (not run) |
| 7.1-7.2 | Format allowlist, MIME, signature, size | IMPLEMENTED (unit) | Extensions `.mp4 .mov .mkv .webm .avi` (document lists no `.avi`), MIME must be `video/*` or octet-stream, signature check for ftyp / EBML / RIFF-AVI, 5 GiB cap |
| 7.2 | Max duration | **BLOCKED** | No ffprobe until the B6 worker exists. Must be documented, not faked |
| 7.2 | Checksum | **MISSING** | Only multipart ETags. `videos.content_sha256` exists (migration 000010) but nothing sets it until the worker hashes the file |
| 7.2 | Configurable limits | **PARTIAL** | `UploadPartSize` (16 MiB), `MaxUploadBytes` (5 GiB), `MaxOpenUploads`, rate limits (10 inits/h, 30 completes/h ...) are constants in `service/upload.go`. Session TTL and max retries not configurable |
| 7.3 | Auth, RBAC, rate limits, concurrency cap, CSRF, CORS, body limit | IMPLEMENTED (unit) | `RequireVerified` (signed-in + verified email), Redis limiter, `MaxOpenUploads`, global CSRF origin check, strict CORS, 1 MiB body limit |
| 8 | Server-enforced age eligibility for upload | **PARTIAL — needs decision** | Upload routes are registered without the `RequireAge` gate (`server.go` passes `gate` to Catalog/Social only). Age is enforced at registration (account gets a VERIFIED `age_verifications` row). Plausible but not per-request; confirm this satisfies the document |
| 8 | Creator eligibility | **PARTIAL — needs decision** | Any verified user can upload; the first init flips `users.is_creator = true` (`qEnsureCreator`). No creator approval step |
| 8 | Consent/rights declaration, no auto-publish | IMPLEMENTED (unit) | `upload_consents` (append-only), `TestPublishRequiresConsent`. Uploads stay `PROCESSING`; `Publish` needs `READY` + consent. Community-guidelines acceptance is not stored separately (only `ConsentVersion`) |
| 9 | DB design | IMPLEMENTED / VERIFIED | `upload_sessions`, `videos`, `video_processing_jobs` (unique active job per video, SKIP LOCKED index), 10 migrations applied cleanly on PostgreSQL 16 |
| 10 | Multipart: part URLs, abort, ListParts, persisted provider upload id | IMPLEMENTED (unit) | `storage` package hand-written SigV4; tested only against an in-process fake S3, **not real MinIO/S3** |
| 10 | Configurable multipart threshold / part size / max parts | PARTIAL | Constants; always multipart (no single-PUT path) |
| 11 | MinIO in Compose, private bucket, volume, bucket init | IMPLEMENTED — NOT VERIFIED | Compose profile `storage`; ports bound to 127.0.0.1; bucket created by `minio-init`. No health check on the `minio` service, `latest` image tags, not run here |
| 11 | Env validation at startup | IMPLEMENTED (unit) | `config_test.go`; partial `STORAGE_*` config is rejected |
| 12 | Cleanup of expired sessions + multipart abort | IMPLEMENTED (unit) | `janitor` (1 min tick) -> `ExpireStale`; `TestExpireStaleAbortsMultipart`; each expired row goes to exactly one API instance |
| 12 | Cleanup of orphan temp objects, cancelled/failed objects past retention, configurable retention | **MISSING / not confirmed** | No retention settings; no sweep for objects without a session row |
| 13 | OpenAPI | **MISSING** | Only `backend/docs/api-contract.md` |
| 14 | Backend unit tests | VERIFIED | service/upload 21 tests, storage 7; whole suite 200 PASS with `-race` |
| 14 | Integration tests (PostgreSQL) | PARTIAL | Handler integration tests cover auth, catalog, social. **No upload HTTP-level test** |
| 14 | Integration tests (MinIO) | **MISSING / BLOCKED here** | No MinIO test exists; no MinIO in this sandbox or in CI |
| 14 | Frontend tests | PARTIAL | See row 6 |
| 15 | Metrics, storage health check | **MISSING** | Logs only; `/ready` checks postgres and redis, not storage |
| 17 | Hand-off to worker (B6) | IMPLEMENTED — worker MISSING | Upload creates a job row; nothing consumes it (no `cmd/worker`, no FFmpeg). Out of scope for Phase 5 |
| 18 | Documentation deliverables | MISSING -> this folder | `docs/`, PROGRESS.md, REMAINING_TASKS.md did not exist |

## 3. Findings from the real runs
- **F1 (verified; FIXED in Phase F)** `db/scripts/migrate.sh`, `db/scripts/test-db.sh` and `security/scripts/promote.sh` are committed
  with mode 100644. Running them as documented fails with `Permission denied` on Linux/macOS. The CI step
  `db/scripts/test-db.sh > ...` is affected the same way unless the runner applies the bit.
- **F2 (verified locally; FIXED in Phase F)** `migrate.sh` prints `applying ...` to stdout, and `test-db.sh`
  passes it through, so the captured `test-env.sh` contains non-export lines. `source`-ing it printed
  `applying: command not found` ten times. A runner that uses `bash -e` may abort there.
- **F3 (verified)** Documentation drift: `dbcheck.py` reports 10 migrations / 39 tables; README, DESIGN.md and
  db-adaptation.md say 9 / 37.
- **F4** `next build` needs network access to Google Fonts (`next/font`). It failed in the sandbox for that reason only.
- **F5** All 200 Go tests passed with PostgreSQL 16 + Redis 7, including `-race`. This verifies the existing B5 logic
  against a real database, not against real object storage.

## 4. Verdict (as of this audit)
Phase 5 status: **PARTIALLY COMPLETE**. Core upload flow is implemented and unit-verified; retry, configurable
limits, full cleanup, metrics, storage health, OpenAPI, upload integration tests and MinIO verification are open.

## 5. Status changes after Phase B (2026-11-14)
| Row | Before | Now |
|---|---|---|
| 7.1-7.2 allowlist | `.mp4 .mov .mkv .webm .avi` | `.mp4 .m4v .mov .mkv .webm`; AVI signature removed; covered by `TestInitAcceptsAllowedFormats`, `TestInitValidation`, `TestValidContainer` |
| 7.2 configurable limits | PARTIAL (constants) | IMPLEMENTED (unit): max size, part size, open uploads and session TTL via `UPLOAD_*`; validated at startup (`TestUploadLimitsRejectBadValues`) and applied by the service (`TestInitUsesConfiguredLimits`). Still constants: rate limits, presign TTL for assets, thumbnail/caption limits |
| 8 creator eligibility | needs decision | Decided: no approval, automatic (unchanged behaviour) |
| 8 age gate per request | needs decision | Unchanged; open, low priority |
| 11 env validation | IMPLEMENTED (unit) | extended to the four upload variables |
Phase 5 overall: still **PARTIALLY COMPLETE** (retry, cleanup, metrics, integration tests, OpenAPI, MinIO verification open).

## 6. Status changes after Phase C (2026-11-14)
| Row | Before | Now |
|---|---|---|
| 5.5 retry endpoint, limit, audit | **MISSING** | IMPLEMENTED and VERIFIED against real PostgreSQL: `POST /upload/{id}/retry`, `retry_count` budget (`UPLOAD_MAX_RETRIES`, default 3), audit `upload.retry`; scope = expired uploads only |
| 5.3 status | IMPLEMENTED (unit) | adds `retryable` for expired uploads with budget left |
| 9 DB design | VERIFIED | migration 000011 (reversible); 11 migrations, 39 tables |
| 14 integration tests (PostgreSQL) | PARTIAL | upload retry is covered at service + repository level on real PostgreSQL (stubbed storage, not HTTP-level); init/complete/status/abort HTTP tests still open (Phase F) |
| 6 frontend retry | PARTIAL | backend ready; no retry button/client call yet (Phase E) |
Phase 5 overall: still **PARTIALLY COMPLETE** (cleanup/metrics/health, frontend, HTTP + MinIO integration tests, OpenAPI open).

## 7. Status changes after Phase D (2026-11-14)
| Row | Before | Now |
|---|---|---|
| 12 cleanup of expired sessions + multipart abort | IMPLEMENTED (unit) | unchanged, plus a retried safety net with backoff |
| 12 orphan / cancelled / failed objects, configurable retention | **MISSING / not confirmed** | IMPLEMENTED and VERIFIED on real PostgreSQL (stubbed storage): `SweepStorage`, `UPLOAD_CLEANUP_DELAY`, `UPLOAD_EXPIRED_RETENTION`. **Gap:** multipart uploads that never got a database row need a bucket lifecycle rule (documented, not configured) |
| 15 storage health check | **MISSING** | IMPLEMENTED (unit): optional `/ready` dependency, reported as `degraded`, never 503 |
| 15 metrics | **MISSING** | **PARTIAL:** in-process counters + a periodic log line; no scrape endpoint |
| 9 DB design | VERIFIED | migration 000012 (reversible): 12 migrations, 39 tables, 82 indexes |
| 14 integration tests (PostgreSQL) | PARTIAL | + cleanup flow at service + repository level; upload HTTP-level tests still open (Phase F) |
Phase 5 overall: still **PARTIALLY COMPLETE** (frontend wiring, HTTP + MinIO integration tests, OpenAPI, metrics endpoint, lifecycle rule open).

## 8. Status changes after Phase E (2026-11-14)
| Row | Before | Now |
|---|---|---|
| 6 frontend: cancel, retry, speed/ETA, states | PARTIAL / NOT VERIFIED | VERIFIED at unit level (Vitest 105/105): cancel race fixed, retry wired to `POST /upload/:id/retry` with fallback, offline pause/resume, stall detection, per-code error messages, `beforeunload` |
| 6 mobile layout | not checked | action buttons 44 px; stacked layout (progress above the form) already in place. **Not verified in a browser** (no Playwright here) |
| 14 frontend tests | PARTIAL | 105 tests incl. a 20-test `useUpload` suite and fake-XHR tests |
| 14 e2e | not run | still **NOT RUN** (no browsers in the sandbox); no e2e spec was added because it could not be verified and CI runs e2e |
Phase 5 overall: still **PARTIALLY COMPLETE** (HTTP-level and MinIO integration tests, OpenAPI, e2e, metrics endpoint, lifecycle rule open).

## 9. Status changes after Phase F (2026-11-14)
| Row | Before | Now |
|---|---|---|
| 14 integration tests (PostgreSQL), upload HTTP level | PARTIAL | VERIFIED: 17 tests through the real router on PostgreSQL 16 (auth, CSRF, validation, lifecycle, refusals, recovery, retry, cancel, `/ready`) |
| 14 integration tests (MinIO) | **MISSING / BLOCKED** | PARTIAL: 8 S3 tests written; verified on two emulators (s3rver, moto), **not on MinIO/AWS S3**; CI job `s3` prepared as a separate commit (needs the *Workflows* token permission to push), never run |
| 10 multipart / SigV4 client | IMPLEMENTED (unit) | multipart lifecycle now checked against an independent S3 implementation (moto); signature enforcement unproven (neither emulator enforces it) |
| 12 cleanup | VERIFIED on stubs | a real defect fixed: `NoSuchUpload` on abort would have made the sweeper retry forever |
| 5.4 cancel | IMPLEMENTED (unit) | a 500 on cancelling a completed upload fixed; its queued job is now closed |
| F1, F2 (CI scripts) | open | fixed |
Phase 5 overall: **PARTIALLY COMPLETE**. Open: OpenAPI, run on real MinIO, Playwright e2e, metrics endpoint, bucket lifecycle rule, B6/B7.
