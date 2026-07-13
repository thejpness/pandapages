#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
backup_script="$repo_root/scripts/postgresql-backup.sh"
restore_verify_script="$repo_root/scripts/postgresql-backup-restore-verify.sh"
retention_script="$repo_root/scripts/postgresql-backup-retention.sh"
health_script="$repo_root/scripts/postgresql-backup-healthcheck.sh"
notify_script="$repo_root/scripts/postgresql-backup-notify.sh"

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-backup-unit.XXXXXX")
cleanup() {
  rm -rf -- "$test_root"
}
trap cleanup EXIT

fake_bin="$test_root/bin"
remote_root="$test_root/remote"
state_dir="$test_root/state"
lock_dir="$test_root/lock"
tmp_parent="$test_root/tmp"
mkdir -p "$fake_bin" "$remote_root" "$state_dir" "$lock_dir" "$tmp_parent"

cat >"$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case ${1:-} in
  inspect)
    printf 'true\n'
    ;;
  exec)
    arguments="$*"
    if [[ "$arguments" == *"pg_dumpall"* ]]; then
      printf '%s\n' '-- PostgreSQL globals' 'CREATE ROLE application_owner;'
    elif [[ "$arguments" == *"pg_dump"* ]]; then
      [[ "$arguments" == *"--snapshot=00000003-0000001B-1"* ]] || exit 1
      if [[ ${FAKE_DATABASE_FAIL:-false} == true ]]; then
        exit 1
      fi
      printf 'FAKE_CUSTOM_DATABASE_DUMP\n'
    elif [[ "$arguments" == *"psql"* ]]; then
      if [[ "$arguments" == *"backup_snapshot_holder=1"* ]]; then
        IFS= read -r begin_statement
        IFS= read -r export_statement
        [[ "$begin_statement" == 'BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;' ]]
        [[ "$export_statement" == 'SELECT pg_export_snapshot();' ]]
        printf '00000003-0000001B-1\n'
        cat >/dev/null
      elif [[ "$arguments" == *"pg_read_all_data"* ]]; then
        [[ ${FAKE_ROLE_CONNECTION_FAIL:-false} != true ]] || exit 1
        printf '%s\n' "${FAKE_ROLE_CHECK:-pandapages_backup|false|true|true}"
      elif [[ "$arguments" == *"SHOW server_version"* ]]; then
        printf '18.1\n'
      elif [[ "$arguments" == *"information_schema.schemata"* ]]; then
        printf 'public\n'
      elif [[ "$arguments" == *"schemaname || '.' || tablename"* ]]; then
        printf '%s\n' public.stories public.story_versions
      elif [[ "$arguments" == *"extname || '|' || extversion"* ]]; then
        printf '%s\n' 'pgcrypto|1.4' 'plpgsql|1.0'
      elif [[ "$arguments" == *"sequencename"* ]]; then
        printf 'public.story_versions_id_seq|2\n'
      elif [[ "$arguments" == *"foreign_keys|"* ]]; then
        printf '%s\n' 'foreign_keys|1|true' 'indexes|3|true' 'utf8_multibyte_story_versions|1|true' 'utf8_roundtrip_failures|0|true'
      else
        cat >/dev/null
        printf '%s\n' 'public.stories|1' 'public.story_versions|2'
      fi
    else
      printf 'Unexpected fake docker exec: %s\n' "$arguments" >&2
      exit 1
    fi
    ;;
  *)
    printf 'Unexpected fake docker command: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF

cat >"$fake_bin/age" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} == --version ]]; then
  printf 'v1.3.1\n'
  exit 0
fi

mode=""
output=""
key_file=""
input=""
while (($# > 0)); do
  case "$1" in
    --encrypt)
      mode=encrypt
      shift
      ;;
    --decrypt)
      mode=decrypt
      shift
      ;;
    --recipients-file|--identity)
      key_file=${2:?}
      shift 2
      ;;
    --output)
      output=${2:?}
      shift 2
      ;;
    *)
      input=$1
      shift
      ;;
  esac
done

[[ -s "$key_file" ]] || exit 1
[[ -n "$output" && -f "$input" ]] || exit 1

case "$mode" in
  encrypt)
    {
      printf 'FAKE-AGE-AUTHENTICATED-HEADER\n'
      cat "$input"
    } >"$output"
    ;;
  decrypt)
    read -r header <"$input"
    [[ "$header" == FAKE-AGE-AUTHENTICATED-HEADER ]] || exit 1
    tail -n +2 "$input" >"$output"
    ;;
  *)
    exit 1
    ;;
esac
EOF

cat >"$fake_bin/rclone" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} == version ]]; then
  printf 'rclone v1.74.4\n'
  exit 0
fi

if [[ ${1:-} == config && ${2:-} == redacted ]]; then
  printf '[fake]\ntype = %s\n' "${FAKE_REMOTE_TYPE:-s3}"
  exit 0
fi

command_name=${1:?}
shift
operands=()
include_pattern=""
while (($# > 0)); do
  case "$1" in
    --config|--include)
      if [[ "$1" == --include ]]; then
        include_pattern=${2:?}
      fi
      shift 2
      ;;
    --immutable|--no-traverse|--files-only)
      shift
      ;;
    *)
      operands+=("$1")
      shift
      ;;
  esac
done

map_path() {
  local value=$1
  if [[ "$value" == fake:* ]]; then
    printf '%s/%s\n' "$FAKE_REMOTE_ROOT" "${value#fake:}"
  else
    printf '%s\n' "$value"
  fi
}

case "$command_name" in
  copyto)
    ((${#operands[@]} == 2))
    source_path=$(map_path "${operands[0]}")
    destination_path=$(map_path "${operands[1]}")
    if [[ ${FAKE_UPLOAD_FAIL:-false} == true && "${operands[1]}" == fake:* ]]; then
      exit 1
    fi
    mkdir -p "$(dirname -- "$destination_path")"
    cp -- "$source_path" "$destination_path"
    if [[ ${FAKE_CORRUPT_DOWNLOAD:-false} == true && "${operands[0]}" == fake:* && "${operands[1]}" != fake:* ]]; then
      printf 'corruption\n' >>"$destination_path"
    fi
    ;;
  deletefile)
    ((${#operands[@]} == 1))
    rm -f -- "$(map_path "${operands[0]}")"
    ;;
  lsf)
    ((${#operands[@]} == 1))
    directory=$(map_path "${operands[0]}")
    [[ -d "$directory" ]] || exit 0
    shopt -s nullglob
    for path in "$directory"/*; do
      filename=${path##*/}
      if [[ -z "$include_pattern" || "$filename" == $include_pattern ]]; then
        printf '%s\n' "$filename"
      fi
    done
    ;;
  *)
    printf 'Unexpected fake rclone command: %s\n' "$command_name" >&2
    exit 1
    ;;
esac
EOF

cat >"$fake_bin/fake-restore" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
report_dir=""
while (($# > 0)); do
  case "$1" in
    --report-dir)
      report_dir=${2:?}
      shift 2
      ;;
    --dump|--expected-row-counts|--expected-metadata-dir|--image)
      shift 2
      ;;
    *)
      exit 2
      ;;
  esac
done
mkdir -p "$report_dir"
printf '%s\n' result=success cleanup=complete >"$report_dir/summary.txt"
EOF

chmod 0755 "$fake_bin/docker" "$fake_bin/age" "$fake_bin/rclone" "$fake_bin/fake-restore"

rclone_config="$test_root/rclone.conf"
recipients_file="$test_root/recipients.txt"
identities_file="$test_root/identities.txt"
printf '[fake]\ntype = local\n' >"$rclone_config"
printf 'age1testrecipient\n' >"$recipients_file"
printf 'AGE-SECRET-KEY-TEST-ONLY\n' >"$identities_file"
chmod 0600 "$rclone_config" "$recipients_file" "$identities_file"

base_environment=(
  "PATH=$fake_bin:$PATH"
  "FAKE_REMOTE_ROOT=$remote_root"
  "PP_BACKUP_POSTGRES_CONTAINER=test-postgres"
  "PP_BACKUP_DATABASE=pandapages_test"
  "PP_BACKUP_DATABASE_USER=pandapages_backup"
  "PP_BACKUP_GLOBALS_USER=application_owner"
  "PP_BACKUP_REMOTE=fake:postgresql"
  "PP_BACKUP_RCLONE_CONFIG=$rclone_config"
  "PP_BACKUP_AGE_RECIPIENTS_FILE=$recipients_file"
  "PP_BACKUP_AGE_IDENTITIES_FILE=$identities_file"
  "PP_BACKUP_STATE_DIR=$state_dir"
  "PP_BACKUP_LOCK_DIR=$lock_dir"
  "PP_BACKUP_TMP_PARENT=$tmp_parent"
  "PP_BACKUP_EXPECT_AGE_VERSION=1.3.1"
  "PP_BACKUP_EXPECT_RCLONE_VERSION=1.74.4"
  "PP_BACKUP_TEST_MODE=true"
)

test_number=0
pass() {
  test_number=$((test_number + 1))
  printf 'ok %s - %s\n' "$test_number" "$1"
}

fail() {
  printf 'not ok %s - %s\n' "$((test_number + 1))" "$1" >&2
  exit 1
}

assert_no_plaintext_temp() {
  if compgen -G "$tmp_parent/pandapages-postgresql-*" >/dev/null; then
    fail "temporary plaintext directory remained"
  fi
}

reset_remote() {
  rm -rf -- "$remote_root/postgresql"
  mkdir -p "$remote_root/postgresql"
}

run_backup() {
  local timestamp=$1
  shift
  env "${base_environment[@]}" "PP_BACKUP_TEST_NOW_UTC=$timestamp" "$@" "$backup_script"
}

printf '1..13\n'

reset_remote
if ! run_backup 20260713T031700Z >"$test_root/success.out" 2>"$test_root/success.err"; then
  sed 's/^/diagnostic: /' "$test_root/success.err" >&2
  fail "successful encrypted backup"
fi
for suffix in .tar.age .tar.age.sha256 .complete; do
  [[ -f "$remote_root/postgresql/pandapages-postgresql-20260713T031700Z$suffix" ]] ||
    fail "successful encrypted backup"
done
grep -qx 'result=success' "$test_root/success.out" || fail "successful encrypted backup"
[[ -f "$state_dir/last-backup-success" ]] || fail "successful encrypted backup"
assert_no_plaintext_temp
pass "successful backup uploads a completed verified set and cleans plaintext"

reset_remote
if run_backup 20260713T041700Z FAKE_ROLE_CONNECTION_FAIL=true >"$test_root/db-fail.out" 2>"$test_root/db-fail.err"; then
  fail "failed database connection"
fi
[[ -z $(find "$remote_root/postgresql" -type f -print -quit) ]] || fail "failed database connection"
assert_no_plaintext_temp
pass "failed database connection returns non-zero without remote or plaintext residue"

reset_remote
if run_backup 20260713T051700Z FAKE_UPLOAD_FAIL=true >"$test_root/upload-fail.out" 2>"$test_root/upload-fail.err"; then
  fail "failed upload"
fi
[[ -z $(find "$remote_root/postgresql" -type f -print -quit) ]] || fail "failed upload"
assert_no_plaintext_temp
pass "failed upload returns non-zero and removes partial objects and plaintext"

reset_remote
if run_backup 20260713T061700Z FAKE_CORRUPT_DOWNLOAD=true >"$test_root/checksum-fail.out" 2>"$test_root/checksum-fail.err"; then
  fail "checksum mismatch"
fi
[[ -z $(find "$remote_root/postgresql" -type f -print -quit) ]] || fail "checksum mismatch"
assert_no_plaintext_temp
pass "remote checksum mismatch rejects and removes the incomplete set"

missing_recipient="$test_root/missing-recipient"
if env "${base_environment[@]}" \
  PP_BACKUP_AGE_RECIPIENTS_FILE="$missing_recipient" \
  PP_BACKUP_TEST_NOW_UTC=20260713T071700Z \
  "$backup_script" >"$test_root/key-fail.out" 2>"$test_root/key-fail.err"; then
  fail "missing encryption recipient"
fi
assert_no_plaintext_temp
pass "missing encryption recipient fails closed"

reset_remote
if run_backup 20260713T074700Z FAKE_REMOTE_TYPE=local >"$test_root/remote-type-fail.out" 2>"$test_root/remote-type-fail.err"; then
  fail "non-S3 remote"
fi
assert_no_plaintext_temp
pass "non-S3 rclone remote is rejected before backup"

exec 8>"$lock_dir/postgresql-backup.lock"
flock -n 8
if run_backup 20260713T081700Z >"$test_root/overlap.out" 2>"$test_root/overlap.err"; then
  fail "overlapping invocation"
fi
flock -u 8
exec 8>&-
pass "overlapping invocation is rejected by flock"

retention_input="$test_root/retention-input"
retention_expected="$test_root/retention-expected"
retention_actual="$test_root/retention-actual"
printf '%s\n' \
  pandapages-postgresql-20260430T031700Z \
  pandapages-postgresql-20260531T031700Z \
  pandapages-postgresql-20260630T031700Z \
  pandapages-postgresql-20260703T031700Z \
  pandapages-postgresql-20260704T031700Z \
  pandapages-postgresql-20260705T031700Z \
  pandapages-postgresql-20260706T031700Z \
  pandapages-postgresql-20260707T031700Z \
  pandapages-postgresql-20260708T031700Z \
  pandapages-postgresql-20260709T031700Z \
  pandapages-postgresql-20260710T031700Z \
  >"$retention_input"
printf '%s\n' \
  pandapages-postgresql-20260531T031700Z \
  pandapages-postgresql-20260630T031700Z \
  pandapages-postgresql-20260705T031700Z \
  pandapages-postgresql-20260709T031700Z \
  pandapages-postgresql-20260710T031700Z \
  >"$retention_expected"
"$retention_script" --daily 2 --weekly 2 --monthly 3 \
  <"$retention_input" >"$retention_actual"
diff -u "$retention_expected" "$retention_actual" || fail "retention selection"
pass "retention selects the union of daily, ISO-weekly, and monthly tiers"

reset_remote
run_backup 20260713T091700Z >"$test_root/restore-source.out" 2>"$test_root/restore-source.err"
if ! env "${base_environment[@]}" \
  PP_BACKUP_RESTORE_SCRIPT="$fake_bin/fake-restore" \
  "$restore_verify_script" >"$test_root/restore-success.out" 2>"$test_root/restore-success.err"; then
  fail "restore verification"
fi
grep -qx 'disposable_restore_verified=true' "$test_root/restore-success.out" || fail "restore verification"
[[ -f "$state_dir/last-restore-success" ]] || fail "restore verification"
assert_no_plaintext_temp
pass "newest completed backup passes checksum, decryption, and restore orchestration"

if env "${base_environment[@]}" \
  PP_BACKUP_AGE_IDENTITIES_FILE="$test_root/missing-identity" \
  PP_BACKUP_RESTORE_SCRIPT="$fake_bin/fake-restore" \
  "$restore_verify_script" >"$test_root/identity-fail.out" 2>"$test_root/identity-fail.err"; then
  fail "missing decryption identity"
fi
assert_no_plaintext_temp
pass "missing decryption identity rejects restore verification"

printf 'tamper\n' >>"$remote_root/postgresql/pandapages-postgresql-20260713T091700Z.tar.age"
if env "${base_environment[@]}" \
  PP_BACKUP_RESTORE_SCRIPT="$fake_bin/fake-restore" \
  "$restore_verify_script" >"$test_root/tamper-fail.out" 2>"$test_root/tamper-fail.err"; then
  fail "invalid encrypted backup"
fi
assert_no_plaintext_temp
pass "tampered encrypted backup is rejected before decryption or restore"

health_state="$test_root/health-state"
mkdir -p "$health_state"
printf '%s\n' completed_at_epoch=1000 completed_at_utc=1970-01-01T00:16:40Z backup_id=test \
  >"$health_state/last-backup-success"
printf '%s\n' completed_at_epoch=1000 completed_at_utc=1970-01-01T00:16:40Z backup_id=test \
  >"$health_state/last-restore-success"
env PP_BACKUP_STATE_DIR="$health_state" PP_BACKUP_TEST_MODE=true PP_BACKUP_TEST_NOW_EPOCH=1100 \
  "$health_script" >"$test_root/health-success.out"
if env PP_BACKUP_STATE_DIR="$health_state" PP_BACKUP_TEST_MODE=true PP_BACKUP_TEST_NOW_EPOCH=2000 \
  PP_BACKUP_MAX_AGE_SECONDS=500 PP_BACKUP_RESTORE_MAX_AGE_SECONDS=500 \
  "$health_script" >"$test_root/health-stale.out" 2>"$test_root/health-stale.err"; then
  fail "stale-state detection"
fi
pass "health check accepts fresh state and rejects stale backup state"

env -u PP_BACKUP_NOTIFY_CURL_CONFIG "$notify_script" --unit test-backup.service \
  >"$test_root/notify.out" 2>"$test_root/notify.err"
grep -q 'notification_hook=not_configured' "$test_root/notify.err" ||
  fail "provider-neutral notification fallback"
pass "notification fallback remains visible in root-only scheduler logs"
