# Remaining tasks (ordered)

1. Decide: per-request age gate on upload routes, and creator approval (audit rows "Age" and "Creator").
2. Decide: keep `.avi`? Document MKV/WebM/MOV/MP4 as the allowlist?
3. Make limits configurable (Phase B).
4. Add the retry endpoint, retry counter and limit (Phase C).
5. Extend cleanup: orphans, retention, metrics, storage health (Phase D).
6. Verify/fill frontend: ETA, `beforeunload`, network loss, e2e (Phase E).
7. Upload HTTP integration tests and MinIO integration test + CI service (Phase F).
8. OpenAPI and the remaining PHASE_5 documents (Phase G).
9. Housekeeping outside Phase 5: exec bits on `db/scripts/*.sh` and `security/scripts/promote.sh` (F1), stdout in
   `test-db.sh` (F2), migration/table counts in README/DESIGN/db-adaptation (F3), `.puku-cli/settings.local.json`
   in `.gitignore`.
10. Blocked on B6 (worker): max duration check, content SHA-256, moving videos to READY.
