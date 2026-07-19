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
resource_suffix=${PP_FIXTURE_TEST_RESOURCE_SUFFIX:-$$}
[[ "$resource_suffix" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$ ]] || {
  printf 'Fixture integration resource suffix is invalid\n' >&2
  exit 1
}
readonly resource_prefix="pandapages-fixture-integration-$resource_suffix"
readonly postgres_container="$resource_prefix-postgres"
readonly api_container="$resource_prefix-api"
readonly network="$resource_prefix-network"
readonly volume="$resource_prefix-data"
readonly run_label="com.pandapages.fixture-run=$resource_prefix"
controlled_goose_failure_status=${PP_FIXTURE_TEST_CONTROLLED_GOOSE_FAILURE_STATUS:-}

if [[ -n "$controlled_goose_failure_status" ]] &&
  [[ ! "$controlled_goose_failure_status" =~ ^([1-9]|[1-9][0-9]|1[01][0-9]|12[0-5])$ ]]; then
  printf 'Controlled Goose failure status must be between 1 and 125\n' >&2
  exit 1
fi

for command_name in curl docker grep mktemp sed tail; do
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
goose_container=''
goose_run_count=0
failure_line=''
failure_signal=''

cleanup() {
  local cleanup_failed=0

  if [[ -n "$goose_container" ]]; then
    if docker container inspect "$goose_container" >/dev/null 2>&1; then
      if ! docker rm --force "$goose_container" >/dev/null 2>&1; then
        printf 'Failed to remove disposable fixture migration container\n' >&2
        cleanup_failed=1
      fi
    fi
    goose_container=''
  fi
  if docker container inspect "$api_container" >/dev/null 2>&1; then
    if ! docker rm --force "$api_container" >/dev/null 2>&1; then
      printf 'Failed to remove disposable fixture API container\n' >&2
      cleanup_failed=1
    fi
  fi
  if docker container inspect "$postgres_container" >/dev/null 2>&1; then
    if ! docker rm --force "$postgres_container" >/dev/null 2>&1; then
      printf 'Failed to remove disposable fixture PostgreSQL container\n' >&2
      cleanup_failed=1
    fi
  fi
  if docker network inspect "$network" >/dev/null 2>&1; then
    if ! docker network rm "$network" >/dev/null 2>&1; then
      printf 'Failed to remove disposable fixture network\n' >&2
      cleanup_failed=1
    fi
  fi
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    if ! docker volume rm "$volume" >/dev/null 2>&1; then
      printf 'Failed to remove disposable fixture volume\n' >&2
      cleanup_failed=1
    fi
  fi
  if [[ -e "$test_root" ]] && ! rm -rf -- "$test_root"; then
    printf 'Failed to remove disposable fixture test artifacts\n' >&2
    cleanup_failed=1
  fi
  return "$cleanup_failed"
}

redact_diagnostics() {
  sed -E \
    -e "s/$database_password/[redacted]/g" \
    -e 's#postgres://[^[:space:]]+#[redacted-database-url]#g'
}

print_failure_diagnostics() {
  local status=$1
  local log_file

  if [[ -n "$failure_signal" ]]; then
    printf 'Fixture integration interrupted by %s (status %d)\n' "$failure_signal" "$status" >&2
  elif [[ -n "$failure_line" ]]; then
    printf 'Fixture integration failed at line %s (status %d)\n' "$failure_line" "$status" >&2
  else
    printf 'Fixture integration failed (status %d)\n' "$status" >&2
  fi

  for log_file in "$test_root"/*-goose.err "$test_root"/*-goose.out; do
    [[ -s "$log_file" ]] || continue
    printf '%s\n' "--- ${log_file##*/} (last 80 lines, redacted) ---" >&2
    tail -n 80 "$log_file" | redact_diagnostics >&2
  done

  if docker container inspect "$api_container" >/dev/null 2>&1; then
    printf '%s\n' '--- fixture API logs (last 80 lines, redacted) ---' >&2
    docker logs --tail 80 "$api_container" 2>&1 | redact_diagnostics >&2 || true
  fi
  if docker container inspect "$postgres_container" >/dev/null 2>&1; then
    printf '%s\n' '--- fixture PostgreSQL logs (last 80 lines, redacted) ---' >&2
    docker logs --tail 80 "$postgres_container" 2>&1 | redact_diagnostics >&2 || true
  fi
}

record_failure() {
  failure_line=$1
}

on_exit() {
  local status=$?
  local cleanup_status=0

  trap - EXIT ERR HUP INT TERM
  if ((status != 0)); then
    if ! print_failure_diagnostics "$status"; then
      printf 'Fixture integration diagnostics were incomplete\n' >&2
    fi
  fi
  cleanup || cleanup_status=$?
  if ((cleanup_status != 0)); then
    printf 'Fixture integration cleanup was incomplete\n' >&2
    if ((status == 0)); then
      status=$cleanup_status
    fi
  fi
  exit "$status"
}

on_signal() {
  failure_signal=$1
  exit "$2"
}

trap on_exit EXIT
trap 'record_failure "$LINENO"' ERR
trap 'on_signal HUP 129' HUP
trap 'on_signal INT 130' INT
trap 'on_signal TERM 143' TERM

docker network create \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
  "$network" >/dev/null

docker volume create \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
  "$volume" >/dev/null

docker run --detach \
  --name "$postgres_container" \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
  --tmpfs /var/run/postgresql:rw,nosuid,nodev,noexec,size=16m \
  --label com.pandapages.disposable=fixture-migration-integration \
  --label "$run_label" \
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
  local status

  if [[ -n "$controlled_goose_failure_status" ]]; then
    printf 'Controlled fixture migration failure for harness regression (status %s)\n' \
      "$controlled_goose_failure_status" >&2
    return "$controlled_goose_failure_status"
  fi

  ((goose_run_count += 1))
  goose_container="$resource_prefix-goose-$goose_run_count"
  if docker run --rm \
    --name "$goose_container" \
    --network "$network" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --label com.pandapages.disposable=fixture-migration-integration \
    --label "$run_label" \
    --env GOOSE_DRIVER=postgres \
    --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$postgres_container:5432/$database?sslmode=disable" \
    --env GOOSE_MIGRATION_DIR=/migrations \
    --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
    "$migration_image" "$@"; then
    goose_container=''
    return 0
  else
    status=$?
    return "$status"
  fi
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

printf '1..18\n'

run_goose up >"$test_root/fresh-goose.out" 2>"$test_root/fresh-goose.err"
grep -q 'OK.*00014_reader_2_contract.sql' \
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

run_goose up-to 13 >"$test_root/exact-cleanup-goose.out" 2>"$test_root/exact-cleanup-goose.err"
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
    (SELECT count(*) FROM works WHERE canonical_title IN ('The Fox and the Grapes','The Little Star and the Moon','Frankenstein; or, The Modern Prometheus')),
    (SELECT count(*) FROM contributors WHERE name IN ('Aesop','Mary Shelley')),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug IN ('the-fox-and-the-grapes','frankenstein-excerpt-ch1'));
" 'exact historical fixture cleanup'
printf 'ok 4 - an unchanged historical lifecycle is removed with versions, sections, segments, links, progress, and fixture profiles/jobs\n'

reset_database
run_goose up-to 12 >"$test_root/preservation-goose.out" 2>"$test_root/preservation-goose.err"

docker exec -i "$postgres_container" \
  psql -X --username="$database_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --file=- >/dev/null <<'SQL'
BEGIN;

-- Content-only edit: retain every story-level field but replace the v1 body
-- and hash. The cleanup must preserve the entire story.
UPDATE story_versions AS version
SET markdown = E'# The Fox and the Grapes\n\nThis is legitimate replacement content.\n',
    rendered_html = '<h1>The Fox and the Grapes</h1><p>This is legitimate replacement content.</p>',
    content_hash = encode(digest(
      E'# The Fox and the Grapes\n\nThis is legitimate replacement content.\n',
      'sha256'
    ), 'hex')
FROM stories AS story
WHERE version.story_id = story.id
  AND version.version = 1
  AND story.slug = 'the-fox-and-the-grapes';

-- Additional draft version: model AdminDraftUpsert's new-version, generic
-- section, segment, and draft-pointer behaviour while leaving v1 published.
INSERT INTO story_versions (
  id, story_id, version, frontmatter, markdown, rendered_html, content_hash
)
SELECT
  'e13e0000-0000-4000-8000-000000000101',
  story.id,
  2,
  '{"title":"The Little Star and the Moon","author":"Panda Pages (seed)","language":"en-GB"}',
  E'# A legitimate new Little Star draft\n\nThe moon now follows a different path.\n',
  '<h1>A legitimate new Little Star draft</h1><p>The moon now follows a different path.</p>',
  encode(digest(
    E'# A legitimate new Little Star draft\n\nThe moon now follows a different path.\n',
    'sha256'
  ), 'hex')
FROM stories AS story
WHERE story.slug = 'the-little-star-and-the-moon';

INSERT INTO story_sections (id, story_version_id, kind, title, ordinal)
VALUES (
  'e13e0000-0000-4000-8000-000000000102',
  'e13e0000-0000-4000-8000-000000000101',
  'section',
  NULL,
  1
);

INSERT INTO story_segments (
  id, story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count
)
VALUES
  (
    'e13e0000-0000-4000-8000-000000000103',
    'e13e0000-0000-4000-8000-000000000101',
    'e13e0000-0000-4000-8000-000000000102',
    1,
    '{"type":"heading","h":1,"index":0}',
    '# A legitimate new Little Star draft',
    '<h1>A legitimate new Little Star draft</h1>',
    6
  ),
  (
    'e13e0000-0000-4000-8000-000000000104',
    'e13e0000-0000-4000-8000-000000000101',
    'e13e0000-0000-4000-8000-000000000102',
    2,
    '{"type":"para","n":1}',
    'The moon now follows a different path.',
    '<p>The moon now follows a different path.</p>',
    7
  );

UPDATE stories
SET draft_version_id = 'e13e0000-0000-4000-8000-000000000101'
WHERE slug = 'the-little-star-and-the-moon';

INSERT INTO reading_progress (
  profile_id, story_id, story_version_id, locator, percent
)
SELECT
  profile.id,
  story.id,
  'e13e0000-0000-4000-8000-000000000101',
  '{"type":"para","n":1}',
  0.61
FROM stories AS story
JOIN LATERAL (
  SELECT id
  FROM profiles
  WHERE account_id = story.account_id
    AND name = 'Default'
  ORDER BY created_at ASC, id ASC
  LIMIT 1
) AS profile ON true
WHERE story.slug = 'the-little-star-and-the-moon';

-- Additional published version: model a new draft followed by AdminPublish,
-- which leaves both pointers on v2 and keeps the story published.
INSERT INTO story_versions (
  id, story_id, version, frontmatter, markdown, rendered_html, content_hash
)
SELECT
  'e13e0000-0000-4000-8000-000000000201',
  story.id,
  2,
  '{"title":"Frankenstein (Excerpt — Chapter 1)","author":"Mary Shelley","language":"en-GB"}',
  E'# A legitimately republished excerpt\n\nThis replacement is intentionally short.\n',
  '<h1>A legitimately republished excerpt</h1><p>This replacement is intentionally short.</p>',
  encode(digest(
    E'# A legitimately republished excerpt\n\nThis replacement is intentionally short.\n',
    'sha256'
  ), 'hex')
FROM stories AS story
WHERE story.slug = 'frankenstein-excerpt-ch1';

INSERT INTO story_sections (id, story_version_id, kind, title, ordinal)
VALUES (
  'e13e0000-0000-4000-8000-000000000202',
  'e13e0000-0000-4000-8000-000000000201',
  'section',
  NULL,
  1
);

INSERT INTO story_segments (
  id, story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count
)
VALUES
  (
    'e13e0000-0000-4000-8000-000000000203',
    'e13e0000-0000-4000-8000-000000000201',
    'e13e0000-0000-4000-8000-000000000202',
    1,
    '{"type":"heading","h":1,"index":0}',
    '# A legitimately republished excerpt',
    '<h1>A legitimately republished excerpt</h1>',
    4
  ),
  (
    'e13e0000-0000-4000-8000-000000000204',
    'e13e0000-0000-4000-8000-000000000201',
    'e13e0000-0000-4000-8000-000000000202',
    2,
    '{"type":"para","n":1}',
    'This replacement is intentionally short.',
    '<p>This replacement is intentionally short.</p>',
    5
  );

UPDATE stories
SET draft_version_id = 'e13e0000-0000-4000-8000-000000000201',
    published_version_id = 'e13e0000-0000-4000-8000-000000000201',
    is_published = true
WHERE slug = 'frankenstein-excerpt-ch1';

INSERT INTO reading_progress (
  profile_id, story_id, story_version_id, locator, percent
)
SELECT
  profile.id,
  story.id,
  'e13e0000-0000-4000-8000-000000000201',
  '{"type":"para","n":1}',
  0.72
FROM stories AS story
JOIN LATERAL (
  SELECT id
  FROM profiles
  WHERE account_id = story.account_id
    AND name = 'Default'
  ORDER BY created_at ASC, id ASC
  LIMIT 1
) AS profile ON true
WHERE story.slug = 'frankenstein-excerpt-ch1';

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

COMMIT;
SQL

run_goose up-to 13 >"$test_root/preservation-cleanup-goose.out" 2>"$test_root/preservation-cleanup-goose.err"

assert_query '1|1|3|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug = 'the-fox-and-the-grapes'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-fox-and-the-grapes' AND version.content_hash <> '2fb47f4013a00e9348fa92c2da31ee0688e3a8ef878fa4308646b1656c9adad4');
" 'content-edited fixture preservation'
printf 'ok 5 - content-only edits preserve the story, version, segments, contributor link, and progress\n'

assert_query '1|2|1|5|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug = 'the-little-star-and-the-moon'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-little-star-and-the-moon'),
    (SELECT count(*) FROM story_sections WHERE story_version_id = 'e13e0000-0000-4000-8000-000000000101'),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'the-little-star-and-the-moon'),
    (SELECT count(*) FROM stories WHERE slug = 'the-little-star-and-the-moon' AND draft_version_id = 'e13e0000-0000-4000-8000-000000000101'),
    (SELECT count(*) FROM stories AS story JOIN story_versions AS version ON version.id = story.published_version_id WHERE story.slug = 'the-little-star-and-the-moon' AND version.version = 1),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug = 'the-little-star-and-the-moon' AND progress.story_version_id = 'e13e0000-0000-4000-8000-000000000101' AND round(progress.percent::numeric, 2) = 0.61);
" 'additional draft preservation'
printf 'ok 6 - an additional draft preserves both versions, every segment, both pointers, and progress\n'

assert_query '1|2|2|5|1|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM story_versions AS version JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM story_sections AS section JOIN story_versions AS version ON version.id = section.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM story_segments AS segment JOIN story_versions AS version ON version.id = segment.story_version_id JOIN stories AS story ON story.id = version.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM stories WHERE slug = 'frankenstein-excerpt-ch1' AND draft_version_id = 'e13e0000-0000-4000-8000-000000000201'),
    (SELECT count(*) FROM stories WHERE slug = 'frankenstein-excerpt-ch1' AND published_version_id = 'e13e0000-0000-4000-8000-000000000201' AND is_published),
    (SELECT count(*) FROM story_contributors AS link JOIN stories AS story ON story.id = link.story_id WHERE story.slug = 'frankenstein-excerpt-ch1'),
    (SELECT count(*) FROM reading_progress AS progress JOIN stories AS story ON story.id = progress.story_id WHERE story.slug = 'frankenstein-excerpt-ch1' AND progress.story_version_id = 'e13e0000-0000-4000-8000-000000000201' AND round(progress.percent::numeric, 2) = 0.72);
" 'republished fixture preservation'
printf 'ok 7 - a republished additional version preserves the whole story lifecycle and progress\n'

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
printf 'ok 8 - shared account/catalogue rows and unrelated stories, settings, progress, and jobs are preserved\n'

run_goose down-to 12 >"$test_root/cleanup-down.out" 2>"$test_root/cleanup-down.err"
run_goose up-to 13 >"$test_root/cleanup-rerun.out" 2>"$test_root/cleanup-rerun.err"
assert_query '3|1|1|1' "
  SELECT
    (SELECT count(*) FROM stories WHERE slug IN ('the-fox-and-the-grapes','the-little-star-and-the-moon','frankenstein-excerpt-ch1')),
    (SELECT count(*) FROM stories WHERE id = 'a11e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM profile_settings WHERE profile_id = 'a11e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'a11e0000-0000-4000-8000-000000000010');
" 'cleanup rerun'
printf 'ok 9 - non-restoring rollback and cleanup rerun preserve ambiguous stories idempotently\n'

run_goose up-to 14 >"$test_root/reader2-upgrade.out" 2>"$test_root/reader2-upgrade.err"
assert_query '0|0' "
  SELECT
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM information_schema.columns
       WHERE table_schema = 'public'
         AND table_name = 'story_segments'
         AND column_name = 'locator');
" 'Reader 2 fixture boundary'
printf 'ok 10 - Reader 2 upgrade resets beta progress and removes the obsolete segment locator\n'

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
printf 'ok 11 - seed command fails closed for acknowledgement, target, Docker, service, and invocation errors\n'

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" >"$test_root/seed.out"
grep -q '^test_seed=installed progress=absent target=local_or_disposable$' "$test_root/seed.out"
assert_query '1|1|1|1|1|1|2|6|1|0' "
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
printf 'ok 12 - explicit seed installs deterministic published UTF-8 chapter/segment fixtures without progress\n'

assert_query '6|3|3|2|2|2|6|3|3|t|## Chapter Two — 世界|<h2>Chapter Two — 世界</h2>|星の光 shimmered over the quiet water. 🐼|<p>星の光 shimmered over the quiet water. 🐼</p>|t' "
  SELECT
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'heading'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'paragraph'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND ordinal IN (1, 2) AND chapter_key IS NULL AND chapter_occurrence IS NULL),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND ordinal IN (3, 4) AND chapter_key = '6f744b440fbf4fa52da46bebf4fd3e5f2de7a1c2fb11f7e9ac2794ccd1956c4e' AND chapter_occurrence = 1),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND ordinal IN (5, 6) AND chapter_key = '3749b6630ab08c6998fd65117d5265c7e7514e35f02022a4005505d0aba52a73' AND chapter_occurrence = 1),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND content_occurrence = 1),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'heading' AND heading_level IN (1, 2)),
    (SELECT count(*) FROM story_segments WHERE story_version_id = heading.story_version_id AND segment_kind = 'paragraph' AND heading_level IS NULL),
    (SELECT bool_and(content_key ~ '^[0-9a-f]{64}$') FROM story_segments WHERE story_version_id = heading.story_version_id),
    heading.markdown,
    heading.rendered_html,
    paragraph.markdown,
    paragraph.rendered_html,
    heading.section_id = paragraph.section_id
      AND heading.section_id = 'f17e0000-0000-4000-8000-000000000021'
  FROM story_segments AS heading
  JOIN story_segments AS paragraph
    ON paragraph.story_version_id = heading.story_version_id
   AND paragraph.ordinal = 6
  WHERE heading.story_version_id = 'f17e0000-0000-4000-8000-000000000011'
    AND heading.ordinal = 5;
" 'explicit fixture ingestion segment shape'
printf 'ok 13 - canonical keys, kinds, chapter propagation, and six independent fixture segments match ingestion\n'

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
  --label "$run_label" \
  --env-file "$api_environment" \
  --publish 127.0.0.1:0:8080 \
  "$api_image" >/dev/null

api_status=$(docker inspect --format '{{.State.Status}}' "$api_container")
[[ "$api_status" == running ]] || {
  printf 'Fixture API exited during startup\n' >&2
  docker logs "$api_container" 2>&1 \
    | redact_diagnostics >&2
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
  "$api_base/api/v1/reader/test-only-moonlit-cafe" \
  >"$test_root/reader.json"
grep -q 'Moonlit Café' "$test_root/reader.json"
grep -q 'Pöndá' "$test_root/reader.json"
grep -q '世界' "$test_root/reader.json"
grep -q '星の光' "$test_root/reader.json"
if grep -q 'markdown\|f17e0000-0000-4000-8000-0000000000' "$test_root/reader.json"; then
  printf 'Reader response exposed Markdown or internal IDs\n' >&2
  exit 1
fi
printf 'ok 14 - the signed-session coherent Reader endpoint returns six UTF-8 segments without internal content\n'

docker rm --force "$api_container" >/dev/null

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
assert_query '1|1|6|2|4|0.35|0.6' "
  SELECT
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT locator->>'schema' FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT locator->'segment'->>'ordinal' FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT locator->'segment'->>'offset' FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT percent FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010');
" 'seed idempotency and opt-in progress'
printf 'ok 15 - repeated seed and valid Locator v2 progress requests are idempotent and opt-in only\n'

env \
  PP_ALLOW_TEST_SEED=1 \
  PP_TEST_SEED_DATABASE="$database" \
  PP_TEST_SEED_CONTAINER="$postgres_container" \
  "$seed_script" --remove >/dev/null
assert_query '0|0|0|0|0|0|0|0|0|0|0|1|1|0' "
  SELECT
    (SELECT count(*) FROM stories WHERE id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_versions WHERE id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_sections WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'f17e0000-0000-4000-8000-000000000011'),
    (SELECT count(*) FROM child_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000001'),
    (SELECT count(*) FROM prompt_profiles WHERE id = 'f17e0000-0000-4000-8000-000000000002'),
    (SELECT count(*) FROM works WHERE id = 'f17e0000-0000-4000-8000-000000000003'),
    (SELECT count(*) FROM contributors WHERE id = 'f17e0000-0000-4000-8000-000000000004'),
    (SELECT count(*) FROM generation_jobs WHERE id = 'f17e0000-0000-4000-8000-000000000040'),
    (SELECT count(*) FROM reading_progress WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
    (SELECT count(*) FROM story_contributors WHERE story_id = 'f17e0000-0000-4000-8000-000000000010'),
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
printf 'ok 16 - fixture removal and recreation touch only fixed test IDs and preserve unrelated data\n'

controlled_root="$test_root/controlled-failure"
controlled_stdout="$controlled_root/stdout"
controlled_stderr="$controlled_root/stderr"
controlled_suffix="$$-controlled"
controlled_prefix="pandapages-fixture-integration-$controlled_suffix"
controlled_label="com.pandapages.fixture-run=$controlled_prefix"
mkdir -p "$controlled_root"

PP_FIXTURE_TEST_RESOURCE_SUFFIX="$controlled_suffix" \
PP_FIXTURE_TEST_CONTROLLED_GOOSE_FAILURE_STATUS=37 \
TMPDIR="$controlled_root" \
  "${BASH_SOURCE[0]}" >"$controlled_stdout" 2>"$controlled_stderr" &
controlled_pid=$!
if wait "$controlled_pid"; then
  controlled_status=0
else
  controlled_status=$?
fi

[[ "$controlled_status" == 37 ]] || {
  printf 'Controlled fixture migration failure status = %s, want 37\n' "$controlled_status" >&2
  exit 1
}
grep -q '^1\.\.18$' "$controlled_stdout"
grep -q 'Fixture integration failed at line .* (status 37)' "$controlled_stderr"
grep -q 'fresh-goose.err .*redacted' "$controlled_stderr"
grep -q 'Controlled fixture migration failure for harness regression (status 37)' "$controlled_stderr"
if grep -Fq "$database_password" "$controlled_stdout" "$controlled_stderr" ||
  grep -Eq 'postgres://[^[:space:]]+' "$controlled_stdout" "$controlled_stderr"; then
  printf 'Controlled fixture diagnostics exposed credentials or a database URL\n' >&2
  exit 1
fi
[[ -z $(docker ps -aq --filter "label=$controlled_label") ]]
[[ -z $(docker network ls -q --filter "label=$controlled_label") ]]
[[ -z $(docker volume ls -q --filter "label=$controlled_label") ]]
if compgen -G "$controlled_root/pandapages-fixture-integration.*" >/dev/null; then
  printf 'Controlled fixture migration left temporary artifacts behind\n' >&2
  exit 1
fi
printf 'ok 17 - controlled migration failure preserves status, emits redacted diagnostics, and leaves no resources or artifacts\n'

cleanup
trap - EXIT ERR HUP INT TERM
[[ -z $(docker ps -aq --filter "label=$run_label") ]]
[[ -z $(docker network ls -q --filter "label=$run_label") ]]
[[ -z $(docker volume ls -q --filter "label=$run_label") ]]
[[ ! -e "$test_root" ]]
printf 'ok 18 - disposable containers, network, volume, credentials, and artifacts are removed\n'
printf 'postgresql_fixtures_integration=passed\n'
