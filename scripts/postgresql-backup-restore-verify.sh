#!/usr/bin/env bash

set -euo pipefail
umask 077

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/lib/postgresql-backup-common.sh
source "$script_dir/lib/postgresql-backup-common.sh"

readonly backup_prefix='pandapages-postgresql'
readonly default_postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'

usage() {
  cat <<'EOF'
Usage: scripts/postgresql-backup-restore-verify.sh

Downloads the newest completed encrypted backup from the configured rclone
remote, verifies its SHA-256, decrypts it with age, validates the fixed archive
layout and inner checksums, then invokes the disposable PostgreSQL 18 restore
rehearsal with captured source expectations. Production storage and ports are
never mounted or used.
EOF
}

if (($# > 0)); then
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
fi

remote=${PP_BACKUP_REMOTE:-}
rclone_config=${PP_BACKUP_RCLONE_CONFIG:-}
age_identities=${PP_BACKUP_AGE_IDENTITIES_FILE:-}
state_dir=${PP_BACKUP_STATE_DIR:-/var/lib/pandapages-postgresql-backup}
lock_dir=${PP_BACKUP_LOCK_DIR:-/run/pandapages-postgresql-backup}
tmp_parent=${PP_BACKUP_TMP_PARENT:-/var/tmp}
expected_age_version=${PP_BACKUP_EXPECT_AGE_VERSION:-1.3.1}
expected_rclone_version=${PP_BACKUP_EXPECT_RCLONE_VERSION:-1.74.4}
postgres_image=${PP_BACKUP_POSTGRES_IMAGE:-$default_postgres_image}
restore_script=${PP_BACKUP_RESTORE_SCRIPT:-$script_dir/postgresql-restore-rehearsal.sh}

backup_require_value PP_BACKUP_REMOTE "$remote"
backup_require_value PP_BACKUP_RCLONE_CONFIG "$rclone_config"
backup_require_value PP_BACKUP_AGE_IDENTITIES_FILE "$age_identities"
backup_validate_remote "$remote"
backup_require_private_file "$rclone_config" "rclone config"
backup_require_private_file "$age_identities" "age identities file"
[[ -x "$restore_script" && -f "$restore_script" ]] ||
  backup_die "restore rehearsal script is unavailable or not executable: $restore_script"
[[ "$postgres_image" == *@sha256:* ]] || backup_die "PostgreSQL restore image must be pinned by digest"

for command_name in age awk chmod diff docker flock grep head mkdir mktemp mv realpath rclone rm sha256sum sort stat tail tar; do
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

workdir=$(mktemp -d -- "$tmp_parent/pandapages-postgresql-restore-verify.XXXXXX")
chmod 0700 "$workdir"
cleanup() {
  local status=$?
  set +e
  if [[ -n ${workdir:-} && -d ${workdir:-} && "$workdir" == "$tmp_parent"/pandapages-postgresql-restore-verify.* ]]; then
    rm -rf -- "$workdir"
  fi
  trap - EXIT
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

remote_markers="$workdir/remote-markers.txt"
if ! rclone lsf --config "$rclone_config" --files-only \
  --include "$backup_prefix-*.complete" "$remote" >"$remote_markers"; then
  backup_die "could not list completed remote backups"
fi

backup_ids="$workdir/backup-ids.txt"
: >"$backup_ids"
while IFS= read -r marker; do
  [[ "$marker" =~ ^($backup_prefix-[0-9]{8}T[0-9]{6}Z)\.complete$ ]] || continue
  printf '%s\n' "${BASH_REMATCH[1]}" >>"$backup_ids"
done <"$remote_markers"
LC_ALL=C sort -u -o "$backup_ids" "$backup_ids"
mapfile -t completed_ids <"$backup_ids"
((${#completed_ids[@]} > 0)) || backup_die "no completed remote backup is available"
backup_id=${completed_ids[${#completed_ids[@]} - 1]}

encrypted_path="$workdir/$backup_id.tar.age"
complete_path="$workdir/$backup_id.complete"

for filename in "$backup_id.tar.age" "$backup_id.tar.age.sha256" "$backup_id.complete"; do
  if ! rclone copyto --config "$rclone_config" --no-traverse \
    "$remote/$filename" "$workdir/$filename"; then
    backup_die "completed backup set could not be downloaded"
  fi
done

marker_backup_id=$(backup_read_state_value "$complete_path" backup_id) ||
  backup_die "completion marker has no backup_id"
marker_bytes=$(backup_read_state_value "$complete_path" ciphertext_bytes) ||
  backup_die "completion marker has no ciphertext_bytes"
marker_sha256=$(backup_read_state_value "$complete_path" ciphertext_sha256) ||
  backup_die "completion marker has no ciphertext_sha256"

[[ "$marker_backup_id" == "$backup_id" ]] || backup_die "completion marker backup ID does not match"
[[ "$marker_bytes" =~ ^[0-9]+$ ]] || backup_die "completion marker size is invalid"
[[ "$marker_sha256" =~ ^[0-9a-f]{64}$ ]] || backup_die "completion marker SHA-256 is invalid"
[[ $(stat -c '%s' "$encrypted_path") == "$marker_bytes" ]] || backup_die "downloaded ciphertext size does not match"
[[ $(sha256sum "$encrypted_path" | awk '{print $1}') == "$marker_sha256" ]] ||
  backup_die "downloaded ciphertext SHA-256 does not match the completion marker"

(
  cd "$workdir"
  sha256sum --check --status "$backup_id.tar.age.sha256"
) || backup_die "downloaded ciphertext checksum file rejected the backup"

archive_path="$workdir/$backup_id.tar"
if ! age --decrypt --identity "$age_identities" --output "$archive_path" "$encrypted_path"; then
  backup_die "backup decryption failed"
fi

archive_members="$workdir/archive-members.txt"
tar --list --file "$archive_path" | LC_ALL=C sort >"$archive_members"
expected_members="$workdir/expected-members.txt"
printf '%s\n' \
  database.dump \
  database.dump.sha256 \
  expectations/ \
  expectations/extensions.tsv \
  expectations/row-counts.tsv \
  expectations/schemas.tsv \
  expectations/sequences.tsv \
  expectations/structural-summary.tsv \
  expectations/tables.tsv \
  globals-no-passwords.sql \
  globals-no-passwords.sql.sha256 \
  metadata.env \
  | LC_ALL=C sort >"$expected_members"

if ! diff -u "$expected_members" "$archive_members" >"$workdir/archive-members.diff"; then
  backup_die "decrypted archive contains an unexpected or missing path"
fi

archive_member_types="$workdir/archive-member-types.txt"
LC_ALL=C tar --list --verbose --numeric-owner --file "$archive_path" |
  awk '{ print substr($1, 1, 1) "|" $NF }' |
  LC_ALL=C sort >"$archive_member_types"
expected_member_types="$workdir/expected-member-types.txt"
printf '%s\n' \
  '-|database.dump' \
  '-|database.dump.sha256' \
  'd|expectations/' \
  '-|expectations/extensions.tsv' \
  '-|expectations/row-counts.tsv' \
  '-|expectations/schemas.tsv' \
  '-|expectations/sequences.tsv' \
  '-|expectations/structural-summary.tsv' \
  '-|expectations/tables.tsv' \
  '-|globals-no-passwords.sql' \
  '-|globals-no-passwords.sql.sha256' \
  '-|metadata.env' \
  | LC_ALL=C sort >"$expected_member_types"

if ! diff -u "$expected_member_types" "$archive_member_types" >"$workdir/archive-member-types.diff"; then
  backup_die "decrypted archive contains an unsafe member type"
fi

bundle_dir="$workdir/bundle"
mkdir -p "$bundle_dir"
chmod 0700 "$bundle_dir"
tar \
  --extract \
  --file "$archive_path" \
  --directory "$bundle_dir" \
  --no-same-owner \
  --no-same-permissions

metadata_backup_id=$(backup_read_state_value "$bundle_dir/metadata.env" backup_id) ||
  backup_die "decrypted metadata has no backup_id"
[[ "$metadata_backup_id" == "$backup_id" ]] || backup_die "decrypted metadata backup ID does not match"

(
  cd "$bundle_dir"
  sha256sum --check --status database.dump.sha256
  sha256sum --check --status globals-no-passwords.sql.sha256
) || backup_die "decrypted database or globals checksum failed"

if grep -Eq "(SCRAM-SHA-256\\\$|md5[0-9a-f]{32}|PASSWORD[[:space:]]+'[^']+)" \
  "$bundle_dir/globals-no-passwords.sql"; then
  backup_die "globals dump unexpectedly contains a password verifier"
fi

report_dir="$workdir/rehearsal-report"
if ! TMPDIR="$tmp_parent" "$restore_script" \
  --dump "$bundle_dir/database.dump" \
  --report-dir "$report_dir" \
  --expected-row-counts "$bundle_dir/expectations/row-counts.tsv" \
  --expected-metadata-dir "$bundle_dir/expectations" \
  --image "$postgres_image" \
  >"$workdir/rehearsal.stdout"; then
  for diagnostic in \
    container.log \
    restore.stderr.log \
    row-counts.diff \
    schemas.diff \
    tables.diff \
    extensions.diff \
    sequences.diff \
    structural-summary.diff; do
    if [[ -s "$report_dir/$diagnostic" ]]; then
      printf 'restore_diagnostic=%s\n' "$diagnostic" >&2
      tail -n 40 "$report_dir/$diagnostic" >&2
    fi
  done
  backup_die "disposable restore verification failed"
fi

grep -qx 'result=success' "$report_dir/summary.txt" ||
  backup_die "disposable restore did not record success"
grep -qx 'cleanup=complete' "$report_dir/summary.txt" ||
  backup_die "disposable restore did not clean its resources"

backup_write_state "$state_dir" last-restore-success "$backup_id"

printf 'result=success\n'
printf 'backup_id=%s\n' "$backup_id"
printf 'ciphertext_sha256_verified=true\n'
printf 'disposable_restore_verified=true\n'
printf 'disposable_cleanup=complete\n'
printf 'plaintext_cleanup=complete\n'

trap - EXIT
rm -rf -- "$workdir"
