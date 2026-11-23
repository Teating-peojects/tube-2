# Media worker (Phase 6)

`cmd/worker` turns an uploaded original into everything the player and the catalogue need. It is a
separate process from the API (it runs ffmpeg, which is CPU-heavy and parses untrusted files) and
connects as the least-privilege `redxtube_worker` database role.

## Pipeline

The queue is the `video_processing_jobs` table (claimed with `FOR UPDATE SKIP LOCKED`). Each job
completes and enqueues the next one in a single transaction.

| Job | What it does | On success |
|---|---|---|
| `METADATA` | downloads the original, computes SHA-256, `ffprobe`, validates (container, codec, duration, size, resolution), writes `video_metadata`, `video_type`, `duration_seconds`, `content_sha256` | enqueues `THUMBNAIL` |
| `THUMBNAIL` | 5 JPEG frames (10%-90%), the middle one becomes the default primary thumbnail | enqueues `TRANSCODE` |
| `TRANSCODE` | one HLS rendition per ladder rung up to the source (never upscales), uploads segments then the playlist, upserts `video_variants` | enqueues `HLS` |
| `HLS` | writes `master.m3u8`, sets `hls_master_key`, `videos.status = READY`, inserts a `VIDEO_READY` notification | pipeline done |

Thumbnails run before encoding so the uploader can pick one while the video is still encoding. The
upload status endpoint counts the 4 jobs (`pipelineJobs = 4`) for its progress bar.

### Storage layout

```
hls/<videoId>/master.m3u8
hls/<videoId>/<360p|480p|720p|1080p>/index.m3u8
hls/<videoId>/<res>/seg_00000.ts ...
thumbs/<videoId>/<uuid>.jpg
```

These are exactly the keys `internal/playback` allows through the signed stream path. Renditions are
named by the short side (a 720x1280 portrait video has a `720p` rendition). Segments are 6 s,
keyframes are forced on a time grid so all renditions switch cleanly. Segments are immutable
(`max-age=31536000, immutable`); playlists cache for 5 minutes.

### Limits (enforced here, whatever the browser claimed)

SHORT: <= 60 s and <= 200 MiB. LONG: <= 60 min and <= 5 GiB. Short side <= 1080 and long side <= 1920
(larger sources are rejected with `resolution_too_high`). Containers: MP4/MOV/WebM. Codecs: H.264,
HEVC, VP8, VP9, AV1.

## Controls and status (Phase 6)

| Route | Who | What |
|---|---|---|
| `GET /api/v1/videos/{id}/processing` | owner or MODERATOR+ | `status` (`queued`, `processing`, `ready`, `published`, `failed`, `cancelled`, `removed`), `stage` (`inspecting`, `thumbnails`, `encoding`, `packaging`), `progress`, `errorCode` + `message`, `renditions`, `thumbnails`, `profile`, `canCancel`, `canRetry`. Never contains keys, paths, URLs or log text. |
| `POST /api/v1/studio/videos/{id}/processing/cancel` | owner or MODERATOR+ | stops a `PROCESSING` video; jobs become `CANCELLED`, the video `FAILED`; a running ffmpeg is killed at the next heartbeat; audited |
| `POST /api/v1/admin/videos/{id}/processing/retry` | MODERATOR+ (reason required) | re-queues a `FAILED` video from `METADATA`; needs a completed upload, refuses removed videos; audited with the previous error |

The creator-facing sentence for a failure comes from an allow-list (`service/processing_messages.go`); internal conditions
(ffmpeg, storage, scanner) always show the generic text. Spec state names vs the stored ones: `processing/docs/PHASE_6_DECISIONS.md`.

## Output validation and verification

The package is validated locally **before the first upload** (playlist well-formed and ended, every segment present, non-empty and
named `seg_NNNNN.ts`, durations consistent, total within max(2 s, 10%) of the source), the master playlist is validated against
the planned renditions, every uploaded object is **verified by size** (`Head`), and the finalize transaction re-checks the video
(still `PROCESSING`, not deleted or removed) and the hash blocklist before setting `READY`. Processing never publishes.

## Cleanup

Scratch directories are removed on success, failure and cancel (and after 24 h as a safety net). The partial `hls/` output of a
video that stayed `FAILED` for `WORKER_FAILED_OUTPUT_RETENTION` (24 h) is removed by a claim-and-delete pass every 10 minutes. The
original upload and generated thumbnails are never removed by the worker. Aborting or deleting an unpublished video also removes
`hls/` through the Phase 5 sweeper. See [worker-recovery.md](worker-recovery.md).

## Failure handling

* **Transient** errors (storage, ffmpeg crash, timeout) retry with exponential backoff
  (30 s, 60 s, 120 s ... capped at 10 min) until `max_attempts` (default 3), then the video is `FAILED`.
* **Permanent** errors (not a video, too long, hash on the blocklist, size mismatch) fail at once with
  a stable code in `error_message`; the uploader gets a `VIDEO_FAILED` notification.
* **Dead workers**: every running job refreshes `heartbeat_at`; a job without a heartbeat for
  `WORKER_STALE_AFTER` (2 min) is re-queued by any worker's janitor (or failed once attempts are spent).
* **Lease guard**: every statement that finishes or fails a job requires `status = PROCESSING AND
  worker_id = me`. A worker that was presumed dead and comes back late cannot overwrite the new owner.
* **Shutdown** (SIGTERM): running jobs get `WORKER_SHUTDOWN_GRACE` (30 s) to finish, then they are
  released back to the queue without consuming an attempt. Compose gives the container 60 s.
* Deleted or removed videos are skipped silently (no notification).

## Security

* Untrusted files are only handed to ffmpeg/ffprobe with `-protocol_whitelist file`, after the
  container name from ffprobe is on an allow-list. A playlist or concat file renamed `.mp4` is rejected
  (tested), so it cannot make ffmpeg read local files or reach the network.
* All metadata (GPS, device, tool tags) and chapters are stripped from every output
  (`-map_metadata -1`).
* The SHA-256 of the original is stored in `videos.content_sha256`; the database trigger removes a
  video whose hash is on `blocked_hashes`. The worker never advances a blocked video.
* Role `redxtube_worker`: no access to credentials, sessions, identity data, consents or the blocklist
  contents (SELECT only), and it cannot UPDATE/DELETE audit or moderation history.
* Compose runs it with a read-only root filesystem, all capabilities dropped, `no-new-privileges`,
  a PID limit and CPU/RAM limits. Keep the worker on a network that can only reach PostgreSQL, Redis
  and the object store.

## Upload scanning (malware / CSAM hook)

REDXTUBE does not bundle a CSAM or malware engine: hash-matching services (PhotoDNA, CSAI Match,
Cloudflare's CSAM tool, NCMEC hash lists) need a vendor agreement and credentials. The worker has the
integration point instead:

* `SCANNER_COMMAND` runs any program on every original, **before ffprobe**. `{file}` is replaced by the
  path (no shell, so file names cannot inject commands). Exit `0` = clean, `1` = match, anything else
  (crash, missing binary, timeout) = scanner failure. Example: `clamscan --no-summary {file}`.
* **Fail-closed**: a failing scanner retries with backoff and, when attempts run out, the video
  `FAILED`s. Nothing unscanned is published while a scanner is configured.
* A match sets `moderation_status = REMOVED`, writes `moderation.scan_match` to `audit_logs` and fails
  the job permanently without notifying the uploader. The original is left in storage (evidence).
* Without `SCANNER_COMMAND` the worker logs a warning in production. For a real deployment write a small
  wrapper script that calls your vendor's API and maps its verdict to the exit codes above; or implement
  `worker.Scanner` in Go for direct integration.
* Every ffmpeg, ffprobe and scanner child runs in its own process group that is killed on timeout or
  shutdown, so a hung process (and anything it spawned) cannot outlive its job.

## Metrics

Set `METRICS_ADDR=host:port` on the worker to serve `/metrics` (Prometheus text format), `/health` (liveness) and `/ready`
(the database answers) on a **separate internal listener**; keep it off the public network. Metrics are written by
`internal/metrics/prometheus.go` (no client library).

| Metric | Labels | Meaning |
|---|---|---|
| `worker_jobs_total` | type, result | attempts finished: `completed`, `retry`, `failed`, `released`, `lease_lost` |
| `worker_job_duration_seconds` | type | wall time of an attempt |
| `worker_job_queue_wait_seconds` | type | time a job waited before a worker claimed it |
| `worker_failures_total` | kind | failed attempts by bounded kind: `ffprobe`, `ffmpeg`, `storage`, `validation_output`, `scanner`, `disk`, `input_rejected`, `blocked`, `unavailable`, `other` |
| `worker_encode_seconds_per_media_minute` | | TRANSCODE wall time per minute of source video (above 60 = slower than real time) |
| `worker_media_seconds_total` | | seconds of source video transcoded |
| `worker_queue_jobs`, `worker_queue_oldest_seconds` | type, status | QUEUED / PROCESSING / FAILED (last 24 h) counts and age of the oldest job, explicit zeros |
| `worker_active_jobs`, `worker_concurrency` | | running now / configured |
| `worker_scratch_free_bytes`, `worker_scratch_used_bytes` | | scratch filesystem |
| `worker_cleanup_total` | result | janitor cleanups of failed output: `ok`, `error` |
| `worker_queue_scrape_ok` | | 0 when the queue cannot be read |

Suggested alerts: `worker_queue_oldest_seconds{status="QUEUED"} > 900` (uploads are not being processed);
`increase(worker_failures_total{kind=~"ffmpeg|storage|scanner|validation_output"}[15m]) > 5` (the platform, not the uploads, is failing);
`worker_scratch_free_bytes < 5e9`; `worker_queue_scrape_ok == 0`; `/ready` failing.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | required | connect as `redxtube_worker` |
| `STORAGE_ENDPOINT`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_BUCKET`, `STORAGE_REGION`, `STORAGE_PATH_STYLE` | required | same bucket as the API |
| `REDIS_URL` | empty | optional: realtime pushes (`VIDEO_READY` / `VIDEO_FAILED`); the UI also polls |
| `WORKER_CONCURRENCY` | 1 | jobs in parallel per process |
| `FFMPEG_THREADS` | 0 (auto) | threads per ffmpeg process |
| `WORKER_WORKDIR` | `/work` | scratch space: needs about 2x the largest original |
| `WORKER_POLL_INTERVAL` / `WORKER_STALE_AFTER` / `WORKER_SHUTDOWN_GRACE` | 3s / 2m / 30s | durations |
| `FFMPEG_PATH`, `FFPROBE_PATH` | `ffmpeg`, `ffprobe` | binaries |
| `SCANNER_COMMAND` | empty | upload scanner, see above |
| `METRICS_ADDR` | empty | `host:port` for `/metrics`, `/health` and `/ready`, internal only (see Metrics below; compose: `WORKER_METRICS_ADDR`) |
| `WORKER_MAX_DURATION_SECONDS` | 3600 | longest accepted video (1 s to 24 h) |
| `WORKER_MAX_SHORT_SIDE` / `WORKER_MAX_LONG_SIDE` | 1080 / 1920 | source size limits; the database allows at most these values |
| `WORKER_MAX_FPS` / `WORKER_MAX_STREAMS` | 120 / 16 | sources above these are rejected (outputs are capped at 60 fps) |
| `WORKER_MIN_FREE_MB` | 1024 | free scratch space that must remain after a download; below it a job waits (`disk_full`) |
| `FFMPEG_PRESET` | `veryfast` | x264 preset (`ultrafast` to `slow`) |
| `WORKER_SEGMENT_SECONDS` | 6 | HLS segment length, 2 to 10 |
| `WORKER_AUDIO_KBPS` / `WORKER_THUMBNAILS` | 128 / 5 | audio bitrate; candidate thumbnails per video (1 to 6) |
| `WORKER_LADDER` | `360:800,480:1400,720:2800,1080:5000` | short side : video kbps; only 360, 480, 720, 1080 exist in the database; **requires a new `WORKER_PROFILE_VERSION`** |
| `WORKER_PROFILE_VERSION` | `h264-aac-hls-v1` | stored on each video as `processing_profile` |
| `WORKER_FAILED_OUTPUT_RETENTION` | `24h` | how long partial output of failed videos is kept |

An unset or empty variable keeps the default (compose passes unset variables as empty strings). Out-of-range or inconsistent values
(raising a limit past what the database enforces, an unknown preset, a ladder without a profile version) stop the worker at startup
with a message; this is tested (`cmd/worker/limits_test.go`).

Scale by adding replicas, not by raising `WORKER_CONCURRENCY`: ffmpeg already uses all cores.

## Running it

```
cd backend
docker compose --profile storage up --build     # API + local MinIO + worker
go run ./cmd/worker                             # without Docker (needs ffmpeg on PATH)
```

## Tests

`go test ./internal/worker/` always runs the unit tests. With ffmpeg installed it also runs the full
pipeline against fake storage; with `TEST_ADMIN_DATABASE_URL` + `TEST_WORKER_DATABASE_URL` (printed by
`db/scripts/test-db.sh`) it tests the queue SQL as the real worker role; with `TEST_S3_ENDPOINT` +
`TEST_S3_BUCKET` it runs the whole worker loop against an S3-compatible server (MinIO, moto).

## Open decisions

* A blocked-hash original is left in storage untouched (evidence). Whether to delete it, quarantine it
  or report it is a legal/policy decision for you.
* Which scanner to use, and what to do on a match beyond removal (report, preserve, delete), is yours
  to decide with your legal advisers.
