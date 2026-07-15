#!/usr/bin/env bash

set -euo pipefail
umask 077

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
readonly session_cookie_contract="$script_dir/lib/postgresql-api-role-session-cookie.awk"

usage() {
  cat <<'EOF'
Usage:
  scripts/postgresql-api-role-verify.sh \
    --api-container NAME \
    --postgres-container NAME \
    --database NAME \
    [--session-contract legacy|signed] \
    [--admin-user ROLE] \
    [--application-role ROLE] \
    [--application-name NAME] \
    [--legacy-role ROLE] \
    [--owner-role ROLE] \
    [--migration-role ROLE] \
    [--backup-role ROLE] \
    [--max-activity-age-seconds SECONDS]

Verifies that the running Panda Pages API is healthy, establishes a protected
session through the unlock endpoint, completes an authenticated library read,
and has a recent PostgreSQL connection on its unique shared Docker network
using only the expected application role, database, and application name.

Session contract selection is explicit when supplied. Auto-detection is not
performed. The legacy compatibility default preserves the currently deployed
pp_unlocked and pp_aid cookie contract; signed mode requires pp_session.

The verifier accepts no database URL, password, cookie, client address, or
other secret. It discovers the API address from Docker metadata and never
places that address or another caller-controlled value into SQL.

Defaults:
  --session-contract             legacy
  --admin-user                   pandapages
  --application-role            pandapages_app
  --application-name            pandapages-api
  --legacy-role                 pandapages
  --owner-role                  pandapages_owner
  --migration-role              pandapages_migrator
  --backup-role                 pandapages_backup
  --max-activity-age-seconds    30
EOF
}

die() {
  printf 'postgresql-api-role-verify: %s\n' "$*" >&2
  exit 1
}

hex_value() {
  printf '%s' "$1" | od -An -v -tx1 | tr -d ' \n'
}

valid_ipv4() {
  local address=$1
  local part
  local -a parts=()

  IFS=. read -r -a parts <<<"$address"
  ((${#parts[@]} == 4)) || return 1
  for part in "${parts[@]}"; do
    [[ "$part" =~ ^[0-9]{1,3}$ ]] || return 1
    ((10#$part <= 255)) || return 1
  done
}

[[ ${1:-} != -h && ${1:-} != --help ]] || {
  usage
  exit 0
}

api_container=""
postgres_container=""
database=""
admin_user=pandapages
application_role=pandapages_app
application_name=pandapages-api
legacy_role=pandapages
owner_role=pandapages_owner
migration_role=pandapages_migrator
backup_role=pandapages_backup
max_activity_age_seconds=30
session_contract=legacy

while (($# > 0)); do
  case "$1" in
    --api-container)
      api_container=${2:-}
      shift 2
      ;;
    --postgres-container)
      postgres_container=${2:-}
      shift 2
      ;;
    --database)
      database=${2:-}
      shift 2
      ;;
    --session-contract)
      (($# >= 2)) || die '--session-contract requires an argument'
      [[ -n ${2:-} ]] || die '--session-contract requires a nonempty argument'
      case "$2" in
        legacy|signed)
          session_contract=$2
          ;;
        *)
          die '--session-contract must be exactly legacy or signed'
          ;;
      esac
      shift 2
      ;;
    --admin-user)
      admin_user=${2:-}
      shift 2
      ;;
    --application-role)
      application_role=${2:-}
      shift 2
      ;;
    --application-name)
      application_name=${2:-}
      shift 2
      ;;
    --legacy-role)
      legacy_role=${2:-}
      shift 2
      ;;
    --owner-role)
      owner_role=${2:-}
      shift 2
      ;;
    --migration-role)
      migration_role=${2:-}
      shift 2
      ;;
    --backup-role)
      backup_role=${2:-}
      shift 2
      ;;
    --max-activity-age-seconds)
      max_activity_age_seconds=${2:-}
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

[[ -n "$api_container" ]] || die '--api-container is required'
[[ -n "$postgres_container" ]] || die '--postgres-container is required'
[[ -n "$database" ]] || die '--database is required'
[[ -r "$session_cookie_contract" ]] || die 'session cookie contract rules are unavailable'

for container in "$api_container" "$postgres_container"; do
  [[ "$container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || die 'invalid container name'
done
[[ "$database" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die 'invalid database name'
for role in "$admin_user" "$application_role" "$legacy_role" "$owner_role" "$migration_role" "$backup_role"; do
  [[ "$role" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die 'invalid role name'
done
[[ "$application_name" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || die 'invalid application name'
[[ "$max_activity_age_seconds" =~ ^[0-9]+$ ]] || die 'activity age must be an integer'
((max_activity_age_seconds >= 1 && max_activity_age_seconds <= 300)) ||
  die 'activity age must be between 1 and 300 seconds'

role_names=("$application_role" "$legacy_role" "$owner_role" "$migration_role" "$backup_role")
for ((left = 0; left < ${#role_names[@]}; left++)); do
  for ((right = left + 1; right < ${#role_names[@]}; right++)); do
    [[ "${role_names[left]}" != "${role_names[right]}" ]] || die 'expected and forbidden role names must be distinct'
  done
done

for command_name in docker od tr; do
  command -v "$command_name" >/dev/null 2>&1 || die "$command_name is required"
done

[[ -z ${DOCKER_HOST:-} ]] || die 'DOCKER_HOST must be unset; remote Docker targets are refused'
docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
[[ "$docker_context" == default || "$docker_context" == desktop-linux ]] ||
  die 'a known local Docker context is required'
[[ "$docker_endpoint" == unix://* ]] || die 'Docker must use a local Unix socket'

for container in "$api_container" "$postgres_container"; do
  running=$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null || true)
  [[ "$running" == true ]] || die 'required container is not running'
done

# The dollar-prefixed names belong to Docker's Go template, not this shell.
# shellcheck disable=SC2016
network_format='{{range $name, $network := .NetworkSettings.Networks}}{{printf "%s|%s|%s|%s\n" $name $network.NetworkID $network.IPAddress $network.GlobalIPv6Address}}{{end}}'
api_networks=$(docker inspect --format "$network_format" "$api_container")
postgres_networks=$(docker inspect --format "$network_format" "$postgres_container")

shared_network_count=0
api_v4_address=""
api_v6_address=""
while IFS='|' read -r api_network_name api_network_id candidate_ipv4 candidate_ipv6 api_extra; do
  [[ -n "$api_network_name" ]] || continue
  [[ -z "$api_extra" ]] || die 'unexpected API network metadata'
  [[ "$api_network_name" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || die 'invalid API network metadata'
  [[ "$api_network_id" =~ ^[a-f0-9]{64}$ ]] || die 'invalid API network metadata'

  while IFS='|' read -r postgres_network_name postgres_network_id _postgres_ipv4 _postgres_ipv6 postgres_extra; do
    [[ -n "$postgres_network_name" ]] || continue
    [[ -z "$postgres_extra" ]] || die 'unexpected PostgreSQL network metadata'
    [[ "$postgres_network_name" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || die 'invalid PostgreSQL network metadata'
    [[ "$postgres_network_id" =~ ^[a-f0-9]{64}$ ]] || die 'invalid PostgreSQL network metadata'
    if [[ "$api_network_id" == "$postgres_network_id" ]]; then
      ((shared_network_count += 1))
      api_v4_address=$candidate_ipv4
      api_v6_address=$candidate_ipv6
    fi
  done <<<"$postgres_networks"
done <<<"$api_networks"

((shared_network_count == 1)) || die 'API and PostgreSQL must share exactly one Docker network'
if [[ -z "$api_v4_address" ]]; then
  [[ -z "$api_v6_address" ]] || die 'IPv6-only shared networks are not supported by this verifier'
  die 'API has no address on the shared PostgreSQL network'
fi
valid_ipv4 "$api_v4_address" || die 'Docker returned an invalid API IPv4 address'

api_healthy=false
for ((attempt = 0; attempt < 10; attempt++)); do
  if docker exec "$api_container" \
    wget -Y off -q -T 5 -O /dev/null http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    api_healthy=true
    break
  fi
  sleep 1
done
[[ "$api_healthy" == true ]] || die 'API health check failed'

# Build an unlock request inside the API container from the passcode already
# present there, then use the returned cookies for a library request that
# always queries PostgreSQL. Secrets stay in one protected temporary directory
# and never enter a host or child-process argument.
probe_status=0
docker exec -i \
  -e "PP_VERIFY_SESSION_CONTRACT=$session_contract" \
  "$api_container" sh -eu -c '
  umask 077
  probe_dir=$(mktemp -d /tmp/pandapages-api-role-verify.XXXXXX)
  cleanup() {
    rm -rf -- "$probe_dir"
  }
  trap cleanup EXIT HUP INT TERM
  case "$probe_dir" in
    /tmp/pandapages-api-role-verify.*) ;;
    *) exit 20 ;;
  esac

  test -n "${PP_PASSCODE:-}"
  test "$PP_VERIFY_SESSION_CONTRACT" = legacy || test "$PP_VERIFY_SESSION_CONTRACT" = signed
  unlock_request="$probe_dir/unlock-request.json"
  unlock_headers="$probe_dir/unlock-headers"
  unlock_response="$probe_dir/unlock-response.json"
  session_cookie_rules="$probe_dir/session-cookie.awk"
  cookie_file="$probe_dir/cookies"
  library_request="$probe_dir/library-request.http"
  library_response="$probe_dir/library-response.http"

  cat >"$session_cookie_rules"
  test -s "$session_cookie_rules"

  awk '\''BEGIN {
    value = ENVIRON["PP_PASSCODE"]
    if (length(value) != 6) exit 1
    printf "{\"passcode\":\""
    for (position = 1; position <= length(value); position++) {
      character = substr(value, position, 1)
      if (character ~ /[[:cntrl:]]/) exit 1
      if (character == "\\" || character == "\"") printf "\\%s", character
      else printf "%s", character
    }
    printf "\"}\n"
  }'\'' >"$unlock_request"

  wget -Y off -T 5 -S -O "$unlock_response" -o "$unlock_headers" \
    --header="Content-Type: application/json" \
    --post-file="$unlock_request" \
    http://127.0.0.1:8080/api/v1/auth/unlock
  grep -Eq '\''"ok"[[:space:]]*:[[:space:]]*true'\'' "$unlock_response"

  awk -v session_contract="$PP_VERIFY_SESSION_CONTRACT" \
    -f "$session_cookie_rules" "$unlock_headers" >"$cookie_file"

  {
    printf "GET /api/v1/library HTTP/1.1\r\n"
    printf "Host: 127.0.0.1:8080\r\n"
    printf "Connection: close\r\n"
    printf "Cookie: "
    cat "$cookie_file"
    printf "\r\n\r\n"
  } >"$library_request"

  busybox nc -w 5 127.0.0.1 8080 \
    <"$library_request" >"$library_response"
  grep -Eq '\''^HTTP/1\.[01] 200 '\'' "$library_response"
  grep -Eq '\''"items"[[:space:]]*:'\'' "$library_response"
' <"$session_cookie_contract" >/dev/null 2>&1 || probe_status=$?
((probe_status == 0)) || die 'API database-backed unlock and library probe failed'

# This SQL is intentionally fixed. Caller-controlled role, database, network,
# and address values are compared in Bash after every text field has been
# hex-encoded by PostgreSQL. No psql variable interpolation is involved.
activity_rows=$(docker exec -i \
  -e 'PGOPTIONS=-c default_transaction_read_only=on' \
  "$postgres_container" \
  psql -X --username="$admin_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --tuples-only --no-align --field-separator='|' --file=- <<'SQL'
SELECT encode(convert_to(COALESCE(usename::text, ''), 'UTF8'), 'hex'),
       encode(convert_to(COALESCE(datname::text, ''), 'UTF8'), 'hex'),
       encode(convert_to(COALESCE(application_name, ''), 'UTF8'), 'hex'),
       encode(convert_to(COALESCE(host(client_addr), ''), 'UTF8'), 'hex'),
       floor(extract(epoch FROM state_change))::bigint,
       floor(extract(epoch FROM clock_timestamp()))::bigint
FROM pg_stat_activity
WHERE backend_type = 'client backend'
ORDER BY pid;
SQL
)

expected_role_hex=$(hex_value "$application_role")
expected_database_hex=$(hex_value "$database")
expected_application_name_hex=$(hex_value "$application_name")
expected_address_hex=$(hex_value "$api_v4_address")
legacy_role_hex=$(hex_value "$legacy_role")
owner_role_hex=$(hex_value "$owner_role")
migration_role_hex=$(hex_value "$migration_role")
backup_role_hex=$(hex_value "$backup_role")

source_sessions=0
recent_expected_sessions=0
forbidden_sessions=0
unexpected_sessions=0
unexpected_named_sources=0

while IFS='|' read -r role_hex database_hex observed_application_name_hex address_hex state_epoch now_epoch extra; do
  [[ -n "$role_hex$database_hex$observed_application_name_hex$address_hex$state_epoch$now_epoch$extra" ]] || continue
  [[ -z "$extra" ]] || die 'PostgreSQL returned malformed activity evidence'
  for encoded in "$role_hex" "$database_hex" "$observed_application_name_hex" "$address_hex"; do
    [[ "$encoded" =~ ^[a-f0-9]*$ ]] || die 'PostgreSQL returned malformed activity evidence'
  done
  [[ "$state_epoch" =~ ^[0-9]+$ && "$now_epoch" =~ ^[0-9]+$ ]] ||
    die 'PostgreSQL returned malformed activity timing evidence'

  if [[ "$observed_application_name_hex" == "$expected_application_name_hex" &&
        "$address_hex" != "$expected_address_hex" ]]; then
    ((unexpected_named_sources += 1))
  fi

  [[ "$address_hex" == "$expected_address_hex" ]] || continue
  ((source_sessions += 1))

  case "$role_hex" in
    "$legacy_role_hex"|"$owner_role_hex"|"$migration_role_hex"|"$backup_role_hex")
      ((forbidden_sessions += 1))
      ;;
  esac

  if [[ "$role_hex" == "$expected_role_hex" &&
        "$database_hex" == "$expected_database_hex" &&
        "$observed_application_name_hex" == "$expected_application_name_hex" ]]; then
    activity_age=$((now_epoch - state_epoch))
    if ((activity_age >= 0 && activity_age <= max_activity_age_seconds)); then
      ((recent_expected_sessions += 1))
    fi
  else
    ((unexpected_sessions += 1))
  fi
done <<<"$activity_rows"

((unexpected_named_sources == 0)) || die 'application-name activity was observed from an unexpected source'
((source_sessions > 0)) || die 'no PostgreSQL connection was observed from the API container'
((forbidden_sessions == 0)) || die 'a forbidden database role was observed from the API container'
((unexpected_sessions == 0)) || die 'an unexpected database identity was observed from the API container'
((recent_expected_sessions > 0)) || die 'no recent qualifying application-role connection was observed'

printf 'api_container_healthy=true\n'
printf 'api_database_probe=passed\n'
printf 'api_database_role=%s\n' "$application_role"
printf 'api_database_name=%s\n' "$database"
printf 'api_database_application_name=%s\n' "$application_name"
printf 'api_database_source=unique-shared-network\n'
printf 'api_session_contract=%s\n' "$session_contract"
printf 'api_role_verification=passed\n'
