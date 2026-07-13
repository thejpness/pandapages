# PostgreSQL 18 production storage and upgrade-readiness audit

Audit date: 2026-07-13 UTC
Repository base: `d1ab76dd7eef893ab055361292311af990570678`
Scope: evidence-only inspection, logical backup, and isolated restore rehearsal

## Executive finding

The production cluster is stored in the named Docker volume
`pandapages_pgdata`. Its physical host path is:

```text
/var/lib/docker/volumes/pandapages_pgdata/_data/18/docker
```

PostgreSQL reports `/var/lib/postgresql/18/docker` as `data_directory`. That is
the PostgreSQL 18 official image default; it is not an application migration or
an explicit Panda Pages `PGDATA` setting.

The tracked and deployed Compose files still request this mount:

```text
pandapages_pgdata -> /var/lib/postgresql/data
```

The PostgreSQL 18 image declares `/var/lib/postgresql` as its volume and has a
compatibility symlink `/var/lib/postgresql/data -> .`. On this running Docker
29.5.3 container, the requested destination resolves to the parent directory.
Kernel mount information shows the Compose named volume layered at the
effective `/var/lib/postgresql` path above the image-created anonymous volume.

```text
official image VOLUME /var/lib/postgresql
  lower mount: anonymous volume 090ae7... (image compatibility files only)
  upper mount: pandapages_pgdata             (effective, authoritative)
    data -> .
    18/docker                                (effective PGDATA and live cluster)
```

A network-disabled read-only inspection of each volume established that:

- `pandapages_pgdata` contains the 65.3 MiB cluster under `18/docker`;
- the anonymous volume contains only the `data -> .` symlink and uses 4 KiB;
- only `pandapages-postgres-1` mounts either volume;
- no duplicate cluster and no orphaned Panda Pages database volume was found.

The current cluster is therefore persisted in the intended named volume. The
tracked destination is nevertheless not the PostgreSQL 18 supported mount
target. The official image documentation says PostgreSQL 18 and later volumes
should target `/var/lib/postgresql`. Do not infer future recreation safety from
the current mount layering. Correcting the destination must be a separately
planned migration with a verified backup; this audit deliberately does not
change Compose or recreate PostgreSQL.

## Safety boundary

The audit did not:

- stop, restart, recreate, or reconfigure PostgreSQL;
- change the image, `PGDATA`, Compose file, `.env`, or a Docker volume;
- run SQL that changes schema or application data;
- copy the physical cluster;
- read table contents or story bodies;
- run maintenance or migration commands against production.

SQL inventory sessions used aggregate metadata queries. The reusable audit
script also forces `default_transaction_read_only=on`. Volume inspectors were
temporary, network-disabled, and mounted both source volumes read-only. They
were removed immediately.

## Evidence layers

| Evidence layer | Observation |
| --- | --- |
| Repository configuration | `docker-compose.yml` at the audit base uses `postgres:18.1-alpine` and `pgdata:/var/lib/postgresql/data`. |
| Deployed Compose | `/opt/pandapages/docker-compose.yml` uses the same PostgreSQL image and storage stanza. Its only difference from current repository Compose is the already-known web upstream port (`80` deployed versus `8080` tracked). |
| Deployed checkout | `/opt/pandapages` is at `e0cf4eb8fd3efb36b9c614f19f48e5199a934c6e` with three manually modified files. The database stanza nevertheless matches the current repository. |
| Running container | `pandapages-postgres-1` uses `postgres:18.1-alpine`; Docker image ID and repo digest both resolve to `sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545`. |
| PostgreSQL runtime | `SHOW data_directory` returns `/var/lib/postgresql/18/docker`; server and client are 18.1. |
| Docker volume metadata | `pandapages_pgdata` is a local named volume. Docker reports the requested destination as `/var/lib/postgresql/data`, read/write, with no explicit propagation value. |
| Kernel mount state | `/proc/self/mountinfo` shows `pandapages_pgdata` at the effective `/var/lib/postgresql` mount above the anonymous image volume. Both are ext4-backed on `/dev/sda1`. |
| Host filesystem | The live files are at `/var/lib/docker/volumes/pandapages_pgdata/_data/18/docker`. |
| Measured recovery evidence | A PostgreSQL 18.1 custom-format dump restored into a local isolated PostgreSQL 18.1 container with exact row and object validation. |

## Exact image and runtime

| Property | Production evidence |
| --- | --- |
| Container | `pandapages-postgres-1` |
| Configured image reference | `postgres:18.1-alpine` |
| Image ID | `sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545` |
| Immutable repo digest | `postgres@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545` |
| Image architecture | `linux/amd64` |
| VPS architecture | `x86_64` |
| Container created | `2025-12-25T01:17:14.065282729Z` |
| Current start | `2026-06-14T06:54:13.525376906Z` |
| Server | PostgreSQL 18.1, x86_64 musl build |
| Client | `psql (PostgreSQL) 18.1` |
| Docker Engine | 29.5.3 |
| Docker Compose | 5.1.4 |

The running PostgreSQL image matches both tracked and deployed Compose. The
audit did not pull or run a different image against the production volume.

## Effective `PGDATA` and configuration paths

| Setting | Runtime value |
| --- | --- |
| Container `PGDATA` | `/var/lib/postgresql/18/docker` |
| Image `PGDATA` | `/var/lib/postgresql/18/docker` |
| `data_directory` | `/var/lib/postgresql/18/docker` |
| `config_file` | `/var/lib/postgresql/18/docker/postgresql.conf` |
| `hba_file` | `/var/lib/postgresql/18/docker/pg_hba.conf` |
| `ident_file` | `/var/lib/postgresql/18/docker/pg_ident.conf` |
| `external_pid_file` | empty |
| Process command | `postgres` |

The source is the official image default. The image declares
`VOLUME /var/lib/postgresql`; Compose does not set `PGDATA`. The official
[PostgreSQL image documentation](https://github.com/docker-library/docs/blob/master/postgres/README.md#pgdata)
records this version-specific layout as the PostgreSQL 18 change intended to
make major-version directories and `pg_upgrade` coexist below one mounted
parent.

## Docker volumes and physical storage

### Authoritative named volume

| Property | Value |
| --- | --- |
| Name | `pandapages_pgdata` |
| Type | Named volume |
| Driver / scope | `local` / `local` |
| Created | `2025-12-25T00:55:06Z` |
| Host mountpoint | `/var/lib/docker/volumes/pandapages_pgdata/_data` |
| Compose-requested destination | `/var/lib/postgresql/data` |
| Kernel-effective destination | `/var/lib/postgresql` |
| Mode | Read/write |
| Labels | Compose project `pandapages`, volume `pgdata`, Compose 2.40.3 creation metadata |
| Other attached containers | None |

### Image-created lower volume

| Property | Value |
| --- | --- |
| Name | `090ae76fe658e060eed6e371ba73f78d7aa191dabaa9f7e288758378546b89ff` |
| Type | Anonymous local volume |
| Created | `2025-12-25T01:17:14Z` |
| Host mountpoint | `/var/lib/docker/volumes/090ae76fe658e060eed6e371ba73f78d7aa191dabaa9f7e288758378546b89ff/_data` |
| Container destination | `/var/lib/postgresql` (lower mount) |
| Content | Only `data -> .`; no `18/docker`, `PG_VERSION`, `base`, or WAL |
| Size | 4 KiB |
| Other attached containers | None |

This anonymous volume is attached, not orphaned. It is hidden beneath the
named-volume mount and is not authoritative. It must not be mistaken for a
backup.

### Cluster-file metadata

The audit inspected metadata only, not raw relation files.

| Item | Owner/group inside container | Mode | Notes |
| --- | --- | --- | --- |
| `18/docker` | UID 70 / GID 0 | `0700` | Active cluster root |
| `PG_VERSION` | UID 70 / GID 70 | `0600` | Contains `18`; 3 bytes |
| `base/` | UID 70 / GID 70 | `0700` | Same device as cluster |
| `global/` | UID 70 / GID 70 | `0700` | Same device as cluster |
| `pg_wal/` | UID 70 / GID 70 | `0700` | Same device as cluster and data |
| `postgresql.auto.conf` | UID 70 / GID 70 | `0600` | 88 bytes; contents were not read |
| `pg_tblspc/` | UID 70 / GID 70 | `0700` | Empty; no external tablespace links |

Storage at audit time:

- filesystem: ext4 on `/dev/sda1`;
- capacity: 300.2 GiB;
- used: 237.9 GiB (83%);
- available: 50.0 GiB;
- inodes: 18.6 million total, 5.7 million used, 12.9 million free (31% used);
- physical cluster size: 65.3 MiB;
- WAL is on the same filesystem and device as the main cluster;
- no user-defined tablespaces exist outside the volume.

The 83% filesystem utilisation is not an immediate capacity block for this
small database, but it needs monitoring because other services share the VPS.

## Database inventory

### Cluster and database settings

| Property | Value |
| --- | --- |
| Application database | `pandapages` |
| Application database size | 10,622,655 bytes (about 10 MiB) |
| Sum of all database sizes | 34,121,292 bytes (about 33 MiB) |
| Encoding | UTF-8 |
| Locale provider | libc |
| Collation / character type | `en_US.utf8` / `en_US.utf8` |
| Data checksums | on |
| WAL level | `replica` |
| Archive mode | off |
| Autovacuum | on |
| Default isolation | read committed |
| Maximum connections | 100 |
| Sample activity | 9 `pg_stat_activity` rows, 1 client backend, 1 active query (the audit query) |

Databases accepting connections were `pandapages` (10,622,655 bytes),
`postgres` (7,861,951 bytes), and `template1` (7,935,679 bytes). `template0`
is included in the cluster total but does not accept normal connections.

The only non-system application schema is `public`. Installed extensions are:

- `pgcrypto` 1.4 in `public`;
- `plpgsql` 1.0 in `pg_catalog`.

There are no replication slots, active replicas, publications, or
subscriptions. Archive mode is off, so there is no WAL archive or point-in-time
recovery chain.

The only non-system login role is `pandapages`. It is currently a superuser
with create-role, create-database, replication, and login capabilities. That is
the official image's bootstrap-role behavior, not a restore requirement. A
future least-privilege role split is recommended, but is outside this audit.

### Application relations

The largest application relations at audit time were:

| Relation | Total size | Exact rows |
| --- | ---: | ---: |
| `story_segments` | 1,000 KiB | 958 |
| `story_versions` | 408 KiB | 6 |
| `stories` | 112 KiB | 6 |
| `story_contributors` | 64 KiB | 3 |
| `story_sections` | 64 KiB | 4 |
| `contributors` | 64 KiB | 3 |
| `generation_jobs` | 64 KiB | 1 |
| `profiles` | 64 KiB | 1 |
| `reading_progress` | 64 KiB | 3 |

The remaining exact row counts were: accounts 1, assets 0, child profiles 2,
Goose migration rows 13, profile settings 1, prompt profiles 2, schema metadata
0, and works 3. The sole sequence is `goose_db_version_id_seq`, with last value
13.

## Backup posture

No reliable scheduled Panda Pages database backup could be established.

Evidence checked:

- repository and deployed filenames/scripts;
- the deployment checkout and Docker containers;
- the `jp` crontab, root-crontab match counts, `/etc/cron*`, and systemd timers;
- PostgreSQL archive and replication state;
- local Docker volumes and backup-like filenames under the deployment,
  operator home, and `/var/backups`.

No Panda Pages `pg_dump`, `pg_dumpall`, base-backup, Docker-volume backup,
snapshot job, retention job, restore-verification job, or monitoring job was
found. The only matching scheduled host task was Ubuntu's package database
backup, which is unrelated. No backup files were found in the inspected paths.

VPS-provider snapshots, account-level backup products, and any manually stored
off-host copies could not be verified from repository or host access. They must
be treated as absent until independently evidenced. Even if enabled, a VPS
snapshot alone is not a PostgreSQL recovery procedure: application consistency,
retention, off-host isolation, and restore testing still need proof.

Consequences at audit time:

- established RPO: unbounded;
- established RTO: undefined;
- encryption, retention, off-host replication, monitoring, and last-success
  evidence: unverified;
- point-in-time recovery: unavailable because `archive_mode=off` and there is
  no evidenced physical backup/WAL chain.

The one-off rehearsal dump was deliberately not retained as an operational
backup. It proves the method, not a recurring backup posture. Establishing an
encrypted, monitored, off-host schedule with periodic restore tests is the
highest-priority follow-up.

## Upgrade and storage-correction options

The current server is already PostgreSQL 18. The comparison applies both to a
future PostgreSQL major upgrade and to the controlled storage-layout correction
that should accompany a PostgreSQL 18 patch-image refresh.

| Option | Prerequisites and compatibility | Planned downtime | Disk requirement | Rollback confidence | Panda Pages suitability / risk |
| --- | --- | --- | --- | --- | --- |
| Logical dump and restore into a new volume | Compatible target server and extensions; roles/globals handled separately; use a target-version `pg_dump` when crossing majors | Write freeze for final dump plus restore, startup, and validation | Old cluster + new cluster + dump; comfortably under 1 GiB here, excluding image layers | High if old container/volume remain stopped and untouched | Best fit for a 10 MiB database; simple, portable, and fully rehearsed. Low risk. |
| Blue/green new volume using logical restore | Same as logical restore, plus parallel isolated target and explicit connection-string cutover | Same data-copy freeze; application switch can be rehearsed separately | Two clusters and dump, plus temporary image/WAL headroom | Highest: old stack remains an intact fallback | Recommended future path. Makes the correct `/var/lib/postgresql` mount explicit without mutating the old volume. Low-to-moderate operational risk. |
| `pg_upgrade` | Both old/new binaries, compatible architecture/initdb settings, extension binaries, accurate old/new paths, and `pg_upgrade --check`; both clusters stopped for upgrade | Usually short for this dataset, but all database services are unavailable while both clusters are stopped | Copy mode needs about two clusters; link/clone needs less but changes rollback properties | Copy/clone can be strong; link mode becomes unsafe to roll back after the new cluster writes | Technically viable but disproportionate for 65 MiB. More mount and binary orchestration than logical restore. Moderate risk. |
| Logical replication | Pre-created compatible schema/extensions; replica identity for updated/deleted tables; separate handling of roles, DDL, sequences, and cutover writes | Potentially brief final catch-up/write freeze | Two full clusters plus retained WAL while lag exists | Moderate; post-cutover writes require reverse replication or reconciliation | Can minimise downtime, but operational complexity is not justified for this workload. High complexity. |
| In-place image tag change while reusing the volume | Safe only for a PostgreSQL 18 minor/patch image whose on-disk format is compatible; verified backup and current storage path still required | Container restart and health checks | Little extra disk beyond image and backup | Low if the container recreation or mount interpretation is wrong | Never use for a major upgrade. Do not use for the storage correction until the named volume's new mount has been rehearsed. Moderate-to-high risk. |
| VPS snapshot-assisted rollback | Provider snapshot capability, consistency plan, sufficient snapshot window, and an independent logical/physical backup | Snapshot/restore and VPS restart time; provider-dependent | Provider snapshot allocation plus database backup | Useful as an additional whole-host rollback point, not as the sole database backup | Defence in depth only. Snapshot existence and restore time remain unverified. |

Official references:

- [PostgreSQL logical backup and restore](https://www.postgresql.org/docs/18/backup-dump.html)
- [`pg_restore`](https://www.postgresql.org/docs/18/app-pgrestore.html)
- [`pg_upgrade`](https://www.postgresql.org/docs/18/pgupgrade.html)
- [Logical-replication restrictions](https://www.postgresql.org/docs/18/logical-replication-restrictions.html)
- [Continuous archiving and PITR](https://www.postgresql.org/docs/18/continuous-archiving.html)

## Recommended future migration

Use a blue/green logical dump-and-restore migration into a new named volume
mounted explicitly at `/var/lib/postgresql`.

Why:

- the application database is only about 10 MiB;
- the exact dump and restore path has been rehearsed successfully;
- `pgcrypto` is bundled and restored successfully in the target image;
- there are no external tablespaces, replicas, slots, publications, or
  subscriptions to coordinate;
- a new volume avoids changing or reinterpreting the only current copy;
- the old container and named volume can remain untouched for rollback.

Prerequisites before authorising that migration:

1. Establish a recurring encrypted off-host backup and a second successful
   restore test.
2. Verify a provider snapshot/rollback option, if it is to be part of the plan.
3. Choose and pin the target PostgreSQL 18 patch image and digest.
4. Confirm at least 1 GiB free for database artifacts, plus image-layer
   headroom. The current 50 GiB free satisfies the database portion, but the
   shared 83%-used filesystem still warrants monitoring.
5. Rehearse the exact target Compose mount and health checks with generated or
   restored data, never the live volume.
6. Define an application write freeze and an operator-approved cutover window.
7. Decide whether to preserve the current superuser role behavior or split
   owner/migrator/application roles in a separate reviewed change.

## Rollback for the recommended path

1. Announce and enforce an application write freeze before the final dump.
2. Record the final dump checksum and preserve the old container definition,
   image digest, `.env` permissions, and `pandapages_pgdata` volume unchanged.
3. Restore and validate the new database while it remains isolated from the
   production application.
4. Stop the old application/API before changing its connection target. Never
   allow old and new databases to receive application writes concurrently.
5. Switch only the database connection target, start the application, then run
   health, public-library, admin-authentication, row-count, UTF-8, and publish
   smoke checks.
6. Roll back immediately for failed health checks, missing objects, row-count
   mismatch, authentication failure, elevated errors, or unacceptable latency.
7. To roll back before accepting new writes, stop the new application, restore
   the old connection target, and start the old application/container against
   its preserved old volume.
8. Once writes occur on the new database, direct rollback would discard them.
   The safe rollback window therefore closes at the first accepted post-cutover
   write unless those writes are frozen, replayed from a durable queue, or
   explicitly reconciled. Do not start both writers as a shortcut.
9. Keep the old container stopped and the old volume intact until a defined
   observation window and a fresh post-cutover backup have both completed.

No DNS or Traefik change should be necessary if the API service name and public
route remain unchanged; the cutover is internal to the database connection.

## Measured downtime envelope

The rehearsal measurements are detailed in
[postgresql-backup-restore.md](postgresql-backup-restore.md). The active
mechanical work was small: 1.823 seconds for the database dump, 2.141 seconds
for disposable PostgreSQL startup, 0.163 seconds for restore, and 0.248 seconds
for validation. These are not a production downtime promise.

Practical change-window estimates include operator steps, a quiesced final
dump, container health, application startup, and smoke validation:

| Option | Planned write freeze | Database unavailable | Application unavailable | Rollback estimate |
| --- | ---: | ---: | ---: | ---: |
| Blue/green logical restore (recommended) | 3-10 minutes | Old database can remain available read-only; target preparation is parallel | 2-5 minutes for connection switch, startup, and smoke checks | 2-5 minutes before new writes |
| Offline logical dump/restore | 5-15 minutes | 3-10 minutes | 5-15 minutes | 5-10 minutes if old stack is preserved |
| `pg_upgrade` copy/clone | 10-30 minutes | 5-20 minutes including checks and post-processing | 10-30 minutes | 10-30 minutes; mode-dependent |
| Logical replication | Final freeze about 1-5 minutes after initial sync | Target preparation remains online | About 1-5 minutes for catch-up and switch | 5-15 minutes before new writes; complex afterwards |
| In-place PostgreSQL 18 patch image | 2-10 minutes | Container restart plus health, about 1-5 minutes if clean | About 2-10 minutes | 5-15 minutes, longer if mount behavior surprises |
| VPS snapshot rollback | Provider-dependent | Provider-dependent | Commonly tens of minutes, unmeasured here | Unverified |

The recommended plan does not claim zero downtime. Panda Pages is small enough
that a short, explicit write freeze is safer than introducing replication
complexity solely to remove a few minutes of planned interruption.

## Remaining risks and follow-ups

1. No verified recurring backup, retention, monitoring, or PITR exists.
2. Compose still uses the pre-18 destination rather than the supported parent
   mount. It currently resolves to the named volume, but must be corrected only
   through a separately authorised, rehearsed migration.
3. PostgreSQL runs the application/bootstrap role as a cluster superuser.
4. The shared filesystem is 83% used; capacity alerts and backup headroom are
   not evidenced.
5. Provider snapshots and off-host backup products remain unverified.
6. The production checkout is old and manually modified even though its
   database stanza matches the repository; deployment provenance should be
   made reproducible.
7. The VPS reports `NTP=yes` but `NTPSynchronized=no`, and its clock was about
   47 seconds ahead of the audit workstation. Fix time synchronisation before
   relying on cross-host log or recovery timestamps. The durable diagnosis,
   repair, and rollout gate are documented in
   [production-time-synchronisation.md](production-time-synchronisation.md).

## Reproduction tooling

- `scripts/postgresql-readonly-audit.sh` emits only Docker metadata and
  aggregate read-only SQL evidence. It requires explicit container, database,
  and user arguments.
- `scripts/postgresql-restore-rehearsal.sh` accepts a custom-format dump and
  creates only uniquely named disposable resources. It refuses a dump inside
  Docker volume storage, refuses non-local Docker contexts, publishes no port,
  and always removes its container, internal network, generated credentials,
  and volume.

Neither script contains a production hostname, volume name, password,
connection string, public IP address, or upgrade operation.
