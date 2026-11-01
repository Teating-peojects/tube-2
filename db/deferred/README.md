# Deferred designs (NOT part of the active migration chain)

These files are intentionally outside `db/migrations/`. Move one into `db/migrations/` (with the next
version number and a matching `.down.sql`) only when the feature is actually built.

- `monetization.sql.disabled` — paid creator subscriptions, transactions, payouts. Designed so it only
  *adds* tables that reference `users`; nothing in the core schema has to change. Never store card data:
  keep provider references only (PCI scope stays with the payment provider).
- `live_streams.sql.disabled` — live streaming. The frontend already has a Live page; until this exists the
  `GET /live` endpoint should return an empty page.
