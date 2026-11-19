# B6.1 Audit - what exists before the processing worker

Date: 2026-11-19 · Method: static reading of the repository at `main` plus tool checks in the sandbox. **No Go tests were run for this
document** (see "Environment"). Labels: VERIFIED (seen in code/SQL or run), PROPOSED (design choice, not yet decided), OPEN.

## 1. Queue table `video_processing_jobs` (db/migrations/000003_catalog.up.sql)
- Columns: `job_type` (`METADATA|TRANSCODE|HLS|THUMBNAIL`), `status` (`QUEUED|PROCESSING|COMPLETED|FAILED`), `attempts`, `max_attempts` (default 3),
  `error_message` (<= 2000 chars), `worker_id`, `heartbeat_at`, `scheduled_at`, `started_at`, `completed_at`. VERIFIED.
- Indexes already shaped for a Postgres queue: `video_jobs_queue (scheduled_at) WHERE status='QUEUED'` (for `FOR UPDATE SKIP LOCKED`),
  `video_jobs_stale (heartbeat_at) WHERE status='PROCESSING'`, unique `video_jobs_one_active (video_id, job_type) WHERE status IN (QUEUED, PROCESSING)`.
- CHECKs: `PROCESSING` needs `started_at`; `COMPLETED` needs `completed_at`; `attempts <= max_attempts`.
- **Missing for B6:** no `stage`, `progress`, `processing_version`, safe `error_code`, cancel marker. `error_message` is free text (a leak risk if raw
  FFmpeg stderr is stored). There is no `CANCELLED`/retry-wait state: retry must use `QUEUED` + a future `scheduled_at`.
- Phase 5 enqueues exactly one `METADATA` job per completed upload; **nothing consumes it**. `videos.status` becomes `PROCESSING` at that moment
  (`qVideoProcess`, repository/upload.go).

## 2. `videos` constraints the worker must respect
- `READY`/`PUBLISHED` require `video_type`, `duration_seconds`, `hls_master_key` (`videos_ready_requirements`). `video_type` and duration are NULL until
  the worker measures them.
- `videos_type_matches_duration`: `SHORT` iff duration <= 60 s, else `LONG`. `videos_duration_range`: 1..86,400 s (hard database cap).
- `videos_size_limits`: file size <= 5 GiB, and **a `SHORT` must be <= 200 MiB**. A 50-second 400 MiB upload therefore cannot be stored as `SHORT`:
  finalize would hit the CHECK. The worker needs an explicit rule and a safe error code (PROPOSED: fail with `short_too_large`). OPEN until B6.3.
- `content_sha256` (bytea, 32 bytes) exists; trigger `videos_hash_blocklist` (migration 000010) sets `moderation_status='REMOVED'` and writes an
  `audit_logs` row `moderation.hash_match` on INSERT/UPDATE OF `content_sha256`. It does not stop the worker from finishing, so the worker must
  re-read `moderation_status` after writing the hash and not leave such a video `READY`. **Trigger not re-verified yet** (B9.1 / B6.6).
- Existing columns used by the worker: `original_storage_key`, `file_size_bytes`, `original_filename`, `hls_master_key`, `deleted_at`.

## 3. `video_variants` and `video_thumbnails`
- `video_variants`: `UNIQUE (video_id, resolution)`, `resolution` enum is only `360p|480p|720p|1080p`; `READY` needs `manifest_path`, `width`, `height`,
  `bitrate`; `storage_key` is the rendition prefix. **No version column**: reprocessing must replace the rows in the same transaction (PROPOSED) or a
  migration must add a version. Resolution labels above 1080p or below 360p are not representable.
- `video_thumbnails`: `type` (`POSTER|THUMBNAIL|PREVIEW`), `source` (`GENERATED|CUSTOM`), `status`. The API reads at most 6 `GENERATED`+`READY` rows
  for the status endpoint (`qGeneratedThumbs`).

## 4. How the API reports processing (a hidden dependency on D3)
`Upload.Status` (service/upload.go) computes progress as `COMPLETED jobs * 100 / pipelineJobs` with `pipelineJobs = 4`
(METADATA, TRANSCODE, HLS, THUMBNAIL) and reports `encoding` while the active job is `TRANSCODE` or `HLS`. A failed job with no active job = `failed`.
- If D3 chooses **one pipeline job**, this arithmetic is wrong (0 -> 25 -> 100). The status code must switch to the job's own `progress`/`stage`.
- If D3 chooses a **chain of four jobs**, the code is already right, but every handoff is another place to lose a job.

## 5. Storage client (backend/internal/storage)
`API` has: multipart create/list/complete/abort, `PresignUploadPart`, `PresignPut`, `Head`, `GetRange` (bytes), `Download(w io.Writer)`,
`Put(reader, size)`, `PutBytes`, `Delete`, `DeletePrefix`. VERIFIED.
- Streaming: `Download` writes to a writer (a file works, no whole-object memory); `Put` needs the size up front (a file on disk provides it).
- **Missing:** no listing of objects (only `DeletePrefix`), no `GetObject` that returns a reader, no `CopyObject`, no per-object `Content-Type` read in
  `Head`. A worker that downloads the original to local disk and uploads outputs from disk needs nothing more; verification by `Head` per key works.
- Behaviour against real S3/MinIO is still unproven (Phase 5 report): worker uploads inherit that risk.

## 6. Cleanup (`freeStorage`, service/upload.go)
Deletes only `uploads/<id>/`, `thumbs/<id>/`, `captions/<id>/`, only for `ABORTED` sessions, and refuses `PUBLISHED` videos.
**Outputs under `videos/<id>/processing/<version>/` are unknown to it**: an aborted/deleted upload would leak them. B6.5 must add the prefix and a test.

## 7. Database roles and privileges
`redxtube_worker` already exists (db/bootstrap/roles.sql): default SELECT/INSERT/UPDATE/DELETE on tables, `statement_timeout=10min`,
`idle_in_transaction_session_timeout=2min`; no access to `sessions`, `verification_tokens`, `age_verifications`, `user_settings`, `users` (column
SELECT only); no UPDATE/DELETE on `audit_logs`, `moderation_actions`, `upload_consents`; SELECT on `blocked_hashes`. VERIFIED in SQL.
The worker must connect as this role, not `redxtube_app`. `DATABASE_URL`-style config for it does not exist yet.

## 8. Config, binaries, compose
- `cmd/` contains only `api` and `devpass`. There is no `cmd/worker`, no worker Dockerfile, no worker service in `backend/docker-compose.yml`
  (services: postgres, redis, bootstrap, migrate, reference-data, minio and minio-init (profile `storage`), api). VERIFIED.
- Config knows `STORAGE_*` and `UPLOAD_*` only. No `PROCESSING_*` variables (concurrency, lease, max duration, ladder, FFmpeg paths, tmp dir).
- `.env.example` mentions `PLAYBACK_SIGNING_KEY` only as a comment (B7).

## 9. Tooling checked in this sandbox
- `ffmpeg` 6.1.1: encoders `libx264` and `aac` present; muxer `hls` present; filters `scale`, `thumbnail`, `transpose`, `setsar`, `fps` present. VERIFIED.
- A 3 s test clip (640x360, H.264 + AAC) was generated with `lavfi` and `ffprobe -print_format json` returned container, duration and both streams. A
  2 s HLS run (`-f hls -hls_playlist_type vod`) produced a playlist and a `.ts` segment. VERIFIED (files deleted afterwards).
- **Not present in this session:** `go`, `postgres`/`psql`, `redis-server`. B6.2 needs all three (REPRODUCE.md describes the earlier setup;
  `golang.org/x/*` was unreachable). **OPEN: restore the Go + PostgreSQL + Redis test environment at the start of B6.2 before any code is written.**
  Nothing in B6.1 depends on it.

## 10. Repository documentation drift
- `instruction.txt` points at `docs/ai-context/`, `docs/phases/` and a root `PROGRESS.md`: **none exist** (only `storage/` and `processing/`). This work
  uses `processing/` as the prompt prescribes.
- The two prompt copies (`Backend-b6_to_b10_prompt.MD` at the root, `processing/PROMPT_B6_TO_B10.md`) are identical (`diff` clean).

## 11. Mismatches with the Phase 6 document (section 2.3 of the prompt)
See `B6_DESIGN_ADR.md`, section "State mapping": none of them needs a new `video_status`; all have a PROPOSED resolution there.
