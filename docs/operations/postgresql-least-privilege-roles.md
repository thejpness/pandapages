# PostgreSQL least-privilege roles

Status: proposed repository configuration; not deployed

This runbook separates Panda Pages database ownership, migrations, application
runtime, logical backup, and exceptional administration. Applying it is a
production database change and requires a separately approved change window.
This pull request does not connect to production, rotate credentials, apply
the policy, recreate containers, or deploy anything.

## Proven repository requirements

The API reads only its container-scoped `DATABASE_URL`. Its database code uses
bounded queries, inserts, updates, deletes, and transactions; it contains no
runtime schema creation, extension management, migration call, role change,
or other DDL. Goose is the only migration runner and migrations create and
alter the `public` schema objects. Current application SQL uses these tables:

- `accounts`, `child_profiles`, `contributors`, and `profile_settings`;
- `profiles`, `prompt_profiles`, and `reading_progress`;
- `stories`, `story_contributors`, `story_sections`, `story_segments`, and
  `story_versions`.

Other migrated tables remain backed up but are not used by current Go runtime
SQL. UUID defaults require only `public.gen_random_uuid()`. Migrations create
the bundled `pgcrypto` extension and use `digest()` while seeding. PostgreSQL
18 marks `pgcrypto` as trusted, so a database owner can install it without
being a superuser. No extension requires permanent migration-role
superuser access.

The existing bootstrap login `pandapages` is created by the official image and
currently has superuser privileges. The storage audit also found application
objects owned by that login and normal PostgreSQL defaults inherited through
`PUBLIC`. The target policy removes that login from application, migration,
and backup execution. It remains temporarily available only for controlled
operator work and rollback until the observation period ends.

Authoritative PostgreSQL 18 references: [predefined roles](https://www.postgresql.org/docs/18/predefined-roles.html), [role membership options](https://www.postgresql.org/docs/18/sql-grant.html), [creator-specific default privileges](https://www.postgresql.org/docs/18/sql-alterdefaultprivileges.html), and the [trusted `pgcrypto` extension](https://www.postgresql.org/docs/18/pgcrypto.html).

## Role model

| Role | LOGIN | Limit | Membership | Purpose |
| --- | --- | ---: | --- | --- |
| `pandapages_owner` | No | n/a | none | Owns the database, `public` schema, and non-extension application objects. |
| `pandapages_migrator` | Yes | 2 | May `SET ROLE pandapages_owner`; no inheritance or admin option. | Goose login only. |
| `pandapages_app` | Yes | 20 | none | Go API runtime only. The API pool is capped at 10 connections. |
| `pandapages_backup` | Yes | 2 | none | Public-schema logical dump and password-free globals inventory. |
| `pandapages` | Yes, temporarily | operator-controlled | none required | Legacy image-bootstrap superuser retained only for exceptional administration and rollback. |

All four policy roles are `NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
NOREPLICATION NOBYPASSRLS`. The owner is `NOLOGIN`. The migration membership
has `ADMIN FALSE`, `INHERIT FALSE`, and `SET TRUE`; it cannot grant the owner
role or alter roles. A database-specific role setting makes Goose sessions
assume the owner explicitly. Neither the API nor backup login can `SET ROLE`
to another policy role.

## Ownership and access

`pandapages_owner` owns the `pandapages` database, `public` schema, and all
non-extension tables, views, materialized views, sequences, routines, enums,
and domains in that schema. Identity/serial sequence ownership follows its
owning table. Existing extension objects keep PostgreSQL-managed ownership;
on a fresh database, `pgcrypto` is installed by the effective owner. Updating
an existing extension remains a reviewed one-time administrative operation.

The application role receives:

- `CONNECT` only to `pandapages` and `USAGE` on `public`;
- `SELECT`, `INSERT`, `UPDATE`, and `DELETE` on the current runtime table
  allowlist;
- `USAGE` and `SELECT` on application identifier sequences, excluding Goose's
  bookkeeping sequence;
- `EXECUTE` only on `public.gen_random_uuid()` among public routines.

It receives no schema `CREATE`, database `TEMPORARY`, table `TRUNCATE`,
`REFERENCES`, `TRIGGER`, or `MAINTAIN` privileges and owns no objects.

The backup role receives:

- `CONNECT` only to `pandapages` and `USAGE` on `public`;
- `SELECT` on every public table and sequence;
- a database-specific `default_transaction_read_only=on` setting.

It has no write, sequence update/usage, routine execution, schema creation, or
role membership. The backup archive deliberately covers the application
`public` schema plus `pgcrypto`; it does not use the cluster-wide
`pg_read_all_data` predefined role. The same backup login can run
`pg_dumpall --globals-only --no-role-passwords` against `pandapages` because
that command can inventory visible role metadata without password hashes.

The policy revokes unnecessary `PUBLIC` access:

- `CONNECT` and `TEMPORARY` on every connectable database in this dedicated
  cluster;
- `USAGE` and `CREATE` on `public`;
- all privileges on public tables, sequences, routines, and application-defined
  types.

Superusers are not constrained by ACLs, which is why the legacy bootstrap
login must never remain in an application or timer environment.

## Future migrations and default privileges

`ALTER DEFAULT PRIVILEGES` is creator-specific. Goose logs in as
`pandapages_migrator`, then its database setting changes `current_user` to
`pandapages_owner`; defaults therefore belong to the owner role. Future owner
tables automatically grant runtime CRUD and backup `SELECT`; future sequences
grant runtime `USAGE, SELECT` and backup `SELECT`. New routines and types do
not become public or executable by the API automatically.

When a migration adds a runtime table, update the explicit current-table
allowlist in `deploy/postgresql-roles/apply.sql` and `verify.sql` in the same
change. When runtime code needs a new function, add one named `EXECUTE` grant
and a negative/positive permission test. Never compensate with `GRANT ALL`,
`pg_read_all_data`, owner membership, or API ownership.

## Repository tooling

The wrapper uses a known local Docker Unix socket and accepts no password:

```bash
scripts/postgresql-roles.sh --help

# Read-only inventory
scripts/postgresql-roles.sh audit \
  --container <POSTGRES_CONTAINER> \
  --database pandapages \
  --admin-user <ADMIN_LOGIN>

# Mutating operation: only in an approved change window
scripts/postgresql-roles.sh apply \
  --container <POSTGRES_CONTAINER> \
  --database pandapages \
  --admin-user <ADMIN_LOGIN> \
  --confirm-apply

# Read-only assertions
scripts/postgresql-roles.sh verify \
  --container <POSTGRES_CONTAINER> \
  --database pandapages \
  --admin-user <ADMIN_LOGIN>
```

`apply` is transactional and advisory-locked. It creates missing roles,
corrects attributes, ownership, memberships, grants, and default privileges,
but never sets a password, drops a role/object, changes application data, or
runs a migration. It refuses PostgreSQL versions before 18, the wrong target
database, a non-superuser administrative session, or a cluster containing an
unexpected database. That last preflight is required because revoking public
database access is cluster-wide.

Run `audit` first and retain its non-secret output with the approved change
record. Stop if deployed roles, databases, owners, extension state, or objects
differ from the reviewed inventory.

## Local development bootstrap

Existing development volumes also begin with only the image-bootstrap login.
Use generated development-only values in the ignored `.env` and secret file;
never reuse production values:

1. Start only PostgreSQL with `docker compose -f docker-compose.dev.yml up -d postgres`.
2. Apply the role policy against the development container with explicit
   confirmation.
3. Set the generated migrator and application passwords interactively through
   `psql` using `\password`; do not put password-bearing SQL in shell history.
4. Set ignored `APP_DATABASE_URL` and `MIGRATION_DATABASE_URL` values matching
   those roles. Keep the existing ignored PostgreSQL bootstrap secret separate.
5. Start the migration and API services. Verify the resolved Compose output
   gives each service only its intended credential.

A missing role or URL fails startup; there is no fallback to the bootstrap
login. Re-running the policy does not reset the generated passwords.

## Credential boundaries

The host deployment environment uses three distinct values:

- `POSTGRES_PASSWORD`: legacy administrative/bootstrap secret, visible only to
  the PostgreSQL container bootstrap contract;
- `MIGRATION_DATABASE_URL`: `pandapages_migrator` credential, visible only to
  the one-shot migration container as `GOOSE_DBSTRING`;
- `APP_DATABASE_URL`: `pandapages_app` credential, visible only to the API
  container as `DATABASE_URL`;
- `PGAPPNAME=pandapages-api`: fixed, non-secret API connection metadata. The
  production and development Compose definitions set it only on the API so
  the cutover verifier can distinguish that workload from an unrelated
  `psql` client. A database URL must not override it.

The backup service uses `PP_BACKUP_DATABASE_USER=pandapages_backup` through a
local container Unix socket. If the reviewed HBA policy requires password
authentication, supply it through a root-owned PostgreSQL passfile or an
equivalent secret mechanism; never add a password to command arguments or the
tracked backup environment example.

Store production values only in the existing root-owned mode-`0600`
deployment/credential files. Generate independent random credentials; do not
reuse the bootstrap, application, migration, or backup secret. URLs must be
encoded correctly and must never be printed, pasted into tickets, or committed.
The frontend receives no database value. PostgreSQL remains on the internal
Docker network with no published host port.

To rotate a login, create a new random value in the protected source, change
only that role's password from an approved interactive administrative session,
update only its consumer, recreate that consumer, and verify it before removing
the previous value from the protected source.

## Production rollout (not authorised by this PR)

Use a maintenance window even though the ACL operation is transactional.
Retain the old configuration until the observation period is complete.

1. Confirm a recent encrypted backup and successful disposable restore, disk
   headroom, current image/volume identity, and an approved rollback operator.
2. Stop before mutation if the read-only audit differs from this policy or the
   cluster contains an unrelated database.
3. Generate independent migration and application credentials into the
   root-owned mode-`0600` deployment file. Prepare the backup credential only
   if HBA requires one. Do not change the active API URL yet.
4. Run `postgresql-roles.sh apply ... --confirm-apply` once from the approved
   administrative session. This creates the owner, migration, application, and
   backup roles, transfers ownership, and establishes current/default ACLs.
5. Set login passwords interactively with `psql`'s `\password` command or the
   approved secret mechanism. Do not place password-bearing SQL in a file,
   shell argument, log, or change record.
6. Run `postgresql-roles.sh verify`, then inspect the read-only audit. Confirm
   all policy roles are non-superuser, memberships match exactly, `PUBLIC`
   access is revoked, and extension ownership is understood.
7. From an isolated operator context, connect as `pandapages_app` and verify
   current reads/CRUD plus expected DDL, cross-database, and `SET ROLE` denial.
8. Connect as `pandapages_migrator`; confirm `session_user` is the migrator and
   `current_user` is the owner. Run Goose status and an approved no-op/current
   migration check before any future migration.
9. Run the backup preflight as `pandapages_backup`, then create an approved
   custom dump and password-free globals inventory. Confirm write/DDL denial.
10. Set `MIGRATION_DATABASE_URL` and `APP_DATABASE_URL` in the protected
    deployment file. Confirm the old bootstrap URL remains available only for
    rollback and is absent from resolved API/migration environments.
11. Run the one-shot migration service. Only after it succeeds, recreate the
    API container; do not recreate PostgreSQL or its volume.
12. Verify library/read/progress operations, the authenticated admin list, and
    UTF-8 preview/publish with an approved non-production fixture. Then run the
    repository-owned API role verifier below. Treat either application smoke
    failure or verifier failure as a failed cutover and follow rollback.
13. Run one approved encrypted backup and disposable restore verification with
    the backup role. Confirm remote completion, checksums, cleanup, and status.
14. Confirm the legacy superuser is absent from active API, migration, and
    backup processes. Observe for at least seven days and through one daily
    backup plus one weekly restore rehearsal before disabling its LOGIN.

### Durable API database-role verification

Run this immediately after the recreated API passes its application and admin
smoke tests. Resolve the two current container IDs from the reviewed Compose
project; do not copy an address from `docker inspect` or pass one to SQL.

```bash
api_container=$(docker compose ps -q api)
postgres_container=$(docker compose ps -q postgres)
scripts/postgresql-api-role-verify.sh \
  --api-container "$api_container" \
  --postgres-container "$postgres_container" \
  --database pandapages \
  --admin-user <ADMIN_LOGIN>
```

The verifier fails closed unless all of the following are true:

- the named API and PostgreSQL containers are running and share exactly one
  Docker network;
- the API health endpoint succeeds;
- an unlock request, built inside the API container without returning its
  passcode to the host, establishes protected session cookies, and an
  immediate authenticated library request exercises an always-querying
  database path even when the unlock account lookup is cached;
- PostgreSQL reports a recent client backend from that API container address
  using database `pandapages`, role `pandapages_app`, and application name
  `pandapages-api`;
- no connection from that API source uses `pandapages`, `pandapages_owner`,
  `pandapages_migrator`, `pandapages_backup`, or another unexpected identity.

Expected success ends with `api_role_verification=passed`. Output contains
only non-secret named conditions; it does not print a client address, database
URL, passcode, cookie, or password. The protected probe directory, including
request, response, and cookie material, is removed on both success and failure.

This is strong operational attribution to the current container, shared
network source, workload name, database, role, and a controlled request. It is
not cryptographic attestation of the container image, proof that every API
query has been exercised, or a substitute for the remaining application and
admin smoke tests. An operator with Docker-root access can intentionally alter
container or network identity, so access to that host remains privileged.

A failed verifier is a rollback condition. Preserve its named failure without
secrets, restore the previous API database configuration, recreate only the
API, and confirm service recovery. Do not accept a cluster-wide count of
`pandapages_app` sessions: an unrelated manual `psql` connection can satisfy
that weaker test.

On 13 July 2026, the API cutover itself succeeded, but a temporary rollout
helper placed a psql literal-variable expression inside `psql --command`.
That command is sent directly for server parsing, so the colon expression was
not interpolated and PostgreSQL rejected the helper SQL. Rollback completed
successfully; no database or application defect was identified. The durable
verifier uses fixed SQL from stdin and compares validated Docker metadata
outside SQL, removing that evaluation boundary entirely.

After this correction is merged and push-to-main CI is green, restart the
controlled production rollout from the beginning. Repeat every preflight,
role, migration, application, backup, restore, cleanup, and timer gate; do not
resume from the stopped cutover step.

Do not drop the legacy role during rollout. After the observation period,
verify it owns no non-extension application objects, preserve a separately
controlled administrative path, and use `ALTER ROLE <LEGACY_ADMIN> NOLOGIN`
only under a separately reviewed change. Role deletion is outside this policy.

## Rollback

Rollback changes the consumer configuration, not database ownership or ACLs:

1. Freeze new deployment activity and capture the failure evidence without
   secrets.
2. Restore the previous API `DATABASE_URL` value from the protected rollback
   source and recreate only the API container.
3. If required, restore the prior migration setting before the next migration;
   do not rerun or reverse a migration merely to roll back credentials.
4. Confirm API health, unlock, library, reading, progress, and admin operations.
5. Keep the new roles and ownership in place while diagnosing. Do not drop
   roles, revoke ownership, run `DROP OWNED`, or delete data.
6. Prevent concurrent use of old and new application credentials beyond the
   minimum rollback window. Record which credential is active.

Because the legacy login remains a superuser during the observation window,
this restores service quickly but temporarily restores the original risk. Set
a short follow-up window to correct the issue and repeat the cutover. Never
disable the last tested administrative path.

## Verification and troubleshooting

The generated PostgreSQL 18 integration suite proves migrations, API startup and unlock, application
CRUD and denials, future-object defaults, custom and globals dumps, backup
write denials, `pgcrypto`, and cleanup using disposable data only:

```bash
docker build --target prod --build-arg API_MAIN=./cmd/api -t pandapages-api:role-test apps/api
docker build --target migrate -t pandapages-migrate:role-test apps/api
scripts/tests/postgresql-roles-contract.sh
scripts/tests/postgresql-roles-integration.sh
```

For an API permission error, identify the table/function named by PostgreSQL,
compare it with current Go SQL and the allowlist, and add the narrow privilege
only if runtime use is proven. For a migration denial, confirm Goose received
only `MIGRATION_DATABASE_URL`, then inspect `session_user`, `current_user`, and
the owner membership options. For backup denial, run role verification and
confirm the object is in the public application schema; do not grant a broad
predefined role to bypass an unexpected schema.

Logs and evidence may include role names, attributes, object names, and named
failed conditions. They must not include URLs, passwords, passfiles, raw
environment blocks, application content, cookies, or tokens.
