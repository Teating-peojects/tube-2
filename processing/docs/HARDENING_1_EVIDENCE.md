# Hardening 1 - subprocess confinement and truncation checks for `internal/worker`

Branch `hardening/worker-subprocess-and-truncation`, created from `feat/media-worker` at `3ae0deb` (2026-11-19 21:55 UTC). It closes gaps 1-5 of
`processing/docs/AUDIT_feat_media_worker.md` (branch `processing/audit-media-worker`). If `feat/media-worker` moved on, rebase before merging.

## Changes
New files (no conflict risk): `internal/worker/subproc.go`, `container.go`, `hardening_test.go`.
Edited files: `probe.go` (ProbeFile), `ffmpeg.go` (args, runFFmpeg), `scan.go` (ExecScanner), `pipeline.go` (two lines in `original()`), `phase6_test.go` (one assertion, see below).

| Gap in the audit | Change |
|---|---|
| 1. children inherit the worker's environment (database, storage credentials) | `isolate(cmd)`: own process group + environment of only `LC_ALL=C` and a fixed `PATH`; used for ffprobe, ffmpeg and the scanner. `ExecScanner.Env` lists the only extra `KEY=VALUE` pairs a scanner receives |
| 2. no demuxer whitelist; unbounded ffprobe stdout; no timeout of its own | `-format_whitelist` (exactly the formats `allowedFormats` accepts) on ffprobe, the HLS encode and the thumbnails; ffprobe stdout capped at 4 MiB (the child is killed when exceeded: permanent `invalid_file`); `probeTimeout` 60 s that kills the process group (transient `ffprobe_failed`) |
| 3. local copy named `original<ext>`; path without `file:` | the local copy is `original` (no extension, mode 0600); `file:` prefix on every `-i` |
| 4. truncated files pass ffprobe and are retried as `ffmpeg_failed` | MP4/MOV top-level box walk and Matroska/WebM segment-size check, plus ffprobe's "ended prematurely" message: permanent `invalid_file` at the METADATA step |
| 5. unbounded ffmpeg stderr | `tailWriter` keeps only the last 2 KiB (scanner output: 8 KiB) |
No new failure codes: `invalid_file` already has a user-facing message in `service/processing_messages.go`, and `ffprobe_failed` already maps to the `ffprobe` metrics kind.

## Evidence (sandbox: Go 1.22.2, PostgreSQL 16 built from the branch's own migrations, Redis 7.0, ffmpeg 6.1.1)
| Check | Result |
|---|---|
| `go build`, `go vet ./...`, `gofmt -l` | clean |
| Whole backend before the change (`feat/media-worker`) | 325 PASS, 0 FAIL, 10 SKIP |
| Whole backend after, `-race`, two consecutive runs | 339 PASS, 0 FAIL, 10 SKIP each; the same 10 skips: 8 S3 client tests, `TestWorkerSmoke`, `TestPlaybackEndToEnd` (they need `TEST_S3_ENDPOINT`, not available here) |
| New tests | 14 (4 children-environment cases incl. ffprobe, ffmpeg and scanner with explicit pass-through; hung probe killed with its child process; endless output; cancellation is not a verdict; bounded writers; truncation message; command-line whitelists; 6 truncated or damaged real files and 3 good controls; hand-made MP4 box and EBML cases; local original name and permissions) |
| Mutation check | 17 of 17 deliberate breakages caught |

## A real bug found by the tests
`ParseProbe` labels a Matroska file `mkv` (not `webm`) unless it is called `.webm`. My first version of the Matroska check only ran for `webm`, so it **never ran for `.mkv` files**. The mutation "Matroska walk disabled" survived, the fixture was
strengthened (a clip large enough that ffprobe still opens it when cut), the real failure appeared ("mkv cut to a third: accepted"), and the match was fixed to `webm` and `mkv`.

## Mutation check (breakages in the changed files, then the tests)
child keeps the environment; no process-group kill; no demuxer whitelist for ffprobe; no protocol whitelist for ffprobe; ffprobe path without `file:`; ffmpeg input without `file:`; thumbnails without the demuxer whitelist;
flood not killed; stderr tail unbounded; ffprobe truncation message ignored; MP4 box walk disabled; Matroska segment walk disabled; local original keeps its extension; scanner without explicit environment;
probe timeout blamed on the file; box walk accepts a box running past the end; EBML header bound dropped. The mutation script is not committed (it patches a /tmp copy).

## Behaviour changes a reviewer should know about
1. **A truncated or damaged file now fails at METADATA** as permanent `invalid_file` (creator sees "may be damaged") instead of failing three times in FFmpeg as `ffmpeg_failed`.
2. **A hung ffprobe** ends after 60 s as a retryable `ffprobe_failed` (it used to be bounded only by the job).
3. **The scanner no longer sees the worker's environment.** A scanner that needs variables must have them listed in `ExecScanner.Env`; **`cmd/worker` has no setting for that yet** (a follow-up, e.g. a `SCANNER_ENV` variable), so a scanner that depends on its environment would stop working until that exists. No scanner is configured by default.
4. The local copy is now `original` instead of `original.mp4`. A worker restarted in the middle of a video simply downloads it again (cache miss).
5. `phase6_test.go`: `TestPipelineWaitsInsteadOfFillingTheDisk` stat'ed `original.mp4`; with the new name that check would pass vacuously, so it now asserts the work directory is empty.

## Not done (still open from the audit)
- CPU/memory limits or `nice` for FFmpeg; real sandboxing of the worker container (seccomp/AppArmor review).
- A hostile-media corpus beyond these fixtures (decompression bombs, pathological containers).
- Truncation is found structurally: a Matroska file of unknown segment size, damage inside intact media data, and a cut exactly at a fragment boundary are still found only when FFmpeg decodes them.
- Nothing here was run against a real S3/MinIO endpoint; the S3-dependent tests remain skipped in this sandbox.
