# B6.3 - media inspection and input policy: what was built and what was proved

Date: 2026-11-19. Scope: ffprobe inspector, input policy, safe error codes, the METADATA job (download -> inspect -> validate -> record -> queue the HLS job, D3).
**Still no transcoding**: after B6.3 an inspected video waits as `PROCESSING` with a queued `HLS` job that no worker consumes until B6.4.

## Built
- `backend/internal/processing/inspect`
  - `probe.go`: `Prober` runs ffprobe from an argument array (no shell). Empty environment (no database or storage credentials), no stdin, `-format_whitelist`
    (mov family, matroska/webm) and `-protocol_whitelist file`, `file:` prefix, 10 MB / 10 s probe limits, output cap (default 4 MiB, the child is killed when it is exceeded),
    hard timeout that kills the whole process group, regular files only. Context cancellation is returned as such and is never a verdict about the file.
  - `parse.go` / `types.go`: typed `Info` (container, duration, real size on disk, streams, codecs, coded and display size, frame rate, pixel format, SAR, rotation, audio rate/layout, bitrate).
    Display size applies SAR to the width and swaps for 90/270 degree rotation. `avg_frame_rate` is preferred over `r_frame_rate` (a timebase artefact for variable-frame-rate files). Cover art is not a video stream.
  - `policy.go`: `Policy` (defaults = database limits + product choices) and `Validate`, cheapest check first. `DurationSecondsInt` rounds up and is the integer finalize will store; `VideoType` uses it.
  - `iso.go`: MP4 top-level box walk and Matroska EBML/Segment size check, because ffprobe does not notice a missing tail when the index is at the front of the file.
  - `codes.go`: 13 safe codes with fixed messages; `Rejection.Detail` (sanitised ffprobe text, for restricted logs) never appears in `Error()` or in the database.
- `backend/internal/processing/metadata.go`: `MetadataProcessor` (+ `Store.SaveInspection`). Checks the video is still `PROCESSING`; compares the storage `Head` size with the recorded size and the policy;
  checks free disk; downloads to a per-job private directory as a file named `input` (no extension, mode 0600) with a cap at the announced size; removes the directory on every exit path;
  records `video_metadata` (display size, fps, codecs, bitrate, container, has_audio) and queues one `HLS` job in one transaction fenced by the lease and idempotent (`ON CONFLICT DO NOTHING` on the active-job index).
- `backend/cmd/worker`: `WORKER_PROCESSOR=metadata` (needs `STORAGE_*` and ffprobe; accepts only METADATA jobs). `WORKER_PROCESSOR` is still mandatory; `noop` is unchanged.
  New variables (all optional): `WORKER_MAX_DURATION`, `WORKER_FFPROBE_TIMEOUT`, `WORKER_FFPROBE_PATH`, `WORKER_TMPDIR`.

## Fixtures (generated at test time with ffmpeg lavfi, nothing binary is committed) and the codes they end in
| Fixture | Result |
|---|---|
| landscape 640x360 H.264+AAC, portrait 360x640, mkv remux, tiny 64x48, silent (no audio) | accepted; sizes and `has_audio` as expected |
| rotation metadata (`-display_rotation 90`) | accepted; coded 640x360, display 360x640, rotation 90/270 |
| SAR 2:1 | accepted; display 1280x360 |
| mp4 content named `.txt` / `.webm`, mkv content named `.mp4`, file name `a b";touch pwned;.mp4` | accepted by content; the name cannot run anything |
| 3840x2160 | `resolution_too_high` |
| 300 fps | `fps_too_high` |
| audio only (m4a) | `no_video_stream` |
| Matroska with 40 audio streams | `too_many_streams` |
| `ftyp` header followed by garbage; EBML header followed by garbage | `corrupt_input` |
| mp4 cut to a third (index in front), mp4 without index, mp4 missing its last 40 bytes, mkv cut to a third | `corrupt_input` |
| text file, empty file | `unsupported_media` |
| HLS playlist or ffconcat script pointing at `/etc/passwd` (named .mp4 or .m3u8) | `unsupported_media` (never followed) |
| policy limits applied to real files: too long, too large, SHORT above its size cap | `too_long`, `too_large`, `short_too_large` |
| fake ffprobe that never exits and leaves a child | `processing_timeout` in about 2 s, the child process is gone |
| fake ffprobe that prints without end | `unsupported_media` within 8 s, memory bounded |
| fake ffprobe that exits 0 but says "File ended prematurely" | `corrupt_input` (control: same JSON without the message is accepted) |

## Evidence (this sandbox: Go 1.22.2, PostgreSQL 16, Redis 7.0.15, ffmpeg 6.1.1)
| Check | Result |
|---|---|
| `go build ./...`, `go vet ./...`, `gofmt -l` | clean |
| Whole suite before B6.3 | 274 PASS, 0 FAIL, 10 SKIP |
| Whole suite after B6.3, `-race`, two consecutive runs | 322 PASS, 0 FAIL, 10 SKIP each (same 10 skips as before) |
| New tests | 48: inspect (parse, policy, fixtures, hostile tools, box walks) and METADATA processor (database, storage stub, runner) |
| Mutation check | 28 of 28 deliberate breakages caught (16 in `inspect`, 12 in the processor); see below |
| Worker role | processor tests run through the `redxtube_worker` pool, so inserts into `video_metadata` and `video_processing_jobs` are checked against its real GRANTs |

## Mutation check
`inspect` (16): stream-count limit; box walk tolerance; demuxer whitelist; protocol whitelist; `file:` prefix; inherited environment; no process-group kill; output flood not stopped; duration rounded down;
`short_too_large` removed; SAR ignored; rotation sign flipped; Matroska segment check; ffprobe truncation hint; cover art counted as video; 4K allowed.
Processor (12): download cap; temp dir not removed; recorded-size check; lease fence in `SaveInspection`; duplicate HLS jobs; deleted video still processed; refusal retried instead of permanent; disk check;
oversized object downloaded; missing object transient; tool failure blamed on the file; input name with extension.
First run: 4 survived (protocol whitelist alone, flood kill, truncation hint, input-name extension) and 1 could not be applied (a typo in the harness's search pattern for the temp-dir mutation). Each survivor was a weak test, not a missing guard; tests were strengthened
(command-line test, an endless output test with a time bound, a fake "ended prematurely" test, a path-recording test) and then all were caught. The mutation script is not committed (it patches a /tmp copy).

## Decisions made here (changeable; the first two are product-visible)
1. **Sources above 1080p are refused** (`resolution_too_high`): the database check `video_metadata_max_1080p` limits the recorded source size to 1920x1080 in either orientation, and the default policy matches it.
   Accepting 4K and downscaling would need a migration that relaxes that CHECK plus a policy change.
2. **Maximum duration 4 hours** by default (`WORKER_MAX_DURATION`, 1 minute to 24 hours; the database allows 24 h). Minimum 0.5 s.
3. Duration is rounded **up** to whole seconds for `duration_seconds` and `video_type` (60.01 s is LONG), the integer the database CHECK compares.
4. Allowed codecs: video h264, hevc, vp8, vp9, av1, mpeg4; audio aac, mp3, opus, vorbis, ac3, eac3, flac, alac, pcm_s16le, pcm_s24le. Containers: the same families Phase 5 accepts (mp4/mov, mkv/webm).
5. Only the first video and first audio stream are judged; other streams only count towards the 16-stream limit.

## Not proved / known limits
- **Truncation detection is structural.** It catches a missing tail of an MP4 or of a Matroska file with a declared segment size. A Matroska recording with unknown segment size, a file whose structure is intact but whose
  media data is damaged inside, or a cut right on a fragment boundary is found only when it is decoded in B6.4. The same applies to fragmented MP4 cut between fragments.
- Real object storage is not exercised: the processor is tested against a stub with the same `Head`/`Download` contract. The S3 client tests that need MinIO are still among the 10 skipped.
- `cmd/worker` with `WORKER_PROCESSOR=metadata` was built and its refusals checked, but not run end to end against MinIO (none available here).
- Nothing consumes the queued HLS job yet; the upload status endpoint still computes progress from four job types (B6.7).
- Rotation direction (clockwise vs counter-clockwise) is derived from ffprobe's documented convention and confirmed only through the display-size swap; B6.4 must verify orientation on a rendered frame.
- ffprobe version differences (output fields) were not tested beyond 6.1.1; parsing tolerates missing and malformed fields.
