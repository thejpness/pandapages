\set ON_ERROR_STOP on

-- This script changes only roles, ownership, and privileges. Passwords and
-- application data are deliberately outside its scope.

SELECT current_setting('server_version_num')::integer >= 180000 AS supported_version
\gset
\if :supported_version
\else
  \warn 'PostgreSQL 18 or newer is required'
  \quit 3
\endif

SELECT current_database() = :'database_name' AS connected_to_target
\gset
\if :connected_to_target
\else
  \warn 'Connected database does not match database_name'
  \quit 3
\endif

SELECT rolsuper AS administrative_session
FROM pg_roles
WHERE rolname = current_user
\gset
\if :administrative_session
\else
  \warn 'Role application requires an explicit PostgreSQL administrative session'
  \quit 3
\endif

SELECT count(*) = 0 AS dedicated_cluster
FROM pg_database
WHERE datname NOT IN (:'database_name', 'postgres', 'template0', 'template1')
\gset
\if :dedicated_cluster
\else
  \warn 'Unexpected database found; refusing to change cluster-wide CONNECT privileges'
  \quit 3
\endif

BEGIN;
SET LOCAL client_min_messages = warning;

SELECT pg_advisory_xact_lock(hashtextextended('pandapages-postgresql-role-policy', 0));

SELECT format(
  'CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
  :'owner_role'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'owner_role')
\gexec

SELECT format(
  'CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2',
  :'migration_role'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migration_role')
\gexec

SELECT format(
  'CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 20',
  :'application_role'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'application_role')
\gexec

SELECT format(
  'CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2',
  :'backup_role'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'backup_role')
\gexec

SELECT format(
  'ALTER ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT -1',
  :'owner_role'
)
\gexec
SELECT format(
  'ALTER ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2',
  :'migration_role'
)
\gexec
SELECT format(
  'ALTER ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 20',
  :'application_role'
)
\gexec
SELECT format(
  'ALTER ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 2',
  :'backup_role'
)
\gexec

-- Dedicated runtime and backup logins have no role memberships. The migrator
-- has one non-inherited, non-admin membership and must explicitly SET ROLE.
SELECT format('REVOKE %I FROM %I', granted.rolname, member.rolname)
FROM pg_auth_members membership
JOIN pg_roles granted ON granted.oid = membership.roleid
JOIN pg_roles member ON member.oid = membership.member
WHERE member.rolname IN (:'migration_role', :'application_role', :'backup_role')
  AND NOT (
    member.rolname = :'migration_role'
    AND granted.rolname = :'owner_role'
  )
\gexec

SELECT format(
  'GRANT %I TO %I WITH ADMIN FALSE, INHERIT FALSE, SET TRUE',
  :'owner_role', :'migration_role'
)
\gexec

SELECT format('ALTER DATABASE %I OWNER TO %I', :'database_name', :'owner_role')
\gexec
SELECT format('ALTER SCHEMA public OWNER TO %I', :'owner_role')
\gexec

-- A role can otherwise inherit CONNECT through PUBLIC. This deployment is a
-- dedicated cluster, so remove PUBLIC CONNECT/TEMP from every connectable
-- database after the preflight above proves no unrelated database is present.
SELECT format('REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC', datname)
FROM pg_database
WHERE datallowconn
\gexec

SELECT format(
  'GRANT CONNECT ON DATABASE %I TO %I, %I, %I',
  :'database_name', :'migration_role', :'application_role', :'backup_role'
)
\gexec

SELECT format(
  'ALTER ROLE %I IN DATABASE %I SET role = %L',
  :'migration_role', :'database_name', :'owner_role'
)
\gexec
SELECT format(
  'ALTER ROLE %I IN DATABASE %I SET search_path = public, pg_catalog',
  :'migration_role', :'database_name'
)
\gexec
SELECT format(
  'ALTER ROLE %I IN DATABASE %I RESET role',
  :'application_role', :'database_name'
)
\gexec
SELECT format(
  'ALTER ROLE %I IN DATABASE %I SET search_path = pg_catalog, public',
  :'application_role', :'database_name'
)
\gexec
SELECT format(
  'ALTER ROLE %I IN DATABASE %I RESET role',
  :'backup_role', :'database_name'
)
\gexec
SELECT format(
  'ALTER ROLE %I IN DATABASE %I SET search_path = pg_catalog, public',
  :'backup_role', :'database_name'
)
\gexec
SELECT format(
  'ALTER ROLE %I IN DATABASE %I SET default_transaction_read_only = on',
  :'backup_role', :'database_name'
)
\gexec

-- Transfer existing non-extension application objects. Trusted extension
-- internals remain owned according to PostgreSQL's extension contract.
SELECT format(
  'ALTER %s %I.%I OWNER TO %I',
  CASE class.relkind
    WHEN 'r' THEN 'TABLE'
    WHEN 'p' THEN 'TABLE'
    WHEN 'v' THEN 'VIEW'
    WHEN 'm' THEN 'MATERIALIZED VIEW'
    WHEN 'S' THEN 'SEQUENCE'
    WHEN 'f' THEN 'FOREIGN TABLE'
  END,
  namespace.nspname,
  class.relname,
  :'owner_role'
)
FROM pg_class class
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = 'public'
  AND class.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
  AND NOT (
    class.relkind = 'S'
    AND EXISTS (
      SELECT 1
      FROM pg_depend ownership_dependency
      WHERE ownership_dependency.classid = 'pg_class'::regclass
        AND ownership_dependency.objid = class.oid
        AND ownership_dependency.refclassid = 'pg_class'::regclass
        AND ownership_dependency.deptype IN ('a', 'i')
    )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM pg_depend dependency
    WHERE dependency.classid = 'pg_class'::regclass
      AND dependency.objid = class.oid
      AND dependency.deptype = 'e'
  )
\gexec

SELECT format(
  'ALTER %s %I.%I(%s) OWNER TO %I',
  CASE routine.prokind
    WHEN 'p' THEN 'PROCEDURE'
    WHEN 'a' THEN 'AGGREGATE'
    ELSE 'FUNCTION'
  END,
  namespace.nspname,
  routine.proname,
  pg_get_function_identity_arguments(routine.oid),
  :'owner_role'
)
FROM pg_proc routine
JOIN pg_namespace namespace ON namespace.oid = routine.pronamespace
WHERE namespace.nspname = 'public'
  AND NOT EXISTS (
    SELECT 1
    FROM pg_depend dependency
    WHERE dependency.classid = 'pg_proc'::regclass
      AND dependency.objid = routine.oid
      AND dependency.deptype = 'e'
  )
\gexec

SELECT format('ALTER TYPE %I.%I OWNER TO %I', namespace.nspname, type.typname, :'owner_role')
FROM pg_type type
JOIN pg_namespace namespace ON namespace.oid = type.typnamespace
WHERE namespace.nspname = 'public'
  AND type.typtype IN ('d', 'e')
  AND NOT EXISTS (
    SELECT 1
    FROM pg_depend dependency
    WHERE dependency.classid = 'pg_type'::regclass
      AND dependency.objid = type.oid
      AND dependency.deptype = 'e'
  )
\gexec

SELECT format(
  'REVOKE ALL PRIVILEGES ON SCHEMA public FROM PUBLIC, %I, %I, %I',
  :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format('GRANT USAGE ON SCHEMA public TO %I, %I', :'application_role', :'backup_role')
\gexec

SELECT format(
  'REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM PUBLIC, %I, %I, %I',
  :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format(
  'REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM PUBLIC, %I, %I, %I',
  :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format(
  'REVOKE ALL PRIVILEGES ON ALL ROUTINES IN SCHEMA public FROM PUBLIC, %I, %I, %I',
  :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format('GRANT EXECUTE ON ALL ROUTINES IN SCHEMA public TO %I', :'owner_role')
\gexec
SELECT format(
  'GRANT EXECUTE ON FUNCTION public.gen_random_uuid() TO %I',
  :'application_role'
)
WHERE to_regprocedure('public.gen_random_uuid()') IS NOT NULL
\gexec

SELECT format(
  'REVOKE ALL PRIVILEGES ON TYPE %I.%I FROM PUBLIC, %I, %I, %I',
  namespace.nspname,
  type.typname,
  :'migration_role', :'application_role', :'backup_role'
)
FROM pg_type type
JOIN pg_namespace namespace ON namespace.oid = type.typnamespace
WHERE namespace.nspname = 'public'
  AND type.typtype IN ('d', 'e')
  AND NOT EXISTS (
    SELECT 1
    FROM pg_depend dependency
    WHERE dependency.classid = 'pg_type'::regclass
      AND dependency.objid = type.oid
      AND dependency.deptype = 'e'
  )
\gexec

-- Runtime access is deliberately tied to the tables used by current Go SQL.
WITH runtime_table(name) AS (
  VALUES
    ('accounts'),
    ('child_profiles'),
    ('contributors'),
    ('profile_settings'),
    ('profiles'),
    ('prompt_profiles'),
    ('reading_progress'),
    ('stories'),
    ('story_contributors'),
    ('story_sections'),
    ('story_segments'),
    ('story_versions')
)
SELECT format(
  'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.%I TO %I',
  class.relname,
  :'application_role'
)
FROM runtime_table
JOIN pg_class class ON class.relname = runtime_table.name
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = 'public'
  AND class.relkind IN ('r', 'p')
\gexec

SELECT format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', :'backup_role')
\gexec
SELECT format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %I', :'application_role')
\gexec
SELECT format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO %I', :'backup_role')
\gexec

-- Goose's bookkeeping sequence is not an application identifier source.
SELECT format(
  'REVOKE ALL PRIVILEGES ON SEQUENCE public.goose_db_version_id_seq FROM %I',
  :'application_role'
)
WHERE to_regclass('public.goose_db_version_id_seq') IS NOT NULL
\gexec

-- Defaults are attached to the effective object-creating role. Goose logs in
-- as the migrator but SET ROLE makes current_user the owner before DDL runs.
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL PRIVILEGES ON TABLES FROM PUBLIC, %I, %I, %I',
  :'owner_role', :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %I',
  :'owner_role', :'application_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO %I',
  :'owner_role', :'backup_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL PRIVILEGES ON SEQUENCES FROM PUBLIC, %I, %I, %I',
  :'owner_role', :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %I',
  :'owner_role', :'application_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON SEQUENCES TO %I',
  :'owner_role', :'backup_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON ROUTINES FROM PUBLIC, %I, %I, %I',
  :'owner_role', :'migration_role', :'application_role', :'backup_role'
)
\gexec
SELECT format(
  'ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON TYPES FROM PUBLIC, %I, %I, %I',
  :'owner_role', :'migration_role', :'application_role', :'backup_role'
)
\gexec

COMMIT;

SELECT 'result=applied' AS result;
