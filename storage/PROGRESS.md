# Phase 5 progress

| Phase | Scope | Status | Verified by |
|---|---|---|---|
| A | Audit and baseline | **DONE** | Real runs: Go 200/200 with PostgreSQL 16 + Redis 7 + `-race`; web 63/63; dbcheck OK |
| B | Config and validation | **DONE** (code) — MinIO/compose not verified | Go 206/206 with PostgreSQL 16 + Redis 7 + `-race`; web 64/64, tsc and lint clean (`baseline/phaseB-*`) |
| C | Retry and state | Not started | — |
| D | Cleanup and reliability | Not started | — |
| E | Frontend | Not started | — |
| F | Integration tests | Not started | — |
| G | OpenAPI, docs, report | Not started | — |

Overall Phase 5: **PARTIALLY COMPLETE** (see `docs/PHASE_5_AUDIT.md`).

## Decisions from the owner (2026-11-14)
- No creator approval step: any signed-in, email-verified user may upload; the first upload makes them a creator
  (existing behaviour, unchanged). Uploads still never auto-publish: publish needs READY + consent declaration.
- `.avi` is dropped. Allowed formats: `.mp4 .m4v .mov .mkv .webm` (all have a magic-byte check: `ftyp` or EBML).
- Per-request age gate on upload routes: not requested, not changed (age is verified at registration).

## Phase B — what changed
Outside storage/ (these must compile into the API/web modules):
- `backend/internal/service/upload.go`: `UploadLimits` + `WithLimits`, limits read from the struct instead of
  constants (defaults identical), `.avi` removed, `.m4v` added, AVI signature case removed, size message states the
  configured maximum.
- `backend/internal/config/config.go`: `UploadConfig`, env `UPLOAD_MAX_BYTES` (1 MiB..5 GiB), `UPLOAD_PART_SIZE_BYTES`
  (5 MiB..1 GiB), `UPLOAD_MAX_OPEN` (1..50), `UPLOAD_SESSION_TTL` (1h..168h); invalid values fail at startup naming
  the variable. Bounds match the DB checks (declared size <= 5 GiB, part size >= 5 MiB).
- `backend/cmd/api/main.go` (wires the limits), `backend/docker-compose.yml` and `backend/.env.example` (the four variables).
- `web/src/config/upload.ts`: extension list.
- Tests: `service/upload_test.go` (+5, 2 edited), `config/config_test.go` (+2), `web/src/config/upload.test.ts` (+1).

Behaviour notes / risks:
- With defaults nothing changes except the allowlist. Presigned part URLs live for half the session TTL (12h by default, as before).
- An `.avi` upload started before this change and completed after it is rejected at `complete` (signature no longer
  accepted). Sessions live 24h, so the window is short; already-processed videos are unaffected.
- The frontend takes the part size from the init response, so a changed `UPLOAD_PART_SIZE_BYTES` needs no web change.
  The web `maxBytes` constant (5 GiB) is only a client-side hint; lowering `UPLOAD_MAX_BYTES` is enforced by the API.
- Not verified: `docker compose` passing the new variables, real MinIO.

## Not verifiable in this environment
Real MinIO/S3, `docker compose up`, Playwright e2e, `next build` (Google Fonts unreachable).
