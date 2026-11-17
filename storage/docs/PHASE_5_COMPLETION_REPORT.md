# Phase 5 completion report — Video Upload & Storage

Date: 2026-11-14 · Repository: `Teating-peojects/tube-2` · Work area: `storage/` (documents, evidence) plus the code in `backend/`, `db/`, `web/`

## Verdict
**PARTIALLY COMPLETE.** The upload path (init, direct-to-storage multipart upload, complete, status, retry, cancel, assets,
draft metadata, consent-gated publish, cleanup, health, counters, OpenAPI, docs) is built and verified on a real PostgreSQL
and in unit and component tests. It has **not** been run against real MinIO/S3, in a real browser (no Playwright), or on
GitHub Actions' new S3 job. **Production readiness is not claimed.** There is no malware/CSAM scanning (B10) and the
processing worker (B6) does not exist, so no upload can become `READY` and be published in a real deployment yet.

Labels: **COMPLETE** = implemented and verified by a test or a real run named below. **PARTIAL** = implemented, verification
or scope incomplete (said how). **BLOCKED** = cannot be done before another phase. **NOT IMPLEMENTED**.

## Requirements (phase document -> status)
| # | Requirement | Status | Evidence / note |
|---|---|---|---|
| 5.1 | Init: auth, validation, draft, presigned part URLs | COMPLETE | `TestUploadHTTPInitValidation`, `...HappyPath`; formats `.mp4 .m4v .mov .mkv .webm` (`.avi` dropped by decision) |
| 5.2 | Complete: ownership, verification, idempotent, one job | COMPLETE | `...HappyPath`, `...RecoverableCompleteFailures`, `...RejectedFiles` |
| 5.3 | Status endpoint | COMPLETE | HTTP tests; `retryable` for expired uploads |
| 5.4 | Cancel, safe cleanup | COMPLETE | `TestUploadHTTPCancel` (found and fixed a 500) |
| 5.5 | Retry endpoint, limit, audit | COMPLETE | `TestUploadHTTPRetryOfAnExpiredUpload`, real-DB retry test; scope: expired uploads only, by design |
| 6 | Frontend: drop zone, progress, speed/ETA, cancel, retry, states, offline, mobile | PARTIAL | Vitest 108/108, mutation-checked; **no real browser, no Playwright e2e, mobile layout not seen** |
| 7.1-7.3 | Allow-list, MIME, signature, size, limits, auth, rate limits, CSRF | COMPLETE | HTTP + unit tests; if Redis is down the limited endpoints fail closed (code only) |
| 7.2 | Maximum duration | BLOCKED | needs ffprobe (B6) |
| 7.2 | SHA-256 checksum | NOT IMPLEMENTED | computed by the worker (B6); ETags are verified now |
| 8 | Consent, no auto-publish | COMPLETE | `TestUploadHTTPPublishNeverHappensByItself` (+ mutation checks) |
| 8 | Age eligibility on every upload request | PARTIAL | verified at registration only; decision pending |
| 8 | Creator approval | COMPLETE (decision) | none: the first upload makes the user a creator, as before |
| 9 | Database design | COMPLETE | migrations 000010-000012, up/down/up on PostgreSQL 16, `dbcheck` OK (12 migrations, 39 tables) |
| 10 | Storage abstraction, multipart, presign, abort | PARTIAL | complete lifecycle passes on two emulators (moto, s3rver) and in tests with an S3-like store; **real MinIO/S3 NOT RUN; SigV4 correctness not proven** (emulators do not enforce signatures) |
| 11 | MinIO in Compose, private bucket, env validation | PARTIAL | compose profile + bucket init exist, env validated (unit tests); `docker compose` **not run** |
| 12 | Cleanup: expiry, abort, orphans, retention | PARTIAL | database-driven sweeper with backoff, retention config, verified on PostgreSQL; **orphan multiparts with no row need a bucket lifecycle rule that is documented, not configured** |
| 13 | OpenAPI | COMPLETE | `storage/openapi.yaml`, valid OpenAPI 3.0.3; Go contract test keeps routes and error codes in step with the code |
| 14 | Tests: unit, PostgreSQL integration, frontend | COMPLETE | see below |
| 14 | Tests: MinIO integration | PARTIAL | 8 tests written; run on moto/s3rver only; CI job `s3` prepared, **never run, not yet pushed** (token lacks the Workflows permission) |
| 14 | Tests: browser e2e | NOT RUN | no browsers in the sandbox |
| 15 | Observability | PARTIAL | counters logged every 10 minutes, optional storage readiness check; **no scrape endpoint** |
| 17 | Hand-off to the processing worker | COMPLETE on the upload side | one `QUEUED` job per completed video; the worker itself is B6 (NOT IMPLEMENTED) |
| 18 | Documentation | COMPLETE | `storage/docs/` (below) |

## Delivered (6 commits on `main` before this one: 75 files, +4,473 / -89; Phase G adds the OpenAPI file, docs, tests below)
- **Backend:** configurable limits and formats (B); `POST /upload/{id}/retry`, retry budget and window (C); storage sweeper, optional storage health, in-process counters, `storage.Ping` (D); contract-preserving fixes (F). `backend/internal/{service,repository,handler,config,storage,metrics,model}`, `cmd/api/main.go`, compose, `.env.example`, `docs/api-contract.md`.
- **Database:** `000011_upload_retry`, `000012_upload_cleanup` (reversible); scripts made executable and `test-db.sh` fixed.
- **Frontend:** resilient uploader (stall detection, offline pause/resume, no retry of 4xx), `useUpload` rewrite (cancel race fixed, session recovery, per-code messages, auto-resume), UploadProgress, `retryUpload`, missing-ETag diagnosis.
- **Docs and evidence:** `storage/` (this folder).

## Tests actually run (final state)
| Suite | Result |
|---|---|
| Go, `-race`, real PostgreSQL 16 + Redis 7 | **253 PASS**, 0 FAIL, 8 SKIP (the S3 tests, which need `TEST_S3_ENDPOINT`); was 200 at the start |
| Go S3 client tests | run on s3rver and moto: every test passes on at least one, two are unverified anywhere (404 for the parts of an aborted upload, 403 for a wrong secret): `baseline/phaseF-s3-matrix.txt` |
| `db/tools/dbcheck.py` | OK: 12 migrations, 39 tables, 30 enums, 29 functions, 82 indexes |
| Web `vitest` | **108/108** (was 63); `tsc` clean; `next lint` clean (2 old warnings) |
| OpenAPI | `openapi-spec-validator`: valid 3.0.3 |
| Mutation checks | 27 deliberate breakages of guards (SQL, service, frontend, contract); each made a test fail, each restored version passed |
| **Not run** | real MinIO/S3/R2, `docker compose`, Playwright e2e, `next build` (Google Fonts unreachable in the sandbox), GitHub Actions |

## Defects found by the verification (all fixed)
1. `AbortMultipart` failed on `404 NoSuchUpload`: the sweeper would have retried every expired session forever (found with moto).
2. Cancelling a completed, unpublished upload returned **500** (`completed_at` CHECK).
3. A cancelled completed upload kept its processing job `QUEUED`.
4. Cancelling while the session was being created left that session open and flipped the UI back to "uploading".
5. An expired presigned link caused four pointless retries per part and left the old session open.
6. CI scripts without the executable bit; `test-db.sh` polluted its own output.
7. A missing `ETag` (bucket CORS) looped silently; it is now a clear, non-retried error.
8. Documentation drift: `.avi` left in the contract, migration counts.

## Known issues and incomplete work
- No malware/CSAM scanning, no content decode (signature only); the hash-blocklist trigger was not re-verified.
- Age is not re-checked per upload request.
- A retried upload re-sends every part; a reload cannot resume an upload (the file handle is lost).
- `size_mismatch` is unreachable over HTTP (the part-layout check fires first); kept as a second line of defence.
- No scrape endpoint for the counters; no per-IP limits on upload endpoints.
- The `s3` CI job is committed locally (branch `ci-s3-job`, patch `ci-s3-job.patch`), not on GitHub.
- `README`/`DESIGN.md` still quote older migration and table counts (F3).

## Infrastructure and configuration still needed
Private bucket with CORS that **exposes `ETag`**; storage credentials; `STORAGE_*` / `UPLOAD_*` variables; a lifecycle rule for
incomplete multipart uploads; migrations 000010-000012; `NEXT_PUBLIC_USE_MOCK=false` and the API URL. Details: `PHASE_5_DEPLOYMENT.md`.
Before launch: run `TestS3*` against the real storage, one full upload from a real browser, decide the scanning (B10) and age-gate questions.

## Next phase: B6 - processing worker (FFmpeg/HLS)
Integration requirements (from this phase): consume the `QUEUED` row in `video_processing_jobs` (`SKIP LOCKED`); skip videos that
are deleted or not `PROCESSING`; read `uploads/<videoId>/original.<ext>`; write outputs under a new prefix and teach
`freeStorage` about it; set `READY` with `video_type`, `duration_seconds`, `hls_master_key` (CHECK `videos_ready_requirements`);
compute `content_sha256`; enforce the maximum duration; report progress for `GET /upload/{id}/status`; sandbox ffmpeg (B10).
Then B7 (signed playback; the signer exists but is not wired).

## Evidence index (`storage/baseline/`)
`go-test-*`, `phaseB..G-go-test-integration-race.txt`, `web-*`, `phaseE/G-web-*`, `phaseC/D/F-dbcheck.txt`, `phaseF-s3-matrix.txt`,
`REPRODUCE.md` (how the sandbox runs differ from a normal machine).
