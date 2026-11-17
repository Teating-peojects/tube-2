# Phase 5 — upload and storage architecture

## Overview
The API never carries the video bytes. It hands the browser presigned URLs, the browser PUTs the file to object storage
in parts, and the API then verifies what arrived and queues the video for processing.

```
 Browser                     API (Go, every instance)                     Object storage (S3 API)
 web/src/lib/uploader.ts     handler -> service -> repository             MinIO (dev) / S3 / R2 (prod)
 features/upload/useUpload   internal/storage (hand-written SigV4)
    |  1 POST /upload/init ------------>  validate, draft video + session  --- CreateMultipart ---->
    |  <---- presigned part URLs -------
    |  2 PUT each part (3 in parallel, straight to storage, no cookies) ---------------------------->
    |  3 POST /upload/{id}/complete --->  ListParts, compare ETags + sizes <--- ListParts -----------
    |                                     CompleteMultipart, Head, read the first bytes (signature)
    |                                     video PROCESSING + one video_processing_jobs row
    |  4 GET /upload/{id}/status ------>  (polls; realtime push optional)
                                          janitor (1/min): expire sessions, sweep storage
        PostgreSQL: upload_sessions, videos, video_processing_jobs, upload_consents, video_thumbnails,
                    video_captions, audit_logs        Redis: rate limits        future worker (B6): FFmpeg/HLS
```

## State machine of an upload session (`upload_sessions.status`)
```
            init                complete ok                 publish (owner, consent, video READY)
 (none) ---------> INITIATED ---------------> COMPLETING ---> COMPLETED ---------------------------> (video PUBLISHED)
                    |  ^   |                      |  |
        cancel      |  |   | deadline passes      |  | parts/storage problem: back to INITIATED (RevertComplete)
        (DELETE)    |  |   v (janitor)           |  | not a video / wrong size: ABORTED + file deleted
                    |  |  EXPIRED --retry (max 3, within 7 days)--> INITIATED  (new multipart, same ids)
                    v  |     |
                  ABORTED <--+--- finalized by the janitor 7 days after the deadline
   COMPLETED --cancel before publish--> ABORTED (file deleted, queued job closed)
```
Video states: `UPLOADING` -> (complete) `PROCESSING` -> (worker, future) `READY` -> (owner) `PUBLISHED`. A cancelled, rejected or
finalized upload leaves the video `FAILED` with `deleted_at` set. **Nothing publishes by itself.**

## Storage layout (private bucket)
| Key | Content | Lifetime |
|---|---|---|
| `uploads/<videoId>/original.<ext>` | the uploaded file (`<videoId>` is a UUIDv7, not guessable from the user) | until the video is processed / cancelled |
| `thumbs/<videoId>/<uuid>.<ext>` | custom thumbnails (JPG/PNG/WebP, 2 MB, max 10) | with the video |
| `captions/<videoId>/<lang>.vtt` (`.srt` is converted) | captions, one per language, max 12 | with the video |

Nothing is publicly readable. Part and asset URLs are presigned PUTs (parts: half of the session TTL, assets: 30 minutes).
Playback and CDN are out of scope (B7).

## Data model (migrations 000010-000012 are Phase 5)
- `upload_sessions` (one per draft video): status, storage key, provider multipart id, declared size, part size/count, deadline,
  `retry_count`, `last_retry_at` (000011), `storage_cleaned_at`, `cleanup_attempts`, `cleanup_next_at` (000012).
  `completed_at` exists only while COMPLETED (CHECK).
- `upload_consents` (append-only, one per video): statement version, HMAC of the IP (32 bytes).
- `video_processing_jobs`: one `QUEUED` row per completed video (unique active job per video); the hand-off to the worker.
- All three migrations are additive and have reversible `.down.sql` files (tested: up, down, up, down twice, up).

## Background work (every API instance, once a minute, safe to run in parallel)
1. `ExpireStale`: sessions past their deadline become EXPIRED and their multipart upload is aborted.
2. `SweepStorage`: finalizes EXPIRED sessions older than the retry window; for every EXPIRED/ABORTED session older than
   `UPLOAD_CLEANUP_DELAY` aborts the multipart upload and (ABORTED only) deletes the video's objects under the three prefixes.
   Failures back off (1, 2, 4 ... 360 min). A PUBLISHED video is never touched. Details: `PHASE_5_CLEANUP.md`.
3. Every 10 minutes the in-process counters are logged as one line.

## Failure behaviour
| Situation | What happens |
|---|---|
| a part upload fails (network, 5xx, 408, 429) | the browser retries it with backoff; offline: it waits and resumes by itself |
| presigned URL expired (storage answers 403) | the browser calls `retry` once; if the session is still open or gone it cancels it and starts a new one |
| tab closed / reload | the file handle is lost: the user picks the file again; the abandoned session expires and is cleaned up |
| a part arrives damaged or the list is wrong | `complete` answers `parts_mismatch`; the upload stays open, the browser re-sends |
| storage is down during `complete` | 5xx `storage_unavailable`; the upload stays open, the browser retries |
| the file is not a video | `invalid_file`: upload closed, file deleted, draft discarded |
| the browser repeats `complete` | 204 again, still one job |
| two API instances act on the same session | row locks / guarded updates: one wins, the other gets a conflict |
| `cancel` after `complete` | file deleted, draft discarded, queued job closed (a running job must be skipped by the worker) |
| the API crashes between `CreateMultipart` and the database insert | an orphan multipart upload no row points to: **needs a bucket lifecycle rule** (see deployment) |
| Redis is down | rate-limited endpoints fail closed (503) |
| a janitor step fails | logged; the next minute tries again |

## Hand-off to the processing worker (B6, not built)
A completed upload leaves exactly one `QUEUED` `video_processing_jobs` row and the original at
`uploads/<videoId>/original.<ext>`. The worker must: skip videos that are deleted or not `PROCESSING` (a cancel cannot stop a
job that is already running), write its outputs under its own prefix (the sweeper only knows `uploads/`, `thumbs/`, `captions/`;
extend `freeStorage` for the new one), measure the duration, compute the SHA-256 (`videos.content_sha256`), and set `READY`
with `video_type`, `duration_seconds` and `hls_master_key` (CHECK `videos_ready_requirements`).

## Configuration
`STORAGE_*` (endpoint, public endpoint, region, keys, bucket, path style) and `UPLOAD_*` (max bytes, part size, max open,
session TTL, max retries, expired retention, cleanup delay) - all validated at startup, defaults in `.env.example`.
Without `STORAGE_*` uploads are off (503 `uploads_disabled`) and the rest of the API works.
