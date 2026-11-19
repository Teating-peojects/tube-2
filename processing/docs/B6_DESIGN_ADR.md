# B6 Design record (ADR) - processing worker

Status: **D1, D2, D3, D8 DECIDED by the owner on 2026-11-19: all proposed defaults accepted.** Items still marked PROPOSED below are engineering choices to be confirmed by tests in later sub-phases.
Evidence for each fact: `B6_AUDIT.md`. No product code exists yet.

## Decisions (owner answered: all defaults)
| ID | Question | Proposed default | Why |
|---|---|---|---|
| D1 DECIDED | After `READY`, may the owner publish alone, or must a moderator approve first? | owner publishes (as today) + post-publication moderation; a pre-publication queue only for flagged accounts (B9) | Keeps the existing consent flow and tests; the queue can be added later without changing B6 |
| D2 DECIDED | HLS segments: MPEG-TS or fMP4 | MPEG-TS | Works everywhere with hls.js (already a web dependency) and Safari; simplest muxer settings. fMP4 is worth it only for encryption/low latency |
| D3 DECIDED | One pipeline job per video, or a chain `METADATA -> TRANSCODE -> HLS -> THUMBNAIL` | `METADATA` job (already enqueued) inspects/validates, then enqueues **one `HLS` job** that transcodes, packages and makes thumbnails | One lease, one retry budget, one output version, one finalize transaction. Cost: the upload status endpoint must read `progress`/`stage` from the job (audit section 4) |
| D8 DECIDED | Keep the original after a successful transcode | keep (configurable), never auto-delete | Needed for reprocessing and for hash/takedown evidence; storage cost is the trade-off |

## Design (PROPOSED, independent of D1/D2/D8; D3 shapes the job part)
**Queue.** The existing `video_processing_jobs` table on PostgreSQL. No second queue. `Claim` = one statement with `FOR UPDATE SKIP LOCKED` on
`video_jobs_queue`, setting `status='PROCESSING'`, `worker_id`, `started_at`, `heartbeat_at`, `attempts+1`.

**Lease.** Worker refreshes `heartbeat_at` every N seconds (config). A recovery step re-queues `PROCESSING` rows whose heartbeat is older than the lease
(`video_jobs_stale`), without consuming an extra attempt if the worker died, and a fenced `Complete/Fail` (`WHERE worker_id=$me AND status='PROCESSING'`) so a
revived stale worker cannot overwrite a newer claim.

**Retry.** Transient failure: `status='QUEUED'`, `scheduled_at = now + backoff(attempts) + jitter`; at `max_attempts` -> `FAILED`. Permanent failure
(bad input, policy) -> `FAILED` immediately. The database `attempts <= max_attempts` CHECK is respected.

**Safe errors.** New `error_code` (short fixed vocabulary, e.g. `unsupported_media`, `too_long`, `short_too_large`, `no_video_stream`, `corrupt_input`,
`processing_timeout`, `storage_error`, `internal`) shown to users; raw FFmpeg output goes to logs only, truncated, never to `error_message` of an API response.

**Cancellation.** `cancel_requested_at` column on the job; the worker checks it between stages and kills FFmpeg; terminal state is `FAILED` with
`error_code='cancelled'`. Reason: adding `CANCELLED` to the `job_status` enum is irreversible in PostgreSQL (values cannot be dropped), which would break the
up/down/up migration check used in Phase 5.

**Versioning.** `processing_version smallint` on the job (starts at 1; bumped by "reprocess"). Outputs live under
`videos/<videoId>/processing/<version>/hls/` (`master.m3u8`, `<res>/index.m3u8`, `<res>/seg_00000.ts`...). `videos.hls_master_key` is switched to the new
master in the **same transaction** that replaces the `video_variants` rows, so a viewer never sees a half-written version. The previous version's prefix is
deleted only after that commit (delayed), and never while the video is being played by an unrevoked token (B7 will define this).
Generated thumbnails: PROPOSED to stay under the existing `thumbs/<videoId>/` prefix (already cleaned by `freeStorage`, already read by the status endpoint);
confirm how the asset URL is built before B6.5.

**Profile ladder.** H.264 + AAC. Rungs from the `video_resolution` enum: 360p, 480p, 720p, 1080p. Never upscale: only rungs with height <= source height
(portrait/rotated sources handled by display dimensions after rotation metadata); if the source is below 360p, one rung is produced at source size labelled
`360p`. `PROCESSING_PROFILE_VERSION` is stored with the output so a ladder change is a new `processing_version`, not a silent edit.

**Input policy (B6.3).** ffprobe JSON; reject: no video stream, zero/absurd dimensions or fps, duration over `PROCESSING_MAX_DURATION` (hard DB cap 86,400 s),
extension/container mismatch handled by content not name, SHORT over 200 MiB (`short_too_large`). The worker never trusts the filename or the Phase 5
signature check as final.

**Execution (B6.4).** FFmpeg run without a shell (argument array), explicit timeout, `-nostdin`, per-job temp directory removed on every exit path, concurrency
cap, resource limits where the OS allows; stderr bounded. Real sandboxing (no network, read-only FS, seccomp) is B10.

**Process boundary.** New binary `backend/cmd/worker`, new image, new compose service (profile `processing`), connects as `redxtube_worker`. The API binary is
unchanged except: status endpoint (D3), `freeStorage` prefix, and `PROCESSING_*` config for the worker only.

## State mapping (audit mismatches resolved as proposals)
| Phase 6 document | Repository | Resolution |
|---|---|---|
| terminal `READY_FOR_REVIEW`, "success must not publish" | video `READY`; publish needs consent | keep `READY` (private until the owner acts); D1 decides whether a moderator step sits in between |
| `RUNNING`, `RETRY_WAIT`, `SUCCEEDED`, `CANCELLED` | `PROCESSING`, `COMPLETED`, `FAILED` | keep the enum; retry = `QUEUED` + future `scheduled_at`; cancel = `cancel_requested_at` + `FAILED/cancelled` |
| fine-grained stages | none | `stage` text + `progress` on the job; no new `video_status` |
| one job per video | four job types | D3 |
| outputs under `videos/<id>/processing/<version>/` | sweeper knows `uploads/ thumbs/ captions/` | extend `freeStorage` with `videos/` and test it |
| playback by token | signer exists, unused | B7.1 |

## Migration list (PROPOSED; written in B6.2, reversible, up/down/up on PostgreSQL 16)
`000013_processing_lifecycle`:
1. `video_processing_jobs`: `processing_version smallint NOT NULL DEFAULT 1`, `stage text`, `progress smallint NOT NULL DEFAULT 0 CHECK (0..100)`,
   `error_code text CHECK (format)`, `cancel_requested_at timestamptz`, `lease_expires_at` only if heartbeat age alone proves insufficient.
2. Index to find the active job of a video for the status endpoint (existing `video_jobs_by_video` may already be enough: verify with `EXPLAIN`).
3. Only if D3 = chain: no new columns beyond (1). Only if D1 = moderator approval: a gate column or status (decided with B9, not here).
4. No `videos` summary columns unless B6.7 shows the status endpoint needs them.
`video_variants` version column: NOT proposed; replace rows in the finalize transaction instead (audit section 3).

## Risks
- Go, PostgreSQL and Redis are absent in the current sandbox session; the B6.2 concurrency tests cannot run until the environment is restored.
- Real S3/MinIO behaviour for worker uploads is unproven (same as Phase 5).
- Hash blocklist trigger not re-verified; B6.6 depends on it.
