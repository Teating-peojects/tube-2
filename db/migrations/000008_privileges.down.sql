-- Restore the default (broad) grants; roles are managed by db/bootstrap/roles.sql.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_app') THEN
    GRANT UPDATE, DELETE ON audit_logs, moderation_actions TO redxtube_app;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'redxtube_worker') THEN
    GRANT UPDATE, DELETE ON audit_logs, moderation_actions TO redxtube_worker;
  END IF;
END $$;
