#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source "$repo_root/scripts/tests/postgresql-stable-readiness.sh"
readonly repo_root
readonly role_script="$repo_root/scripts/postgresql-roles.sh"
readonly migration_image=${PP_ROLE_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database=pandapages_role_test
readonly admin_user=postgres
readonly owner_role=pandapages_owner
readonly migration_role=pandapages_migrator
readonly application_role=pandapages_app
readonly backup_role=pandapages_backup
readonly migration_password='generated-migrator-password-not-for-production'
readonly application_password='generated-application-password-not-for-production'
readonly database_password='generated-admin-password-not-for-production'
readonly resource_prefix="pandapages-pg-role-integration-$$"
readonly postgres_container="$resource_prefix-postgres"
readonly network_name="$resource_prefix-network"
readonly volume_name="$resource_prefix-data"
readonly resource_label="com.pandapages.role-run=$resource_prefix"

for command_name in docker grep mktemp sed; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'Refusing integration test while DOCKER_HOST is set\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] ||
  [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Integration test requires a known local Docker Unix socket\n' >&2
  exit 1
fi

docker image inspect "$migration_image" >/dev/null 2>&1 || {
  printf 'Required migration image is unavailable: %s\n' "$migration_image" >&2
  exit 1
}

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-role-integration.XXXXXX")
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

docker network create --internal \
  --label com.pandapages.disposable=role-integration \
  --label "$resource_label" \
  "$network_name" >/dev/null
network_created=true

docker volume create \
  --label com.pandapages.disposable=role-integration \
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
  --label com.pandapages.disposable=role-integration \
  --label "$resource_label" \
  --env "POSTGRES_USER=$admin_user" \
  --env "POSTGRES_DB=$database" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --mount "type=volume,src=$volume_name,dst=/var/lib/postgresql" \
  --health-cmd "pg_isready --username=$admin_user --dbname=$database" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null

wait_for_stable_postgres "$postgres_container" "$admin_user" "$database" \
  'Generated PostgreSQL role-test source'

apply_policy() {
  "$role_script" apply \
    --container "$postgres_container" \
    --database "$database" \
    --admin-user "$admin_user" \
    --confirm-apply >/dev/null
}

verify_policy() {
  "$role_script" verify \
    --container "$postgres_container" \
    --database "$database" \
    --admin-user "$admin_user" >/dev/null
}

psql_as() {
  local role=$1
  shift
  docker exec "$postgres_container" \
    psql -X --username="$role" --dbname="$database" --set=ON_ERROR_STOP=1 "$@"
}

expect_denied() {
  local description=$1
  local role=$2
  local statement=$3
  if psql_as "$role" --command="$statement" >"$test_root/denied.out" 2>"$test_root/denied.err"; then
    printf 'Privilege denial unexpectedly succeeded: %s\n' "$description" >&2
    exit 1
  fi
}

"$role_script" audit \
  --container "$postgres_container" \
  --database "$database" \
  --admin-user "$admin_user" >/dev/null

apply_policy

docker exec -i "$postgres_container" \
  psql -X --username="$admin_user" --dbname="$database" --set=ON_ERROR_STOP=1 <<SQL >/dev/null
ALTER ROLE $migration_role PASSWORD '$migration_password';
ALTER ROLE $application_role PASSWORD '$application_password';
SQL

docker run --rm \
  --network "$network_name" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=32m \
  --env GOOSE_DRIVER=postgres \
  --env "GOOSE_DBSTRING=postgres://$migration_role:$migration_password@$postgres_container:5432/$database?sslmode=disable" \
  --env GOOSE_MIGRATION_DIR=/migrations \
  --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
  "$migration_image" up >/dev/null

# Newly created objects receive the current runtime grants only after policy
# reconciliation; this is deliberate and mirrors bootstrap-postgresql.sh.
apply_policy
verify_policy
apply_policy
verify_policy

version=$(psql_as "$migration_role" --tuples-only --no-align \
  --command="SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1;")
[[ "$version" == 1 ]]

role_state=$(docker exec "$postgres_container" \
  psql -X --username="$admin_user" --dbname="$database" --tuples-only --no-align \
  --command="SELECT string_agg(rolname || ':' || rolsuper::text || ':' || rolcreatedb::text || ':' || rolcreaterole::text || ':' || rolreplication::text || ':' || rolbypassrls::text, ',' ORDER BY rolname) FROM pg_roles WHERE rolname IN ('$owner_role','$migration_role','$application_role','$backup_role');")
[[ "$role_state" == "$application_role:false:false:false:false:false,$backup_role:false:false:false:false:false,$migration_role:false:false:false:false:false,$owner_role:false:false:false:false:false" ]]

identity=$(psql_as "$migration_role" --tuples-only --no-align \
  --command="SELECT session_user || '|' || current_user;")
[[ "$identity" == "$migration_role|$owner_role" ]]

psql_as "$application_role" --command="
  INSERT INTO accounts (id, name)
  VALUES ('a1500000-0000-4000-8000-000000000001', 'Runtime household');
  INSERT INTO profiles (id, account_id, name, reading_level)
  VALUES (
    'a1500000-0000-4000-8000-000000000011',
    'a1500000-0000-4000-8000-000000000001',
    'Runtime reader',
    'story-explorers'
  );
  INSERT INTO stories (id, visibility, owner_account_id, slug, title)
  VALUES (
    'a1500000-0000-4000-8000-000000000021',
    'public',
    NULL,
    'runtime-story',
    'Runtime Story'
  );
  UPDATE stories SET title='Runtime Story Updated'
  WHERE id='a1500000-0000-4000-8000-000000000021';
  INSERT INTO source_acquisitions (
    id, provider, external_id, title, contributors, languages, landing_url,
    representation_media_type, representation_provider_url,
    normalisation_version, retrieved_content_hash, normalised_content_hash,
    source_text, snapshot_hash
  ) VALUES (
    'a1500000-0000-4000-8000-000000000031',
    'project-gutenberg', '11', 'Runtime acquisition', '[]'::jsonb,
    jsonb_build_array('en'), 'https://www.gutenberg.org/ebooks/11',
    'text/plain; charset=utf-8', 'https://www.gutenberg.org/files/11/11-0.txt',
    'project-gutenberg-plain-text-v1',
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    'Runtime source text.',
    'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
  );
  INSERT INTO source_acquisition_reviews (
    acquisition_id, rights_status, rights_note, rights_reviewed_at,
    editorial_status
  ) VALUES (
    'a1500000-0000-4000-8000-000000000031',
    'approved', 'Initial review rationale.', now(), 'pending'
  );
  UPDATE source_acquisition_reviews
  SET rights_note='Updated review rationale.', rights_reviewed_at=now()
  WHERE acquisition_id='a1500000-0000-4000-8000-000000000031';
" >/dev/null

psql_as "$application_role" --command="SELECT count(*) FROM stories;" >/dev/null
psql_as "$application_role" --command="SELECT count(*) FROM source_acquisitions;" >/dev/null

expect_denied \
  'application acquisition mutation' \
  "$application_role" \
  "UPDATE source_acquisitions SET title='Mutation denied' WHERE id='a1500000-0000-4000-8000-000000000031';"

expect_denied \
  'application acquisition deletion' \
  "$application_role" \
  "DELETE FROM source_acquisitions WHERE id='a1500000-0000-4000-8000-000000000031';"

expect_denied \
  'application review deletion' \
  "$application_role" \
  "DELETE FROM source_acquisition_reviews WHERE acquisition_id='a1500000-0000-4000-8000-000000000031';"

expect_denied \
  'application DDL' \
  "$application_role" \
  "CREATE TABLE public.application_must_not_create_tables (id integer);"

expect_denied \
  'application pgcrypto digest routine' \
  "$application_role" \
  "SELECT public.digest('abc'::text, 'sha256'::text);"

psql_as "$backup_role" --command="SELECT count(*) FROM accounts;" >/dev/null

expect_denied \
  'backup mutation' \
  "$backup_role" \
  "INSERT INTO accounts (name) VALUES ('backup must not write');"

expect_denied \
  'backup DDL' \
  "$backup_role" \
  "CREATE TABLE public.backup_must_not_create_tables (id integer);"

verify_policy

printf 'postgresql_roles_integration=passed\n'
