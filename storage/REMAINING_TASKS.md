# Remaining tasks (ordered)

1. ~~Creator approval~~ decided: none. ~~`.avi`~~ decided: dropped, `.m4v` added. ~~Configurable limits~~ done (Phase B).
2. Open, low priority: per-request age gate on upload routes (age is verified at registration only).
4. ~~Retry endpoint, retry counter and limit~~ done (Phase C). Frontend retry UI is Phase E.
5. ~~Cleanup, retention, storage health, counters~~ done (Phase D). Still open from it: a scrape endpoint for the counters (needs an auth/network decision) and the bucket lifecycle rule for multipart uploads that never got a row (docs only).
6. ~~Frontend: retry, ETA, `beforeunload`, network loss~~ done (Phase E, unit level). Open: Playwright e2e for cancel/offline/retry (needs browsers; CI runs e2e), a Studio view that uses `retryable`.
7. ~~Upload HTTP integration tests~~ done (Phase F). Open: run the S3 tests and the new CI job `s3` against real MinIO and pin its image (never run); confirm the two unverified S3 expectations there.
8. OpenAPI and the remaining PHASE_5 documents (Phase G).
9. Housekeeping outside Phase 5: ~~exec bits (F1)~~ and ~~`test-db.sh` stdout (F2)~~ fixed in Phase F. Still open: migration/table counts in README/DESIGN/db-adaptation (F3: now 12 migrations, 39 tables), `.puku-cli/settings.local.json` in `.gitignore`.
10. Blocked on B6 (worker): max duration check, content SHA-256, moving videos to READY. The worker must skip videos that are deleted or not PROCESSING (a cancelled upload's running job cannot be stopped from the API).
