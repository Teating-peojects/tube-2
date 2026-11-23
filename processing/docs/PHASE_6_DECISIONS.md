# Phase 6 (video processing and HLS): decisions taken and why

The spec (`xhamtube_phase_6_video_processing_hls.md`) says to extend the existing system without breaking contracts and to
document every place where the implementation differs from its examples. These are those places. **Items marked OWNER need
your confirmation**; everything else follows directly from the existing code.

## Status model and queue (spec sections 5, 6, 13)

**D1. Existing enums, extended by one value (OWNER).** The spec proposes new video states (`PROCESSING_QUEUED`,
`GENERATING_THUMBNAILS`, `UPLOADING_OUTPUTS`, `VALIDATING_OUTPUTS`, `READY_FOR_REVIEW`, `PROCESSING_FAILED`,
`PROCESSING_CANCELLED`) and job states (`RETRY_WAIT`, `SUCCEEDED`, `CANCELLED`...). The database already has `video_status`
(`UPLOADING, PROCESSING, READY, FAILED...`) and `job_status` (`QUEUED, PROCESSING, COMPLETED, FAILED`), used by Phase 5, the
upload status endpoint, the studio list and the web app. Renaming them would break those contracts, so migration 000013 adds
only `job_status = CANCELLED`. The API exposes the spec's granularity as `status` + `stage`:

| Spec state | Stored as | `GET /videos/{id}/processing` |
|---|---|---|
| UPLOADED | `upload_sessions.status = COMPLETED` | (upload status endpoint) |
| PROCESSING_QUEUED | job `QUEUED` | `status: queued` |
| PROCESSING | job `PROCESSING` (METADATA) | `processing`, `stage: inspecting` |
| GENERATING_THUMBNAILS | job THUMBNAIL | `stage: thumbnails` |
| UPLOADING_OUTPUTS / VALIDATING_OUTPUTS | jobs TRANSCODE, HLS (validation happens inside, before and after the upload) | `stage: encoding` / `packaging` |
| READY_FOR_REVIEW | `videos.status = READY` (never `PUBLISHED` by processing) | `ready` |
| PROCESSING_FAILED | `videos.status = FAILED`, job `FAILED` | `failed` + `errorCode` |
| PROCESSING_CANCELLED | `videos.status = FAILED`, job `CANCELLED` | `cancelled` |
| RETRY_WAIT | job `QUEUED` with `scheduled_at` in the future and `attempts > 0` | `queued` |

**D2. Four chained jobs instead of one (OWNER).** The enum already has `METADATA, THUMBNAIL, TRANSCODE, HLS` and the Phase 5
status endpoint counts four jobs for progress. The worker runs them as a chain; each step completes and enqueues the next in
one transaction. Order: METADATA, THUMBNAIL, TRANSCODE, HLS (thumbnails first, so the creator can pick one while encoding).
Cost: the original is downloaded by every step that needs it (cached in the scratch directory when the same worker continues).

## Outputs (spec sections 8, 9, 12)

**D3. MPEG-TS segments, not fMP4 (OWNER).** `internal/playback` (existing) only serves `seg_NNNNN.ts` and the web player is
hls.js; MPEG-TS plays everywhere hls.js and native HLS do. fMP4 can be added later as a new profile version.

**D4. Object layout follows `internal/playback`, not the spec's example (OWNER).** The spec suggests
`videos/{id}/processing/{version}/hls/...`. The existing signed-stream path (Phase 7 work) accepts exactly
`hls/{videoId}/master.m3u8`, `hls/{videoId}/{rendition}/index.m3u8` and `.../seg_NNNNN.ts`, and thumbnails live under
`thumbs/{videoId}/`. The worker writes there. The encoding **version** is recorded in `videos.processing_profile`
(`h264-aac-hls-v1`) instead of in the path. Consequence: processing a video again replaces `hls/{id}/` (the worker deletes the
prefix first).

**D5. No temporary prefix + promote step (OWNER, known deviation).** The spec says to upload to a temporary location and
promote atomically. Object stores have no rename; promoting means copying every object again. Instead: the package is
**validated locally before the first upload**, uploaded to its final prefix while the video is still `PROCESSING` (the stream
gate refuses anything that is not `READY`/`PUBLISHED`, so nothing is servable), **verified object by object by size**, and only
then does one transaction set `READY`. A crash leaves unreferenced objects that the janitor removes. The spec's goal (no
partial package ever served) is met; its mechanism is not.

**D6. Thumbnails.** Five candidates (10% to 90%), the middle one is the default primary; a black middle frame is replaced by the
brightest candidate; every JPEG is decoded and checked. No animated preview or poster image yet.

**D7. Renditions.** 360p/480p/720p/1080p by the source's short side, never upscaled; a source below 360p gets one rendition at
its native size (named `360p`, because the database enum has no other name). Anamorphic (non-square pixel) sources are
planned and encoded for their display size and the output declares square pixels.

## API and security (spec sections 14, 15)

**D8. Routes follow the existing split (OWNER).** The spec suggests `/api/v1/videos/{id}/processing/...`. Creator actions in this
code base live under `/studio`, staff actions under `/admin`, so:

* `GET  /api/v1/videos/{id}/processing` (owner or MODERATOR+)
* `POST /api/v1/studio/videos/{id}/processing/cancel` (owner or MODERATOR+)
* `POST /api/v1/admin/videos/{id}/processing/retry` (MODERATOR+, reason required)

Retry is staff-only, as the spec allows; creators re-upload (Phase 5 already supports it).

**D9. A scanner hook, not an engine.** Hash-matching and malware engines need a vendor agreement. `SCANNER_COMMAND` runs any
program on every original before ffprobe, fail-closed; see `backend/docs/worker.md`.

**D10. The blocklist is checked twice.** When METADATA stores the SHA-256 (database trigger) and again when the video is about
to become READY (a hash blocklisted in between removes and audits the video).

**D11. Original files of removed videos stay in storage.** Deleting evidence is a legal decision (OWNER).

## Configuration (spec section 17)

Names follow the existing `WORKER_*` / `FFMPEG_*` style of the worker rather than the spec's `VIDEO_*` suggestions:

| Spec suggestion | Here | Note |
|---|---|---|
| `FFMPEG_PATH`, `FFPROBE_PATH` | same | |
| `VIDEO_WORKER_ENABLED` | run or do not run the `worker` container (compose profile) | no flag |
| `VIDEO_WORKER_CONCURRENCY` | `WORKER_CONCURRENCY` | |
| `VIDEO_MAX_DURATION_SECONDS` | `WORKER_MAX_DURATION_SECONDS` | can be lowered, default 3600 |
| `VIDEO_MAX_SOURCE_SIZE_BYTES` | Phase 5 upload limits and database constraints (200 MiB SHORT, 5 GiB LONG) | the worker does not add its own knob |
| `VIDEO_TEMP_DIR` | `WORKER_WORKDIR` | |
| `VIDEO_TEMP_DISK_LIMIT_BYTES` | `WORKER_MIN_FREE_MB` | a free-space guard (jobs wait), not a quota |
| `VIDEO_HLS_SEGMENT_SECONDS` | `WORKER_SEGMENT_SECONDS` | 2 to 10 |
| `VIDEO_RENDITIONS` | `WORKER_LADDER` | needs `WORKER_PROFILE_VERSION` |
| `VIDEO_PROCESSING_VERSION` | `WORKER_PROFILE_VERSION` | stored on each video |
| `VIDEO_HLS_FORMAT` | **not configurable** (D3) | |
| `VIDEO_PROCESSING_TIMEOUT_SECONDS` | **not configurable**: 20 min + 2x the video length per ffmpeg command | |
| `VIDEO_PROCESSING_MAX_RETRIES` | **not configurable**: `video_processing_jobs.max_attempts`, default 3 | |
| `VIDEO_RETRY_BASE_DELAY_SECONDS` | **not configurable**: 30 s doubling to 10 min | |
| `VIDEO_THUMBNAIL_QUALITY` | **not configurable** (JPEG quality 3); count via `WORKER_THUMBNAILS` | |
