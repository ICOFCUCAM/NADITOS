-- Provision the test app role used by integration tests.
-- Run as a superuser (postgres) once per Postgres cluster; idempotent.
--
--   psql -U postgres -d naditos -f scripts/provision_test_role.sql
--
-- After this:
--   naditos     — owner of the schema; BYPASSRLS for migrations + fixtures
--   naditos_app — login-less role tests SET ROLE into; NOBYPASSRLS so
--                 RLS is exercised exactly as in production.

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='naditos_app') THEN
    CREATE ROLE naditos_app NOLOGIN NOBYPASSRLS NOSUPERUSER;
  END IF;
END $$;

-- Grant naditos_app membership to naditos BEFORE downgrading naditos —
-- once naditos loses superuser, only role admins can grant naditos_app.
GRANT naditos_app TO naditos;
ALTER ROLE naditos NOSUPERUSER BYPASSRLS;

GRANT USAGE ON SCHEMA public TO naditos_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO naditos_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO naditos_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO naditos_app;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO naditos_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO naditos_app;
