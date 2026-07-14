# Automated encrypted PostgreSQL backups

Status: proposed repository configuration; not deployed

This runbook describes the backup automation added by this change. It builds
on the measured storage and restore evidence in
[postgresql-18-storage-audit.md](postgresql-18-storage-audit.md) and the manual
[backup and restore runbook](postgresql-backup-restore.md). It does not
authorise a production installation, PostgreSQL restart, image change,
`PGDATA` change, volume remount, schema migration, or recovery cutover.

## Recovery targets

- Initial recovery-point objective (RPO): 24 hours or better when the daily
  timer and remote upload are healthy.
- Initial recovery-time objective (RTO): 30 minutes or better for this roughly
  10.6 MB database.
- Point-in-time recovery is not provided. WAL archiving remains disabled.

The measured dump and disposable restore each take only a few seconds. The
30-minute RTO includes failure discovery, operator access, object download,
checksum verification, decryption, restore, validation, application connection
switching, and smoke checks. It is not a claim that `pg_restore` itself takes
30 minutes.

## Architecture

```text
systemd daily timer
  -> root-owned host script (flock + private temporary directory)
  -> docker exec against the existing PostgreSQL Unix socket
       -> one exported read-only database snapshot shared by:
            -> public-schema + pgcrypto custom dump as pandapages_backup
            -> aggregate restore expectations
       -> pg_dumpall --globals-only --no-role-passwords as pandapages_backup
  -> deterministic tar bundle
  -> age client-side encryption
  -> rclone S3-compatible remote outside the VPS
  -> download and SHA-256 verification
  -> completion marker
  -> 7 daily + 4 weekly + 3 monthly retention
  -> local last-success state

systemd weekly timer
  -> newest completed remote backup
  -> download + SHA-256 + age authentication/decryption
  -> fixed archive-layout and inner-checksum validation
  -> existing isolated PostgreSQL 18 restore rehearsal
  -> exact schemas, tables, extensions, constraints, indexes, sequences,
     aggregate row counts, and UTF-8 checks
  -> disposable container/network/volume/credentials/plaintext cleanup
  -> local last-restore-success state
```

The scripts never copy `/var/lib/postgresql/18/docker`, never mount
`pandapages_pgdata`, never publish a PostgreSQL port, and never connect the
production application to the rehearsal database. The off-host remote must not
be GitHub, the Panda Pages asset store, another Docker volume on the VPS, or a
path on the same VPS.

No object-storage provider is currently configured in the repository. The
interface is therefore an explicitly named `rclone` remote using an
S3-compatible provider. Backblaze B2 S3, Hetzner Object Storage, AWS S3, or an
equivalent existing provider can be selected during a separately approved
deployment. This change does not create or purchase a bucket.

The scripts inspect rclone's redacted configuration and fail closed unless the
selected remote uses the S3 backend; an accidentally local rclone remote is
rejected before a dump is created.

## Schedules and retention

| Operation | UTC schedule | Freshness threshold |
| --- | --- | --- |
| Encrypted backup | Daily at 03:17 | 36 hours |
| Disposable restore verification | Sunday at 05:00 | 9 days |
| Local stale-state check | Daily at 09:00 and 21:00 | Both thresholds above |

The timers use `Persistent=true`, so systemd runs a missed event after the host
returns. The backup uses a non-blocking `flock`; a second backup or restore
verification cannot overlap it.

Completed backup IDs are deterministic UTC names:

```text
pandapages-postgresql-YYYYMMDDTHHMMSSZ.tar.age
pandapages-postgresql-YYYYMMDDTHHMMSSZ.tar.age.sha256
pandapages-postgresql-YYYYMMDDTHHMMSSZ.complete
```

The completion marker is uploaded last. Restore verification and retention
consider only sets with a valid completion-marker name. Retention keeps the
union of the newest backup from each of the most recent seven UTC days, four
ISO weeks, and three UTC months. A failed upload attempts to remove its unique
partial object set; plaintext is removed by an exit trap in every case.

Provider-side bucket versioning and, where supported, Object Lock or an
equivalent immutable retention period should also be enabled. The scoped VPS
credential needs list, read, write, and delete access to only the configured
backup prefix because the script performs verification and pruning. Provider
immutability is the defence against a compromised VPS credential deleting all
recovery points. Configure automatic abortion of stale multipart uploads.

## Encryption and key custody

Backups are encrypted locally with `age` before upload. The object-storage
provider sees ciphertext, its SHA-256, size, timestamp, and completion marker;
it cannot read the database or globals dump.

Required files:

| File | Contents | Required mode |
| --- | --- | --- |
| `/etc/pandapages/postgresql-backup-age-recipients.txt` | One or more public age recipients | `0600` |
| `/etc/pandapages/postgresql-backup-age-identities.txt` | Matching private age identities for automated verification | `0600` or stricter |
| `/etc/pandapages/postgresql-backup-rclone.conf` | Scoped object-store endpoint and credentials | `0600` |
| `/etc/pandapages/postgresql-backup.env` | Non-secret paths, identifiers, retention, and pinned versions | `0600` |
| `/etc/pandapages/postgresql-backup-notify.curl` | Optional private webhook URL/headers | `0600` |

Keep at least one tested offline copy of every active private age identity in a
separate password manager, hardware-encrypted medium, or organisational key
escrow. Losing all matching identities permanently destroys the ability to
decrypt every affected backup. Object-store support cannot recover the key.

For rotation:

1. Generate a new identity offline and back it up separately.
2. Add its public recipient alongside the old recipient; do not remove the old
   identity from the restore host yet.
3. Run and restore-verify a new multi-recipient backup using the new identity.
4. Keep both private identities until every backup encrypted only to the old
   key has expired from all retention tiers and provider versions.
5. Remove the old recipient and identity only after an approved recovery test.

The automated verifier necessarily has a decryption identity on the VPS. That
meets the goal of hiding backup content from the storage provider, but it does
not protect against a fully compromised root account. Offline key custody,
bucket immutability, credential scoping, and provider audit alerts remain
necessary defence in depth.

## Pinned tools

Install the exact stable versions below from their official release artifacts.
Verify the publisher's release checksum, Sigsum proof, or equivalent provenance
mechanism provided for each artifact before installation:

- `age` 1.3.1;
- `rclone` 1.74.4;
- disposable restore image
  `postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545`.

The scripts fail closed if the `age` or `rclone` version differs. Do not replace
these with mutable `latest` references. A version change requires a reviewed
repository update and a successful generated-data backup/restore test.

## Dedicated database role

Both dumps use `pandapages_backup`, the dedicated role defined in
[postgresql-least-privilege-roles.md](postgresql-least-privilege-roles.md).
It is a non-superuser login with no memberships, database or schema ownership,
write privileges, routine execution, or schema creation. It receives direct
`SELECT` on public tables and sequences, `CONNECT` to `pandapages`, `USAGE` on
`public`, and a database-specific read-only transaction default. It does not
receive the broader `pg_read_all_data` predefined role.

The custom archive is explicitly scoped to the application `public` schema
plus `pgcrypto`. This keeps unrelated operator schemas outside the backup
contract. `pg_dumpall --globals-only --no-role-passwords` uses the same backup
login and includes visible role and global metadata without password verifiers.
The legacy bootstrap superuser is never used by either timer.

The backup script verifies this exact privilege shape before dumping: role
attributes and memberships, target-database connect, public-schema usage,
read access to every public table and sequence, and absence of write or
sequence-update privileges. An unexpected or inaccessible schema is a policy
review, not a reason to grant a cluster-wide role.

No password is accepted by the script or passed in process arguments. The
intended production contract uses the existing container-local Unix socket.
If reviewed HBA rules require authentication, use a root-owned PostgreSQL
passfile or equivalent protected secret and test it separately; do not put a
password in arguments, the tracked environment example, or logs. PostgreSQL
must remain unexposed.

## Deployment prerequisites requiring approval

None of these steps are performed by this pull request:

1. Select or approve an existing S3-compatible provider and private bucket.
2. Enable bucket versioning/immutability and multipart-upload cleanup where
   supported.
3. Create a prefix-scoped service credential; do not reuse an administrator
   credential.
4. Install and verify the pinned `age` and `rclone` binaries.
5. Generate the age identity offline, escrow it separately, and install only
   the required identity/recipient files.
6. Apply and verify the reviewed least-privilege role policy, including the
   dedicated backup role; follow its staged rollout and rollback runbook.
7. Copy the example configuration files to `/etc/pandapages`, replace every
   placeholder, set owner `root:root`, and set mode `0600`.
8. Copy the scripts to `/opt/pandapages/scripts`, preserving executable modes.
9. Install `deploy/tmpfiles.d/pandapages-postgresql-backup.conf` as
   `/etc/tmpfiles.d/pandapages-postgresql-backup.conf`, owned by `root:root`
   with mode `0644`. Before installing or starting the services, create and
   verify their shared operations-log directory:

   ```bash
   sudo systemd-tmpfiles --create \
     /etc/tmpfiles.d/pandapages-postgresql-backup.conf
   sudo stat -c '%U:%G:%a' /var/log/pandapages-postgresql-backup
   ```

   The expected result is `root:root:700`. systemd opens each `append:` output
   target while preparing the service. If its parent directory does not
   already exist, startup fails with `209/STDOUT` before the job script runs;
   `LogsDirectory=` alone is not a sufficient bootstrap for that append path.
10. Copy the units to `/etc/systemd/system`, preserving mode `0644`. Run
   `systemd-analyze verify` against all units and confirm the root-only
   operations log path has appropriate host disk monitoring.
11. Run one on-demand backup and one restore verification before enabling the
    stale checker or timers.
12. Confirm the remote objects, local success stamps, cleanup, root-only
    operations log, and notification delivery.
13. Enable the three timers only after review of the evidence.

The tracked examples are:

- `deploy/postgresql-backup/postgresql-backup.env.example`;
- `deploy/postgresql-backup/rclone.conf.example`;
- `deploy/postgresql-backup/age-recipients.txt.example`;
- `deploy/postgresql-backup/notify.curl.example`;
- `deploy/tmpfiles.d/pandapages-postgresql-backup.conf`;
- `deploy/systemd/pandapages-postgresql-*.service` and `.timer`.

Never commit the installed files, private identity, real remote name, endpoint,
bucket, access keys, webhook URL, or any generated backup.

## Operating the jobs

### Preflight and on-demand backup

After approved configuration, validate the source role and toolchain without
creating a dump:

```bash
sudo systemd-run --wait --pipe \
  --property=EnvironmentFile=/etc/pandapages/postgresql-backup.env \
  --property=RuntimeDirectory=pandapages-postgresql-backup \
  --property=RuntimeDirectoryMode=0750 \
  /opt/pandapages/scripts/postgresql-backup.sh --dry-run
```

Run a real on-demand backup through its hardened unit:

```bash
sudo systemctl start pandapages-postgresql-backup.service
sudo systemctl status pandapages-postgresql-backup.service
sudo tail -n 100 /var/log/pandapages-postgresql-backup/operations.log
```

Do not use `set -x`. Successful output contains only the backup ID, encrypted
size, duration, retention counts, and cleanup status.

### On-demand restore verification

```bash
sudo systemctl start pandapages-postgresql-restore-verify.service
sudo systemctl status pandapages-postgresql-restore-verify.service
sudo tail -n 100 /var/log/pandapages-postgresql-backup/operations.log
```

The verifier downloads only the newest completed set. It checks the outer
ciphertext SHA-256 and size, authenticates/decrypts with age, permits only the
fixed archive member paths and safe regular-file/directory types, verifies
inner checksums, and invokes
`postgresql-restore-rehearsal.sh`. Its PostgreSQL container, internal network,
new volume, password file, decrypted dump, and report directory are removed on
success or failure. It never restores globals into the disposable target.

The backup and restore units retain `PrivateTmp=true`. Temporary backup,
decryption, and restore-secret files therefore use the host-visible, ephemeral
systemd runtime directory `/run/pandapages-postgresql-backup`, not `/tmp` or
`/var/tmp`. This is required because the host Docker daemon resolves bind-mount
sources outside the service's private temporary-file namespace. The restore
wrapper also overrides `TMPDIR` for the rehearsal so its generated password
file remains in that protected runtime directory.

### Status and freshness

```bash
sudo systemctl list-timers 'pandapages-postgresql-*'
sudo systemctl status pandapages-postgresql-backup.timer
sudo systemctl status pandapages-postgresql-restore-verify.timer
sudo systemctl status pandapages-postgresql-backup-healthcheck.timer
sudo cat /var/lib/pandapages-postgresql-backup/last-backup-success
sudo cat /var/lib/pandapages-postgresql-backup/last-restore-success
sudo /opt/pandapages/scripts/postgresql-backup-healthcheck.sh
sudo tail -n 100 /var/log/pandapages-postgresql-backup/operations.log
```

The local backup stamp is written only after remote ciphertext was downloaded
again and matched by byte count and SHA-256, sidecars were uploaded, a
completion marker was written, and retention succeeded. The restore stamp is
written only after validation and disposable cleanup succeed.

The twice-daily health unit fails if backup state is older than 36 hours,
restore state is older than 9 days, a stamp is missing/malformed, or a timestamp
is unexpectedly in the future. All service output is appended to the
root-readable, mode-`0700` log directory at
`/var/log/pandapages-postgresql-backup`; systemd also retains each unit's exit
status. The generic `OnFailure` service runs for every failure.

If the optional curl config is present, the notifier posts only this shape:

```json
{"event":"pandapages_postgresql_backup_failure","unit":"...","occurred_at_utc":"..."}
```

The private URL and authorization header stay in the curl config rather than
process arguments. If no existing monitoring webhook is approved, the notifier
records `notification_hook=not_configured` in the root-only operations log.
External alert delivery is then a known deployment blocker, not silently
implied by this PR.

## Full recovery procedure

1. Declare an incident, assign an operator, freeze application writes if the
   old system is still reachable, and preserve all old containers/volumes.
2. Obtain the newest completed backup set, its checksum, and completion marker
   using the approved off-host recovery credential.
3. Verify ciphertext byte count and SHA-256 before decryption.
4. Retrieve the age identity from the independent key store and decrypt into a
   mode-`0700` temporary directory on a trusted recovery host.
5. Verify the fixed archive member list and both inner SHA-256 files.
6. Review `metadata.env`, aggregate expectations, and
   `globals-no-passwords.sql` without printing application data.
7. Restore the database into a new empty PostgreSQL 18 cluster and new volume
   using the exact procedure in
   [postgresql-backup-restore.md](postgresql-backup-restore.md).
8. Restore reviewed roles/global grants first where required. The globals file
   deliberately has no role password verifiers; set new passwords from the
   approved secret source after role creation. Do not blindly restore built-in
   roles or obsolete superuser privileges.
9. Validate schemas, tables, extensions, row counts, sequences, foreign keys,
   indexes, UTF-8 integrity, assets outside PostgreSQL, API health, public
   reads, and authenticated admin reads.
10. Switch the application connection only after validation. Never permit old
    and new databases to accept writes concurrently.
11. Follow the rollback boundary in the manual runbook: direct rollback is
    safe only before the new database accepts writes unless later writes can be
    replayed or reconciled.
12. Securely remove plaintext artifacts and temporary credentials; preserve
    encrypted evidence and incident logs according to policy.

## Failure and disaster scenarios

### Backup or upload failure

- The job exits non-zero, does not advance the last-success stamp, attempts to
  remove its uniquely named partial remote set, and removes local plaintext.
- Inspect the root-only operations log and unit status without enabling shell
  tracing.
- Correct the database, network, provider, capacity, or credential fault and
  run an on-demand backup.
- Do not delete the last known-good remote object to make pruning pass.

### Checksum, decryption, or restore failure

- Treat the selected set as unverified; do not use it for cutover.
- Preserve encrypted evidence, logs, and the completion marker, but remove
  plaintext and disposable Docker resources.
- Test an earlier completed backup and investigate provider corruption, key
  mismatch, extension drift, image drift, or schema incompatibility.
- A missing required extension or metadata mismatch is a hard failure.

### Provider outage

- The daily job fails and the local success timestamp becomes stale; the
  existing remote recovery points remain the source of truth.
- Do not redirect the only backup to local VPS storage as a workaround.
- If the approved outage plan provides a second independent provider, use a
  separately reviewed remote and retain both histories. This repository does
  not configure one automatically.

### VPS loss or ransomware

- Rebuild a clean host from reviewed repository/deployment definitions.
- Retrieve immutable/versioned encrypted objects using a recovery credential
  not stored solely on the lost VPS.
- Recover the age identity from its independent offline copy.
- Restore into a new volume; never attach a suspect old volume as the target.
- Rotate database, object-store, application, session, admin, SSH, and
  notification credentials after containment.
- Review bucket audit logs and recover an earlier immutable version if the VPS
  credential deleted or replaced current objects.

### Encryption-key loss

If every matching private age identity is lost, encrypted backups are
irrecoverable even when checksums and objects remain healthy. Stop rotation or
pruning when key custody is uncertain, locate the independently escrowed key,
and prove it against a disposable restore before resuming normal retention.

## Rollback of the automation deployment

Disabling this automation must not touch PostgreSQL or remote recovery points:

1. Stop and disable only the three systemd timers.
2. Allow any active one-shot service to finish, or investigate it before a
   controlled stop; never remove its temporary directory manually while it is
   running.
3. Preserve remote objects, age identities, rclone configuration, state stamps,
   and journals until a replacement backup system has completed a verified
   restore.
4. Remove unit/configuration files only after the replacement is approved.
5. Do not remove the backup database role, bucket, keys, or remote history as
   part of timer rollback.

This automation improves logical recovery but does not protect assets stored in
the separate `assets` volume. A future reviewed task must establish independent
asset backup and consistency requirements.
