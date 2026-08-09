#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source "$repo_root/scripts/tests/postgresql-stable-readiness.sh"
readonly repo_root
readonly seed_script="$repo_root/scripts/dev/seed-test-data.sh"
readonly migration_image=${PP_FIXTURE_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database=pandapages
readonly database_user=pandapages
readonly database_password='generated-fixture-password-not-for-production'
readonly resource_prefix="pandapages-fixture-integration-$$"
readonly postgres_container="$resource_prefix-postgres"
readonly network="$resource_prefix-network"
readonly volume="$resource_prefix-data"
readonly resource_label="com.pandapages.fixture-run=$resource_prefix"

for command_name in docker grep mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

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
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] ||
  [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Fixture integration requires a known local Docker Unix socket\n' >&2
  exit 1
fi

docker image inspect "$migration_image" >/dev/null 2>&1 || {
  printf 'Required migration image is unavailable: %s\n' "$migration_image" >&2
  exit 1
}

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-fixture-integration.XXXXXX")
network_created=false
volume_created=false
cleanup() {
  set +e
  docker ps -aq --filter "label=$resource_label" |
    while IFS= read -r container_id; do
      [[ -n "$container_id" ]] || continue
      docker rm --force "$container_id" >/dev/null 2>&1
    done
  $network_created && docker network rm "$network" >/dev/null 2>&1
  $volume_created && docker volume rm "$volume" >/dev/null 2>&1
  rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

docker network create \
  --label com.pandapages.disposable=fixture-integration \
  --label "$resource_label" \
  "$network" >/dev/null
network_created=true

docker volume create \
  --label com.pandapages.disposable=fixture-integration \
  --label "$resource_label" \
  "$volume" >/dev/null
volume_created=true

docker run --detach \
  --name "$postgres_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=fixture-integration \
  --label com.pandapages.test-seed-target=disposable-fixture-integration \
  --label "$resource_label" \
  --env "POSTGRES_DB=$database" \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --mount "type=volume,src=$volume,dst=/var/lib/postgresql" \
  --health-cmd "pg_isready --username=$database_user --dbname=$database" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null

wait_for_stable_postgres "$postgres_container" "$database_user" "$database" \
  'Disposable fixture PostgreSQL'

docker run --rm \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --env GOOSE_DRIVER=postgres \
  --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$postgres_container:5432/$database?sslmode=disable" \
  --env GOOSE_MIGRATION_DIR=/migrations \
  --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
  "$migration_image" up >/dev/null

query() {
  docker exec "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="$1"
}

[[ $(query "SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1;") == 1 ]]
[[ $(query "SELECT count(*) FROM accounts;") == 0 ]]
[[ $(query "SELECT count(*) FROM profiles;") == 0 ]]

if env -u PP_ALLOW_TEST_SEED \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >"$test_root/missing-ack.out" 2>"$test_root/missing-ack.err"; then
  printf 'Fixture seed unexpectedly accepted a missing acknowledgement\n' >&2
  exit 1
fi
grep -Fq 'PP_ALLOW_TEST_SEED=1' "$test_root/missing-ack.err"

PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE="$database" \
PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >/dev/null

[[ $(query "SELECT count(*) FROM accounts WHERE id='f17e0000-0000-4000-8000-000000000001';") == 1 ]]
[[ $(query "SELECT count(*) FROM profiles WHERE id='f17e0000-0000-4000-8000-000000000002' AND reading_level='classic';") == 1 ]]
[[ $(query "SELECT count(*) FROM stories WHERE id='f17e0000-0000-4000-8000-000000000010';") == 1 ]]
[[ $(query "SELECT count(*) FROM story_releases WHERE story_id='f17e0000-0000-4000-8000-000000000010' AND release_number=1;") == 1 ]]
[[ $(query "SELECT count(*) FROM reading_progress WHERE story_id='f17e0000-0000-4000-8000-000000000010';") == 0 ]]

# Explicit fixture installation remains idempotent.
PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE="$database" \
PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >/dev/null

PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE="$database" \
PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --with-progress >/dev/null

[[ $(query "SELECT count(*) FROM reading_progress WHERE story_id='f17e0000-0000-4000-8000-000000000010';") == 1 ]]

PP_ALLOW_TEST_SEED=1 \
PP_TEST_SEED_DATABASE="$database" \
PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --remove >/dev/null

[[ $(query "SELECT count(*) FROM stories WHERE id='f17e0000-0000-4000-8000-000000000010';") == 0 ]]
[[ $(query "SELECT count(*) FROM profiles WHERE id='f17e0000-0000-4000-8000-000000000002';") == 0 ]]
[[ $(query "SELECT count(*) FROM accounts WHERE id='f17e0000-0000-4000-8000-000000000001';") == 0 ]]
[[ $(query "SELECT count(*) FROM reading_progress WHERE story_id='f17e0000-0000-4000-8000-000000000010';") == 0 ]]

printf 'postgresql_fixtures_integration=passed\n'
