# Phase 6 API: processing status, cancel, retry

Three routes added by Phase 6. They follow the existing conventions (JSON errors `{code, message}`, session cookie, CSRF origin
check on POST, rate limiting, audit log). `storage/openapi.yaml` (Phase 5) does not list them.

## GET /api/v1/videos/{id}/processing

Session required. The owner of the video, or staff (MODERATOR or higher). Anyone else, or an unknown or deleted id, gets
`404 target_not_found` (a video's existence is never confirmed to another creator).

```json
{
  "status": "processing",
  "stage": "encoding",
  "progress": 50,
  "renditions": ["360p", "480p"],
  "thumbnails": 5,
  "profile": "h264-aac-hls-v1",
  "canCancel": true,
  "canRetry": false,
  "updatedAt": "2026-11-15T10:00:00Z"
}
```

| Field | Values |
|---|---|
| `status` | `queued`, `processing`, `ready`, `published`, `failed`, `cancelled`, `removed` |
| `stage` | `inspecting`, `thumbnails`, `encoding`, `packaging` (only while queued or processing) |
| `progress` | 0 to 100: completed pipeline steps of four, 100 when ready or published |
| `errorCode`, `message` | only for `failed`, `cancelled`, `removed`: a stable code (`too_long`, `resolution_too_high`, `unsupported_codec`, `cancelled`, ...) and a creator-facing sentence; internal failures show the generic text with the code omitted from the allow-list |
| `renditions`, `thumbnails`, `profile` | what exists so far; the encoding recipe version once ready |
| `canCancel` | video is being processed |
| `canRetry` | staff only, video is `failed` |

The response never contains storage keys, paths, URLs or log text.

## POST /api/v1/studio/videos/{id}/processing/cancel

Verified-email session; the owner of the video or staff. Body optional: `{"reason": "text, 1 to 500 characters"}`. `204` on success.

* the video must be `PROCESSING`, otherwise `409 invalid_state` (already ready, already failed, already cancelled)
* jobs become `CANCELLED`, the video `FAILED`; a running ffmpeg or scanner is killed at the worker's next heartbeat; late results are refused
* not the owner and not staff: `404 target_not_found`; invalid reason: `422 validation_failed`; audited as `processing.cancelled` (`by: owner|staff`)

## POST /api/v1/admin/videos/{id}/processing/retry

Staff (MODERATOR or higher). Body: `{"reason": "text, 1 to 500 characters"}` (required). `204` on success.

* the video must be `FAILED`, its upload completed, not removed by moderation (restore it first), not deleted, otherwise `409 invalid_state`
* old job rows are replaced by one fresh `METADATA` job; the video returns to `PROCESSING`; the previous error goes into the audit entry (`processing.retried`)
* not staff: `403 forbidden`; unknown id: `404 target_not_found`
