#!/usr/bin/env bash

set -euo pipefail
umask 077

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/lib/postgresql-backup-common.sh
source "$script_dir/lib/postgresql-backup-common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/postgresql-backup-notify.sh --unit SYSTEMD_UNIT

Writes a generic failure event to the journal. If
PP_BACKUP_NOTIFY_CURL_CONFIG points to a private curl config file, the same
event is POSTed through that provider-neutral hook without exposing its URL or
authorization header in process arguments.
EOF
}

unit_name=""
while (($# > 0)); do
  case "$1" in
    --unit)
      unit_name=${2:?missing value for --unit}
      shift 2
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

backup_require_value unit_name "$unit_name"
[[ "$unit_name" =~ ^[A-Za-z0-9@_.:-]+$ ]] || backup_die "invalid systemd unit name"

timestamp=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
printf 'backup_operation_failure unit=%s occurred_at_utc=%s\n' "$unit_name" "$timestamp" >&2

notify_config=${PP_BACKUP_NOTIFY_CURL_CONFIG:-}
if [[ -z "$notify_config" || ! -e "$notify_config" ]]; then
  printf 'notification_hook=not_configured\n' >&2
  exit 0
fi

backup_require_private_file "$notify_config" "notification curl config"
backup_require_command curl

payload=$(printf '{"event":"pandapages_postgresql_backup_failure","unit":"%s","occurred_at_utc":"%s"}' "$unit_name" "$timestamp")
if ! printf '%s\n' "$payload" | curl \
  --config "$notify_config" \
  --fail \
  --silent \
  --show-error \
  --max-time 15 \
  --header 'Content-Type: application/json' \
  --data-binary @- \
  >/dev/null; then
  backup_die "notification hook failed"
fi

printf 'notification_hook=delivered\n'
