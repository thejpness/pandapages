#!/usr/bin/env bash

set -euo pipefail
umask 077

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/lib/postgresql-backup-common.sh
source "$script_dir/lib/postgresql-backup-common.sh"

readonly backup_prefix='pandapages-postgresql'

usage() {
  cat <<'EOF'
Usage: scripts/postgresql-backup.sh [--dry-run]

Creates a PostgreSQL custom-format database dump and password-free globals
dump through the running PostgreSQL container, records aggregate restore
expectations, encrypts the bundle with age, uploads it through an explicitly
configured rclone remote, verifies the uploaded ciphertext, applies tiered
retention, and records local success state.

No database password is accepted by this script. The database dump role must
be a dedicated non-superuser member of pg_read_all_data that can authenticate
over the container's local Unix socket.
EOF
}

dry_run=false
while (($# > 0)); do
  case "$1" in
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

postgres_container=${PP_BACKUP_POSTGRES_CONTAINER:-}
database=${PP_BACKUP_DATABASE:-}
database_user=${PP_BACKUP_DATABASE_USER:-}
globals_user=${PP_BACKUP_GLOBALS_USER:-}
remote=${PP_BACKUP_REMOTE:-}
rclone_config=${PP_BACKUP_RCLONE_CONFIG:-}
age_recipients=${PP_BACKUP_AGE_RECIPIENTS_FILE:-}
state_dir=${PP_BACKUP_STATE_DIR:-/var/lib/pandapages-postgresql-backup}
lock_dir=${PP_BACKUP_LOCK_DIR:-/run/pandapages-postgresql-backup}
tmp_parent=${PP_BACKUP_TMP_PARENT:-/var/tmp}
expected_age_version=${PP_BACKUP_EXPECT_AGE_VERSION:-1.3.1}
expected_rclone_version=${PP_BACKUP_EXPECT_RCLONE_VERSION:-1.74.4}
retention_daily=${PP_BACKUP_RETENTION_DAILY:-7}
retention_weekly=${PP_BACKUP_RETENTION_WEEKLY:-4}
retention_monthly=${PP_BACKUP_RETENTION_MONTHLY:-3}
retention_script=${PP_BACKUP_RETENTION_SCRIPT:-$script_dir/postgresql-backup-retention.sh}

for pair in \
  "PP_BACKUP_POSTGRES_CONTAINER:$postgres_container" \
  "PP_BACKUP_DATABASE:$database" \
  "PP_BACKUP_DATABASE_USER:$database_user" \
  "PP_BACKUP_GLOBALS_USER:$globals_user" \
  "PP_BACKUP_REMOTE:$remote" \
  "PP_BACKUP_RCLONE_CONFIG:$rclone_config" \
  "PP_BACKUP_AGE_RECIPIENTS_FILE:$age_recipients"; do
  backup_require_value "${pair%%:*}" "${pair#*:}"
done

[[ "$postgres_container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] ||
  backup_die "invalid PostgreSQL container name"
[[ "$database" =~ ^[A-Za-z_][A-Za-z0-9_.-]*$ ]] || backup_die "invalid database name"
[[ "$database_user" =~ ^[A-Za-z_][A-Za-z0-9_.-]*$ ]] || backup_die "invalid database backup role"
[[ "$globals_user" =~ ^[A-Za-z_][A-Za-z0-9_.-]*$ ]] || backup_die "invalid globals role"
[[ "$database_user" != "$globals_user" ]] ||
  backup_die "database and globals users must differ so the data dump uses a dedicated least-privilege role"
backup_validate_remote "$remote"
backup_require_private_file "$rclone_config" "rclone config"
backup_require_private_file "$age_recipients" "age recipients file"
[[ -x "$retention_script" && -f "$retention_script" ]] ||
  backup_die "retention script is unavailable or not executable: $retention_script"

for count in "$retention_daily" "$retention_weekly" "$retention_monthly"; do
  [[ "$count" =~ ^[0-9]+$ ]] || backup_die "retention counts must be non-negative integers"
done

for command_name in age awk chmod date docker flock grep head mkdir mktemp mv realpath rclone rm sha256sum sort stat tar wc; do
  backup_require_command "$command_name"
done
backup_verify_tool_versions "$expected_age_version" "$expected_rclone_version"
backup_require_s3_remote "$remote" "$rclone_config"

tmp_parent=$(realpath -e "$tmp_parent")
[[ -d "$tmp_parent" ]] || backup_die "temporary parent is not a directory: $tmp_parent"

mkdir -p "$lock_dir"
chmod 0750 "$lock_dir"
exec 9>"$lock_dir/postgresql-backup.lock"
if ! flock -n 9; then
  backup_die "another PostgreSQL backup or restore verification is already running"
fi

if [[ $(docker inspect --format '{{.State.Running}}' "$postgres_container" 2>/dev/null) != true ]]; then
  backup_die "PostgreSQL container is not running"
fi

role_check=""
if ! role_check=$(docker exec \
  -e 'PGOPTIONS=-c default_transaction_read_only=on' \
  "$postgres_container" \
  psql -X --username="$database_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align \
  --command="SELECT current_user || '|' || rolsuper::text || '|' || rolinherit::text || '|' || pg_has_role(current_user, 'pg_read_all_data', 'MEMBER')::text FROM pg_roles WHERE rolname = current_user;" \
  2>/dev/null); then
  backup_die "dedicated database backup role could not connect"
fi

[[ "$role_check" == "$database_user|false|true|true" ]] ||
  backup_die "database backup role must be a non-superuser inheriting member of pg_read_all_data"

if $dry_run; then
  printf 'plan=validated\n'
  printf 'database_dump=pg_dump_custom\n'
  printf 'globals_dump=pg_dumpall_globals_without_passwords\n'
  printf 'encryption=age_%s\n' "$expected_age_version"
  printf 'remote_interface=rclone_%s_s3_compatible\n' "$expected_rclone_version"
  printf 'retention=daily:%s,weekly:%s,monthly:%s\n' "$retention_daily" "$retention_weekly" "$retention_monthly"
  exit 0
fi

timestamp=$(backup_now_utc)
backup_id="$backup_prefix-$timestamp"
workdir=$(mktemp -d -- "$tmp_parent/pandapages-postgresql-backup.XXXXXX")
chmod 0700 "$workdir"
bundle_dir="$workdir/bundle"
expectations_dir="$bundle_dir/expectations"
mkdir -p "$expectations_dir"
chmod 0700 "$bundle_dir" "$expectations_dir"

remote_started=false
marker_uploaded=false
snapshot_session_active=false
snapshot_input_fd=""
snapshot_output_fd=""
snapshot_pid=""

close_snapshot_session() {
  $snapshot_session_active || return 0

  printf 'ROLLBACK;\n' >&"$snapshot_input_fd" || true
  exec {snapshot_input_fd}>&-
  while IFS= read -r _ <&"$snapshot_output_fd"; do :; done
  exec {snapshot_output_fd}<&-
  snapshot_session_active=false
  wait "$snapshot_pid"
}

cleanup() {
  local status=$?
  set +e

  close_snapshot_session >/dev/null 2>&1 || true

  if ((status != 0)) && $remote_started && ! $marker_uploaded; then
    for suffix in '.tar.age' '.tar.age.sha256' '.complete'; do
      rclone deletefile --config "$rclone_config" "$remote/$backup_id$suffix" >/dev/null 2>&1 || true
    done
  fi

  if [[ -n ${workdir:-} && -d ${workdir:-} && "$workdir" == "$tmp_parent"/pandapages-postgresql-backup.* ]]; then
    rm -rf -- "$workdir"
  fi

  trap - EXIT
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

started_epoch=$(date -u +%s)

coproc SNAPSHOT_SESSION {
  docker exec -i \
    -e 'PGOPTIONS=-c default_transaction_read_only=on' \
    "$postgres_container" \
    psql -X --quiet --username="$database_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --set=backup_snapshot_holder=1 \
    --tuples-only --no-align \
    2>"$workdir/snapshot-session.stderr"
}
snapshot_pid=$SNAPSHOT_SESSION_PID
snapshot_input_fd=${SNAPSHOT_SESSION[1]}
snapshot_output_fd=${SNAPSHOT_SESSION[0]}
snapshot_session_active=true

printf '%s\n' \
  'BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;' \
  'SELECT pg_export_snapshot();' \
  >&"$snapshot_input_fd"
if ! IFS= read -r snapshot_id <&"$snapshot_output_fd"; then
  backup_die "could not export a consistent backup snapshot"
fi
[[ "$snapshot_id" =~ ^[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+$ ]] ||
  backup_die "PostgreSQL returned an invalid exported snapshot identifier"

if ! docker exec \
  -e 'PGOPTIONS=-c default_transaction_read_only=on' \
  "$postgres_container" \
  pg_dump \
  --username="$database_user" \
  --dbname="$database" \
  --format=custom \
  --compress=zstd:6 \
  --snapshot="$snapshot_id" \
  --no-owner \
  --no-acl \
  --lock-wait-timeout=30s \
  >"$bundle_dir/database.dump" \
  2>"$workdir/pg_dump.stderr"; then
  backup_die "database dump failed"
fi
[[ ! -s "$workdir/pg_dump.stderr" ]] || backup_die "database dump emitted unexpected stderr"

if ! docker exec \
  -e 'PGOPTIONS=-c default_transaction_read_only=on' \
  "$postgres_container" \
  pg_dumpall \
  --username="$globals_user" \
  --globals-only \
  --no-role-passwords \
  >"$bundle_dir/globals-no-passwords.sql" \
  2>"$workdir/pg_dumpall.stderr"; then
  backup_die "globals dump failed"
fi
[[ ! -s "$workdir/pg_dumpall.stderr" ]] || backup_die "globals dump emitted unexpected stderr"

run_query() {
  local output_path=$1
  local query=$2
  if ! docker exec \
    -e 'PGOPTIONS=-c default_transaction_read_only=on' \
    "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --quiet --tuples-only --no-align --field-separator='|' \
    --command="BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY; SET TRANSACTION SNAPSHOT '$snapshot_id'; $query ROLLBACK;" \
    >"$output_path" 2>/dev/null; then
    backup_die "aggregate restore-expectation query failed"
  fi
}

run_query "$expectations_dir/schemas.tsv" \
  "SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog', 'information_schema') AND schema_name !~ '^pg_toast' ORDER BY schema_name;"
run_query "$expectations_dir/tables.tsv" \
  "SELECT schemaname || '.' || tablename FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema') ORDER BY schemaname, tablename;"
run_query "$expectations_dir/extensions.tsv" \
  "SELECT extname || '|' || extversion FROM pg_extension ORDER BY extname;"
run_query "$expectations_dir/sequences.tsv" \
  "SELECT schemaname || '.' || sequencename || '|' || COALESCE(last_value::text, '<unavailable>') FROM pg_sequences WHERE schemaname NOT IN ('pg_catalog', 'information_schema') ORDER BY schemaname, sequencename;"
run_query "$expectations_dir/structural-summary.tsv" \
  "SELECT 'foreign_keys|' || count(*)::text || '|' || COALESCE(bool_and(convalidated), true)::text FROM pg_constraint WHERE contype = 'f' UNION ALL SELECT 'indexes|' || count(*)::text || '|' || COALESCE(bool_and(indisvalid AND indisready), true)::text FROM pg_index JOIN pg_class ON pg_class.oid = indexrelid JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace WHERE pg_namespace.nspname NOT IN ('pg_catalog', 'information_schema') UNION ALL SELECT 'utf8_multibyte_story_versions|' || count(*)::text || '|true' FROM story_versions WHERE octet_length(markdown) > char_length(markdown) UNION ALL SELECT 'utf8_roundtrip_failures|' || count(*)::text || '|true' FROM story_versions WHERE markdown <> convert_from(convert_to(markdown, 'UTF8'), 'UTF8') ORDER BY 1;"

if ! docker exec -i \
  -e 'PGOPTIONS=-c default_transaction_read_only=on' \
  "$postgres_container" \
  psql -X --username="$database_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --quiet --tuples-only --no-align <<SQL \
  >"$expectations_dir/row-counts.tsv" 2>/dev/null; then
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET TRANSACTION SNAPSHOT '$snapshot_id';
SELECT format(
  'SELECT %L || ''|'' || count(*)::text FROM %I.%I;',
  schemaname || '.' || tablename,
  schemaname,
  tablename
)
FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schemaname, tablename
\gexec
ROLLBACK;
SQL
  backup_die "aggregate row-count capture failed"
fi

LC_ALL=C sort -o "$expectations_dir/row-counts.tsv" "$expectations_dir/row-counts.tsv"

run_query "$workdir/server-version.txt" 'SHOW server_version;'
server_version=$(<"$workdir/server-version.txt")

if ! close_snapshot_session; then
  backup_die "consistent backup snapshot session ended unexpectedly"
fi
[[ ! -s "$workdir/snapshot-session.stderr" ]] ||
  backup_die "consistent backup snapshot session emitted unexpected stderr"

(
  cd "$bundle_dir"
  sha256sum database.dump >database.dump.sha256
  sha256sum globals-no-passwords.sql >globals-no-passwords.sql.sha256
)

{
  printf 'format_version=1\n'
  printf 'backup_id=%s\n' "$backup_id"
  printf 'created_at_utc=%s-%s-%sT%s:%s:%sZ\n' \
    "${timestamp:0:4}" "${timestamp:4:2}" "${timestamp:6:2}" \
    "${timestamp:9:2}" "${timestamp:11:2}" "${timestamp:13:2}"
  printf 'postgresql_server_version=%s\n' "$server_version"
  printf 'database_dump_format=custom\n'
  printf 'globals_role_passwords=omitted\n'
} >"$bundle_dir/metadata.env"

archive_path="$workdir/$backup_id.tar"
tar \
  --create \
  --file "$archive_path" \
  --directory "$bundle_dir" \
  --sort=name \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  --mtime="${timestamp:0:4}-${timestamp:4:2}-${timestamp:6:2} ${timestamp:9:2}:${timestamp:11:2}:${timestamp:13:2} UTC" \
  database.dump \
  database.dump.sha256 \
  expectations \
  globals-no-passwords.sql \
  globals-no-passwords.sql.sha256 \
  metadata.env

encrypted_path="$workdir/$backup_id.tar.age"
if ! age \
  --encrypt \
  --recipients-file "$age_recipients" \
  --output "$encrypted_path" \
  "$archive_path"; then
  backup_die "backup encryption failed"
fi

encrypted_sha256=$(sha256sum "$encrypted_path" | awk '{print $1}')
encrypted_bytes=$(stat -c '%s' "$encrypted_path")
checksum_path="$workdir/$backup_id.tar.age.sha256"
printf '%s  %s.tar.age\n' "$encrypted_sha256" "$backup_id" >"$checksum_path"

complete_path="$workdir/$backup_id.complete"
{
  printf 'format_version=1\n'
  printf 'backup_id=%s\n' "$backup_id"
  printf 'ciphertext_bytes=%s\n' "$encrypted_bytes"
  printf 'ciphertext_sha256=%s\n' "$encrypted_sha256"
} >"$complete_path"

remote_started=true
if ! rclone copyto --config "$rclone_config" --immutable --no-traverse \
  "$encrypted_path" "$remote/$backup_id.tar.age"; then
  backup_die "encrypted backup upload failed"
fi

remote_verify_path="$workdir/remote-verify.tar.age"
if ! rclone copyto --config "$rclone_config" --no-traverse \
  "$remote/$backup_id.tar.age" "$remote_verify_path"; then
  backup_die "uploaded backup could not be downloaded for verification"
fi

remote_sha256=$(sha256sum "$remote_verify_path" | awk '{print $1}')
remote_bytes=$(stat -c '%s' "$remote_verify_path")
[[ "$remote_sha256" == "$encrypted_sha256" && "$remote_bytes" == "$encrypted_bytes" ]] ||
  backup_die "uploaded ciphertext size or SHA-256 does not match"

rclone copyto --config "$rclone_config" --immutable --no-traverse \
  "$checksum_path" "$remote/$backup_id.tar.age.sha256" ||
  backup_die "ciphertext checksum upload failed"
rclone copyto --config "$rclone_config" --immutable --no-traverse \
  "$complete_path" "$remote/$backup_id.complete" ||
  backup_die "completion marker upload failed"
marker_uploaded=true

remote_markers="$workdir/remote-markers.txt"
if ! rclone lsf --config "$rclone_config" --files-only \
  --include "$backup_prefix-*.complete" "$remote" >"$remote_markers"; then
  backup_die "remote retention inventory failed"
fi

backup_ids="$workdir/backup-ids.txt"
: >"$backup_ids"
while IFS= read -r marker; do
  [[ "$marker" =~ ^($backup_prefix-[0-9]{8}T[0-9]{6}Z)\.complete$ ]] || continue
  printf '%s\n' "${BASH_REMATCH[1]}" >>"$backup_ids"
done <"$remote_markers"
LC_ALL=C sort -u -o "$backup_ids" "$backup_ids"

kept_ids="$workdir/kept-ids.txt"
if ! "$retention_script" \
  --daily "$retention_daily" \
  --weekly "$retention_weekly" \
  --monthly "$retention_monthly" \
  <"$backup_ids" >"$kept_ids"; then
  backup_die "retention selection failed"
fi

declare -A keep=()
while IFS= read -r kept_id; do
  [[ -n "$kept_id" ]] && keep[$kept_id]=1
done <"$kept_ids"

deleted_count=0
while IFS= read -r candidate_id; do
  [[ -n "$candidate_id" ]] || continue
  if [[ -z ${keep[$candidate_id]+x} ]]; then
    for suffix in '.tar.age' '.tar.age.sha256' '.complete'; do
      rclone deletefile --config "$rclone_config" "$remote/$candidate_id$suffix" ||
        backup_die "retention deletion failed"
    done
    ((deleted_count += 1))
  fi
done <"$backup_ids"

backup_write_state "$state_dir" last-backup-success "$backup_id"
finished_epoch=$(date -u +%s)
kept_count=$(wc -l <"$kept_ids")

printf 'result=success\n'
printf 'backup_id=%s\n' "$backup_id"
printf 'ciphertext_bytes=%s\n' "$encrypted_bytes"
printf 'duration_seconds=%s\n' "$((finished_epoch - started_epoch))"
printf 'retained_backup_sets=%s\n' "$kept_count"
printf 'deleted_backup_sets=%s\n' "$deleted_count"
printf 'plaintext_cleanup=complete\n'

trap - EXIT
rm -rf -- "$workdir"
