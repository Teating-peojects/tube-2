# REDXTUBE Web (Phase 1)

Next.js (App Router) + TypeScript + Tailwind v4 + TanStack Query + Zustand.

## Run
    cp .env.example .env.local
    npm install
    npm run dev        # http://localhost:3000

## Build
    npm run typecheck && npm run build && npm start

## Connect the Go backend
Set `NEXT_PUBLIC_USE_MOCK=false` and `NEXT_PUBLIC_API_URL=<your API origin>`.
Adjust routes in `src/config/endpoints.ts`; response types live in `src/types`.
Auth should use httpOnly cookies issued by the backend (CORS: allow origin + credentials).

## Authentication (Phase 4)
- Pages: `/login`, `/register`, `/forgot-password`, `/reset-password?token=`, `/verify`; protected pages live in `src/app/(main)/(protected)/`.
- Session = httpOnly cookie issued by the backend. The frontend never stores tokens; the current user is cached in TanStack Query (`["me"]`).
- Protection layers: middleware (optimistic, needs `SESSION_COOKIE_NAME` to be visible to this origin, e.g. API on a sibling subdomain with `Domain=.redxtube.com`), `AuthGuard` (client), and the backend (authoritative).
- Backend notes: use `SameSite=Lax`/`Strict` + `Secure`; CORS allow-list with credentials; add CSRF protection if cross-site requests are possible; return the same response for unknown emails on forgot-password; rate-limit with `429` + `Retry-After`.
- Error contract: JSON `{ "code": "invalid_credentials" | "verification_required" | "invalid_code" | "invalid_token" | "conflict" | "rate_limited" }`.
- Mock mode demo: `demo@redxtube.test` (or `demo`) / `Demo-12345`; verification code `123456`; any reset token of 8+ characters. Mock accounts live in memory and reset on reload.

## Library, playlists, channels, notifications (Phase 5)
- Routes: `/channel/[id]` (+ `/videos`, `/shorts`, `/live`, `/playlists`, `/about`), `/playlists`, `/playlists/[id]` (public), `/history`, `/watch-later`, `/liked`, `/subscriptions`, `/notifications`.
- Server-rendered pages forward the visitor's cookies to the API via `src/lib/server-fetch.ts`, so private playlists, reactions and subscription state are correct on first paint.
- All list pages use cursor pagination (`{ items, nextCursor }`) through `features/library/PagedList.tsx`.
- Backend routes are listed in `src/config/endpoints.ts`. Playlist reorder is `PUT /playlists/:id/order { videoIds }`.
- Unread notification count polls every 60s; swap `useUnreadCount` for a realtime subscription later.
- Mock data: `demo` account has Watch Later (v2, v5), a history list and two playlists. Subscribe to a creator on any watch page to see Subscriptions fill up.

## Upload & Studio (Phase 6)
**Upload flow** (`/upload`): `POST /upload/init` returns `{ uploadId, videoId, partSize, parts: [{ partNumber, url }] }` (one presigned PUT URL per part, e.g. S3/R2 multipart). The browser uploads parts directly to object storage (3 in parallel, retry with backoff, resume by skipping finished parts), then calls `POST /upload/:id/complete { parts: [{ partNumber, etag }] }`. The backend (FFmpeg) moves the video through `processing → encoding → ready`; the UI follows via `GET /upload/:id/status` (polled every 3s) and, if configured, WebSocket topic `video:<videoId>`. Publishing calls `POST /videos/:id/publish`; "Save draft" calls `PATCH /videos/:id/metadata`.
- Bucket CORS must allow `PUT` from your site and **expose the `ETag` header** (needed to complete multipart uploads).
- Thumbnails/captions use `POST /upload/:id/assets { kind, contentType, size, lang? } -> { assetId, url }`, then a `PUT` to that URL.
- Expired presigned URLs (403) make the next "Retry" start a fresh session. Client-side file checks are UX only; validate type/size/content on the server.
- Uploader publishes require an explicit "everyone in the video is 18+ and consented" confirmation; enforce and log this server-side too.

**Realtime** (`src/lib/realtime.ts`): set `NEXT_PUBLIC_WS_URL` to enable. One shared socket, connects only while something is subscribed, reconnects with exponential backoff, re-subscribes automatically. Protocol: client `{ action: "subscribe" | "unsubscribe", topic }`, server `{ topic, data }`. Topics used: `notifications`, `video:<id>`. Without a URL, polling covers everything.

**Studio** (`/studio`, `/studio/videos`, `/studio/analytics`, `/studio/comments`): see `src/config/endpoints.ts` (`studio.*`). Revenue is shown only when the API returns a non-null value.

**Mock mode:** uploading simulates ~40 MB/s per part, then ~4s processing and ~5s encoding before "Ready". A published mock video appears in Studio but has no watch page.

## Safety, settings, legal, SEO, tests (Phase 7)
- **Toasts** (`store/toast.ts`, `ToastViewport`): `toast.success/error/info(...)` from anywhere; max 3, auto-dismiss, announced to screen readers.
- **Modals** (`components/ui/Modal.tsx` on native `<dialog>`, `store/modal.ts`, `features/safety/ModalHost.tsx`): `modals.confirm(...)` (returns a Promise<boolean>), `modals.share(...)`, `modals.report(...)`, and the "Log in required" prompt via `useRequireAuth()`. Escape, outside click, focus trap/restore, bottom sheet on phones.
- **Reporting**: video menu, comment button and `/report?type=&id=` share one `ReportForm`. Reasons come from `GET /reports/reasons` (built-in fallback). Handles submitted / already reported (409) / rate limited (429) / error.
- **Settings**: `/settings/{account,privacy,security,notifications,playback,appearance}`. Server-backed toggles are optimistic with rollback; playback/appearance are stored on the device.
- **Shorts** `/shorts`, **Live** `/live`.
- **Legal pages** (`/terms`, `/privacy`, `/community-guidelines`, `/dmca`) are drafts. Set `NEXT_PUBLIC_LEGAL_NAME`, `NEXT_PUBLIC_SUPPORT_EMAIL`, `NEXT_PUBLIC_DMCA_EMAIL`, `NEXT_PUBLIC_LEGAL_UPDATED` and have counsel review them.
- **SEO**: `app/sitemap.ts` (static pages + `GET /sitemap/videos`), `app/robots.ts`, per-page metadata, JSON-LD on watch pages, RTA adult-content label meta tag. Note: the age gate redirects every visitor without the cookie, **including search-engine crawlers**; decide your indexing policy before launch.
- **Tests**: `npm test` (Vitest + Testing Library: utils, redirect safety, search params, chunked uploader, schemas, stores, Modal, Toggle, TagInput, UploadProgress). `npm run e2e` (Playwright on Chromium, Firefox, WebKit, Pixel 7, iPhone 14, with the mock API): age gate, browse/search, auth, watch, upload, library, overflow at 10 widths, axe checks. First run: `npx playwright install`.
