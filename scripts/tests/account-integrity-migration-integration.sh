#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repo_root
readonly migration_image=${PP_ACCOUNT_INTEGRITY_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
readonly api_image=${PP_ACCOUNT_INTEGRITY_TEST_API_IMAGE:-pandapages-api:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database_user=postgres
readonly database_password='generated-account-integrity-password-not-for-production'
readonly resource_prefix="pandapages-account-integrity-$$"
readonly postgres_container="$resource_prefix-postgres"
readonly api_container="$resource_prefix-api"
readonly network_name="$resource_prefix-network"
readonly volume_name="$resource_prefix-data"
readonly resource_label="com.pandapages.account-integrity-run=$resource_prefix"
readonly v14_database=pandapages_integrity_v14
readonly full_database=pandapages_integrity_full
readonly clean_database=pandapages_integrity_clean

for command_name in docker grep mktemp sed tr; do
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

for image in "$migration_image" "$api_image"; do
  docker image inspect "$image" >/dev/null 2>&1 || {
    printf 'Required test image is unavailable: %s\n' "$image" >&2
    exit 1
  }
done

if docker container inspect "$postgres_container" >/dev/null 2>&1 ||
  docker container inspect "$api_container" >/dev/null 2>&1; then
  printf 'Refusing to reuse an existing disposable test container\n' >&2
  exit 1
fi

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-account-integrity.XXXXXX")
network_created=false
volume_created=false
postgres_created=false
api_created=false
goose_run_count=0

redact() {
  sed "s/$database_password/[redacted]/g; s#postgres://[^[:space:]]*#[redacted-database-url]#g"
}

cleanup() {
  set +e
  if $api_created; then
    docker rm --force "$api_container" >/dev/null 2>&1
  fi
  docker ps -aq --filter "label=$resource_label" |
    while IFS= read -r container_id; do
      [[ -n "$container_id" ]] || continue
      docker rm --force "$container_id" >/dev/null 2>&1
    done
  if $network_created; then
    docker network rm "$network_name" >/dev/null 2>&1
  fi
  if $volume_created; then
    docker volume rm "$volume_name" >/dev/null 2>&1
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM
trap 'printf "Account-integrity integration failed at line %d\n" "$LINENO" >&2' ERR

docker network create --internal \
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
  --env POSTGRES_DB=postgres \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  --mount "type=volume,src=$volume_name,dst=/var/lib/postgresql" \
  --health-cmd "pg_isready --username=$database_user --dbname=postgres" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null
postgres_created=true

health=starting
for _ in {1..60}; do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$postgres_container")
  [[ "$health" == healthy ]] && break
  [[ "$health" != unhealthy ]] || {
    printf 'Disposable account-integrity PostgreSQL became unhealthy\n' >&2
    exit 1
  }
  sleep 1
done
[[ "$health" == healthy ]] || {
  printf 'Disposable account-integrity PostgreSQL did not become healthy\n' >&2
  exit 1
}

valid_database_name() {
  [[ "$1" =~ ^[a-z][a-z0-9_]{0,62}$ ]]
}

create_database() {
  local database=$1
  valid_database_name "$database" || {
    printf 'Invalid generated database name\n' >&2
    exit 1
  }
  docker exec "$postgres_container" \
    createdb --username="$database_user" "$database"
}

clone_database() {
  local source=$1
  local target=$2
  valid_database_name "$source" && valid_database_name "$target" || {
    printf 'Invalid generated clone database name\n' >&2
    exit 1
  }
  docker exec "$postgres_container" \
    createdb --username="$database_user" --template="$source" "$target"
}

drop_database() {
  local database=$1
  valid_database_name "$database" || {
    printf 'Invalid generated database name for cleanup\n' >&2
    exit 1
  }
  docker exec "$postgres_container" \
    dropdb --username="$database_user" --force --if-exists "$database"
}

psql_query() {
  local database=$1
  local statement=$2
  valid_database_name "$database" || {
    printf 'Invalid generated query database name\n' >&2
    exit 1
  }
  docker exec "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="$statement"
}

psql_file() {
  local database=$1
  valid_database_name "$database" || {
    printf 'Invalid generated SQL database name\n' >&2
    exit 1
  }
  docker exec -i "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 --file=-
}

assert_query() {
  local expected=$1
  local database=$2
  local statement=$3
  local description=$4
  local actual
  actual=$(psql_query "$database" "$statement")
  [[ "$actual" == "$expected" ]] || {
    printf '%s: expected %s, got %s\n' "$description" "$expected" "$actual" >&2
    exit 1
  }
}

run_goose() {
  local database=$1
  shift
  valid_database_name "$database" || {
    printf 'Invalid generated Goose database name\n' >&2
    exit 1
  }
  ((goose_run_count += 1))
  docker run --rm \
    --name "$resource_prefix-goose-$goose_run_count" \
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
    "$migration_image" "$@"
}

seed_v14_data() {
  local database=$1
  psql_file "$database" >/dev/null <<'SQL'
BEGIN;

-- Migration 00011 creates one bootstrap account/profile. Replace only those
-- generated disposable rows so the representative ownership graph is exact.
DELETE FROM profiles;
DELETE FROM accounts;

INSERT INTO accounts (id, name, created_at) VALUES
  ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Household A', '2026-01-01T00:00:00Z'),
  ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Household B', '2026-01-02T00:00:00Z'),
  ('cccccccc-cccc-4ccc-8ccc-cccccccccccc', 'Empty household', '2026-01-03T00:00:00Z');

INSERT INTO profiles (id, account_id, name, created_at) VALUES
  ('aaaaaaaa-1000-4000-8000-000000000001', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Older named reader', '2025-01-01T00:00:00Z'),
  ('aaaaaaaa-1000-4000-8000-000000000002', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Default', '2026-01-01T00:00:00Z'),
  ('bbbbbbbb-1000-4000-8000-000000000001', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'First tied reader', '2026-02-01T00:00:00Z'),
  ('bbbbbbbb-1000-4000-8000-000000000002', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Second tied reader', '2026-02-01T00:00:00Z');

INSERT INTO child_profiles (id, account_id, name, age_months) VALUES
  ('aaaaaaaa-2000-4000-8000-000000000001', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Child A', 72),
  ('bbbbbbbb-2000-4000-8000-000000000001', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Child B', 84);

INSERT INTO prompt_profiles (id, account_id, name) VALUES
  ('aaaaaaaa-2100-4000-8000-000000000001', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'Prompt A'),
  ('bbbbbbbb-2100-4000-8000-000000000001', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Prompt B');

INSERT INTO stories (id, account_id, slug, title, is_published) VALUES
  ('aaaaaaaa-3000-4000-8000-000000000001', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'story-a', 'Story A', true),
  ('bbbbbbbb-3000-4000-8000-000000000001', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'story-b', 'Story B', true);

INSERT INTO story_versions (id, story_id, version, markdown, rendered_html, content_hash) VALUES
  ('aaaaaaaa-4000-4000-8000-000000000001', 'aaaaaaaa-3000-4000-8000-000000000001', 1, 'Story A body', '<p>Story A body</p>', 'integrity-a'),
  ('bbbbbbbb-4000-4000-8000-000000000001', 'bbbbbbbb-3000-4000-8000-000000000001', 1, 'Story B body', '<p>Story B body</p>', 'integrity-b');

UPDATE stories SET
  draft_version_id = CASE id
    WHEN 'aaaaaaaa-3000-4000-8000-000000000001' THEN 'aaaaaaaa-4000-4000-8000-000000000001'::uuid
    ELSE 'bbbbbbbb-4000-4000-8000-000000000001'::uuid
  END,
  published_version_id = CASE id
    WHEN 'aaaaaaaa-3000-4000-8000-000000000001' THEN 'aaaaaaaa-4000-4000-8000-000000000001'::uuid
    ELSE 'bbbbbbbb-4000-4000-8000-000000000001'::uuid
  END
WHERE id IN (
  'aaaaaaaa-3000-4000-8000-000000000001',
  'bbbbbbbb-3000-4000-8000-000000000001'
);

INSERT INTO story_sections (id, story_version_id, kind, title, ordinal) VALUES
  ('aaaaaaaa-5000-4000-8000-000000000001', 'aaaaaaaa-4000-4000-8000-000000000001', 'chapter', 'Chapter A', 1);

INSERT INTO story_segments (
  id, story_version_id, section_id, ordinal, segment_kind, heading_level,
  content_key, content_occurrence, chapter_key, chapter_occurrence,
  markdown, rendered_html, word_count
) VALUES (
  'aaaaaaaa-6000-4000-8000-000000000001',
  'aaaaaaaa-4000-4000-8000-000000000001',
  'aaaaaaaa-5000-4000-8000-000000000001',
  1, 'paragraph', NULL,
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 1,
  NULL, NULL, 'Story A body', '<p>Story A body</p>', 3
);

INSERT INTO profile_settings (
  profile_id, active_child_profile_id, active_prompt_profile_id
) VALUES (
  'aaaaaaaa-1000-4000-8000-000000000002',
  'aaaaaaaa-2000-4000-8000-000000000001',
  'aaaaaaaa-2100-4000-8000-000000000001'
), (
  'bbbbbbbb-1000-4000-8000-000000000001',
  'bbbbbbbb-2000-4000-8000-000000000001',
  'bbbbbbbb-2100-4000-8000-000000000001'
);

INSERT INTO reading_progress (
  profile_id, story_id, story_version_id, locator, percent
) VALUES (
  'aaaaaaaa-1000-4000-8000-000000000002',
  'aaaaaaaa-3000-4000-8000-000000000001',
  'aaaaaaaa-4000-4000-8000-000000000001',
  '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0.5}}',
  0.5
), (
  'bbbbbbbb-1000-4000-8000-000000000001',
  'bbbbbbbb-3000-4000-8000-000000000001',
  'bbbbbbbb-4000-4000-8000-000000000001',
  '{"schema":2,"segment":{"key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","occurrence":1,"ordinal":1,"offset":0.271}}',
  0.37
);

COMMIT;
SQL
}

expect_migration_failure() {
  local ordinal=$1
  local description=$2
  local setup_sql=$3
  local expected_message=$4
  local database="pandapages_integrity_case_$ordinal"
  local stdout_file="$test_root/case-$ordinal.out"
  local stderr_file="$test_root/case-$ordinal.err"

  clone_database "$v14_database" "$database"
  psql_query "$database" "$setup_sql" >/dev/null
  if run_goose "$database" up-to 15 >"$stdout_file" 2>"$stderr_file"; then
    printf 'Corruption case unexpectedly migrated: %s\n' "$description" >&2
    exit 1
  fi
  grep -Fq "$expected_message" "$stdout_file" "$stderr_file" || {
    printf 'Corruption case failed for the wrong reason: %s\n' "$description" >&2
    redact <"$stderr_file" >&2
    exit 1
  }
  assert_query '14' "$database" "
    WITH latest AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM goose_db_version
      WHERE version_id > 0
      ORDER BY version_id, id DESC
    )
    SELECT COALESCE(max(version_id) FILTER (WHERE is_applied), 0)
    FROM latest;
  " \
    "$description retained schema version"
  assert_query '0' "$database" "
    SELECT count(*)
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND (
        (table_name = 'profiles' AND column_name = 'is_default')
        OR (table_name = 'reading_progress' AND column_name = 'account_id')
        OR (table_name = 'profile_settings' AND column_name = 'account_id')
      );
  " "$description left no partial schema"
  drop_database "$database"
  printf 'ok %d - %s fails closed without partial migration\n' "$((ordinal + 2))" "$description"
}

expect_sql_failure() {
  local database=$1
  local description=$2
  local statement=$3
  local expected_constraint=$4
  local output_file="$test_root/sql-failure-${description//[^a-zA-Z0-9]/-}.err"

  if psql_query "$database" "$statement" >"$test_root/sql-failure.out" 2>"$output_file"; then
    printf 'Integrity operation unexpectedly succeeded: %s\n' "$description" >&2
    exit 1
  fi
  grep -Fq "$expected_constraint" "$output_file" || {
    printf 'Integrity operation failed outside expected constraint: %s\n' "$description" >&2
    redact <"$output_file" >&2
    exit 1
  }
}

raw_http() {
  local path=$1
  docker exec "$api_container" sh -c \
    "{ printf 'GET $path HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n'; sleep 3; } | busybox nc -w 5 127.0.0.1 8080" |
    tr -d '\r'
}

print_local_response() {
  local description=$1
  local response_file=$2

  printf 'Local HTTP response for %s:\n' "$description" >&2
  if [[ -s "$response_file" ]]; then
    sed -n '1,80p' "$response_file" >&2
  else
    printf '(empty response)\n' >&2
  fi
}

print_readiness_diagnostics() {
  local description=$1
  local response_file=$2
  local sql_state_file=$3

  print_local_response "$description" "$response_file"
  printf 'Direct local readiness SQL state for %s:\n' "$description" >&2
  if [[ -s "$sql_state_file" ]]; then
    sed -n '1,20p' "$sql_state_file" >&2
  else
    printf '(state unavailable)\n' >&2
  fi
  printf 'Redacted local API logs for %s:\n' "$description" >&2
  if ! docker logs "$api_container" 2>&1 | redact | sed -n '1,120p' >&2; then
    printf '(API logs unavailable)\n' >&2
  fi
}

probe_readiness() {
  local database=$1
  local expected_status=$2
  local expected_body=$3
  local environment_file="$test_root/api-$database.env"
  local response_file="$test_root/api-$database.response"
  local sql_state_file="$test_root/api-$database-readiness-state.out"

  psql_query "$database" "
    SELECT 'metadata|' || (to_regclass('public.goose_db_version') IS NOT NULL)::text;
    WITH latest_version_state AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM public.goose_db_version
      WHERE version_id > 0
      ORDER BY version_id, id DESC
    )
    SELECT 'state|' || count(*)::text || '|' || COALESCE(min(version_id), 0)::text ||
      '|' || COALESCE(max(version_id), 0)::text || '|' ||
      COALESCE(bool_and(is_applied), false)::text
    FROM latest_version_state;
  " >"$sql_state_file"

  {
    printf 'DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
      "$database_user" "$database_password" "$postgres_container" "$database"
    printf 'PGAPPNAME=pandapages-account-integrity-test\n'
    printf 'PP_PASSCODE=123456\n'
    printf 'PP_ADMIN_KEY=generated-admin-key-not-for-production-00000000\n'
    printf 'PP_SESSION_SECRET=generated-session-secret-not-for-production-00000000\n'
    printf 'PP_LOG_LEVEL=info\n'
  } >"$environment_file"
  chmod 0600 "$environment_file"

  docker run --detach \
    --name "$api_container" \
    --network "$network_name" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --label com.pandapages.disposable=account-integrity-integration \
    --label "$resource_label" \
    --env-file "$environment_file" \
    "$api_image" >/dev/null
  api_created=true

  api_healthy=false
  for _ in {1..30}; do
    if docker exec "$api_container" \
      wget -Y off -q -T 3 -O /dev/null http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
      api_healthy=true
      break
    fi
    sleep 1
  done
  [[ "$api_healthy" == true ]] || {
    printf 'Disposable API did not become healthy for %s\n' "$database" >&2
    docker logs "$api_container" 2>&1 | redact >&2
    exit 1
  }

  if ! raw_http /healthz >"$response_file"; then
    printf 'Raw local HTTP transport failed for /healthz on %s\n' "$database" >&2
    print_readiness_diagnostics "/healthz on $database" "$response_file" "$sql_state_file"
    return 1
  fi
  if ! grep -Eq '^HTTP/1\.[01] 200 ' "$response_file"; then
    printf 'Local /healthz did not return HTTP 200 on %s\n' "$database" >&2
    print_readiness_diagnostics "/healthz on $database" "$response_file" "$sql_state_file"
    return 1
  fi
  if ! grep -Fxq 'ok' "$response_file"; then
    printf 'Local /healthz response body was not the liveness contract on %s\n' "$database" >&2
    print_readiness_diagnostics "/healthz on $database" "$response_file" "$sql_state_file"
    return 1
  fi

  if ! raw_http /readyz >"$response_file"; then
    printf 'Raw local HTTP transport failed for /readyz on %s\n' "$database" >&2
    print_readiness_diagnostics "/readyz on $database" "$response_file" "$sql_state_file"
    return 1
  fi
  if ! grep -Eq "^HTTP/1\.[01] $expected_status " "$response_file"; then
    printf 'Local /readyz returned an unexpected status on %s\n' "$database" >&2
    print_readiness_diagnostics "/readyz on $database" "$response_file" "$sql_state_file"
    return 1
  fi
  if ! grep -Fq "$expected_body" "$response_file"; then
    printf 'Local /readyz returned an unexpected body on %s\n' "$database" >&2
    print_readiness_diagnostics "/readyz on $database" "$response_file" "$sql_state_file"
    return 1
  fi

  docker rm --force "$api_container" >/dev/null
  api_created=false
  rm -f -- "$environment_file"
}

printf '1..16\n'

create_database "$full_database"
run_goose "$full_database" up >"$test_root/full-up.out" 2>"$test_root/full-up.err"
grep -q 'OK.*00015_account_ownership_integrity.sql' \
  "$test_root/full-up.out" "$test_root/full-up.err"
assert_query '15|1|1|0' "$full_database" "
  SELECT
    (WITH latest AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM goose_db_version WHERE version_id > 0 ORDER BY version_id, id DESC
    ) SELECT max(version_id) FILTER (WHERE is_applied) FROM latest),
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles WHERE is_default),
    (SELECT count(*) FROM pg_constraint WHERE contype = 'f' AND NOT convalidated);
" 'fresh full migration chain'
printf 'ok 1 - a fresh database migrates through account-integrity version 15\n'

create_database "$v14_database"
run_goose "$v14_database" up-to 14 >"$test_root/v14-up.out" 2>"$test_root/v14-up.err"
seed_v14_data "$v14_database"
assert_query '14|3|4|2|2|2' "$v14_database" "
  SELECT
    (WITH latest AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM goose_db_version WHERE version_id > 0 ORDER BY version_id, id DESC
    ) SELECT max(version_id) FILTER (WHERE is_applied) FROM latest),
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles),
    (SELECT count(*) FROM stories),
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM profile_settings);
" 'representative v14 baseline'
printf 'ok 2 - representative valid v14 ownership data is prepared in isolation\n'
assert_query 'bbbbbbbb-1000-4000-8000-000000000001|bbbbbbbb-3000-4000-8000-000000000001|bbbbbbbb-4000-4000-8000-000000000001|true|true' "$v14_database" "
  SELECT
    profile_id,
    story_id,
    story_version_id,
    (locator = jsonb_build_object(
      'schema', 2,
      'segment', jsonb_build_object(
        'key', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        'occurrence', 1,
        'ordinal', 1,
        'offset', 0.271
      )
    ))::text,
    (percent = 0.37::real)::text
  FROM reading_progress
  WHERE profile_id='bbbbbbbb-1000-4000-8000-000000000001'
    AND story_id='bbbbbbbb-3000-4000-8000-000000000001';
" 'Household B v14 progress payload'

expect_migration_failure 1 'orphan profile' \
  "INSERT INTO profiles (id, account_id, name) VALUES ('d0000001-0000-4000-8000-000000000001','dddddddd-dddd-4ddd-8ddd-dddddddddddd','Orphan');" \
  'account ownership preflight failed: orphan profile'
expect_migration_failure 2 'orphan story' \
  "INSERT INTO stories (id, account_id, slug, title) VALUES ('d0000002-0000-4000-8000-000000000001','dddddddd-dddd-4ddd-8ddd-dddddddddddd','orphan','Orphan');" \
  'account ownership preflight failed: orphan story'
expect_migration_failure 3 'orphan child profile' \
  "INSERT INTO child_profiles (id, account_id, name, age_months) VALUES ('d0000003-0000-4000-8000-000000000001','dddddddd-dddd-4ddd-8ddd-dddddddddddd','Orphan',72);" \
  'account ownership preflight failed: orphan child profile'
expect_migration_failure 4 'orphan prompt profile' \
  "INSERT INTO prompt_profiles (id, account_id, name) VALUES ('d0000004-0000-4000-8000-000000000001','dddddddd-dddd-4ddd-8ddd-dddddddddddd','Orphan');" \
  'account ownership preflight failed: orphan prompt profile'
expect_migration_failure 5 'cross-account progress' \
  "INSERT INTO reading_progress (profile_id,story_id,story_version_id,locator,percent) VALUES ('aaaaaaaa-1000-4000-8000-000000000001','bbbbbbbb-3000-4000-8000-000000000001','bbbbbbbb-4000-4000-8000-000000000001','{\"schema\":2,\"segment\":{\"key\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"occurrence\":1,\"ordinal\":1,\"offset\":0}}',0);" \
  'account ownership preflight failed: cross-account progress'
expect_migration_failure 6 'cross-account child settings' \
  "UPDATE profile_settings SET active_child_profile_id='bbbbbbbb-2000-4000-8000-000000000001' WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';" \
  'account ownership preflight failed: cross-account child settings'
expect_migration_failure 7 'cross-account prompt settings' \
  "UPDATE profile_settings SET active_prompt_profile_id='bbbbbbbb-2100-4000-8000-000000000001' WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';" \
  'account ownership preflight failed: cross-account prompt settings'
expect_migration_failure 8 'cross-story progress version' \
  "UPDATE reading_progress SET story_version_id='bbbbbbbb-4000-4000-8000-000000000001' WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';" \
  'account ownership preflight failed: progress version belongs to another story'

clone_database "$v14_database" "$clean_database"
run_goose "$clean_database" up-to 15 >"$test_root/clean-up.out" 2>"$test_root/clean-up.err"
assert_query '15|15|0|3|3' "$clean_database" "
  SELECT
    (WITH latest AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM goose_db_version WHERE version_id > 0 ORDER BY version_id, id DESC
    ) SELECT max(version_id) FILTER (WHERE is_applied) FROM latest),
    (SELECT count(*) FROM pg_constraint WHERE conname IN (
      'profiles_account_id_fkey','stories_account_id_fkey',
      'child_profiles_account_id_fkey','prompt_profiles_account_id_fkey',
      'profiles_id_account_id_key','stories_id_account_id_key',
      'child_profiles_id_account_id_key','prompt_profiles_id_account_id_key',
      'story_versions_id_story_id_key','reading_progress_profile_account_fkey',
      'reading_progress_story_account_fkey','reading_progress_story_version_story_fkey',
      'profile_settings_profile_account_fkey','profile_settings_child_account_fkey',
      'profile_settings_prompt_account_fkey'
    ) AND convalidated),
    (SELECT count(*) FROM pg_constraint WHERE contype = 'f' AND NOT convalidated),
    (SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND
      ((table_name='profiles' AND column_name='is_default' AND is_nullable='NO') OR
       (table_name='reading_progress' AND column_name='account_id' AND is_nullable='NO') OR
       (table_name='profile_settings' AND column_name='account_id' AND is_nullable='NO'))),
    (SELECT count(*) FROM pg_class WHERE oid IN (
      to_regclass('public.profiles_one_default_per_account_idx'),
      to_regclass('public.reading_progress_story_account_idx'),
      to_regclass('public.reading_progress_story_version_story_idx')
    ));
" 'validated ownership schema'
assert_query 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa|aaaaaaaa-1000-4000-8000-000000000002|Default|1|0|1|6|bbbbbbbb-1000-4000-8000-000000000001|bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb|bbbbbbbb-1000-4000-8000-000000000001|bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb|bbbbbbbb-2000-4000-8000-000000000001|bbbbbbbb-2100-4000-8000-000000000001' "$clean_database" "
  SELECT
    (SELECT account_id FROM reading_progress WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT account_id FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT account_id FROM stories WHERE id='aaaaaaaa-3000-4000-8000-000000000001'),
    (SELECT id FROM profiles WHERE account_id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' AND is_default),
    (SELECT name FROM profiles WHERE account_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' AND is_default),
    (SELECT count(*) FROM profiles WHERE account_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' AND name='Default' AND is_default),
    (SELECT count(*) FROM profiles WHERE account_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' AND name<>'Default' AND is_default),
    (SELECT count(*) FROM profiles WHERE account_id='cccccccc-cccc-4ccc-8ccc-cccccccccccc' AND name='Default' AND is_default),
    (SELECT count(*) FROM profiles),
    (SELECT profile_id FROM reading_progress WHERE story_id='bbbbbbbb-3000-4000-8000-000000000001'),
    (SELECT account_id FROM reading_progress WHERE story_id='bbbbbbbb-3000-4000-8000-000000000001'),
    (SELECT profile_id FROM profile_settings WHERE profile_id='bbbbbbbb-1000-4000-8000-000000000001'),
    (SELECT account_id FROM profile_settings WHERE profile_id='bbbbbbbb-1000-4000-8000-000000000001'),
    (SELECT active_child_profile_id FROM profile_settings WHERE profile_id='bbbbbbbb-1000-4000-8000-000000000001'),
    (SELECT active_prompt_profile_id FROM profile_settings WHERE profile_id='bbbbbbbb-1000-4000-8000-000000000001');
" 'ownership and legacy Default-profile backfill'
assert_query 'bbbbbbbb-1000-4000-8000-000000000001|bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb|bbbbbbbb-3000-4000-8000-000000000001|bbbbbbbb-4000-4000-8000-000000000001|true|true|0' "$clean_database" "
  SELECT
    progress.profile_id,
    progress.account_id,
    progress.story_id,
    progress.story_version_id,
    (progress.locator = jsonb_build_object(
      'schema', 2,
      'segment', jsonb_build_object(
        'key', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        'occurrence', 1,
        'ordinal', 1,
        'offset', 0.271
      )
    ))::text,
    (progress.percent = 0.37::real)::text,
    (
      SELECT count(*)
      FROM reading_progress AS default_progress
      JOIN profiles AS default_profile ON default_profile.id = default_progress.profile_id
      WHERE default_profile.account_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
        AND default_profile.name='Default'
    )
  FROM reading_progress AS progress
  WHERE progress.profile_id='bbbbbbbb-1000-4000-8000-000000000001'
    AND progress.story_id='bbbbbbbb-3000-4000-8000-000000000001';
" 'Household B v15 progress payload and new Default isolation'
assert_query 'profiles_account_id_fkey:r,stories_account_id_fkey:r,child_profiles_account_id_fkey:r,prompt_profiles_account_id_fkey:r' "$clean_database" "
  SELECT string_agg(conname || ':' || confdeltype::text, ',' ORDER BY CASE conname
    WHEN 'profiles_account_id_fkey' THEN 1 WHEN 'stories_account_id_fkey' THEN 2
    WHEN 'child_profiles_account_id_fkey' THEN 3 ELSE 4 END)
  FROM pg_constraint
  WHERE conname IN ('profiles_account_id_fkey','stories_account_id_fkey',
    'child_profiles_account_id_fkey','prompt_profiles_account_id_fkey');
" 'account delete actions'
printf 'ok 11 - valid data receives validated ownership, deterministic defaults, and restrictive roots\n'

expect_sql_failure "$clean_database" 'cross-account progress insert' \
  "INSERT INTO reading_progress (account_id,profile_id,story_id,story_version_id,locator,percent) VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','aaaaaaaa-1000-4000-8000-000000000001','bbbbbbbb-3000-4000-8000-000000000001','bbbbbbbb-4000-4000-8000-000000000001','{\"schema\":2,\"segment\":{\"key\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"occurrence\":1,\"ordinal\":1,\"offset\":0}}',0);" \
  'reading_progress_story_account_fkey'
expect_sql_failure "$clean_database" 'cross-account progress update' \
  "UPDATE reading_progress SET story_id='bbbbbbbb-3000-4000-8000-000000000001',story_version_id='bbbbbbbb-4000-4000-8000-000000000001' WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';" \
  'reading_progress_story_account_fkey'
expect_sql_failure "$clean_database" 'cross-account settings insert' \
  "INSERT INTO profile_settings (profile_id,account_id) VALUES ('bbbbbbbb-1000-4000-8000-000000000002','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa');" \
  'profile_settings_profile_account_fkey'
expect_sql_failure "$clean_database" 'cross-account child settings update' \
  "UPDATE profile_settings SET active_child_profile_id='bbbbbbbb-2000-4000-8000-000000000001' WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';" \
  'profile_settings_child_account_fkey'
expect_sql_failure "$clean_database" 'cross-account prompt settings update' \
  "UPDATE profile_settings SET active_prompt_profile_id='bbbbbbbb-2100-4000-8000-000000000001' WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';" \
  'profile_settings_prompt_account_fkey'
assert_query '2|2' "$clean_database" "
  SELECT
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM profile_settings);
" 'failed ownership writes preserve rows'
printf 'ok 12 - PostgreSQL rejects cross-account progress and settings inserts and updates\n'

psql_query "$clean_database" "DELETE FROM prompt_profiles WHERE id='aaaaaaaa-2100-4000-8000-000000000001';" >/dev/null
assert_query '1|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa|aaaaaaaa-2000-4000-8000-000000000001|null' "$clean_database" "
  SELECT
    (SELECT count(*) FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT account_id FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT coalesce(active_child_profile_id::text, 'null') FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT coalesce(active_prompt_profile_id::text, 'null') FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002');
" 'active prompt deletion preserves settings account tuple and child selection'
psql_query "$clean_database" "
  INSERT INTO prompt_profiles (id, account_id, name) VALUES ('aaaaaaaa-2100-4000-8000-000000000002','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','Prompt replacement');
  UPDATE profile_settings
  SET active_prompt_profile_id='aaaaaaaa-2100-4000-8000-000000000002'
  WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002';
  DELETE FROM child_profiles WHERE id='aaaaaaaa-2000-4000-8000-000000000001';
" >/dev/null
assert_query '1|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa|null|aaaaaaaa-2100-4000-8000-000000000002' "$clean_database" "
  SELECT
    (SELECT count(*) FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT account_id FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT coalesce(active_child_profile_id::text, 'null') FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002'),
    (SELECT coalesce(active_prompt_profile_id::text, 'null') FROM profile_settings WHERE profile_id='aaaaaaaa-1000-4000-8000-000000000002');
" 'active child deletion preserves settings account tuple and prompt selection'

expect_sql_failure "$clean_database" 'owned account deletion' \
  "DELETE FROM accounts WHERE id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';" \
  '_account_id_fkey'
expect_sql_failure "$clean_database" 'multiple account defaults' \
  "UPDATE profiles SET is_default=true WHERE id='aaaaaaaa-1000-4000-8000-000000000001';" \
  'profiles_one_default_per_account_idx'
psql_query "$clean_database" "
  INSERT INTO accounts (id, name) VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'Deletable empty household');
  DELETE FROM accounts WHERE id='dddddddd-dddd-4ddd-8ddd-dddddddddddd';
" >/dev/null
assert_query '3|6|3' "$clean_database" "
  SELECT
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles),
    (SELECT count(*) FROM profiles WHERE is_default);
" 'account deletion and default policy'
printf 'ok 13 - owned account deletion and duplicate defaults fail while a new empty account remains deletable\n'

probe_readiness "$v14_database" 503 '"reason":"schema_not_ready"'
probe_readiness "$clean_database" 200 '"status":"ready"'
printf 'ok 14 - the API reports v14 schema_not_ready and v15 ready while health stays live\n'

run_goose "$clean_database" down-to 14 >"$test_root/clean-down.out" 2>"$test_root/clean-down.err"
assert_query '14|0|6|3|6|2|2|2|2' "$clean_database" "
  SELECT
    (WITH latest AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM goose_db_version WHERE version_id > 0 ORDER BY version_id, id DESC
    ) SELECT max(version_id) FILTER (WHERE is_applied) FROM latest),
    (SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND
      ((table_name='profiles' AND column_name='is_default') OR
       (table_name='reading_progress' AND column_name='account_id') OR
       (table_name='profile_settings' AND column_name='account_id'))),
    (SELECT count(*) FROM pg_constraint WHERE conname IN (
      'reading_progress_profile_id_fkey','reading_progress_story_id_fkey',
      'reading_progress_story_version_id_fkey','profile_settings_profile_id_fkey',
      'profile_settings_active_child_profile_id_fkey',
      'profile_settings_active_prompt_profile_id_fkey') AND convalidated),
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles),
    (SELECT count(*) FROM stories),
    (SELECT count(*) FROM story_versions),
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM profile_settings);
" 'v14 rollback shape and data'
printf 'ok 15 - down migration restores the v14 shape without losing existing data\n'
assert_query 'bbbbbbbb-1000-4000-8000-000000000001|bbbbbbbb-3000-4000-8000-000000000001|bbbbbbbb-4000-4000-8000-000000000001|true|true|0' "$clean_database" "
  SELECT
    progress.profile_id,
    progress.story_id,
    progress.story_version_id,
    (progress.locator = jsonb_build_object(
      'schema', 2,
      'segment', jsonb_build_object(
        'key', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        'occurrence', 1,
        'ordinal', 1,
        'offset', 0.271
      )
    ))::text,
    (progress.percent = 0.37::real)::text,
    (
      SELECT count(*)
      FROM reading_progress AS default_progress
      JOIN profiles AS default_profile ON default_profile.id = default_progress.profile_id
      WHERE default_profile.account_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
        AND default_profile.name='Default'
    )
  FROM reading_progress AS progress
  WHERE progress.profile_id='bbbbbbbb-1000-4000-8000-000000000001'
    AND progress.story_id='bbbbbbbb-3000-4000-8000-000000000001';
" 'Household B rollback progress payload and retained Default isolation'

run_goose "$clean_database" up-to 15 >"$test_root/clean-reup.out" 2>"$test_root/clean-reup.err"
assert_query '15|3|6|2|2|1|1|2|2' "$clean_database" "
  SELECT
    (WITH latest AS (
      SELECT DISTINCT ON (version_id) version_id, is_applied
      FROM goose_db_version WHERE version_id > 0 ORDER BY version_id, id DESC
    ) SELECT max(version_id) FILTER (WHERE is_applied) FROM latest),
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles),
    (SELECT count(*) FROM stories),
    (SELECT count(*) FROM story_versions),
    (SELECT count(*) FROM story_sections),
    (SELECT count(*) FROM story_segments),
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM profile_settings);
" 'v15 reapplication data preservation'
assert_query 'bbbbbbbb-1000-4000-8000-000000000001|bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb|bbbbbbbb-3000-4000-8000-000000000001|bbbbbbbb-4000-4000-8000-000000000001|true|true|0' "$clean_database" "
  SELECT
    progress.profile_id,
    progress.account_id,
    progress.story_id,
    progress.story_version_id,
    (progress.locator = jsonb_build_object(
      'schema', 2,
      'segment', jsonb_build_object(
        'key', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        'occurrence', 1,
        'ordinal', 1,
        'offset', 0.271
      )
    ))::text,
    (progress.percent = 0.37::real)::text,
    (
      SELECT count(*)
      FROM reading_progress AS default_progress
      JOIN profiles AS default_profile ON default_profile.id = default_progress.profile_id
      WHERE default_profile.account_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
        AND default_profile.name='Default'
    )
  FROM reading_progress AS progress
  WHERE progress.profile_id='bbbbbbbb-1000-4000-8000-000000000001'
    AND progress.story_id='bbbbbbbb-3000-4000-8000-000000000001';
" 'Household B v15 progress payload and new Default isolation'

assert_query '3|3|0' "$clean_database" "
  SELECT
    (SELECT count(*) FROM profiles WHERE is_default),
    (SELECT count(*) FROM profiles WHERE name='Default'),
    (SELECT count(*) FROM pg_constraint WHERE contype='f' AND NOT convalidated);
" 'v15 reapplication constraints'
printf 'ok 16 - reapplication restores validated v15 integrity and preserves the hierarchy\n'

cleanup
trap - EXIT HUP INT TERM
[[ -z $(docker ps -aq --filter "label=$resource_label") ]]
[[ -z $(docker network ls -q --filter "label=$resource_label") ]]
[[ -z $(docker volume ls -q --filter "label=$resource_label") ]]
[[ ! -e "$test_root" ]]
printf 'account_integrity_migration_integration=passed\n'
