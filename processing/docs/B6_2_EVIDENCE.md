# B6.2 - job lifecycle: what was built and what was proved

Date: 2026-11-19. Scope: queue state machine, lease/heartbeat, retry, cancel, stale recovery, `cmd/worker` skeleton. **No FFmpeg, no real processor.**

## Built
- `db/migrations/000013_processing_lifecycle` (+ down): `processing_version`, `stage`, `progress`, `error_code`, `cancel_requested_at` on `video_processing_jobs`, each with a CHECK
  (error codes are `^[a-z][a-z_]{0,39}$`, so free text cannot be stored by mistake).
- `backend/internal/processing`: `state.go` (transition table, `Backoff`, `PermanentError`), `store.go` (`Claim`, `Heartbeat`, `Complete`, `Fail`, `Release`, `Cancel`,
  `RecoverStale`, `FailOrphans`), `worker.go` (`Runner`: concurrency, heartbeat goroutine, cancel/lease-lost handling, panic containment, graceful shutdown; `NoopProcessor`).
- `backend/cmd/worker`: refuses to start without `WORKER_PROCESSOR=noop` (the no-op completes jobs without processing videos). Connects with `WORKER_DATABASE_URL` as `redxtube_worker`.
- `backend/.env.example`: worker variables (commented). `internal/handler/fixtures_test.go`: shared database advisory lock (see "Existing test file touched").

## Evidence (all run in this sandbox, Go 1.22.2, PostgreSQL 16, Redis 7.0.15 from apt)
| Check | Result |
|---|---|
| `go build ./...`, `go vet ./...`, `gofmt -l` | clean |
| Whole suite before B6.2 | 251 PASS, 0 FAIL, 10 SKIP |
| Whole suite after B6.2, `-race`, two consecutive runs | 274 PASS, 0 FAIL, 10 SKIP each (the same 10 skips: 8 S3 tests need MinIO, 2 OpenAPI route/code tests) |
| New tests | 23: 3 unit (transition table, backoff, jitter/floor), 20 integration (store + runner) against real PostgreSQL |
| Store tests run as the worker role | pool runs `SET ROLE redxtube_worker`, so every query is checked against its real GRANTs |
| Migration | `down 1` -> 0 of the 5 columns, `up` -> 5 (reversible); `dbcheck.py`: 13 migrations, 39 tables, OK |
| Worker binary | refuses to start without a processor (exit 1); with `noop` and a direct login as `redxtube_worker` it completed a job (attempts 1, progress 100) and exited 0 on SIGTERM |
| Concurrency | 10 claimers over 60 jobs: each job claimed exactly once; 25 rounds of 2 claimers on 1 job: exactly one winner; 2 runners x 3 workers drained 40 jobs, each processed once |
| Recovery | killed worker's lease expires -> job re-queued, completed by another worker on attempt 2; a revived stale worker cannot complete it (fencing) |

## Mutation check (deliberate breakages in `store.go`, then the tests)
10 of 10 caught: no worker fence on Complete; no worker fence on Heartbeat; attempt budget ignored by Claim; deleted videos claimable; retries never end; progress can go
backwards; stale recovery ignores cancel; release does not refund; orphans of deleted videos kept; **no SKIP LOCKED**.
The last one first **survived**: under READ COMMITTED a claimer that waits still ends with another job, so the guard is about liveness, not correctness. A test was added
(`TestClaimDoesNotWaitForARowThatAnotherTransactionHolds`) and the mutation is then caught. The mutation script is not committed (it patches a /tmp copy).

## Existing test file touched
`internal/handler/fixtures_test.go` now takes `pg_advisory_lock(746100)` once per test (re-entrant via `sync.Map`; the first version deadlocked a test that calls `setup` twice and was
caught by running the whole suite). Reason: handler tests TRUNCATE every table and `go test ./...` runs packages in parallel, so `internal/processing` tests would lose their rows.
Both packages take the same lock; no CI change is needed.

## Not proved / known limits
- No video is processed yet. Jobs are never created for HLS (D3 flow: the METADATA job should enqueue the HLS job; B6.3+). The status endpoint still computes progress from 4 job types (B6.7).
- A failing heartbeat query (database blip) only logs and retries on the next tick; this path has no test. Lease and heartbeat relation is not validated in `Options` (`cmd/worker` enforces lease >= 10 s, heartbeat defaults to lease/4).
- `FailOrphans` (asks a running job of a deleted video to stop) and the runner's cancel handling are tested separately, not as one end-to-end flow.
- No Dockerfile or compose service for the worker yet (planned with B6.4/B6.8). Redis is not used by the worker yet (`redx:rt` ping comes with finalize, B6.6).
- Wall-clock tests (retry waits ~2.4 s, panic ~1.4 s) could flake on a very slow machine; timeouts are generous (10-20 s).
- The 10 skipped tests are unchanged and unrelated to B6.2.
