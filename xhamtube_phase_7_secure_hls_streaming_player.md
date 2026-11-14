# xhamtube.com — Phase 7: Secure HLS Streaming & Video Player Infrastructure

## Role

Act as a senior Go backend engineer, Next.js/TypeScript frontend engineer, video-streaming architect, and application security engineer.

Your task is to implement **Phase 7 — Secure HLS Streaming & Video Player Infrastructure** for **xhamtube.com**, an 18+ video-sharing platform.

## Project stack

Use the existing project stack and conventions:

- Frontend: Next.js, React, TypeScript, Tailwind CSS
- Backend: Go
- Database: PostgreSQL
- Cache/session/rate-limit infrastructure: Redis, if already configured
- Object storage: MinIO or an existing S3-compatible provider
- Video processing: FFmpeg/FFprobe
- Streaming: HTTP Live Streaming (HLS)
- Deployment: Docker / Docker Compose, following the existing repository setup

Do not replace the stack or introduce a new infrastructure dependency without first proving it is necessary.

---

# 1. Mandatory repository audit before coding

Before changing anything:

1. Inspect the complete repository structure.
2. Read the existing backend architecture, route registration, middleware, services, database models, migrations, configuration, and tests.
3. Inspect the frontend routing, video/watch page, API client, authentication state, and UI components.
4. Review Phase 5 upload/storage implementation and Phase 6 FFmpeg/HLS processing implementation.
5. Identify the actual object-storage layout, playlist format, rendition naming, processing states, and metadata currently produced.
6. Check whether videos are stored privately and whether any public bucket or permanent object URL is exposed.
7. Identify existing conventions and reuse them.

Create or update `docs/PHASE_7_AUDIT.md` with:

- Existing functionality found
- Missing functionality
- Integration gaps between Phase 5, Phase 6, and the player
- Security risks
- Configuration or infrastructure blockers

**Do not rewrite working modules. Do not assume a feature exists because a plan or UI suggests it exists. Verify the code.**

If Phase 5 or Phase 6 is incomplete, document the dependency and implement only the safe integration work possible. Do not fabricate processing outputs or mark videos playable when their HLS assets are missing or invalid.

---

# 2. Phase 7 objective

Implement secure playback for videos that have completed processing and passed moderation/review.

The expected flow is:

1. A creator uploads a video.
2. Phase 6 processes it into HLS renditions and validates the output.
3. The video remains private or under review until approved.
4. An authorized viewer requests playback.
5. The backend checks authentication, age eligibility, video visibility, moderation state, and access permissions.
6. The backend issues short-lived playback authorization.
7. The player loads the HLS master playlist and media segments through the approved access mechanism.
8. Expired or revoked access must stop future requests.
9. Private, unapproved, deleted, suspended, or restricted videos must not be playable by unauthorized viewers.

Do not automatically publish videos after processing.

---

# 3. Backend playback APIs

Follow the existing API versioning and response conventions. Use the current route structure where possible.

Implement or adapt endpoints equivalent to:

## 3.1 Playback session creation

`POST /api/v1/videos/{videoId}/playback`

Responsibilities:

- Authenticate the viewer when required by the platform's access policy.
- Enforce server-side age verification / age eligibility.
- Load the video and its current status from PostgreSQL.
- Verify that processing is complete and valid HLS assets exist.
- Enforce visibility rules: public, unlisted, private, restricted, deleted, blocked, or under review.
- Enforce moderation approval and account/video restrictions.
- Apply rate limiting and abuse protection.
- Create a short-lived playback session or signed playback token.
- Return only the playback information required by the player.

The client must never be trusted to decide whether a video is public, approved, age-eligible, or accessible.

## 3.2 Playback authorization

Implement the mechanism needed to authorize requests for:

- Master playlist
- Variant/media playlists
- HLS segments
- Encryption keys, if encryption is implemented
- Thumbnails, if protected by the same access policy

Choose the safest design that fits the existing storage and deployment architecture. Consider either:

- A backend playback gateway/proxy that validates each request, or
- Short-lived signed URLs/cookies supported by the storage/CDN layer.

Document the chosen approach and its trade-offs. Do not combine incompatible authorization approaches.

If using signed URLs:

- Keep signing keys only on the server.
- Use short expiry periods.
- Bind authorization to the video and, where practical, a playback session.
- Prevent path traversal and arbitrary object signing.
- Never sign arbitrary client-supplied storage paths.
- Avoid exposing permanent private bucket URLs.
- Ensure child playlist and segment URLs remain authorized for the intended playback window.

If using a proxy:

- Stream objects without loading entire video segments into memory.
- Validate every requested object against the playback session and allowed video prefix.
- Prevent path traversal, prefix confusion, SSRF, and arbitrary bucket access.
- Support correct streaming headers and efficient response handling.

## 3.3 Playback session lifecycle

Support the minimum lifecycle needed by the selected design:

- Create session
- Validate session/token
- Expire session
- Revoke session when required
- Reject expired, malformed, tampered, or revoked credentials

Use Redis only if it is already available and appropriate. Otherwise use a secure stateless signed-token design with a documented revocation strategy.

Do not store raw secrets or signing keys in source code or logs.

## 3.4 Video metadata endpoint

Implement or adapt a read endpoint equivalent to:

`GET /api/v1/videos/{videoId}/playback-info`

Return only safe metadata needed by the player, such as:

- Video title and permitted display metadata
- Playback availability
- Duration
- Thumbnail/poster reference
- Available playback quality labels, if appropriate
- Whether captions/subtitles are available

Do not expose internal storage bucket names, private object keys, filesystem paths, signing secrets, or moderation-only information.

Avoid leaking whether a private video exists to unauthorized users. Use consistent not-found/forbidden behavior according to the existing security policy.

---

# 4. HLS asset and playlist security

Inspect the actual Phase 6 output format before implementing URL rewriting or playlist handling.

Requirements:

- Master playlists and media playlists must be served with correct content types.
- Segment requests must use the same access policy as the playlist.
- Relative playlist paths must resolve safely.
- Reject path traversal and unexpected file extensions.
- Restrict every request to the authorized video's exact storage prefix.
- Do not allow a user to substitute another video's object key.
- Do not expose private storage endpoints or permanent public URLs.
- Prevent playlist responses from being cached publicly.
- Use appropriate cache headers for private, tokenized playback.
- Ensure authorization is not accidentally dropped when a playlist references child playlists, segments, subtitles, or keys.
- Validate that playlists reference existing assets before declaring playback ready.

If playlist rewriting is required, parse playlists safely; do not perform fragile unrestricted string replacement.

Do not enable HLS encryption or DRM merely for appearance. If encryption is implemented, document key storage, key delivery authorization, rotation, and player compatibility. Do not claim DRM protection unless a real DRM system is implemented.

---

# 5. Frontend video player

Integrate the player into the existing Next.js watch/video page.

Requirements:

- Use the existing design system and components.
- Use a maintained HLS-compatible player approach appropriate for the supported browsers.
- For browsers with native HLS support, use native playback where appropriate.
- For browsers without native HLS support, use a maintained JavaScript HLS library such as `hls.js`, if compatible with the current project.
- Do not add multiple overlapping player libraries.
- Load playback only after the backend grants access.
- Never construct private storage URLs in the browser.
- Handle loading, playback, buffering, retry, and fatal errors.
- Show a useful message for unavailable, processing, under-review, removed, or access-denied videos without leaking private details.
- Support play/pause, seek, volume, mute, fullscreen, and quality selection when supported.
- Show duration and playback progress.
- Preserve responsive behavior on mobile and desktop.
- Clean up event listeners, HLS instances, and media resources when the component unmounts or the video changes.
- Avoid autoplay with sound.
- Respect browser autoplay policies and user interaction requirements.
- Do not put secrets or long-lived playback credentials in local storage.

If the existing UI contains a mock player, replace the mock behavior with real API integration without unnecessarily redesigning the page.

---

# 6. Playback progress and view tracking

Integrate with existing view/history models if they already exist.

If they do not exist, propose the smallest compatible schema/API addition and explain it before applying migrations.

Possible endpoint:

`POST /api/v1/videos/{videoId}/progress`

Track only the minimum useful information:

- Playback position
- Duration, if needed for validation
- Session identifier or safe idempotency key
- Timestamp

Requirements:

- Validate all client-submitted values.
- Do not trust client-submitted view counts.
- Prevent trivial repeated refreshes from inflating views.
- Make progress updates idempotent or safely deduplicated.
- Throttle progress writes to avoid excessive database traffic.
- Do not record sensitive playback credentials in analytics.
- Respect account privacy and platform retention policy.

A view should be counted according to a documented server-side rule, not simply because the watch page loaded.

---

# 7. Age assurance, consent, and moderation enforcement

This is an 18+ platform. Playback must remain behind the platform's server-enforced age and safety controls.

Requirements:

- Enforce the existing server-side age-verification/age-eligibility state.
- Never rely only on a frontend age-gate modal or a client-side boolean.
- Apply the platform's configured geographic/legal restrictions where implemented.
- Do not expose videos that are under moderation review or removed.
- Respect account suspensions, bans, and moderator restrictions.
- Support immediate playback revocation after a video is removed or access is restricted, subject to the documented token/session expiry design.
- Keep moderation decisions and audit data server-side.
- Do not weaken age, consent, or moderation checks to make playback work.

The platform must have a documented process for reporting and removing non-consensual intimate imagery, suspected exploitation, illegal content, and copyright-infringing material. Playback infrastructure must respect takedown and restriction decisions.

---

# 8. Database and migrations

Inspect existing models first. Reuse existing tables and fields where possible.

Add only the minimum schema needed for playback sessions or progress tracking.

Potential data concepts:

- Playback session ID
- User ID, nullable only if the platform's policy permits guest playback
- Video ID
- Expiry and revocation state
- Created timestamp
- Safe progress position
- Last progress update

Requirements:

- Add reversible migrations using the repository's existing migration tool.
- Add foreign keys and indexes where appropriate.
- Apply retention/cleanup rules for expired sessions.
- Never store raw signed tokens or signing secrets in the database.
- Avoid duplicating video processing state from Phase 6.

Document migration and rollback commands.

---

# 9. Performance and reliability

- Support HTTP range requests if required by the chosen delivery path.
- Stream data efficiently; do not buffer entire segments in application memory.
- Use appropriate timeouts and cancellation.
- Avoid N+1 database queries.
- Cache only safe metadata and authorization data with a clear invalidation strategy.
- Never cache private playback responses in a shared public cache.
- Ensure a video removal or access restriction invalidates cached authorization promptly.
- Add request IDs and structured logs.
- Record useful playback error metrics without logging tokens or private URLs.
- Document expected CDN behavior, including query-string/cookie forwarding and cache-key configuration.
- Do not claim CDN support is active unless it is configured and tested.

---

# 10. Security requirements

Review and test at least:

- Unauthorized playback requests
- Unverified-age account requests
- Private video access by another user
- Unlisted video access policy
- Under-review video access
- Removed/deleted video access
- Suspended user access
- Expired token/session
- Tampered token
- Revoked playback session
- Cross-video token reuse
- Arbitrary object-key requests
- Path traversal attempts
- Playlist child URL authorization
- Segment URL authorization
- Token leakage through logs, referrers, analytics, or browser storage
- CORS and cookie policy
- Rate-limit bypass attempts
- Cache poisoning or shared-cache leakage

Use secure defaults. Do not disable authentication, age checks, CORS protections, or TLS requirements to make tests pass.

---

# 11. Tests

Add automated tests using the existing test framework.

## Backend tests

At minimum:

- Authorized public video playback
- Unauthenticated playback according to policy
- Age verification enforcement
- Private/unlisted visibility enforcement
- Moderation-state enforcement
- Deleted/restricted video rejection
- Invalid/expired/tampered token rejection
- Cross-video token reuse rejection
- Unauthorized playlist and segment access
- Path traversal rejection
- Playback session expiry/revocation
- Progress validation and deduplication

## Frontend tests

At minimum:

- Loading state
- Successful playback initialization
- Access denied state
- Processing/unavailable state
- HLS fatal error and recovery behavior
- Component cleanup on unmount/video change

## Integration tests

Where practical, test the complete path using the project's Docker Compose services:

1. Create a test video record.
2. Provide valid test HLS assets in a private test bucket.
3. Request playback authorization.
4. Fetch the master playlist.
5. Fetch a child playlist and segment.
6. Verify unauthorized access fails.
7. Revoke or expire the session and verify future requests fail.

Do not use real user content or production credentials in tests.

---

# 12. API documentation and developer documentation

Update OpenAPI/Swagger using the existing approach.

Create or update:

- `docs/PHASE_7_PLAYBACK_ARCHITECTURE.md`
- `docs/PHASE_7_SECURITY_CHECKLIST.md`
- `docs/PHASE_7_OPERATIONS.md`

Document:

- Playback request flow
- Authorization design and expiry
- Playlist/segment delivery behavior
- Required environment variables (names and placeholder examples only)
- CDN/storage configuration requirements
- Local development steps
- Migration commands
- Test commands
- Troubleshooting common playback failures
- How to revoke access after moderation/takedown

Never write actual credentials or secret values into documentation.

---

# 13. Deployment and configuration

- Preserve existing Docker and deployment conventions.
- Add only necessary environment variables to `.env.example`.
- Validate required configuration at startup.
- Keep production signing keys outside source control.
- Require HTTPS in production.
- Configure storage/CDN access so private HLS assets are not publicly readable.
- Ensure health/readiness checks reflect required dependencies without exposing internal details.
- Document deployment steps and rollback steps.
- Do not deploy or modify production infrastructure unless explicitly authorized.

---

# 14. Definition of done

Phase 7 is complete only when all applicable conditions are verified:

- A processed and approved video can obtain playback authorization.
- An eligible authorized viewer can load the master playlist.
- Child playlists and segments are also protected.
- Unauthorized users cannot access private or restricted video assets.
- Expired, tampered, revoked, and cross-video credentials are rejected.
- The Next.js player plays real HLS assets, not mock data.
- Playback errors are handled without leaking private information.
- Progress tracking is validated and does not trivially inflate views.
- Age and moderation checks are enforced by the backend.
- Automated tests pass.
- API and operations documentation are updated.
- No secrets or permanent private storage URLs are exposed.

If a CDN, TLS endpoint, production object storage, or external service is unavailable, clearly mark that part as **BLOCKED / NOT VERIFIED** rather than claiming production readiness.

---

# 15. Required final report

At the end, provide a concise but detailed report with these exact sections:

1. **Repository Audit**
2. **Existing Code Reused**
3. **Files Created**
4. **Files Modified**
5. **Backend APIs Implemented**
6. **Playback Authorization Design**
7. **Frontend Player Integration**
8. **Database Changes & Migrations**
9. **Security Controls**
10. **Tests Executed and Results**
11. **Infrastructure Verified**
12. **Configuration Still Required**
13. **Known Limitations / Risks**
14. **Production Readiness**
15. **Recommended Phase 8**

For every feature, use one of these statuses:

- `COMPLETE & TESTED`
- `IMPLEMENTED — NOT TESTED`
- `PARTIALLY IMPLEMENTED`
- `BLOCKED`
- `NOT IMPLEMENTED`

List exact commands executed and their real results. Distinguish unit tests, integration tests, manual checks, and infrastructure checks.

Do not claim a test passed if it was not run. Do not claim production readiness based only on successful compilation.

## Final instruction

Work incrementally. First audit, then propose a short implementation plan, then implement the smallest safe changes in the existing architecture. Preserve working code, avoid unnecessary rewrites, and report blockers honestly.

**Phase 7 must deliver secure, real HLS playback—not merely a visual player mockup.**
