#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source "$repo_root/scripts/tests/postgresql-stable-readiness.sh"
readonly repo_root
readonly migration_image=${PP_ACCOUNT_INTEGRITY_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database=pandapages_integrity
readonly database_user=postgres
readonly database_password='generated-account-integrity-password-not-for-production'
readonly resource_prefix="pandapages-account-integrity-$$"
readonly postgres_container="$resource_prefix-postgres"
readonly network_name="$resource_prefix-network"
readonly volume_name="$resource_prefix-data"
readonly resource_label="com.pandapages.account-integrity-run=$resource_prefix"

for command_name in docker grep mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'Refusing account-integrity integration while DOCKER_HOST is set\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] ||
  [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Account-integrity integration requires a known local Docker Unix socket\n' >&2
  exit 1
fi

docker image inspect "$migration_image" >/dev/null 2>&1 || {
  printf 'Required migration image is unavailable: %s\n' "$migration_image" >&2
  exit 1
}

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-account-integrity.XXXXXX")
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
  rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

docker network create \
  --label com.pandapages.disposable=account-integrity-integration \
  --label "$resource_label" \
  "$network_name" >/dev/null
network_created=true

docker volume create \
  --label com.pandapages.disposable=account-integrity-integration \
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
  --label com.pandapages.disposable=account-integrity-integration \
  --label "$resource_label" \
  --env "POSTGRES_DB=$database" \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --mount "type=volume,src=$volume_name,dst=/var/lib/postgresql" \
  --health-cmd "pg_isready --username=$database_user --dbname=$database" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null

wait_for_stable_postgres "$postgres_container" "$database_user" "$database" \
  'Disposable account-integrity PostgreSQL'

docker run --rm \
  --network "$network_name" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=account-integrity-integration \
  --label "$resource_label" \
  --env GOOSE_DRIVER=postgres \
  --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$postgres_container:5432/$database?sslmode=disable" \
  --env GOOSE_MIGRATION_DIR=/migrations \
  --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
  "$migration_image" up >/dev/null

psql() {
  docker exec -i "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 "$@"
}

psql --quiet --file=- <<'SQL'
INSERT INTO accounts (id, name) VALUES
  ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Household A'),
  ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Household B');

INSERT INTO profiles (id, account_id, name, reading_level) VALUES
  ('aaaaaaaa-1000-4000-8000-000000000001', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Reader A', 'growing-readers'),
  ('bbbbbbbb-1000-4000-8000-000000000001', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Reader B', 'little-listeners');

INSERT INTO stories (id, account_id, slug, title) VALUES
  ('aaaaaaaa-2000-4000-8000-000000000001', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'story-a', 'Story A'),
  ('bbbbbbbb-2000-4000-8000-000000000001', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'story-b', 'Story B');

INSERT INTO story_editions (id, story_id, edition_key) VALUES
  ('aaaaaaaa-3000-4000-8000-000000000001', 'aaaaaaaa-2000-4000-8000-000000000001', 'classic'),
  ('bbbbbbbb-3000-4000-8000-000000000001', 'bbbbbbbb-2000-4000-8000-000000000001', 'classic');

INSERT INTO story_versions (
  id, story_id, edition_id, version, markdown, rendered_html, content_hash
) VALUES
  (
    'aaaaaaaa-4000-4000-8000-000000000001',
    'aaaaaaaa-2000-4000-8000-000000000001',
    'aaaaaaaa-3000-4000-8000-000000000001',
    1, '# A', '<h1>A</h1>', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  ),
  (
    'bbbbbbbb-4000-4000-8000-000000000001',
    'bbbbbbbb-2000-4000-8000-000000000001',
    'bbbbbbbb-3000-4000-8000-000000000001',
    1, '# B', '<h1>B</h1>', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  );

INSERT INTO reading_progress (
  account_id, profile_id, story_id, story_version_id, locator, percent
) VALUES (
  'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  'aaaaaaaa-1000-4000-8000-000000000001',
  'aaaaaaaa-2000-4000-8000-000000000001',
  'aaaaaaaa-4000-4000-8000-000000000001',
  '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0.5}}',
  0.5
);
SQL

expect_rejected() {
  local description=$1
  local constraint=$2
  local statement=$3
  if psql --command="$statement" >"$test_root/rejected.out" 2>"$test_root/rejected.err"; then
    printf 'Integrity check unexpectedly succeeded: %s\n' "$description" >&2
    exit 1
  fi
  grep -Fq "$constraint" "$test_root/rejected.err" || {
    printf 'Integrity check failed outside expected constraint %s: %s\n' "$constraint" "$description" >&2
    cat "$test_root/rejected.err" >&2
    exit 1
  }
}

expect_rejected \
  'cross-account profile progress' \
  'reading_progress_profile_account_fkey' \
  "INSERT INTO reading_progress (account_id,profile_id,story_id,story_version_id,locator,percent) VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','bbbbbbbb-1000-4000-8000-000000000001','aaaaaaaa-2000-4000-8000-000000000001','aaaaaaaa-4000-4000-8000-000000000001','{\"schema\":2,\"segment\":{\"key\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"occurrence\":1,\"ordinal\":1,\"offset\":0}}',0);"

expect_rejected \
  'progress version from another story' \
  'reading_progress_story_version_story_fkey' \
  "UPDATE reading_progress SET story_version_id='bbbbbbbb-4000-4000-8000-000000000001' WHERE account_id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';"

expect_rejected \
  'deleting an account that still owns profiles and stories' \
  'profiles_account_id_fkey' \
  "DELETE FROM accounts WHERE id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';"

expect_rejected \
  'invalid reading level' \
  'profiles_reading_level_check' \
  "INSERT INTO profiles (account_id,name,reading_level) VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','Bad level','nearest-easier');"

printf 'account_integrity_integration=passed\n'
