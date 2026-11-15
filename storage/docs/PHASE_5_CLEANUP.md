# Phase 5 / D — storage cleanup, health and counters

## What gets freed, and when
The immediate paths (cancel, reject at `complete`, janitor expiry) free storage on a best-effort basis: a failed
`AbortMultipart` or `Delete` is only logged. Phase D adds the safety net that finds whatever they missed **from the
database** (`upload_sessions`, migration 000012) and retries it until it works.

| Session state | Becomes eligible | What the sweeper frees | Why |
|---|---|---|---|
| `EXPIRED` | `UPLOAD_CLEANUP_DELAY` (10 min) after it closed | the multipart upload only | a retry reuses the storage key, so objects are not touched |
| `EXPIRED` past its retry window | `UPLOAD_EXPIRED_RETENTION` (7 days) after its deadline | becomes `ABORTED` first, then as below | final: no longer retryable |
| `ABORTED` (cancelled, rejected, finalized) | `UPLOAD_CLEANUP_DELAY` after it closed | multipart upload + everything under `uploads/<video>/`, `thumbs/<video>/`, `captions/<video>/` | the draft video never published |
| `INITIATED`, `COMPLETING`, `COMPLETED` | never | nothing | live or processed uploads |

Guards: the video id must be a UUID; a session whose video is `PUBLISHED` is never deleted (logged as an error, counted
as `cleanup_skipped_published`); the result of a sweep is recorded only if the session still holds the same provider
upload id, so a session that was retried meanwhile is left alone (tested, including the "expired again" case).

Failures: `cleanup_attempts` and `cleanup_next_at` give a backoff of 1, 2, 4 ... 360 minutes, so one broken row cannot
starve the batch (50 rows per minute per instance). From the 5th failed attempt on it is logged at ERROR.
Every API instance runs the sweeper; work may be duplicated but is idempotent (a missing object or multipart is success).
Without `STORAGE_*` the sweeper does nothing and marks nothing.

## Configuration (all optional)
| Variable | Default | Range | Meaning |
|---|---|---|---|
| `UPLOAD_EXPIRED_RETENTION` | 168h | 1h..2160h | how long an expired upload stays retryable |
| `UPLOAD_CLEANUP_DELAY` | 10m | 1m..24h | age of a closed session before the sweeper touches its storage |

## Health
`GET /ready` checks Postgres and Redis (required: failure -> 503). When `STORAGE_*` is set it also checks the bucket
(`HEAD bucket`) as an **optional** dependency: a failure is reported as `"storage":"error"` and `"degraded":["storage"]`
but the status stays 200, so an object-storage outage does not take the whole API (browsing, login) out of rotation.
Error details are logged, never returned.

## Counters (`internal/metrics`)
No metrics stack exists, so counters live in memory and the janitor logs them as one line ("upload counters since
start") every 10 minutes. There is **no scrape endpoint** yet (it needs an auth / network decision).
`upload_init`, `upload_complete`, `upload_complete_retryable_failure`, `upload_rejected_<code>` (invalid_file,
size_mismatch ...), `upload_aborted`, `upload_retry`, `upload_expired`, `upload_expired_finalized`, `cleanup_sessions`,
`cleanup_objects_deleted`, `cleanup_failed`, `cleanup_skipped_published`.

## Known gaps (not covered here)
- A multipart upload that never got a database row (process crash between `CreateMultipart` and the insert) is invisible
  to a database-driven sweep. Cover it with a bucket lifecycle rule: AWS S3 `AbortIncompleteMultipartUpload` after
  1-2 days; MinIO aborts stale multipart uploads by itself after 24h by default. **Configured nowhere in this repo; not verified.**
- Outputs of the future FFmpeg worker (B6) will live elsewhere (e.g. `videos/<id>/`): extend `freeStorage` when it lands.
- The deleted-by-owner path of a *published* video is a content-lifecycle task (B6/B7), not an upload cleanup.
- Nothing here was run against real MinIO/S3; the storage client is covered by an in-process fake S3 and the stub used
  by the integration test.
