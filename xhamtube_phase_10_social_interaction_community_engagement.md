# xhamtube.com — Phase 10: Social Interaction & Community Engagement System

## Role

Act as a senior full-stack engineer and application security engineer specializing in Next.js, React, TypeScript, Go, PostgreSQL, Redis, real-time systems, and user-generated-content moderation.

Implement **Phase 10 — Social Interaction & Community Engagement System** for **xhamtube.com**, an 18+ video-sharing platform.

The goal is to connect and complete the platform's social interaction features using real backend APIs and persistent data. Users should be able to interact with eligible public content, while creators and moderators have clearly separated permissions.

This is not a frontend-only feature. Every mutation, permission check, counter, and moderation action must be enforced by the backend.

## Existing stack

Preserve the actual repository architecture:

- Frontend: Next.js, React, TypeScript, Tailwind CSS
- Backend: Go
- Database: PostgreSQL
- Redis: only if already configured and suitable
- Background jobs / queues: reuse the existing system if present
- Notifications: reuse the existing implementation if present
- Infrastructure: existing Docker / Docker Compose setup

Do not add a new framework, message broker, or real-time service without first proving it is necessary.

---

# 1. Mandatory repository audit

Before coding, inspect the real repository.

Review:

- User and channel schemas
- Authentication, session, age eligibility, and RBAC
- Video ownership, visibility, moderation, and playback access
- Phase 8 frontend/API integration
- Phase 9 creator/channel implementation
- Existing comments, replies, reactions, likes, subscriptions, and playlists
- Existing notification tables, APIs, workers, and delivery channels
- Redis and queue setup
- WebSocket/SSE infrastructure, if any
- Rate limiting, abuse detection, and audit logging
- Frontend components and current interaction patterns
- Database migrations and test framework
- API documentation and local development instructions

Create or update:

- `docs/PHASE_10_AUDIT.md`
- `docs/PHASE_10_SOCIAL_DATA_MODEL.md`
- `docs/PHASE_10_API_MATRIX.md`

Classify each feature:

- `COMPLETE & TESTED`
- `IMPLEMENTED — NOT TESTED`
- `PARTIALLY IMPLEMENTED`
- `UI ONLY`
- `MOCK DATA ONLY`
- `MISSING`
- `BLOCKED`

Reuse existing working code. Do not duplicate tables, routes, notification systems, or state models.

First produce a concise implementation plan based on verified gaps.

---

# 2. Phase 10 scope

Implement only the social features that fit the existing product and backend capabilities:

1. Video reactions
2. Comments and threaded replies
3. Comment reactions, if supported by the product policy
4. Creator comment-management tools
5. Channel subscriptions
6. Watch later
7. User playlists, if already planned or supported
8. Notifications for relevant social events
9. Reporting and abuse controls
10. Frontend integration and accessibility

Do not implement live chat, private messaging, group communities, creator monetization, or public user-to-user contact in this phase unless already present and explicitly in scope. Record them as future work.

---

# 3. Video reactions

Inspect the current reaction model and product rules.

Implement or integrate a consistent reaction API.

Possible actions:

- Like
- Remove like
- Dislike, only if the product explicitly supports it

Requirements:

- Require authentication and backend age eligibility according to platform policy.
- Enforce one effective reaction per user per video.
- Use a database uniqueness constraint such as `(user_id, video_id)`.
- Make repeated requests idempotent.
- Handle switching a reaction without double-counting.
- Return authoritative reaction state and counts from the backend.
- Do not trust client-supplied counters.
- Prevent reactions to deleted, restricted, or inaccessible videos.
- Apply rate limits.
- Ensure retries and concurrent requests cannot corrupt counts.
- Do not reveal private viewer identities.
- Use optimistic UI only with rollback on failure.

Do not add dislikes if they are not part of the verified product design.

---

# 4. Comments and threaded replies

Implement a safe, persistent comment system if it is missing.

Required capabilities:

- Create a comment on an accessible video
- List comments with pagination
- Reply to a comment
- Edit or delete one's own comment according to policy
- Display creator/moderator moderation state appropriately
- Optional comment reactions only if explicitly supported

Requirements:

- Require authentication and age eligibility according to platform policy.
- Verify that the viewer can access the target video.
- Enforce maximum comment length and request size on the backend.
- Validate Unicode and normalize input safely.
- Store plain text or a documented safe rich-text format.
- Escape output; never render untrusted comment HTML directly.
- Prevent stored and reflected XSS.
- Prevent arbitrary nested reply depth; choose and document a bounded threading model.
- Paginate comments and replies.
- Use stable ordering, such as newest or top, with documented cursor behavior.
- Avoid N+1 queries.
- Apply rate limits, duplicate-submission protection, and abuse controls.
- Handle deleted parent comments without breaking reply context.
- Preserve required audit/evidence records where moderation policy requires it.
- Do not expose email, phone, private profile details, or internal account IDs.

Define and enforce comment states, for example:

- `VISIBLE`
- `HIDDEN_BY_CREATOR`
- `UNDER_REVIEW`
- `REMOVED_BY_MODERATION`
- `DELETED_BY_AUTHOR`

Use the existing state model if one exists. Do not create conflicting duplicate states.

---

# 5. Creator comment management

Integrate creator-side moderation tools for comments on videos owned by that creator.

Potential actions:

- View comments on owned videos
- Hide or unhide comments within policy
- Pin a comment, if supported
- Reply as the creator/channel, if the product supports it
- Report a comment to platform moderators

Requirements:

- Verify video/channel ownership on every backend request.
- Separate creator moderation from platform moderation.
- Creators cannot erase platform audit history.
- Creators cannot reverse a platform moderator's removal or restriction.
- Record moderation actions with actor, target, reason/category, and timestamp where appropriate.
- Prevent creators from moderating comments on another creator's content.
- Apply rate limits and safe input validation.

Do not provide creators with unrestricted access to platform moderation controls.

---

# 6. Channel subscriptions

Reuse Phase 9 subscription functionality if present.

Implement or connect:

- Subscribe
- Unsubscribe
- Current subscription state
- Subscriber count
- Subscribed channels list, if supported

Requirements:

- Enforce authentication and applicable age eligibility.
- Enforce uniqueness for `(user_id, channel_id)`.
- Make subscribe/unsubscribe idempotent.
- Handle concurrent requests and retries safely.
- Prevent self-subscription if that is the product rule.
- Keep subscriber identity private by default.
- Return only aggregate counts publicly unless the user privacy policy explicitly allows more.
- Ensure counts are derived from reliable data.
- Clear or invalidate relevant caches after mutations.
- Apply rate limits to prevent spam.
- Integrate notification creation only through the existing notification service/queue.

Do not create a second subscription model if Phase 9 already implemented one.

---

# 7. Watch later and playlists

Inspect the existing schema before adding features.

## Watch later

If supported or in scope, implement:

- Add video to watch later
- Remove video from watch later
- List saved videos with pagination

Requirements:

- Private to the authenticated account.
- Never expose a user's saved list publicly.
- Enforce video access and visibility policy.
- Avoid duplicate entries with a unique constraint.
- Remove or safely represent videos that later become deleted/restricted.
- Clear user-specific cache on logout/account switch.

## Playlists

Only implement if the product already includes playlists or the current architecture has a clear requirement.

Potential capabilities:

- Create playlist
- Edit title/description
- Set supported privacy level
- Add/remove videos
- Reorder items
- List accessible playlists

Requirements:

- Verify playlist ownership for every mutation.
- Validate video access and visibility.
- Prevent duplicate entries unless duplicates are an explicit feature.
- Use transaction-safe ordering.
- Do not allow private videos to leak through public playlists.
- Respect takedown and moderation restrictions.
- Do not introduce collaborative playlists or complex sharing unless explicitly required.

---

# 8. Notifications

Inspect the existing notification system and reuse it.

Potential events:

- New subscriber
- New comment on creator's video
- Reply to a user's comment
- Creator reply
- Comment moderation outcome, where appropriate
- Other existing supported social events

Requirements:

- Create notifications server-side after the relevant action succeeds.
- Use the existing queue for asynchronous delivery if configured.
- Avoid duplicate notifications on retries using an idempotency key or unique event identity.
- Do not notify users about their own actions unless explicitly intended.
- Do not reveal private content details to unauthorized recipients.
- Keep notification preferences and privacy rules in mind.
- Paginate notification lists.
- Support mark-as-read and mark-all-as-read if already supported or required.
- Do not send email, SMS, or push notifications unless a configured provider and consent/preferences system exist.
- Do not log private notification payloads or tokens.

## Real-time delivery

If WebSocket or SSE is already implemented, integrate it safely.

If not, use a simple existing polling or refresh pattern rather than introducing a new real-time infrastructure stack without need.

If real-time delivery is added:

- Authenticate the connection.
- Authorize each user's subscription to their own notification stream.
- Validate origin and connection limits.
- Handle reconnects and duplicate events.
- Do not expose other users' notifications.
- Document scaling and deployment requirements.

---

# 9. Reporting and abuse prevention

Integrate the existing report/moderation workflow. If missing, propose the smallest backend-supported addition.

Potential report targets:

- Video
- Comment
- Channel
- User, if supported by platform policy

Potential report categories:

- Non-consensual intimate imagery
- Suspected exploitation or illegal content
- Underage-content concern
- Copyright infringement
- Harassment or threats
- Spam or deceptive behavior
- Other policy violation

Requirements:

- Provide clear report reasons and optional contextual details with strict length limits.
- Prevent report spam and duplicate active reports.
- Keep reporter identity and report details private.
- Do not automatically reveal the reporter to the reported user.
- Route reports to authorized moderation staff.
- Create audit records.
- Support takedown/restriction decisions through the platform's existing moderation system.
- Do not allow creators to resolve platform-level reports involving serious safety concerns.
- Provide an urgent escalation path for suspected illegal or non-consensual content according to the platform's operational policy.
- Do not make report submission itself publicly visible.

The platform must have a documented process for reviewing and removing non-consensual intimate imagery, suspected exploitation, and illegal content. Do not build features that facilitate harassment, doxxing, coercion, or evasion of moderation.

---

# 10. Backend API design

Follow existing route conventions and versioning. Reuse existing endpoints where possible.

Potential endpoint shapes, only if missing:

- `PUT /api/v1/videos/{videoId}/reaction`
- `DELETE /api/v1/videos/{videoId}/reaction`
- `GET /api/v1/videos/{videoId}/comments`
- `POST /api/v1/videos/{videoId}/comments`
- `POST /api/v1/comments/{commentId}/replies`
- `PATCH /api/v1/comments/{commentId}`
- `DELETE /api/v1/comments/{commentId}`
- `PUT /api/v1/channels/{channelId}/subscription`
- `DELETE /api/v1/channels/{channelId}/subscription`
- `GET /api/v1/me/subscriptions`
- `PUT /api/v1/me/watch-later/{videoId}`
- `DELETE /api/v1/me/watch-later/{videoId}`
- `GET /api/v1/me/watch-later`
- `GET /api/v1/me/notifications`
- `PATCH /api/v1/me/notifications/{notificationId}`
- `POST /api/v1/reports`

These are examples, not a requirement to duplicate existing routes.

For every endpoint:

- Define typed request/response schemas.
- Enforce authentication and backend authorization.
- Enforce age eligibility and video access policy.
- Validate all inputs.
- Apply pagination and rate limits where needed.
- Use consistent errors.
- Add OpenAPI documentation.
- Add automated tests.

Never trust frontend-supplied user IDs, channel ownership, reaction counts, notification recipients, moderation decisions, or report status.

---

# 11. Frontend integration

Integrate features into the existing UI without redesigning unrelated pages.

Potential UI components:

- Video reaction controls
- Comment list and composer
- Reply thread
- Creator comment-management panel
- Subscribe button and subscriber count
- Watch-later control and saved list
- Playlist controls, if supported
- Notification list and unread indicator
- Report dialog

Requirements:

- Use real backend data only in production.
- Keep mock fixtures isolated to tests or explicit development-only mode.
- Add loading, empty, error, and retry states.
- Use accessible labels and keyboard interactions.
- Prevent duplicate form submissions.
- Use optimistic updates only when rollback is implemented.
- Handle expired sessions.
- Clear private user data from cache on logout/account switch.
- Do not expose private notifications, watch-later items, or playlists.
- Ensure mobile and desktop layouts work.
- Respect reduced-motion preferences.
- Avoid fake counts or hardcoded success messages.

---

# 12. Database and migrations

Inspect existing tables first.

Potential entities:

- Reactions
- Comments
- Subscriptions
- Watch-later entries
- Playlists and playlist items
- Notifications
- Reports
- Moderation/audit events

Requirements:

- Reuse existing schema where possible.
- Add only missing tables/columns.
- Add foreign keys, unique constraints, indexes, and appropriate check constraints.
- Use transactions for multi-step mutations.
- Make migrations reversible using the existing tool.
- Define deletion/retention behavior carefully.
- Avoid storing unnecessary personal data.
- Keep audit evidence where policy requires it.
- Do not duplicate counters as the sole source of truth.
- Document migration and rollback commands.

---

# 13. Security, privacy, and performance review

Test at least:

- Unauthenticated reaction/comment/subscription attempts
- Cross-account comment edit/delete
- Cross-channel creator moderation attempts
- IDOR on comment, video, channel, playlist, and notification IDs
- Age-ineligible user interactions
- Interaction with private/restricted videos
- XSS in comments and profile/video metadata
- SQL injection through filters and search parameters
- Duplicate reaction/subscription/comment requests
- Concurrent counter updates
- Notification recipient tampering
- Private watch-later/playlist leakage
- Report enumeration and reporter identity leakage
- Rate-limit bypass and spam
- Cache leakage across accounts
- WebSocket/SSE authorization, if applicable
- CSRF according to the authentication model

Performance requirements:

- Paginate large collections.
- Avoid N+1 queries.
- Add indexes based on actual query patterns.
- Use transaction-safe updates.
- Throttle notification and progress-related writes.
- Cache only safe public data.
- Invalidate user-specific caches correctly.
- Do not publicly cache private responses.

---

# 14. Automated tests

Use the existing frameworks.

## Backend tests

At minimum:

- Reaction create/remove/switch and idempotency
- Comment create/list/reply/edit/delete
- Comment length and XSS-safe handling
- Creator ownership checks
- Subscription idempotency and counts
- Watch-later privacy and uniqueness
- Playlist ownership/privacy, if implemented
- Notification recipient isolation and deduplication
- Report privacy, validation, and duplicate prevention
- Age and video-access enforcement
- Rate limiting and authorization failures

## Frontend tests

At minimum:

- Reaction state and rollback
- Comment pagination and reply flow
- Comment validation and submission
- Subscribe/unsubscribe behavior
- Watch-later add/remove
- Playlist controls, if implemented
- Notification loading/read state
- Report submission and error handling
- Logout/account-switch cache clearing

## Integration / E2E tests

Where practical, use synthetic test accounts and videos:

1. Authenticate as eligible test user A.
2. Load an approved test video.
3. Add and remove a reaction.
4. Create a comment and reply.
5. Subscribe to a test channel.
6. Add a video to watch later.
7. Trigger a notification through a supported event.
8. Submit a report against a synthetic test item.
9. Verify private data is visible only to the correct account.
10. Attempt unauthorized cross-account actions and verify rejection.
11. Verify restricted or under-review content cannot be interacted with beyond policy.

Never use production accounts, real user data, or real sensitive media in tests.

Record any tests blocked by missing services or configuration.

---

# 15. Documentation

Create or update:

- `docs/PHASE_10_SOCIAL_ARCHITECTURE.md`
- `docs/PHASE_10_MODERATION_AND_REPORTING.md`
- `docs/PHASE_10_PRIVACY_RULES.md`
- `docs/PHASE_10_OPERATIONS.md`

Document:

- Social data model
- API contracts
- Reaction/comment rules
- Subscription privacy
- Watch-later and playlist visibility
- Notification event flow and deduplication
- Reporting and moderation workflow
- Rate limits
- Retention and deletion behavior
- Required environment variables (placeholder values only)
- Migrations, tests, and troubleshooting

Do not include real secrets, private user information, or production credentials.

---

# 16. Definition of done

Phase 10 is complete only when applicable features are verified:

- Reactions persist and remain consistent under retries.
- Comments and replies are paginated, validated, and safely rendered.
- Creator moderation is ownership-checked and cannot override platform moderation.
- Subscriptions are persistent and idempotent.
- Watch-later and playlists remain private according to policy.
- Notifications are delivered only to authorized recipients and are deduplicated.
- Reporting is private and connected to the moderation workflow.
- Backend enforces age eligibility, video access, and authorization.
- Frontend uses real APIs and handles failures correctly.
- Automated tests pass and actual results are recorded.
- No private account data, tokens, or restricted content leak.
- Missing features are documented rather than simulated.

A UI-only implementation or successful build does not qualify as completion.

---

# 17. Required final report

Use these exact sections:

1. **Repository Audit**
2. **Existing Functionality Reused**
3. **Implementation Plan**
4. **Reactions**
5. **Comments & Replies**
6. **Creator Comment Management**
7. **Subscriptions**
8. **Watch Later & Playlists**
9. **Notifications**
10. **Reporting & Abuse Prevention**
11. **Database Changes & Migrations**
12. **API Endpoints Implemented**
13. **Frontend Integration**
14. **Files Created**
15. **Files Modified**
16. **Security & Privacy Review**
17. **Tests Executed and Results**
18. **Configuration / Infrastructure Required**
19. **Known Limitations / Risks**
20. **Production Readiness**
21. **Recommended Phase 11**

For every feature, use one of these statuses:

- `COMPLETE & TESTED`
- `IMPLEMENTED — NOT TESTED`
- `PARTIALLY IMPLEMENTED`
- `BLOCKED`
- `NOT IMPLEMENTED`

List exact commands executed and real outputs. Separate unit, integration, E2E, build, and manual checks. Never claim tests passed if they were not run.

## Final instruction

Work incrementally:

1. Audit the real repository.
2. Report existing functionality and gaps.
3. Propose the smallest safe implementation plan.
4. Implement only missing features.
5. Add migrations and tests.
6. Verify ownership, age eligibility, privacy, and moderation.
7. Produce the final report.

Preserve existing working code. Do not rebuild previous phases, invent APIs, or hide incomplete work behind mock data.

**Phase 10 must deliver a real, secure social interaction system for xhamtube.com—not just buttons and counters in the frontend.**
