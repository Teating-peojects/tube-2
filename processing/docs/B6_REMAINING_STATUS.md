# B6: what was still open, what was closed, what remains

Branch `b6/scanner-env-and-limits`, stacked on `hardening/worker-subprocess-and-truncation` (it uses `isolate`, `tailWriter` and `ExecScanner.Env` from there).
Related branches, all created from `feat/media-worker`: `tests/mutation-gaps`, `fix/cleanup-test-hls-prefix`, `processing/audit-media-worker`.

## Closed in this branch
| Open item | Change |
|---|---|
| a scanner that needs environment variables could not get them (follow-up of the hardening) | `SCANNER_ENV` (comma-separated `KEY=VALUE`, validated: names, duplicates, control characters, at most 32). Refused without `SCANNER_COMMAND`. The worker's own environment still never reaches the scanner |
| no CPU or memory limit for ffmpeg | `FFMPEG_NICE` (default 10) lowers the priority of every ffmpeg run; `FFMPEG_MEMORY_MB` (default off, minimum 512) caps its address space. Applied with `setpriority`/`prlimit` right after the child starts; a limit that cannot be applied does not fail the encode |
| the scanner hook was only tested with fake scanners | `TestExecScannerWithARealClamscan` runs the real ClamAV binary with a signature built at test time: infected file reported, clean file passes, a scanner that cannot run is an error (fail closed). Skipped when `clamscan` is absent |
| the ten S3-dependent tests had never run here | run against the `moto` S3 emulator (`pip install "moto[server]"`): **`TestWorkerSmoke` and `TestPlaybackEndToEnd` pass** (a real worker process, PostgreSQL, S3 API, ffmpeg, signed playback), and 5 of the 8 storage client tests pass |

## Evidence (sandbox: Go 1.22.2, PostgreSQL 16, Redis 7.0, ffmpeg 6.1.1, ClamAV 1.5.4, moto 5.2.5)
| Check | Result |
|---|---|
| default suite (`-race`, no S3 emulator), two consecutive runs | 344 PASS, 0 FAIL, 10 SKIP |
| same suite with moto (`TEST_S3_*` set) | 350 PASS, 3 FAIL, 0 SKIP |
| the 3 failures | `TestS3AbortedUploadRejectsParts`, `TestS3ExpiredPresignedURLAnswers403`, `TestS3RejectsWrongCredentials`: moto does not enforce credentials or presigned-URL expiry and answers an aborted part with HTTP 500. They test what real S3/MinIO must do, so **they stay unverified until run against MinIO or S3**; this is an emulator limit, not evidence of a code bug |
| mutation check of the new code | 9 of 9 caught (niceness and memory limit not applied; limits skipped by the runner; a thumbnail and an encode call site without limits; duplicate names, invalid names, variable count and control characters accepted by `ParseScannerEnv`) |
| `go vet`, `gofmt -l` | clean |

How to reproduce the S3 run: start `moto_server -p 9000`, create bucket `redxtube` with `boto3`, then
`TEST_S3_ENDPOINT=http://127.0.0.1:9000 TEST_S3_ACCESS_KEY=minioadmin TEST_S3_SECRET_KEY=minioadmin TEST_S3_BUCKET=redxtube go test ./...`.

## B6 items that remain open
1. **Unique temporary prefix with atomic promotion (the report's decision D5)** is not built. The storage client has no server-side copy, so the usual "write elsewhere, then rename" is not available; the practical design is a versioned
   output prefix plus an atomic switch of `hls_master_key` in the finalize transaction (with delayed cleanup of the old version). Not started: it touches the schema, finalize, cleanup and playback.
2. **Docker and compose were not run** (no Docker in this sandbox): `Dockerfile.worker`, the compose service, read-only root and dropped capabilities are unverified.
3. **Real S3/MinIO**: the 3 tests above, and any behaviour that depends on real signature and expiry enforcement.
4. **A production scanner**: the exec contract is verified against ClamAV with a test signature only. Choosing the engine, running `freshclam` for signature updates and deciding on a CSAM hash-matching service are operations and product decisions.
5. `FFMPEG_MEMORY_MB` ships **off**: a safe value needs measurements on real encodes. A hostile-media corpus beyond the generated fixtures (decompression bombs, pathological containers) is also still to be built.
6. Merging: five branches wait for review; any order works (they were merge-tested together on `230bee8` without conflicts), `fix/cleanup-test-hls-prefix` first is the natural order because it repairs a failing test.
