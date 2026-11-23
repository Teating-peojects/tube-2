# Media worker: recovery guide (Phase 6)

What to look at and what to do when processing does not behave. Every step below was exercised by a test unless it is
marked **(manual)**. The worker is stateless: its only state is `video_processing_jobs`, `videos` and the object store,
so recovery is almost always "let it retry" or "put the video back in the queue".

## Where to look first

| Question | Where |
|---|---|
| Is the worker alive? | container `HEALTHCHECK` (`worker healthcheck`), `GET /health` and `GET /ready` on `METRICS_ADDR` |
| What is the queue doing? | `worker_queue_jobs`, `worker_queue_oldest_seconds`, `worker_active_jobs` (see the Metrics section of worker.md) |
| Why did a video fail? | `GET /api/v1/videos/{id}/processing` (owner or staff): `errorCode` + `message`; the worker log has the detail |
| What happened to a video? | `audit_logs` (`processing.cancelled`, `processing.retried`, `moderation.scan_match`) |

Log lines carry `job`, `video`, `type`, `attempt`. Internal detail (ffmpeg output) appears **only** in the log, with paths
removed; it is never stored in the database or returned by the API.

## Failure scenarios

| Scenario | What happens by itself | What you do |
|---|---|---|
| **Worker process crashes or is killed (SIGKILL, OOM)** | Its job keeps `status = PROCESSING` but stops heartbeating. After `WORKER_STALE_AFTER` (2 min) any worker's janitor re-queues it (`worker_lost`), or fails it when attempts are spent. Tested: `TestPGLeaseGuardAndStaleRequeue`. | Nothing. If the video ended up `FAILED` with `worker_lost`, retry it (below). |
| **Worker is stopped cleanly (deploy, SIGTERM)** | Running jobs get `WORKER_SHUTDOWN_GRACE` (30 s), then ffmpeg is killed and the job goes back to the queue **without using an attempt**. Tested: `TestShutdownReleasesTheRunningJob`. Give the container `stop_grace_period` >= the grace (compose: 60 s). | Nothing. |
| **A zombie worker reports late** (it was presumed dead, its job was given to another worker) | Every write that completes or fails a job requires `status = PROCESSING AND worker_id = me`; the stale result is refused. Tested. | Nothing. |
| **PostgreSQL unavailable** | The worker cannot claim jobs and logs `claim failed`; `/ready` returns 503. Running jobs keep encoding but cannot record results: they retry the write or are re-queued by the janitor later. Nothing is lost: jobs are rows. | Restore the database. The worker resumes by itself. |
| **Object storage unavailable** | A step that cannot read or write fails with `storage_failed` (retryable): retried with backoff 30 s, 60 s, 120 s ... up to `max_attempts` (3), then the video is `FAILED` (creator sees the generic text). | Restore storage, then retry the video. |
| **Storage accepted an object but returned a different size** | `storage_verify`: the step fails and is retried; the video is never marked ready on an unverified package. Tested. | Usually a flaky store: see above. |
| **ffmpeg hangs** | Per-command timeout (20 min + 2x the video length); the whole process group is killed; the attempt fails with `ffmpeg_timeout` (retryable). Tested (process group kill). | If it repeats for one file, the file is the problem: the video ends `FAILED`. |
| **ffmpeg produced a broken package** | The package is validated locally before anything is uploaded (`output_invalid`, retryable). Nothing reaches storage. Tested. | If it repeats, treat the file as bad. |
| **Scratch disk filling up** | Before each download the worker needs `2 x file size + WORKER_MIN_FREE_MB` free; otherwise the job **waits** (`disk_full`, retried) instead of failing the upload. A cancelled/failed video's scratch directory is removed; leftovers older than 24 h are removed hourly. | Add disk or lower `WORKER_CONCURRENCY`. Alert on `worker_scratch_free_bytes`. |
| **Queue grows** | Jobs wait, nothing is lost. `worker_queue_oldest_seconds{status="QUEUED"}` rises. | Add replicas (jobs are claimed with `SKIP LOCKED`; tested with 2 and 10 workers). |
| **Scanner (SCANNER_COMMAND) failing** | Fail-closed: the step retries; when attempts run out the video is `FAILED`. Nothing unscanned is published. | Fix the scanner, then retry the videos. |
| **A hash was blocklisted while the video was processing** | Caught again at finalize: the video is removed and audited, never `READY`. Tested. | Nothing. |

## Putting a failed video back in the queue

Staff only (MODERATOR or higher), with a written reason:

```
POST /api/v1/admin/videos/{id}/processing/retry     { "reason": "storage outage 12:00-12:40" }
```

It works for a `FAILED` video whose upload completed and that was not removed by moderation (restore it first). The old job
rows are replaced by one fresh `METADATA` job, the video goes back to `PROCESSING` and the previous error is written to the
audit entry. The original upload is reused (no re-upload).

Creators can stop their own video while it is queued or running:

```
POST /api/v1/studio/videos/{id}/processing/cancel    { "reason": "optional" }
```

The running ffmpeg is killed at the worker's next heartbeat (about 0.2 s in tests, at most `HeartbeatEvery`, default 15 s, in
production); late writes from the worker are refused.

## Cleanup

* Scratch: per job on success, failure and cancel; a janitor removes directories older than 24 h.
* Remote: partial HLS output of a `FAILED` video is removed `WORKER_FAILED_OUTPUT_RETENTION` (24 h) after it failed, by one
  claim-and-delete pass every 10 minutes (20 videos per pass). **The original upload is never touched**, nor generated
  thumbnails. If a delete fails, the claim is released and the video is tried again after another retention period.
  **(residual risk)** a retry that starts at the very moment the janitor deletes that video's prefix could lose objects;
  the window is milliseconds and needs a retry within the same instant as a 24-hour-old cleanup.

## Rolling back migration 000013

`db/migrations/000013_processing_controls.down.sql` drops `videos.processing_profile`, `videos.outputs_cleaned_at` and the
cleanup index, and folds `CANCELLED` jobs into `FAILED`. PostgreSQL cannot remove an enum value, so `CANCELLED` stays in
`job_status` (harmless). Up/down/up/down/up was run against a real database. Roll the **code** back first, then the migration.

## Changing limits or the encoding recipe

Limits (`WORKER_MAX_*`) apply to the next upload; videos already processed are untouched. Changing the ladder, preset or
segment length changes the encoding recipe: set `WORKER_PROFILE_VERSION` to a new value (the worker refuses to start
otherwise). The version is stored on each video (`videos.processing_profile`), so you can find what was encoded with what.
Existing videos are **not** re-encoded automatically.
