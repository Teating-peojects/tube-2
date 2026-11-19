# Master prompt — Backend phases B6 to B10 (REDXTUBE / xhamtube)

Repository: `Teating-peojects/tube-2` (Go API, PostgreSQL 16, Redis, S3-compatible storage, Next.js web).
Written on 2026-11-14 after Phase 5 (upload and storage) was finished and pushed. **This file is a plan and a prompt. It does not
start any work.** Work starts only when the owner says so, one phase at a time.

Where the phase documents in the repository root disagree with the B-numbers of the README, this file is the map:

| README | Scope | Phase document (repo root) |
|---|---|---|
| **B6** | processing worker: ffprobe, FFmpeg, HLS, thumbnails, job queue | `xhamtube_phase_6_video_processing_hls.md` |
| **B7** | secure playback, signed HLS, player | `xhamtube_phase_7_secure_hls_streaming_player.md` |
| **B8** | notifications, studio, realtime (built earlier; never audited) | `xhamtube_phase_10_...md` section 8 (notifications), studio parts of phase 8 |
| **B9** | moderation and admin | phase 6 section 15, phase 7 section 7 |
| **B10** | hardening: scanning, sandbox, load, metrics, security review | phase 6 section 10, 11, 19; phase 7 section 10 |

(`xhamtube_phase_8_...` is the frontend-backend integration phase and `xhamtube_phase_10_...` the social phase. They are not
B8 and B10; they are inputs and a later closing step, see section 11.)

---

## 0. How to use this prompt

1. Read this file, `instruction.txt`, `README.md`, `storage/docs/PHASE_5_COMPLETION_REPORT.md` and the phase document of the
   phase you are about to do. Do not rely on any of them being true: verify against the code (rule 4 of `instruction.txt`).
2. Do **one sub-phase at a time** (sections 6 to 10). Finish it (implementation, real tests, mutation checks, docs, push), report,
   and **stop at every point marked `DECISION`** until the owner answers.
3. Keep all notes, evidence and progress in `processing/` (same layout as `storage/`: `PROGRESS.md`, `REMAINING_TASKS.md`,
   `docs/`, `baseline/`). Code goes where Go requires it (`backend/`, `db/`, `web/`); list those edits per sub-phase in `PROGRESS.md`.
4. Talk to the owner in **Bengali**. Code, comments, documents, commit messages: English.

## 1. Non-negotiable rules (inherited, they held for Phase 5 and still hold)

- Verify, do not trust: a document, a README row or an earlier report is a claim, not a fact. Existing behaviour and contracts
  win: extend, do not rewrite; no breaking changes to APIs, enums or migrations that exist.
- A feature is **verified** only if a test or a real run you executed proves it. Use the labels COMPLETE / PARTIALLY COMPLETE /
  BLOCKED / NOT IMPLEMENTED. Say plainly what was not run (real MinIO, real browser, Docker, GitHub Actions ...).
  **Never claim production readiness.**
- Tests must be able to fail. After writing a test for a guard, break the guard on a copy and watch the test fail (Phase 5 did this
  27 times and it found a real bug each time a stub hid it). Prefer real PostgreSQL, a real process, a real file over a mock.
- Fixtures are generated, never downloaded (ffmpeg `lavfi` sources); tiny files, deterministic.
- Never hard-code, log or commit secrets, presigned URLs, tokens or object keys of users. Fixed user-facing messages per error
  code; internal detail only in restricted logs.
- Everything user-controlled is hostile: filenames, media content, metadata, object keys, client-supplied ids.
- Migrations are additive and reversible (`.up.sql` + `.down.sql`, tested: up, down, up, down twice, up) and run through `db/tools/dbcheck.py`.
- Every sub-phase ends with: full Go suite with `-race` on real PostgreSQL/Redis, web suite if `web/` changed, `gofmt`, `go vet`,
  evidence files in `processing/baseline/`, updated docs, then a commit and push of **that sub-phase only**.

## 2. Verified starting point (2026-11-14)

### 2.1 What Phase 5 delivered and verified
Upload init, direct-to-storage multipart upload, complete, status, retry (expired uploads), cancel, assets (thumbnail/caption
presign), draft metadata, consent-gated publish, storage sweeper with backoff, optional storage health check, in-process counters,
OpenAPI (`storage/openapi.yaml`) with a drift test. Evidence: Go 253 tests pass with `-race` on PostgreSQL 16 + Redis 7 (+ 8 S3
tests skipped without a server), web 108 pass, 12 migrations / 39 tables. 8 defects were found and fixed along the way.
Docs: `storage/docs/PHASE_5_*.md`, final status `PHASE_5_COMPLETION_REPORT.md` (**PARTIALLY COMPLETE**).

### 2.2 What already exists for B6 to B10 (checked in the code)
- `video_processing_jobs` (migration 000003): `job_type` in (`METADATA`, `TRANSCODE`, `HLS`, `THUMBNAIL`), `job_status` in
  (`QUEUED`, `PROCESSING`, `COMPLETED`, `FAILED`), `attempts`/`max_attempts` (default 3), `worker_id`, `heartbeat_at`,
  `scheduled_at`, `error_message` (max 2000), unique active job per `(video_id, job_type)`, indexes made for
  `FOR UPDATE SKIP LOCKED` (`video_jobs_queue`) and stale-lease recovery (`video_jobs_stale`).
  **Phase 5 enqueues exactly one `METADATA` job per completed upload; nothing consumes it. There is no `cmd/worker`, no FFmpeg code.**
- `videos`: status enum (`UPLOADING`, `PROCESSING`, `READY`, `PUBLISHED`, `FAILED`), `moderation_status` (`OK`, `RESTRICTED`, `REMOVED`),
  `hls_master_key`, `duration_seconds`, `video_type`, `content_sha256` (migration 000010), CHECK `videos_ready_requirements`
  (READY needs type + duration + HLS master).
- `video_variants` (per resolution 360p/480p/720p/1080p: width, height, bitrate, codec, `storage_key`, `manifest_path`, size, status).
- `internal/playback`: HMAC token over `(videoID, expiry)` + `Verify` + object-key allow-list. **Not wired anywhere**, no
  `PLAYBACK_SIGNING_KEY` in the config, the token is not bound to a session or user.
- Moderation/admin API (B9, no UI): reports queue (list, assign, resolve), actions, restore video/user, blocked hashes
  (list/add/remove), audit log, role change. Schema has the hash blocklist trigger (migration 000010) - **not re-verified in Phase 5**.
- Notifications, studio, WebSocket realtime (B8): `useRealtime("video:<id>")` on the web side already refreshes upload status.
- The web app already depends on `hls.js`.
- Tooling in the sandbox: `ffmpeg`/`ffprobe` 6.1.1 are installed (`libx264` and the `hls` muxer are present; **verify `aac`, `libx264`
  presets and `lavfi` first, do not assume**). Docker, MinIO and browsers are not available.

### 2.3 Mismatches to reconcile in the audit (do not silently pick a side)
| Document says | Code has | Default proposal (owner confirms) |
|---|---|---|
| terminal processing state `READY_FOR_REVIEW`, "processing success must not publish" | video `READY`, publish needs the owner's consent | keep `READY`; it already means "processed, private, waiting for the owner/moderation" |
| job states `RUNNING`, `RETRY_WAIT`, `SUCCEEDED`, `CANCELLED` | `PROCESSING`, `COMPLETED`, `FAILED` | keep the enum; express retry with `scheduled_at`, cancel with a column or a new enum value added by migration |
| fine-grained stages (`GENERATING_THUMBNAILS`, `UPLOADING_OUTPUTS`, ...) | none | a `stage` text column + `progress` on the job, not new video statuses |
| one job per video | four job types | decide: one pipeline job vs a chain `METADATA -> TRANSCODE -> HLS -> THUMBNAIL` (see B6.1) |
| outputs under `videos/<id>/processing/<version>/` | the sweeper only knows `uploads/`, `thumbs/`, `captions/` | extend `freeStorage` with the new prefix, tested |
| playback by token | signer unused | design in B7.1 |

## 3. Environment lessons from Phase 5 (they will bite again)
- Background processes do **not** survive between tool calls in the sandbox: start PostgreSQL, Redis, emulators and the test in
  **one** script, kill by PID (never `pkill -f <word that is in your own command line>`).
- `golang.org/x/*` and `gopkg.in/*` are unreachable: build in a copy with `replace` directives to github mirrors and stubs
  (`storage/baseline/REPRODUCE.md`). `db/scripts/test-db.sh` prints only the three `export` lines on stdout.
- `next build` cannot run (Google Fonts), Playwright browsers are missing, so a Playwright spec cannot be verified here: do not add
  one that CI would run unverified; say so.
- Git: the owner pastes a fine-grained GitHub token in the chat. Use it per command (`-c http.extraheader=...`), never write it
  to a file, `.git/config`, memory or logs, and remind the owner to revoke it. Pushing `.github/workflows/*` needs the token's
  **Workflows** permission; keep such commits on a separate branch (`ci-s3-job` is waiting) and use `git add <paths>`, never `git add -A`.
- Real S3/MinIO is not available; emulators (`s3rver`, `moto`) disagree with each other. Anything that depends on exact S3
  behaviour is PARTIAL until run on MinIO/S3.

## 4. Method of every sub-phase
1. **Audit** the part of the code the sub-phase touches (read, run, list what is real). Write findings in `processing/docs/`.
2. **Plan** in a few lines, list the files, flag the `DECISION`s.
3. **Implement** small, additive, behind existing contracts.
4. **Test for real**, then **mutation-check** every important guard.
5. **Document**: what changed, behaviour notes and risks, what was not verified.
6. **Report** to the owner (Bengali): what was built, results (numbers from actual runs), what is not proven, next step.
7. **Push** only that sub-phase.

## 5. Decisions the owner must make (ask when the sub-phase that needs it starts, one at a time)
| # | Decision | Needed by | Default proposal |
|---|---|---|---|
| D1 | Review gate: owner publishes after `READY` (today) or a moderator must approve first? | B6.1 / B9 | keep owner-publish + post-publication moderation, add a pre-publication queue only for flagged accounts |
| D2 | HLS segment format: MPEG-TS or fMP4 | B6.1 | MPEG-TS (widest support, simplest); revisit if encryption/low latency is wanted |
| D3 | One pipeline job per video or a chain of the four job types | B6.1 | one worker, one `HLS` job that does probe, transcode, package, thumbnails; `METADATA` job triggers it |
| D4 | Playback delivery: backend gateway streaming from storage, or signed URLs to storage/CDN | B7.1 | gateway first (revocation and logging are exact); signed URLs only when a CDN is real |
| D5 | Age check on every upload/playback request, or at registration only | B7 / B10 | on every playback request (server-side); upload stays as is unless the owner wants it |
| D6 | Scanning: which provider/hash list, what happens on a match (quarantine, report) and who is legally responsible | B10.1 | adapter interface + quarantine state; no scanner is built in-house |
| D7 | Metrics exposure: scrape endpoint behind auth / internal network, or logs only | B10.3 | internal-only `/metrics` on a separate listener |
| D8 | Retention of the original after a successful transcode | B6.5 | keep (configurable), never auto-delete |
| D9 | Admin/moderation UI inside this repository or API only | B9 | API + a minimal read-only queue page after B7 |

---

## 6. B6 — Processing worker (FFmpeg, HLS, thumbnails)
Goal: a completed upload becomes a private, validated, adaptive HLS package plus thumbnails, with an accurate, retry-safe status,
and nothing becomes public by itself. Outcome states: `PROCESSING` -> `READY` (or `FAILED` with a safe code).

### B6.1 Audit and design record
- Audit: `video_processing_jobs` usage, `videos` constraints, `video_variants`, the storage client (does it stream large objects? `Download`
  writes to an `io.Writer`, `Put` takes a reader; list what is missing), config, where a worker binary would live, `docker-compose`.
- Decide and write down (a short ADR): queue (Postgres `SKIP LOCKED` over the existing table: **no second queue system**), job model
  (D3), lease and heartbeat (`worker_id`, `heartbeat_at`, `video_jobs_stale`), retry (exponential backoff + jitter via `scheduled_at`,
  `max_attempts`, permanently failed = `FAILED` + safe code), cancellation, `processing_version`, output layout (versioned, e.g.
  `videos/<id>/processing/<version>/hls/...`), HLS format (D2), profile ladder and its versioning, how states map (section 2.3).
- Verify the tool: `ffmpeg -encoders` has `libx264` and `aac`; `-muxers` has `hls`; generate one clip with `lavfi` and run ffprobe on it.
- Done when: the ADR and the audit exist, decisions D1-D3, D8 are answered, the migration list is known. No product code yet.

### B6.2 Job lifecycle (no FFmpeg yet)
- Migration(s): `processing_version`, `stage`, `progress`, safe `error_code`, cancel flag/state, anything D1/D3 require, plus a `processing_*`
  summary on `videos` only if the audit shows it is needed. Reversible, tested.
- `internal/processing` (or similar): central transition table that **rejects invalid transitions**, `Claim` (`FOR UPDATE SKIP LOCKED`, sets
  worker id and lease), `Heartbeat`, `Complete`, `Fail` (retry or permanent), `Cancel`, stale-lease recovery, idempotent re-delivery.
- `cmd/worker`: starts, claims, heartbeats, shuts down gracefully (finishes or releases the job), configurable concurrency; the API
  binary is unchanged.
- Verify on real PostgreSQL: two and ten concurrent claimers never run the same job; a killed worker's job is re-queued after the
  lease; retry backoff and the maximum are honoured; double `complete` is a no-op; every illegal transition is refused; the
  existing `METADATA` job from Phase 5 is consumed exactly once.
- Done when: a no-op processor (sleeps, then completes) drives jobs through the full state machine under concurrency and crashes.

### B6.3 Media inspection and input policy
- `ffprobe` through `exec.CommandContext` with an **argument array** (never a shell), JSON output parsed into a typed struct: container,
  duration, size, streams, codecs, width/height, frame rate, pixel format, SAR/DAR, rotation (side data and tags), audio rate/layout, bitrate.
- Configurable policy: max size/duration/dimensions/frame rate/streams, allowed containers and codecs, max processing time and temp disk.
  Violations produce a **safe error code** and a fixed message; raw ffprobe/ffmpeg output only in restricted logs (sanitised, no paths).
- Handle: no video stream, no audio stream (silent video is valid), rotation, non-square pixels, absurd claimed dimensions, truncated or
  corrupt files, files whose extension lies.
- Verify with generated fixtures: landscape, portrait, rotation metadata, silent, SAR != 1, tiny (below every rendition), a file with the
  right signature and garbage inside, a text file renamed `.mp4`, a stream-count bomb, a long duration. Each ends in the expected code.
- Done when: the inspector classifies every fixture correctly and no fixture can make it hang or exceed the limits.

### B6.4 Transcoding and HLS packaging
- Versioned, configurable profile ladder (initial 360p/480p/720p/1080p, H.264 + AAC, `yuv420p`, GOP aligned with the segment duration;
  the numbers are defaults, not promises). **Never upscale**; skip renditions larger than the source; preserve aspect ratio; correct for
  rotation and SAR; no needless frame-rate increase; keep A/V sync; support silent input.
- HLS output per D2: one media playlist per rendition, a master playlist with correct `BANDWIDTH`/`RESOLUTION`/`CODECS`, equal segment
  durations, keyframes aligned across renditions, relative paths valid for the later playback gateway.
- Validate before anything is uploaded: parse every playlist, every referenced segment exists and is non-empty, durations add up
  to the source duration within a tolerance, each rendition is decodable (`ffprobe` the output, sample-decode a few seconds).
- Verify: generated landscape, portrait and rotated inputs come out with the right orientation and aspect ratio (check width/height
  and a rendered frame), silent input has no audio stream and plays, a 240p source yields only renditions <= its size, a corrupt segment
  is detected by the validator (inject one).
- Done when: the package for each fixture passes the independent validator and ffprobe.

### B6.5 Thumbnails, storage integration and atomic publication
- Poster (+ optional previews): a frame that is neither the first nor black where practical, short videos handled, rotation respected,
  JPEG validated, dimensions recorded. Previews and any sprite format documented.
- Uploads through the existing storage abstraction (stream, do not hold files in memory; bounded retries on transient errors; no presigned
  URLs or credentials in logs): write to a unique temporary prefix, verify every object (existence, size), then **atomically** finalize
  in one database transaction: `video_variants` rows, `hls_master_key`, `duration_seconds`, `video_type`, `content_sha256`, poster,
  video `PROCESSING -> READY`, job `COMPLETED`. Never `READY` before every output is verified.
- **Before finalizing, re-check the video**: not deleted, not `moderation_status = 'REMOVED'`, still `PROCESSING`, job not cancelled; and
  check `content_sha256` against the hash blocklist. A video removed while queued must never become ready.
- Original: kept (D8), never deleted by ordinary cleanup. Outputs of a completed version are never deleted by cleanup.
- Cleanup: extend the storage sweeper to the new prefixes (abandoned temporary prefixes, cancelled or failed jobs, superseded versions
  after a retention period), idempotent, bounded retries, tested with the database and storage in agreement, never touching `uploads/` originals
  or a completed version.
- Verify (storage stub with S3 semantics, plus the S3 client tests on an emulator): a crash between upload and finalize leaves no `READY`
  video and the next attempt recovers; the double finalize is a no-op; the cleanup tests include "must not delete" cases with mutation checks.

### B6.6 Secure execution and resource control
- Argument arrays only; generated temporary file names (never the original name); a per-job temp directory with a disk quota, removed
  on success, failure, cancel and shutdown; path traversal impossible; no arbitrary FFmpeg arguments from any client.
- Per-job timeout and cancellation kill the **whole process group** (no orphaned `ffmpeg`), tested by cancelling mid-encode and checking the
  process table and the temp directory. Configurable max concurrent jobs, memory/CPU/`nice`/`ulimit`-style limits where Go can set them.
- `Dockerfile.worker` (non-root user, no Docker socket, only the secrets the worker needs, read-only root where possible) and a compose
  service. Document isolation assumptions and residual risk honestly; the container itself is **not runnable here**, say so.
- Verify: hostile file names and metadata (quotes, `$()`, newlines, `../`, very long) never reach a shell and never leave the temp dir; a
  job that exceeds the timeout is killed and retried; disk-quota overflow fails safely; shutdown mid-job leaves the job re-queueable.

### B6.7 Processing APIs, notifications and the creator view
- `GET /api/v1/videos/{id}/processing` (owner or authorised moderator; stage, progress, safe error code, renditions and thumbnail availability,
  timestamps; never paths, logs or private URLs). `POST .../processing/retry` (admin/moderator, audited with actor and reason, no duplicate active
  job) and `POST .../processing/cancel` (owner or moderator, valid states only, terminates the running job). Keep `GET /upload/{id}/status` consistent.
- Notify the owner when processing finishes or fails (existing notification service) and push the status on the realtime topic `video:<id>`.
- Web: the upload/Studio view shows the accurate stage and progress, a safe failure message with "upload again", and retry/cancel where allowed
  (Vitest + the hook tests style of Phase 5; no unverifiable e2e).
- OpenAPI: extend `storage/openapi.yaml` or add `processing/openapi.yaml`; the drift test must cover the new routes.
- Counters (`processing_started`, `_succeeded`, `_failed`, `_retried`, durations) in the existing registry.

### B6.8 Recovery, documentation and the B6 report
- Recovery runbook with tests where possible: worker crash, queue (database) outage, storage outage, database outage, FFmpeg timeout, disk
  exhaustion, corrupted output, repeated failure -> permanent failure.
- Documents: architecture, state machine, profile versions, storage layout, operations, security, testing, deployment (worker image, env vars).
- Run the **completion checklist of section 24 of the Phase 6 document** one line at a time with the evidence for each; label every line.
- End-to-end check with real files: upload through the HTTP tests' fake browser, run the worker on a generated video, then publish through
  the existing consent flow. Only possible with an S3-like store in tests, say so.
- Done when: the report exists with honest labels. B6 is **not** COMPLETE if any checklist line lacks evidence.

---

## 7. B7 — Secure playback
Goal: only an authorised, age-eligible viewer can play a processed, approved video, with short-lived, revocable, non-guessable access, and
removal takes effect quickly. Backend first; the player UI is the last sub-phase.

### B7.1 Audit and design record (DECISION D4)
- Audit `internal/playback` (what the HMAC covers, what it does not: session, user, IP, revocation), visibility rules in the catalog queries
  (public/unlisted/private, `moderation_status`, suspended owners), the age cookie and registration-time age check, the Redis usage, the web player today.
- Write the ADR: delivery (D4), token/session model, binding, TTL, revocation (denylist keyed by video and by user, versioning, or very short TTL),
  what is cached and where (never in a shared cache), playlist handling (a gateway serves playlists itself and rewrites nothing client-controlled).
  List the threats the design must stop: path traversal and prefix confusion, SSRF, another video's key, token replay on another video, leaked URL reuse,
  range abuse, cache poisoning, playback after removal.

### B7.2 Authorization core
- One function decides "may this principal play this video now": owner, moderator, public/unlisted/private rules, `moderation_status`, video status
  (`READY`/`PUBLISHED` as the owner allows), suspended or banned accounts, **age eligibility enforced on the server** (D5). Same decision for
  playlists, segments, thumbnails and `playback-info`. Unauthorised and non-existent answer the same (no existence leak).
- Table-driven security matrix tests (every role x visibility x state x age x suspended) on real PostgreSQL; mutation-check each rule.

### B7.3 Playback session and delivery
- `POST /api/v1/videos/{id}/playback` (creates a short-lived session/token bound to the video and, where practical, the session/user; rate limited),
  `GET /api/v1/videos/{id}/playback-info` (safe metadata only: title, duration, poster, qualities, captions flag; no keys, buckets or paths).
- Delivery per D4: the gateway streams objects (no whole segment in memory, `Range` support, correct content types, `Cache-Control: private/no-store`),
  restricted to the video's exact prefix, only `.m3u8`/`.ts`(/`.m4s`) names, no traversal. Wire the existing signer only if the design keeps it.
- Config: `PLAYBACK_SIGNING_KEY` (validated at startup, never logged), TTL, limits; compose and `.env.example`.
- Verify with real PostgreSQL and the S3-like store: tampered, expired, wrong-video and malformed tokens are refused; traversal strings all fail;
  range requests return the right bytes (compare with the stored object); a large segment does not grow memory (measure).

### B7.4 Revocation and moderation enforcement
- A removal, restriction, suspension or deletion stops new playback requests at once and live sessions within the documented bound; test the bound.
- Moderation takedown (B9) calls the same revocation; cached authorisation is invalidated on every such event.

### B7.5 Progress, views and the player
- Safe progress position (resume), view counting that does not update one hot row per view (batch or buffer; measure the old behaviour first), retention of expired sessions.
- Player: `hls.js` integration on the watch page using the playback session, quality labels, captions, error states for 401/403/expired/removed,
  token refresh before expiry, no token in logs or URLs shown to the user. Vitest component/hook tests; real-browser playback is **not verifiable here**, say so.
- Done when: B7 completion checklist (phase 7 section 14) is answered line by line with evidence; OpenAPI updated.

---

## 8. B8 — Notifications, studio, realtime (audit, then integrate)
B8 was built before this project's verification method existed. Do not rewrite it.
- B8.1 Audit against the code and the tests: notification kinds, delivery (database + WebSocket), per-user isolation, unread counts, rate limits,
  authentication and origin checks on the WebSocket, studio endpoints (overview, analytics, videos, visibility, delete, comments approve). Write the gap list; fix only real defects.
- B8.2 Integrate with B6/B7: processing done/failed/needs attention notifications, `video:<id>` status push, studio listing shows the processing stage,
  studio delete/visibility changes call the playback revocation of B7.4 and the storage cleanup.
- B8.3 Real tests: HTTP-level tests like Phase 5 (isolation between two users, no cross-user leaks), WebSocket auth/limits, studio analytics correctness
  (numbers against SQL), mutation checks. Hot-row counters: measure and fix only if they really contend.
- Done when: gap list closed or explicitly deferred, tests pass, docs updated.

## 9. B9 — Moderation and admin
- B9.1 Audit the moderation service and API against the schema: report states, assign/resolve, actions and what each does to the video and its files,
  restore, blocked hashes and the trigger, audit logging, role checks (`mod`/`adm`/superadmin). Decide D1 and D9.
- B9.2 Takedown integration: a removal sets `moderation_status`, closes playback (B7.4), cancels queued/running processing, makes the outputs
  inaccessible (quarantine prefix or delete per retention and legal-hold rules, **audited, reversible by restore where policy allows**), notifies the owner.
  Restore re-enables what was quarantined. Tests on real PostgreSQL + S3-like store.
- B9.3 Hash blocklist: `content_sha256` from B6 is checked before processing finalizes and before publish; a match blocks, quarantines, audits and opens a report.
  **Re-verify the trigger** (it was not verified in Phase 5) with tests.
- B9.4 Review gate (per D1) and the processing admin endpoints of B6.7 (retry/cancel with actor and reason).
- B9.5 Documented procedures (required by the phase documents): reporting and removal of non-consensual intimate imagery, suspected exploitation, illegal and
  infringing content: who does what, in what time, evidence preservation, legal reporting duties. **Write it as an operational document for the owner and
  their counsel to complete; do not invent legal obligations or claim compliance.**
- B9.6 Minimal admin surface per D9 (read-only queue page at most), with Vitest tests.
- Done when: moderation actions have verified effects on every artefact (video, files, playback, jobs, notifications) and the audit trail is complete.

## 10. B10 — Hardening
Each item is a sub-phase; do them after B6-B9 are verified.
- B10.1 **Content scanning** (D6): a `Scanner` interface called by the worker before finalize (and optionally on the original at upload completion), a
  development/no-op implementation, a documented provider adapter contract (hash-matching service / external classifier), quarantine state and
  reporting hooks. The scanner's availability policy (fail closed or open) is an explicit, tested choice. **Do not build detection models; do not claim
  that signature checks or ffprobe validation prove content is safe or lawful.**
- B10.2 **Worker sandbox verification**: apply and test everything B6.6 documented (non-root, limits, no network where feasible, no Docker socket, secrets
  minimised), seccomp/AppArmor notes, a hostile-media corpus (fuzzed headers, decompression bombs, many-stream files) that must end in a safe failure within limits.
- B10.3 **Observability and metrics** (D7): the counters of Phase 5 and B6/B7 exposed through an authenticated or internal-only endpoint, structured logs with request
  ids and no secrets, alert-worthy conditions listed (stuck jobs, growing cleanup backlog, `degraded` storage, playback error rates).
- B10.4 **Abuse and rate limits**: per-IP and per-account limits for upload, playback and reporting, tested; fail-closed behaviour when Redis is down is
  documented and tested for each endpoint class.
- B10.5 **Load and soak tests** with a small Go tool (no external load tools available): upload fan-out, worker throughput, playback gateway under
  concurrent range requests, database connection use, memory growth. Report the **measured** numbers and the conditions; no claim beyond them.
- B10.6 **Security review**: authN/authZ matrix across the whole API (not only uploads), headers, CORS, CSRF on every state-changing route, input
  validation sweep, dependency and secret scanning as far as the sandbox allows (say what could not run), logging review.
- B10.7 **Operations**: backup and restore of PostgreSQL and the bucket (documented and, for PostgreSQL, rehearsed), migration rollback rehearsal, runbooks for
  each alert, a deployment guide for the API, worker and web.
- B10.8 **Pre-production checklist**: run `TestS3*` on real MinIO/S3, one full browser upload -> process -> playback with the owner, bucket CORS (`ExposeHeaders: ETag`),
  lifecycle rule for incomplete multipart uploads, the pending CI job (needs the Workflows permission), Playwright e2e where the owner has browsers.
  Output: an honest list of what is verified, partly verified and open. **No "production ready" statement.**

---

## 11. Order, dependencies and closing steps
```
B6.1 -> B6.2 -> B6.3 -> B6.4 -> B6.5 -> B6.6 -> B6.7 -> B6.8        (B6 is the critical path: nothing is ever READY without it)
                                  \-> B7.1 -> B7.2 -> B7.3 -> B7.4 -> B7.5
B8.1 can run any time (audit only); B8.2 after B6.7 and B7.4
B9.1 any time; B9.2-B9.4 after B6.5 and B7.4
B10.x after B6-B9; B10.1, B10.3, B10.4 may start earlier
```
After B10: the **Phase 8 integration** (frontend to the real API instead of the mock, full-flow e2e) and the **Phase 10 social audit** (B4 against its document)
are separate, later phases; do not fold them into B6-B10.

## 12. Final report format (per phase, in Bengali for the owner, labels in English)
Files created/modified; what was implemented; **tests run with the actual numbers**; mutation checks; defects found and fixed; known issues and
incomplete work; infrastructure/configuration still needed; what was **not** run; next step. Use COMPLETE / PARTIALLY COMPLETE / BLOCKED / NOT IMPLEMENTED
per requirement. End every report with the open `DECISION`s.

## Appendix A — carried over from Phase 5 (not B6-B10, but still open)
Run `TestS3*` and the CI job `s3` on real MinIO/S3 and push branch `ci-s3-job` (needs the Workflows permission); bucket lifecycle rule; Playwright e2e;
a scrape endpoint for the counters; README and `DESIGN.md` counts (12 migrations / 39 tables); `.puku-cli/settings.local.json` into `.gitignore`;
per-request age gate decision (D5). Revoke the GitHub token when the work is finished.

## Appendix B — first commands when work starts
```bash
git pull; git log --oneline -8
cat instruction.txt storage/docs/PHASE_5_COMPLETION_REPORT.md
ffmpeg -version | head -1; ffmpeg -hide_banner -encoders | grep -E "libx264| aac "; ffmpeg -hide_banner -muxers | grep " hls "
# PostgreSQL + Redis + test database + Go build copy: storage/baseline/REPRODUCE.md and the helper scripts described there
# then: B6.1 audit, write processing/docs/B6_AUDIT.md and the design record, stop at D1-D3 and D8
```
