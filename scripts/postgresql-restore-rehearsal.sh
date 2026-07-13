#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly default_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly target_database='pandapages_restore_rehearsal'
readonly target_user='rehearsal_admin'

usage() {
  cat <<'EOF'
Usage:
  scripts/postgresql-restore-rehearsal.sh \
    --dump /private/path/database.dump \
    --report-dir /private/path/rehearsal-report \
    [--expected-row-counts /private/path/row-counts.tsv] \
    [--expected-metadata-dir /private/path/expectations] \
    [--image postgres:18.1-alpine@sha256:...] \
    [--dry-run]

Restores a custom-format PostgreSQL dump into newly created disposable Docker
resources. It never publishes a port, never accepts an existing volume, and
always removes its container, internal network, temporary credentials, and
volume. The report directory must be outside a Git worktree.

Expected row counts use one sorted record per line:
  public.stories|6

The optional metadata directory must contain schemas.tsv, tables.tsv,
extensions.tsv, sequences.tsv, and structural-summary.tsv as emitted by
postgresql-backup.sh.
EOF
}

dump_path=""
report_dir=""
expected_row_counts=""
expected_metadata_dir=""
image=$default_image
dry_run=false

while (($# > 0)); do
  case "$1" in
    --dump)
      dump_path=${2:?missing value for --dump}
      shift 2
      ;;
    --report-dir)
      report_dir=${2:?missing value for --report-dir}
      shift 2
      ;;
    --expected-row-counts)
      expected_row_counts=${2:?missing value for --expected-row-counts}
      shift 2
      ;;
    --expected-metadata-dir)
      expected_metadata_dir=${2:?missing value for --expected-metadata-dir}
      shift 2
      ;;
    --image)
      image=${2:?missing value for --image}
      shift 2
      ;;
    --dry-run)
      dry_run=true
      shift
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

if [[ -z "$dump_path" || -z "$report_dir" ]]; then
  usage >&2
  exit 2
fi

for command in awk chmod date diff docker git grep mkdir mktemp od realpath rm rmdir sha256sum sleep sort tr; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'Required command is unavailable: %s\n' "$command" >&2
    exit 1
  fi
done

dump_path=$(realpath -e "$dump_path")
if [[ ! -f "$dump_path" ]]; then
  printf 'Dump is not a regular file: %s\n' "$dump_path" >&2
  exit 1
fi

case "$dump_path" in
  /var/lib/docker/volumes/*)
    printf 'Refusing a dump path inside Docker volume storage: %s\n' "$dump_path" >&2
    exit 1
    ;;
esac

if [[ -n "$expected_row_counts" ]]; then
  expected_row_counts=$(realpath -e "$expected_row_counts")
  if [[ ! -s "$expected_row_counts" ]]; then
    printf 'Expected row-count manifest is not a regular file: %s\n' "$expected_row_counts" >&2
    exit 1
  fi
  if grep -Ev '^[A-Za-z0-9_.-]+\|[0-9]+$' "$expected_row_counts" | grep -q .; then
    printf 'Expected row-count manifest has an invalid record\n' >&2
    exit 1
  fi
fi

if [[ -n "$expected_metadata_dir" ]]; then
  expected_metadata_dir=$(realpath -e "$expected_metadata_dir")
  if [[ ! -d "$expected_metadata_dir" || -L "$expected_metadata_dir" ]]; then
    printf 'Expected metadata path is not a non-symlink directory: %s\n' "$expected_metadata_dir" >&2
    exit 1
  fi
  for expectation_file in schemas.tsv tables.tsv extensions.tsv sequences.tsv structural-summary.tsv; do
    if [[ ! -f "$expected_metadata_dir/$expectation_file" || -L "$expected_metadata_dir/$expectation_file" ]]; then
      printf 'Expected metadata file is missing or unsafe: %s\n' "$expectation_file" >&2
      exit 1
    fi
  done
fi

mkdir -p "$report_dir"
report_dir=$(realpath -e "$report_dir")
chmod 0700 "$report_dir"

if git -C "$report_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'Refusing to write rehearsal output inside a Git worktree: %s\n' "$report_dir" >&2
  exit 1
fi

if [[ -n ${DOCKER_HOST:-} ]]; then
  printf 'Refusing to run while DOCKER_HOST is set; select a known local Docker engine explicitly\n' >&2
  exit 1
fi

docker_context=$(docker context show)
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]]; then
  printf 'Refusing non-local Docker context without script review: %s\n' "$docker_context" >&2
  exit 1
fi
docker_endpoint=$(docker context inspect "$docker_context" --format "{{.Endpoints.docker.Host}}")
if [[ "$docker_endpoint" != unix://* ]]; then
  printf "Refusing Docker endpoint that is not a local Unix socket: %s\n" "$docker_endpoint" >&2
  exit 1
fi

readonly resource_prefix="pandapages-pg-restore-rehearsal-$$"
readonly container_name=$resource_prefix
readonly network_name="${resource_prefix}-network"
readonly volume_name="${resource_prefix}-data"

if $dry_run; then
  printf 'image=%s\n' "$image"
  printf 'dump=%s\n' "$dump_path"
  printf 'report_dir=%s\n' "$report_dir"
  printf 'container=%s\n' "$container_name"
  printf 'network=%s (internal, no published ports)\n' "$network_name"
  printf 'volume=%s (new and disposable)\n' "$volume_name"
  exit 0
fi

for resource in "$container_name" "$network_name" "$volume_name"; do
  if docker container inspect "$resource" >/dev/null 2>&1 ||
    docker network inspect "$resource" >/dev/null 2>&1 ||
    docker volume inspect "$resource" >/dev/null 2>&1; then
    printf 'Refusing to reuse an existing Docker resource: %s\n' "$resource" >&2
    exit 1
  fi
done

secret_dir=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-pg-restore-secret.XXXXXX")
chmod 0700 "$secret_dir"
password_file="$secret_dir/postgres-password"
od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"$password_file"
chmod 0600 "$password_file"

container_created=false
network_created=false
volume_created=false
cleanup_status=not_started
summary_file="$report_dir/summary.txt"
: >"$summary_file"
chmod 0600 "$summary_file"

cleanup() {
  set +e
  local failed=0

  if $container_created; then
    docker rm --force "$container_name" >/dev/null 2>&1 || failed=1
    container_created=false
  fi
  if $network_created; then
    docker network rm "$network_name" >/dev/null 2>&1 || failed=1
    network_created=false
  fi
  if $volume_created; then
    docker volume rm "$volume_name" >/dev/null 2>&1 || failed=1
    volume_created=false
  fi

  rm -f "$password_file"
  rmdir "$secret_dir" >/dev/null 2>&1 || failed=1

  if docker container inspect "$container_name" >/dev/null 2>&1 ||
    docker network inspect "$network_name" >/dev/null 2>&1 ||
    docker volume inspect "$volume_name" >/dev/null 2>&1 ||
    [[ -e "$secret_dir" ]]; then
    failed=1
  fi

  if ((failed == 0)); then
    cleanup_status=complete
  else
    cleanup_status=failed
  fi
  set -e
}

on_exit() {
  local status=$?
  trap - EXIT
  cleanup
  printf 'cleanup=%s\n' "$cleanup_status" >>"$summary_file"
  if ((status != 0)); then
    printf 'result=failed\n' >>"$summary_file"
  fi
  exit "$status"
}

trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

started_at=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
total_started_ns=$(date +%s%N)

docker network create \
  --internal \
  --label com.pandapages.disposable=restore-rehearsal \
  "$network_name" >/dev/null
network_created=true

docker volume create \
  --label com.pandapages.disposable=restore-rehearsal \
  "$volume_name" >/dev/null
volume_created=true

startup_started_ns=$(date +%s%N)
docker run --detach \
  --name "$container_name" \
  --network "$network_name" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=restore-rehearsal \
  --env POSTGRES_USER="$target_user" \
  --env POSTGRES_DB=postgres \
  --env POSTGRES_PASSWORD_FILE=/run/secrets/postgres-password \
  --env POSTGRES_INITDB_ARGS=--data-checksums \
  --mount "type=volume,src=$volume_name,dst=/var/lib/postgresql" \
  --mount "type=bind,src=$dump_path,dst=/backup/source.dump,readonly" \
  --mount "type=bind,src=$password_file,dst=/run/secrets/postgres-password,readonly" \
  --health-cmd "pg_isready --username=$target_user --dbname=postgres" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$image" >/dev/null
container_created=true

for ((attempt = 0; attempt < 60; attempt++)); do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_name")
  case "$health" in
    healthy)
      break
      ;;
    unhealthy)
      docker logs "$container_name" >"$report_dir/container.log" 2>&1
      printf 'Disposable PostgreSQL container became unhealthy\n' >&2
      exit 1
      ;;
  esac
  sleep 1
done

if [[ ${health:-none} != healthy ]]; then
  docker logs "$container_name" >"$report_dir/container.log" 2>&1
  printf 'Timed out waiting for disposable PostgreSQL\n' >&2
  exit 1
fi
startup_finished_ns=$(date +%s%N)

docker exec "$container_name" pg_restore --list /backup/source.dump >"$report_dir/pg_restore.list"

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname=postgres --set=ON_ERROR_STOP=1 <<SQL
CREATE DATABASE $target_database
  WITH OWNER $target_user
       TEMPLATE template0
       ENCODING 'UTF8';
SQL

restore_started_ns=$(date +%s%N)
docker exec "$container_name" \
  pg_restore \
  --username="$target_user" \
  --dbname="$target_database" \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  --exit-on-error \
  --verbose \
  /backup/source.dump \
  >"$report_dir/restore.stdout.log" \
  2>"$report_dir/restore.stderr.log"
restore_finished_ns=$(date +%s%N)

validation_started_ns=$(date +%s%N)
docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --pset=pager=off <<'SQL' >"$report_dir/analyze.log"
ANALYZE;
SQL

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --pset=pager=off --tuples-only --no-align --field-separator='|' <<'SQL' >"$report_dir/validation.tsv"
SELECT 'database', current_database();
SELECT 'server_version', current_setting('server_version');
SELECT 'encoding', pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database();
SELECT 'data_checksums', current_setting('data_checksums');
SELECT 'database_size_bytes', pg_database_size(current_database());
SELECT 'schema_count', count(*) FROM information_schema.schemata WHERE schema_name = 'public';
SELECT 'table_count', count(*) FROM pg_tables WHERE schemaname = 'public';
SELECT 'extensions', string_agg(extname || ':' || extversion, ',' ORDER BY extname) FROM pg_extension;
SELECT 'foreign_keys', count(*), bool_and(convalidated) FROM pg_constraint JOIN pg_class ON pg_class.oid = conrelid
JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
WHERE contype = 'f' AND pg_namespace.nspname = 'public';
SELECT 'indexes', count(*), bool_and(indisvalid AND indisready) FROM pg_index JOIN pg_class ON pg_class.oid = indexrelid JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace WHERE pg_namespace.nspname = 'public';
SELECT 'application_library_rows', count(*) FROM stories AS story JOIN story_versions AS version ON version.id = story.published_version_id WHERE story.published_version_id IS NOT NULL;
SELECT 'utf8_roundtrip_failures', count(*) FROM story_versions WHERE markdown <> convert_from(convert_to(markdown, 'UTF8'), 'UTF8');
SELECT 'utf8_multibyte_story_versions', count(*) FROM story_versions WHERE octet_length(markdown) > char_length(markdown);
SELECT 'sequence', schemaname, sequencename, COALESCE(last_value::text, '<unavailable>') FROM pg_sequences WHERE schemaname = 'public' ORDER BY schemaname, sequencename;
SQL

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align <<'SQL' >"$report_dir/actual-row-counts.tsv"
SELECT format(
  'SELECT %L || ''|'' || count(*)::text FROM %I.%I;',
  schemaname || '.' || tablename,
  schemaname,
  tablename
)
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY schemaname, tablename
\gexec
SQL
docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align <<'SQL' >"$report_dir/actual-schemas.tsv"
SELECT schema_name
FROM information_schema.schemata
WHERE schema_name NOT IN ('pg_catalog', 'information_schema')
  AND schema_name !~ '^pg_toast'
ORDER BY schema_name;
SQL

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align <<'SQL' >"$report_dir/actual-tables.tsv"
SELECT schemaname || '.' || tablename
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY schemaname, tablename;
SQL

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align <<'SQL' >"$report_dir/actual-extensions.tsv"
SELECT extname || '|' || extversion
FROM pg_extension
ORDER BY extname;
SQL

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align <<'SQL' >"$report_dir/actual-sequences.tsv"
SELECT schemaname || '.' || sequencename || '|' || COALESCE(last_value::text, '<unavailable>')
FROM pg_sequences
WHERE schemaname = 'public'
ORDER BY schemaname, sequencename;
SQL

docker exec -i "$container_name" \
  psql -X --username="$target_user" --dbname="$target_database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align <<'SQL' >"$report_dir/actual-structural-summary.tsv"
SELECT 'foreign_keys|' || count(*)::text || '|' || COALESCE(bool_and(convalidated), true)::text
FROM pg_constraint
JOIN pg_class ON pg_class.oid = conrelid
JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
WHERE contype = 'f' AND pg_namespace.nspname = 'public'
UNION ALL
SELECT 'indexes|' || count(*)::text || '|' || COALESCE(bool_and(indisvalid AND indisready), true)::text
FROM pg_index
JOIN pg_class ON pg_class.oid = indexrelid
JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
WHERE pg_namespace.nspname = 'public'
UNION ALL
SELECT 'utf8_multibyte_story_versions|' || count(*)::text || '|true'
FROM story_versions
WHERE octet_length(markdown) > char_length(markdown)
UNION ALL
SELECT 'utf8_roundtrip_failures|' || count(*)::text || '|true'
FROM story_versions
WHERE markdown <> convert_from(convert_to(markdown, 'UTF8'), 'UTF8')
ORDER BY 1;
SQL


LC_ALL=C sort -o "$report_dir/actual-row-counts.tsv" "$report_dir/actual-row-counts.tsv"

if [[ -n "$expected_row_counts" ]]; then
  LC_ALL=C sort "$expected_row_counts" >"$report_dir/expected-row-counts.tsv"
  diff -u \
    "$report_dir/expected-row-counts.tsv" \
    "$report_dir/actual-row-counts.tsv" \
    >"$report_dir/row-counts.diff"
fi
if [[ -n "$expected_metadata_dir" ]]; then
  for expectation_name in schemas tables extensions sequences structural-summary; do
    LC_ALL=C sort "$expected_metadata_dir/$expectation_name.tsv" \
      >"$report_dir/expected-$expectation_name.tsv"
    LC_ALL=C sort -o "$report_dir/actual-$expectation_name.tsv" \
      "$report_dir/actual-$expectation_name.tsv"
    diff -u \
      "$report_dir/expected-$expectation_name.tsv" \
      "$report_dir/actual-$expectation_name.tsv" \
      >"$report_dir/$expectation_name.diff"
  done
fi


validation_finished_ns=$(date +%s%N)
total_finished_ns=$(date +%s%N)
finished_at=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

dump_sha256=$(sha256sum "$dump_path" | awk '{print $1}')
archive_toc_entries=$(awk -F: '/^;[[:space:]]+TOC Entries:/ { gsub(/[[:space:]]/, "", $2); print $2; exit }' "$report_dir/pg_restore.list")
listed_restore_items=$(awk '!/^;/ && NF { count++ } END { print count + 0 }' "$report_dir/pg_restore.list")
restored_database_bytes=$(awk -F'|' '$1 == "database_size_bytes" { print $2 }' "$report_dir/validation.tsv")

{
  printf 'result=success\n'
  printf 'started_at_utc=%s\n' "$started_at"
  printf 'finished_at_utc=%s\n' "$finished_at"
  printf 'image=%s\n' "$image"
  printf 'dump_sha256=%s\n' "$dump_sha256"
  printf 'archive_toc_entries=%s\n' "$archive_toc_entries"
  printf 'listed_restore_items=%s\n' "$listed_restore_items"
  printf 'startup_milliseconds=%s\n' "$(((startup_finished_ns - startup_started_ns) / 1000000))"
  printf 'restore_milliseconds=%s\n' "$(((restore_finished_ns - restore_started_ns) / 1000000))"
  printf 'validation_milliseconds=%s\n' "$(((validation_finished_ns - validation_started_ns) / 1000000))"
  printf 'total_milliseconds=%s\n' "$(((total_finished_ns - total_started_ns) / 1000000))"
  printf 'restored_database_bytes=%s\n' "$restored_database_bytes"
  printf 'manual_intervention=none\n'
} >>"$summary_file"

cleanup
printf 'cleanup=%s\n' "$cleanup_status" >>"$summary_file"
if [[ "$cleanup_status" != complete ]]; then
  printf 'Disposable resource cleanup failed\n' >&2
  exit 1
fi

trap - EXIT
cat "$summary_file"
