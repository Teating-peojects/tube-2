# Baseline runs (Phase A) — what was run, how, and what it does not prove

Run on 2026-11-14 in a sandbox (Ubuntu 24, Go 1.22.2, Node 22, PostgreSQL 16, Redis 7 installed via apt).
The repository itself was NOT modified; Go and web checks ran on copies in /tmp.

| Check | Result | Evidence file |
|---|---|---|
| `go build ./...`, `go vet ./...`, `gofmt -l .` | pass, no output | (console) |
| `go test -count=1 ./...` (no TEST_* vars, integration skipped) | all packages ok | go-test-unit.txt |
| `go test -race -count=1 ./...` with real PostgreSQL 16 + Redis 7 (`TEST_*` set) | all packages ok; 200 tests PASS, 0 FAIL, 0 SKIP | go-test-integration-race.txt |
| `db/scripts/test-db.sh` (roles, 10 migrations, reference data) | pass | (console) |
| `python3 db/tools/dbcheck.py` | OK: 10 migrations, 39 tables, 30 enums, 29 functions, 80 indexes | (console) |
| web `tsc --noEmit` | pass | web-typecheck.txt |
| web `vitest run` | 13 files, 63 tests pass | web-vitest.txt |
| web `next lint` | pass, 2 warnings (unused vars in services/mock/auth.ts) | web-lint.txt |
| web `next build` | FAILED in sandbox: cannot reach fonts.googleapis.com (next/font). Environmental, not a code result | web-build.txt |

## Not verified (no infrastructure available here)
- MinIO / any real S3 endpoint: Docker and the MinIO binary are unavailable. The storage client is only tested
  against an in-process fake S3 (`internal/storage/storage_test.go`).
- `docker compose up` (compose file, bucket init, health): not run.
- Playwright e2e (needs browsers + the mock API): not run.
- `next build`: blocked by sandbox network (see above).

## Sandbox workarounds (do not apply to your environment)
- `golang.org/x/*` and `gopkg.in/*` are unreachable here. The Go build copy in /tmp used `replace` directives to
  github.com mirrors and empty stubs for test-only `gopkg.in` modules. Your `go.mod` is unchanged.
- `db/scripts/*.sh` are committed without the executable bit (see audit finding F1), so the DB copy in /tmp was
  `chmod +x` before running.
