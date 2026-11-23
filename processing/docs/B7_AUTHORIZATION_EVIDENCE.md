# B7.2 / B7.4: who may play a video now (authorization matrix) and a gap it found

Branch `b7/playback-authorization-matrix` (stacked on `fix/cleanup-test-hls-prefix`, which repairs a test that failed on `230bee8`). One production change (a SQL condition) and one test file.

## What decides playback today
1. **Issue time**: `Catalog.VideoDetail(id, viewer)` (SQL fragment `watchable`) decides who gets the video, and with it the signed stream URL. Rule: status PUBLISHED with `published_at`, not deleted, moderation not REMOVED,
   owner ACTIVE and not deleted, and either (PUBLIC or UNLISTED and moderation OK) for anyone, or the viewer is the owner.
2. **Serve time**: the gateway (`internal/stream`) asks `Catalog.VideoServable(id)` (cached for the gate TTL, 15 s) before every segment. It has no viewer.

## Gap found and fixed (B7.4)
`VideoServable` ignored the owner's account. **A token issued earlier kept streaming after its owner was suspended, banned, deleted or still pending.** The detail endpoint already hid such videos, so new viewers were
blocked while holders of an old token (valid 6 h) were not. `VideoServable` now also requires `users.status = 'ACTIVE' AND users.deleted_at IS NULL` (one JOIN on the creator). The takedown bound is unchanged: a removal or
suspension stops playback within the gate TTL.

## Evidence (real PostgreSQL 16 built from the branch's migrations)
- `TestPlaybackAuthorizationMatrix` (handler package): 450 videos (5 statuses x 3 visibilities x 3 moderation states x deleted yes/no x 5 owner states: active, suspended, banned, deleted, pending) x 5 viewers (owner, anonymous,
  plain user, moderator, admin) for `VideoDetail`, plus the gateway gate: **2700 checks**; 14 combinations may open, 12 may keep streaming; unknown ids open nothing. The expectations are written from the rules, not copied from the SQL.
- On the unmodified code the matrix **failed** for every suspended, banned, deleted and pending owner (gateway still served); with the fix it passes.
- Mutation check of the two rules: 15 mutants, **12 caught**. The 3 survivors are equivalent because the database already forbids the data that would tell them apart: `videos_published_at_only_when_published`
  and `videos_published_requirements` (status PUBLISHED exactly when `published_at` is set) make "status = PUBLISHED" and "published_at is not null" interchangeable; `users_deleted_consistency` (`status = 'DELETED'` exactly when
  `deleted_at` is set) makes the owner's `deleted_at` test redundant with `status = 'ACTIVE'`.
- Whole backend with `-race`: only the failure that already exists on `230bee8` appears when this branch is built without `fix/cleanup-test-hls-prefix`; with the fix underneath it is green (numbers in the report).

## Findings that need an owner decision (not changed)
1. **A video that becomes PRIVATE or RESTRICTED keeps streaming for tokens issued before** (up to 6 h). The gateway cannot tell the owner's own playback of a private video from a stranger's, because the token does not say whom it was
   issued to. Closing it means changing the token model (decision D4): for example a claim saying "issued to the owner", checked by the gate when the video is not public. The matrix pins today's behaviour with a comment.
2. **Moderators and admins get no special access** through the watch route: a reported PRIVATE or RESTRICTED video cannot be opened in the player by staff (they see data in admin tooling). Whether staff review needs playback is a product decision.
3. The token is a bearer of (video, expiry): not bound to a user or session; there is no `POST /playback` session and no `playback-info` endpoint, and the gateway has no `Range` support (not needed for MPEG-TS segments).
4. **Age gate**: `RequireAge` wraps routes through `handler.Gate`; this test environment runs without authentication, so the age rule on the video route is **not** covered here. It needs the auth-wired router.

## Limits
Repository-level on real PostgreSQL; not through the HTTP router with a token, and not against real object storage. The mutation script patches a /tmp copy and is not committed.
