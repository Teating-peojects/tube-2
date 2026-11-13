# Media worker (B6)

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
* No metrics endpoint yet: queue depth, failures and encode time are visible in the logs and in
  `video_processing_jobs`. Prometheus metrics belong to B10.
