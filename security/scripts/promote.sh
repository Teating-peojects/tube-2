#!/usr/bin/env bash
# Set an account's role. This is the only way to create staff accounts: there is
# deliberately no API for it, and clients can never choose their own role.
#   DATABASE_URL=postgres://<owner or maintenance role>@host/redxtube security/scripts/promote.sh user@example.com ADMIN
# Roles: USER | CREATOR | MODERATOR | ADMIN | SUPER_ADMIN   (CREATOR also sets is_creator, as the schema requires)
set -euo pipefail
: "${DATABASE_URL:?DATABASE_URL is required}"
email="${1:?usage: promote.sh <email> <role>}"
role="${2:?usage: promote.sh <email> <role>}"
case "$role" in USER|CREATOR|MODERATOR|ADMIN|SUPER_ADMIN) ;; *) echo "invalid role: $role" >&2; exit 2 ;; esac

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -v email="$email" -v role="$role" <<'SQL'
BEGIN;
WITH target AS (
  SELECT id, role AS old_role FROM users WHERE email = :'email' AND deleted_at IS NULL
), upd AS (
  UPDATE users u SET role = :'role'::user_role, is_creator = u.is_creator OR :'role' = 'CREATOR'
  FROM target t WHERE u.id = t.id RETURNING u.id, t.old_role
)
INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, metadata)
SELECT NULL, 'admin.role_changed', 'user', id::text, jsonb_build_object('from', old_role, 'to', :'role', 'via', 'security/scripts/promote.sh') FROM upd;

SELECT CASE WHEN count(*) = 0 THEN 'no such account' ELSE 'ok: ' || max(email::text) || ' is now ' || max(role::text) END
FROM users WHERE email = :'email' AND deleted_at IS NULL;
COMMIT;
SQL
