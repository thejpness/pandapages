#!/usr/bin/env bash

# Shared safety helpers for the PostgreSQL backup scripts. This file is sourced;
# each executable is responsible for enabling strict mode before loading it.

backup_error() {
  printf 'ERROR: %s\n' "$*" >&2
}

backup_die() {
  backup_error "$*"
  exit 1
}

backup_require_command() {
  local command_name=$1
  command -v "$command_name" >/dev/null 2>&1 ||
    backup_die "required command is unavailable: $command_name"
}

backup_require_value() {
  local name=$1
  local value=${2:-}
  [[ -n "$value" ]] || backup_die "required configuration is unset: $name"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    backup_die "configuration contains a newline: $name"
}

backup_require_private_file() {
  local path=$1
  local description=$2
  local mode

  [[ -f "$path" && ! -L "$path" ]] ||
    backup_die "$description is not a regular non-symlink file: $path"
  mode=$(stat -c '%a' "$path")
  if ((((8#$mode) & 077) != 0)); then
    backup_die "$description must not be accessible by group or other users: $path"
  fi
}

backup_validate_remote() {
  local remote=$1
  [[ "$remote" =~ ^[A-Za-z0-9][A-Za-z0-9_-]*:.+ ]] ||
    backup_die "PP_BACKUP_REMOTE must be an explicitly named rclone remote and non-empty path"
  [[ "$remote" != *'..'* ]] ||
    backup_die "PP_BACKUP_REMOTE must not contain parent-directory traversal"
}

backup_require_s3_remote() {
  local remote=$1
  local config=$2
  local remote_name=${remote%%:*}

  if ! rclone config redacted --config "$config" "$remote_name" 2>/dev/null |
    grep -qx 'type = s3'; then
    backup_die "configured rclone remote must use the S3 backend for off-host storage"
  fi
}

backup_normalize_version() {
  local version=$1
  version=${version#rclone }
  version=${version#v}
  printf '%s\n' "$version"
}

backup_verify_tool_versions() {
  local expected_age=$1
  local expected_rclone=$2
  local actual_age actual_rclone

  actual_age=$(backup_normalize_version "$(age --version 2>&1 | head -n 1)")
  actual_rclone=$(backup_normalize_version "$(rclone version 2>&1 | head -n 1)")

  [[ "$actual_age" == "$expected_age" ]] ||
    backup_die "age version mismatch: expected $expected_age, found $actual_age"
  [[ "$actual_rclone" == "$expected_rclone" ]] ||
    backup_die "rclone version mismatch: expected $expected_rclone, found $actual_rclone"
}

backup_now_utc() {
  if [[ -n ${PP_BACKUP_TEST_NOW_UTC:-} ]]; then
    [[ ${PP_BACKUP_TEST_MODE:-false} == true ]] ||
      backup_die "PP_BACKUP_TEST_NOW_UTC is allowed only when PP_BACKUP_TEST_MODE=true"
    [[ "$PP_BACKUP_TEST_NOW_UTC" =~ ^[0-9]{8}T[0-9]{6}Z$ ]] ||
      backup_die "PP_BACKUP_TEST_NOW_UTC is not a compact UTC timestamp"
    printf '%s\n' "$PP_BACKUP_TEST_NOW_UTC"
    return
  fi
  date -u +'%Y%m%dT%H%M%SZ'
}

backup_write_state() {
  local state_dir=$1
  local state_name=$2
  local backup_id=$3
  local now_epoch now_utc temporary

  mkdir -p "$state_dir"
  chmod 0700 "$state_dir"
  now_epoch=$(date -u +%s)
  now_utc=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
  temporary=$(mktemp "$state_dir/.${state_name}.XXXXXX")
  chmod 0600 "$temporary"
  {
    printf 'completed_at_epoch=%s\n' "$now_epoch"
    printf 'completed_at_utc=%s\n' "$now_utc"
    printf 'backup_id=%s\n' "$backup_id"
  } >"$temporary"
  mv -f "$temporary" "$state_dir/$state_name"
}

backup_read_state_value() {
  local path=$1
  local key=$2
  awk -F= -v key="$key" '$1 == key { print substr($0, index($0, "=") + 1); found=1; exit } END { if (!found) exit 1 }' "$path"
}
