# PostgreSQL least-privilege roles

Status: production least-privilege cutover and backup/restore rollout completed;
observation active since 14 July 2026 at 17:10:41 UTC. Repository verifier
support for signed sessions is not deployed because the signed-session
application remains a future change.

This runbook separates Panda Pages database ownership, migrations, application
runtime, logical backup, and exceptional administration. The role model,
automated encrypted production backups, and disposable restore verification
are deployed and working. The observation period remains active. This pull
request does not contact or change production; it only adds repository support
for explicitly verifying either the legacy or signed application session
contract. Signed mode is not the current production mode and this change does
not authorise or require a production rerun, restart, deployment, or rollout.

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
the bundled `pgcrypto` extension; historical migration `00008` used `digest()`
while installing test data, and forward migration `00013` removes that data.
PostgreSQL 18 marks `pgcrypto` as trusted, so a database owner can install it
without being a superuser. No extension requires permanent migration-role
superuser access.

Historically, before the completed cutover, the bootstrap login `pandapages`
was created by the official image with superuser privileges. The storage audit
also found application objects owned by that login and normal PostgreSQL
defaults inherited through `PUBLIC`. The completed policy removed that login
from application, migration, and backup execution. It remains enabled solely
for rollback during the active observation period. Disabling it remains
deferred until the observation gate and its required backup/restore evidence
have completed.

Authoritative PostgreSQL 18 references: [predefined roles](https://www.postgresql.org/docs/18/predefined-roles.html), [role membership options](https://www.postgresql.org/docs/18/sql-grant.html), [creator-specific default privileges](https://www.postgresql.org/docs/18/sql-alterdefaultprivileges.html), and the [trusted `pgcrypto` extension](https://www.postgresql.org/docs/18/pgcrypto.html).

## Role model

| Role | LOGIN | Limit | Membership | Purpose |
| --- | --- | ---: | --- | --- |
| `pandapages_owner` | No | n/a | none | Owns the database, `public` schema, and non-extension application objects. |
| `pandapages_migrator` | Yes | 2 | May `SET ROLE pandapages_owner`; no inheritance or admin option. | Goose login only. |
| `pandapages_app` | Yes | 20 | none | Go API runtime only. The API pool is capped at 10 connections. |
| `pandapages_backup` | Yes | 2 | none | Public-schema logical dump and password-free globals inventory. |
| `pandapages` | Yes, observation period only | operator-controlled | none required | Legacy image-bootstrap superuser retained solely for rollback while the observation gate is active. |

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

Migration `00008_seed_test_data.sql` is immutable applied history. Current
repository Goose runs continue through `00014_reader_2_contract.sql`.
Migration `00013` leaves no positively identified historical test stories,
child/prompt profiles, progress, or generation jobs; migration `00014` then
performs the approved complete beta progress reset and installs the Reader 2
segment/locator contract. Local and disposable
fixtures are installed only through the fail-closed command described in
`docs/development/test-fixtures.md`; neither production Compose nor the
migration container invokes it. This repository change does not deploy either
migration; a future 00014 rollout must update API and web together and accept
the irreversible progress-reset limitation documented in
`docs/development/reader-2-backend-contract.md`.

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

## Completed production rollout and active observation

The least-privilege role cutover, encrypted backup rollout, and disposable
restore verification are complete and working. The observation period began
on 14 July 2026 at 17:10:41 UTC and must continue undisturbed. Steps 1–13 below
are retained as the reviewed record of the completed rollout sequence; step 14
describes the active observation gate. They are not instructions to repeat the
rollout because repository verifier capability changes.

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

This verifier was used after the recreated API passed its application and
admin smoke tests. PR #14 does not require another production run. For a
separately approved verification of the currently deployed legacy application,
resolve the two current container IDs from the reviewed Compose project; do
not copy an address from `docker inspect` or pass one to SQL.

```bash
api_container=$(docker compose ps -q api)
postgres_container=$(docker compose ps -q postgres)
scripts/postgresql-api-role-verify.sh \
  --api-container "$api_container" \
  --postgres-container "$postgres_container" \
  --database pandapages \
  --session-contract legacy \
  --admin-user <ADMIN_LOGIN>
```

Run the verifier from a complete repository checkout. If approved operator
tooling is copied elsewhere, deploy both
`scripts/postgresql-api-role-verify.sh` and
`scripts/lib/postgresql-api-role-session-cookie.awk`, preserve their relative
directory structure, and preserve appropriate executable and read permissions.
The verifier deliberately fails closed when the adjacent AWK contract file is
unavailable. Do not paste or reconstruct the AWK logic inline during a
production change.

`legacy` is the current production contract and the verifier's
backward-compatible default. All tracked repository and operational
invocations select it explicitly. Current production must continue to use
`--session-contract legacy`; it requires the deployed `pp_unlocked=1` and
nonempty `pp_aid` cookies and rejects a signed-only response.

The verifier also supports an explicit `--session-contract signed` mode for
the future PR #13 application contract. Signed mode requires exactly one
active `pp_session`, rejects active legacy authentication cookies, and permits
empty legacy cookie deletion headers. Selection is deliberately not
auto-detected: a verifier run must prove the contract intended for the API
version under review.

The verifier does not inspect or reimplement the signed token's HMAC format.
The successful protected library request is the proof that the running API
accepted the session cookie returned by unlock.

Adding both modes to repository tooling does not change production. No rollout
or restart is part of this change. A future separately approved deployment of
PR #13 or its successor must invoke the verifier with
`--session-contract signed` after the API cutover; do not claim signed-session
verification while the legacy application is deployed.

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

During the earlier rollout attempt on 13 July 2026, the API cutover portion
succeeded, but a temporary rollout helper placed a psql literal-variable
expression inside `psql --command`. That command is sent directly for server
parsing, so the colon expression was not interpolated and PostgreSQL rejected
the helper SQL. Rollback completed successfully; no database or application
defect was identified. The durable verifier uses fixed SQL from stdin and
compares validated Docker metadata outside SQL, removing that evaluation
boundary entirely. The later production cutover and backup/restore rollout
completed successfully before the current observation period began.

Merging this verifier-capability change does not require a production rollout
or restart. Continue the existing observation period unchanged. Current
production remains on the legacy application session contract and must be
verified with `--session-contract legacy`.

A future separately approved deployment of PR #13 or its successor must use
`--session-contract signed` after the API cutover. That future application
deployment must follow its own preflight, browser-update, smoke-test, and
rollback procedure. It must not restart or repeat the already-completed
PostgreSQL role and backup/restore rollout without a separate operational
reason.

The legacy bootstrap login remains enabled solely for rollback during the
active observation period. Disabling it remains deferred until the observation
gate and its required backup/restore evidence have completed. At that point,
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
