-- 000008 privileges: least-privilege grants. Roles themselves are created once by db/bootstrap/roles.sql
-- (superuser, passwords come from your secret manager). Each statement is skipped if the role is absent,
-- so migrations also run in CI / local dev without the bootstrap step.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_app') THEN
    -- Audit and moderation history is append-only for the API and workers.
    REVOKE UPDATE, DELETE, TRUNCATE ON audit_logs, moderation_actions FROM redxtube_app;
  END IF;

  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_worker') THEN
    REVOKE UPDATE, DELETE, TRUNCATE ON audit_logs, moderation_actions FROM redxtube_worker;
    -- Media/analytics workers never need credentials, sessions or identity data.
    REVOKE ALL ON sessions, verification_tokens, age_verifications, user_settings FROM redxtube_worker;
    REVOKE ALL ON users FROM redxtube_worker;
    GRANT SELECT (id, username, display_name, is_creator, status, role) ON users TO redxtube_worker;
  END IF;

  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_readonly') THEN
    -- Analytics / BI read replicas must not see secrets.
    REVOKE ALL ON sessions, verification_tokens, age_verifications FROM redxtube_readonly;
    REVOKE ALL ON users FROM redxtube_readonly;
    GRANT SELECT (id, username, display_name, role, status, is_verified, is_creator, created_at, deleted_at) ON users TO redxtube_readonly;
  END IF;
END $$;
