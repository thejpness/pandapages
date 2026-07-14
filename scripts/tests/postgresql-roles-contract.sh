#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"
readonly verifier='scripts/postgresql-api-role-verify.sh'

for command_name in docker git grep jq mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

readonly app_url='postgres://pandapages_app:ci-app-password-not-for-production@postgres:5432/pandapages?sslmode=disable'
readonly migration_url='postgres://pandapages_migrator:ci-migration-password-not-for-production@postgres:5432/pandapages?sslmode=disable'
readonly admin_password='ci-admin-password-not-for-production'

common_environment=(
  "APP_DATABASE_URL=$app_url"
  "MIGRATION_DATABASE_URL=$migration_url"
  "POSTGRES_PASSWORD=$admin_password"
  'PP_ADMIN_IPS=192.0.2.1/32'
  'PP_ADMIN_KEY=ci-only-admin-key-not-for-production-00000000'
  'PP_PASSCODE=ci-only-passcode-not-for-production'
  'PP_SESSION_SECRET=ci-only-session-secret-not-for-production-00000000'
)

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-role-contract.XXXXXX")
cleanup() {
  rm -rf -- "$test_root"
}
trap cleanup EXIT

compose_json() {
  local compose_file=$1
  local output=$2
  env "${common_environment[@]}" \
    docker compose --env-file /dev/null -f "$compose_file" config --format json >"$output"
}

expect_compose_failure() {
  local compose_file=$1
  local missing_name=$2

  filtered_environment=()
  for assignment in "${common_environment[@]}"; do
    [[ "${assignment%%=*}" == "$missing_name" ]] || filtered_environment+=("$assignment")
  done

  if env -u "$missing_name" "${filtered_environment[@]}" \
    docker compose --env-file /dev/null -f "$compose_file" config --quiet \
    >"$test_root/missing.out" 2>"$test_root/missing.err"; then
    printf '%s unexpectedly accepted a missing %s\n' "$compose_file" "$missing_name" >&2
    exit 1
  fi
}

printf '1..7\n'

production_json="$test_root/production.json"
development_json="$test_root/development.json"
compose_json docker-compose.yml "$production_json"
compose_json docker-compose.dev.yml "$development_json"
printf 'ok 1 - production and development Compose resolve with separate generated credentials\n'

for compose_file in docker-compose.yml docker-compose.dev.yml; do
  expect_compose_failure "$compose_file" APP_DATABASE_URL
  expect_compose_failure "$compose_file" MIGRATION_DATABASE_URL
done
printf 'ok 2 - runtime and migration credentials each fail closed when absent\n'

for config in "$production_json" "$development_json"; do
  jq -e --arg app "$app_url" --arg migration "$migration_url" '
    .services.api.environment.DATABASE_URL == $app
    and .services.api.environment.PGAPPNAME == "pandapages-api"
    and .services.migrate.environment.GOOSE_DBSTRING == $migration
    and .services.migrate.environment.GOOSE_DRIVER == "postgres"
    and .services.migrate.environment.GOOSE_MIGRATION_DIR == "/migrations"
    and (.services.api.environment | has("GOOSE_DBSTRING") | not)
    and (.services.api.environment | has("POSTGRES_PASSWORD") | not)
    and (.services.migrate.environment | has("DATABASE_URL") | not)
    and (.services.migrate.environment | has("POSTGRES_PASSWORD") | not)
    and (.services.migrate.environment | has("PGAPPNAME") | not)
    and (.services.postgres.environment | has("DATABASE_URL") | not)
    and (.services.postgres.environment | has("GOOSE_DBSTRING") | not)
    and (.services.postgres.environment | has("PGAPPNAME") | not)
    and ([.services.api.environment[]] | index($migration) | not)
    and ([.services.migrate.environment[]] | index($app) | not)
    and (.services.postgres.healthcheck.test | join(" ") | contains("pandapages_app"))
  ' "$config" >/dev/null
done
jq -e --arg admin "$admin_password" ' .services.postgres.environment.POSTGRES_PASSWORD == $admin ' "$production_json" >/dev/null
jq -e ' .services.postgres.environment.POSTGRES_PASSWORD_FILE == "/run/secrets/pg_password" and (.services.postgres.environment | has("POSTGRES_PASSWORD") | not) ' "$development_json" >/dev/null
printf 'ok 3 - API, migration, and administrative containers receive only their own credential\n'

if APP_DATABASE_URL='' MIGRATION_DATABASE_URL="$migration_url" \
  POSTGRES_PASSWORD="$admin_password" PP_ADMIN_IPS=192.0.2.1/32 \
  PP_ADMIN_KEY=test PP_PASSCODE=test PP_SESSION_SECRET=test \
  docker compose --env-file /dev/null -f docker-compose.yml config --quiet \
  >"$test_root/empty-runtime.out" 2>"$test_root/empty-runtime.err"; then
  printf 'Production Compose accepted an empty runtime database URL\n' >&2
  exit 1
fi
if MIGRATION_DATABASE_URL='' APP_DATABASE_URL="$app_url" \
  POSTGRES_PASSWORD="$admin_password" PP_ADMIN_IPS=192.0.2.1/32 \
  PP_ADMIN_KEY=test PP_PASSCODE=test PP_SESSION_SECRET=test \
  docker compose --env-file /dev/null -f docker-compose.yml config --quiet \
  >"$test_root/empty-migration.out" 2>"$test_root/empty-migration.err"; then
  printf 'Production Compose accepted an empty migration database URL\n' >&2
  exit 1
fi
printf 'ok 4 - empty runtime and migration credentials also fail closed\n'

if git grep -n -E 'MIGRATION_DATABASE_URL|APP_DATABASE_URL|PP_BACKUP_DATABASE_USER' -- apps/web >/dev/null; then
  printf 'A database credential contract leaked into frontend source\n' >&2
  exit 1
fi
if git grep -n -E 'MIGRATION_DATABASE_URL|APP_DATABASE_URL' -- ':(glob)apps/api/**/*.go' >/dev/null; then
  printf 'The API source reads host-side migration or application URL names directly\n' >&2
  exit 1
fi
git grep -q -F 'databaseURL:   getenv("DATABASE_URL")' -- apps/api/cmd/api/main.go
git grep -q -F 'db.MustOpen(cfg.databaseURL)' -- apps/api/cmd/api/main.go
printf 'ok 5 - frontend has no database credentials and API accepts only its container-scoped URL\n'

if git grep -n -E 'PP_BACKUP_GLOBALS_USER|pg_read_all_data' -- \
  scripts/postgresql-backup.sh deploy/postgresql-backup/postgresql-backup.env.example >/dev/null; then
  printf 'Backup automation still depends on a bootstrap user or cluster-wide read role\n' >&2
  exit 1
fi
if scripts/postgresql-roles.sh apply --container unreachable --database pandapages \
  >"$test_root/unconfirmed.out" 2>"$test_root/unconfirmed.err"; then
  printf 'Role policy apply did not require explicit confirmation\n' >&2
  exit 1
fi
grep -q 'apply requires --confirm-apply' "$test_root/unconfirmed.err"
printf 'ok 6 - backup and role tooling have no privileged fallback or destructive default\n'

if grep -nE -- 'api_ip|--(api|client)-(ip|address)|--command=.*pg_stat_activity' "$verifier" >/dev/null; then
  printf 'API role verifier contains the failed inline-address SQL pattern\n' >&2
  exit 1
fi
grep -q -- '--file=-' "$verifier"
grep -q 'FROM pg_stat_activity' "$verifier"
grep -q 'host(client_addr)' "$verifier"
grep -q 'application_name' "$verifier"
if "$verifier" \
  --api-container generated-api \
  --postgres-container generated-postgres \
  --database generated_database \
  --session-contract legacy \
  --api-address 'malformed;SELECT-current_user' \
  >"$test_root/address.out" 2>"$test_root/address.err"; then
  printf 'API role verifier accepted caller-provided client-address input\n' >&2
  exit 1
fi
grep -q '^Usage:' "$test_root/address.err"
printf 'ok 7 - API activity verification uses fixed SQL and accepts no client-address input\n'
