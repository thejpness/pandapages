#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source "$repo_root/scripts/tests/postgresql-stable-readiness.sh"
readonly repo_root
readonly database_name=pandapages_reader_store_test
readonly database_user=postgres
readonly database_password='generated-reader-store-password-not-for-production'
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly migration_image=${PP_READER_STORE_TEST_MIGRATION_IMAGE:-pandapages-migrate:reader-test}
readonly resource_prefix="pandapages-reader-store-$$"
readonly postgres_container="$resource_prefix-postgres"
readonly network_name="$resource_prefix-network"
readonly volume_name="$resource_prefix-data"
readonly resource_label="com.pandapages.reader-store-run=$resource_prefix"

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
docker image inspect "$migration_image" >/dev/null 2>&1 || {
  printf 'required migration image is unavailable: %s\n' "$migration_image" >&2
  exit 1
}

network_created=false
volume_created=false
cleanup() {
  set +e
  docker ps -aq --filter "label=$resource_label" |
    while IFS= read -r container_id; do
      [[ -n "$container_id" ]] || continue
      docker rm --force "$container_id" >/dev/null 2>&1
    done
  $network_created && docker network rm "$network_name" >/dev/null 2>&1
  $volume_created && docker volume rm "$volume_name" >/dev/null 2>&1
}
trap cleanup EXIT HUP INT TERM

docker network create \
  --label com.pandapages.disposable=reader-store-integration \
  --label "$resource_label" \
  "$network_name" >/dev/null
network_created=true

docker volume create \
  --label com.pandapages.disposable=reader-store-integration \
  --label "$resource_label" \
  "$volume_name" >/dev/null
volume_created=true

docker run --detach \
  --name "$postgres_container" \
  --network "$network_name" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=reader-store-integration \
  --label "$resource_label" \
  --env "POSTGRES_DB=$database_name" \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --mount "type=volume,src=$volume_name,dst=/var/lib/postgresql" \
  --publish 127.0.0.1::5432 \
  --health-cmd "pg_isready --username=$database_user --dbname=$database_name" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null

wait_for_stable_postgres "$postgres_container" "$database_user" "$database_name" \
  'Disposable Reader-store PostgreSQL'

docker run --rm \
  --name "$resource_prefix-goose" \
  --network "$network_name" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=reader-store-integration \
  --label "$resource_label" \
  --env GOOSE_DRIVER=postgres \
  --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$postgres_container:5432/$database_name?sslmode=disable" \
  --env GOOSE_MIGRATION_DIR=/migrations \
  --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
  "$migration_image" up >/dev/null

query() {
  docker exec "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database_name" \
      --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="$1"
}

[[ $(query "SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1;") == 1 ]]
[[ $(query "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='story_segments' AND column_name='locator';") == 0 ]]
[[ $(query "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='story_segments' AND column_name IN ('segment_kind','heading_level','content_key','content_occurrence','chapter_key','chapter_occurrence');") == 6 ]]
[[ $(query "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='story_releases' AND column_name='migration_backfill';") == 0 ]]
[[ $(query "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('story_editions','story_releases','story_release_editions','story_sources','story_source_versions','reader_story_edition_overrides','reading_progress');") == 7 ]]

published_address=$(docker port "$postgres_container" 5432/tcp)
published_port=${published_address##*:}
[[ "$published_port" =~ ^[0-9]+$ ]]
database_url="postgres://$database_user:$database_password@127.0.0.1:$published_port/$database_name?sslmode=disable"

(
  cd "$repo_root/apps/api"
  PP_READER_STORE_TEST_DISPOSABLE=1 \
    PP_READER_STORE_TEST_DATABASE_URL="$database_url" \
    go test ./internal/db \
      -run '^(TestReaderStoreIntegration|TestAdminEditionBundleIntegration|TestAdminReleaseIntegration|TestReaderEditionOverrideIntegration|TestReaderResolutionIntegration)$' \
      -count=1
)

printf 'reader_store_integration=passed\n'
