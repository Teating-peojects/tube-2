# Phase 6 --- Production-Grade Video Processing, FFmpeg Transcoding & HLS Packaging

## Project: xhamtube.com

**Platform:** 18+ Video Sharing & Streaming Platform\
**Phase:** 6 --- Video Processing & HLS Transcoding\
**Primary stack:** Go, PostgreSQL, Redis (if already integrated), MinIO
/ S3-compatible object storage, FFmpeg / FFprobe, Docker

You are acting as a Senior Go Backend Engineer, Video Infrastructure
Engineer, FFmpeg Specialist, Distributed Systems Architect, and
Application Security Engineer.

Your task is to implement **Phase 6 --- Video Processing, FFmpeg
Transcoding & HLS Packaging** for **xhamtube.com**.

This is an existing project. Inspect the complete repository and the
actual Phase 5 implementation before changing anything. Build on the
code that exists. Do not assume Phase 5 was implemented exactly as
planned: verify it.

The goal is to take a securely uploaded, verified source video and
process it into validated, private, adaptive-bitrate HLS outputs with
thumbnails and reliable processing state.

Do not implement the public playback experience or public CDN delivery
in this phase. Those belong to later phases. Keep all source files and
generated HLS assets private until the moderation and publication
workflow explicitly authorizes access.

------------------------------------------------------------------------

# 1. Existing Architecture --- Verify Before Using

The expected project stack is:

### Frontend

-   Next.js
-   React
-   TypeScript
-   Tailwind CSS

### Backend

-   Go (Golang)
-   Existing REST API architecture
-   Existing authentication and authorization system

### Database

-   PostgreSQL
-   Existing migration system

### Infrastructure

-   MinIO / S3-compatible object storage
-   Docker / Docker Compose
-   Redis and a background job queue, if already implemented
-   FFmpeg and FFprobe
-   Future HLS playback and CDN integration

These are expected technologies, not proof that each component exists or
is operational.

## Mandatory repository inspection

Inspect:

-   Existing project structure and conventions.
-   Phase 5 upload implementation.
-   Upload and video database models.
-   Upload completion handler and service.
-   Object-storage abstraction and MinIO/S3 implementation.
-   Object key conventions and bucket policies.
-   Upload state transitions.
-   Existing background worker or queue.
-   Redis configuration and connection lifecycle.
-   Existing logging, metrics, tracing, and request IDs.
-   Authentication, RBAC, age eligibility, and moderation state.
-   Docker Compose and deployment configuration.
-   Existing tests and CI workflows.
-   Existing API and OpenAPI documentation.

Create or update:

-   `docs/PHASE_6_AUDIT.md`
-   `docs/PHASE_6_IMPLEMENTATION_PLAN.md`

Classify every relevant capability as: - Implemented and verified. -
Implemented but not verified. - Partially implemented. - Missing. -
Blocked by configuration or infrastructure.

If Phase 5 is incomplete, identify the exact dependency and implement
only the minimum safe compatibility work required for Phase 6. Do not
silently rewrite Phase 5 or claim it is complete.

------------------------------------------------------------------------

# 2. Main Objective

Build a reliable asynchronous video-processing pipeline:

``` text
Verified Upload
      |
      v
UPLOADED / PROCESSING_QUEUED
      |
      v
Create Processing Job
      |
      v
Worker Claims Job
      |
      v
Download or Stream Private Source
      |
      v
FFprobe Inspection
      |
      v
Input Validation
      |
      v
FFmpeg Transcoding
      |
      +----------------------+
      |                      |
      v                      v
Adaptive HLS Renditions   Thumbnail Generation
      |                      |
      +-----------+----------+
                  |
                  v
Validate Generated Outputs
                  |
                  v
Upload Outputs to Private Storage
                  |
                  v
Verify Stored Objects
                  |
                  v
Atomically Mark Processing Complete
                  |
                  v
READY_FOR_REVIEW
```

The pipeline must support:

-   Asynchronous processing.
-   Multiple concurrent jobs within configured resource limits.
-   Retryable failures.
-   Idempotent processing.
-   Worker crash recovery.
-   Safe temporary-file cleanup.
-   Adaptive-bitrate HLS generation.
-   Thumbnail generation.
-   Output validation.
-   Private object storage.
-   Detailed processing status.
-   Future secure playback integration.

A successful upload must not automatically make a video public.

------------------------------------------------------------------------

# 3. Architecture & Processing Boundaries

Use the existing application architecture.

Suggested logical components:

``` text
Go API
  |
  +--> PostgreSQL: upload/video/job state
  |
  +--> Queue: processing job
           |
           v
      Go Worker
           |
           +--> Storage adapter
           |
           +--> FFprobe
           |
           +--> FFmpeg
           |
           +--> Output validation
           |
           +--> Private object storage
           |
           +--> PostgreSQL state update
```

Engineering rules:

-   Keep video transcoding out of normal HTTP request handlers.
-   Never run FFmpeg synchronously inside an upload completion request.
-   Use the existing queue if one exists.
-   Do not introduce a second queue system without a documented
    technical reason.
-   If no queue exists, choose a maintainable Go-compatible job
    mechanism and document the decision.
-   Keep storage access behind the existing storage abstraction.
-   Do not pass large video files through application memory.
-   Use temporary disk storage with strict quotas.
-   Do not create unnecessary microservices.
-   Make the worker independently deployable from the API where the
    existing architecture permits.

------------------------------------------------------------------------

# 4. Processing State Machine

Inspect the existing status model and extend it without breaking
existing records or API contracts.

Suggested video processing states:

``` text
UPLOADED
PROCESSING_QUEUED
PROCESSING
GENERATING_THUMBNAILS
UPLOADING_OUTPUTS
VALIDATING_OUTPUTS
READY_FOR_REVIEW
PROCESSING_FAILED
PROCESSING_CANCELLED
```

Suggested job states:

``` text
QUEUED
RUNNING
RETRY_WAIT
SUCCEEDED
FAILED
CANCELLED
```

Requirements:

-   Define valid state transitions centrally.
-   Reject invalid transitions.
-   Persist state changes.
-   Record timestamps for each meaningful transition.
-   Store a safe failure code and sanitized failure message.
-   Keep internal diagnostic details in restricted logs, not public API
    responses.
-   Prevent duplicate jobs for the same processing version.
-   Use transactions or equivalent consistency controls for related
    database updates.
-   Make state updates safe under concurrent workers.
-   Do not mark a video ready before every required output has been
    verified.

A processing job must include or reference:

-   Job ID.
-   Video ID.
-   Source object key.
-   Processing profile/version.
-   Attempt number.
-   Creation and start timestamps.
-   Lease/heartbeat information if supported by the queue.
-   Current status.

Never trust a client-supplied object key or user ID. The backend must
load the authorized source object key from the database.

------------------------------------------------------------------------

# 5. Processing Job Creation

Trigger processing only after the Phase 5 upload-completion service has
independently verified the uploaded object.

Requirements:

1.  Confirm upload belongs to the video.
2.  Confirm upload state is eligible for processing.
3.  Confirm source object exists.
4.  Confirm the video is not cancelled, blocked, or removed.
5.  Confirm moderation policy permits processing for private review.
6.  Create a durable processing job.
7.  Persist the queued state.
8.  Ensure database and queue consistency.

If the queue cannot participate in the same database transaction, use a
reliable pattern such as a transactional outbox or another documented
mechanism. Avoid a state where the database says a job is queued but no
job can ever be delivered.

Duplicate completion requests must not create duplicate processing jobs.

------------------------------------------------------------------------

# 6. FFprobe Inspection & Input Validation

Use FFprobe to inspect the actual uploaded media before transcoding.

Capture, where available:

-   Container format.
-   Duration.
-   File size.
-   Video stream count.
-   Audio stream count.
-   Video codec.
-   Audio codec.
-   Width and height.
-   Frame rate.
-   Pixel format.
-   Sample aspect ratio.
-   Display aspect ratio.
-   Rotation metadata.
-   Audio sample rate.
-   Audio channel layout.
-   Bitrate.

Validate against configurable policies:

-   Maximum source file size.
-   Maximum duration.
-   Minimum and maximum dimensions.
-   Allowed container formats.
-   Allowed video codecs.
-   Allowed audio codecs.
-   Maximum frame rate.
-   Maximum number of streams.
-   Maximum processing duration.
-   Maximum temporary disk usage.

Do not trust metadata supplied by the browser.

Reject or quarantine inputs that: - Are not valid media. - Have no
usable video stream. - Exceed configured limits. - Contain unsupported
or malformed streams. - Trigger resource-limit violations. - Fail
security or moderation preconditions.

Be careful with rotation metadata and non-square pixel aspect ratios.
Generated output must display with the correct orientation and aspect
ratio.

Do not assume every input has an audio stream. Support valid silent
videos without failing unnecessarily.

Do not claim that FFprobe validation proves a file is safe. Treat media
parsing as untrusted-input processing.

------------------------------------------------------------------------

# 7. FFmpeg Transcoding Profiles

Implement configurable transcoding profiles rather than hardcoding all
parameters in handlers.

A reasonable initial adaptive ladder may include:

  Rendition     Maximum dimensions   Target video bitrate
  ----------- -------------------- ----------------------
  360p                     640×360          600--900 kbps
  480p                     854×480      1,000--1,500 kbps
  720p                    1280×720      2,000--3,000 kbps
  1080p                  1920×1080      4,000--6,000 kbps

These are initial engineering defaults, not guaranteed optimal values.
Tune them based on source dimensions, frame rate, quality targets,
storage cost, and bandwidth.

Requirements:

-   Never upscale a source to a higher resolution than it contains.
-   Preserve aspect ratio.
-   Handle portrait, landscape, and rotated video.
-   Use configurable codecs and encoding presets.
-   Use H.264/AAC as a broadly compatible initial profile when
    appropriate.
-   Support silent input.
-   Use `yuv420p` for broad compatibility where suitable.
-   Set sensible keyframe intervals aligned with HLS segment duration.
-   Avoid unnecessarily high frame rates.
-   Preserve audio/video synchronization.
-   Avoid generating empty or invalid renditions.
-   Make profiles versioned so future encoding changes are traceable.

If a source is smaller than a rendition, skip that rendition. Do not
create duplicate lower-quality outputs unnecessarily.

If the source uses unsupported codecs or profiles, fail with a
documented and actionable error rather than silently producing corrupted
output.

Do not promise a particular quality or bitrate for every source.

------------------------------------------------------------------------

# 8. HLS Packaging

Generate adaptive-bitrate HLS outputs.

Use a clear, versioned object layout, for example:

``` text
videos/
  {video_id}/
    processing/
      {processing_version}/
        hls/
          master.m3u8
          360p/
            index.m3u8
            segment_000001.ts
          480p/
            index.m3u8
            segment_000001.ts
          720p/
            index.m3u8
            segment_000001.ts
          1080p/
            index.m3u8
            segment_000001.ts
        thumbnails/
          poster.jpg
          preview_001.jpg
```

The actual extension and segment format may differ based on the selected
HLS profile. Document the decision.

Requirements:

-   Generate a master playlist.
-   Generate one media playlist per available rendition.
-   Ensure bandwidth and resolution metadata are correct.
-   Use consistent segment durations.
-   Use keyframe alignment between renditions.
-   Validate playlist references.
-   Verify every referenced segment exists.
-   Verify segment files are non-empty.
-   Avoid publishing partial playlists.
-   Keep all HLS objects private.
-   Do not expose public URLs in this phase.
-   Ensure playlist paths are relative and valid for later secure
    playback integration.

Use atomic publication:

1.  Generate outputs in a unique temporary processing prefix.
2.  Validate all playlists and segments.
3.  Upload outputs to a versioned private destination.
4.  Verify uploaded objects.
5.  Persist the final manifest/object references.
6.  Only then transition the video to `READY_FOR_REVIEW`.

If any stage fails, do not expose a partially generated HLS package as
complete.

Do not implement public CDN delivery or public playback authorization in
Phase 6.

------------------------------------------------------------------------

# 9. Thumbnail & Preview Generation

Generate thumbnails using FFmpeg.

Required initial outputs:

-   Main poster thumbnail.
-   Optional multiple preview thumbnails.
-   Configurable thumbnail dimensions and quality.

Requirements:

-   Select a suitable frame while avoiding invalid timestamps.
-   Handle short videos.
-   Handle portrait and landscape videos.
-   Respect rotation.
-   Avoid blank or black frames where practical.
-   Validate generated image files.
-   Store thumbnails privately.
-   Record object keys and dimensions in the database.
-   Make thumbnail generation retryable.
-   Do not expose thumbnails publicly before the moderation/publication
    policy permits it.

If preview sprite sheets are implemented, document their format and
future player integration.

Do not add image-generation services or unrelated image processing
dependencies.

------------------------------------------------------------------------

# 10. Secure FFmpeg Execution

Treat uploaded media as hostile input.

Security requirements:

-   Never concatenate user-controlled strings into shell commands.
-   Invoke FFmpeg/FFprobe with safe argument arrays through Go process
    APIs.
-   Do not use `sh -c`, `bash -c`, or equivalent shell interpolation.
-   Use generated temporary filenames, not original filenames, for
    filesystem paths.
-   Validate all paths and prevent traversal.
-   Use a dedicated non-root worker user.
-   Run workers in isolated containers where practical.
-   Apply CPU, memory, process, disk, and execution-time limits.
-   Restrict worker network access where feasible.
-   Keep FFmpeg and FFprobe versions maintained.
-   Do not mount Docker socket into the processing worker.
-   Do not give the worker unnecessary application secrets.
-   Do not allow arbitrary FFmpeg arguments from API clients.
-   Sanitize FFmpeg diagnostic output before logging.
-   Avoid exposing filesystem paths or internal command details to
    users.

Use context cancellation and process termination when a job is cancelled
or exceeds its timeout.

Document the isolation assumptions and residual risks.

------------------------------------------------------------------------

# 11. Worker Concurrency & Resource Management

Implement controlled concurrency.

Requirements:

-   Configurable maximum active jobs.
-   Configurable CPU/memory limits.
-   Configurable per-job timeout.
-   Configurable temporary disk quota.
-   Queue backpressure.
-   Worker heartbeat or lease renewal where supported.
-   Graceful shutdown.
-   Safe recovery after worker restart.
-   Retry with exponential backoff and jitter.
-   Maximum retry attempts.
-   Dead-letter or permanently failed job handling.
-   Manual requeue mechanism restricted to authorized administrators.

Do not let a large number of concurrent FFmpeg processes exhaust the
server.

Ensure cancellation and shutdown do not leave orphaned FFmpeg processes
or temporary files.

------------------------------------------------------------------------

# 12. Storage Integration

Reuse the Phase 5 storage abstraction.

Requirements:

-   Download or stream the private source object safely.
-   Upload HLS playlists, segments, and thumbnails to private storage.
-   Use deterministic, versioned destination keys.
-   Verify object size and metadata after upload.
-   Avoid public-read ACLs.
-   Support MinIO locally and the configured S3-compatible provider in
    production.
-   Handle transient storage failures with bounded retries.
-   Avoid logging presigned URLs or credentials.
-   Clean up abandoned temporary prefixes.
-   Do not delete the original source automatically after successful
    transcoding.

Preserve the original source according to a configurable retention
policy. Any future deletion must be an explicit, auditable operation and
must respect legal, moderation, and recovery requirements.

Do not assume that MinIO and every S3-compatible service implement
identical lifecycle or multipart behavior.

------------------------------------------------------------------------

# 13. PostgreSQL Data Model

Inspect existing models first and reuse them.

Extend the schema only where necessary.

Potential fields on the existing video record:

-   `processing_status`
-   `processing_version`
-   `processing_started_at`
-   `processing_completed_at`
-   `processing_error_code`
-   `duration_seconds`
-   `width`
-   `height`
-   `frame_rate`
-   `video_codec`
-   `audio_codec`
-   `master_playlist_key`
-   `poster_thumbnail_key`

Potential processing-job table:

-   `id`
-   `video_id`
-   `processing_version`
-   `status`
-   `attempt_count`
-   `max_attempts`
-   `last_error_code`
-   `started_at`
-   `completed_at`
-   `created_at`
-   `updated_at`

Potential rendition table:

-   `id`
-   `video_id`
-   `processing_version`
-   `resolution`
-   `width`
-   `height`
-   `bandwidth`
-   `codec`
-   `playlist_key`
-   `created_at`

Use the existing schema conventions. Do not create duplicate tables if
equivalent structures already exist.

Database requirements:

-   Foreign keys.
-   Useful indexes.
-   Appropriate uniqueness constraints.
-   Transaction-safe state transitions.
-   Reversible migrations where supported.
-   No destructive migration of existing production data.
-   No storage of large binary video data in PostgreSQL.

------------------------------------------------------------------------

# 14. Processing APIs

Follow existing route conventions and avoid exposing internal worker
controls publicly.

Potential endpoints:

### Get processing status

`GET /api/v1/videos/{video_id}/processing`

Return only information the authenticated owner or authorized moderator
may access:

-   video ID
-   processing status
-   progress stage
-   available rendition metadata, if appropriate
-   thumbnail availability
-   safe error code
-   created/updated timestamps

Do not expose internal logs, filesystem paths, storage credentials, or
private object URLs.

### Retry processing

`POST /api/v1/videos/{video_id}/processing/retry`

Requirements:

-   Admin/moderator-only or another explicitly authorized policy.
-   Validate that retry is allowed.
-   Prevent duplicate active jobs.
-   Record the actor and reason in audit logs.
-   Create a new processing attempt/version safely.

### Cancel processing

`POST /api/v1/videos/{video_id}/processing/cancel`

Requirements:

-   Enforce ownership or moderator authorization according to policy.
-   Cancel only valid states.
-   Terminate active worker processing safely.
-   Preserve audit information.
-   Clean temporary outputs asynchronously.

If the existing API design uses different paths or methods, follow the
existing convention and document the final routes.

------------------------------------------------------------------------

# 15. Content Safety, Age & Moderation Workflow

xhamtube.com is an 18+ platform.

The processing system must not bypass existing age, consent, rights, or
moderation controls.

Requirements:

-   Only process uploads that passed Phase 5's server-side upload
    verification.
-   Preserve creator ownership and audit history.
-   Keep original and generated files private.
-   Do not automatically publish processed videos.
-   Processing success must transition to a review state, not public
    visibility.
-   Preserve moderation status independently from processing status.
-   Support blocked, removed, reported, or legally restricted content
    states.
-   Prevent a queued job from making a video available after it has been
    blocked or removed.
-   Ensure workers re-check current video eligibility before finalizing
    outputs.
-   Provide integration points for moderation review and takedown
    workflows.
-   Never treat an uploader checkbox as proof of consent or legal
    rights.

Do not implement features that facilitate non-consensual intimate
imagery, exploitation, or evasion of moderation.

------------------------------------------------------------------------

# 16. Frontend Integration

Do not build a public video player in this phase.

Integrate processing status into the existing creator upload/dashboard
experience.

Display:

-   Upload completed.
-   Waiting for processing.
-   Inspecting video.
-   Generating quality versions.
-   Generating thumbnails.
-   Validating outputs.
-   Ready for review.
-   Processing failed.
-   Processing cancelled.

Requirements:

-   Use existing design tokens and components.
-   Keep UI responsive.
-   Use typed API responses.
-   Poll status at a reasonable interval or use an existing real-time
    mechanism.
-   Avoid aggressive polling.
-   Stop polling when the page is inactive or processing reaches a
    terminal state.
-   Display safe, actionable error messages.
-   Provide retry only when backend authorization and state permit it.
-   Do not expose storage object keys or signed URLs.
-   Do not imply that `READY_FOR_REVIEW` means publicly published.

------------------------------------------------------------------------

# 17. Configuration & Docker

Integrate FFmpeg and worker configuration into existing Docker
infrastructure.

Use a maintained FFmpeg image or install a pinned, documented FFmpeg
version in the worker image.

Suggested environment variables:

``` env
FFMPEG_PATH=
FFPROBE_PATH=
VIDEO_WORKER_ENABLED=
VIDEO_WORKER_CONCURRENCY=
VIDEO_PROCESSING_TIMEOUT_SECONDS=
VIDEO_MAX_DURATION_SECONDS=
VIDEO_MAX_SOURCE_SIZE_BYTES=
VIDEO_TEMP_DIR=
VIDEO_TEMP_DISK_LIMIT_BYTES=
VIDEO_HLS_SEGMENT_SECONDS=
VIDEO_HLS_FORMAT=
VIDEO_PROCESSING_MAX_RETRIES=
VIDEO_RETRY_BASE_DELAY_SECONDS=
VIDEO_PROCESSING_VERSION=
VIDEO_THUMBNAIL_QUALITY=
VIDEO_RENDITIONS=
```

These are suggested names. Follow existing configuration naming where
applicable.

Requirements:

-   Validate configuration at startup.
-   Provide safe development defaults.
-   Use `.env.example` placeholders only.
-   Never commit real secrets.
-   Persist only the data that must survive container restarts.
-   Keep temporary processing storage separate from permanent object
    storage.
-   Apply CPU and memory limits in Docker where supported.
-   Provide health checks for the worker.
-   Document local development and production deployment.
-   Do not expose FFmpeg worker internals publicly.

------------------------------------------------------------------------

# 18. Cleanup & Recovery

Implement safe cleanup for:

-   Abandoned local temporary files.
-   Failed partial output directories.
-   Incomplete output uploads.
-   Expired processing leases.
-   Orphaned processing objects.
-   Cancelled job temporary data.

Requirements:

-   Idempotent cleanup.
-   Configurable retention periods.
-   Bounded retries.
-   Structured logs.
-   Safe behavior after crashes.
-   Never delete the original source as part of ordinary temporary
    cleanup.
-   Never delete outputs referenced by a completed processing version.
-   Use database state and storage verification before deletion.

Document a recovery procedure for: - Worker crash. - Queue outage. -
Storage outage. - Database outage. - FFmpeg timeout. - Disk
exhaustion. - Corrupted output. - Repeated job failure.

------------------------------------------------------------------------

# 19. Observability

Add structured logs and operational metrics using existing tooling.

Track where practical:

-   Jobs queued.
-   Jobs started.
-   Jobs completed.
-   Jobs failed.
-   Job duration.
-   Processing time per media minute.
-   Retry count.
-   Queue wait time.
-   FFprobe failures.
-   FFmpeg failures.
-   Storage upload failures.
-   Output validation failures.
-   Temporary disk usage.
-   Worker CPU and memory usage.
-   Active worker count.

Requirements:

-   Include job ID and video ID in restricted internal logs.
-   Never log signed URLs, secrets, or sensitive user data.
-   Avoid exposing internal diagnostics in user-facing APIs.
-   Add worker liveness/readiness checks.
-   Document useful alerts and thresholds.
-   Do not invent metrics that are not actually instrumented.

------------------------------------------------------------------------

# 20. Testing Strategy

Write meaningful tests.

## Unit tests

Test:

-   State transition validation.
-   Processing job creation.
-   Duplicate job prevention.
-   Retry eligibility.
-   Retry limit.
-   Cancellation.
-   FFprobe result parsing.
-   Invalid media metadata.
-   Rendition selection.
-   Portrait/landscape dimension calculations.
-   Silent video handling.
-   Storage key generation.
-   Output manifest validation.
-   Safe error mapping.

## Integration tests

Test:

-   PostgreSQL state persistence.
-   Queue delivery and retry behavior.
-   MinIO source retrieval.
-   FFmpeg processing with small synthetic fixtures.
-   HLS playlist generation.
-   Segment existence and validation.
-   Thumbnail generation.
-   Uploading generated outputs to private storage.
-   Worker restart and recovery.
-   Idempotent processing.
-   Cleanup safety.

Use small, non-sensitive test media fixtures. Generate synthetic media
when practical.

## Security tests

Test:

-   Malicious filenames.
-   Path traversal attempts.
-   Shell-injection-shaped input strings.
-   Unsupported media formats.
-   Oversized media.
-   Excessive duration.
-   Unauthorized processing status access.
-   Unauthorized retry/cancel.
-   Video blocked during processing.
-   Duplicate job delivery.
-   Worker timeout and cancellation.

## Frontend tests

Test:

-   Processing status display.
-   Status polling behavior.
-   Terminal-state handling.
-   Safe error display.
-   Retry visibility and permissions.
-   No accidental publication claim.

Run the tests. Report actual results. Do not mark tests passed if they
were not run.

------------------------------------------------------------------------

# 21. API & Developer Documentation

Update existing OpenAPI documentation and create or update:

-   `docs/PHASE_6_AUDIT.md`
-   `docs/PHASE_6_IMPLEMENTATION_PLAN.md`
-   `docs/PHASE_6_ARCHITECTURE.md`
-   `docs/PHASE_6_PROCESSING_PIPELINE.md`
-   `docs/PHASE_6_FFMPEG_PROFILES.md`
-   `docs/PHASE_6_STORAGE_LAYOUT.md`
-   `docs/PHASE_6_SECURITY.md`
-   `docs/PHASE_6_TESTING.md`
-   `docs/PHASE_6_DEPLOYMENT.md`
-   `docs/PHASE_6_RECOVERY_RUNBOOK.md`
-   `docs/PHASE_6_COMPLETION_REPORT.md`

Also update existing:

-   `README.md`
-   `.env.example`
-   Docker documentation
-   OpenAPI documentation
-   `PROGRESS.md`
-   `REMAINING_TASKS.md`

Do not create duplicate documentation where equivalent files already
exist.

------------------------------------------------------------------------

# 22. Implementation Workflow

Follow this order:

1.  Inspect repository and Phase 5 implementation.
2.  Write audit report.
3.  Identify missing dependencies and blockers.
4.  Finalize architecture and implementation plan.
5.  Define processing states and database changes.
6.  Implement durable job creation.
7.  Implement worker lifecycle.
8.  Implement FFprobe inspection and validation.
9.  Implement secure FFmpeg execution.
10. Implement configurable rendition profiles.
11. Generate adaptive HLS playlists and segments.
12. Generate thumbnails.
13. Validate generated outputs.
14. Upload outputs to private object storage.
15. Verify stored objects.
16. Implement retries, cancellation, and recovery.
17. Integrate creator dashboard processing status.
18. Update API documentation.
19. Write and run tests.
20. Fix discovered issues.
21. Update deployment and recovery documentation.
22. Produce the final completion report.

Do not skip repository inspection or testing.

------------------------------------------------------------------------

# 23. Strict Engineering Rules

-   Do not rewrite the whole project.
-   Do not duplicate Phase 5 functionality.
-   Do not assume Phase 5 is complete without verification.
-   Do not process unverified uploads.
-   Do not run FFmpeg in HTTP request handlers.
-   Do not invoke shell commands with user-controlled input.
-   Do not run workers as root unless a documented platform constraint
    makes it unavoidable.
-   Do not expose original files or HLS outputs publicly.
-   Do not automatically publish videos.
-   Do not bypass age verification or moderation.
-   Do not allow unbounded FFmpeg concurrency.
-   Do not leave orphaned processes.
-   Do not silently ignore processing errors.
-   Do not hide failing tests.
-   Do not mark unverified features as complete.
-   Do not commit secrets.
-   Do not perform destructive database migrations.
-   Do not force-push Git branches.
-   Do not introduce unnecessary microservices or dependencies.

If a requirement is blocked by missing infrastructure, credentials,
storage configuration, or unavailable test fixtures, document the exact
blocker and continue with independent work where safe.

------------------------------------------------------------------------

# 24. Phase 6 Completion Criteria

Phase 6 may be marked **COMPLETE** only when:

-   [ ] Existing Phase 5 implementation has been inspected.
-   [ ] Verified upload completion reliably triggers one processing job.
-   [ ] Duplicate job delivery is safe.
-   [ ] Processing state transitions are validated.
-   [ ] Worker starts and shuts down safely.
-   [ ] FFprobe inspects actual source media.
-   [ ] Invalid or oversized media is rejected safely.
-   [ ] FFmpeg runs without shell interpolation.
-   [ ] CPU, memory, disk, and timeout limits are configured.
-   [ ] Renditions are generated according to source dimensions.
-   [ ] Portrait and rotated media are handled correctly.
-   [ ] Silent video is handled correctly.
-   [ ] HLS master playlist is generated.
-   [ ] Media playlists and segments are validated.
-   [ ] Thumbnails are generated and validated.
-   [ ] Outputs are stored privately.
-   [ ] Stored objects are verified before completion.
-   [ ] Processing completion is atomic and idempotent.
-   [ ] Retry and cancellation work safely.
-   [ ] Worker crash recovery is documented and tested where possible.
-   [ ] Temporary cleanup does not delete original or completed outputs.
-   [ ] Creator UI displays accurate processing status.
-   [ ] Videos remain private and unpublished pending review.
-   [ ] Backend tests pass.
-   [ ] Integration tests pass, or blockers are clearly documented.
-   [ ] Security tests pass.
-   [ ] OpenAPI and developer documentation are updated.
-   [ ] No secrets are committed.
-   [ ] No critical unresolved security issues remain.

Use these final status labels:

-   **COMPLETE** --- implemented and verified.
-   **PARTIALLY COMPLETE** --- implemented but not fully verified.
-   **BLOCKED** --- cannot proceed due to a documented blocker.
-   **NOT IMPLEMENTED** --- not completed.

------------------------------------------------------------------------

# 25. Final Completion Report

Produce a report containing:

1.  Executive summary.
2.  Phase 5 integration findings.
3.  Existing functionality reused.
4.  Architecture and processing flow.
5.  Database migrations and state changes.
6.  Job queue and worker implementation.
7.  FFprobe validation behavior.
8.  FFmpeg profiles and actual encoding settings.
9.  HLS output structure and validation results.
10. Thumbnail generation details.
11. Storage integration.
12. Security controls and isolation assumptions.
13. Frontend processing status integration.
14. Tests run and their actual results.
15. Docker/build verification.
16. Known issues and limitations.
17. Remaining tasks.
18. Production resource requirements.
19. Recovery and operational guidance.
20. Recommended next phase.

Clearly distinguish: - Implemented code. - Tests passed. - Tests not
run. - Infrastructure actually verified. - Configuration still
required. - Production risks still unresolved.

Do not claim production readiness merely because the code compiles or a
local test succeeds.

------------------------------------------------------------------------

# FINAL INSTRUCTION

Build Phase 6 as a secure, reliable, maintainable video-processing
pipeline for **xhamtube.com**.

The priority is correctness, safe media processing, private storage,
operational reliability, and compatibility with the existing codebase.

**Inspect first. Plan second. Implement third. Test everything possible.
Document honestly.**

Begin with repository inspection now.
