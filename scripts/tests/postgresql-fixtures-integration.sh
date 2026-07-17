#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
seed_script="$repo_root/scripts/dev/seed-test-data.sh"
migration_image=${PP_FIXTURE_TEST_MIGRATION_IMAGE:-pandapages-migrate:role-test}
api_image=${PP_FIXTURE_TEST_API_IMAGE:-pandapages-api:role-test}
readonly postgres_image='postgres:18.1-alpine@sha256:b40d931bd0e7ce6eecc59a5a6ac3b3c04a01e559750e73e7086b6dbd7f8bf545'
readonly database=pandapages
readonly database_user=pandapages
readonly database_password='generated-fixture-password-not-for-production'
readonly resource_prefix="pandapages-fixture-integration-$$"
readonly postgres_container="$resource_prefix-postgres"
readonly api_container="$resource_prefix-api"
readonly network="$resource_prefix-network"
readonly volume="$resource_prefix-data"

for command_name in curl docker grep mktemp sed; do
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
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] || [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Fixture integration requires a known local Docker Unix socket\n' >&2
  exit 1
fi

for image in "$migration_image" "$api_image"; do
  docker image inspect "$image" >/dev/null 2>&1 || {
    printf 'Required test image is unavailable: %s\n' "$image" >&2
    exit 1
  }
done

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-fixture-integration.XXXXXX")
postgres_created=false
api_created=false
network_created=false
volume_created=false

cleanup() {
  set +e
  if $api_created; then
    docker rm --force "$api_container" >/dev/null 2>&1
    api_created=false
  fi
  if $postgres_created; then
    docker rm --force "$postgres_container" >/dev/null 2>&1
    postgres_created=false
  fi
  if $network_created; then
    docker network rm "$network" >/dev/null 2>&1
    network_created=false
  fi
  if $volume_created; then
    docker volume rm "$volume" >/dev/null 2>&1
    volume_created=false
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM
trap 'printf "Fixture integration failed at line %d\n" "$LINENO" >&2' ERR

docker network create \
  --label com.pandapages.disposable=fixture-migration-integration \
  "$network" >/dev/null
network_created=true

docker volume create \
  --label com.pandapages.disposable=fixture-migration-integration \
  "$volume" >/dev/null
volume_created=true

docker run --detach \
  --name "$postgres_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label com.pandapages.test-seed-target=disposable-fixture-integration \
  --env POSTGRES_DB="$database" \
  --env POSTGRES_USER="$database_user" \
  --env POSTGRES_PASSWORD="$database_password" \
  --mount "type=volume,src=$volume,dst=/var/lib/postgresql" \
  --health-cmd "pg_isready --username=$database_user --dbname=$database" \
  --health-interval 1s \
  --health-timeout 3s \
  --health-retries 60 \
  "$postgres_image" >/dev/null
postgres_created=true

health=starting
for ((attempt = 0; attempt < 60; attempt++)); do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$postgres_container")
  [[ "$health" == healthy ]] && break
  [[ "$health" != unhealthy ]] || {
    printf 'Disposable fixture PostgreSQL became unhealthy\n' >&2
    exit 1
  }
  sleep 1
done
[[ "$health" == healthy ]] || {
  printf 'Timed out waiting for disposable fixture PostgreSQL\n' >&2
  exit 1
}

psql_query() {
  local statement=$1
  docker exec "$postgres_container" \
    psql -X --username="$database_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="$statement"
}

assert_query() {
  local expected=$1
  local statement=$2
  local description=$3
  local actual
  actual=$(psql_query "$statement")
  [[ "$actual" == "$expected" ]] || {
    printf '%s: expected %s, got %s\n' "$description" "$expected" "$actual" >&2
    exit 1
  }
}

run_goose() {
  docker run --rm \
    --network "$network" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --env GOOSE_DRIVER=postgres \
    --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$postgres_container:5432/$database?sslmode=disable" \
    --env GOOSE_MIGRATION_DIR=/migrations \
    --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
    "$migration_image" "$@"
}

reset_database() {
  psql_query 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null 2>&1
}

expect_seed_failure() {
  local name=$1
  shift
  if "$@" >"$test_root/$name.out" 2>"$test_root/$name.err"; then
    printf 'Seed safety case unexpectedly succeeded: %s\n' "$name" >&2
    exit 1
  fi
}

printf '1..12\n'

run_goose up >"$test_root/fresh-goose.out" 2>"$test_root/fresh-goose.err"
grep -q 'OK.*00013_remove_historical_test_fixtures.sql' \
  "$test_root/fresh-goose.out" "$test_root/fresh-goose.err"
assert_query 't' "
  SELECT bool_and(relation IS NOT NULL)
  FROM (VALUES
    (to_regclass('public.accounts')),
    (to_regclass('public.child_profiles')),
    (to_regclass('public.generation_jobs')),
    (to_regclass('public.profile_settings')),
    (to_regclass('public.profiles')),
    (to_regclass('public.reading_progress')),
    (to_regclass('public.stories')),
    (to_regclass('public.story_sections')),
    (to_regclass('public.story_segments')),
    (to_regclass('public.story_versions'))
  ) AS required(relation);
" 'fresh schema tables'
assert_query '0|0|0|0|0|0|0|0|0|0|0|0|0' "
  SELECT
    (SELECT count(*) FROM stories),
    (SELECT count(*) FROM story_versions),
    (SELECT count(*) FROM story_sections),
    (SELECT count(*) FROM story_segments),
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM child_profiles),
    (SELECT count(*) FROM prompt_profiles),
    (SELECT count(*) FROM generation_jobs),
    (SELECT count(*) FROM profile_settings),
    (SELECT count(*) FROM works),
    (SELECT count(*) FROM contributors),
    (SELECT count(*) FROM story_contributors),
    (SELECT count(*) FROM assets);
" 'fresh migration fixture inventory'
printf 'ok 1 - fresh migrations leave the complete application schema without fixture content\n'

assert_query '1|1|0' "
  SELECT
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles WHERE name = 'Default'),
    (SELECT count(*) FROM pg_constraint WHERE contype = 'f' AND NOT convalidated);
" 'fresh bootstrap and constraints'
printf 'ok 2 - runtime bootstrap records and validated foreign keys remain intact\n'

reset_database
run_goose up-to 12 >"$test_root/pre-cleanup-goose.out" 2>"$test_root/pre-cleanup-goose.err"
assert_query '3|3|1|9|1|1|1|1|3|2|2' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_sections AS section JOIN story_versions AS version ON version.id = section.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM child_profiles WHERE name = 'Ted'),
    (SELECT count(*) FROM prompt_profiles WHERE name = 'Bedtime calm v1'),
    (SELECT count(*) FROM generation_jobs WHERE model = 'seed-model' AND prompt_version = 'v1'),
    (SELECT count(*) FROM works WHERE canonical_title IN ('The Fox and the Grapes','The Little Star and the Moon','Frankenstein; or, The Modern Prometheus')),
    (SELECT count(*) FROM contributors WHERE name IN ('Aesop','Mary Shelley')),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug IN ('the-fox-and-the-grapes','frankenstein-excerpt-ch1'));
" 'historical fixture inventory'
printf 'ok 3 - the real pre-cleanup migration path recreates the complete historical fixture set\n'

docker exec -i "$postgres_container" \
  psql -X --username="$database_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --file=- >/dev/null <<'SQL'
WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO child_profiles (id, account_id, name, age_months, interests, sensitivities)
SELECT 'a11e0000-0000-4000-8000-000000000002', id, 'Preserved child', 96, '["unrelated"]', '[]'
FROM target_account;

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO prompt_profiles (id, account_id, name, rules, schema_version)
SELECT 'a11e0000-0000-4000-8000-000000000003', id, 'Preserved prompt', '{"unrelated":true}', 7
FROM target_account;

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO profiles (id, account_id, name)
SELECT 'a11e0000-0000-4000-8000-000000000001', id, 'Preserved profile'
FROM target_account;

INSERT INTO profile_settings (profile_id, active_child_profile_id, active_prompt_profile_id)
VALUES (
  'a11e0000-0000-4000-8000-000000000001',
  'a11e0000-0000-4000-8000-000000000002',
  'a11e0000-0000-4000-8000-000000000003'
);

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at ASC, id ASC LIMIT 1
), fox_work AS (
  SELECT id FROM works WHERE canonical_title = 'The Fox and the Grapes' ORDER BY created_at ASC, id ASC LIMIT 1
)
INSERT INTO stories (id, account_id, slug, title, author, language, source, work_id)
SELECT
  'a11e0000-0000-4000-8000-000000000010',
  target_account.id,
  'preserved-unrelated-story',
  'Preserved unrelated story',
  'Unrelated author',
  'en-GB',
  '{"origin":"fixture-migration-preservation-test"}',
  fox_work.id
FROM target_account, fox_work;

INSERT INTO story_versions (id, story_id, version, frontmatter, markdown, rendered_html, content_hash)
VALUES (
  'a11e0000-0000-4000-8000-000000000011',
  'a11e0000-0000-4000-8000-000000000010',
  1,
  '{"unrelated":true}',
  '# Preserved unrelated story',
  '<h1>Preserved unrelated story</h1>',
  encode(digest('# Preserved unrelated story', 'sha256'), 'hex')
);

UPDATE stories
SET published_version_id = 'a11e0000-0000-4000-8000-000000000011',
    draft_version_id = 'a11e0000-0000-4000-8000-000000000011',
    is_published = true
WHERE id = 'a11e0000-0000-4000-8000-000000000010';

INSERT INTO story_segments (id, story_version_id, ordinal, locator, markdown, rendered_html, word_count)
VALUES (
  'a11e0000-0000-4000-8000-000000000012',
  'a11e0000-0000-4000-8000-000000000011',
  1,
  '{"type":"heading","h":1,"index":0}',
  '# Preserved unrelated story',
  '<h1>Preserved unrelated story</h1>',
  4
);

INSERT INTO story_contributors (story_id, contributor_id, role)
SELECT
  'a11e0000-0000-4000-8000-000000000010',
  id,
  'source-author'
FROM contributors
WHERE name = 'Aesop';

INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent)
VALUES (
  'a11e0000-0000-4000-8000-000000000001',
  'a11e0000-0000-4000-8000-000000000010',
  'a11e0000-0000-4000-8000-000000000011',
  '{"type":"heading","h":1,"index":0}',
  0.42
);

INSERT INTO generation_jobs (
  id, status, story_id, story_version_id, child_profile_id, prompt_profile_id,
  theme, request_payload, response_payload, model, prompt_version
)
VALUES (
  'a11e0000-0000-4000-8000-000000000013',
  'queued',
  'a11e0000-0000-4000-8000-000000000010',
  'a11e0000-0000-4000-8000-000000000011',
  'a11e0000-0000-4000-8000-000000000002',
  'a11e0000-0000-4000-8000-000000000003',
  'preserved unrelated job',
  '{"unrelated":true}',
  '{}',
  'preserved-model',
  'preserved-v1'
);
SQL

run_goose up-to 13 >"$test_root/cleanup-goose.out" 2>"$test_root/cleanup-goose.err"
assert_query '0|0|0|0|0|0|0|0|0|0|0' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_sections AS section JOIN story_versions AS version ON version.id = section.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM child_profiles WHERE name = 'Ted' AND age_months = 34),
    (SELECT count(*) FROM prompt_profiles WHERE name = 'Bedtime calm v1' AND schema_version = 1),
    (SELECT count(*) FROM generation_jobs WHERE model = 'seed-model' AND prompt_version = 'v1'),
    (SELECT count(*) FROM works WHERE canonical_title IN ('The Little Star and the Moon','Frankenstein; or, The Modern Prometheus')),
    (SELECT count(*) FROM contributors WHERE name = 'Mary Shelley'),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug IN ('the-fox-and-the-grapes','frankenstein-excerpt-ch1'));
" 'cleanup fixture removal'
printf 'ok 4 - forward cleanup removes every positively identified fixture and dependent row\n'

assert_query '1|1|1|1|1|1|1|1|1|1|1|1|1|1' "
  SELECT
    (SELECT count(*) FROM accounts),
    (SELECT count(*) FROM profiles WHERE name = 'Default'),
    (SELECT count(*) FROM profiles WHERE id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM child_profiles WHERE id = 'a11e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'a11e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001' AND active_child_profile_id = 'a11e0000-0000-4000-8000-000000000002' AND active_prompt_profile_id = 'a11e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'a11e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE id = 'a11e0000-0000-4000-8000-000000000012'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM generation_jobs WHERE id = 'a11e0000-0000-4000-8000-000000000013'),
    (SELECT count(*) FROM works WHERE canonical_title = 'The Fox and the Grapes'),
    (SELECT count(*) FROM contributors WHERE name = 'Aesop'),
    (SELECT count(*) FROM story_contributors WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'upgrade preservation'
printf 'ok 5 - shared account/catalogue rows and unrelated stories, settings, progress, and jobs are preserved\n'

run_goose down-to 12 >"$test_root/cleanup-down.out" 2>"$test_root/cleanup-down.err"
run_goose up-to 13 >"$test_root/cleanup-rerun.out" 2>"$test_root/cleanup-rerun.err"
assert_query '0|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'cleanup rerun'
printf 'ok 6 - non-restoring rollback and cleanup rerun are safe and idempotent\n'

expect_seed_failure missing-ack \
  env -u PP_ALLOW_TEST_SEED \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script"
expect_seed_failure empty-database \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE= \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script"
expect_seed_failure unexpected-database \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE=postgres \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script"
expect_seed_failure remote-docker \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    DOCKER_HOST=tcp://example.invalid:2375 \
    "$seed_script"
expect_seed_failure missing-container \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER=missing-fixture-integration-container \
    "$seed_script"
expect_seed_failure malformed-invocation \
  env PP_ALLOW_TEST_SEED=1 \
    PP_TEST_SEED_DATABASE="$database" \
    PP_TEST_SEED_CONTAINER="$postgres_container" \
    "$seed_script" --unknown
printf 'ok 7 - seed command fails closed for acknowledgement, target, Docker, service, and invocation errors\n'

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >"$test_root/seed.out"
grep -q '^test_seed=installed progress=absent target=local_or_disposable$' "$test_root/seed.out"
assert_query '1|1|1|1|1|1|2|5|1|0' "
  SELECT
    (SELECT count(*) FROM child_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM works WHERE id = 'f17e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM contributors WHERE id = 'f17e0000-0000-4000-8000-000000000004'),
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010' AND is_published),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_sections WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM generation_jobs WHERE id = 'f17e0000-0000-4000-8000-000000000040'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010');
" 'explicit seed inventory'
printf 'ok 8 - explicit seed installs deterministic published UTF-8 chapter/segment fixtures without progress\n'

api_environment="$test_root/api.env"
{
  printf 'DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
    "$database_user" "$database_password" "$postgres_container" "$database"
  printf 'PGAPPNAME=pandapages-api\n'
  printf 'PP_PASSCODE=123456\n'
  printf 'PP_ADMIN_KEY=generated-admin-key-not-for-production\n'
  printf 'PP_SESSION_SECRET=generated-session-secret-not-for-production-00000000\n'
} >"$api_environment"
chmod 0600 "$api_environment"

docker run --detach \
  --name "$api_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=fixture-migration-integration \
  --env-file "$api_environment" \
  --publish 127.0.0.1:0:8080 \
  "$api_image" >/dev/null
api_created=true

api_status=$(docker inspect --format '{{.State.Status}}' "$api_container")
[[ "$api_status" == running ]] || {
  printf 'Fixture API exited during startup\n' >&2
  docker logs "$api_container" 2>&1 \
    | sed "s/$database_password/[redacted]/g" >&2
  exit 1
}

api_address=$(docker port "$api_container" 8080/tcp)
api_port=${api_address##*:}
[[ "$api_port" =~ ^[0-9]+$ ]]
api_base="http://127.0.0.1:$api_port"

api_ready=false
for ((attempt = 0; attempt < 60; attempt++)); do
  if curl --fail --silent --show-error "$api_base/healthz" >/dev/null 2>&1; then
    api_ready=true
    break
  fi
  sleep 1
done
[[ "$api_ready" == true ]] || {
  printf 'Fixture API did not become healthy\n' >&2
  exit 1
}

cookie_jar="$test_root/api.cookies"
curl --fail --silent --show-error \
  --cookie-jar "$cookie_jar" \
  --header 'Content-Type: application/json' \
  --data '{"passcode":"123456"}' \
  "$api_base/api/v1/auth/unlock" >/dev/null
curl --fail --silent --show-error \
  --cookie "$cookie_jar" \
  "$api_base/api/v1/story/test-only-moonlit-cafe" \
  >"$test_root/story.json"
curl --fail --silent --show-error \
  --cookie "$cookie_jar" \
  "$api_base/api/v1/story/test-only-moonlit-cafe/segments" \
  >"$test_root/segments.json"
grep -q 'Moonlit Café' "$test_root/story.json"
grep -q 'Pöndá' "$test_root/story.json"
grep -q '世界' "$test_root/segments.json"
grep -q '星の光' "$test_root/segments.json"
printf 'ok 9 - the current signed-session API reads the seeded story, chapters, segments, and UTF-8 content\n'

docker rm --force "$api_container" >/dev/null
api_created=false

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >/dev/null
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --with-progress >/dev/null
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --with-progress >/dev/null
assert_query '1|1|5|0.6' "
  SELECT
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT percent FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010');
" 'seed idempotency and opt-in progress'
printf 'ok 10 - repeated seed and progress requests are idempotent, with progress opt-in only\n'

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --remove >/dev/null
assert_query '0|0|0|0|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM child_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'explicit fixture removal preservation'
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >/dev/null
env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --remove >/dev/null
printf 'ok 11 - fixture removal and recreation touch only fixed test IDs and preserve unrelated data\n'

cleanup
trap - EXIT HUP INT TERM
[[ -z $(docker ps -aq --filter label=com.pandapages.disposable=fixture-migration-integration) ]]
[[ -z $(docker network ls -q --filter label=com.pandapages.disposable=fixture-migration-integration) ]]
[[ -z $(docker volume ls -q --filter label=com.pandapages.disposable=fixture-migration-integration) ]]
[[ ! -e "$test_root" ]]
printf 'ok 12 - disposable containers, network, volume, credentials, and artifacts are removed\n'
printf 'postgresql_fixtures_integration=passed\n'
