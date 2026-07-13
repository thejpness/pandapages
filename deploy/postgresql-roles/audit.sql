\set ON_ERROR_STOP on

BEGIN READ ONLY;

SELECT 'server_version' AS record, current_setting('server_version') AS value;
SELECT 'database' AS record, current_database() AS name, pg_get_userbyid(database.datdba) AS owner
FROM pg_database database
WHERE database.datname = current_database();

SELECT
  'role' AS record,
  role.rolname,
  role.rolcanlogin,
  role.rolsuper,
  role.rolcreatedb,
  role.rolcreaterole,
  role.rolinherit,
  role.rolreplication,
  role.rolbypassrls,
  role.rolconnlimit
FROM pg_roles role
WHERE role.rolname IN (:'owner_role', :'migration_role', :'application_role', :'backup_role')
ORDER BY role.rolname;

SELECT
  'membership' AS record,
  member.rolname AS member,
  granted.rolname AS granted_role,
  membership.admin_option,
  membership.inherit_option,
  membership.set_option
FROM pg_auth_members membership
JOIN pg_roles member ON member.oid = membership.member
JOIN pg_roles granted ON granted.oid = membership.roleid
WHERE member.rolname IN (:'migration_role', :'application_role', :'backup_role')
ORDER BY member.rolname, granted.rolname;

SELECT
  'database_access' AS record,
  database.datname,
  has_database_privilege(:'application_role', database.oid, 'CONNECT') AS application_connect,
  has_database_privilege(:'migration_role', database.oid, 'CONNECT') AS migration_connect,
  has_database_privilege(:'backup_role', database.oid, 'CONNECT') AS backup_connect,
  EXISTS (
    SELECT 1
    FROM aclexplode(COALESCE(database.datacl, acldefault('d', database.datdba))) privilege
    WHERE privilege.grantee = 0
      AND privilege.privilege_type = 'CONNECT'
  ) AS public_connect
FROM pg_database database
WHERE database.datallowconn
ORDER BY database.datname;

SELECT
  'schema' AS record,
  namespace.nspname,
  pg_get_userbyid(namespace.nspowner) AS owner,
  has_schema_privilege(:'application_role', namespace.oid, 'USAGE') AS application_usage,
  has_schema_privilege(:'application_role', namespace.oid, 'CREATE') AS application_create,
  has_schema_privilege(:'backup_role', namespace.oid, 'USAGE') AS backup_usage,
  EXISTS (
    SELECT 1
    FROM aclexplode(COALESCE(namespace.nspacl, acldefault('n', namespace.nspowner))) privilege
    WHERE privilege.grantee = 0
      AND privilege.privilege_type = 'CREATE'
  ) AS public_create
FROM pg_namespace namespace
WHERE namespace.nspname = 'public';

SELECT
  'object_owners' AS record,
  pg_get_userbyid(class.relowner) AS owner,
  count(*) AS object_count
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
GROUP BY class.relowner
ORDER BY owner;

SELECT
  'extension' AS record,
  extension.extname,
  extension.extversion,
  pg_get_userbyid(extension.extowner) AS owner
FROM pg_extension extension
ORDER BY extension.extname;

SELECT
  'public_grant' AS record,
  namespace.nspname,
  class.relname,
  privilege.privilege_type
FROM pg_class class
JOIN pg_namespace namespace ON namespace.oid = class.relnamespace
CROSS JOIN LATERAL aclexplode(COALESCE(class.relacl, acldefault(
  CASE WHEN class.relkind = 'S' THEN 'S'::"char" ELSE 'r'::"char" END,
  class.relowner
))) privilege
WHERE namespace.nspname = 'public'
  AND privilege.grantee = 0
ORDER BY class.relname, privilege.privilege_type;

ROLLBACK;
