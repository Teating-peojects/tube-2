# Security

Security design and operational tooling for REDXTUBE.

## Documents
- [auth.md](auth.md) — sessions, Argon2id passwords, age gate, CSRF/origin checks, rate limits, role model.

The Go implementation lives in the backend module:
- `backend/internal/auth` — password hashing, OTP/HMAC, signed age cookie, roles, client IP
- `backend/internal/middleware` — session auth, role guards, age gate, CSRF origin check
- `backend/internal/ratelimit` — Redis atomic counters
- `backend/internal/httpx` — security headers, body limits, request IDs/logging

## Tooling
- [scripts/promote.sh](scripts/promote.sh) — the only supported way to grant MODERATOR / ADMIN /
  SUPER_ADMIN. Every change is written to `audit_logs`.

```bash
DATABASE_URL=postgres://redxtube_migrator:...@localhost:5432/redxtube \
  security/scripts/promote.sh user@example.com ADMIN
```

## Upload and moderation safeguards
- Uploads go straight to a **private** bucket (`uploads/`); the API verifies parts, size and file signature before the media worker sees the file. Thumbnails and captions are re-checked (size, image signature, caption parsing) at publish time.
- Publishing needs an explicit consent declaration (everyone 18+, consented, rights held), stored append-only with the hashed IP (`upload_consents`).
- `blocked_hashes`: a database trigger removes any video whose original file hash is blocklisted, whoever writes the hash.
- Moderation actions, report resolutions and role changes are transactional with their append-only records; staff cannot act on equal/higher roles or themselves.
- Mass-report abuse: auto-restrict counts only verified accounts older than 3 days, needs 3 distinct reporters, and is reversible.
- WebSocket: session cookie + `Origin` allow-list, per-user caps, `video:<id>` topics owner-only, events carry no content.

## Rules
- Never run migrations as a superuser; the API runs as the least-privilege `redxtube_app` role.
- Secrets live in `backend/.env` or a secret manager — never in git or images.
- Staff accounts are never created through the API; clients cannot choose their own role.
- The frontend gate is UX only; the backend enforces age and authorization on every request.
