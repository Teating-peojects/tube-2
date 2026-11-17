# Phase 5 — deployment and operations

## 1. Object storage
Any S3-compatible service: MinIO (local: `docker compose --profile storage up`), AWS S3, Cloudflare R2, Ceph.
- Create **one private bucket** (no public read, no public ACLs). Block public access where the provider has a switch.
- Create an access key that can: list/put/get/delete objects in the bucket, create/list/abort/complete multipart uploads,
  and `HEAD` the bucket (the readiness check). Nothing else.
- Set the variables in section 3. `STORAGE_PATH_STYLE=true` for MinIO and most self-hosted servers.
- `STORAGE_PUBLIC_ENDPOINT` is the host the **browser** reaches (presigned URLs are signed for it); it can differ from the
  endpoint the API uses inside a private network.

## 2. Bucket CORS - the most common mistake
The browser PUTs parts directly to the bucket from your site, so the bucket must allow it **and expose the `ETag` header**.
Without `ExposeHeaders: ETag` the part is stored but the browser cannot read its ETag, no upload can complete, and the app
shows "Uploads are not set up correctly on the server (storage CORS)".
```json
[{
  "AllowedOrigins": ["https://your-site.example"],
  "AllowedMethods": ["PUT"],
  "AllowedHeaders": ["*"],
  "ExposeHeaders": ["ETag"],
  "MaxAgeSeconds": 3600
}]
```
(AWS S3: bucket CORS. R2: bucket CORS policy. MinIO: `MINIO_API_CORS_ALLOW_ORIGIN`, set in the compose file from
`CORS_ALLOWED_ORIGINS`; whether MinIO exposes `ETag` by default was **not verified**: test one upload from a browser.)

## 3. API environment (all validated at startup, wrong values stop the process with the variable's name)
| Variable | Default | Meaning |
|---|---|---|
| `STORAGE_ENDPOINT`, `STORAGE_PUBLIC_ENDPOINT`, `STORAGE_REGION`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_BUCKET`, `STORAGE_PATH_STYLE` | none | unset = uploads off (503 `uploads_disabled`), the rest of the API keeps working |
| `UPLOAD_MAX_BYTES` | 5 GiB | 1 MiB..5 GiB (the database caps a video at 5 GiB) |
| `UPLOAD_PART_SIZE_BYTES` | 16 MiB | 5 MiB..1 GiB |
| `UPLOAD_MAX_OPEN` | 5 | unfinished uploads per user, 1..50 |
| `UPLOAD_SESSION_TTL` | 24h | 1h..168h; presigned part URLs live half of it |
| `UPLOAD_MAX_RETRIES` | 3 | 0..10, 0 turns retry off |
| `UPLOAD_EXPIRED_RETENTION` | 168h | how long an expired upload stays retryable, 1h..2160h |
| `UPLOAD_CLEANUP_DELAY` | 10m | age of a closed session before the sweeper frees its storage, 1m..24h |
Web: `NEXT_PUBLIC_USE_MOCK=false` and the API URL; see `web/README.md`.

## 4. Database
Apply migrations `000010` (upload + moderation), `000011` (retry), `000012` (cleanup bookkeeping) with `db/scripts/migrate.sh`
(now executable). All are additive; `.down.sql` files exist for `000011` and `000012` (tested).
**First start after `000012`:** closed sessions from before the migration have no cleanup mark, so the sweeper works through
them (50 per minute per instance): every old ABORTED session loses its `uploads/`, `thumbs/`, `captions/` objects. Intended, but
it is a one-time bulk delete in production; check that no ABORTED session is still wanted before deploying.

## 5. Lifecycle rule for orphan multipart uploads
A crash between creating a multipart upload and recording it in the database leaves an upload no row points to. The sweeper
cannot see it, so let the bucket clean it up (not configured by this repository, **not verified**):
```json
{ "Rules": [{ "ID": "abort-stale-multipart", "Status": "Enabled", "Filter": {"Prefix": "uploads/"},
  "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 2 } }] }
```
MinIO removes stale multipart uploads on its own (24 h by default; check the version you run).

## 6. Health and monitoring
- `GET /health` (liveness), `GET /ready` (PostgreSQL and Redis required; object storage optional: `"degraded":["storage"]`, still 200).
  Alert on `degraded` including `storage`: uploads fail while browsing keeps working.
- Logs: "upload storage sweep" (finalized/cleaned/failed per minute), "freeing upload storage failed" (WARN, ERROR from the 5th
  attempt of one session: look at it), "expired uploads closed", "upload counters since start" every 10 minutes
  (`upload_init`, `upload_complete`, `upload_complete_retryable_failure`, `upload_rejected_<code>`, `upload_aborted`,
  `upload_retry`, `upload_expired`, `upload_expired_finalized`, `cleanup_sessions`, `cleanup_objects_deleted`, `cleanup_failed`,
  `cleanup_skipped_published`). There is no scrape endpoint yet.
- Watch for a growing number of rows with `storage_cleaned_at IS NULL AND status IN ('EXPIRED','ABORTED')` older than a few hours.

## 7. Rollout and rollback
1. Deploy storage + CORS, then the migrations, then the API (the janitor starts with it), then the web app.
2. Check `/ready`, upload a small file from a real browser, cancel one, let one expire (`UPLOAD_SESSION_TTL=1h` in staging).
3. Rollback: the API can be rolled back without touching the schema (the new columns have defaults); `000012`/`000011` down
   files remove them if needed. Objects the sweeper already deleted are gone.

## 8. Not verified (do before relying on it)
Real MinIO / AWS S3 / R2 with `TestS3*`; MinIO's default `ETag` CORS exposure; `docker compose --profile storage`; the CI job `s3`
(never run); a real browser end to end (Playwright was not available); the lifecycle rule; load behaviour.
