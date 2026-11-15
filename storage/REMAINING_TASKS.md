# Remaining tasks (ordered)

1. ~~Creator approval~~ decided: none. ~~`.avi`~~ decided: dropped, `.m4v` added. ~~Configurable limits~~ done (Phase B).
2. Open, low priority: per-request age gate on upload routes (age is verified at registration only).
4. ~~Retry endpoint, retry counter and limit~~ done (Phase C). Frontend retry UI is Phase E.
5. ~~Cleanup, retention, storage health, counters~~ done (Phase D). Still open from it: a scrape endpoint for the counters (needs an auth/network decision) and the bucket lifecycle rule for multipart uploads that never got a row (docs only).
6. ~~Frontend: retry, ETA, `beforeunload`, network loss~~ done (Phase E, unit level). Open: Playwright e2e for cancel/offline/retry (needs browsers; CI runs e2e), a Studio view that uses `retryable`.
7. Upload HTTP integration tests and MinIO integration test + CI service (Phase F).
8. OpenAPI and the remaining PHASE_5 documents (Phase G).
9. Housekeeping outside Phase 5: exec bits on `db/scripts/*.sh` and `security/scripts/promote.sh` (F1), stdout in
   `test-db.sh` (F2), migration/table counts in README/DESIGN/db-adaptation (F3), `.puku-cli/settings.local.json`
   in `.gitignore`.
10. Blocked on B6 (worker): max duration check, content SHA-256, moving videos to READY.
