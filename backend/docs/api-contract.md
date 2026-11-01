# API contract (derived from the frontend `src/config/endpoints.ts`)

The frontend is the source of truth. The backend follows its routes and payload shapes.

## Conventions
- Base path `/api/v1`. JSON only. Cookie session (`xh_session`, httpOnly, Secure, SameSite=Lax) + `credentials: "include"`.
- **Success**: the payload itself, no envelope. Lists are `{ "items": [...], "nextCursor": string|null }`. Actions with no result return `204`.
- **Error**: `{ "code": "snake_case", "message": "...", "fields": { "email": "..." } }`. The frontend reads only `code` and the `Retry-After` header; `message` is for logs/tools.
- Codes the frontend already understands: `invalid_credentials`, `invalid_code`, `invalid_token`, `conflict`, `email_taken`, `username_taken`, `verification_required`, `rate_limited` (429 + `Retry-After`), `already_reported` (409), `too_large` (413), `unauthorized` (401), `not_found`, `validation_failed` (422), `internal` (500).
- Pagination is cursor-based and opaque (`?cursor=`); page size is chosen by the server.
- Private/unlisted/removed videos never appear in public lists. Anonymous users may call read endpoints only after the age gate (B3).

## Endpoint map by backend phase
| Phase | Endpoints |
|---|---|
| B2 read | `GET /videos`, `/videos/:id`, `/search`, `/search/suggestions`, `/trending`, `/shorts`, `/live`, `/channels/popular`, `/channels/:id`, `/channels/:id/videos`, `/channels/:id/playlists`, `/reports/reasons`, `/sitemap/videos` |
| B3 auth | `POST /auth/register`, `/auth/login`, `/auth/logout`, `/auth/logout-all`, `GET /auth/me`, `POST /auth/verify`, `/auth/verify/resend`, `/auth/forgot-password`, `/auth/reset-password`, `/auth/change-password`, `PATCH /account`, `POST /account/delete`, `GET/PATCH /settings`, age-gate endpoint |
| B4 social | `PUT /videos/:id/reaction`, `PUT /channels/:id/subscription`, comments (`GET/POST /videos/:id/comments`, `GET /comments/:id/replies`, `PUT /comments/:id/like`, `DELETE /comments/:id`), `GET/DELETE /history`, `DELETE /history/:videoId`, watch-later (`GET /watch-later`, `/watch-later/ids`, `PUT/DELETE /watch-later/:videoId`), `GET /liked`, `GET /subscriptions/channels`, `/subscriptions/feed`, playlists (`GET/POST /playlists`, `GET/PATCH/DELETE /playlists/:id`, `PUT/DELETE /playlists/:id/videos/:videoId`, `PUT /playlists/:id/order`), `POST /videos/:id/not-interested`, `PUT/DELETE /channels/:id/block`, `POST /reports` |
| B5 upload | `POST /upload/init`, `POST /upload/:id/complete`, `DELETE /upload/:id`, `POST /upload/:id/assets`, `PATCH /videos/:id/metadata`, `POST /videos/:id/publish` |
| B6 processing | `GET /upload/:id/status` (progress/encoding/ready/failed). **The FFmpeg worker is not built yet**, see README. |
| B8 notifications | `GET /notifications`, `POST /notifications/read`, `GET /notifications/unread-count`, WebSocket |
| B9 studio/admin | `GET /studio/overview`, `/studio/videos`, `/studio/analytics`, `/studio/comments`, `PATCH/DELETE /studio/videos/:id`, `POST /studio/comments/:id/approve`, admin + moderation APIs |

## Mismatches with the original backend prompt (decisions)
| Prompt | Frontend (used) | Why |
|---|---|---|
| `{success,data}` envelope | raw payload | frontend `api()` returns `res.json()` directly |
| `POST /videos/:id/like` | `PUT /videos/:id/reaction` | frontend already built |
| `/creators/:id/subscribe` | `PUT /channels/:id/subscription` | same |
| `UPPER_CASE` error codes | `lower_snake` codes | frontend switches on lowercase codes |

## B2 read API: behaviour notes
- **Pagination**: feeds, shorts, live and channel videos use keyset cursors (`published_at`/`view_count` + id). Cursors are opaque, bound to their listing (a "latest" cursor is rejected on "popular"), and invalid ones return `400 invalid_cursor`. Trending is a bounded ranked list (max 100 items) and search uses offset cursors (max offset 1000).
- **What counts as public**: `READY` + `PUBLIC` + published + not deleted, and the creator is `ACTIVE` and not deleted. `UNLISTED` videos open by direct id/link but are never listed, searched or put in the sitemap. `PRIVATE`, drafts, removed and soft-deleted videos return `404 video_not_found`.
- **Counts never leak hidden content**: a channel's `videoCount`/`totalViews` and a playlist's `videoCount`/thumbnail only consider public videos.
- **Home chips** (`GET /videos?category=`): empty/`all`/`new` = newest, `popular` = most viewed, `trending` = trending "now", `live`, `shorts`, or a category slug (`music`, `gaming`, ...). Unknown slug = empty list; malformed value = `422`.
- **Search** (`GET /search`): `q` (max 100 chars, control characters and invalid UTF-8 removed), `sort=relevance|latest|views`, `type=all|videos|channels|playlists|live`, `duration=any|short(<4m)|medium(4-20m)|long(>20m)`, `date=any|today|week|month|year`. Channel and playlist matches come first on `all`, then ranked videos. `%` and `_` are literals, not wildcards. `total` is capped at 10,000.
- **Suggestions** (`GET /search/suggestions?q=`): needs 2+ characters, max 8, deduplicated, prefix matches first, only public titles and active channel names.
- **Media URLs** are built from `CDN_URL` and storage keys. With `CDN_URL` unset every image URL is `null` (the frontend shows placeholders). `streamUrl` is set only when HLS variants exist; B7 replaces it with signed URLs.
- **Not yet personalised** (B4): `reaction` is `null` and `isSubscribed` is `false`; blocked creators and "not interested" are not filtered yet. Signed-in owners can already open their own PRIVATE videos.
- **Age gate**: content routes require the signed `xh_age` cookie (`403 age_verification_required`); `/reports/reasons` and `/sitemap/videos` are open. See [auth.md](../../security/auth.md).

## B3 auth API (all JSON, cookie session)
| Endpoint | Auth | Body -> result |
|---|---|---|
| `POST /age/verify` | - | `{confirmed:true}` -> 204 + `xh_age` cookies |
| `GET /age/status` | - | `{verified}` |
| `POST /auth/register` | age cookie | `{email,username,password}` -> 201 `User` + session |
| `POST /auth/login` | - | `{identifier,password}` -> `User`; unverified email -> **403 `verification_required` with the session cookie set** |
| `POST /auth/logout`, `/auth/logout-all` | session (logout: optional) | 204, cookie cleared |
| `GET /auth/me` | session | `User` (401 `unauthenticated`) |
| `POST /auth/verify` / `/auth/verify/resend` | session | `{code}` -> `User` / 204 |
| `POST /auth/forgot-password` | - | `{email}` -> 204 always |
| `POST /auth/reset-password` | - | `{token,password}` -> 204 (all sessions revoked) |
| `POST /auth/change-password` | session | `{currentPassword,newPassword}` -> 204 (other devices signed out) |
| `PATCH /account` | session | `{displayName,bio}` -> `User` |
| `POST /account/delete` | session | `{password}` -> 204 |
| `GET /settings`, `PATCH /settings` | session | nested partial patch -> `Settings` / 204 |

`User` = `{id, username, displayName, email, avatarUrl, emailVerified, isCreator, joinedAt, bio}`. `isCreator` is true for roles CREATOR and above.


## B4 social API (cookie session; "verified" = signed in with a confirmed email)
| Endpoint | Needs | Body -> result |
|---|---|---|
| `PUT /videos/{id}/reaction` | verified | `{reaction: "like"\|"dislike"\|null}` -> 204 (idempotent) |
| `PUT /channels/{id}/subscription` | verified | `{subscribed: bool}` -> 204 |
| `GET /subscriptions/channels`, `/subscriptions/feed` | session + age | `ChannelResult[]`, `Paginated<Video>` |
| `GET /videos/{id}/comments?sort=top\|new&cursor=` | age | `Paginated<Comment>` (pinned first on page 1) |
| `POST /videos/{id}/comments` | verified | `{text, parentId?}` -> 201 `Comment` (reply to a reply attaches to the top-level comment) |
| `GET /comments/{id}/replies` | age | `Paginated<Comment>` oldest first |
| `PUT /comments/{id}/like` | verified | `{liked: bool}` -> 204 |
| `PUT /comments/{id}/pin` | verified (video creator or moderator) | `{pinned: bool}` -> 204 |
| `DELETE /comments/{id}` | session (author, video creator or moderator) | 204 |
| `POST /videos/{id}/view` | age | `{source?}` -> 204 (counted once per viewer per 30 min; bots/creator ignored) |
| `POST /videos/{id}/progress` | session | `{positionSec, watchedSec}` -> 204 |
| `GET /history`, `DELETE /history`, `DELETE /history/{videoId}` | session (+age for GET) | `Paginated<{video, watchedAt, progressSec}>` |
| `GET /watch-later`, `GET /watch-later/ids`, `PUT/DELETE /watch-later/{videoId}` | session | max 500 (`422 limit_reached`) |
| `GET /liked` | session + age | `Paginated<Video>` |
| `GET/POST /playlists`, `GET/PATCH/DELETE /playlists/{id}`, `PUT/DELETE /playlists/{id}/videos/{videoId}`, `PUT /playlists/{id}/order` | verified for writes | max 100 playlists, 200 videos each |
| `POST /videos/{id}/not-interested`, `PUT/DELETE /channels/{id}/block` | session | 204 |
| `POST /reports` | session | `{targetType: video\|comment\|channel\|user, targetId, reason, details?}` -> 204; `409 already_reported`; `details` required for `other` |

Extra error codes: `comments_disabled` 403, `duplicate_comment` 409, `limit_reached` 422, `not_pinnable` 422, `invalid_order` 422,
`cannot_subscribe_to_self` / `cannot_block_self` / `cannot_report_self` 422, `target_not_found` 404, plus `*_not_found` for video, comment, playlist, channel.
Comment creation: 10 per minute and 100 per hour per user; the same text on the same video within 5 minutes is `409 duplicate_comment`.
The pinned comment, reactions and `isSubscribed` appear for the signed-in viewer on video detail, channel pages, popular channels and search.
Personal feeds hide blocked channels and "not interested" videos; a channel's own page and direct links still work.
The Watch page calls `POST /videos/{id}/view` and `/progress` through `usePlaybackReporting` (one view per play, progress every 15 s of playback, on pause and on page hide).

## B5 upload API (verified account; needs `STORAGE_*`, otherwise `503 uploads_disabled`)
The video bytes never pass through the API: the browser PUTs each part straight to object storage with presigned URLs.

| Endpoint | Body -> result |
|---|---|
| `POST /upload/init` | `{filename, size, contentType}` -> 201 `{uploadId, videoId, partSize, parts:[{partNumber,url}]}`. `.mp4 .mov .mkv .webm .avi`, max 5 GiB, 16 MiB parts. 10 inits/hour, 5 open uploads. |
| `POST /upload/{id}/complete` | `{parts:[{partNumber, etag}]}` -> 204 (idempotent). The server re-lists the parts from storage and compares count, ETags and exact sizes (`422 parts_mismatch`, retryable), checks the final size (`422 size_mismatch`) and the container signature (`422 invalid_file`; the object is deleted). |
| `DELETE /upload/{id}` | -> 204. Aborts/discards a draft; `409 already_published` for live videos. |
| `GET /upload/{id}/status` | -> `{status: processing\|encoding\|ready\|published\|failed, progress, thumbnails[], error?}` |
| `POST /upload/{id}/assets` | `{kind: thumbnail\|caption, contentType, size, lang?}` -> 201 `{assetId, url}` (presigned PUT). Thumbnails: jpg/png/webp <= 2 MB (max 10). Captions: .vtt/.srt <= 1 MB (max 12 languages); converted to clean WebVTT at publish. |
| `PATCH /videos/{id}/metadata` | draft fields (all optional) -> 204 |
| `POST /videos/{id}/publish` | same fields + `thumbnail`, `captions[]`, **`consent: true`** (required) -> 204. Errors: `409 not_ready`, `409 already_published`, `403 video_removed`, `422 file_too_large_for_short`. |

The consent declaration (everyone is 18+, consented, rights held) is stored append-only in `upload_consents` with the hashed IP.
Publishing a PUBLIC video notifies subscribers (up to 5,000 per publish). Anyone who uploads becomes a creator (`users.is_creator`).
Error codes: `upload_not_found`, `upload_in_progress`, `upload_closed`, `limit_reached`, `storage_unavailable` (502), `video_not_found`.

## B8 notifications, Studio, realtime
| Endpoint | Result |
|---|---|
| `GET /notifications?cursor=&unread=1` | `Paginated<{id,type,title,body,href,createdAt,read}>`; text and links are built at read time; likes are never shown; notices about removed/private videos disappear |
| `GET /notifications/unread-count` | `{count}` (capped at 100) |
| `POST /notifications/read` | `{ids?:[...], all?:true}` -> 204 (own notices only, max 100 ids) |
| `GET /studio/overview?period=7d\|28d\|90d` | totals (views, watch hours, subscribers, delta), daily views, top 5 videos; `revenue` is `null` |
| `GET /studio/analytics?period=` | views and watch-time series, country/device/traffic shares (sum to 100), likes, comments, average view duration |
| `GET /studio/videos?filter=all\|published\|draft\|processing\|private\|unlisted&cursor=` | `Paginated<StudioVideo>` |
| `PATCH /studio/videos/{id}` | `{visibility}` -> 204; `DELETE` soft-deletes -> 204 (own videos only) |
| `GET /studio/comments?filter=all\|held&cursor=` / `POST /studio/comments/{id}/approve` | held-for-review inbox |
| `GET /ws` (WebSocket) | needs the session cookie and an allowed `Origin`; max 5 sockets per user, 20 topics per socket |

Realtime protocol: client sends `{"action":"subscribe"\|"unsubscribe","topic"}`, server sends `{"topic","data"}`. Topics: `notifications` and `video:<id>` (owner only; anything else is silently ignored). Events are pings only, the browser re-fetches through the normal API. Events fan out through Redis, so any API instance (or the media worker, publish a JSON `{"u":userId,"t":topic}` to channel `redx:rt`) can trigger them.
Set `NEXT_PUBLIC_WS_URL=wss://api.example.com/api/v1/ws` to turn it on; polling keeps working without it.

## B9 moderation and administration (no web UI yet)
Roles: MODERATOR < ADMIN < SUPER_ADMIN. Nobody can act on themselves or on an account of equal/higher role. Every action is one transaction with its append-only record.

| Endpoint | Min role | Notes |
|---|---|---|
| `GET /admin/reports?status=open\|closed&cursor=` | MODERATOR | open queue: urgent first, then oldest; `openForTarget` counts duplicates |
| `POST /admin/reports/{id}/assign` | MODERATOR | -> `UNDER_REVIEW` |
| `POST /admin/reports/{id}/resolve` | MODERATOR | `{action: dismiss\|warn\|remove\|restrict\|suspend\|ban, reason, notes?, days?}`; closes every open report on the same target |
| `POST /admin/actions` | MODERATOR | same body + `{targetType, targetId}` without a report |
| `POST /admin/videos/{id}/restore`, `POST /admin/users/{id}/restore` | MODERATOR | `{reason}`; lifting a ban needs ADMIN |
| `GET /admin/users?q=&status=&cursor=` | ADMIN | username prefix search |
| `GET /admin/audit?action=&actor=&cursor=` | ADMIN | `action` is a prefix |
| `GET/POST /admin/blocked-hashes`, `DELETE /admin/blocked-hashes/{sha256}` | ADMIN | adding also removes existing videos with that hash |
| `PUT /admin/users/{id}/role` | SUPER_ADMIN | `user\|moderator\|admin`; SUPER_ADMIN is only set in the database (`security/scripts/promote.sh`) |

Valid verbs per target: video `warn/remove/restrict`, comment `warn/remove`, user `warn/suspend/ban`. Suspensions last 1-365 days (default 7) and are lifted automatically (janitor, every minute). Suspending or banning revokes all sessions. The affected owner gets a `system` notification with the reason.
**Auto-restrict**: a video with urgent reports (underage / non-consensual / illegal) from 3 different verified accounts older than 3 days is hidden (`RESTRICTED`) pending review and logged as `moderation.auto_restrict`.
