# Audit of `feat/media-worker` against the B6-B10 prompt

Audited commit: `3ae0deb` (branch `feat/media-worker`, 2026-11-19 21:55 UTC). Written 2026-11-19 on a separate branch; nothing on `feat/media-worker` or `main` was changed.
Labels: DONE (code and a test exist), PARTIAL, GAP (not found), NOT VERIFIED (could not be checked here).

## Correction of the earlier answer
The first status answer ("B6-B10 not started") looked at `main` only. `main` is still at the Phase-5 state; the work lives on `feat/media-worker`, which is far ahead:
`cmd/worker`, `internal/worker` (queue, probe, policy, ladder, FFmpeg, thumbnails, finalize, janitor, scanner hook, metrics), `internal/stream` (signed playback), processing API
(status, cancel, retry), `Dockerfile.worker`, a compose service, `docs/worker.md`, `docs/playback.md`, `docs/observability.md`, a k6 script, CI workflows.
My own branches `processing/b6-1-audit`, `b6-2-job-lifecycle`, `b6-3-inspection` were built in parallel on top of `main` without knowing this. See "What to do with my branches".

## What was run (sandbox: Go 1.22.2, PostgreSQL 16, Redis 7.0, ffmpeg 6.1.1)
- Checked out `feat/media-worker`, built the schema with **its own** migrations (`000013_processing_controls`), `go build`, `go vet`, `gofmt -l`: clean.
- `go test -race -count=1 ./...` for the whole backend: **323 PASS, 0 FAIL, 12 SKIP**. Two of the skips (OpenAPI contract tests) only need `storage/openapi.yaml` from a full checkout; run that way they **pass**
  (so 325 PASS, 10 SKIP). The remaining 10 need an S3 API (`TEST_S3_ENDPOINT`, `TEST_S3_BUCKET`), which is not available here: 8 S3 client tests, `TestWorkerSmoke` and `TestPlaybackEndToEnd`.
  **The branch's claim "tested end to end with a real ffmpeg player" was therefore not reproduced by me.**
- Static reading of `worker.md`, `playback.md`, `observability.md`, `probe.go`, `ffmpeg.go`, `proc_unix.go`, `policy.go`, `stream.go` and the test names. No mutation check was done on the branch's tests.

## Per sub-phase
| Sub-phase | Status | Evidence | Gap or difference |
|---|---|---|---|
| B6.1 audit and design record | PARTIAL | design and decisions are in `docs/worker.md` | no audit/ADR files, no `processing/PROGRESS.md` with real status; `processing/` holds only the prompt |
| B6.2 job lifecycle | DONE | PostgreSQL queue; tests `PGLeaseGuardAndStaleRequeue`, `PGReleaseKeepsAttempt`, `PGRetryBackoffThenFail`, `ManyWorkersNeverShareAJob`, `DuplicateJobsAndIllegalTransitionsAreRefused`, `ShutdownReleasesTheRunningJob`, `CancelKillsARunningJob` | adds `CANCELLED` to the `job_status` enum (PostgreSQL cannot drop enum values, so the migration is not cleanly reversible); no `processing_version`; no recorded evidence (mutation check) |
| B6.3 inspection and policy | PARTIAL | `ParseProbe`, `Policy` (`MaxStreams` 16, 1080p cap, FPS cap 120, duration cap 60 min, `PolicyIsConfigurableButNeverBeyondTheDatabase`), SAR kept in `Probe`, `attached_pic`, rotation test, blocklist check at METADATA | hardening gaps listed below |
| B6.4 transcode and HLS | DONE | ladder 360-1080 never upscaling, MPEG-TS 6 s segments, master and playlist validation, `BrokenEncodeIsNeverUploaded`, portrait/silent/rotated tests | NOT VERIFIED: whether outputs go to a unique temporary prefix and are switched atomically; no output versioning beyond the profile string `h264-aac-hls-v1` |
| B6.5 finalize, thumbnails, cleanup | DONE | `BlocklistGrowingWhileProcessingIsCaughtAtFinalize`, `FinalizeBlocklistHitBecomesASilentPermanentFailure`, `StoredObjectsAreVerified`, `CleanupOutputsKeepsTheOriginal`, `PGCleanupClaimsOnlyOldFailedVideosOnce`, `ProcessingNeverPublishes`, `ThumbnailInspectionAndBlackFrameAvoidance` | NOT VERIFIED: that the Phase-5 sweeper also covers abandoned processing outputs |
| B6.6 isolation | PARTIAL | process group kill (`proc_unix.go`), `PipelineWaitsInsteadOfFillingTheDisk`, `HLSArgsAreSafe`, `SanitizeDiagHidesPathsAndControlChars`, `Dockerfile.worker`, compose service | child processes inherit the full environment; no CPU/memory limits or `nice` found; `runFFmpeg` stderr buffer is unbounded; no hostile-media corpus test |
| B6.7 API and UI | PARTIAL | `GET /videos/{id}/processing` (owner or staff), cancel and retry with audit (`ProcessingCancelAuthorizationAndAudit`, `ProcessingRetry`), `FailureCodeReachesTheStatusQuery`, realtime topic `video:<id>`, web upload progress | I found no `VIDEO_READY`/`VIDEO_FAILED` notification in the worker code; NOT VERIFIED that the worker pushes realtime updates |
| B6.8 completion report | GAP | `docs/worker.md` only | no completion report with evidence and known limits |
| B7 signed HLS playback | PARTIAL | `internal/playback` (HMAC token `exp.sig`, 6 h) + `internal/stream` handler with a takedown gate (`TakedownStopsPlaybackWithinTheGateTTL`), `nosniff`, `no-store` for playlists, tokens never logged, hls.js player in the web app | token is a bearer of (video, expiry): no user/session binding found, no `POST /playback` session or playback-info endpoint, no authorization-matrix tests, no Range handling (not needed for MPEG-TS); `TestPlaybackEndToEnd` not run here |
| B8 notifications, studio, realtime | PARTIAL | inbox, studio, WebSocket over Redis pub/sub (README, existing tests) | processing events into notifications/studio not verified (see B6.7) |
| B9 moderation and admin | PARTIAL | report queue, actions, restore, audit, hash blocklist (`PGBlockedHashRemovesVideo`, `PGRemoveVideoAudits`), playback gate stops a removed video within the gate TTL | API only (no admin UI); no moderation/legal-hold procedure document; no pre-publication review gate (consistent with D1) |
| B10 hardening | PARTIAL | fail-closed exec scanner hook (`ExecScannerExitCodes`, `ExecScannerTimeoutKillsProcessGroup`, `PipelineScannerIsFailClosed`), Prometheus metrics (`WorkerMetricsCoverTheOperationalSignals`), OpenAPI generated and contract-tested, k6 script, CI workflows | no real scanner engine (vendor), load test never run (k6 absent here), container/seccomp review open (the README says so), no security review, ops/backup runbook or pre-production checklist document found |

## B6.3 hardening gaps (verified in `probe.go`, `ffmpeg.go`, `pipeline.go`)
1. `ffprobe`, `ffmpeg` and the scanner **inherit the worker's whole environment** (database and storage credentials). `cmd.Env` is never set.
2. `ProbeFile` has `-protocol_whitelist file` but **no demuxer whitelist** (`-format_whitelist`), and runs `cmd.Output()` with **unbounded stdout** and **no timeout of its own** (only the job context).
3. The local copy is named `original` + the uploaded extension, so a lying extension can steer ffprobe's demuxer choice; the path is passed without the `file:` prefix.
4. **Truncated files**: any ffprobe failure becomes `invalid_file`; a file with its index in front and its tail missing passes ffprobe and fails later in FFmpeg, where it is classified as transient (`ffmpeg_failed`) and retried until the attempts run out.
5. `runFFmpeg` keeps stderr in an unbounded `bytes.Buffer`.
These are the points my `inspect` package (branch `processing/b6-3-inspection`) already covers with tests; they can be ported to `internal/worker` as small, separately reviewable changes.

## Decisions D1-D8 against the branch
| | Decision taken on my branch | `feat/media-worker` | Result |
|---|---|---|---|
| D1 | owner publishes after READY | `ProcessingNeverPublishes`, owner publishes | consistent |
| D2 | MPEG-TS | `-hls_segment_type mpegts`, `seg_%05d.ts` | consistent |
| D3 | one HLS job per video | a chain METADATA -> THUMBNAIL -> TRANSCODE -> HLS | **differs**: recommend adopting the chain as built and updating the ADR |
| D8 | keep the original | `CleanupOutputsKeepsTheOriginal`, never deletes the original | consistent |
Product-visible policy values differ from my branch: duration cap 60 min (mine 4 h), FPS cap 120 (mine 240). Both keep the 1080p source limit that the database enforces.

## What to do with my branches
- `b6-1-audit`: keep as a record (the audit of `main`); its "NOT IMPLEMENTED" table is about `main` only.
- `b6-2-job-lifecycle`, `b6-3-inspection`: **do not merge.** They duplicate the queue and the probe, and their migration `000013_processing_lifecycle` collides with `000013_processing_controls` and with the enum approach.
- Reusable: the `inspect` hardening and its tests (see gaps above) and the lifecycle test ideas, to be re-applied on top of `feat/media-worker`.

## Proposed order of work (each a small branch created from the latest `feat/media-worker`, coordinated with whoever is committing there)
1. Subprocess hardening (gaps 1, 2, 3, 5): scrubbed environment, bounded output, own timeout, `file:` prefix, extension-less local name, demuxer whitelist; fake-binary tests.
2. Truncation detection (gap 4): box and segment checks, permanent `corrupt_input`, fixtures cut at several points.
3. Evidence: `processing/PROGRESS.md`, a real B6.8-style report, and a mutation pass over the branch's own tests.
4. B6.7/B8: confirm or add READY/FAILED notifications and realtime pushes.
5. B7: owner decision on token binding and a playback authorization-matrix test.
6. B9/B10: moderation and legal-hold procedure, security review, backup/restore and pre-production checklist, hostile-media corpus, run the load test where k6 exists.
