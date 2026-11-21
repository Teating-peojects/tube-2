# Phase 6 completion report: video processing and HLS

Spec: `xhamtube_phase_6_video_processing_hls.md`. Branch: `feat/media-worker` (PR #1).
**Overall status: PARTIALLY COMPLETE.** The spec allows COMPLETE only when every criterion in its section 24 is verified.
Eight of them are not fully verified (marked below), mostly because this was built and tested in an environment without Docker,
without a real S3/MinIO, and without a browser. **This report does not claim production readiness.**

Labels: COMPLETE = implemented and verified by a test or a run shown here; PARTIALLY COMPLETE = implemented, not fully
verified; NOT IMPLEMENTED. Test names are real; run them with `go test -run <name>` (see "Tests actually run").

## 1. Executive summary

A separate `cmd/worker` process turns an uploaded original into private HLS renditions (360p to 1080p by source size), five
thumbnail candidates and metadata, and moves the video from `PROCESSING` to `READY` (never `PUBLISHED`). It reuses the
existing PostgreSQL queue (`video_processing_jobs`, `SKIP LOCKED`), enums, storage client and signed-playback key layout. Creators
can see, and cancel, processing; moderators can retry a failed video; failures show a specific reason without leaking
internals; failed output is cleaned up; the worker emits Prometheus metrics. Every guard listed in section 14 was **broken on
purpose in a copy of the code and the matching test failed (16 of 16 mutants killed)**.

Not verified: Docker image/compose runtime, real object storage, a browser, load, real-world media variety, any CPU/RAM
figures. Not built: a malware/CSAM engine (a fail-closed hook exists), runtime decode-verification of segments, the
spec's temporary-prefix promotion (decision D5), moderator UI.

## 2. Phase 5 integration findings

Inspected **in code** (not every Phase 5 document): upload service/repository/handlers, status endpoint, studio list, migrations 1 to
12, config, `internal/playback`, storage client, janitor/sweeper, web upload flow. `storage/docs/PHASE_5_COMPLETION_REPORT.md`
was read for its hand-off requirements. Findings:

* Completing an upload queues one `METADATA` job and sets the video `PROCESSING` (`FinishComplete`). Phase 5's HTTP-level tests
  (`upload_http_integration_test.go`) already check that a completed upload has exactly one `QUEUED` job. **Added here:** a
  repository-level test that two deliveries of the completion queue one job and that exactly one worker can take it
  (`TestCompletingAnUploadTwiceQueuesOneJob`).
* **Gap fixed:** Phase 5's `freeStorage` deleted `uploads/`, `thumbs/` and `captions/` of an aborted video but not `hls/`, so
  cancelling or deleting a processed, never-published video would have left its renditions in storage forever. `hls/` is now
  in the list (`service/upload.go`, one line, test updated).
* The status endpoint counts four jobs for progress: kept (decision D2). The generic failure text is replaced by the worker's
  code-mapped reason (allow-list in `service/processing_messages.go`; `upload.go` changed by one line).
* Phase 5 renamed nothing the worker needed; no Phase 5 behaviour was changed except the two lines above.
* Two OpenAPI files now exist: `storage/openapi.yaml` (Phase 5, upload routes) and `backend/docs/openapi.json` (generated from
  all routes, including the new ones). They should be unified (section 17).

## 3. Existing functionality reused

Queue table and indexes, `job_status`/`video_status` enums, `video_variants`, `video_thumbnails`, `video_metadata`, the
`videos_ready_requirements` CHECK, the blocklist trigger on `content_sha256`, `audit_logs`, `notifications` (`VIDEO_READY`,
`VIDEO_FAILED`), the realtime bus, the storage client (`Download`, `Put`, `Head`, `DeletePrefix`), `internal/playback` key
layout, role middleware, rate limiter, `reasonBody`/`done` handler helpers, the admin audit transaction pattern.

## 4. Architecture and processing flow

```
upload completed (Phase 5) -> METADATA -> THUMBNAIL -> TRANSCODE -> HLS -> videos.status = READY (creator publishes later)
```

Each step completes its job and enqueues the next in **one transaction**. METADATA: download, SHA-256, scanner (optional),
ffprobe, policy check, `video_metadata`. THUMBNAIL: five frames, decode-checked. TRANSCODE: one HLS rendition at a time,
validated locally, uploaded, verified. HLS: master playlist validated, uploaded, verified, then the finalize transaction (video
re-checked, blocklist re-checked, `READY`, notification). Details and decisions: `PHASE_6_DECISIONS.md`.

## 5. Database migrations and state changes

`000013_processing_controls` (additive, reversible): `job_status += CANCELLED`; `videos.processing_profile text`;
`videos.outputs_cleaned_at timestamptz`; partial index `videos_failed_cleanup`. No data is rewritten. **Verified:** up, down, up,
down, up against a real database; static check `db/tools/dbcheck.py` passes (13 migrations). The enum value cannot be removed by
the down migration (PostgreSQL); jobs using it are folded into `FAILED`. State mapping to the spec's names: decision D1.

## 6. Job queue and worker implementation

Claim with `FOR UPDATE SKIP LOCKED` (tested with 2 and 10 concurrent claimers over 30 jobs: each job claimed exactly once);
heartbeat every 15 s; a job without a heartbeat for 2 min is re-queued (or failed when attempts are spent); retry with
exponential backoff (30 s doubling to 10 min, 3 attempts); every statement that completes or fails a job requires
`status = PROCESSING AND worker_id = me` (a zombie's late result is refused); SIGTERM gives running jobs a grace period, then
kills the process group and releases the job **without using an attempt**; lease loss (cancel) kills the running ffmpeg or
scanner. Concurrency is bounded by `WORKER_CONCURRENCY` (default 1) and, per ffmpeg, `FFMPEG_THREADS`.

## 7. FFprobe validation behaviour

ffprobe runs on the real file with `-protocol_whitelist file`. The container name reported by ffprobe must be MP4/MOV
(`mov,mp4,...`) or Matroska/WebM; anything else (an `.m3u8` renamed `.mp4`, concat files) is rejected before ffmpeg touches it
(`TestMetadataRejectsNonVideoAndBlockedHash`). Policy (all configurable, defaults shown): max 60 min, SHORT up to 60 s / 200 MiB,
LONG up to 5 GiB, short side <= 1080 and long side <= 1920, min side 16, <= 120 fps, <= 16 streams, codecs H.264/HEVC/VP8/VP9/AV1.
Limits the database also enforces can be lowered, never raised (refused at startup). Rotation flags and non-square pixels
(SAR) are applied to the stored and planned size. Every rejection is permanent, with a stable code and a creator-facing sentence.
Verified with real files made by ffmpeg, plus unit tests of every limit.

## 8. FFmpeg profiles and actual encoding settings

Profile `h264-aac-hls-v1` (recorded on each video): libx264, preset `veryfast`, High profile (level 3.1; 4.0 for 1080p), yuv420p,
`setsar=1`, bitrates 800/1400/2800/5000 kbps (maxrate 107%, bufsize 2x), AAC 128 kbps stereo 48 kHz, keyframes forced every 6 s
on a time grid (identical in every rendition), frame rate capped at 60, `-map_metadata -1 -map_chapters -1` (no GPS/device tags),
no subtitle/data streams. Commands are argument arrays: no shell. Renditions are chosen by the source's short side and never
upscale; below 360p, one rendition at native size. Configurable: ladder, preset, segment length, audio bitrate, thumbnail count
(a ladder change requires a new profile version).

## 9. HLS output structure and validation results

`hls/{videoId}/master.m3u8`, `hls/{videoId}/{360p|480p|720p|1080p}/index.m3u8`, `.../seg_NNNNN.ts` (MPEG-TS, 6 s, VOD).
Validated **before upload**: playlist well-formed and ended, every segment present, non-empty and named `seg_NNNNN.ts`, segment
lengths within the target, total within max(2 s, 10%) of the source, master lists every planned rendition with the right
RESOLUTION and a BANDWIDTH. Verified **after upload**: each object's stored size equals what was sent. Test evidence: 14 s 720p
source gives 3 renditions of 3 segments each with identical segment durations across renditions; a real ffmpeg decodes the full
stream through the signed URL; an injected missing segment is caught and nothing is uploaded (`TestBrokenEncodeIsNeverUploaded`).
**Not done at runtime:** decoding the produced segments (the tests do decode them with ffmpeg).

## 10. Thumbnail generation details

Five JPEGs at 10/30/50/70/90% of the duration, width <= 640 (aspect preserved, real size read from the decoded file), `-q:v 3`.
Each is decoded (a non-JPEG is rejected); the middle frame is the default primary unless it is black (mean luma < 20), then the
brightest candidate becomes primary. Uploaded immutable with a UUID key, verified by size, recorded in one transaction that also
replaces earlier generated thumbnails. No poster or animated preview.

## 11. Storage integration

Originals are read from `uploads/...` (key from `videos.original_storage_key`, size checked against the declared size). Outputs
go to the existing `hls/` and `thumbs/` prefixes (D4). Objects are never made public by the code; they are served only through
the signed stream handler, which refuses anything not `READY`/`PUBLISHED` or removed. The original is never deleted by the worker.
A janitor removes the partial `hls/` output of videos that stayed `FAILED` for 24 h. Verified against an S3 API (moto), **not**
against real S3/MinIO; the bucket's access policy is infrastructure and was not verified.

## 12. Security controls and isolation assumptions

Controls (tested): no shell; `-protocol_whitelist file`; container allow-list; limits; scratch-disk guard; process-group kill on
timeout, cancel and shutdown; error text sanitised and never stored (only `code: sentence`); strangers get `target_not_found`;
cancel/retry audited in the same transaction, rate limited; retry staff-only with a reason; blocklist checked twice;
fail-closed scanner hook (`SCANNER_COMMAND`); least-privilege database role `redxtube_worker` (no credentials, sessions or
identity data); processing never publishes. Assumptions **not verified**: the container runs unprivileged on a read-only root
filesystem with no capabilities and CPU/memory/PID limits (written in `docker-compose.yml`, never run); the worker network can
reach only PostgreSQL, Redis and the object store; ffmpeg is patched (it is a large attack surface fed with hostile files).

## 13. Frontend processing status integration

The upload screen shows the server's specific failure reason instead of a generic text, and offers **Cancel processing** while a
video is processing or encoding (409 means "already finished": the status refreshes). The creator-facing text comes from an
allow-list on the server. Verified by component and hook tests (jsdom). **Not verified in a browser.** The studio list was not
changed.

## 14. Tests actually run

| What | Result |
|---|---|
| `go vet ./...`, `gofmt -l .` | clean |
| `go test -race -count=1 ./...` (CI-equivalent, no S3 server) | **all packages ok**, 324 test functions (worker package: 54) |
| Worker smoke + signed-playback end-to-end with an S3 API (moto) | passed: real worker loop, PostgreSQL as `redxtube_worker`, ffmpeg decodes the stream, takedown stops playback |
| Mutation checks (16 guards broken in a copy) | **16 of 16 killed** (`baseline/phase6-mutation-checks.txt`) |
| Web `tsc --noEmit`, `vitest run` | clean; 118 tests passed |
| Migration 000013 up/down/up/down/up | passed on a real database |
| `Dockerfile.worker` build steps simulated (`CGO_ENABLED=0 go build -trimpath`) | static 10.9 MB binary; refuses to start without config; `healthcheck` exits 1 when stale |
| Secrets scan of tracked files and branch history | nothing found (one documented AWS example key in a signing test) |

**Not run:** `docker compose up` / image build; real S3 or MinIO; a browser; load or soak tests (`loadtest/k6-read.js` was
written, not run); real-world media variety (all fixtures are generated by ffmpeg); any fuzzing; GitHub CI for the last commits
(checked when pushed, see PR #1).

## 15. Docker/build verification

`Dockerfile.worker` and the compose `worker` service (read-only root, no capabilities, `no-new-privileges`, 256 PIDs, 2 CPUs,
2 GiB, 60 s stop grace, healthcheck) exist and the compose file parses. **They were not built or run here.** The build commands
were reproduced outside Docker (table above). Run `docker compose --profile storage up --build` before relying on them.

## 16. Known issues and limitations

1. Segment decodability is verified in tests, not at runtime. 2. No temporary-prefix promotion (D5): a crash can leave
unreferenced objects (not servable, cleaned later). 3. Cleanup race: a retry that starts in the same instant as the janitor
deleting that video's prefix could lose objects (needs a 24-hour-old failure; milliseconds wide). 4. Cancel latency is bounded
by the heartbeat (15 s default; 0.2 s in tests). 5. No malware/CSAM engine. 6. Anamorphic handling is covered by unit tests, not
by a real anamorphic file. 7. Timeout, max attempts, retry delay, HLS format and thumbnail quality are not configurable
(`PHASE_6_DECISIONS.md`). 8. Every step re-downloads the original unless the same worker continues. 9. Two OpenAPI specs. 10. The
moderator UI for retry does not exist (API only). 11. Phase 5 documents beyond the completion report were not read in full.

## 17. Remaining tasks

Build and run the worker image; run against real S3/MinIO; measure throughput and memory on real media; run the k6 script;
choose and wire a scanning engine; unify the OpenAPI files; a moderator screen for retry; decide D1 to D5 and D8 (OWNER items);
a browser pass over the upload screen.

## 18. Production resource requirements

**Estimates, not measurements.** Scratch disk per running job: about `2 x original + largest rendition + WORKER_MIN_FREE_MB`
(a 5 GB original: about 9 GB). Stored outputs: roughly 9.5 Mbit/s of ladder for a 1080p source, about 4.3 GB per hour of video, in
addition to the original. CPU: libx264 `veryfast` is CPU-bound; plan one worker container per 2 to 4 cores and measure
`worker_encode_seconds_per_media_minute` on staging (above 60 means slower than real time). Memory: the compose limit of 2 GiB is a
starting point, not a measured need. Database: one pool of `concurrency + 3` connections per worker.

## 19. Recovery and operational guidance

`backend/docs/worker-recovery.md` (failure scenarios, retry and cancel, cleanup, migration rollback) and
`backend/docs/worker.md`, `backend/docs/observability.md` (metrics and alerts). Crash recovery, shutdown, lease loss and retry
are tested; killing a real worker process with SIGKILL was simulated by ageing the heartbeat.

## 20. Recommended next phase

Phase 7 (secure HLS streaming and player). A signed-URL stream handler already exists on this branch (written against the
existing `internal/playback`) and was not compared against the Phase 7 document; do that first. Before it, run the worker once
in the real stack (compose with MinIO) so playback is tested on real output.

---

## Completion criteria (spec section 24)

| # | Criterion | Status | Evidence / note |
|---|---|---|---|
| 1 | Phase 5 inspected | PARTIALLY COMPLETE | code inspected, completion report read; other Phase 5 docs not read in full |
| 2 | Upload completion triggers one job | COMPLETE | `TestCompletingAnUploadTwiceQueuesOneJob` |
| 3 | Duplicate delivery safe | COMPLETE | `TestDuplicateJobsAndIllegalTransitionsAreRefused`, `TestManyWorkersNeverShareAJob`, lease-guard test, mutants |
| 4 | State transitions validated | COMPLETE | no single transition table; guarded SQL + DB constraints; illegal transitions tested |
| 5 | Starts and shuts down safely | COMPLETE | `TestShutdownReleasesTheRunningJob`, `cmd/worker` config tests; Docker SIGTERM not run |
| 6 | FFprobe inspects real media | COMPLETE | real generated files |
| 7 | Invalid/oversized rejected safely | COMPLETE | policy and disguised-file tests; no fuzzing |
| 8 | FFmpeg without shell interpolation | COMPLETE | argument arrays; injection test for the scanner path |
| 9 | CPU/memory/disk/timeout limits configured | PARTIALLY COMPLETE | timeout and disk guard tested; CPU/memory limits are in compose, never run |
| 10 | Renditions by source dimensions | COMPLETE | ladder tests, 720p/480p/portrait runs |
| 11 | Portrait and rotated media | COMPLETE | `TestPortraitAndSilentSource`, `TestRotatedPhoneVideoIsEncodedUpright` |
| 12 | Silent video | COMPLETE | `TestPortraitAndSilentSource` |
| 13 | Master playlist generated | COMPLETE | validated and decoded by ffmpeg |
| 14 | Playlists and segments validated | PARTIALLY COMPLETE | structure/duration/existence at runtime; decode only in tests |
| 15 | Thumbnails generated and validated | COMPLETE | decode, size, black-frame tests |
| 16 | Outputs stored privately | PARTIALLY COMPLETE | code never makes objects public and serving is gated; bucket policy unverified |
| 17 | Stored objects verified before completion | COMPLETE | size verification; mutant killed; against moto only |
| 18 | Completion atomic and idempotent | COMPLETE | one transaction; second completion refused |
| 19 | Retry and cancellation safe | COMPLETE | authorization, audit, state and kill tests |
| 20 | Crash recovery documented and tested | COMPLETE | `worker-recovery.md`; stale re-queue and release tests (SIGKILL simulated) |
| 21 | Cleanup spares originals and completed outputs | COMPLETE | `TestCleanupOutputsKeepsTheOriginal`, claim test |
| 22 | Creator UI accurate | PARTIALLY COMPLETE | jsdom tests only |
| 23 | Private and unpublished pending review | COMPLETE | `TestProcessingNeverPublishes` |
| 24 | Backend tests pass | COMPLETE | section 14 |
| 25 | Integration tests pass or blockers documented | PARTIALLY COMPLETE | PostgreSQL, Redis, S3 API pass; real S3/Docker blocked (no such infrastructure here) |
| 26 | Security tests pass | PARTIALLY COMPLETE | injection, authorization, leak, disguised-file tests pass; no fuzzing, no container isolation test |
| 27 | OpenAPI and docs updated | COMPLETE | `backend/docs/openapi.json` (generated, drift-tested), worker, recovery, observability docs; two specs exist |
| 28 | No secrets committed | COMPLETE | scan |
| 29 | No critical unresolved security issues | PARTIALLY COMPLETE | none known; items in section 16 and the unverified isolation in section 12 remain |

## Evidence index (`processing/baseline/`)

`phase6-backend-suite.txt`, `phase6-mutation-checks.txt`, `phase6-web-typecheck.txt`, `phase6-web-vitest.txt`.
