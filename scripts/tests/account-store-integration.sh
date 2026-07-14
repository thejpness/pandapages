#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repo_root
readonly database_name='pandapages_account_store_test'
readonly database_user='postgres'
readonly database_password='generated-account-store-password-not-for-production'
readonly postgres_image=${PP_ACCOUNT_STORE_TEST_POSTGRES_IMAGE:-postgres:18.1-alpine}
readonly container_name="pandapages-account-store-test-$$"

for command_name in docker go; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf '%s is required\n' "$command_name" >&2
    exit 1
  }
done

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'DOCKER_HOST must be unset; remote Docker targets are refused\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
[[ "$docker_context" == default || "$docker_context" == desktop-linux ]] || {
  printf 'a known local Docker context is required\n' >&2
  exit 1
}
[[ "$docker_endpoint" == unix://* ]] || {
  printf 'Docker must use a local Unix socket\n' >&2
  exit 1
}

if docker container inspect "$container_name" >/dev/null 2>&1; then
  printf 'refusing to reuse existing container: %s\n' "$container_name" >&2
  exit 1
fi

container_created=false
cleanup() {
  if $container_created; then
    docker rm --force "$container_name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT HUP INT TERM

docker run --detach \
  --name "$container_name" \
  --label com.pandapages.disposable=account-store-integration \
  --env "POSTGRES_DB=$database_name" \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --publish 127.0.0.1::5432 \
  --health-cmd "pg_isready --username=$database_user --dbname=$database_name" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null
container_created=true

health=starting
for _ in {1..60}; do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_name")
  [[ "$health" == healthy ]] && break
  [[ "$health" != unhealthy ]] || {
    printf 'disposable PostgreSQL became unhealthy\n' >&2
    exit 1
  }
  sleep 1
done
[[ "$health" == healthy ]] || {
  printf 'disposable PostgreSQL did not become healthy\n' >&2
  exit 1
}

published_address=$(docker port "$container_name" 5432/tcp)
published_port=${published_address##*:}
[[ "$published_port" =~ ^[0-9]+$ ]] || {
  printf 'Docker returned an invalid disposable PostgreSQL port\n' >&2
  exit 1
}

database_url="postgres://$database_user:$database_password@127.0.0.1:$published_port/$database_name?sslmode=disable"

(
  cd "$repo_root/apps/api"
  PP_ACCOUNT_STORE_TEST_DISPOSABLE=1 \
    PP_ACCOUNT_STORE_TEST_DATABASE_URL="$database_url" \
    go test ./internal/db -run '^TestAccountStoreIntegration$' -count=1
)

printf 'account_store_integration=passed\n'
