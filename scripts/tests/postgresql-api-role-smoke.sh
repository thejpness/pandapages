#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
verifier="$repo_root/scripts/postgresql-api-role-verify.sh"
source_container=${PP_ROLE_TEST_SOURCE_CONTAINER:-}
network=${PP_ROLE_TEST_NETWORK:-}
api_image=${PP_ROLE_TEST_API_IMAGE:-}
database=${PP_ROLE_TEST_DATABASE:-}
admin_user=${PP_ROLE_TEST_ADMIN_USER:-}
application_role=${PP_ROLE_TEST_APPLICATION_ROLE:-}
application_password=${PP_ROLE_TEST_APPLICATION_PASSWORD:-}
migration_role=${PP_ROLE_TEST_MIGRATION_ROLE:-}
migration_password=${PP_ROLE_TEST_MIGRATION_PASSWORD:-}
legacy_role=${PP_ROLE_TEST_LEGACY_ROLE:-}
legacy_password=${PP_ROLE_TEST_LEGACY_PASSWORD:-}
readonly expected_application_name=pandapages-api

for pair in \
  "source container:$source_container" \
  "network:$network" \
  "API image:$api_image" \
  "database:$database" \
  "admin user:$admin_user" \
  "application role:$application_role" \
  "application password:$application_password" \
  "migration role:$migration_role" \
  "migration password:$migration_password" \
  "legacy role:$legacy_role" \
  "legacy password:$legacy_password"; do
  [[ -n ${pair#*:} ]] || {
    printf 'Missing generated role-smoke input: %s\n' "${pair%%:*}" >&2
    exit 1
  }
done

for value in "$source_container" "$network" "$database" "$admin_user" "$application_role" "$migration_role" "$legacy_role"; do
  [[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || {
    printf 'Invalid generated role-smoke identifier\n' >&2
    exit 1
  }
done

[[ -x "$verifier" ]] || {
  printf 'API role verifier is unavailable\n' >&2
  exit 1
}

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'Refusing role smoke test while DOCKER_HOST is set\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] || [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Role smoke test requires a known local Docker Unix socket\n' >&2
  exit 1
fi

docker image inspect "$api_image" >/dev/null 2>&1 || {
  printf 'API role-smoke image is unavailable: %s\n' "$api_image" >&2
  exit 1
}

readonly api_container="${source_container}-api"
api_created=false
manual_session_started=false
test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-api-role-smoke.XXXXXX")

cleanup() {
  set +e
  if $api_created; then
    docker rm --force "$api_container" >/dev/null 2>&1
  fi
  if $manual_session_started; then
    docker exec "$source_container" psql -X --username="$admin_user" --dbname="$database" --set=ON_ERROR_STOP=1 --command="SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = 'manual-psql' AND pid <> pg_backend_pid();" >/dev/null 2>&1
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT

stop_api() {
  if $api_created; then
    docker rm --force "$api_container" >/dev/null
    api_created=false
  fi
}

write_api_environment() {
  local output=$1
  local database_role=$2
  local database_password=$3
  local workload_name=$4
  local passcode=${5:-123456}

  {
    printf 'DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
      "$database_role" "$database_password" "$source_container" "$database"
    printf 'PGAPPNAME=%s\n' "$workload_name"
    printf 'PP_PASSCODE=%s\n' "$passcode"
    printf 'PP_ADMIN_KEY=generated-admin-key-not-for-production\n'
    printf 'PP_SESSION_SECRET=generated-session-secret-not-for-production\n'
  } >"$output"
  chmod 0600 "$output"
}

start_api() {
  local database_role=$1
  local database_password=$2
  local workload_name=$3
  local passcode=${4:-123456}
  local environment_file="$test_root/api.env"

  write_api_environment "$environment_file" "$database_role" "$database_password" "$workload_name" "$passcode"
  docker run --detach \
    --name "$api_container" \
    --network "$network" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --label com.pandapages.disposable=role-api-smoke \
    --env-file "$environment_file" \
    "$api_image" >/dev/null
  api_created=true
}

verify_arguments=(
  --api-container "$api_container"
  --postgres-container "$source_container"
  --database "$database"
  --admin-user "$admin_user"
  --application-role "$application_role"
  --application-name "$expected_application_name"
  --legacy-role "$legacy_role"
  --migration-role "$migration_role"
)

assert_probe_cleanup() {
  local leftover
  leftover=$(docker exec "$api_container" sh -c \
    "find /tmp -maxdepth 1 -name 'pandapages-api-role-verify.*' -print -quit")
  [[ -z "$leftover" ]] || {
    printf 'API role verifier left protected probe material behind\n' >&2
    exit 1
  }
}

assert_no_generated_credentials() {
  local file=$1
  local content
  local credential

  content=$(<"$file")
  for credential in "$application_password" "$migration_password" "$legacy_password"; do
    [[ "$content" != *"$credential"* ]] || {
      printf 'Generated database credentials appeared in verifier output\n' >&2
      exit 1
    }
  done
}

missing_runtime_environment="$test_root/missing-runtime.env"
{
  printf 'MIGRATION_DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
    "$migration_role" "$migration_password" "$source_container" "$database"
  printf 'PP_PASSCODE=123456\n'
} >"$missing_runtime_environment"
chmod 0600 "$missing_runtime_environment"

if docker run --rm \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --env-file "$missing_runtime_environment" \
  "$api_image" >"$test_root/missing-runtime.out" 2>"$test_root/missing-runtime.err"; then
  printf 'API unexpectedly accepted only the migration credential variable\n' >&2
  exit 1
fi
grep -q 'DATABASE_URL is required' "$test_root/missing-runtime.err"

start_api "$application_role" "$application_password" "$expected_application_name"
if ! "$verifier" "${verify_arguments[@]}" >"$test_root/warm-cache.out" 2>"$test_root/warm-cache.err"; then
  printf 'Positive API role verification failed: ' >&2
  sed 's/^postgresql-api-role-verify: //' "$test_root/warm-cache.err" >&2
  exit 1
fi
grep -q '^api_role_verification=passed$' "$test_root/warm-cache.out"
assert_no_generated_credentials "$test_root/warm-cache.out"
assert_no_generated_credentials "$test_root/warm-cache.err"
assert_probe_cleanup

# Ensure the durable probe still refreshes pg_stat_activity after the unlock
# path has warmed its default-account cache. The three-second age gate makes the
# first verifier's database activity too old to satisfy the second run.
sleep 5
if ! "$verifier" "${verify_arguments[@]}" --max-activity-age-seconds 3 \
  >"$test_root/cached-unlock.out" 2>"$test_root/cached-unlock.err"; then
  printf 'Cached-unlock API role verification failed: ' >&2
  sed 's/^postgresql-api-role-verify: //' "$test_root/cached-unlock.err" >&2
  exit 1
fi
grep -q '^api_role_verification=passed$' "$test_root/cached-unlock.out"
assert_no_generated_credentials "$test_root/cached-unlock.out"
assert_no_generated_credentials "$test_root/cached-unlock.err"
assert_probe_cleanup
stop_api

# A failed application probe must fail closed and remove its protected
# request, response, and cookie material.
start_api "$application_role" "$application_password" "$expected_application_name" 12345
if "$verifier" "${verify_arguments[@]}" \
  >"$test_root/probe-failure.out" 2>"$test_root/probe-failure.err"; then
  printf 'Verifier accepted a failed database-backed application probe\n' >&2
  exit 1
fi
grep -q 'API database-backed unlock and library probe failed' "$test_root/probe-failure.err"
assert_no_generated_credentials "$test_root/probe-failure.out"
assert_no_generated_credentials "$test_root/probe-failure.err"
assert_probe_cleanup
stop_api

# An application-role session with the wrong workload name is not qualifying
# evidence, even though the API itself can start and answer the probe.
start_api "$application_role" "$application_password" manual-psql
if "$verifier" "${verify_arguments[@]}" \
  >"$test_root/wrong-workload.out" 2>"$test_root/wrong-workload.err"; then
  printf 'Verifier accepted an application-role session without the API workload identity\n' >&2
  exit 1
fi
grep -Eq 'unexpected database identity|no recent qualifying' "$test_root/wrong-workload.err"
assert_no_generated_credentials "$test_root/wrong-workload.out"
assert_no_generated_credentials "$test_root/wrong-workload.err"
assert_probe_cleanup
stop_api

# Keep a manual application-role psql backend open. The old username-only
# count is now positive, but it must not make a legacy-role API pass.
docker exec -d \
  -e PGAPPNAME=manual-psql \
  "$source_container" \
  psql -X --username="$application_role" --dbname="$database" \
  --command='SELECT pg_sleep(60);'
manual_session_started=true

manual_session_count=0
for ((attempt = 0; attempt < 10; attempt++)); do
  manual_session_count=$(docker exec -i "$source_container" \
    psql -X --username="$admin_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --set=application_role="$application_role" \
    --tuples-only --no-align --file=- <<'SQL'
SELECT count(*)
FROM pg_stat_activity
WHERE usename = :'application_role'
  AND application_name = 'manual-psql';
SQL
  )
  [[ "$manual_session_count" =~ ^[1-9][0-9]*$ ]] && break
  sleep 1
done
[[ "$manual_session_count" =~ ^[1-9][0-9]*$ ]] || {
  printf 'Manual application-role regression session did not start\n' >&2
  exit 1
}

start_api "$legacy_role" "$legacy_password" "$expected_application_name"
if "$verifier" "${verify_arguments[@]}" \
  >"$test_root/legacy.out" 2>"$test_root/legacy.err"; then
  printf 'Verifier accepted a legacy-role API because a manual application session existed\n' >&2
  exit 1
fi
grep -q 'forbidden database role' "$test_root/legacy.err"
assert_no_generated_credentials "$test_root/legacy.out"
assert_no_generated_credentials "$test_root/legacy.err"
assert_probe_cleanup
stop_api

docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --set=application_role="$application_role" --file=- <<'SQL' >/dev/null
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE usename = :'application_role'
  AND application_name = 'manual-psql'
  AND pid <> pg_backend_pid();
SQL

manual_session_started=false

printf 'api_role_smoke=passed\n'
