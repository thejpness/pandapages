#!/usr/bin/env bash

set -euo pipefail
umask 077

source_container=${PP_ROLE_TEST_SOURCE_CONTAINER:-}
network=${PP_ROLE_TEST_NETWORK:-}
api_image=${PP_ROLE_TEST_API_IMAGE:-}
database=${PP_ROLE_TEST_DATABASE:-}
admin_user=${PP_ROLE_TEST_ADMIN_USER:-}
application_role=${PP_ROLE_TEST_APPLICATION_ROLE:-}
application_password=${PP_ROLE_TEST_APPLICATION_PASSWORD:-}
migration_role=${PP_ROLE_TEST_MIGRATION_ROLE:-}
migration_password=${PP_ROLE_TEST_MIGRATION_PASSWORD:-}

for pair in \
  "source container:$source_container" \
  "network:$network" \
  "API image:$api_image" \
  "database:$database" \
  "admin user:$admin_user" \
  "application role:$application_role" \
  "application password:$application_password" \
  "migration role:$migration_role" \
  "migration password:$migration_password"; do
  [[ -n ${pair#*:} ]] || {
    printf 'Missing generated role-smoke input: %s\n' "${pair%%:*}" >&2
    exit 1
  }
done

for value in "$source_container" "$network" "$database" "$admin_user" "$application_role" "$migration_role"; do
  [[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || {
    printf 'Invalid generated role-smoke identifier\n' >&2
    exit 1
  }
done

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
test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-api-role-smoke.XXXXXX")

cleanup() {
  set +e
  if $api_created; then
    docker rm --force "$api_container" >/dev/null 2>&1
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT

if docker run --rm \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --env "MIGRATION_DATABASE_URL=postgres://$migration_role:$migration_password@$source_container:5432/$database?sslmode=disable" \
  --env PP_PASSCODE=123456 \
  "$api_image" >"$test_root/missing-runtime.out" 2>"$test_root/missing-runtime.err"; then
  printf 'API unexpectedly accepted only the migration credential variable\n' >&2
  exit 1
fi
grep -q 'DATABASE_URL is required' "$test_root/missing-runtime.err"

docker run --detach \
  --name "$api_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=role-api-smoke \
  --env "DATABASE_URL=postgres://$application_role:$application_password@$source_container:5432/$database?sslmode=disable" \
  --env PP_PASSCODE=123456 \
  --env PP_ADMIN_KEY=generated-admin-key-not-for-production \
  --env PP_SESSION_SECRET=generated-session-secret-not-for-production \
  "$api_image" >/dev/null
api_created=true

api_ready=false
for ((attempt = 0; attempt < 30; attempt++)); do
  if docker exec "$api_container" \
    wget -q -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; then
    api_ready=true
    break
  fi
  [[ $(docker inspect --format '{{.State.Running}}' "$api_container") == true ]] || {
    printf 'API exited before the role smoke test became ready\n' >&2
    exit 1
  }
  sleep 1
done
[[ "$api_ready" == true ]] || {
  printf 'API did not become ready with the application role\n' >&2
  exit 1
}

unlock_response=$(docker exec "$api_container" \
  wget -q -O - \
  --header='Content-Type: application/json' \
  --post-data='{"passcode":"123456"}' \
  http://127.0.0.1:8080/api/v1/auth/unlock)
[[ "$unlock_response" == *'"ok":true'* ]] || {
  printf 'API unlock did not complete through the application role\n' >&2
  exit 1
}

api_sessions=$(docker exec "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" \
  --tuples-only --no-align --set=ON_ERROR_STOP=1 \
  --command="SELECT count(*) FROM pg_stat_activity WHERE usename = '$application_role' AND datname = '$database';")
[[ "$api_sessions" =~ ^[1-9][0-9]*$ ]] || {
  printf 'No application-role API session was observed\n' >&2
  exit 1
}

printf 'api_role_smoke=passed\n'
