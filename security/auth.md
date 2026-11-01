# Authentication, sessions and the 18+ gate (B3)

## Model
- **Session cookie** `xh_session`: 32 random bytes (base64url), `HttpOnly; Secure; SameSite=Lax; Path=/; Domain=$COOKIE_DOMAIN`. `sessions.session_token_hash` holds only its SHA-256 (plus a /24 or /56 IP prefix), so a database leak cannot be replayed as sessions. Sliding 30-day expiry (re-issued at most once an hour) with a hard 90-day cap measured from `created_at`. Every login mints a new token (no fixation); logout, logout-all, password change/reset and deletion revoke server-side.
- **Passwords**: Argon2id (64 MiB, t=3, p=2, random 16-byte salt, PHC string). At most 8 hashes run at once so a login flood cannot exhaust RAM. Unknown accounts burn an equal hash so timing does not reveal which emails exist. Weaker stored hashes are upgraded on login. Rules match the frontend (10-128 chars, a letter and a number) plus a common-password list and "not your username/email".
- **Email verification**: 6-digit code, 10 minutes, 5 attempts. Stored in `verification_tokens` as `HMAC-SHA256(server key, purpose|userId|code)` (a plain hash of 6 digits would be brute-forced offline); `attempts`/`max_attempts` live in the row. Attempts are counted under a row lock; the 5th wrong guess locks the code (even the right one) until the user requests a new one. Resend: 1 per minute, 5 per hour.
- **Password reset**: link token `<token row id>.<32 random bytes>`, 30 minutes, single use; the row stores `HMAC(purpose|user|secret)`. A new request replaces the old link. Success signs out every device, marks the email verified and mails a "password changed" notice. `forgot-password` always answers 204 (no account enumeration); one address is mailed at most 3 times per hour.
- **Roles**: `USER < CREATOR < MODERATOR < ADMIN < SUPER_ADMIN`, read from the database on every request (promotion/demotion is immediate). Nothing from the client (header, cookie, body field) can change it; extra JSON fields are rejected. Staff are created only with `security/scripts/promote.sh`.
- **Deleting an account** (`POST /account/delete` + password): soft delete that erases personal data, frees the email/username, hides the creator's videos everywhere, and keeps audit/age records.

## 18+ age gate (server-enforced)
1. `POST /api/v1/age/verify {"confirmed": true}` sets (nothing is stored for anonymous visitors: `age_verifications` is per account):
   - `xh_age`: **signed** (HMAC-SHA256) `v1.<subject>.<expiry>.<mac>`, HttpOnly, 30 days. This is the only thing the API trusts.
   - `xh_age_ok=1`: plain marker so the Next.js middleware can skip a redirect. UI convenience only.
2. Every content endpoint (feeds, detail, search, trending, channels, shorts, live) returns `403 age_verification_required` without a valid `xh_age`. Verification is a signature + expiry check (no DB call). A hand-made `xh_age_ok=1` opens nothing.
3. Registration requires a valid `xh_age`, and the account gets its own `age_verifications` row (VERIFIED, SELF_DECLARATION). Logging in on a new device restores the cookie for accounts that already verified.
4. Always open: `/reports/reasons`, `/sitemap/videos` (ids only), `/age/*`, `/auth/*`.

## Abuse protection (Redis, atomic Lua counters, fails closed)
| Action | Limit | Key |
|---|---|---|
| login failures | 5 / 15 min per account, 30 / 15 min per IP; cleared by a successful login | hashed identifier, IP |
| register | 10 / hour | IP |
| verify code | 5 attempts per code, 30 / 10 min | code row, IP |
| resend code | 1 / min, 5 / hour | user |
| forgot password | 10 / hour per IP, 3 mails / hour per address (silent) | IP, hashed email |
| reset password | 20 / hour | IP |
| change password / delete account (password guesses) | 5 failures / 15 min | user |
| age verify | 30 / hour | IP |

429 responses carry `Retry-After` (whole seconds). If Redis is down these endpoints answer `503 service_unavailable` instead of running unprotected. No email or username is ever stored in Redis (hashed keys).
`TRUST_PROXY_HEADERS=true` makes the API read the client IP from `CF-Connecting-IP` / the right-most `X-Forwarded-For`. Enable it only when the API is reachable exclusively through your proxy/CDN, otherwise clients can spoof it and dodge limits.

## CSRF and CORS
`SameSite=Lax` cookies + strict CORS (exact origins, no wildcard) + an Origin check on every POST/PUT/PATCH/DELETE (`403 csrf_blocked` for foreign origins, or `Sec-Fetch-Site: cross-site` without an Origin). Requests with no Origin (curl, mobile apps) are allowed.

## Email
`MAIL_DRIVER=resend` (production) posts to Resend's HTTP API with an idempotency key, from an in-process queue with retries (1s, 4s, 15s; permanent errors such as a bad key are not retried) and a graceful drain on shutdown. `MAIL_DRIVER=log` prints emails, **including codes and reset links**, to the log: development only, and the config refuses it in production. `none` drops mail. Logs never contain the recipient address or body in production paths, passwords, tokens or session ids (covered by a test).
`MAIL_FROM` must be a sender on a domain you verified in Resend. In-process mail is lost if the process dies mid-send; users can always request another code. B6 moves email onto the Redis job queue.

## Error codes (match the frontend)
`invalid_credentials` 401, `unauthenticated` 401, `verification_required` 403, `account_suspended` 403, `age_verification_required` 403, `csrf_blocked` 403, `invalid_code` 400, `invalid_token` 400, `invalid_json` 400, `conflict` 409, `validation_failed` 422 (+`fields`), `rate_limited` 429 (+`Retry-After`), `too_large` 413, `service_unavailable` 503. Changing/deleting with a wrong current password answers `403 invalid_credentials` (not 401, so the frontend does not treat it as "logged out").

## Frontend changes needed when you connect (not done now)
1. Age gate "I am 18+" must call `POST /api/v1/age/verify` (credentials: include) instead of setting `xh_age_ok` itself.
2. On `403 age_verification_required` redirect to `/age-verification`.
3. Set the API and the site on the same registrable domain and `COOKIE_DOMAIN=.redxtube.com` (or proxy the API under the site's own origin) so cookies are first-party.
4. `/auth/logout-all` and `/auth/change-password` already match `services/auth.ts`.

## Known limits / next
- Registering reveals whether an email/username exists (`409 conflict`), as the frontend expects; it is rate limited per IP. Forgot-password is constant-answer but not constant-time.
- No phone verification or TOTP yet. Age verification is self-declaration (`SELF_DECLARATION`); the schema already supports `DATE_OF_BIRTH` / `THIRD_PARTY` providers.
- Session lookups hit PostgreSQL (indexed); a Redis session cache is a B13 optimisation.
- Existing comments by a deleted user are handled in B4 (author shown as "Deleted user").
