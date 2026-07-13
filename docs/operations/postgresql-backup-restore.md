# PostgreSQL backup, restore, and rehearsal runbook

This runbook applies to the current Panda Pages PostgreSQL 18 deployment. It
does not authorise an image upgrade, live-volume remount, container recreation,
or production cutover. Replace every angle-bracket placeholder from the
approved deployment inventory; never paste credentials into the document,
shell history, process arguments, or Git.

Read the measured storage evidence and migration decision in
[postgresql-18-storage-audit.md](postgresql-18-storage-audit.md) first.

## Current recovery posture

As of 2026-07-13, no recurring, monitored, encrypted, or restore-tested Panda
Pages backup was found. `archive_mode` is off, so point-in-time recovery is not
available. The measured one-off dump below proved that logical recovery works,
but it was removed after the rehearsal and is not an operational backup.

Until a scheduled system exists:

- recovery-point objective (RPO): unbounded;
- recovery-time objective (RTO): undefined;
- last successful scheduled backup: no evidence;
- off-host retention and encryption: no evidence;
- provider snapshot recovery: unverified.

Recommended minimum follow-up:

1. Take a nightly custom-format logical dump and password-free globals dump.
2. Encrypt before off-host transfer; restrict local staging to mode `0700` and
   files to `0600`.
3. Retain at least seven daily and four weekly recovery points, subject to an
   approved data-retention policy.
4. Monitor job exit status, age, size, checksum creation, upload success, and
   storage capacity.
5. Rehearse a restore at least quarterly and after PostgreSQL, extension, or
   schema changes.
6. Define business-approved RPO/RTO values; a nightly schedule alone implies a
   worst-case logical-backup RPO of almost 24 hours.
7. Add physical base backups plus archived WAL only if a tighter RPO or
   point-in-time recovery is required. Logical dumps do not form a WAL/PITR
   chain.

## Safe logical backup

### Prerequisites

- Confirm the production container, database, and role from read-only Docker
  and SQL evidence. Do not infer them from an old Compose file.
- Use a `pg_dump` client of the same major version as the server, or a newer
  target-version client supported for the source server.
- Write the dump outside the live volume. Prefer streaming it directly to a
  permission-restricted off-host staging directory.
- Ensure the staging filesystem has enough free space and is not inside a Git
  worktree.
- Obtain secrets from the approved secret manager or protected deployment
  file. Do not print them. A local Unix-socket `docker exec` against the current
  container does not need a password argument.
- Confirm the operator has a separate, tested way to restore the image and
  Compose definitions.

### Database dump

The following pattern streams the archive directly away from the VPS. It does
not create a file in the live cluster or on the production host:

```bash
umask 077
backup_dir=<PRIVATE_OFF_HOST_DIRECTORY>
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"

ssh <PRODUCTION_SSH_ALIAS> \
  'docker exec <POSTGRES_CONTAINER> pg_dump \
    --username=<DATABASE_ROLE> \
    --dbname=<DATABASE_NAME> \
    --format=custom \
    --compress=zstd:6 \
    --no-owner \
    --no-acl \
    --lock-wait-timeout=30s' \
  >"$backup_dir/database.dump" \
  2>"$backup_dir/pg_dump.stderr.log"

chmod 0600 "$backup_dir/database.dump" "$backup_dir/pg_dump.stderr.log"
sha256sum "$backup_dir/database.dump" >"$backup_dir/database.dump.sha256"
chmod 0600 "$backup_dir/database.dump.sha256"
```

`pg_dump` takes a transactionally consistent logical snapshot and does not
block ordinary reads or writes. It does take access-share locks, so the
lock-wait timeout prevents an indefinite wait behind conflicting DDL. Record
start/end times, exit status, file size, client/server versions, and stderr.
Treat any non-zero exit or unexpected stderr as a failed backup.

### Roles and global objects

Database archives do not contain cluster roles or tablespaces. Capture globals
without password hashes:

```bash
ssh <PRODUCTION_SSH_ALIAS> \
  'docker exec <POSTGRES_CONTAINER> pg_dumpall \
    --username=<DATABASE_ROLE> \
    --globals-only \
    --no-role-passwords' \
  >"$backup_dir/globals-no-passwords.sql" \
  2>"$backup_dir/pg_dumpall.stderr.log"

chmod 0600 "$backup_dir/globals-no-passwords.sql" \
  "$backup_dir/pg_dumpall.stderr.log"
sha256sum "$backup_dir/globals-no-passwords.sql" \
  >"$backup_dir/globals-no-passwords.sql.sha256"
chmod 0600 "$backup_dir/globals-no-passwords.sql.sha256"
```

Review this file before restore. It can contain privileged role definitions and
tablespace paths even though passwords are omitted. The current deployment has
one non-system role, `pandapages`, with bootstrap superuser capabilities and no
external tablespaces. Reproducing those capabilities during disaster recovery
preserves current behavior; reducing them is a separate, tested security
migration.

### Backup verification

1. Verify both checksums.
2. Run PostgreSQL 18 `pg_restore --list database.dump`; record the source and
   dump versions, compression, format, and TOC entry count.
3. Confirm the globals file contains no password hash and only expected roles
   and tablespaces. Do not publish the file.
4. Scan logs for errors, warnings, and secret leakage.
5. Transfer the encrypted artifacts off-host and verify their checksums again.
6. Record retention/expiry and monitor the next scheduled run.
7. A backup is not considered verified until a disposable restore succeeds.

## Exact restore procedure

### Prerequisites and compatibility

- An approved incident or change window and named operator.
- The database dump, globals dump, checksums, source manifest, image digest,
  schema/application commit, and required non-database assets.
- Secrets supplied separately: target bootstrap password, application database
  URL, session/admin secrets, and any deployment credentials. Never store them
  in the backup or Git.
- A new empty named volume. Never mount the source/live volume into a rehearsal
  or new target.
- A PostgreSQL image compatible with the dump. A PostgreSQL 18.1 custom dump
  can be restored to a supported PostgreSQL 18 target. For a later major
  target, use that target major's `pg_dump` against the source and read its
  migration notes.
- The target must provide `pgcrypto` before or during restore. It is available
  in the official image used by Panda Pages.
- Matching UTF-8 encoding and `en_US.utf8` libc locale unless a separately
  validated collation migration is intended.
- Enough space for the old cluster, new cluster, dump, WAL growth, and image
  layers. Reserve at least 1 GiB for this database's migration artifacts in
  addition to normal image and host headroom.

### Create an isolated empty target

Use a separately reviewed temporary Compose file or equivalent Docker command
that has all of these properties:

- a unique target container name;
- a new, explicitly named volume mounted at `/var/lib/postgresql`;
- no reference to `pandapages_pgdata`;
- an internal-only network;
- no host-published database port;
- no connection from the production API;
- a pinned PostgreSQL image digest;
- the target password supplied through a mode-`0600` secret file;
- `POSTGRES_USER` and `POSTGRES_DB` set to the approved restored role/database;
- checksums enabled and a health check configured.

Do not run `docker compose down`, `docker volume rm`, or a current production
Compose command as part of target creation. Record the new volume ID and verify
through `docker inspect` that it is not the live volume before proceeding.

The official image bootstrap role is a superuser. For an exact recovery, use
the recovered role definition or bootstrap the existing `pandapages` role and
set its password interactively/from the approved secret source. Never pass the
password on a command line. If a least-privilege owner/migrator/application role
split is desired, implement and test that separately rather than during an
emergency restore.

### Restore globals, schema, and data

1. Verify the backup checksums again on the restore host.
2. Inspect `pg_restore --list` and the password-free globals file.
3. Restore only required roles/global grants into the isolated cluster. Skip or
   carefully review definitions for built-in roles already created by the
   target image. Set new passwords separately.
4. If the target bootstrap did not create the database, create it from
   `template0` with UTF-8 encoding, the approved locale, and the restored owner.
5. Restore the custom archive into that empty database:

```bash
docker exec <NEW_TARGET_CONTAINER> pg_restore \
  --username=<RESTORED_OWNER_ROLE> \
  --dbname=<EMPTY_TARGET_DATABASE> \
  --no-owner \
  --no-privileges \
  --exit-on-error \
  --verbose \
  /backup/database.dump \
  >restore.stdout.log \
  2>restore.stderr.log
```

The archive must be mounted read-only. Restoring as the intended owner with
`--no-owner --no-privileges` makes new objects owned by that session role and
avoids stale ACLs. If exact multi-role ownership is required, restore reviewed
globals first and omit `--no-owner` only after testing the ownership graph.

1. Run `ANALYZE` inside the restored disposable/new database. This is permitted
   only on the new target, never as part of the production audit.
2. Do not run Goose migrations merely because a restore occurred. The dump
   includes the schema and Goose history. Run new migrations only when the
   target application release explicitly requires them.

### Validate before cutover

Compare source and target without printing application content:

- database exists and accepts an authenticated connection;
- server version, UTF-8 encoding, locale, checksums, and database size;
- non-system schemas and tables;
- extension names and versions;
- exact row count for every application table;
- sequence names and last values;
- foreign-key count and `convalidated=true`;
- index count and `indisvalid=true`, `indisready=true`;
- published-library query count;
- UTF-8 round-trip check and count of multibyte story versions;
- application/API health and safe public/admin read smoke tests;
- asset storage and referenced files, which are outside this database dump;
- restore logs and `pg_restore --list`.

Do not display titles, story bodies, profile values, account data, cookies, or
credentials in validation logs.

### Cutover

1. Enforce the planned application write freeze.
2. Take and checksum a final logical backup after the freeze.
3. Restore/validate the final backup into the target. Keep the old database
   container and volume unchanged.
4. Stop the production API before switching its database connection. The web
   frontend can remain static, but requests that write must not reach either
   database during the switch.
5. Update the protected application connection setting to the new internal
   database target without printing it.
6. Start/recreate only the application services required for the connection
   switch. Do not delete the old database resources.
7. Validate health, library/story reads, admin authentication and listing,
   UTF-8 preview, and a controlled write/publish transaction if approved.
8. Take a fresh post-cutover backup before the old volume's retention period
   can begin.

Public DNS and Traefik should not need to change when only the API's internal
database target changes.

### Failure handling and rollback

Rollback triggers include a failed database/API health check, missing or
invalid objects, count mismatch, extension failure, authentication failure,
incorrect content encoding, unexpected error rate, or validation timeout.

Before the target accepts writes:

1. Stop the API connected to the target.
2. Restore the old protected database connection setting.
3. Start the old PostgreSQL/API path against its preserved old volume.
4. Repeat health and read validation.

After the target accepts a write, switching back loses or forks those writes.
The maximum safe direct-rollback window therefore ends with the first accepted
post-cutover write. Beyond that point, re-freeze writes and either replay them
from an approved durable source, perform a forward recovery, or execute an
explicit reconciliation plan. Never run old and new databases as simultaneous
application writers.

Preserve the old container definition, old image digest, old volume, final
backup, and validation records through an approved observation window. Do not
delete the old volume until a new backup has itself passed restore verification.

## Disposable rehearsal tool

For a dump already staged outside Git and Docker volume storage:

```bash
scripts/postgresql-restore-rehearsal.sh \
  --dump <PRIVATE_PATH>/database.dump \
  --report-dir <PRIVATE_PATH>/rehearsal-report \
  --expected-row-counts <PRIVATE_PATH>/row-counts.tsv
```

The script:

- refuses a dump under `/var/lib/docker/volumes`;
- refuses a report directory inside any Git worktree;
- refuses `DOCKER_HOST` and non-local Docker contexts;
- pins the production PostgreSQL 18.1 image digest by default;
- creates uniquely named, labeled disposable resources;
- uses an internal network and publishes no port;
- accepts no existing/source volume argument;
- generates a temporary password file without printing it;
- restores into `pandapages_restore_rehearsal`;
- compares optional exact row counts;
- validates extensions, constraints, indexes, sequence state, application read
  shape, and UTF-8 content without displaying rows;
- removes the container, network, volume, and temporary credentials on success
  or failure.

Use `--dry-run` and `--help` to inspect the plan. The script is a restore test,
not a production cutover or upgrade tool.

## 2026-07-13 measured rehearsal

### Source backup

| Property | Result |
| --- | --- |
| Source | Running PostgreSQL 18.1 container over its local Unix socket |
| Method | `pg_dump --format=custom --compress=zstd:6 --no-owner --no-acl --lock-wait-timeout=30s` |
| Destination | Permission-restricted local temporary directory; streamed over SSH; no VPS-side copy |
| Receive start | `2026-07-13T09:34:42.333717648Z` |
| Receive finish | `2026-07-13T09:34:44.156741802Z` |
| End-to-end file-write duration | 1.823 seconds |
| Exit status | 0 |
| Error log | 0 bytes |
| Size | 305,916 bytes |
| SHA-256 | `a0d8fe9588aaf08b3b96e1c9502d87b1eb24a883d3e35828553aeb9728fc18e4` |
| Format | Custom, zstd, dump format 1.16-0 |
| Source/dump client | PostgreSQL 18.1 / PostgreSQL 18.1 |
| TOC entries | 108 (104 listed restore items) |

The password-free globals dump was 530 bytes, took 1.128 seconds, exited 0,
had an empty error log, and had SHA-256
`4174d88dfb45db883c64cbf5c5f15370c2f0d7720d8000dbe8ce9f7dfefd8dc4`.

The VPS clock was not used to calculate duration: it reported NTP enabled but
not synchronized and was approximately 47 seconds ahead of the receiving
workstation. Local file birth/modification timestamps and in-script elapsed
measurements were used.

### Restore target and timing

| Property | Result |
| --- | --- |
| Location | Local Docker engine, not the VPS |
| Image | `postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545` |
| Network | Unique internal disposable network; no published port |
| Storage | New unique disposable volume mounted at `/var/lib/postgresql` |
| Database | `pandapages_restore_rehearsal` |
| Credentials | Generated non-production password file, deleted during cleanup |
| Container startup/health | 2.141 seconds |
| Restore | 0.163 seconds |
| Analyze and validation | 0.248 seconds |
| Script total | 2.714 seconds |
| Active dump + globals + restore-script time | 5.665 seconds |
| Restored database size | 11,089,599 bytes |
| Manual intervention | None |
| Errors/warnings | None; verbose restore progress only |

### Validation result

- 17 application tables restored;
- exact row counts matched all 17 source tables (zero-byte diff);
- `pgcrypto` 1.4 and `plpgsql` 1.0 restored;
- 19 foreign keys were present and validated;
- 99 application indexes were valid and ready;
- `goose_db_version_id_seq` last value matched at 13;
- five published-library join rows matched the source;
- zero UTF-8 round-trip failures;
- four story versions contained multibyte text on both source and target;
- target checksums were on and encoding was UTF-8;
- no story body or personal/application row content was logged.

### Cleanup

The disposable container, internal network, named volume, and temporary
credential directory were removed. Follow-up inspection found no labeled
rehearsal resource and no credential directory. The production dump and globals
artifacts were removed after documentation and validation; no backup, `.env`,
credential, or production data is tracked by Git.

Only safe aggregate logs and checksums were retained temporarily outside the
repository for the duration of this audit.
