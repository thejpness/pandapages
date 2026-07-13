\set ON_ERROR_STOP on

BEGIN READ ONLY;

SELECT current_setting('server_version_num')::integer >= 180000 AS assertion
\gset
\if :assertion
\else
  \warn 'verification failed: PostgreSQL 18 or newer is required'
  \quit 3
\endif

SELECT current_database() = :'database_name' AS assertion
\gset
\if :assertion
\else
  \warn 'verification failed: connected database does not match database_name'
  \quit 3
\endif

WITH expected(role_name, can_login, connection_limit) AS (
  VALUES
    (:'owner_role', false, -1),
    (:'migration_role', true, 2),
    (:'application_role', true, 20),
    (:'backup_role', true, 2)
)
SELECT count(*) = 4 AND bool_and(
  role.rolcanlogin = expected.can_login
  AND role.rolconnlimit = expected.connection_limit
  AND NOT role.rolsuper
  AND NOT role.rolcreatedb
  AND NOT role.rolcreaterole
  AND NOT role.rolinherit
  AND NOT role.rolreplication
  AND NOT role.rolbypassrls
) AS assertion
FROM expected
JOIN pg_roles role ON role.rolname = expected.role_name
\gset
\if :assertion
\else
  \warn 'verification failed: role attributes do not match policy'
  \quit 3
\endif

SELECT
  count(*) = 1
  AND bool_and(
    member.rolname = :'migration_role'
    AND granted.rolname = :'owner_role'
    AND NOT membership.admin_option
    AND NOT membership.inherit_option
    AND membership.set_option
  ) AS assertion
FROM pg_auth_members membership
JOIN pg_roles member ON member.oid = membership.member
JOIN pg_roles granted ON granted.oid = membership.roleid
WHERE member.rolname IN (:'migration_role', :'application_role', :'backup_role')
\gset
\if :assertion
\else
  \warn 'verification failed: an unexpected role membership or escalation path exists'
  \quit 3
\endif

SELECT pg_get_userbyid(database.datdba) = :'owner_role' AS assertion
FROM pg_database database
WHERE database.datname = :'database_name'
\gset
\if :assertion
\else
  \warn 'verification failed: application database owner is incorrect'
  \quit 3
\endif

SELECT
  count(*) = 0
  AND NOT EXISTS (
    SELECT 1
    FROM pg_database target_database
    CROSS JOIN LATERAL aclexplode(COALESCE(target_database.datacl, acldefault('d', target_database.datdba))) privilege
    WHERE target_database.datname = :'database_name'
      AND privilege.grantee = 0
      AND privilege.privilege_type IN ('CONNECT', 'TEMPORARY')
  )
  AND has_database_privilege(:'migration_role', :'database_name', 'CONNECT')
  AND has_database_privilege(:'application_role', :'database_name', 'CONNECT')
  AND has_database_privilege(:'backup_role', :'database_name', 'CONNECT')
  AS assertion
FROM pg_database database
WHERE database.datallowconn
  AND database.datname <> :'database_name'
  AND (
    has_database_privilege(:'migration_role', database.oid, 'CONNECT')
    OR has_database_privilege(:'application_role', database.oid, 'CONNECT')
    OR has_database_privilege(:'backup_role', database.oid, 'CONNECT')
    OR EXISTS (
      SELECT 1
      FROM aclexplode(COALESCE(database.datacl, acldefault('d', database.datdba))) privilege
      WHERE privilege.grantee = 0
        AND privilege.privilege_type = 'CONNECT'
    )
  )
\gset
\if :assertion
\else
  \warn 'verification failed: database CONNECT/TEMP privileges are broader than policy'
  \quit 3
\endif

SELECT
  pg_get_userbyid(namespace.nspowner) = :'owner_role'
  AND has_schema_privilege(:'application_role', namespace.oid, 'USAGE')
  AND NOT has_schema_privilege(:'application_role', namespace.oid, 'CREATE')
  AND has_schema_privilege(:'backup_role', namespace.oid, 'USAGE')
  AND NOT has_schema_privilege(:'backup_role', namespace.oid, 'CREATE')
  AND NOT EXISTS (
    SELECT 1
    FROM aclexplode(COALESCE(namespace.nspacl, acldefault('n', namespace.nspowner))) privilege
    WHERE privilege.grantee = 0
      AND privilege.privilege_type IN ('USAGE', 'CREATE')
  )
  AS assertion
FROM pg_namespace namespace
WHERE namespace.nspname = 'public'
\gset
\if :assertion
\else
  \warn 'verification failed: public schema ownership or privileges are incorrect'
  \quit 3
\endif

SELECT count(*) = 0 AS assertion
FROM pg_class class
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = 'public'
  AND class.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
  AND NOT EXISTS (
    SELECT 1
    FROM pg_depend dependency
    WHERE dependency.classid = 'pg_class'::regclass
      AND dependency.objid = class.oid
      AND dependency.deptype = 'e'
  )
  AND pg_get_userbyid(class.relowner) <> :'owner_role'
\gset
\if :assertion
\else
  \warn 'verification failed: a non-extension application relation has the wrong owner'
  \quit 3
\endif

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
), checked AS (
  SELECT
    runtime_table.name,
    class.oid,
    has_table_privilege(:'application_role', class.oid, 'SELECT') AS can_select,
    has_table_privilege(:'application_role', class.oid, 'INSERT') AS can_insert,
    has_table_privilege(:'application_role', class.oid, 'UPDATE') AS can_update,
    has_table_privilege(:'application_role', class.oid, 'DELETE') AS can_delete,
    has_table_privilege(:'application_role', class.oid, 'TRUNCATE') AS can_truncate,
    has_table_privilege(:'application_role', class.oid, 'REFERENCES') AS can_reference,
    has_table_privilege(:'application_role', class.oid, 'TRIGGER') AS can_trigger,
    has_table_privilege(:'application_role', class.oid, 'MAINTAIN') AS can_maintain
  FROM runtime_table
  LEFT JOIN pg_class class ON class.relname = runtime_table.name
    AND class.relnamespace = 'public'::regnamespace
    AND class.relkind IN ('r', 'p')
)
SELECT count(*) = 12 AND bool_and(
  oid IS NOT NULL
  AND can_select
  AND can_insert
  AND can_update
  AND can_delete
  AND NOT can_truncate
  AND NOT can_reference
  AND NOT can_trigger
  AND NOT can_maintain
) AS assertion
FROM checked
\gset
\if :assertion
\else
  \warn 'verification failed: application table privileges are incomplete or excessive'
  \quit 3
\endif

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
SELECT count(*) = 0 AS assertion
FROM pg_class class
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = 'public'
  AND class.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND class.relname NOT IN (SELECT name FROM runtime_table)
  AND (
    has_table_privilege(:'application_role', class.oid, 'SELECT')
    OR has_table_privilege(:'application_role', class.oid, 'INSERT')
    OR has_table_privilege(:'application_role', class.oid, 'UPDATE')
    OR has_table_privilege(:'application_role', class.oid, 'DELETE')
    OR has_table_privilege(:'application_role', class.oid, 'TRUNCATE')
    OR has_table_privilege(:'application_role', class.oid, 'REFERENCES')
    OR has_table_privilege(:'application_role', class.oid, 'TRIGGER')
    OR has_table_privilege(:'application_role', class.oid, 'MAINTAIN')
  )
\gset
\if :assertion
\else
  \warn 'verification failed: application can access an unapproved table'
  \quit 3
\endif

SELECT count(*) = 0 AS assertion
FROM pg_class class
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = 'public'
  AND class.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND (
    NOT has_table_privilege(:'backup_role', class.oid, 'SELECT')
    OR has_table_privilege(:'backup_role', class.oid, 'INSERT')
    OR has_table_privilege(:'backup_role', class.oid, 'UPDATE')
    OR has_table_privilege(:'backup_role', class.oid, 'DELETE')
    OR has_table_privilege(:'backup_role', class.oid, 'TRUNCATE')
    OR has_table_privilege(:'backup_role', class.oid, 'REFERENCES')
    OR has_table_privilege(:'backup_role', class.oid, 'TRIGGER')
    OR has_table_privilege(:'backup_role', class.oid, 'MAINTAIN')
  )
\gset
\if :assertion
\else
  \warn 'verification failed: backup table privileges are incomplete or writable'
  \quit 3
\endif

SELECT
  to_regprocedure('public.gen_random_uuid()') IS NOT NULL
  AND has_function_privilege(:'application_role', 'public.gen_random_uuid()', 'EXECUTE')
  AND NOT has_function_privilege(:'backup_role', 'public.gen_random_uuid()', 'EXECUTE')
  AS assertion
\gset
\if :assertion
\else
  \warn 'verification failed: required runtime function privileges are incorrect'
  \quit 3
\endif

SELECT count(*) = 0 AS assertion
FROM pg_proc routine
JOIN pg_namespace namespace ON namespace.oid = routine.pronamespace
WHERE namespace.nspname = 'public'
  AND (
    has_function_privilege(:'backup_role', routine.oid, 'EXECUTE')
    OR (
      has_function_privilege(:'application_role', routine.oid, 'EXECUTE')
      AND routine.oid <> 'public.gen_random_uuid()'::regprocedure
    )
  )
\gset
\if :assertion
\else
  \warn 'verification failed: a public routine has an excessive runtime or backup grant'
  \quit 3
\endif

SELECT count(*) = 0 AS assertion
FROM pg_class class
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
WHERE namespace.nspname = 'public'
  AND class.relkind = 'S'
  AND (
    NOT has_sequence_privilege(:'backup_role', class.oid, 'SELECT')
    OR has_sequence_privilege(:'backup_role', class.oid, 'UPDATE')
  )
\gset
\if :assertion
\else
  \warn 'verification failed: backup sequence privileges are incomplete or writable'
  \quit 3
\endif

SELECT NOT EXISTS (
  SELECT 1
  FROM pg_class class
  JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
  WHERE namespace.nspname = 'public'
    AND class.relkind = 'S'
    AND class.relname = 'goose_db_version_id_seq'
    AND (
      has_sequence_privilege(:'application_role', class.oid, 'USAGE')
      OR has_sequence_privilege(:'application_role', class.oid, 'SELECT')
      OR has_sequence_privilege(:'application_role', class.oid, 'UPDATE')
    )
) AS assertion
\gset
\if :assertion
\else
  \warn 'verification failed: application can use the Goose bookkeeping sequence'
  \quit 3
\endif

SELECT
  EXISTS (
    SELECT 1
    FROM pg_db_role_setting setting
    JOIN pg_roles role ON role.oid = setting.setrole
    JOIN pg_database database ON database.oid = setting.setdatabase
    WHERE role.rolname = :'migration_role'
      AND database.datname = :'database_name'
      AND format('role=%s', :'owner_role') = ANY (setting.setconfig)
  )
  AND EXISTS (
    SELECT 1
    FROM pg_db_role_setting setting
    JOIN pg_roles role ON role.oid = setting.setrole
    JOIN pg_database database ON database.oid = setting.setdatabase
    WHERE role.rolname = :'backup_role'
      AND database.datname = :'database_name'
      AND 'default_transaction_read_only=on' = ANY (setting.setconfig)
  ) AS assertion
\gset
\if :assertion
\else
  \warn 'verification failed: migrator owner assumption or backup read-only default is missing'
  \quit 3
\endif

ROLLBACK;

SELECT 'result=verified' AS result;
