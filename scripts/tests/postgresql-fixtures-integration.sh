#!/usr/bin/env bash

# This harness deliberately handles disposable credentials and diagnostic probes.
# Do not allow an inherited or explicit xtrace setting to print them before the
# redaction boundary.
set +x
set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
seed_script="$repo_root/scripts/dev/seed-test-data.sh"
migration_image=${PP_FIXTURE_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
api_image=${PP_FIXTURE_TEST_API_IMAGE:-pandapages-api:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database=pandapages
readonly database_user=pandapages
readonly database_password='generated-fixture-password-not-for-production'
resource_suffix=${PP_FIXTURE_TEST_RESOURCE_SUFFIX:-$$}
[[ "$resource_suffix" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$ ]] || {
  printf 'Fixture integration resource suffix is invalid\n' >&2
  exit 1
}
readonly resource_prefix="pandapages-fixture-integration-$resource_suffix"
readonly postgres_container="$resource_prefix-postgres"
readonly api_container="$resource_prefix-api"
readonly network="$resource_prefix-network"
readonly volume="$resource_prefix-data"
readonly run_label="com.pandapages.fixture-run=$resource_prefix"
controlled_goose_failure_status=${PP_FIXTURE_TEST_CONTROLLED_GOOSE_FAILURE_STATUS:-}
controlled_diagnostic_secret=${PP_FIXTURE_TEST_CONTROLLED_DIAGNOSTIC_SECRET:-}
controlled_diagnostic_password=${PP_FIXTURE_TEST_CONTROLLED_DIAGNOSTIC_PASSWORD:-}
controlled_process_helper_mode=${PP_FIXTURE_TEST_PROCESS_HELPER_MODE:-}
controlled_process_helper_ready=${PP_FIXTURE_TEST_PROCESS_HELPER_READY:-}
controlled_process_helper_term_seen=${PP_FIXTURE_TEST_PROCESS_HELPER_TERM_SEEN:-}
controlled_process_helper_marker=${PP_FIXTURE_TEST_PROCESS_HELPER_MARKER:-}

if [[ -n "$controlled_goose_failure_status" ]] &&
  [[ ! "$controlled_goose_failure_status" =~ ^([1-9]|[1-9][0-9]|1[01][0-9]|12[0-5])$ ]]; then
  printf 'Controlled Goose failure status must be between 1 and 125\n' >&2
  exit 1
fi

if [[ -n "$controlled_diagnostic_secret" ]] &&
  [[ ! "$controlled_diagnostic_secret" =~ ^[a-zA-Z0-9._-]{16,120}$ ]]; then
  printf 'Controlled diagnostic secret must contain 16-120 safe characters\n' >&2
  exit 1
fi

if [[ -n "$controlled_diagnostic_password" ]] &&
  [[ ! "$controlled_diagnostic_password" =~ ^[a-zA-Z0-9._-]{16,120}$ ]]; then
  printf 'Controlled diagnostic password must contain 16-120 safe characters\n' >&2
  exit 1
fi

for command_name in curl docker grep mktemp ps sed setsid tail; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

fixture_harness_pgid=$(ps -o pgid= -p "$$")
fixture_harness_pgid=${fixture_harness_pgid//[[:space:]]/}
[[ "$fixture_harness_pgid" =~ ^[1-9][0-9]*$ ]] || {
  printf 'Could not determine the fixture harness process group\n' >&2
  exit 1
}
readonly fixture_harness_pgid

if [[ -n "$controlled_process_helper_mode" ]]; then
  [[ -n "$controlled_process_helper_ready" &&
    "$controlled_process_helper_ready" == "${TMPDIR:-/tmp}/"* ]] || {
    printf 'Controlled process helper ready path is invalid\n' >&2
    exit 1
  }
  case "$controlled_process_helper_mode" in
    foreground)
      trap 'exit 143' TERM
      bash -c '
        printf "%s\n" "$$" >"$1"
        exec sleep 30
      ' fixture-foreground-child "$controlled_process_helper_ready"
      ;;
    delayed-docker)
      [[ -n "$controlled_process_helper_term_seen" &&
        "$controlled_process_helper_term_seen" == "${TMPDIR:-/tmp}/"* &&
        -n "$controlled_process_helper_marker" &&
        "$controlled_process_helper_marker" == "${TMPDIR:-/tmp}/"* ]] || {
        printf 'Controlled delayed-Docker helper paths are invalid\n' >&2
        exit 1
      }
      trap 'exit 143' TERM
      bash -c '
        term_seen=$2
        marker=$3
        fake_docker_create() {
          sleep 5
          printf "com.pandapages.fixture-run=synthetic-delayed-docker\n" >"$marker"
        }
        handle_term() {
          printf "TERM received before fake Docker create\n" >"$term_seen"
          fake_docker_create
        }
        trap handle_term TERM
        printf "%s\n" "$$" >"$1"
        while :; do
          sleep 30
        done
      ' fixture-delayed-docker \
        "$controlled_process_helper_ready" \
        "$controlled_process_helper_term_seen" \
        "$controlled_process_helper_marker"
      ;;
    early-exit)
      exit 41
      ;;
    *)
      printf 'Controlled process helper mode is invalid\n' >&2
      exit 1
      ;;
  esac
fi

[[ -x "$seed_script" ]] || {
  printf 'Test-seed command is unavailable\n' >&2
  exit 1
}

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'Refusing fixture integration while DOCKER_HOST is set\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] || [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Fixture integration requires a known local Docker Unix socket\n' >&2
  exit 1
fi

for image in "$migration_image" "$api_image"; do
  docker image inspect "$image" >/dev/null 2>&1 || {
    printf 'Required test image is unavailable: %s\n' "$image" >&2
    exit 1
  }
done

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-fixture-integration.XXXXXX")
goose_container=''
goose_run_count=0
failure_line=''
failure_signal=''
controlled_pid=''
controlled_child_status=''
controlled_pgid=''
controlled_sid=''
controlled_sentinel_pid=''
controlled_sentinel_start_time=''
controlled_launching=0
controlled_pending_signal=''
controlled_pending_status=''
controlled_termination_grace_seconds=10
controlled_identity_root=''
controlled_launch_test_signal=''
controlled_launch_test_identity_file=''
controlled_launch_test_identity_failure=0
controlled_process_group_verify_failure=0
controlled_prefix=''
controlled_label=''
controlled_root=''
controlled_stdout=''
controlled_stderr=''

redact_diagnostics() {
  local escaped_controlled_secret
  local escaped_controlled_password
  local -a sed_arguments=(-E)

  if [[ -n "$controlled_diagnostic_secret" ]]; then
    escaped_controlled_secret=${controlled_diagnostic_secret//./\\.}
    sed_arguments+=(-e "s/$escaped_controlled_secret/[redacted-controlled-secret]/g")
  fi
  if [[ -n "$controlled_diagnostic_password" ]]; then
    escaped_controlled_password=${controlled_diagnostic_password//./\\.}
    sed_arguments+=(-e "s/$escaped_controlled_password/[redacted]/g")
  fi
  sed_arguments+=(
    -e "s/$database_password/[redacted]/g"
    -e 's#postgres(%3A|%3a)(%2F|%2f)(%2F|%2f)[^[:space:]]+#[redacted-database-url]#g'
    -e 's#postgres://[^[:space:]]+#[redacted-database-url]#g'
  )
  sed "${sed_arguments[@]}"
}

docker_query() {
  local description=$1
  local output
  local status
  shift

  if output=$(docker "$@" 2>&1); then
    [[ -z "$output" ]] || printf '%s\n' "$output"
    return 0
  else
    status=$?
  fi

  printf 'Docker command failed while %s (status %d)\n' "$description" "$status" >&2
  if [[ -n "$output" ]]; then
    printf '%s\n' "$output" | redact_diagnostics >&2
  fi
  return "$status"
}

list_labeled_resource_ids() {
  local kind=$1
  local label=$2

  case "$kind" in
    container)
      docker_query 'listing disposable fixture containers' \
        ps -aq --filter "label=$label"
      ;;
    network)
      docker_query 'listing disposable fixture networks' \
        network ls -q --filter "label=$label"
      ;;
    volume)
      docker_query 'listing disposable fixture volumes' \
        volume ls -q --filter "label=$label"
      ;;
    *)
      printf 'Unknown disposable fixture resource kind\n' >&2
      return 2
      ;;
  esac
}

inspect_container_name() {
  local container_id=$1
  docker_query 'inspecting a disposable fixture container' \
    container inspect --format '{{.Name}}' "$container_id"
}

verify_no_labeled_resources() {
  local label=$1
  local scope=$2
  local kind
  local resource_ids
  local verification_failed=0

  for kind in container network volume; do
    if resource_ids=$(list_labeled_resource_ids "$kind" "$label"); then
      if [[ -n "$resource_ids" ]]; then
        printf 'Disposable %s %s resources remain after cleanup\n' "$scope" "$kind" >&2
        verification_failed=1
      fi
    else
      verification_failed=1
    fi
  done

  return "$verification_failed"
}

remove_labeled_resource_kind() {
  local kind=$1
  local label=$2
  local resource_id
  local resource_ids
  local remaining_ids
  local removal_failed=0

  if ! resource_ids=$(list_labeled_resource_ids "$kind" "$label"); then
    return 1
  fi

  while IFS= read -r resource_id; do
    [[ -n "$resource_id" ]] || continue
    case "$kind" in
      container)
        if ! docker_query 'removing a disposable fixture container' \
          rm --force "$resource_id" >/dev/null; then
          removal_failed=1
        fi
        ;;
      network)
        if ! docker_query 'removing a disposable fixture network' \
          network rm "$resource_id" >/dev/null; then
          removal_failed=1
        fi
        ;;
      volume)
        if ! docker_query 'removing a disposable fixture volume' \
          volume rm "$resource_id" >/dev/null; then
          removal_failed=1
        fi
        ;;
    esac
  done <<<"$resource_ids"

  if remaining_ids=$(list_labeled_resource_ids "$kind" "$label"); then
    if [[ -n "$remaining_ids" ]]; then
      printf 'Disposable fixture %s removal was incomplete\n' "$kind" >&2
      removal_failed=1
    fi
  else
    removal_failed=1
  fi

  return "$removal_failed"
}

cleanup_labeled_resources() {
  local label=$1
  local kind
  local cleanup_failed=0

  for kind in container network volume; do
    if ! remove_labeled_resource_kind "$kind" "$label"; then
      cleanup_failed=1
    fi
  done

  return "$cleanup_failed"
}

is_safe_isolated_pgid() {
  local pgid=$1

  [[ "$pgid" =~ ^[1-9][0-9]*$ ]] &&
    ((pgid > 1)) &&
    [[ "$pgid" != "$fixture_harness_pgid" ]]
}

read_process_start_time() {
  local pid=$1
  local process_stat
  local -a stat_fields=()

  [[ "$pid" =~ ^[1-9][0-9]*$ && -r "/proc/$pid/stat" ]] || return 1
  IFS= read -r process_stat <"/proc/$pid/stat" || return 1
  process_stat=${process_stat#*) }
  read -r -a stat_fields <<<"$process_stat"
  ((${#stat_fields[@]} > 19)) || return 1
  printf '%s\n' "${stat_fields[19]}"
}

verify_controlled_process_group_identity() {
  local pgid=$1
  local sentinel_identity
  local observed_pgid=''
  local observed_sid=''
  local observed_start_time=''

  is_safe_isolated_pgid "$pgid" || return 1
  [[ "$controlled_pgid" == "$pgid" &&
    "$controlled_sid" == "$pgid" &&
    "$controlled_sentinel_pid" =~ ^[1-9][0-9]*$ &&
    -n "$controlled_sentinel_start_time" ]] || return 1
  if sentinel_identity=$(ps -o pgid= -o sid= -p "$controlled_sentinel_pid" 2>/dev/null); then
    read -r observed_pgid observed_sid <<<"$sentinel_identity"
  fi
  observed_start_time=$(read_process_start_time "$controlled_sentinel_pid") || return 1
  [[ "$observed_pgid" == "$pgid" &&
    "$observed_sid" == "$controlled_sid" &&
    "$observed_start_time" == "$controlled_sentinel_start_time" ]]
}

process_group_exists() {
  local pgid=$1

  is_safe_isolated_pgid "$pgid" || return 2
  kill -0 -- "-$pgid" 2>/dev/null
}

wait_for_process_group_exit() {
  local pgid=$1
  local attempt

  is_safe_isolated_pgid "$pgid" || return 1
  if ((controlled_process_group_verify_failure != 0)); then
    printf 'Controlled process-group verification failure for harness regression\n' >&2
    return 1
  fi
  for ((attempt = 0; attempt < 100; attempt++)); do
    if ! process_group_exists "$pgid"; then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

remove_controlled_identity_root() {
  if [[ -n "$controlled_identity_root" &&
    "$controlled_identity_root" == "$test_root/"* &&
    -e "$controlled_identity_root" ]]; then
    rm -rf -- "$controlled_identity_root"
  fi
  controlled_identity_root=''
}

replay_controlled_pending_signal() {
  local pending_signal
  local pending_status

  [[ -n "$controlled_pending_signal" ]] || return 0
  pending_signal=$controlled_pending_signal
  pending_status=$controlled_pending_status
  controlled_pending_signal=''
  controlled_pending_status=''
  on_signal "$pending_signal" "$pending_status"
}

launch_controlled_process() {
  local stdout_file=$1
  local stderr_file=$2
  local ready_file
  local acknowledge_file
  local ready_pid=''
  local ready_sentinel_pid=''
  local session_identity=''
  local sentinel_identity=''
  local observed_pgid=''
  local observed_sid=''
  local observed_sentinel_pgid=''
  local observed_sentinel_sid=''
  local sentinel_start_time=''
  local current_sentinel_start_time=''
  local group_was_verified=0
  local cleanup_failed=0
  local attempt
  local child_pid
  shift 2

  [[ -z "$controlled_pid" && -z "$controlled_pgid" ]] || {
    printf 'A controlled fixture child is already owned\n' >&2
    return 1
  }
  [[ -z "$controlled_launch_test_signal" || "$controlled_launch_test_signal" == TERM ]] || {
    printf 'Controlled launch test signal is invalid\n' >&2
    return 1
  }

  controlled_identity_root=$(mktemp -d "$test_root/controlled-process.XXXXXX")
  ready_file="$controlled_identity_root/ready"
  acknowledge_file="$controlled_identity_root/acknowledge"
  controlled_launching=1
  setsid bash -c '
    set -euo pipefail
    ready_file=$1
    acknowledge_file=$2
    shift 2
    bash -c '\''
      trap "" HUP INT TERM
      exec sleep 2147483647
    '\'' fixture-controlled-sentinel &
    sentinel_pid=$!
    printf "%s %s\n" "$$" "$sentinel_pid" >"$ready_file"
    while [[ ! -e "$acknowledge_file" ]]; do
      sleep 0.01
    done
    exec "$@"
  ' fixture-controlled-session "$ready_file" "$acknowledge_file" "$@" \
    >"$stdout_file" 2>"$stderr_file" &
  child_pid=$!
  controlled_pid=$child_pid
  if [[ "$controlled_launch_test_signal" == TERM ]]; then
    kill -TERM "$BASHPID"
  fi

  for ((attempt = 0; attempt < 1000; attempt++)); do
    if [[ -s "$ready_file" ]]; then
      read -r ready_pid ready_sentinel_pid <"$ready_file"
      break
    fi
    if ! kill -0 "$child_pid" 2>/dev/null; then
      break
    fi
    sleep 0.01 || true
  done

  if session_identity=$(ps -o pgid= -o sid= -p "$child_pid" 2>/dev/null); then
    read -r observed_pgid observed_sid <<<"$session_identity"
  fi
  if sentinel_identity=$(ps -o pgid= -o sid= -p "$ready_sentinel_pid" 2>/dev/null); then
    read -r observed_sentinel_pgid observed_sentinel_sid <<<"$sentinel_identity"
  fi
  sentinel_start_time=$(read_process_start_time "$ready_sentinel_pid") || true
  if ((controlled_launch_test_identity_failure != 0)) &&
    [[ -n "$controlled_launch_test_identity_file" ]]; then
    printf '%s %s %s\n' "$child_pid" "$child_pid" "$ready_sentinel_pid" \
      >"$controlled_launch_test_identity_file"
  fi
  if [[ "$ready_pid" == "$child_pid" &&
    "$observed_pgid" == "$child_pid" &&
    "$observed_sid" == "$child_pid" &&
    "$ready_sentinel_pid" =~ ^[1-9][0-9]*$ &&
    "$observed_sentinel_pgid" == "$child_pid" &&
    "$observed_sentinel_sid" == "$child_pid" &&
    -n "$sentinel_start_time" &&
    "$child_pid" != "$fixture_harness_pgid" &&
    "$controlled_launch_test_identity_failure" == 0 ]]; then
    controlled_pgid=$child_pid
    controlled_sid=$child_pid
    controlled_sentinel_pid=$ready_sentinel_pid
    controlled_sentinel_start_time=$sentinel_start_time
    if [[ -n "$controlled_launch_test_identity_file" ]]; then
      printf '%s %s\n' "$controlled_pid" "$controlled_pgid" \
        >"$controlled_launch_test_identity_file"
    fi
    : >"$acknowledge_file"
    controlled_launching=0
    replay_controlled_pending_signal
    return 0
  fi

  controlled_launching=0
  if [[ "$observed_pgid" == "$child_pid" && "$observed_sid" == "$child_pid" ]] ||
    [[ "$ready_sentinel_pid" =~ ^[1-9][0-9]*$ &&
      "$observed_sentinel_pgid" == "$child_pid" &&
      "$observed_sentinel_sid" == "$child_pid" &&
      -n "$sentinel_start_time" ]]; then
    group_was_verified=1
    if [[ "$observed_sentinel_pgid" == "$child_pid" &&
      "$observed_sentinel_sid" == "$child_pid" &&
      -n "$sentinel_start_time" ]]; then
      controlled_pgid=$child_pid
      controlled_sid=$child_pid
      controlled_sentinel_pid=$ready_sentinel_pid
      controlled_sentinel_start_time=$sentinel_start_time
    fi
    kill -KILL -- "-$child_pid" 2>/dev/null || true
  else
    kill -KILL "$child_pid" 2>/dev/null || true
    current_sentinel_start_time=$(read_process_start_time "$ready_sentinel_pid") || true
    if [[ -n "$sentinel_start_time" &&
      "$current_sentinel_start_time" == "$sentinel_start_time" ]]; then
      kill -KILL "$ready_sentinel_pid" 2>/dev/null || true
    fi
  fi
  wait "$child_pid" 2>/dev/null || true
  controlled_pid=''
  if is_safe_isolated_pgid "$child_pid" &&
    ! wait_for_process_group_exit "$child_pid"; then
    cleanup_failed=1
  fi
  if ((cleanup_failed == 0)); then
    controlled_pgid=''
    controlled_sid=''
    controlled_sentinel_pid=''
    controlled_sentinel_start_time=''
    remove_controlled_identity_root
  elif ((group_was_verified == 0)); then
    printf 'Could not verify failed-launch process cleanup\n' >&2
  fi
  if [[ -n "$controlled_pending_signal" ]]; then
    replay_controlled_pending_signal
  fi
  printf 'Could not establish an isolated controlled fixture process group\n' >&2
  return 1
}

finish_controlled_process_group() {
  local child_pgid=$controlled_pgid

  [[ -n "$child_pgid" ]] || {
    remove_controlled_identity_root
    return 0
  }
  if ! is_safe_isolated_pgid "$child_pgid"; then
    printf 'Refusing to signal an unverified controlled fixture process group\n' >&2
    return 1
  fi
  if process_group_exists "$child_pgid"; then
    if ! verify_controlled_process_group_identity "$child_pgid"; then
      printf 'Controlled fixture process-group identity changed before cleanup\n' >&2
      return 1
    fi
    kill -KILL -- "-$child_pgid" 2>/dev/null || true
  fi
  if ! wait_for_process_group_exit "$child_pgid"; then
    printf 'Failed to remove the controlled fixture process group\n' >&2
    return 1
  fi
  controlled_pgid=''
  controlled_sid=''
  controlled_sentinel_pid=''
  controlled_sentinel_start_time=''
  remove_controlled_identity_root
}

wait_for_controlled_child() {
  local child_pid=$controlled_pid
  local child_status

  [[ -n "$child_pid" ]] || return 0
  if wait "$child_pid" 2>/dev/null; then
    child_status=0
  else
    child_status=$?
  fi
  controlled_child_status=$child_status
  controlled_pid=''
  finish_controlled_process_group
}

terminate_controlled_child() {
  local child_pid=$controlled_pid
  local child_pgid=$controlled_pgid
  local child_status
  local watchdog_pid=''

  [[ -n "$child_pid" || -n "$child_pgid" ]] || {
    remove_controlled_identity_root
    return 0
  }
  if [[ -n "$child_pgid" ]]; then
    if ! is_safe_isolated_pgid "$child_pgid"; then
      printf 'Refusing to signal an unverified controlled fixture process group\n' >&2
      return 1
    fi
    if process_group_exists "$child_pgid"; then
      if ! verify_controlled_process_group_identity "$child_pgid"; then
        printf 'Controlled fixture process-group identity changed before termination\n' >&2
        return 1
      fi
      kill -TERM -- "-$child_pgid" 2>/dev/null || true
      (
        sleep "$controlled_termination_grace_seconds"
        if process_group_exists "$child_pgid" &&
          verify_controlled_process_group_identity "$child_pgid"; then
          kill -KILL -- "-$child_pgid" 2>/dev/null || true
        fi
      ) &
      watchdog_pid=$!
    fi
  elif [[ -n "$child_pid" ]]; then
    # Launch cleanup reaches this branch only before process-group validation.
    kill -KILL "$child_pid" 2>/dev/null || true
  fi

  if [[ -n "$child_pid" ]]; then
    if wait "$child_pid" 2>/dev/null; then
      child_status=0
    else
      child_status=$?
    fi
    controlled_child_status=$child_status
    controlled_pid=''
  fi
  if [[ -n "$watchdog_pid" ]]; then
    kill "$watchdog_pid" 2>/dev/null || true
    wait "$watchdog_pid" 2>/dev/null || true
  fi

  finish_controlled_process_group
}

cleanup() {
  local cleanup_failed=0

  if ! terminate_controlled_child; then
    cleanup_failed=1
  fi
  if [[ -n "$controlled_label" ]] &&
    ! cleanup_labeled_resources "$controlled_label"; then
    cleanup_failed=1
  fi
  if ! cleanup_labeled_resources "$run_label"; then
    cleanup_failed=1
  fi
  goose_container=''

  if [[ -n "$controlled_root" && "$controlled_root" == "$test_root/"* && -e "$controlled_root" ]] &&
    ! rm -rf -- "$controlled_root"; then
    printf 'Failed to remove controlled fixture child artifacts\n' >&2
    cleanup_failed=1
  fi
  if [[ -n "$test_root" && -e "$test_root" ]] && ! rm -rf -- "$test_root"; then
    printf 'Failed to remove disposable fixture test artifacts\n' >&2
    cleanup_failed=1
  fi

  return "$cleanup_failed"
}

print_failure_diagnostics() {
  local status=$1
  local container_id
  local container_ids
  local container_logs
  local container_name
  local diagnostics_failed=0
  local log_file

  if [[ -n "$failure_signal" ]]; then
    printf 'Fixture integration interrupted by %s (status %d)\n' "$failure_signal" "$status" >&2
  elif [[ -n "$failure_line" ]]; then
    printf 'Fixture integration failed at line %s (status %d)\n' "$failure_line" "$status" >&2
  else
    printf 'Fixture integration failed (status %d)\n' "$status" >&2
  fi

  for log_file in "$test_root"/*-goose.err "$test_root"/*-goose.out; do
    [[ -s "$log_file" ]] || continue
    printf '%s\n' "--- ${log_file##*/} (last 80 lines, redacted) ---" >&2
    if ! tail -n 80 "$log_file" | redact_diagnostics >&2; then
      diagnostics_failed=1
    fi
  done

  if container_ids=$(list_labeled_resource_ids container "$run_label"); then
    while IFS= read -r container_id; do
      [[ -n "$container_id" ]] || continue
      if ! container_name=$(inspect_container_name "$container_id"); then
        diagnostics_failed=1
        continue
      fi
      container_name=${container_name#/}
      case "$container_name" in
        "$api_container")
          printf '%s\n' '--- fixture API logs (last 80 lines, redacted) ---' >&2
          ;;
        "$postgres_container")
          printf '%s\n' '--- fixture PostgreSQL logs (last 80 lines, redacted) ---' >&2
          ;;
        *)
          continue
          ;;
      esac
      if container_logs=$(docker_query 'reading disposable fixture container logs' \
        logs --tail 80 "$container_id"); then
        printf '%s\n' "$container_logs" | redact_diagnostics >&2
      else
        diagnostics_failed=1
      fi
    done <<<"$container_ids"
  else
    diagnostics_failed=1
  fi

  return "$diagnostics_failed"
}

record_failure() {
  failure_line=$1
}

combined_exit_status() {
  local original_status=$1
  local cleanup_status=$2

  if ((original_status != 0)); then
    printf '%d\n' "$original_status"
  else
    printf '%d\n' "$cleanup_status"
  fi
}

on_exit() {
  local status=$?
  local cleanup_status=0

  trap - EXIT ERR HUP INT TERM
  if ((status != 0)); then
    if ! print_failure_diagnostics "$status"; then
      printf 'Fixture integration diagnostics were incomplete\n' >&2
    fi
  fi
  cleanup || cleanup_status=$?
  if ((cleanup_status != 0)); then
    printf 'Fixture integration cleanup was incomplete\n' >&2
  fi
  status=$(combined_exit_status "$status" "$cleanup_status")
  exit "$status"
}

on_signal() {
  if ((controlled_launching != 0)); then
    if [[ -z "$controlled_pending_signal" ]]; then
      controlled_pending_signal=$1
      controlled_pending_status=$2
    fi
    return 0
  fi
  failure_signal=$1
  exit "$2"
}

trap on_exit EXIT
trap 'record_failure "$LINENO"' ERR
trap 'on_signal HUP 129' HUP
trap 'on_signal INT 130' INT
trap 'on_signal TERM 143' TERM

docker network create \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
  "$network" >/dev/null

docker volume create \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
  "$volume" >/dev/null

docker run --detach \
  --name "$postgres_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
  --label com.pandapages.test-seed-target=disposable-fixture-integration \
  --env POSTGRES_DB="$database" \
  --env POSTGRES_USER="$database_user" \
  --env POSTGRES_PASSWORD="$database_password" \
  --mount "type=volume,src=$volume,dst=/var/lib/postgresql" \
  --health-cmd "pg_isready --username=$database_user --dbname=$database" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null

health=starting
for ((attempt = 0; attempt < 60; attempt++)); do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$postgres_container")
  [[ "$health" == healthy ]] && break
  [[ "$health" != unhealthy ]] || {
    printf 'Disposable fixture PostgreSQL became unhealthy\n' >&2
    exit 1
  }
  sleep 1
done
[[ "$health" == healthy ]] || {
  printf 'Timed out waiting for disposable fixture PostgreSQL\n' >&2
  exit 1
}

psql_query() {
  local statement=$1
  docker exec "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="$statement"
}

assert_query() {
  local expected=$1
  local statement=$2
  local description=$3
  local actual
  actual=$(psql_query "$statement")
  [[ "$actual" == "$expected" ]] || {
    printf '%s: expected %s, got %s\n' "$description" "$expected" "$actual" >&2
    exit 1
  }
}

run_goose() {
  local status

  if [[ -n "$controlled_goose_failure_status" ]]; then
    if [[ -n "$controlled_diagnostic_secret" || -n "$controlled_diagnostic_password" ]]; then
      printf 'Controlled fixture diagnostic marker: disposable synthetic input\n' >&2
    fi
    if [[ -n "$controlled_diagnostic_secret" ]]; then
      printf 'Synthetic fixture secret: %s\n' "$controlled_diagnostic_secret" >&2
    fi
    if [[ -n "$controlled_diagnostic_password" ]]; then
      printf 'Synthetic fixture password: %s\n' "$controlled_diagnostic_password" >&2
      printf 'Synthetic fixture database URL: postgres://fixture:%s@fixture.invalid:5432/disposable?sslmode=disable\n' \
        "$controlled_diagnostic_password" >&2
      printf '%s\n' \
        "Synthetic fixture encoded database URL: postgres%3A%2F%2Ffixture%3A${controlled_diagnostic_password}%40fixture.invalid%3A5432%2Fdisposable" \
        >&2
    fi
    printf 'Controlled fixture migration failure for harness regression (status %s)\n' \
      "$controlled_goose_failure_status" >&2
    return "$controlled_goose_failure_status"
  fi

  ((goose_run_count += 1))
  goose_container="$resource_prefix-goose-$goose_run_count"
  if docker run --rm \
    --name "$goose_container" \
    --network "$network" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --label com.pandapages.disposable=fixture-migration-integration \
    --label "$run_label" \
    --env GOOSE_DRIVER=postgres \
    --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$postgres_container:5432/$database?sslmode=disable" \
    --env GOOSE_MIGRATION_DIR=/migrations \
    --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
    "$migration_image" "$@"; then
    goose_container=''
    return 0
  else
    status=$?
    return "$status"
  fi
}

reset_database() {
  psql_query 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null 2>&1
}

expect_seed_failure() {
  local name=$1
  shift
  if "$@" >"$test_root/$name.out" 2>"$test_root/$name.err"; then
    printf 'Seed safety case unexpectedly succeeded: %s\n' "$name" >&2
    exit 1
  fi
}

printf '1..18\n'

run_goose up >"$test_root/fresh-goose.out" 2>"$test_root/fresh-goose.err"
grep -q 'OK.*00014_reader_2_contract.sql' \
  "$test_root/fresh-goose.out" "$test_root/fresh-goose.err"
assert_query 't' "
  SELECT bool_and(relation IS NOT NULL)
  FROM (VALUES
    (to_regclass('public.accounts')),
    (to_regclass('public.child_profiles')),
    (to_regclass('public.generation_jobs')),
    (to_regclass('public.profile_settings')),
    (to_regclass('public.profiles')),
    (to_regclass('public.reading_progress')),
    (to_regclass('public.stories')),
    (to_regclass('public.story_sections')),
    (to_regclass('public.story_segments')),
    (to_regclass('public.story_versions'))
  ) AS required(relation);
" 'fresh schema tables'
assert_query '0|0|0|0|0|0|0|0|0|0|0|0|0' "
  SELECT
    (SELECT count(*) FROM stories),
    (SELECT count(*) FROM story_versions),
    (SELECT count(*) FROM story_sections),
    (SELECT count(*) FROM story_segments),
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM child_profiles),
    (SELECT count(*) FROM prompt_profiles),
    (SELECT count(*) FROM generation_jobs),
    (SELECT count(*) FROM profile_settings),
    (SELECT count(*) FROM works),
    (SELECT count(*) FROM contributors),
    (SELECT count(*) FROM story_contributors),
    (SELECT count(*) FROM assets);
" 'fresh migration fixture inventory'
printf 'ok 1 - fresh migrations leave the complete application schema without fixture content\n'

assert_query '1|1|0' "
  SELECT
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles WHERE name = 'Default'),
    (SELECT count(*) FROM pg_constraint WHERE contype = 'f' AND NOT convalidated);
" 'fresh bootstrap and constraints'
printf 'ok 2 - runtime bootstrap records and validated foreign keys remain intact\n'

reset_database
run_goose up-to 12 >"$test_root/pre-cleanup-goose.out" 2>"$test_root/pre-cleanup-goose.err"
assert_query '3|3|1|9|1|1|1|1|3|2|2' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_sections AS section JOIN story_versions AS version ON version.id = section.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM child_profiles WHERE name = 'Ted'),
    (SELECT count(*) FROM prompt_profiles WHERE name = 'Bedtime calm v1'),
    (SELECT count(*) FROM generation_jobs WHERE model = 'seed-model' AND prompt_version = 'v1'),
    (SELECT count(*) FROM works WHERE canonical_title IN ('The Fox and the Grapes','The Little Star and the Moon','Frankenstein; or, The Modern Prometheus')),
    (SELECT count(*) FROM contributors WHERE name IN ('Aesop','Mary Shelley')),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug IN ('the-fox-and-the-grapes','frankenstein-excerpt-ch1'));
" 'historical fixture inventory'
printf 'ok 3 - the real pre-cleanup migration path recreates the complete historical fixture set\n'

run_goose up-to 13 >"$test_root/exact-cleanup-goose.out" 2>"$test_root/exact-cleanup-goose.err"
assert_query '0|0|0|0|0|0|0|0|0|0|0' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_sections AS section JOIN story_versions AS version ON version.id = section.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM child_profiles WHERE name = 'Ted' AND age_months = 34),
    (SELECT count(*) FROM prompt_profiles WHERE name = 'Bedtime calm v1' AND schema_version = 1),
    (SELECT count(*) FROM generation_jobs WHERE model = 'seed-model' AND prompt_version = 'v1'),
    (SELECT count(*) FROM works WHERE canonical_title IN ('The Fox and the Grapes','The Little Star and the Moon','Frankenstein; or, The Modern Prometheus')),
    (SELECT count(*) FROM contributors WHERE name IN ('Aesop','Mary Shelley')),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug IN ('the-fox-and-the-grapes','frankenstein-excerpt-ch1'));
" 'exact historical fixture cleanup'
printf 'ok 4 - an unchanged historical lifecycle is removed with versions, sections, segments, links, progress, and fixture profiles/jobs\n'

reset_database
run_goose up-to 12 >"$test_root/preservation-goose.out" 2>"$test_root/preservation-goose.err"

docker exec -i "$postgres_container" \
  psql -X --username="$database_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --file=- >/dev/null <<'SQL'
BEGIN;

-- Content-only edit: retain every story-level field but replace the v1 body
-- and hash. The cleanup must preserve the entire story.
UPDATE story_versions AS version
SET markdown = E'# The Fox and the Grapes\n\nThis is legitimate replacement content.\n',
    rendered_html = '<h1>The Fox and the Grapes</h1><p>This is legitimate replacement content.</p>',
    content_hash = encode(digest(
      E'# The Fox and the Grapes\n\nThis is legitimate replacement content.\n',
      'sha256'
    ), 'hex')
FROM stories AS story
WHERE version.story_id = story.id
  AND version.version = 1
  AND story.slug = 'the-fox-and-the-grapes';

-- Additional draft version: model AdminDraftUpsert's new-version, generic
-- section, segment, and draft-pointer behaviour while leaving v1 published.
INSERT INTO story_versions (
  id, story_id, version, frontmatter, markdown, rendered_html, content_hash
)
SELECT
  'e13e0000-0000-4000-8000-000000000101',
  story.id,
  2,
  '{"title":"The Little Star and the Moon","author":"Panda Pages (seed)","language":"en-GB"}',
  E'# A legitimate new Little Star draft\n\nThe moon now follows a different path.\n',
  '<h1>A legitimate new Little Star draft</h1><p>The moon now follows a different path.</p>',
  encode(digest(
    E'# A legitimate new Little Star draft\n\nThe moon now follows a different path.\n',
    'sha256'
  ), 'hex')
FROM stories AS story
WHERE story.slug = 'the-little-star-and-the-moon';

INSERT INTO story_sections (id, story_version_id, kind, title, ordinal)
VALUES (
  'e13e0000-0000-4000-8000-000000000102',
  'e13e0000-0000-4000-8000-000000000101',
  'section',
  NULL,
  1
);

INSERT INTO story_segments (
  id, story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count
)
VALUES
  (
    'e13e0000-0000-4000-8000-000000000103',
    'e13e0000-0000-4000-8000-000000000101',
    'e13e0000-0000-4000-8000-000000000102',
    1,
    '{"type":"heading","h":1,"index":0}',
    '# A legitimate new Little Star draft',
    '<h1>A legitimate new Little Star draft</h1>',
    6
  ),
  (
    'e13e0000-0000-4000-8000-000000000104',
    'e13e0000-0000-4000-8000-000000000101',
    'e13e0000-0000-4000-8000-000000000102',
    2,
    '{"type":"para","n":1}',
    'The moon now follows a different path.',
    '<p>The moon now follows a different path.</p>',
    7
  );

UPDATE stories
SET draft_version_id = 'e13e0000-0000-4000-8000-000000000101'
WHERE slug = 'the-little-star-and-the-moon';

INSERT INTO reading_progress (
  profile_id, story_id, story_version_id, locator, percent
)
SELECT
  profile.id,
  story.id,
  'e13e0000-0000-4000-8000-000000000101',
  '{"type":"para","n":1}',
  0.61
FROM stories AS story
JOIN LATERAL (
  SELECT id
  FROM profiles
  WHERE account_id = story.account_id
    AND name = 'Default'
  ORDER BY created_at ASC, id ASC
  LIMIT 1
) AS profile ON true
WHERE story.slug = 'the-little-star-and-the-moon';

-- Additional published version: model a new draft followed by AdminPublish,
-- which leaves both pointers on v2 and keeps the story published.
INSERT INTO story_versions (
  id, story_id, version, frontmatter, markdown, rendered_html, content_hash
)
SELECT
  'e13e0000-0000-4000-8000-000000000201',
  story.id,
  2,
  '{"title":"Frankenstein (Excerpt — Chapter 1)","author":"Mary Shelley","language":"en-GB"}',
  E'# A legitimately republished excerpt\n\nThis replacement is intentionally short.\n',
  '<h1>A legitimately republished excerpt</h1><p>This replacement is intentionally short.</p>',
  encode(digest(
    E'# A legitimately republished excerpt\n\nThis replacement is intentionally short.\n',
    'sha256'
  ), 'hex')
FROM stories AS story
WHERE story.slug = 'frankenstein-excerpt-ch1';

INSERT INTO story_sections (id, story_version_id, kind, title, ordinal)
VALUES (
  'e13e0000-0000-4000-8000-000000000202',
  'e13e0000-0000-4000-8000-000000000201',
  'section',
  NULL,
  1
);

INSERT INTO story_segments (
  id, story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count
)
VALUES
  (
    'e13e0000-0000-4000-8000-000000000203',
    'e13e0000-0000-4000-8000-000000000201',
    'e13e0000-0000-4000-8000-000000000202',
    1,
    '{"type":"heading","h":1,"index":0}',
    '# A legitimately republished excerpt',
    '<h1>A legitimately republished excerpt</h1>',
    4
  ),
  (
    'e13e0000-0000-4000-8000-000000000204',
    'e13e0000-0000-4000-8000-000000000201',
    'e13e0000-0000-4000-8000-000000000202',
    2,
    '{"type":"para","n":1}',
    'This replacement is intentionally short.',
    '<p>This replacement is intentionally short.</p>',
    5
  );

UPDATE stories
SET draft_version_id = 'e13e0000-0000-4000-8000-000000000201',
    published_version_id = 'e13e0000-0000-4000-8000-000000000201',
    is_published = true
WHERE slug = 'frankenstein-excerpt-ch1';

INSERT INTO reading_progress (
  profile_id, story_id, story_version_id, locator, percent
)
SELECT
  profile.id,
  story.id,
  'e13e0000-0000-4000-8000-000000000201',
  '{"type":"para","n":1}',
  0.72
FROM stories AS story
JOIN LATERAL (
  SELECT id
  FROM profiles
  WHERE account_id = story.account_id
    AND name = 'Default'
  ORDER BY created_at ASC, id ASC
  LIMIT 1
) AS profile ON true
WHERE story.slug = 'frankenstein-excerpt-ch1';

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO child_profiles (id, account_id, name, age_months, interests, sensitivities)
SELECT 'a11e0000-0000-4000-8000-000000000002', id, 'Preserved child', 96, '["unrelated"]', '[]'
FROM target_account;

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO prompt_profiles (id, account_id, name, rules, schema_version)
SELECT 'a11e0000-0000-4000-8000-000000000003', id, 'Preserved prompt', '{"unrelated":true}', 7
FROM target_account;

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO profiles (id, account_id, name)
SELECT 'a11e0000-0000-4000-8000-000000000001', id, 'Preserved profile'
FROM target_account;

INSERT INTO profile_settings (profile_id, active_child_profile_id, active_prompt_profile_id)
VALUES (
  'a11e0000-0000-4000-8000-000000000001',
  'a11e0000-0000-4000-8000-000000000002',
  'a11e0000-0000-4000-8000-000000000003'
);

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
), fox_work AS (
  SELECT id FROM works WHERE canonical_title = 'The Fox and the Grapes' ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO stories (id, account_id, slug, title, author, language, source, work_id)
SELECT
  'a11e0000-0000-4000-8000-000000000010',
  target_account.id,
  'preserved-unrelated-story',
  'Preserved unrelated story',
  'Unrelated author',
  'en-GB',
  '{"origin":"fixture-migration-preservation-test"}',
  fox_work.id
FROM target_account, fox_work;

INSERT INTO story_versions (id, story_id, version, frontmatter, markdown, rendered_html, content_hash)
VALUES (
  'a11e0000-0000-4000-8000-000000000011',
  'a11e0000-0000-4000-8000-000000000010',
  1,
  '{"unrelated":true}',
  '# Preserved unrelated story',
  '<h1>Preserved unrelated story</h1>',
  encode(digest('# Preserved unrelated story', 'sha256'), 'hex')
);

UPDATE stories
SET published_version_id = 'a11e0000-0000-4000-8000-000000000011',
    draft_version_id = 'a11e0000-0000-4000-8000-000000000011',
    is_published = true
WHERE id = 'a11e0000-0000-4000-8000-000000000010';

INSERT INTO story_segments (id, story_version_id, ordinal, locator, markdown, rendered_html, word_count)
VALUES (
  'a11e0000-0000-4000-8000-000000000012',
  'a11e0000-0000-4000-8000-000000000011',
  1,
  '{"type":"heading","h":1,"index":0}',
  '# Preserved unrelated story',
  '<h1>Preserved unrelated story</h1>',
  4
);

INSERT INTO story_contributors (story_id, contributor_id, role)
SELECT
  'a11e0000-0000-4000-8000-000000000010',
  id,
  'source-author'
FROM contributors
WHERE name = 'Aesop';

INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent)
VALUES (
  'a11e0000-0000-4000-8000-000000000001',
  'a11e0000-0000-4000-8000-000000000010',
  'a11e0000-0000-4000-8000-000000000011',
  '{"type":"heading","h":1,"index":0}',
  0.42
);

INSERT INTO generation_jobs (
  id, status, story_id, story_version_id, child_profile_id, prompt_profile_id,
  theme, request_payload, response_payload, model, prompt_version
)
VALUES (
  'a11e0000-0000-4000-8000-000000000013',
  'queued',
  'a11e0000-0000-4000-8000-000000000010',
  'a11e0000-0000-4000-8000-000000000011',
  'a11e0000-0000-4000-8000-000000000002',
  'a11e0000-0000-4000-8000-000000000003',
  'preserved unrelated job',
  '{"unrelated":true}',
  '{}',
  'preserved-model',
  'preserved-v1'
);

COMMIT;
SQL

run_goose up-to 13 >"$test_root/preservation-cleanup-goose.out" 2>"$test_root/preservation-cleanup-goose.err"

assert_query '1|1|3|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-fox-and-the-grapes' AND version.content_hash <> '2fb47f4013a00e9348fa92c2da31ee0688e3a8ef878fa4308646b1656c9adad4');
" 'content-edited fixture preservation'
printf 'ok 5 - content-only edits preserve the story, version, segments, contributor link, and progress\n'

assert_query '1|2|1|5|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug = 'the-little-star-and-the-moon'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-little-star-and-the-moon'),
    (SELECT count(*) FROM story_sections WHERE story_version_id = 'e13e0000-0000-4000-8000-000000000101'),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-little-star-and-the-moon'),
    (SELECT count(*) FROM stories WHERE slug = 'the-little-star-and-the-moon' AND draft_version_id = 'e13e0000-0000-4000-8000-000000000101'),
    (SELECT count(*) FROM stories AS story JOIN story_versions AS version ON version.id = story.published_version_id WHERE story.slug = 'the-little-star-and-the-moon' AND version.version = 1),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug = 'the-little-star-and-the-moon' AND progress.story_version_id = 'e13e0000-0000-4000-8000-000000000101' AND round(progress.percent::numeric, 2) = 0.61);
" 'additional draft preservation'
printf 'ok 6 - an additional draft preserves both versions, every segment, both pointers, and progress\n'

assert_query '1|2|2|5|1|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM story_sections AS section JOIN story_versions AS version ON version.id = section.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM stories WHERE slug = 'frankenstein-excerpt-ch1' AND draft_version_id = 'e13e0000-0000-4000-8000-000000000201'),
    (SELECT count(*) FROM stories WHERE slug = 'frankenstein-excerpt-ch1' AND published_version_id = 'e13e0000-0000-4000-8000-000000000201' AND is_published),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug = 'frankenstein-excerpt-ch1' AND progress.story_version_id = 'e13e0000-0000-4000-8000-000000000201' AND round(progress.percent::numeric, 2) = 0.72);
" 'republished fixture preservation'
printf 'ok 7 - a republished additional version preserves the whole story lifecycle and progress\n'

assert_query '1|1|1|1|1|1|1|1|1|1|1|1|1|1' "
  SELECT
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles WHERE name = 'Default'),
    (SELECT count(*) FROM profiles WHERE id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM child_profiles WHERE id = 'a11e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'a11e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001' AND active_child_profile_id = 'a11e0000-0000-4000-8000-000000000002' AND active_prompt_profile_id = 'a11e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'a11e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE id = 'a11e0000-0000-4000-8000-000000000012'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM generation_jobs WHERE id = 'a11e0000-0000-4000-8000-000000000013'),
    (SELECT count(*) FROM works WHERE canonical_title = 'The Fox and the Grapes'),
    (SELECT count(*) FROM contributors WHERE name = 'Aesop'),
    (SELECT count(*) FROM story_contributors WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'upgrade preservation'
printf 'ok 8 - shared account/catalogue rows and unrelated stories, settings, progress, and jobs are preserved\n'

run_goose down-to 12 >"$test_root/cleanup-down.out" 2>"$test_root/cleanup-down.err"
run_goose up-to 13 >"$test_root/cleanup-rerun.out" 2>"$test_root/cleanup-rerun.err"
assert_query '3|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'cleanup rerun'
printf 'ok 9 - non-restoring rollback and cleanup rerun preserve ambiguous stories idempotently\n'

run_goose up-to 14 >"$test_root/reader2-upgrade.out" 2>"$test_root/reader2-upgrade.err"
assert_query '0|0' "
  SELECT
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM information_schema.columns
       WHERE table_schema = 'public'
         AND table_name = 'story_segments'
         AND column_name = 'locator');
" 'Reader 2 fixture boundary'
printf 'ok 10 - Reader 2 upgrade resets beta progress and removes the obsolete segment locator\n'

expect_seed_failure missing-ack \
  env -u PP_ALLOW_TEST_SEED \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script"
expect_seed_failure empty-database \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE= \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script"
expect_seed_failure unexpected-database \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE=postgres \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script"
expect_seed_failure remote-docker \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    DOCKER_HOST=tcp://example.invalid:2375 \
    "$seed_script"
expect_seed_failure missing-container \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER=missing-fixture-integration-container \
    "$seed_script"
expect_seed_failure malformed-invocation \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script" --unknown
printf 'ok 11 - seed command fails closed for acknowledgement, target, Docker, service, and invocation errors\n'

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >"$test_root/seed.out"
grep -q '^test_seed=installed progress=absent target=local_or_disposable$' "$test_root/seed.out"
assert_query '1|1|1|1|1|1|2|6|1|0' "
  SELECT
    (SELECT count(*) FROM child_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM works WHERE id = 'f17e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM contributors WHERE id = 'f17e0000-0000-4000-8000-000000000004'),
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010' AND is_published),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_sections WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM generation_jobs WHERE id = 'f17e0000-0000-4000-8000-000000000040'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010');
" 'explicit seed inventory'
printf 'ok 12 - explicit seed installs deterministic published UTF-8 chapter/segment fixtures without progress\n'

assert_query '6|3|3|2|2|2|6|3|3|t|## Chapter Two — 世界|<h2>Chapter Two — 世界</h2>|星の光 shimmered over the quiet water. 🐼|<p>星の光 shimmered over the quiet water. 🐼</p>|t' "
  SELECT
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'heading'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'paragraph'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND ordinal IN (1, 2) AND chapter_key IS NULL AND chapter_occurrence IS NULL),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND ordinal IN (3, 4) AND chapter_key = '6f744b440fbf4fa52da46bebf4fd3e5f2de7a1c2fb11f7e9ac2794ccd1956c4e' AND chapter_occurrence = 1),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND ordinal IN (5, 6) AND chapter_key = '3749b6630ab08c6998fd65117d5265c7e7514e35f02022a4005505d0aba52a73' AND chapter_occurrence = 1),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND content_occurrence = 1),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'heading' AND heading_level IN (1, 2)),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'paragraph' AND heading_level IS NULL),
    (SELECT bool_and(content_key ~ '^[0-9a-f]{64}$') FROM story_segments WHERE story_version_id = heading.story_version_id),
    heading.markdown,
    heading.rendered_html,
    paragraph.markdown,
    paragraph.rendered_html,
    heading.section_id = paragraph.section_id
      AND heading.section_id = 'f17e0000-0000-4000-8000-000000000021'
  FROM story_segments AS heading
  JOIN story_segments AS paragraph
    ON paragraph.story_version_id = heading.story_version_id
   AND paragraph.ordinal = 6
  WHERE heading.story_version_id = 'f17e0000-0000-4000-8000-000000000011'
    AND heading.ordinal = 5;
" 'explicit fixture ingestion segment shape'
printf 'ok 13 - canonical keys, kinds, chapter propagation, and six independent fixture segments match ingestion\n'

api_environment="$test_root/api.env"
{
  printf 'DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
    "$database_user" "$database_password" "$postgres_container" "$database"
  printf 'PGAPPNAME=pandapages-api\n'
  printf 'PP_PASSCODE=123456\n'
  printf 'PP_ADMIN_KEY=generated-admin-key-not-for-production\n'
  printf 'PP_SESSION_SECRET=generated-session-secret-not-for-production-00000000\n'
} >"$api_environment"
chmod 0600 "$api_environment"

docker run --detach \
  --name "$api_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
  --env-file "$api_environment" \
  --publish 127.0.0.1:0:8080 \
  "$api_image" >/dev/null

api_status=$(docker inspect --format '{{.State.Status}}' "$api_container")
[[ "$api_status" == running ]] || {
  printf 'Fixture API exited during startup\n' >&2
  docker logs "$api_container" 2>&1 \
    | redact_diagnostics >&2
  exit 1
}

api_address=$(docker port "$api_container" 8080/tcp)
api_port=${api_address##*:}
[[ "$api_port" =~ ^[0-9]+$ ]]
api_base="http://127.0.0.1:$api_port"

api_ready=false
for ((attempt = 0; attempt < 60; attempt++)); do
  if curl --fail --silent --show-error "$api_base/healthz" >/dev/null 2>&1; then
    api_ready=true
    break
  fi
  sleep 1
done
[[ "$api_ready" == true ]] || {
  printf 'Fixture API did not become healthy\n' >&2
  exit 1
}

cookie_jar="$test_root/api.cookies"
curl --fail --silent --show-error \
  --cookie-jar "$cookie_jar" \
  --header 'Content-Type: application/json' \
  --data '{"passcode":"123456"}' \
  "$api_base/api/v1/auth/unlock" >/dev/null
curl --fail --silent --show-error \
  --cookie "$cookie_jar" \
  "$api_base/api/v1/reader/test-only-moonlit-cafe" \
  >"$test_root/reader.json"
grep -q 'Moonlit Café' "$test_root/reader.json"
grep -q 'Pöndá' "$test_root/reader.json"
grep -q '世界' "$test_root/reader.json"
grep -q '星の光' "$test_root/reader.json"
if grep -q 'markdown\|f17e0000-0000-4000-8000-0000000000' "$test_root/reader.json"; then
  printf 'Reader response exposed Markdown or internal IDs\n' >&2
  exit 1
fi
printf 'ok 14 - the signed-session coherent Reader endpoint returns six UTF-8 segments without internal content\n'

docker rm --force "$api_container" >/dev/null

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >/dev/null
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --with-progress >/dev/null
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --with-progress >/dev/null
assert_query '1|1|6|2|4|0.35|0.6' "
  SELECT
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT locator->>'schema' FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT locator->'segment'->>'ordinal' FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT locator->'segment'->>'offset' FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT percent FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010');
" 'seed idempotency and opt-in progress'
printf 'ok 15 - repeated seed and valid Locator v2 progress requests are idempotent and opt-in only\n'

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --remove >/dev/null
assert_query '0|0|0|0|0|0|0|0|0|0|0|1|1|0' "
  SELECT
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_sections WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM child_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM works WHERE id = 'f17e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM contributors WHERE id = 'f17e0000-0000-4000-8000-000000000004'),
    (SELECT count(*) FROM generation_jobs WHERE id = 'f17e0000-0000-4000-8000-000000000040'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_contributors WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'explicit fixture removal preservation'
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >/dev/null
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --remove >/dev/null
printf 'ok 16 - fixture removal and recreation touch only fixed test IDs and preserve unrelated data\n'

controlled_suffix="$$-controlled"
controlled_prefix="pandapages-fixture-integration-$controlled_suffix"
controlled_label="com.pandapages.fixture-run=$controlled_prefix"
controlled_root="$test_root/controlled-failure"
controlled_stdout="$controlled_root/stdout"
controlled_stderr="$controlled_root/stderr"
controlled_diagnostic_secret="fixture-secret-${resource_suffix}-${RANDOM}-${RANDOM}"
controlled_diagnostic_password="fixture-password-${resource_suffix}-${RANDOM}-${RANDOM}"
mkdir -p "$controlled_root"

launch_controlled_process "$controlled_stdout" "$controlled_stderr" \
  env \
  -u PP_FIXTURE_TEST_PROCESS_HELPER_MODE \
  -u PP_FIXTURE_TEST_PROCESS_HELPER_READY \
  -u PP_FIXTURE_TEST_PROCESS_HELPER_TERM_SEEN \
  -u PP_FIXTURE_TEST_PROCESS_HELPER_MARKER \
  PP_FIXTURE_TEST_RESOURCE_SUFFIX="$controlled_suffix" \
  PP_FIXTURE_TEST_CONTROLLED_GOOSE_FAILURE_STATUS=37 \
  PP_FIXTURE_TEST_CONTROLLED_DIAGNOSTIC_SECRET="$controlled_diagnostic_secret" \
  PP_FIXTURE_TEST_CONTROLLED_DIAGNOSTIC_PASSWORD="$controlled_diagnostic_password" \
  TMPDIR="$controlled_root" \
  bash -x "${BASH_SOURCE[0]}"
if ! wait_for_controlled_child; then
  printf 'Controlled fixture migration process-group cleanup was incomplete\n' >&2
  exit 1
fi
controlled_status=$controlled_child_status

if ! cleanup_labeled_resources "$controlled_label"; then
  printf 'Controlled fixture migration resource cleanup was incomplete\n' >&2
  exit 1
fi
verify_no_labeled_resources "$controlled_label" 'controlled child'

[[ "$controlled_status" == 37 ]] || {
  printf 'Controlled fixture migration failure status = %s, want 37\n' "$controlled_status" >&2
  exit 1
}
grep -q '^1\.\.18$' "$controlled_stdout"
grep -q 'Fixture integration failed at line .* (status 37)' "$controlled_stderr"
grep -q 'fresh-goose.err .*redacted' "$controlled_stderr"
grep -q 'Controlled fixture migration failure for harness regression (status 37)' "$controlled_stderr"
grep -Fq '+ set +x' "$controlled_stderr"
grep -Fq 'Controlled fixture diagnostic marker: disposable synthetic input' "$controlled_stderr"
grep -Fq '[redacted-controlled-secret]' "$controlled_stderr"
grep -Fq 'Synthetic fixture password: [redacted]' "$controlled_stderr"
grep -Fq '[redacted-database-url]' "$controlled_stderr"
if grep -Fq "$controlled_diagnostic_secret" "$controlled_stdout" "$controlled_stderr" ||
  grep -Fq "$controlled_diagnostic_password" "$controlled_stdout" "$controlled_stderr" ||
  grep -Fq "$database_password" "$controlled_stdout" "$controlled_stderr" ||
  grep -Eq 'postgres://[^[:space:]]+' "$controlled_stdout" "$controlled_stderr" ||
  grep -Eiq 'postgres(%3A|%3a)(%2F|%2f)(%2F|%2f)[^[:space:]]+' \
    "$controlled_stdout" "$controlled_stderr"; then
  printf 'Controlled fixture diagnostics exposed credentials or a database URL\n' >&2
  exit 1
fi
if compgen -G "$controlled_root/pandapages-fixture-integration.*" >/dev/null; then
  printf 'Controlled fixture migration left temporary artifacts behind\n' >&2
  exit 1
fi

fake_list_stderr="$controlled_root/fake-list.stderr"
fake_inspect_stderr="$controlled_root/fake-inspect.stderr"
fake_sensitive_stderr="$controlled_root/fake-sensitive.stderr"
(
  fake_docker_mode=''
  docker() {
    case "$fake_docker_mode" in
      list-empty-failure)
        return 71
        ;;
      inspect-empty-failure)
        return 72
        ;;
      sensitive-failure)
        printf 'synthetic daemon secret=%s url=postgres://fixture:%s@fixture.invalid:5432/disposable\n' \
          "$controlled_diagnostic_secret" "$controlled_diagnostic_secret" >&2
        return 73
        ;;
      success-empty)
        return 0
        ;;
      *)
        return 74
        ;;
    esac
  }

  fake_docker_mode=list-empty-failure
  if list_labeled_resource_ids container "$controlled_label" \
    >/dev/null 2>"$fake_list_stderr"; then
    printf 'Empty-output Docker list failure was treated as resource absence\n' >&2
    exit 1
  else
    fake_status=$?
  fi
  [[ "$fake_status" == 71 ]]
  grep -Fq 'Docker command failed while listing disposable fixture containers (status 71)' \
    "$fake_list_stderr"

  fake_docker_mode=inspect-empty-failure
  if inspect_container_name synthetic-container \
    >/dev/null 2>"$fake_inspect_stderr"; then
    printf 'Empty-output Docker inspect failure was treated as success\n' >&2
    exit 1
  else
    fake_status=$?
  fi
  [[ "$fake_status" == 72 ]]
  grep -Fq 'Docker command failed while inspecting a disposable fixture container (status 72)' \
    "$fake_inspect_stderr"

  fake_docker_mode=sensitive-failure
  if list_labeled_resource_ids container "$controlled_label" \
    >/dev/null 2>"$fake_sensitive_stderr"; then
    printf 'Synthetic sensitive Docker failure unexpectedly succeeded\n' >&2
    exit 1
  else
    fake_status=$?
  fi
  [[ "$fake_status" == 73 ]]
  grep -Fq '[redacted-controlled-secret]' "$fake_sensitive_stderr"
  grep -Fq '[redacted-database-url]' "$fake_sensitive_stderr"
  if grep -Fq "$controlled_diagnostic_secret" "$fake_sensitive_stderr" ||
    grep -Eq 'postgres://[^[:space:]]+' "$fake_sensitive_stderr"; then
    printf 'Synthetic Docker failure diagnostics were not redacted\n' >&2
    exit 1
  fi

  fake_docker_mode=success-empty
  verify_no_labeled_resources "$controlled_label" 'fake controlled child'
)

fake_cleanup_root="$controlled_root/fake-cleanup-root"
fake_cleanup_stderr="$controlled_root/fake-cleanup.stderr"
if (
  trap - EXIT ERR HUP INT TERM
  test_root="$fake_cleanup_root"
  controlled_root=''
  controlled_label=''
  controlled_pid=''
  failure_line='synthetic'
  failure_signal=''
  docker() {
    return 71
  }
  mkdir -p "$test_root"
  trap on_exit EXIT
  exit 37
) >/dev/null 2>"$fake_cleanup_stderr"; then
  fake_cleanup_status=0
else
  fake_cleanup_status=$?
fi
[[ "$fake_cleanup_status" == 37 ]]
grep -Fq 'Docker command failed while listing disposable fixture containers (status 71)' \
  "$fake_cleanup_stderr"
grep -Fq 'Fixture integration diagnostics were incomplete' "$fake_cleanup_stderr"
grep -Fq 'Fixture integration cleanup was incomplete' "$fake_cleanup_stderr"
[[ ! -e "$fake_cleanup_root" ]]

[[ $(combined_exit_status 37 71) == 37 ]]
[[ $(combined_exit_status 41 71) == 41 ]]
[[ $(combined_exit_status 137 71) == 137 ]]
[[ $(combined_exit_status 143 71) == 143 ]]
[[ $(combined_exit_status 0 71) == 71 ]]

launch_process_helper() {
  local mode=$1
  local ready_file=$2
  local stdout_file=$3
  local stderr_file=$4
  local term_seen_file=${5:-}
  local marker_file=${6:-}

  launch_controlled_process "$stdout_file" "$stderr_file" \
    env \
    -u PP_FIXTURE_TEST_CONTROLLED_GOOSE_FAILURE_STATUS \
    -u PP_FIXTURE_TEST_CONTROLLED_DIAGNOSTIC_SECRET \
    -u PP_FIXTURE_TEST_CONTROLLED_DIAGNOSTIC_PASSWORD \
    PP_FIXTURE_TEST_PROCESS_HELPER_MODE="$mode" \
    PP_FIXTURE_TEST_PROCESS_HELPER_READY="$ready_file" \
    PP_FIXTURE_TEST_PROCESS_HELPER_TERM_SEEN="$term_seen_file" \
    PP_FIXTURE_TEST_PROCESS_HELPER_MARKER="$marker_file" \
    TMPDIR="$test_root" \
    "${BASH_SOURCE[0]}"
}

wait_for_process_helper_ready() {
  local ready_file=$1
  local pgid=$2
  local attempt

  helper_descendant_pid=''
  for ((attempt = 0; attempt < 1000; attempt++)); do
    if [[ -s "$ready_file" ]]; then
      IFS= read -r helper_descendant_pid <"$ready_file"
      [[ "$helper_descendant_pid" =~ ^[1-9][0-9]*$ ]] && return 0
      break
    fi
    if ! process_group_exists "$pgid"; then
      break
    fi
    sleep 0.01
  done
  printf 'Controlled fixture process helper did not become ready\n' >&2
  return 1
}

assert_controlled_processes_absent() {
  local leader_pid=$1
  local pgid=$2
  local descendant_pid=${3:-}
  local description=$4
  local group_status

  if process_group_exists "$pgid"; then
    printf '%s left its isolated process group running\n' "$description" >&2
    return 1
  else
    group_status=$?
  fi
  if ((group_status != 1)); then
    printf '%s had an invalid isolated process-group identity\n' "$description" >&2
    return 1
  fi
  if kill -0 "$leader_pid" 2>/dev/null; then
    printf '%s left its controlled leader running\n' "$description" >&2
    return 1
  fi
  if [[ -n "$descendant_pid" ]] && kill -0 "$descendant_pid" 2>/dev/null; then
    printf '%s left its foreground descendant running\n' "$description" >&2
    return 1
  fi
}

launch_identity_root="$controlled_root/launch-identity-root"
launch_identity_file="$controlled_root/launch-identity.identity"
launch_identity_stderr="$controlled_root/launch-identity.stderr"
if (
  trap - EXIT ERR HUP INT TERM
  test_root="$launch_identity_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-launch-identity-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_sid=''
  controlled_sentinel_pid=''
  controlled_sentinel_start_time=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  controlled_launch_test_signal=''
  controlled_launch_test_identity_failure=1
  controlled_launch_test_identity_file="$launch_identity_file"
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  launch_identity_ready="$test_root/unused.ready"
  if launch_process_helper early-exit "$launch_identity_ready" \
    "$test_root/early.stdout" "$test_root/early.stderr"; then
    printf 'Controlled launch-identity failure unexpectedly succeeded\n' >&2
    exit 1
  else
    launch_identity_status=$?
  fi
  [[ "$launch_identity_status" == 1 ]]
  controlled_launch_test_identity_failure=0
  controlled_launch_test_identity_file=''
  cleanup
  cleanup
) 2>"$launch_identity_stderr"; then
  launch_identity_test_status=0
else
  launch_identity_test_status=$?
fi
[[ "$launch_identity_test_status" == 0 ]]
grep -Fq 'Could not establish an isolated controlled fixture process group' \
  "$launch_identity_stderr"
read -r launch_identity_leader launch_identity_pgid launch_identity_sentinel \
  <"$launch_identity_file"
assert_controlled_processes_absent \
  "$launch_identity_leader" "$launch_identity_pgid" "$launch_identity_sentinel" \
  'Launch-identity failure regression'
[[ ! -e "$launch_identity_root" ]]

launch_signal_root="$controlled_root/launch-signal-root"
launch_signal_identity_file="$controlled_root/launch-signal.identity"
launch_signal_stderr="$controlled_root/launch-signal.stderr"
if (
  trap - EXIT ERR HUP INT TERM
  test_root="$launch_signal_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-launch-signal-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  controlled_launch_test_signal=TERM
  controlled_launch_test_identity_file="$launch_signal_identity_file"
  failure_line=''
  failure_signal=''
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  trap on_exit EXIT
  trap 'on_signal TERM 143' TERM
  launch_signal_ready="$test_root/foreground.ready"
  launch_process_helper foreground "$launch_signal_ready" \
    "$test_root/foreground.stdout" "$test_root/foreground.stderr"
  printf 'Launch-window TERM regression unexpectedly resumed after signal replay\n' >&2
  exit 1
) >/dev/null 2>"$launch_signal_stderr"; then
  launch_signal_status=0
else
  launch_signal_status=$?
fi
[[ "$launch_signal_status" == 143 ]]
grep -Fq 'Fixture integration interrupted by TERM (status 143)' "$launch_signal_stderr"
read -r launch_signal_leader launch_signal_pgid <"$launch_signal_identity_file"
assert_controlled_processes_absent \
  "$launch_signal_leader" "$launch_signal_pgid" '' \
  'Launch-window TERM regression'
[[ ! -e "$launch_signal_root" ]]

parent_term_root="$controlled_root/parent-term-root"
parent_term_identity_file="$controlled_root/parent-term.identity"
parent_term_stderr="$controlled_root/parent-term.stderr"
if (
  trap - EXIT ERR HUP INT TERM
  test_root="$parent_term_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-parent-term-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  controlled_termination_grace_seconds=1
  failure_line=''
  failure_signal=''
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  trap on_exit EXIT
  trap 'on_signal TERM 143' TERM
  parent_term_ready="$test_root/foreground.ready"
  launch_process_helper foreground "$parent_term_ready" \
    "$test_root/foreground.stdout" "$test_root/foreground.stderr"
  parent_term_leader=$controlled_pid
  parent_term_pgid=$controlled_pgid
  wait_for_process_helper_ready "$parent_term_ready" "$parent_term_pgid"
  printf '%s %s %s\n' \
    "$parent_term_leader" "$parent_term_pgid" "$helper_descendant_pid" \
    >"$parent_term_identity_file"
  kill -TERM "$BASHPID"
) >/dev/null 2>"$parent_term_stderr"; then
  parent_term_status=0
else
  parent_term_status=$?
fi
[[ "$parent_term_status" == 143 ]]
grep -Fq 'Fixture integration interrupted by TERM (status 143)' "$parent_term_stderr"
read -r parent_term_leader parent_term_pgid parent_term_descendant \
  <"$parent_term_identity_file"
assert_controlled_processes_absent \
  "$parent_term_leader" "$parent_term_pgid" "$parent_term_descendant" \
  'Parent TERM regression'
[[ ! -e "$parent_term_root" ]]

delayed_docker_root="$controlled_root/delayed-docker-root"
delayed_docker_identity_file="$controlled_root/delayed-docker.identity"
(
  trap - EXIT ERR HUP INT TERM
  test_root="$delayed_docker_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-delayed-docker-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_sid=''
  controlled_sentinel_pid=''
  controlled_sentinel_start_time=''
  controlled_child_status=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  controlled_termination_grace_seconds=1
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  delayed_docker_ready="$test_root/delayed-docker.ready"
  delayed_docker_term_seen="$test_root/delayed-docker.term-seen"
  delayed_docker_marker="$test_root/delayed-docker.resource"
  launch_process_helper delayed-docker "$delayed_docker_ready" \
    "$test_root/delayed-docker.stdout" "$test_root/delayed-docker.stderr" \
    "$delayed_docker_term_seen" "$delayed_docker_marker"
  delayed_docker_leader=$controlled_pid
  delayed_docker_pgid=$controlled_pgid
  wait_for_process_helper_ready "$delayed_docker_ready" "$delayed_docker_pgid"
  printf '%s %s %s\n' \
    "$delayed_docker_leader" "$delayed_docker_pgid" "$helper_descendant_pid" \
    >"$delayed_docker_identity_file"
  terminate_controlled_child
  [[ "$controlled_child_status" == 137 ]] || {
    printf 'Delayed fake-Docker child status = %s, want 137\n' \
      "$controlled_child_status" >&2
    exit 1
  }
  grep -Fq 'TERM received before fake Docker create' "$delayed_docker_term_seen"
  [[ ! -e "$delayed_docker_marker" ]] || {
    printf 'Delayed fake Docker created a labelled resource after group cleanup\n' >&2
    exit 1
  }
  cleanup
  cleanup
)
read -r delayed_docker_leader delayed_docker_pgid delayed_docker_descendant \
  <"$delayed_docker_identity_file"
assert_controlled_processes_absent \
  "$delayed_docker_leader" "$delayed_docker_pgid" "$delayed_docker_descendant" \
  'Delayed fake-Docker regression'
[[ ! -e "$delayed_docker_root" ]]

terminated_child_root="$controlled_root/terminated-child-root"
terminated_child_identity_file="$controlled_root/terminated-child.identity"
(
  trap - EXIT ERR HUP INT TERM
  test_root="$terminated_child_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-terminated-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_child_status=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  terminated_child_ready="$test_root/foreground.ready"
  launch_process_helper foreground "$terminated_child_ready" \
    "$test_root/foreground.stdout" "$test_root/foreground.stderr"
  terminated_child_leader=$controlled_pid
  terminated_child_pgid=$controlled_pgid
  wait_for_process_helper_ready "$terminated_child_ready" "$terminated_child_pgid"
  printf '%s %s %s\n' \
    "$terminated_child_leader" "$terminated_child_pgid" "$helper_descendant_pid" \
    >"$terminated_child_identity_file"
  terminate_controlled_child
  [[ "$controlled_child_status" == 143 ]] || {
    printf 'Terminated-child status = %s, want 143\n' "$controlled_child_status" >&2
    exit 1
  }
  cleanup
  cleanup
)
read -r terminated_child_leader terminated_child_pgid terminated_child_descendant \
  <"$terminated_child_identity_file"
assert_controlled_processes_absent \
  "$terminated_child_leader" "$terminated_child_pgid" "$terminated_child_descendant" \
  'Terminated-child regression'
[[ ! -e "$terminated_child_root" ]]

group_verify_root="$controlled_root/group-verify-root"
group_verify_identity_file="$controlled_root/group-verify.identity"
group_verify_stderr="$controlled_root/group-verify.stderr"
if (
  trap - EXIT ERR HUP INT TERM
  test_root="$group_verify_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-group-verify-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_sid=''
  controlled_sentinel_pid=''
  controlled_sentinel_start_time=''
  controlled_child_status=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  controlled_process_group_verify_failure=0
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  group_verify_ready="$test_root/foreground.ready"
  launch_process_helper foreground "$group_verify_ready" \
    "$test_root/foreground.stdout" "$test_root/foreground.stderr"
  group_verify_leader=$controlled_pid
  group_verify_pgid=$controlled_pgid
  wait_for_process_helper_ready "$group_verify_ready" "$group_verify_pgid"
  printf '%s %s %s\n' \
    "$group_verify_leader" "$group_verify_pgid" "$helper_descendant_pid" \
    >"$group_verify_identity_file"
  controlled_process_group_verify_failure=1
  if terminate_controlled_child; then
    printf 'Controlled process-group verification failure unexpectedly passed\n' >&2
    exit 1
  else
    group_verify_status=$?
  fi
  [[ "$group_verify_status" == 1 ]]
  [[ "$controlled_child_status" == 143 ]]
  controlled_process_group_verify_failure=0
  terminate_controlled_child
  cleanup
  cleanup
) 2>"$group_verify_stderr"; then
  group_verify_test_status=0
else
  group_verify_test_status=$?
fi
[[ "$group_verify_test_status" == 0 ]]
grep -Fq 'Controlled process-group verification failure for harness regression' \
  "$group_verify_stderr"
read -r group_verify_leader group_verify_pgid group_verify_descendant \
  <"$group_verify_identity_file"
assert_controlled_processes_absent \
  "$group_verify_leader" "$group_verify_pgid" "$group_verify_descendant" \
  'Process-group verification-failure regression'
[[ ! -e "$group_verify_root" ]]

killed_child_root="$controlled_root/killed-child-root"
killed_child_identity_file="$controlled_root/killed-child.identity"
(
  trap - EXIT ERR HUP INT TERM
  test_root="$killed_child_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-killed-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_child_status=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  killed_child_ready="$test_root/foreground.ready"
  launch_process_helper foreground "$killed_child_ready" \
    "$test_root/foreground.stdout" "$test_root/foreground.stderr"
  killed_child_leader=$controlled_pid
  killed_child_pgid=$controlled_pgid
  wait_for_process_helper_ready "$killed_child_ready" "$killed_child_pgid"
  printf '%s %s %s\n' \
    "$killed_child_leader" "$killed_child_pgid" "$helper_descendant_pid" \
    >"$killed_child_identity_file"
  kill -KILL "$controlled_pid"
  wait_for_controlled_child
  [[ "$controlled_child_status" == 137 ]] || {
    printf 'Killed-child status = %s, want 137\n' "$controlled_child_status" >&2
    exit 1
  }
  cleanup
  cleanup
)
read -r killed_child_leader killed_child_pgid killed_child_descendant \
  <"$killed_child_identity_file"
assert_controlled_processes_absent \
  "$killed_child_leader" "$killed_child_pgid" "$killed_child_descendant" \
  'Killed-child regression'
[[ ! -e "$killed_child_root" ]]

early_child_root="$controlled_root/early-child-root"
early_child_identity_file="$controlled_root/early-child.identity"
(
  trap - EXIT ERR HUP INT TERM
  test_root="$early_child_root"
  controlled_root="$test_root/recursive-child"
  controlled_label='com.pandapages.fixture-run=synthetic-early-child'
  controlled_pid=''
  controlled_pgid=''
  controlled_child_status=''
  controlled_identity_root=''
  controlled_launching=0
  controlled_pending_signal=''
  controlled_pending_status=''
  docker() {
    return 0
  }
  mkdir -p "$controlled_root"
  early_child_ready="$test_root/unused.ready"
  launch_process_helper early-exit "$early_child_ready" \
    "$test_root/early.stdout" "$test_root/early.stderr"
  early_child_leader=$controlled_pid
  early_child_pgid=$controlled_pgid
  printf '%s %s\n' "$early_child_leader" "$early_child_pgid" \
    >"$early_child_identity_file"
  wait_for_controlled_child
  [[ "$controlled_child_status" == 41 ]] || {
    printf 'Early-child status = %s, want 41\n' "$controlled_child_status" >&2
    exit 1
  }
  cleanup
  cleanup
)
read -r early_child_leader early_child_pgid <"$early_child_identity_file"
assert_controlled_processes_absent \
  "$early_child_leader" "$early_child_pgid" '' 'Early-exit regression'
[[ ! -e "$early_child_root" ]]

if grep -RFq "$controlled_diagnostic_secret" "$controlled_root" ||
  grep -RFq "$controlled_diagnostic_password" "$controlled_root" ||
  grep -RFq "$database_password" "$controlled_root" ||
  grep -REq 'postgres://[^[:space:]]+' "$controlled_root" ||
  grep -REiq 'postgres(%3A|%3a)(%2F|%2f)(%2F|%2f)[^[:space:]]+' \
    "$controlled_root"; then
  printf 'A surviving controlled fixture artifact exposed a credential or database URL\n' >&2
  exit 1
fi

printf 'ok 17 - controlled failures preserve status and redacted diagnostics while abnormal children and Docker query failures clean up fail-closed\n'

cleanup
trap - EXIT ERR HUP INT TERM
verify_no_labeled_resources "$run_label" 'fixture integration'
verify_no_labeled_resources "$controlled_label" 'controlled child'
[[ ! -e "$test_root" ]]
printf 'ok 18 - disposable containers, network, volume, credentials, and artifacts are removed\n'
printf 'postgresql_fixtures_integration=passed\n'
