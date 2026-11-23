# Mutation testing of the worker's own suite, and the tests added for what it found

Branch `p6/tests-mutation-gaps`, created from `feat/phase6-processing` at `a6d1394` (the same tests as `tests/mutation-gaps` on the `feat/media-worker` line, minus the gateway test, because that line alone has `internal/stream`). It adds tests only; no production code is touched, so it merges cleanly with `feat/media-worker`
and with `hardening/worker-subprocess-and-truncation`.

## Method
49 deliberate breakages ("mutants") of guards in the worker's code, one at a time, then the tests of the affected packages (`go test -failfast`, PostgreSQL 16 built from the branch's own migrations).
Files: `worker/pg.go` (queue, retry, fencing, blocklist at finalize), `worker/validate.go` (playlist and master validation), `worker/policy.go`, `worker/ladder.go`, `worker/pipeline.go` (scanner, verification,
blocklist, disk, cache), `service/processing.go` (status authorization), `playback/playback.go` (token verification), `stream/stream.go`. Four mutants did not compile and were replaced by compilable variants.
The selection is mine and aims at security and limit checks; it is a probe, not a complete measure of the suite.

## Result before the new tests: 30 of 49 caught
19 survived. Judged one by one against the code:

| Survivor | Verdict |
|---|---|
| **S1** any signed-in user can read any video's processing status (owner/staff check removed) | **real gap**: there was no service-level test of the processing status, cancel or retry at all |
| **S2** a removed video shown as normal | real gap (same) |
| **W37** stored HLS segments and playlists never verified after upload | real gap: only thumbnails were covered; segments, rendition playlists and the master were not |
| **W32** stored size checked only for "bigger", not for "smaller" than sent | real gap: a short object (a truncated upload) was the case that mattered and the test only inflated sizes |
| **W21** long side of the picture not limited (e.g. 2000x500) | real gap: only the short side was tested; such a video would be left to the database `video_metadata_max_1080p` CHECK instead of the clear "resolution too high" answer |
| **W15** a playlist with no segments accepted for sources up to 2 s | real gap |
| **W17** a master variant without BANDWIDTH accepted | real gap |
| **W20, W22, W25** duration, minimum side and SHORT size limits one off | real gaps (boundaries untested) |
| **W28** zero width reported as "too small" instead of "no video" | real gap (wrong message for the creator) |
| **W38** a cached original of the wrong size trusted | real gap |
| **W1** claim ignores the attempt budget | real gap (the database CHECK turns it into an error on every poll instead of a quiet skip) |
| **P8** the playback gateway serves any HTTP method | real gap |
| W12 fps above 240 reaches the database | equivalent: `Policy.Check` already limits `MaxFPS` to 240 |
| W18 `len(want) != 0` removed from the master check | equivalent: a duplicated reference already fails as "unknown playlist", so the condition is implied |
| W29 `target > short` changed to `>=` in the ladder | equivalent: both give the same rung size |
| P4 object-key id not re-validated as a UUID | equivalent: `objectRe` already enforces the UUID shape |
| P5 token with an empty signature part | equivalent: an empty signature fails the HMAC comparison |

## Tests added (10 here; 11 on the `feat/media-worker` line)
- `service/processing_test.go` (4): who may read the status (owner, moderator, admin yes; stranger, deleted video, malformed id, unknown video no, always as "target not found"); the status mapping for every video state
  (removed beats published, progress capped at 99, retry offered to staff only, message only with a code); cancel (owner vs staff, audit entry with hashed IP, bad reason, 20 per minute, limiter failure fails closed); retry (staff only, reason required, 30 per minute).
- (`stream/methods_test.go`, only on the `feat/media-worker` line: only GET and HEAD are served.)
- `worker/mutation_gaps_test.go` (6): policy boundaries (18 cases), playlist validation (13), master validation (8), every stored output verified in both directions for segments, rendition playlists, thumbnails and the master (with an honest-storage control), cached original of the wrong size, claim skips a job with no attempts left.

## Evidence
| Check | Result |
|---|---|
| New tests against the unmodified code of `feat/media-worker` | pass |
| The 14 real-gap mutants re-run against the new tests | **14 of 14 caught**; the 5 equivalent mutants stay alive, as they should |
| Whole backend, `-race`, two consecutive runs | 336 PASS, 0 FAIL, 10 SKIP each (was 325 PASS, 10 SKIP); the 10 skips are the S3-dependent tests (`TestWorkerSmoke`, `TestPlaybackEndToEnd`, 8 S3 client tests) |
| `go vet`, `gofmt -l` | clean |

## Limits
- 49 hand-chosen mutants, not an exhaustive run; other files (thumbnail selection, scanner exec, metrics, realtime, handlers) were not mutated here.
- The S3-dependent tests are still skipped in this sandbox, so nothing here touches real object storage.
- The mutation script patches a /tmp copy and is not committed.
