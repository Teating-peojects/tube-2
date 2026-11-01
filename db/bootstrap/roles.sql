-- Run ONCE per cluster as a superuser, before the first migration:
--   psql "$SUPERUSER_URL" -v dbname=redxtube -f db/bootstrap/roles.sql
-- Roles are created WITHOUT passwords. Set them from your secret manager, never from this repo:
--   ALTER ROLE redxtube_app PASSWORD '<from secret manager>';
-- Use scram-sha-256 (password_encryption = 'scram-sha-256') and TLS (sslmode=verify-full) for every connection.

DO $$
DECLARE r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['redxtube_migrator', 'redxtube_app', 'redxtube_worker', 'redxtube_maint', 'redxtube_readonly'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
      EXECUTE format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT', r);
    END IF;
  END LOOP;
END $$;

-- The database + schema are owned by the migrator (DDL only runs from CI/deploy, never from the API).
ALTER DATABASE :"dbname" OWNER TO redxtube_migrator;
ALTER DATABASE :"dbname" SET timezone TO 'UTC';
GRANT CONNECT ON DATABASE :"dbname" TO redxtube_app, redxtube_worker, redxtube_maint, redxtube_readonly;

\c :"dbname"
ALTER SCHEMA public OWNER TO redxtube_migrator;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO redxtube_app, redxtube_worker, redxtube_maint, redxtube_readonly;

-- Every table the migrator creates from now on is automatically usable by the right roles.
ALTER DEFAULT PRIVILEGES FOR ROLE redxtube_migrator IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO redxtube_app, redxtube_worker;
ALTER DEFAULT PRIVILEGES FOR ROLE redxtube_migrator IN SCHEMA public
  GRANT SELECT ON TABLES TO redxtube_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE redxtube_migrator IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO redxtube_app, redxtube_worker;

-- Guard rails against runaway queries and leaked transactions.
ALTER ROLE redxtube_app    SET statement_timeout = '15s';
ALTER ROLE redxtube_app    SET lock_timeout = '5s';
ALTER ROLE redxtube_app    SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE redxtube_worker SET statement_timeout = '10min';
ALTER ROLE redxtube_worker SET idle_in_transaction_session_timeout = '2min';
ALTER ROLE redxtube_readonly SET statement_timeout = '60s';
ALTER ROLE redxtube_readonly SET default_transaction_read_only = on;
ALTER ROLE redxtube_maint  SET statement_timeout = '5min';
