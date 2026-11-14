# xhamtube.com — Phase 8: Frontend–Backend Integration & Complete Video Experience

## Role

Act as a senior full-stack engineer specializing in Next.js, React, TypeScript, Go APIs, PostgreSQL, secure authentication, and video-platform architecture.

Implement **Phase 8 — Frontend–Backend Integration & Complete Video Experience** for **xhamtube.com**, an 18+ video-sharing platform.

The goal is to connect the existing Next.js frontend to the real Go backend and deliver a coherent end-to-end user experience using real API data. This phase is primarily an integration phase: do not rebuild completed modules or silently invent missing backend functionality.

## Existing stack

- Frontend: Next.js, React, TypeScript, Tailwind CSS
- Backend: Go
- Database: PostgreSQL
- Cache/session/rate limiting: Redis, if already configured
- Object storage: MinIO or existing S3-compatible storage
- Video processing: FFmpeg/FFprobe
- Streaming: HLS
- Local infrastructure: existing Docker / Docker Compose setup

Preserve the repository's actual structure, libraries, naming conventions, API versioning, and design system.

---

# 1. Mandatory repository audit — no coding before this

Inspect the actual repository and current implementation.

Review:

- Next.js routes, layouts, server/client components, middleware, and route handlers
- Existing UI pages and reusable components
- API client, fetch wrappers, generated types, and environment configuration
- Go route registration, handlers, services, middleware, and response/error conventions
- PostgreSQL schema and migrations
- Authentication, refresh-token, session, cookie, and age-verification flows
- Video metadata, upload, processing, moderation, and playback APIs
- Search, feed, category, channel, and creator endpoints
- Comments, reactions, subscriptions, history, playlists, and notifications, if implemented
- Admin/moderation endpoints and role enforcement, if implemented
- Existing unit, integration, and end-to-end tests
- Docker Compose and local development instructions

Create or update:

- `docs/PHASE_8_AUDIT.md`
- `docs/PHASE_8_API_INTEGRATION_MATRIX.md`

For every frontend feature, classify it as:

- `CONNECTED & VERIFIED`
- `CONNECTED — NOT VERIFIED`
- `MOCK DATA ONLY`
- `UI ONLY`
- `BACKEND API MISSING`
- `PARTIALLY CONNECTED`
- `BLOCKED`

For every API integration, record:

- Frontend page/component
- HTTP method and endpoint
- Authentication requirement
- Request and response shape
- Loading, empty, and error behavior
- Backend implementation status
- Tests or verification evidence

**Do not assume previous phases are complete just because the user or a previous plan described them as complete. Verify the code.**

Before implementation, provide a short plan based on the audit. Preserve working code and avoid broad rewrites.

---

# 2. Phase 8 objective and boundaries

Connect the existing frontend to the actual backend so users can complete supported workflows without relying on mock data.

This phase includes:

1. A consistent typed API integration layer.
2. Authentication and account-state integration.
3. Real home/feed and video discovery data.
4. Video watch page and Phase 7 playback integration.
5. Search and category navigation, if backend APIs exist.
6. Creator/channel pages and upload status integration, if supported.
7. Existing social features integration, only where APIs already exist.
8. Consistent loading, empty, error, and retry states.
9. Frontend/backend contract tests and end-to-end verification.

Do not build an entirely new social system, recommendation engine, creator monetization system, or admin platform in this phase. If a required backend API is missing, document it and propose the smallest backend task needed. Do not fake a successful operation with local-only state.

---

# 3. API contract and typed client

Create or improve a centralized frontend API layer consistent with the current project.

Requirements:

- Use strict TypeScript types for API requests and responses.
- Reuse OpenAPI-generated types/client if the repository already has a reliable setup.
- Otherwise define maintainable types from verified backend contracts.
- Avoid `any` and unsafe type assertions.
- Centralize base URL, headers, credentials, timeout/abort handling, and error parsing.
- Handle JSON and non-JSON responses safely.
- Support request cancellation when pages/components unmount or query parameters change.
- Prevent stale responses from overwriting newer search/feed results.
- Normalize API errors into a consistent frontend error type.
- Never expose backend stack traces, internal database errors, secrets, or private storage paths to users.
- Do not duplicate business rules in the frontend as a replacement for backend enforcement.

Document any mismatches between the frontend's assumptions and actual Go API contracts.

## Environment configuration

- Inspect the existing environment variable naming.
- Use a public variable only for values safe to expose to the browser.
- Keep private service credentials server-side.
- Update `.env.example` with placeholder values only.
- Validate required frontend configuration at startup/build time where appropriate.
- Do not hardcode production API URLs in source code.
- Do not commit `.env` files or credentials.

---

# 4. Authentication and account-state integration

Connect existing UI authentication flows to the real Go authentication APIs.

Integrate only the flows that are actually implemented:

- Registration
- Login
- Logout
- Session restoration / current-user profile
- Access-token refresh, if supported
- Email verification
- Password reset
- Phone OTP, if supported
- Two-factor authentication, if supported
- Account deletion or re-authentication, if supported

Requirements:

- Follow the backend's actual token/session model.
- Prefer secure, HttpOnly, Secure, SameSite cookies where the existing architecture supports them.
- Never put refresh tokens, session secrets, or long-lived credentials in localStorage or sessionStorage.
- Do not invent a token format or refresh endpoint.
- Prevent refresh loops and concurrent refresh races.
- Handle expired sessions and revoked sessions gracefully.
- Clear user-specific client cache on logout or account switch.
- Protect authenticated pages without relying only on client-side route guards.
- Keep authorization and role checks enforced by the backend.
- Ensure the UI does not display admin controls based solely on a client-editable role value.
- Handle loading state during session restoration to avoid incorrect logged-out flashes.
- Avoid redirect loops between login, verification, age-gate, and protected pages.

Create a clear route-access matrix covering public, authenticated, age-eligible, creator, moderator, and admin routes according to the actual backend policy.

---

# 5. Age gate and age-eligibility integration

This is an 18+ platform. Integrate the existing age-verification system without weakening it.

Requirements:

- Treat the backend as the source of truth for age eligibility.
- Do not trust a frontend boolean, localStorage value, cookie set by JavaScript, or dismissible modal as proof of age.
- Fetch and handle the actual account eligibility state.
- Show an appropriate age-gate or verification flow when required.
- Do not fetch or render restricted video content before access is authorized.
- Handle verification pending, rejected, expired, and restricted states if the backend supports them.
- Avoid revealing sensitive verification details in client logs or analytics.
- Do not implement a bypass for development that could accidentally ship to production.

Document any external age-assurance provider or legal/geographic configuration that remains unconfigured.

---

# 6. Home page and video discovery integration

Connect the existing home page to real backend data.

Use the actual available endpoints for:

- Home feed
- Recommended videos, if implemented
- Trending videos
- Recent uploads
- Categories
- Featured creators/channels
- Search suggestions, if implemented

Requirements:

- Remove mock data from production execution paths.
- Keep mock fixtures only in tests or an explicit development-only mock mode.
- Use stable video IDs as React keys.
- Display only backend-authorized metadata.
- Implement pagination or cursor-based loading according to the actual API.
- Prevent duplicate cards when loading subsequent pages.
- Handle refresh, empty feed, network failure, and retry.
- Avoid infinite request loops and duplicate pagination requests.
- Preserve responsive layout and existing design.
- Use appropriate image optimization and safe thumbnail URLs.
- Do not expose private object-storage URLs.
- Do not claim personalized recommendations if the backend returns a generic feed.

---

# 7. Watch page and Phase 7 player integration

Integrate the watch page with the actual video metadata and secure playback flow implemented in Phase 7.

Requirements:

1. Resolve the video using its stable ID or the existing route convention.
2. Fetch safe video metadata from the backend.
3. Handle missing, deleted, private, restricted, under-review, processing, and unavailable videos.
4. Request a playback session only after access checks are satisfied.
5. Pass the authorized playback response to the Phase 7 player.
6. Never construct private storage URLs in the browser.
7. Do not bypass token expiry, revocation, age, visibility, or moderation checks.
8. Clean up player resources when navigating to another video.
9. Handle playback errors and retry according to the backend contract.
10. Integrate watch progress only if the backend endpoint is implemented and verified.

Display supported metadata such as:

- Title
- Description
- Creator/channel
- Upload date
- Duration
- Thumbnail
- View count, if supplied by the backend
- Available categories/tags

Do not show mock counts, fake engagement, or fabricated creator information in production.

If Phase 7 is incomplete, document the exact dependency and do not substitute public MinIO/S3 URLs or a fake player source.

---

# 8. Search and category integration

Connect search UI to the actual backend search contract.

If supported, integrate:

- Search query
- Search suggestions
- Search results
- Category filters
- Sorting
- Pagination
- Empty results
- Search errors

Requirements:

- Debounce suggestions without causing stale-response bugs.
- Cancel outdated requests.
- Encode query parameters correctly.
- Preserve search state in the URL where appropriate.
- Support browser back/forward navigation.
- Validate page size and filter values on the backend.
- Do not implement client-side filtering over a small mock list and present it as real platform search.
- If backend search is missing, mark it `BACKEND API MISSING` and provide a clear dependency report.

---

# 9. Creator and upload experience integration

Connect existing creator-facing pages to the actual APIs that exist.

Potential flows:

- Creator/channel profile
- Creator dashboard
- Start upload
- Upload progress
- Upload cancellation/retry
- Processing status
- Review/moderation status
- Video list and visibility state
- Edit metadata, if supported

Requirements:

- Reuse Phase 5 upload implementation and Phase 6 processing status.
- Reuse Phase 7 playback only for videos that are approved and authorized.
- Show upload, processing, review, rejected, failed, and ready states only when represented by real backend state.
- Do not mark a video published because upload or transcoding succeeded.
- Do not expose internal storage keys or signed upload credentials after their intended lifetime.
- Ensure ownership is enforced by the backend.
- Handle network interruption and retry safely according to upload API capabilities.
- Never create fake upload completion in frontend state.

If a creator API is missing, list the exact endpoint and data contract needed instead of inventing a local implementation.

---

# 10. Social interactions integration — only if implemented

Integrate existing backend functionality where present:

- Like/dislike or reactions
- Comments and replies
- Subscribe/unsubscribe
- Watch later
- History
- Playlists
- Notifications

Requirements:

- Use optimistic UI only with rollback on API failure.
- Prevent duplicate submissions.
- Handle unauthorized actions and expired sessions.
- Ensure the backend remains authoritative for counts and permissions.
- Paginate comments and long lists.
- Validate comment length and content on both frontend and backend.
- Do not add social features that have no backend support.
- Do not use hardcoded counters or local-only persistence in production.

Record missing APIs in the integration matrix.

---

# 11. Shared UI states and user experience

Create reusable, accessible patterns for:

- Initial loading
- Skeleton loading
- Empty state
- Error state
- Retry action
- Offline/network failure
- Unauthorized state
- Age verification required
- Processing state
- Under-review/restricted state
- Not found
- Rate limited
- Form validation
- Success feedback

Requirements:

- Keep messages understandable and non-technical.
- Do not leak private resource existence or internal backend errors.
- Use semantic HTML and accessible labels.
- Ensure keyboard navigation and visible focus states.
- Respect reduced-motion preferences.
- Preserve the existing dark visual design and responsive layout.
- Avoid redesigning unrelated pages.

---

# 12. Data fetching, cache, and state management

Inspect the current state-management and data-fetching libraries before choosing anything new.

- Reuse the current approach if it is suitable.
- Do not add React Query, SWR, Redux, Zustand, or another library just for fashion.
- Define cache keys consistently if a query library is already used.
- Invalidate relevant data after successful mutations.
- Prevent private user data from being shared across accounts.
- Clear or partition cache on logout/account switch.
- Avoid caching age-sensitive or authorization-sensitive responses in unsafe shared caches.
- Handle stale data intentionally.
- Use server components for safe server-side data retrieval where appropriate.
- Do not expose server-only environment variables or secrets to client components.

Document the selected data-fetching strategy.

---

# 13. Security and privacy review

Test the integrated application for:

- Missing or expired authentication
- Refresh-token race conditions
- CSRF risks according to the cookie/auth model
- CORS mismatch
- Untrusted redirect URLs
- XSS through video titles, descriptions, comments, or creator metadata
- Unsafe HTML rendering
- Unauthorized creator-dashboard access
- Client-side role tampering
- Age-gate bypass attempts
- Private video metadata leakage
- Private storage URL leakage
- Token exposure in logs, browser storage, referrers, or analytics
- Cross-account cache leakage
- Insecure error messages
- Rate-limit handling
- Production mock-data leakage

Keep security enforcement on the backend. Frontend checks improve UX but are not authorization controls.

---

# 14. Testing strategy

Use the existing test tools. Do not add a new test framework unless necessary.

## Frontend tests

Test at least:

- API client success and error parsing
- Session restoration
- Login/logout behavior
- Expired-session behavior
- Age-eligibility states
- Home feed loading, pagination, and empty state
- Search and stale-request handling
- Watch page metadata loading
- Playback session initialization
- Player error handling
- Creator upload/processing status
- Mutation success and rollback
- Cache clearing on logout

## Backend/API contract tests

Where needed, add or update tests for:

- Request and response contract
- Authentication and authorization
- Age verification
- Visibility and moderation enforcement
- Pagination validation
- Rate limiting
- Error response consistency

## End-to-end tests

Use the project's existing E2E framework if available. Test a realistic local workflow with test accounts and test media:

1. Start local dependencies using the documented Docker Compose command.
2. Register or seed a test account.
3. Authenticate.
4. Complete the supported test age-eligibility flow.
5. Load the real home feed.
6. Open a test video.
7. Request secure playback.
8. Play test HLS assets.
9. Exercise one supported creator/upload workflow.
10. Verify access denial for an unauthorized or restricted test video.
11. Log out and verify protected content is no longer accessible.

Use synthetic test data only. Never use real user data or production credentials.

If an external service is unavailable, mark the affected test `BLOCKED` and state why.

---

# 15. Local development and deployment configuration

- Verify frontend and backend can run together using documented local commands.
- Update Docker Compose only when required.
- Add placeholder-only environment variables to `.env.example`.
- Keep secrets out of Git.
- Document allowed frontend origins and backend CORS configuration.
- Document API base URL configuration for local and production environments.
- Confirm production builds do not include development mock data.
- Confirm HTTPS and secure cookie behavior in production configuration.
- Do not deploy or change production infrastructure unless explicitly authorized.

Create or update:

- `docs/PHASE_8_LOCAL_DEVELOPMENT.md`
- `docs/PHASE_8_E2E_TEST_PLAN.md`
- `docs/PHASE_8_RELEASE_CHECKLIST.md`

---

# 16. Definition of done

Phase 8 is complete only when all applicable requirements are verified:

- Frontend uses the real Go backend for supported workflows.
- API contracts are typed and centralized.
- Authentication/session behavior works with the actual backend.
- Age eligibility is enforced by the backend.
- Home feed displays real authorized data.
- Watch page uses real metadata and Phase 7 secure playback.
- Search uses real backend results where supported.
- Creator/upload pages show real backend states where supported.
- Social interactions use real APIs where supported.
- Loading, empty, error, and retry states work.
- Production routes do not silently fall back to mock data.
- Tests pass and their commands/results are recorded.
- No secrets, private URLs, or sensitive account data leak to the browser.
- Missing backend functionality is documented rather than simulated.

A successful frontend build alone does not mean Phase 8 is complete.

---

# 17. Required final report

Provide the final report using these exact sections:

1. **Repository Audit**
2. **Integration Plan**
3. **API Integration Matrix**
4. **Authentication & Session Integration**
5. **Age Verification Integration**
6. **Home Feed & Discovery**
7. **Watch Page & HLS Player**
8. **Search & Categories**
9. **Creator & Upload Experience**
10. **Social Features Integration**
11. **Files Created**
12. **Files Modified**
13. **Security Review**
14. **Tests Executed and Results**
15. **Local Development Verification**
16. **Remaining Backend Dependencies**
17. **Known Limitations / Risks**
18. **Production Readiness**
19. **Recommended Phase 9**

For each feature, use exactly one status:

- `COMPLETE & TESTED`
- `IMPLEMENTED — NOT TESTED`
- `PARTIALLY IMPLEMENTED`
- `BLOCKED`
- `NOT IMPLEMENTED`

List exact commands executed and actual results. Distinguish unit, integration, E2E, build, and manual tests. Never claim a test passed if it was not run.

## Final instruction

Audit first, plan second, implement third, test fourth. Preserve the current architecture and working code. Do not rebuild completed phases, do not invent backend APIs, and do not hide incomplete integrations behind mock data.

**Phase 8 must make the existing xhamtube.com frontend and backend work together as one real application.**
