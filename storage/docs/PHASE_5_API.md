# Phase 5 — upload API

Machine-readable contract: `storage/openapi.yaml` (OpenAPI 3.0.3, validated; a Go test fails when a route or an error code
there disappears from the code). Every example below was exercised by the HTTP tests in
`backend/internal/handler/upload_http_*_integration_test.go`.

All routes need a signed-in user with a verified email (session cookie) and, for browsers, an allowed `Origin`.
Errors: `{"code":"...","message":"...","fields":{"field":"message"}}`; 429 carries `Retry-After`.

## The browser flow
```
POST   /api/v1/upload/init              -> 201 { uploadId, videoId, partSize, parts:[{partNumber,url}] }
PUT    <each parts[].url>               -> 200 + ETag header (straight to storage, no cookies, no other headers)
POST   /api/v1/upload/{id}/assets       -> 201 { assetId, url }          (optional: custom thumbnail / captions, PUT to url)
POST   /api/v1/upload/{id}/complete     -> 204                           body { parts:[{partNumber, etag}] }
GET    /api/v1/upload/{id}/status       -> 200 { status, progress, thumbnails, error?, retryable? }   (poll until ready)
PATCH  /api/v1/videos/{videoId}/metadata -> 204                          (any time; only the sent fields change)
POST   /api/v1/videos/{videoId}/publish -> 204                           body { ..., category, consent: true }
POST   /api/v1/upload/{id}/retry        -> 200 (same body as init)       only for an expired upload
DELETE /api/v1/upload/{id}              -> 204                           cancel
```

## Endpoints
| Endpoint | Purpose | Limits | Errors (`code`) |
|---|---|---|---|
| `POST /upload/init` | validate, create draft + multipart upload | `.mp4 .m4v .mov .mkv .webm`; 1 B .. 5 GiB (configurable); 5 open uploads; 10/hour | 401 `unauthorized`, 403 `verification_required` / `csrf_blocked`, 422 `validation_failed` (fields `filename`, `size`) / `limit_reached`, 429, 502 `storage_unavailable`, 503 `uploads_disabled` |
| `POST /upload/{id}/complete` | verify and queue | 30/hour | 404 `upload_not_found`, 409 `upload_in_progress` / `upload_closed`, 422 `validation_failed` (part list wrong) / `parts_mismatch` (damaged part, upload stays open) / `invalid_file` / `size_mismatch` (upload closed), 502 `storage_unavailable` |
| `GET /upload/{id}/status` | processing state | 120/minute | 404 `upload_not_found` |
| `POST /upload/{id}/retry` | reopen an expired upload (same ids, new URLs) | 3 per upload, 7 days after the deadline, 20/hour | 409 `upload_active` (still open) / `not_retryable` (cancelled, rejected, completed, too old) / `retry_limit`, 422 `limit_reached` |
| `DELETE /upload/{id}` | cancel, delete the file, discard the draft | idempotent | 409 `already_published` |
| `POST /upload/{id}/assets` | presigned PUT for a thumbnail (JPG/PNG/WebP, 2 MB, max 10) or caption (`.vtt`/`.srt`, 1 MB, max 12 languages) | 40/hour | 409 `upload_closed`, 422 `validation_failed` / `limit_reached` |
| `PATCH /videos/{id}/metadata` | draft fields: title (1-100), description (5000), category, tags (max 15, 1-30 chars, no commas), visibility `public\|unlisted\|private`, comments `on\|off\|review`, language, ageGate | 60/minute | 404 `video_not_found`, 422 `validation_failed` |
| `POST /videos/{id}/publish` | the only way a video becomes visible | 30/hour | 409 `not_ready` (still processing) / `already_published`, 403 `video_removed`, 422 `validation_failed` (missing `consent`, missing category, bad thumbnail/caption) / `file_too_large_for_short` |

A user only ever sees their own uploads: someone else's id answers `404 upload_not_found`.

## Examples
```http
POST /api/v1/upload/init
{"filename":"clip.mp4","size":11534336,"contentType":"video/mp4"}

201 {"uploadId":"0192...","videoId":"0192...","partSize":5242880,
     "parts":[{"partNumber":1,"url":"https://storage.example/uploads/0192.../original.mp4?X-Amz-..."},{"partNumber":2,...},{"partNumber":3,...}]}

POST /api/v1/upload/0192.../complete
{"parts":[{"partNumber":1,"etag":"9b2cf535..."},{"partNumber":2,"etag":"..."},{"partNumber":3,"etag":"..."}]}
204
```
```http
POST /api/v1/videos/0192.../publish        (video must be READY)
{"title":"My clip","category":"music","visibility":"public","consent":true,"thumbnail":"<assetId>","captions":[{"lang":"bn","assetId":"<assetId>"}]}
204        409 not_ready while the worker has not finished; 422 {"fields":{"consent":"You must confirm ..."}} without consent
```

## Behaviour worth knowing
- The part size comes from the API (default 16 MiB, never below the S3 minimum of 5 MiB); the last part is shorter.
- `complete` compares the browser's list with what storage received. Send the ETag without quotes.
- A reopened (retried) upload must send **every part again**.
- `status` of a refused, cancelled or finalized upload is `failed` without `retryable`.
- Uploading does not publish. Publishing needs the owner's `consent: true` (stored with the statement version and a hashed IP).
