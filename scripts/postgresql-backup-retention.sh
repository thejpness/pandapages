#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/postgresql-backup-retention.sh [--daily N] [--weekly N] [--monthly N]

Reads completed backup IDs from standard input and prints the IDs to retain.
IDs must use the form pandapages-postgresql-YYYYMMDDTHHMMSSZ. The newest
backup in each retained UTC day, ISO week, and UTC month is kept; the output is
the union of the three tiers.
EOF
}

daily=7
weekly=4
monthly=3

while (($# > 0)); do
  case "$1" in
    --daily)
      daily=${2:?missing value for --daily}
      shift 2
      ;;
    --weekly)
      weekly=${2:?missing value for --weekly}
      shift 2
      ;;
    --monthly)
      monthly=${2:?missing value for --monthly}
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

for value in "$daily" "$weekly" "$monthly"; do
  [[ "$value" =~ ^[0-9]+$ ]] || {
    printf 'Retention counts must be non-negative integers\n' >&2
    exit 2
  }
done

for command_name in date sort; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

mapfile -t backup_ids < <(LC_ALL=C sort -r -u)

declare -A seen_day=()
declare -A seen_week=()
declare -A seen_month=()
declare -A keep=()
daily_count=0
weekly_count=0
monthly_count=0

for backup_id in "${backup_ids[@]}"; do
  [[ "$backup_id" =~ ^pandapages-postgresql-([0-9]{8})T([0-9]{6})Z$ ]] || {
    printf 'Invalid backup ID: %s\n' "$backup_id" >&2
    exit 1
  }

  day=${BASH_REMATCH[1]}
  month=${day:0:6}
  iso_week=$(date -u -d "${day:0:4}-${day:4:2}-${day:6:2}" +'%G-W%V')

  if ((daily_count < daily)) && [[ -z ${seen_day[$day]+x} ]]; then
    seen_day[$day]=1
    keep[$backup_id]=1
    ((daily_count += 1))
  fi

  if ((weekly_count < weekly)) && [[ -z ${seen_week[$iso_week]+x} ]]; then
    seen_week[$iso_week]=1
    keep[$backup_id]=1
    ((weekly_count += 1))
  fi

  if ((monthly_count < monthly)) && [[ -z ${seen_month[$month]+x} ]]; then
    seen_month[$month]=1
    keep[$backup_id]=1
    ((monthly_count += 1))
  fi
done

if ((${#keep[@]} > 0)); then
  printf '%s\n' "${!keep[@]}" | LC_ALL=C sort
fi
