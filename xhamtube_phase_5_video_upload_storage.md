# Phase 5 --- Production-Grade Video Upload & Storage Infrastructure

## Project: xhamtube.com

**Platform:** 18+ Video Sharing & Streaming Platform\
**Phase:** 5 --- Video Upload & Storage Infrastructure

You are acting as a Senior Go Backend Engineer, Senior Next.js Engineer,
Cloud Storage Architect, and Application Security Engineer.

Your task is to implement **Phase 5 --- Video Upload & Storage
Infrastructure** for **xhamtube.com**.

This is an existing project. You MUST inspect the entire repository
before writing or modifying any code.

Do not rebuild existing functionality. Extend the current architecture
and integrate with the existing frontend, Go backend, PostgreSQL
database, authentication system, and security middleware.

------------------------------------------------------------------------

## 1. Existing Project Architecture

The current technology stack is:

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
-   Existing database schema and migrations

### Planned Infrastructure

-   Redis
-   MinIO / S3-compatible object storage
-   Docker / Docker Compose
-   FFmpeg (future Phase 6)
-   HLS streaming (future Phase 6/7)
-   CDN (future deployment phase)

Important: - Do not change the existing stack without a compelling
technical reason. - Do not introduce another backend framework. - Do not
create a separate admin application. - Do not duplicate existing
database models, middleware, or services. - Follow the repository's
existing naming conventions, architecture, and coding standards.

------------------------------------------------------------------------

## 2. Primary Objective

Build a complete video upload and storage infrastructure that allows
authenticated and age-verified creators to upload videos securely.

The system must support:

1.  Video upload initialization.
2.  Secure direct-to-object-storage upload.
3.  Upload progress tracking.
4.  Upload completion verification.
5.  Video metadata persistence.
6.  Upload validation.
7.  Secure object storage.
8.  Upload cancellation.
9.  Failed upload recovery.
10. Retry handling.
11. Upload ownership verification.
12. Storage cleanup.
13. Frontend and backend integration.

The architecture must be suitable for future video transcoding, HLS
streaming, CDN delivery, and large-scale growth.

------------------------------------------------------------------------

## 3. First Task --- Repository Inspection

Before implementation, inspect:

-   Complete frontend directory.
-   Complete Go backend directory.
-   Existing API routes.
-   Authentication middleware.
-   Authorization and RBAC.
-   Age verification implementation.
-   PostgreSQL schema.
-   Database migration system.
-   Existing video-related models.
-   Existing upload-related functionality.
-   Redis configuration.
-   Docker Compose configuration.
-   Existing storage integrations.
-   Frontend API client and hooks.
-   Existing UI upload components.
-   Existing tests.

Identify:

-   What is already implemented?
-   What is partially implemented?
-   What is missing?
-   What needs modification?
-   What can be reused?

Create or update:

-   `docs/PHASE_5_AUDIT.md`
-   `docs/PHASE_5_IMPLEMENTATION_PLAN.md`

Do not overwrite useful existing documentation.

After inspection, proceed with implementation unless a genuine blocker
requires clarification.

------------------------------------------------------------------------

## 4. Storage Architecture

Implement an object-storage abstraction using MinIO with S3-compatible
APIs.

### Architecture

1.  Frontend requests upload initialization.
2.  Go backend authenticates and authorizes the user.
3.  Backend validates metadata and creates an upload record.
4.  Backend generates a short-lived presigned upload URL.
5.  Frontend uploads the video directly to MinIO / S3.
6.  Frontend requests upload completion.
7.  Backend independently verifies the object and its metadata.
8.  Backend updates the upload and video records in PostgreSQL.

### Architectural rules

-   Video binary data must not pass through the Go application server
    during normal direct uploads.
-   Never expose storage access keys or secret credentials to the
    frontend.
-   Never expose permanent public bucket access.
-   Generate short-lived presigned URLs.
-   Keep original uploaded videos private.
-   Use randomized, unpredictable object keys.
-   Separate temporary uploads from finalized video objects.
-   Use a storage interface so MinIO can later be replaced by AWS S3,
    Cloudflare R2, or another compatible provider.
-   Do not assume every S3-compatible provider supports identical
    features.

Suggested object structure:

``` text
redxtube/
    temporary/
        uploads/{upload_id}/source
    videos/
        {video_id}/original/source
    thumbnails/
        {video_id}/
    processing/
        {video_id}/
```

Use the new project identity in new documentation and storage naming
where appropriate. Do not rename existing infrastructure resources
blindly; document any migration implications.

Do not generate public URLs for original videos.

------------------------------------------------------------------------

## 5. Backend Implementation --- Go

Implement the following modules using the existing backend architecture.

Suggested module structure (adapt to the existing repository):

``` text
backend/
    internal/
        upload/
            handler.go
            service.go
            repository.go
            validation.go
            types.go
        storage/
            storage.go
            minio.go
            config.go
        video/
            repository.go
            model.go
        middleware/
            upload_limits.go
```

Do not force this exact structure if the project already has an
established architecture.

### 5.1 Upload Initialization API

**Endpoint:** `POST /api/v1/uploads/init`

Purpose: Initialize a new video upload session.

Example request:

``` json
{
  "filename": "sample-video.mp4",
  "content_type": "video/mp4",
  "size_bytes": 104857600,
  "title": "Sample Video"
}
```

Requirements:

-   Require authenticated user.
-   Require server-verified age eligibility.
-   Verify upload permissions.
-   Validate filename.
-   Validate declared MIME type.
-   Validate file size.
-   Generate unique upload ID.
-   Generate unpredictable storage object key.
-   Create database upload record.
-   Generate short-lived presigned upload URL.
-   Return upload ID and upload instructions.
-   Never trust frontend user ID or ownership claims.

Response should contain:

-   `upload_id`
-   `video_id` (if allocated at initialization)
-   `upload_url`
-   HTTP method
-   Required headers
-   `expires_at`
-   Maximum allowed size
-   Upload strategy

Use consistent response formats already established in the project.

### 5.2 Upload Completion API

**Endpoint:** `POST /api/v1/uploads/{upload_id}/complete`

Requirements:

-   Authenticate user.
-   Verify upload ownership.
-   Verify upload session is valid.
-   Verify upload has not expired.
-   Verify object exists in storage.
-   Verify actual object size.
-   Verify content type where possible.
-   Verify checksum when available.
-   Verify object key matches the database record.
-   Prevent duplicate completion.
-   Update upload and video states transactionally where appropriate.

Never trust a frontend claim that an upload completed successfully.

### 5.3 Upload Status API

**Endpoint:** `GET /api/v1/uploads/{upload_id}`

Return:

-   `upload_id`
-   `video_id`
-   `status`
-   Uploaded size (only when reliably available)
-   Total size
-   `created_at`
-   `updated_at`
-   `expires_at`
-   Failure reason (safe, non-sensitive information)

Verify ownership before returning information.

### 5.4 Upload Cancellation API

**Endpoint:** `DELETE /api/v1/uploads/{upload_id}`

Requirements:

-   Verify ownership.
-   Allow cancellation only in valid states.
-   Mark upload as `CANCELLED`.
-   Clean up temporary storage objects where possible.
-   Ensure cleanup is safe to retry.
-   Do not delete an unrelated or already published video.

### 5.5 Upload Retry API

**Endpoint:** `POST /api/v1/uploads/{upload_id}/retry`

Requirements:

-   Retry only eligible failed or expired uploads.
-   Verify ownership.
-   Prevent unlimited retries.
-   Generate a new short-lived upload URL.
-   Keep a clear audit trail.
-   Do not accidentally create duplicate video records.

If resumable uploads are implemented, use a provider-supported multipart
upload mechanism with persisted upload state.

------------------------------------------------------------------------

## 6. Frontend Implementation --- Next.js

Integrate the upload functionality into the existing creator/upload
interface.

Do not create an unrelated standalone upload page.

Implement:

-   Drag-and-drop video selection.
-   File picker.
-   Video preview.
-   Filename display.
-   File size display.
-   Upload progress.
-   Upload percentage.
-   Upload speed (if measurable).
-   Estimated remaining time (if reliably measurable).
-   Cancel upload.
-   Retry failed upload.
-   Upload success notification.
-   Upload error notification.
-   Upload status display.
-   Mobile-responsive UI.

Suggested upload states:

``` text
IDLE
SELECTED
INITIALIZING
UPLOADING
VERIFYING
UPLOADED
FAILED
CANCELLED
```

Use TypeScript types for every API request and response.

Important frontend rules:

-   Never store storage secret credentials in browser code.
-   Never trust client-side validation as the security boundary.
-   Do not expose signed URLs in logs, analytics, or error reports.
-   Avoid accidental duplicate submissions.
-   Prevent navigation from silently discarding an active upload when
    practical.
-   Handle network interruptions.
-   Clean up event listeners and upload resources.
-   Keep the current xhamtube.com visual identity and design system.
-   Preserve existing UI functionality.

Use an appropriate upload mechanism that supports progress reporting. If
using XMLHttpRequest for presigned PUT uploads, ensure required headers
exactly match the backend-generated upload instructions.

------------------------------------------------------------------------

## 7. File Validation & Security

Security is a critical requirement.

Implement layered validation.

### 7.1 Allowed Video Formats

Start with a configurable allowlist:

-   MP4
-   WebM
-   MOV
-   MKV (only if supported by the planned processing pipeline)

Do not assume the filename extension proves the actual file type.

### 7.2 Validation Requirements

Validate:

-   File extension.
-   Declared MIME type.
-   File signature / magic bytes.
-   Actual file size.
-   Object size after upload.
-   Storage object ownership.
-   Upload session expiration.
-   Maximum duration, when reliably available.
-   Maximum upload attempts.

Use configurable limits.

Do not hardcode arbitrary production limits in multiple files.

### 7.3 Security Controls

Implement:

-   Authentication.
-   RBAC.
-   Server-enforced age eligibility.
-   Ownership checks.
-   Rate limiting.
-   Upload concurrency limits.
-   Filename sanitization.
-   Randomized object keys.
-   Short-lived signed URLs.
-   CSRF protections consistent with the existing authentication
    architecture.
-   Secure CORS configuration.
-   Request size limits for metadata endpoints.
-   Safe error responses.
-   Audit logging.
-   Cleanup of abandoned uploads.

Never:

-   Trust client-provided user IDs.
-   Trust client-provided upload status.
-   Trust client-provided video visibility.
-   Allow arbitrary object keys from the client.
-   Allow unrestricted bucket access.
-   Return internal storage credentials.
-   Log passwords, tokens, signed URLs, or secrets.

------------------------------------------------------------------------

## 8. Age, Consent & Content Safety

xhamtube.com is an 18+ platform.

Enforce existing server-side age verification before granting upload
permission.

Do not introduce a client-only age gate as a substitute for server-side
authorization.

Build the necessary integration points for:

-   Creator upload eligibility.
-   Consent and rights confirmation.
-   Ownership / authorization declaration.
-   Community guidelines acceptance.
-   Reporting and moderation workflows.
-   Content review before publication.

Important:

-   Do not automatically publish newly uploaded videos.
-   Upload completion must not mean public availability.
-   A video must remain in a private, non-public state until the future
    moderation and processing workflow allows publication.
-   Provide integration points for future detection of non-consensual
    intimate imagery and prohibited content.
-   Do not claim that a checkbox alone proves consent or ownership.
-   Do not implement any mechanism that bypasses moderation or takedown
    requirements.

Do not build explicit-content generation features.

------------------------------------------------------------------------

## 9. PostgreSQL Database Design

Inspect the current schema first.

Reuse existing tables where possible.

If needed, create or extend tables for:

### Uploads

Suggested fields:

-   `id`
-   `user_id`
-   `video_id`
-   `object_key`
-   `original_filename`
-   `content_type`
-   `declared_size_bytes`
-   `actual_size_bytes`
-   `checksum`
-   `status`
-   `storage_provider`
-   `multipart_upload_id` (nullable)
-   `retry_count`
-   `expires_at`
-   `completed_at`
-   `created_at`
-   `updated_at`

### Videos

Reuse the existing video model.

Potential additional fields:

-   `original_object_key`
-   `original_filename`
-   `original_size_bytes`
-   `original_content_type`
-   `upload_status`
-   `processing_status`
-   `moderation_status`

Use the existing project's state conventions if they already exist.

Important:

-   Add indexes where appropriate.
-   Add foreign key constraints.
-   Add appropriate uniqueness constraints.
-   Use database transactions for related state changes.
-   Avoid duplicate records.
-   Use reversible migrations if supported by the existing migration
    system.
-   Never delete existing production data during migration.

Suggested upload states:

``` text
PENDING
UPLOADING
VERIFYING
UPLOADED
FAILED
CANCELLED
EXPIRED
```

------------------------------------------------------------------------

## 10. Large File & Resumable Upload Support

Design for large video files.

Prefer multipart uploads when supported by the storage provider.

Requirements:

-   Configurable multipart threshold.
-   Configurable part size.
-   Maximum number of parts.
-   Persisted multipart upload ID.
-   Part URL generation.
-   Upload completion verification.
-   Multipart abort and cleanup.
-   Retry support for failed parts.
-   Safe handling of expired sessions.

If full multipart support is too large for the first implementation,
implement a clean provider abstraction and document the remaining work.

Do not falsely claim resumable uploads are supported if only a single
PUT upload is implemented.

------------------------------------------------------------------------

## 11. Storage Configuration & Docker

Integrate MinIO into the existing Docker Compose environment.

Required configuration should include:

-   MinIO service.
-   Persistent storage volume.
-   Health check.
-   Internal application network.
-   Bucket initialization or provisioning.
-   Private bucket configuration.
-   Development credentials supplied through environment variables.

Suggested environment variables:

``` env
STORAGE_PROVIDER=minio
S3_ENDPOINT=
S3_REGION=
S3_BUCKET=
S3_ACCESS_KEY=
S3_SECRET_KEY=
S3_USE_SSL=
S3_PRESIGN_EXPIRY=
MAX_VIDEO_SIZE_BYTES=
MAX_UPLOAD_DURATION_SECONDS=
UPLOAD_SESSION_TTL=
UPLOAD_MAX_RETRIES=
```

Rules:

-   Update `.env.example` with placeholder values only.
-   Never commit real credentials.
-   Validate environment variables during application startup.
-   Fail safely when required storage configuration is missing.
-   Do not expose internal MinIO management ports publicly in
    production.
-   Document production bucket provisioning and IAM permissions.

------------------------------------------------------------------------

## 12. Cleanup & Background Jobs

Build a cleanup mechanism for:

-   Expired upload sessions.
-   Abandoned temporary objects.
-   Incomplete multipart uploads.
-   Cancelled upload objects.
-   Failed uploads beyond retention limits.

Use the existing background job infrastructure if available.

If Redis or another queue is not yet implemented, use an architecture
compatible with the existing Go backend rather than introducing an
unnecessary second job system.

Requirements:

-   Idempotent cleanup.
-   Retryable failures.
-   Structured logs.
-   Safe database state transitions.
-   Configurable retention periods.
-   No accidental deletion of completed videos.

Do not perform long-running cleanup operations inside normal HTTP
request handlers.

------------------------------------------------------------------------

## 13. API Documentation

Update the existing OpenAPI / Swagger documentation.

Document:

-   `POST /api/v1/uploads/init`
-   `POST /api/v1/uploads/{upload_id}/complete`
-   `GET /api/v1/uploads/{upload_id}`
-   `DELETE /api/v1/uploads/{upload_id}`
-   `POST /api/v1/uploads/{upload_id}/retry`

Include:

-   Authentication requirements.
-   Request schemas.
-   Response schemas.
-   Error responses.
-   Upload constraints.
-   Signed URL expiration behavior.
-   Ownership rules.

Follow existing API versioning and documentation conventions.

------------------------------------------------------------------------

## 14. Testing Requirements

Write meaningful tests.

### Backend Unit Tests

Test:

-   Upload initialization.
-   Invalid file extension.
-   Invalid MIME type.
-   Invalid file size.
-   Unauthorized upload.
-   Age verification failure.
-   Ownership failure.
-   Invalid upload state.
-   Duplicate completion.
-   Storage verification failure.
-   Expired upload.
-   Cancellation.
-   Retry limit.

### Integration Tests

Test:

-   PostgreSQL integration.
-   MinIO integration.
-   Presigned URL generation.
-   Upload object verification.
-   Completion state transition.
-   Failed upload recovery.
-   Cleanup behavior.

Use mocks for isolated unit tests and a real test storage/database
environment for integration tests.

### Frontend Tests

Test:

-   File selection.
-   File validation feedback.
-   Upload initialization.
-   Progress updates.
-   Upload completion.
-   Cancellation.
-   Retry.
-   Network failure handling.
-   Mobile UI behavior where practical.

Never use production storage or real user data in automated tests.

------------------------------------------------------------------------

## 15. Observability & Reliability

Implement structured logging and useful operational metrics.

Track:

-   Upload initialization count.
-   Successful upload count.
-   Failed upload count.
-   Upload duration.
-   Upload size.
-   Storage verification failures.
-   Expired uploads.
-   Cleanup failures.
-   Multipart upload failures.

Requirements:

-   Use the existing logger.
-   Include request IDs.
-   Do not log sensitive information.
-   Add health checks for storage connectivity where appropriate.
-   Avoid making normal application readiness depend on an unnecessarily
    fragile optional external service.
-   Document operational failure scenarios.

------------------------------------------------------------------------

## 16. Performance & Scalability

Design for:

-   Large video uploads.
-   Concurrent users.
-   Network interruptions.
-   Storage provider failures.
-   Horizontal backend scaling.
-   Multiple application instances.

Important:

-   Keep binary video data out of the application server.
-   Avoid loading entire video files into Go memory.
-   Use streaming/object-storage operations.
-   Avoid unnecessary database polling.
-   Use efficient indexes.
-   Ensure upload IDs and object keys are safe across multiple backend
    instances.
-   Keep storage provider details behind an abstraction.

Do not implement premature microservices.

------------------------------------------------------------------------

## 17. Future Phase Compatibility

This phase must integrate cleanly with the next development phases.

### Phase 6 --- Video Processing

Future pipeline:

``` text
UPLOADED
    |
    v
PROCESSING_QUEUED
    |
    v
FFmpeg Processing
    |
    v
Thumbnail Generation
    |
    v
HLS Generation
    |
    v
READY_FOR_REVIEW
```

### Phase 7 --- Secure Streaming

Future requirements:

-   Private original source files.
-   HLS output storage.
-   Secure playback authorization.
-   Signed playback access.
-   CDN integration.

Do not implement full FFmpeg transcoding or HLS generation in Phase 5.

However, make sure uploaded video records can be passed safely to a
future processing queue.

------------------------------------------------------------------------

## 18. Documentation Deliverables

Create or update:

-   `docs/PHASE_5_AUDIT.md`
-   `docs/PHASE_5_IMPLEMENTATION_PLAN.md`
-   `docs/PHASE_5_ARCHITECTURE.md`
-   `docs/PHASE_5_API.md`
-   `docs/PHASE_5_SECURITY.md`
-   `docs/PHASE_5_TESTING.md`
-   `docs/PHASE_5_DEPLOYMENT.md`
-   `docs/PHASE_5_COMPLETION_REPORT.md`

Also update:

-   `README.md`
-   `.env.example`
-   Docker Compose documentation
-   OpenAPI documentation
-   `PROGRESS.md`
-   `REMAINING_TASKS.md`

Do not create duplicate documents if equivalent documentation already
exists. Update the existing ones.

------------------------------------------------------------------------

## 19. Implementation Workflow

Work in the following order:

1.  Inspect repository.
2.  Produce audit report.
3.  Finalize implementation plan.
4.  Design database changes.
5.  Implement storage abstraction.
6.  Integrate MinIO.
7.  Implement upload initialization.
8.  Implement direct upload URL generation.
9.  Implement completion verification.
10. Implement status, cancellation, and retry.
11. Implement file validation and security controls.
12. Integrate frontend upload UI.
13. Implement cleanup.
14. Update OpenAPI.
15. Write and run tests.
16. Fix discovered bugs.
17. Update documentation.
18. Produce final completion report.

Do not skip tests simply because the code compiles.

------------------------------------------------------------------------

## 20. Strict Engineering Rules

-   Do not rewrite the whole project.
-   Do not delete working functionality.
-   Do not replace existing architecture without justification.
-   Do not use fake APIs or mock production functionality.
-   Do not leave critical TODOs while claiming completion.
-   Do not hardcode credentials.
-   Do not expose private storage objects.
-   Do not bypass authentication or age verification.
-   Do not automatically publish uploaded videos.
-   Do not mark untested features as verified.
-   Do not hide failing tests.
-   Do not silently ignore errors.
-   Do not make destructive database changes.
-   Do not force-push Git branches.
-   Do not commit `.env` files or secrets.

If a requirement cannot be implemented due to missing configuration,
unavailable infrastructure, or external service limitations, document
the blocker clearly.

------------------------------------------------------------------------

## 21. Completion Criteria

Phase 5 can be marked COMPLETE only when:

-   [ ] Existing codebase was audited.
-   [ ] Database migrations run successfully.
-   [ ] MinIO starts correctly through Docker Compose.
-   [ ] Authenticated eligible creators can initialize uploads.
-   [ ] Backend generates short-lived presigned URLs.
-   [ ] Frontend uploads directly to object storage.
-   [ ] Upload progress is displayed correctly.
-   [ ] Backend independently verifies uploaded objects.
-   [ ] Upload metadata persists correctly.
-   [ ] Unauthorized users cannot access other users' upload records.
-   [ ] Age-ineligible users cannot upload.
-   [ ] Invalid file uploads are rejected.
-   [ ] Cancellation works.
-   [ ] Retry works within defined limits.
-   [ ] Temporary objects can be cleaned up safely.
-   [ ] Original video files remain private.
-   [ ] Uploaded videos are not automatically published.
-   [ ] Backend tests pass.
-   [ ] Frontend tests pass.
-   [ ] Integration tests pass, or unavailable infrastructure is
    explicitly documented.
-   [ ] OpenAPI documentation is updated.
-   [ ] Deployment documentation is updated.
-   [ ] No secrets are committed.
-   [ ] No critical security issues remain unresolved.

Use the following final status labels:

-   **COMPLETE** --- implemented and verified.
-   **PARTIALLY COMPLETE** --- implemented but not fully verified.
-   **BLOCKED** --- cannot proceed due to a documented blocker.
-   **NOT IMPLEMENTED** --- not completed.

------------------------------------------------------------------------

## 22. Final Report

At the end, produce a clear report containing:

1.  Executive summary.
2.  Existing functionality reused.
3.  New files created.
4.  Existing files modified.
5.  Database migrations.
6.  API endpoints implemented.
7.  Storage architecture.
8.  Frontend functionality.
9.  Security measures.
10. Test results.
11. Build results.
12. Docker verification.
13. Known issues.
14. Remaining tasks.
15. Production deployment requirements.
16. Recommended next phase.

Clearly distinguish:

-   Code implemented.
-   Tests passed.
-   Tests not run.
-   Infrastructure actually verified.
-   Features still requiring production configuration.

Do not claim that the platform is production-ready solely because the
implementation is complete.

------------------------------------------------------------------------

# FINAL INSTRUCTION

Build Phase 5 as a professional, secure, maintainable,
production-oriented component of **xhamtube.com**.

Prioritize correctness, security, reliability, maintainability, and
compatibility with the existing codebase over implementation speed.

**Inspect first. Plan second. Implement third. Test everything possible.
Document honestly.**

Begin with repository inspection now.
