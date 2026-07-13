#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/lib/postgresql-backup-common.sh
source "$script_dir/lib/postgresql-backup-common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/postgresql-backup-healthcheck.sh

Fails when the last verified off-host backup is older than 36 hours or the
last successful disposable restore rehearsal is older than 9 days. Thresholds
may be tightened with PP_BACKUP_MAX_AGE_SECONDS and
PP_BACKUP_RESTORE_MAX_AGE_SECONDS.
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

state_dir=${PP_BACKUP_STATE_DIR:-/var/lib/pandapages-postgresql-backup}
max_backup_age=${PP_BACKUP_MAX_AGE_SECONDS:-129600}
max_restore_age=${PP_BACKUP_RESTORE_MAX_AGE_SECONDS:-777600}

[[ "$max_backup_age" =~ ^[0-9]+$ && "$max_restore_age" =~ ^[0-9]+$ ]] ||
  backup_die "stale thresholds must be non-negative integer seconds"

if [[ -n ${PP_BACKUP_TEST_NOW_EPOCH:-} ]]; then
  [[ ${PP_BACKUP_TEST_MODE:-false} == true ]] ||
    backup_die "PP_BACKUP_TEST_NOW_EPOCH is allowed only when PP_BACKUP_TEST_MODE=true"
  [[ "$PP_BACKUP_TEST_NOW_EPOCH" =~ ^[0-9]+$ ]] ||
    backup_die "PP_BACKUP_TEST_NOW_EPOCH must be an integer"
  now_epoch=$PP_BACKUP_TEST_NOW_EPOCH
else
  now_epoch=$(date -u +%s)
fi

check_state() {
  local filename=$1
  local maximum_age=$2
  local label=$3
  local path="$state_dir/$filename"
  local completed_epoch age

  [[ -f "$path" && ! -L "$path" ]] || backup_die "$label success state is missing: $path"
  completed_epoch=$(backup_read_state_value "$path" completed_at_epoch) ||
    backup_die "$label success state is malformed: $path"
  [[ "$completed_epoch" =~ ^[0-9]+$ ]] || backup_die "$label success timestamp is invalid"
  ((completed_epoch <= now_epoch + 300)) || backup_die "$label success timestamp is unexpectedly in the future"

  age=$((now_epoch - completed_epoch))
  ((age <= maximum_age)) || backup_die "$label success state is stale (${age}s old; maximum ${maximum_age}s)"
  printf '%s_age_seconds=%s\n' "$label" "$age"
}

check_state last-backup-success "$max_backup_age" backup
check_state last-restore-success "$max_restore_age" restore
printf 'status=healthy\n'
