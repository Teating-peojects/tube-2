# Phase 5 / F — testing

## Layers
| Layer | Where | Runs with | What it proves |
|---|---|---|---|
| Unit | `internal/service`, `config`, `storage`, `metrics`, `handler/health_test.go` | `go test ./...` | logic with fakes: validation, limits, retry/cleanup decisions, counters |
| Repository + service on real PostgreSQL | `handler/upload_retry_integration_test.go`, `upload_cleanup_integration_test.go` | `TEST_*` variables (PostgreSQL 16 + Redis) | the SQL, constraints, races, backoff (stubbed object storage) |
| **HTTP through the real router** | `handler/upload_http_integration_test.go`, `upload_http_failures_integration_test.go` | same | CSRF, sessions, verified-email guard, JSON bodies and status codes, the full lifecycle |
| S3 client against a real S3 server | `internal/storage/s3_integration_test.go` | `TEST_S3_ENDPOINT` (+ key, secret, bucket) | the hand-written SigV4 client, multipart, presigned URLs, abort semantics |
| Frontend | `web/src/**/*.test.ts(x)` | `npm test` | uploader, `useUpload`, UploadProgress (Phase E) |

The HTTP tests use `handler/memstore_test.go`, an in-memory store with S3 multipart semantics (part ETag = MD5, completion
checks ETags and assembles the object, a closed upload answers 404 `NoSuchUpload`). The test plays the browser: it PUTs each
part to the presigned URL it was given, then calls `complete`.

## HTTP scenarios (17 tests incl. subtests)
Access control (anonymous 401, unverified 403, cross-site request refused, strangers get 404 on status/complete/retry/abort) ·
init validation (extension incl. `.avi` refused, size, per-user cap of 5 open uploads) · happy path (3 parts, object bytes and
content type, exactly one processing job, repeated complete is idempotent, finished upload not retryable) · refused file (not a
video -> `invalid_file`, upload closed, file deleted, sweeper frees all three prefixes) · a short part is caught and can be sent
again · wrong part lists (`validation_failed` / `parts_mismatch`, upload stays open) · storage outage while verifying (5xx
`storage_unavailable`, not poisoned) · retry of an expired upload (same ids, new URLs, old URL answers 4xx, budget 3, window 7 days,
finalized by the sweeper) · cancel of an open and of a completed upload · slot freed by a cancel · `/ready` with storage.

## Bugs these tests found (all fixed in this phase)
1. **`AbortMultipart` failed on `404 NoSuchUpload`.** Aborting an upload that was already aborted or completed is an error on
   S3/MinIO, and the client treated only object-not-found as "already gone". The Phase D sweeper aborts what the janitor already
   aborted, so every expired session would have failed forever (backoff, never "cleaned"). Found by running the client against
   moto; the earlier stubs never returned `NoSuchUpload`. Fix: `storage.AbortMultipart` treats it as success; unit test with
   a fake S3 plus the gated S3 test.
2. **Cancelling a completed, unpublished upload answered 500** (`upload_sessions_completed` CHECK: `completed_at` may exist only
   on a COMPLETED session). A Phase B5 bug, never reached by the stub-based tests. Fix: the abort query clears `completed_at`.
3. **A cancelled completed upload left its processing job `QUEUED`**, so the future worker would have processed a deleted video.
   Fix: the discard transaction marks queued jobs `FAILED` ("upload cancelled"). A job already `PROCESSING` cannot be stopped
   here: **the B6 worker must skip videos that are deleted or no longer PROCESSING.**
4. **CI scripts** (Phase A findings F1, F2): `db/scripts/*.sh` and `security/scripts/promote.sh` were committed without the
   executable bit, and `test-db.sh` printed `applying ...` lines into the file CI sources. Fixed (`100755`, migrations log to stderr;
   verified by running it exactly as CI does under `bash -e`).

Observation: `size_mismatch` is unreachable through the HTTP API, because the part layout check (`parts_mismatch`) rejects a short
or long part first; it remains as a second line of defence and is covered by unit tests.

## S3 client results (storage/baseline/phaseF-s3-matrix.txt)
Run against two independent emulators; **real MinIO / AWS S3 were not available (NOT RUN)**.
| Test | s3rver 3.7.1 | moto 5.2.5 | Note |
|---|---|---|---|
| Ping (HEAD bucket), missing bucket fails | pass | pass | |
| Multipart lifecycle: presigned part PUT, ETag, ListParts, complete, Head, GetRange, Download, Delete | fail (no ListParts: 405) | **pass** | first check of the hand-written client against a server it was not written alongside |
| Complete refuses a wrong ETag | fail (accepts it) | **pass** | |
| Abort is idempotent | fail (no abort: 405) | **pass** (after fix 1) | |
| Part of an aborted upload answers 404 | fail | fail (500) | emulator gaps; expected on S3/MinIO, **unverified** |
| Expired presigned URL answers 403 | **pass** | fail (no expiry check) | proves the assumption the frontend recovery relies on |
| Wrong secret key answers 403 | fail (not enforced) | fail (not enforced) | **unverified**; MinIO enforces signatures. SigV4 correctness itself is therefore not proven by an emulator |
| DeletePrefix, PutBytes, Head | pass | pass | |
Every test passes on at least one server except the two marked unverified. The CI job `s3` (separate commit `ci: add s3 job`; pushing a workflow file needs the token's *Workflows* permission) runs all eight against MinIO
(`minio/minio:latest`, bucket created with `curl --aws-sigv4`); **that job has never been run**, so it may need adjustment.

## How to run
`go test -race ./...` (needs the `TEST_*` variables: `db/scripts/test-db.sh`). S3 tests: see the README.
