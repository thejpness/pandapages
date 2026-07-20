#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
role_script="$repo_root/scripts/postgresql-roles.sh"
api_smoke_script="$repo_root/scripts/tests/postgresql-api-role-smoke.sh"
migration_image=${PP_ROLE_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
api_image=${PP_ROLE_TEST_API_IMAGE:-pandapages-api:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database=pandapages_role_test
readonly admin_user=postgres
readonly owner_role=pandapages_owner
readonly migration_role=pandapages_migrator
readonly application_role=pandapages_app
readonly backup_role=pandapages_backup
readonly legacy_role=pandapages
readonly migration_password='generated-migrator-password-not-for-production'
readonly application_password='generated-application-password-not-for-production'
readonly legacy_password='generated-legacy-password-not-for-production'

for command_name in docker grep mktemp sed sort; do
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
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] || [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Integration test requires a known local Docker Unix socket\n' >&2
  exit 1
fi

for image in "$migration_image" "$api_image"; do
  docker image inspect "$image" >/dev/null 2>&1 || {
    printf 'Required test image is unavailable: %s\n' "$image" >&2
    exit 1
  }
done

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-role-integration.XXXXXX")
resource_prefix="pandapages-pg-role-integration-$$"
source_container="$resource_prefix-source"
source_network="$resource_prefix-network"
source_volume="$resource_prefix-data"
source_created=false
network_created=false
volume_created=false

cleanup() {
  set +e
  if $source_created; then
    docker rm --force "$source_container" >/dev/null 2>&1
  fi
  if $network_created; then
    docker network rm "$source_network" >/dev/null 2>&1
  fi
  if $volume_created; then
    docker volume rm "$source_volume" >/dev/null 2>&1
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT

secret_dir="$test_root/secret"
mkdir -p "$secret_dir"
admin_password_file="$secret_dir/postgres-password"
printf 'generated-admin-password-not-for-production\n' >"$admin_password_file"
chmod 0600 "$admin_password_file"

docker network create --internal \
  --label com.pandapages.disposable=role-integration \
  "$source_network" >/dev/null
network_created=true
docker volume create \
  --label com.pandapages.disposable=role-integration \
  "$source_volume" >/dev/null
volume_created=true

docker run --detach \
  --name "$source_container" \
  --network "$source_network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=role-integration \
  --env POSTGRES_USER="$admin_user" \
  --env POSTGRES_DB="$database" \
  --env POSTGRES_PASSWORD_FILE=/run/secrets/postgres-password \
  --env POSTGRES_INITDB_ARGS=--data-checksums \
  --mount "type=volume,src=$source_volume,dst=/var/lib/postgresql" \
  --mount "type=bind,src=$admin_password_file,dst=/run/secrets/postgres-password,readonly" \
  --health-cmd "pg_isready --username=$admin_user --dbname=$database" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null
source_created=true

health=starting
for ((attempt = 0; attempt < 60; attempt++)); do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$source_container")
  [[ "$health" == healthy ]] && break
  [[ "$health" != unhealthy ]] || {
    printf 'Generated PostgreSQL source became unhealthy\n' >&2
    exit 1
  }
  sleep 1
done
[[ "$health" == healthy ]] || {
  printf 'Timed out waiting for generated PostgreSQL source\n' >&2
  exit 1
}

database_ready=false
for ((attempt = 0; attempt < 30; attempt++)); do
  if docker exec "$source_container" \
    psql -X --username="$admin_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --tuples-only --command='SELECT 1;' \
    >/dev/null 2>&1; then
    database_ready=true
    break
  fi
  sleep 1
done
[[ "$database_ready" == true ]] || {
  printf 'Generated PostgreSQL database did not become connectable\n' >&2
  exit 1
}

apply_policy() {
  "$role_script" apply \
    --container "$source_container" \
    --database "$database" \
    --admin-user "$admin_user" \
    --confirm-apply >/dev/null
}

verify_policy() {
  "$role_script" verify \
    --container "$source_container" \
    --database "$database" \
    --admin-user "$admin_user" >/dev/null
}

psql_as() {
  local role=$1
  shift
  docker exec "$source_container" \
    psql -X --username="$role" --dbname="$database" --set=ON_ERROR_STOP=1 "$@"
}

expect_denied() {
  local description=$1
  local role=$2
  local target_database=$3
  local statement=$4

  if docker exec "$source_container" \
    psql -X --username="$role" --dbname="$target_database" \
    --set=ON_ERROR_STOP=1 --command="$statement" \
    >"$test_root/denied.out" 2>"$test_root/denied.err"; then
    printf 'Privilege denial unexpectedly succeeded: %s\n' "$description" >&2
    exit 1
  fi
}

printf '1..12\n'

"$role_script" audit \
  --container "$source_container" \
  --database "$database" \
  --admin-user "$admin_user" >/dev/null
apply_policy
identity=$(psql_as "$migration_role" --tuples-only --no-align \
  --command="SELECT session_user || '|' || current_user;")
[[ "$identity" == "$migration_role|$owner_role" ]]
printf 'ok 1 - pre-apply audit accepts absent roles and the migrator assumes only the NOLOGIN owner\n'

docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --set=ON_ERROR_STOP=1 <<SQL >/dev/null
ALTER ROLE $migration_role PASSWORD '$migration_password';
ALTER ROLE $application_role PASSWORD '$application_password';
CREATE ROLE $legacy_role LOGIN SUPERUSER PASSWORD '$legacy_password';
SQL

if ! docker run --rm \
  --network "$source_network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=32m \
  --env GOOSE_DRIVER=postgres \
  --env "GOOSE_DBSTRING=postgres://$migration_role:$migration_password@$source_container:5432/$database?sslmode=disable" \
  --env GOOSE_MIGRATION_DIR=/migrations \
  --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
  "$migration_image" up >"$test_root/goose.out" 2>"$test_root/goose.err"; then
  sed 's/generated-migrator-password-not-for-production/[redacted]/g' "$test_root/goose.err" >&2
  exit 1
fi
grep -q 'OK.*00014_reader_2_contract.sql' "$test_root/goose.out" "$test_root/goose.err"
printf 'ok 2 - Goose applies every migration as a non-superuser owner session\n'

docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --set=ON_ERROR_STOP=1 <<SQL >/dev/null
ALTER ROLE $application_role SUPERUSER CREATEDB CREATEROLE INHERIT REPLICATION BYPASSRLS CONNECTION LIMIT 1;
ALTER ROLE $backup_role INHERIT;
GRANT pg_read_all_data TO $backup_role;
GRANT $owner_role TO $migration_role WITH ADMIN TRUE, INHERIT TRUE, SET TRUE;
SQL

apply_policy
verify_policy
apply_policy
verify_policy
printf 'ok 3 - role application corrects drift, is idempotent, and verifies\n'

role_state=$(docker exec "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --tuples-only --no-align \
  --command="SELECT string_agg(rolname || ':' || rolsuper::text || ':' || rolcreatedb::text || ':' || rolcreaterole::text || ':' || rolreplication::text || ':' || rolbypassrls::text, ',' ORDER BY rolname) FROM pg_roles WHERE rolname IN ('$owner_role','$migration_role','$application_role','$backup_role');")
[[ "$role_state" == "$application_role:false:false:false:false:false,$backup_role:false:false:false:false:false,$migration_role:false:false:false:false:false,$owner_role:false:false:false:false:false" ]]
expect_denied 'pgcrypto digest routine' "$application_role" "$database" \
  "SELECT public.digest('abc'::text, 'sha256'::text);"
printf 'ok 4 - all policy roles have non-superuser non-administrative attributes\n'

smoke_tmp="$test_root/api-role-smoke-tmp"
mkdir -p "$smoke_tmp"
env \
  TMPDIR="$smoke_tmp" \
  PP_ROLE_TEST_SOURCE_CONTAINER="$source_container" \
  PP_ROLE_TEST_NETWORK="$source_network" \
  PP_ROLE_TEST_API_IMAGE="$api_image" \
  PP_ROLE_TEST_DATABASE="$database" \
  PP_ROLE_TEST_ADMIN_USER="$admin_user" \
  PP_ROLE_TEST_APPLICATION_ROLE="$application_role" \
  PP_ROLE_TEST_APPLICATION_PASSWORD="$application_password" \
  PP_ROLE_TEST_MIGRATION_ROLE="$migration_role" \
  PP_ROLE_TEST_MIGRATION_PASSWORD="$migration_password" \
  PP_ROLE_TEST_LEGACY_ROLE="$legacy_role" \
  PP_ROLE_TEST_LEGACY_PASSWORD="$legacy_password" \
  "$api_smoke_script" >/dev/null
[[ -z $(find "$smoke_tmp" -mindepth 1 -print -quit) ]] || {
  printf 'API role smoke test left temporary files behind\n' >&2
  exit 1
}

docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --set=ON_ERROR_STOP=1 <<'SQL' >/dev/null
CREATE SCHEMA operator_private;
CREATE TABLE operator_private.private_marker (id integer PRIMARY KEY);
INSERT INTO operator_private.private_marker VALUES (1);
SQL

psql_as "$application_role" --command="BEGIN; INSERT INTO accounts (name) VALUES ('Generated role test'); UPDATE accounts SET name='Generated role test updated' WHERE name='Generated role test'; DELETE FROM accounts WHERE name='Generated role test updated'; COMMIT;" >/dev/null
psql_as "$application_role" --command="SELECT count(*) FROM stories;" >/dev/null
psql_as "$application_role" --command="INSERT INTO stories (account_id, slug, title) SELECT id, 'generated-role-test', 'Pöndá reads 世界 🐼' FROM accounts ORDER BY created_at LIMIT 1; UPDATE stories SET title='Pöndá reads UTF-8 世界 🐼' WHERE slug='generated-role-test'; DELETE FROM stories WHERE slug='generated-role-test';" >/dev/null
printf 'ok 5 - application role unlocks, serves the admin catalogue, and performs current runtime CRUD\n'

expect_denied 'create database' "$application_role" "$database" 'CREATE DATABASE forbidden_database;'
expect_denied 'create role' "$application_role" "$database" 'CREATE ROLE forbidden_role;'
expect_denied 'create schema' "$application_role" "$database" 'CREATE SCHEMA forbidden_schema;'
expect_denied 'create extension' "$application_role" "$database" 'CREATE EXTENSION hstore;'
expect_denied 'alter table' "$application_role" "$database" 'ALTER TABLE stories ADD COLUMN forbidden integer;'
expect_denied 'drop table' "$application_role" "$database" 'DROP TABLE stories;'
expect_denied 'restricted schema' "$application_role" "$database" 'SELECT * FROM operator_private.private_marker;'
expect_denied 'system setting' "$application_role" "$database" "ALTER SYSTEM SET log_min_messages = 'debug1';"
expect_denied 'server file function' "$application_role" "$database" "SELECT pg_read_file('PG_VERSION');"
expect_denied 'unrelated database' "$application_role" postgres 'SELECT 1;'
expect_denied 'owner escalation' "$application_role" "$database" "SET ROLE $owner_role;"
printf 'ok 6 - application role cannot perform DDL, administration, escalation, or unrelated access\n'

psql_as "$migration_role" --command="CREATE TABLE role_future_runtime (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, body text NOT NULL); ALTER TABLE role_future_runtime ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(); CREATE FUNCTION role_required_upper(text) RETURNS text LANGUAGE sql IMMUTABLE STRICT RETURN upper(\$1); REVOKE ALL ON FUNCTION role_required_upper(text) FROM PUBLIC; GRANT EXECUTE ON FUNCTION role_required_upper(text) TO $application_role;" >/dev/null
future_owner=$(docker exec "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --tuples-only --no-align \
  --command="SELECT tableowner FROM pg_tables WHERE schemaname='public' AND tablename='role_future_runtime';")
[[ "$future_owner" == "$owner_role" ]]
psql_as "$application_role" --command="INSERT INTO role_future_runtime(body) VALUES (role_required_upper('Pöndá 世界 🐼')); UPDATE role_future_runtime SET body=role_required_upper(body); DELETE FROM role_future_runtime;" >/dev/null
printf 'ok 7 - future owner-created tables, sequences, and explicit functions work without manual grants\n'

future_acl=$(docker exec "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --tuples-only --no-align \
  --command="SELECT has_table_privilege('$application_role','public.role_future_runtime','SELECT,INSERT,UPDATE,DELETE') || '|' || has_table_privilege('$backup_role','public.role_future_runtime','SELECT') || '|' || has_table_privilege('$backup_role','public.role_future_runtime','INSERT,UPDATE,DELETE,TRUNCATE') || '|' || has_sequence_privilege('$application_role','public.role_future_runtime_id_seq','USAGE,SELECT') || '|' || has_sequence_privilege('$backup_role','public.role_future_runtime_id_seq','SELECT') || '|' || has_sequence_privilege('$backup_role','public.role_future_runtime_id_seq','USAGE,UPDATE');")
[[ "$future_acl" == 'true|true|false|true|true|false' ]]
printf 'ok 8 - future-object defaults grant runtime use and backup read-only access only\n'

docker exec "$source_container" pg_dump \
  --username="$backup_role" \
  --dbname="$database" \
  --schema=public \
  --extension=pgcrypto \
  --format=custom \
  --no-owner \
  --no-acl \
  >"$test_root/database.dump"
docker exec -i "$source_container" pg_restore --list \
  <"$test_root/database.dump" >"$test_root/database.list"
grep -q 'TABLE DATA public stories' "$test_root/database.list"
docker exec "$source_container" pg_dumpall \
  --username="$backup_role" \
  --database="$database" \
  --globals-only \
  --no-role-passwords \
  >"$test_root/globals.sql"
grep -q "CREATE ROLE $application_role;" "$test_root/globals.sql"
if grep -Eq 'PASSWORD[[:space:]]+('"'"'|SCRAM-SHA-256|md5)' "$test_root/globals.sql"; then
  printf 'Password material appeared in globals output\n' >&2
  exit 1
fi
printf 'ok 9 - backup role completes custom and password-free globals dumps\n'

psql_as "$backup_role" --command="SELECT count(*) FROM stories; SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" >/dev/null
expect_denied 'backup insert' "$backup_role" "$database" "INSERT INTO accounts(name) VALUES ('forbidden');"
expect_denied 'backup update' "$backup_role" "$database" "UPDATE accounts SET name='forbidden';"
expect_denied 'backup delete' "$backup_role" "$database" 'DELETE FROM accounts;'
expect_denied 'backup truncate' "$backup_role" "$database" 'TRUNCATE accounts;'
expect_denied 'backup schema create' "$backup_role" "$database" 'CREATE SCHEMA backup_forbidden;'
expect_denied 'backup table alter' "$backup_role" "$database" 'ALTER TABLE stories ADD COLUMN backup_forbidden integer;'
expect_denied 'backup database create' "$backup_role" "$database" 'CREATE DATABASE backup_forbidden;'
expect_denied 'backup role create' "$backup_role" "$database" 'CREATE ROLE backup_forbidden;'
expect_denied 'backup owner escalation' "$backup_role" "$database" "SET ROLE $owner_role;"
printf 'ok 10 - backup role can inspect metadata but cannot write, mutate schema, or escalate\n'

extension_owner=$(docker exec "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" --tuples-only --no-align \
  --command="SELECT pg_get_userbyid(extowner) FROM pg_extension WHERE extname='pgcrypto';")
[[ "$extension_owner" == "$owner_role" ]]
expect_denied 'migration create database' "$migration_role" "$database" 'CREATE DATABASE migration_forbidden;'
expect_denied 'migration create role' "$migration_role" "$database" 'CREATE ROLE migration_forbidden;'
printf 'ok 11 - trusted pgcrypto and DDL need no migration-role superuser capability\n'

after_resources="$test_root/after-resources"
docker ps -aq --filter label=com.pandapages.disposable=role-integration | sort >"$after_resources"
[[ $(wc -l <"$after_resources") -eq 1 ]]
printf 'ok 12 - generated test uses only isolated disposable resources\n'
