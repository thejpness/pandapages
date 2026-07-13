#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/postgresql-readonly-audit.sh --container NAME --database NAME --user NAME

Collects PostgreSQL, Docker image, mount, and aggregate database metadata.
The SQL session is forced into default_transaction_read_only mode. The script
does not print container environment values other than PGDATA and never emits
application row content.
EOF
}

container=""
database=""
database_user=""

while (($# > 0)); do
  case "$1" in
    --container)
      container=${2:?missing value for --container}
      shift 2
      ;;
    --database)
      database=${2:?missing value for --database}
      shift 2
      ;;
    --user)
      database_user=${2:?missing value for --user}
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$container" || -z "$database" || -z "$database_user" ]]; then
  usage >&2
  exit 2
fi

for command in docker awk; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command" >&2
    exit 1
  fi
done

if [[ $(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null) != true ]]; then
  printf 'Container is not running: %s\n' "$container" >&2
  exit 1
fi

printf 'collected_at_utc\t%s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
docker inspect --format 'container_name\t{{.Name}}
image_reference\t{{.Config.Image}}
image_id\t{{.Image}}
container_created\t{{.Created}}
container_started\t{{.State.StartedAt}}
compose_project\t{{index .Config.Labels "com.docker.compose.project"}}
compose_working_directory\t{{index .Config.Labels "com.docker.compose.project.working_dir"}}
compose_files\t{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$container"

printf 'container_pgdata\t'
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$container" |
  awk -F= '$1 == "PGDATA" { print substr($0, index($0, "=") + 1); found=1 } END { if (!found) print "<unset>" }'

printf 'mount_type\tmount_name\thost_source\trequested_destination\tread_write\tpropagation\n'
docker inspect --format '{{range .Mounts}}{{printf "%s\t%s\t%s\t%s\t%t\t%s\n" .Type .Name .Source .Destination .RW .Propagation}}{{end}}' "$container"

image_id=$(docker inspect --format '{{.Image}}' "$container")
docker image inspect --format 'image_architecture\t{{.Architecture}}
image_os\t{{.Os}}
image_created\t{{.Created}}
image_repo_digests\t{{json .RepoDigests}}
image_declared_volumes\t{{json .Config.Volumes}}
image_entrypoint\t{{json .Config.Entrypoint}}
image_command\t{{json .Config.Cmd}}' "$image_id"

printf 'image_pgdata\t'
docker image inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$image_id" |
  awk -F= '$1 == "PGDATA" { print substr($0, index($0, "=") + 1); found=1 } END { if (!found) print "<unset>" }'

printf 'effective_mounts_begin\n'
docker exec "$container" sh -c 'grep " /var/lib/postgresql" /proc/self/mountinfo || true'
printf 'effective_mounts_end\n'

docker exec -i \
  -e 'PGOPTIONS=-c default_transaction_read_only=on' \
  "$container" \
  psql -X --username="$database_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --pset=pager=off --tuples-only --no-align --field-separator='|' <<'SQL'
SELECT 'SETTING', name, setting, COALESCE(unit, '')
FROM pg_settings
WHERE name IN (
  'archive_mode',
  'autovacuum',
  'config_file',
  'data_checksums',
  'data_directory',
  'default_transaction_isolation',
  'external_pid_file',
  'hba_file',
  'ident_file',
  'max_connections',
  'server_version',
  'server_version_num',
  'wal_level'
)
ORDER BY name;

SELECT 'VERSION', version();

SELECT
  'DATABASE',
  datname,
  pg_database_size(datname),
  pg_size_pretty(pg_database_size(datname)),
  pg_encoding_to_char(encoding),
  datlocprovider,
  datcollate,
  datctype,
  COALESCE(datlocale, '')
FROM pg_database
WHERE datallowconn
ORDER BY datname;

SELECT 'CLUSTER_DATABASE_TOTAL', sum(pg_database_size(datname)), pg_size_pretty(sum(pg_database_size(datname)))
FROM pg_database;

SELECT 'SCHEMA', schema_name, schema_owner
FROM information_schema.schemata
WHERE schema_name !~ '^pg_toast'
ORDER BY schema_name;

SELECT 'EXTENSION', extname, extversion, nspname
FROM pg_extension
JOIN pg_namespace ON pg_namespace.oid = extnamespace
ORDER BY extname;

SELECT 'TABLESPACE', spcname, pg_tablespace_location(oid), pg_tablespace_size(oid), pg_size_pretty(pg_tablespace_size(oid))
FROM pg_tablespace
ORDER BY spcname;

SELECT
  'RELATION',
  namespace.nspname,
  relation.relname,
  relation.relkind,
  pg_total_relation_size(relation.oid),
  pg_size_pretty(pg_total_relation_size(relation.oid)),
  COALESCE(relation.reltuples::bigint, 0)
FROM pg_class AS relation
JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
WHERE namespace.nspname NOT IN ('pg_catalog', 'information_schema')
  AND relation.relkind IN ('r', 'm')
ORDER BY pg_total_relation_size(relation.oid) DESC
LIMIT 20;

SELECT
  'INDEX',
  namespace.nspname,
  relation.relname,
  pg_relation_size(relation.oid),
  pg_size_pretty(pg_relation_size(relation.oid))
FROM pg_class AS relation
JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
WHERE namespace.nspname NOT IN ('pg_catalog', 'information_schema')
  AND relation.relkind = 'i'
ORDER BY pg_relation_size(relation.oid) DESC
LIMIT 20;

SELECT
  'SEQUENCE',
  schemaname,
  sequencename,
  data_type,
  start_value,
  min_value,
  max_value,
  increment_by,
  cycle,
  cache_size,
  COALESCE(last_value::text, '<unavailable>')
FROM pg_sequences
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schemaname, sequencename;

SELECT 'REPLICATION_SLOT', slot_name, slot_type, database, active
FROM pg_replication_slots
ORDER BY slot_name;

SELECT 'REPLICA', application_name, state, sync_state
FROM pg_stat_replication
ORDER BY application_name;

SELECT 'PUBLICATION', pubname, puballtables, pubinsert, pubupdate, pubdelete, pubtruncate
FROM pg_publication
ORDER BY pubname;

SELECT 'SUBSCRIPTION', subname, subenabled, subslotname, subpublications
FROM pg_subscription
ORDER BY subname;

SELECT
  'CONNECTIONS',
  count(*),
  count(*) FILTER (WHERE state = 'active'),
  count(*) FILTER (WHERE backend_type = 'client backend')
FROM pg_stat_activity;

SELECT format(
  'SELECT ''ROWCOUNT'', %L, count(*) FROM %I.%I;',
  schemaname || '.' || tablename,
  schemaname,
  tablename
)
FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schemaname, tablename
\gexec
SQL
