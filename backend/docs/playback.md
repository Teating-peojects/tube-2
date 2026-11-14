# Playback (B7): signed HLS

Videos are never served from a public bucket. A player gets a **signed, expiring URL** and the API
streams the objects from private storage after checking the video may still be served.

```
GET /api/v1/videos/{id}            (age gate + visibility rules)
  -> streamUrl = <STREAM_BASE_URL>/<exp>.<sig>/hls/<videoId>/master.m3u8

GET /stream/<exp>.<sig>/hls/<videoId>/master.m3u8
GET /stream/<exp>.<sig>/hls/<videoId>/720p/index.m3u8
GET /stream/<exp>.<sig>/hls/<videoId>/720p/seg_00012.ts
```

The token is the first path segment, so the relative URLs inside a playlist inherit it and no
playlist is rewritten. `sig` is a 128-bit HMAC-SHA256 over the video id and the expiry, so a token for
one video opens nothing else. Tokens last 6 hours (`playback.DefaultTTL`).

## What the stream handler checks, per request

1. The token verifies for the **video in the requested key**, and it has not expired.
2. The key is a servable HLS object (master, rendition playlist or numbered segment). Originals,
   thumbnails and arbitrary keys are unreachable by construction, whatever the token.
3. The video is `READY`/`PUBLISHED`, not `REMOVED` by moderation and not deleted. The answer is
   cached for 15 s (`WithGateTTL`), so a **takedown stops playback within seconds**, not at token
   expiry, without a database query per segment.
4. A database failure fails closed (502).

Responses are `Cache-Control: private` (never stored by shared caches), `nosniff`, and the request
path is logged with the token redacted (`/stream/<token>/...`). Session lookup is skipped for
`/stream/` because the URL is the credential.

## Configuration

| Variable | Meaning |
|---|---|
| `STREAM_BASE_URL` | public base players use, e.g. `https://api.redxtube.com/stream`. **Required in production when `STORAGE_*` is set.** Empty in development = plain `CDN_URL` URLs. |
| `PLAYBACK_SIGNING_KEY` | optional, >= 32 chars. Default: derived from `AUTH_SECRET`. |

CORS for the web origin is the normal `CORS_ALLOWED_ORIGINS` (hls.js fetches playlists and segments
with XHR).

## Known limits

* A leaked URL works until it expires (6 h) or the video is removed. Shorten `DefaultTTL` for stricter
  environments; players re-request `GET /videos/{id}` to get a fresh URL.
* Making a video `PRIVATE` after a token was issued does not revoke that token (moderation removal does).
* Segments flow through the API process. At scale put the same HMAC check at the CDN edge (the token
  format is verifiable without a database) and keep this handler as the origin, or run several API
  replicas behind the CDN.
