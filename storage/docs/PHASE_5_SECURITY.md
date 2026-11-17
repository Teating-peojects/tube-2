# Phase 5 — security review of the upload path

Scope: the upload API, the object storage it uses and the browser code. "Evidence" names the test that exercises the
control; a control without a test is marked **code only**. This is a review of what was built, not a certification.

## Controls
| Threat | Control | Evidence |
|---|---|---|
| Anonymous or unverified upload | session cookie + verified email (`403 verification_required`) | `TestUploadHTTPAccessControl` |
| Cross-site request with the user's cookie | `Origin` allow-list (`403 csrf_blocked`) | `TestUploadHTTPAccessControl` |
| Touching someone else's upload or video | every lookup is scoped to the owner; a stranger gets `404` exactly like a missing id, on status, complete, retry, abort, assets, metadata, publish | `TestUploadHTTPAccessControl`, `...Assets`, `...DraftMetadata`, `...Publish...` |
| Wrong file type | extension allow-list, declared type must be `video/*` or octet-stream, **file signature** checked on the assembled object (`ftyp` for MP4/M4V/MOV, EBML for MKV/WebM); refused files are deleted and the draft discarded | `TestUploadHTTPRejectedFiles`, `TestUploadHTTPInitValidation` |
| Oversized or truncated file | declared size capped (5 GiB, configurable lower), the part layout is fixed by the server, the part list is compared with what storage received (sizes and MD5 ETags) | `TestUploadHTTPRecoverableCompleteFailures` |
| Resource abuse | 5 open uploads per user, rate limits (init 10/h, complete 30/h, retry 20/h, status 120/min, assets 40/h, metadata 60/min, publish 30/h), at most 3 retries, bounded retry window | `TestUploadHTTPInitValidation` (cap), unit tests; limits are code, the limiter itself is shared with the other modules. If Redis is down the limited endpoints fail closed (code only) |
| Guessing storage keys | keys contain a UUIDv7 video id; the bucket is private; nothing is readable without a presigned URL | code only (deployment must keep the bucket private) |
| Stolen presigned URL | PUT only, one object/part, short life (parts: half the session TTL, default 12 h; assets: 30 min); the URL cannot read anything | expiry answers 403 on a real S3 server (s3rver), see the S3 matrix |
| Automatic publication of unreviewed content | uploading never publishes; publishing needs `READY` + the owner's `consent: true`, a category, valid assets | `TestUploadHTTPPublishNeverHappensByItself` |
| Missing audit trail / consent | `upload_consents` (append-only, statement version, HMAC of the IP), `audit_logs` for init, complete, rejected, retry, abort, metadata, publish | `...PublishNeverHappensByItself`, `TestUploadRetryAgainstRealDatabase` |
| Uploaded thumbnail or caption that is not what it claims | content is read back and checked at publish (image signature, VTT/SRT parse); refused files are deleted | `...PublishChecksTheUploadedAssets` |
| Information leaks | the browser never shows server text (fixed messages per error code); `/ready` never returns error details; storage errors are logged, the client gets `storage_unavailable` | web `classify` test, `TestReadyReports...`, `TestReadyOptionalDependenciesNeverFailReadiness` |
| Leftover data after a cancel | the file and its thumbnails/captions are deleted immediately and again by the sweeper, queued jobs are closed | `TestUploadHTTPCancel`, `TestUploadStorageCleanupAgainstRealDatabase` |
| Secrets | storage keys come from the environment; `.env.example` holds placeholders; the compose defaults are for local MinIO only | code only |
| Cheating the cleanup into deleting live data | never touches INITIATED/COMPLETING/COMPLETED sessions or PUBLISHED videos; a result is recorded only for the multipart id it saw | `TestUploadStorageCleanupAgainstRealDatabase` (+ mutation checks) |

## Not covered (open)
1. **No malware or CSAM scanning.** Only the file signature is checked; the content is not decoded. The hash-blocklist trigger
   in the schema (migration 000010) exists but was not re-verified in Phase 5, and no scanner is integrated (B10). Do not
   open uploads to the public before this is decided.
2. **Age gate per request.** Upload routes require a verified email; age is verified at registration (server-signed cookie
   `xh_age`), not on every upload request. Accepted so far; confirm it meets the requirement.
3. **No duration or content check** (needs ffprobe, B6) and **no SHA-256** of the file yet.
4. **SigV4 correctness on real S3/MinIO is unproven.** The client passes against emulators that do not enforce signatures.
   Run `TestS3*` against MinIO/S3 (README) before relying on it; the two unverified expectations are listed in the matrix.
5. **Bucket CORS must expose `ETag`.** If it does not, no browser can complete an upload; the app now reports this as a server
   setup error instead of looping (see deployment).
6. **Retry re-sends the whole file**; a leaked part URL can be used by anyone until it expires (12 h by default); lower
   `UPLOAD_SESSION_TTL` to shorten that window.
7. **Orphan multipart uploads** (a crash between creating the upload and recording it) need a bucket lifecycle rule.
8. No malware sandbox for the future worker (B10), no per-IP limits on the upload endpoints (limits are per user).
