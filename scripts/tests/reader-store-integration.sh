#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repo_root
readonly database_name='pandapages_reader_store_test'
readonly database_user='postgres'
readonly database_password='generated-reader-store-password-not-for-production'
readonly postgres_image=${PP_READER_STORE_TEST_POSTGRES_IMAGE:-postgres:18.1-alpine}
readonly migration_image=${PP_READER_STORE_TEST_MIGRATION_IMAGE:-pandapages-migrate:reader-test}
readonly resource_prefix="pandapages-reader-store-test-$$"
readonly container_name="$resource_prefix-postgres"
readonly network_name="$resource_prefix-network"

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

container_created=false
network_created=false
cleanup() {
  set +e
  if $container_created; then
    docker rm --force "$container_name" >/dev/null 2>&1
  fi
  if $network_created; then
    docker network rm "$network_name" >/dev/null 2>&1
  fi
}
trap cleanup EXIT HUP INT TERM
trap 'printf "Reader Store integration failed at line %d\n" "$LINENO" >&2' ERR

docker network create \
  --label com.pandapages.disposable=reader-store-integration \
  "$network_name" >/dev/null
network_created=true

docker run --detach \
  --name "$container_name" \
  --network "$network_name" \
  --label com.pandapages.disposable=reader-store-integration \
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

psql_query() {
  local statement=$1
  docker exec "$container_name" \
    psql -X --username="$database_user" --dbname="$database_name" \
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
    --network "$network_name" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --env GOOSE_DRIVER=postgres \
    --env "GOOSE_DBSTRING=postgres://$database_user:$database_password@$container_name:5432/$database_name?sslmode=disable" \
    --env GOOSE_MIGRATION_DIR=/migrations \
    --mount "type=bind,src=$repo_root/apps/api/migrations,dst=/migrations,readonly" \
    "$migration_image" "$@"
}

expect_locator_rejected() {
  local name=$1
  local locator=$2
  local output
  if output=$(
    docker exec -i "$container_name" \
      psql -X --username="$database_user" --dbname="$database_name" \
        --set=ON_ERROR_STOP=1 --set="locator=$locator" --file=- 2>&1 <<'SQL'
UPDATE reading_progress AS progress
SET locator = :'locator'::jsonb
FROM stories AS story
WHERE progress.story_id = story.id
  AND story.slug = 'reader-2-migration-vector';
SQL
  ); then
    printf 'database locator constraint accepted invalid case: %s\n' "$name" >&2
    exit 1
  fi
  [[ "$output" == *reading_progress_reader_locator_v2_check* ]] || {
    printf 'invalid locator case failed outside the v2 constraint: %s\n' "$name" >&2
    exit 1
  }
}

run_goose up-to 13 >/dev/null

docker exec -i "$container_name" \
  psql -X --username="$database_user" --dbname="$database_name" \
    --set=ON_ERROR_STOP=1 --file=- >/dev/null <<'SQL'
BEGIN;

WITH target_account AS (
  SELECT id FROM accounts ORDER BY created_at, id LIMIT 1
)
INSERT INTO stories (id, account_id, slug, title, author, language, is_published)
SELECT
  'e2140000-0000-4000-8000-000000000001',
  id,
  'reader-2-migration-vector',
  'Reader 2 migration vector',
  'Test only',
  'en-GB',
  true
FROM target_account;

INSERT INTO story_versions (id, story_id, version, markdown, rendered_html, content_hash)
VALUES (
  'e2140000-0000-4000-8000-000000000002',
  'e2140000-0000-4000-8000-000000000001',
  1,
  'migration vector',
  '<p>migration vector</p>',
  encode(digest('migration vector', 'sha256'), 'hex')
);

INSERT INTO story_segments (story_version_id, ordinal, locator, markdown, rendered_html, word_count)
VALUES
  ('e2140000-0000-4000-8000-000000000002', 1, '{"type":"heading","h":1,"index":0}', '# Café Panda 🐼', '<h1>Café Panda 🐼</h1>', 3),
  ('e2140000-0000-4000-8000-000000000002', 2, '{"type":"para","n":1}', E'Line one\r\nLine two', '<p>Line one<br>Line two</p>', 4),
  ('e2140000-0000-4000-8000-000000000002', 3, '{"type":"para","n":2}', E'Line one\nLine two', '<p>Line one<br>Line two</p>', 4),
  ('e2140000-0000-4000-8000-000000000002', 4, '{"type":"heading","h":2,"index":1}', 'Same words', '<h2>Same words</h2>', 2),
  ('e2140000-0000-4000-8000-000000000002', 5, '{"type":"para","n":3}', 'Same words', '<p>Same words</p>', 2),
  ('e2140000-0000-4000-8000-000000000002', 6, '{"type":"heading","h":2,"index":2}', 'Same words', '<h2>Same words</h2>', 2),
  ('e2140000-0000-4000-8000-000000000002', 7, '{"type":"heading","h":3,"index":3}', 'Same words', '<h3>Same words</h3>', 2);

UPDATE stories
SET draft_version_id = 'e2140000-0000-4000-8000-000000000002',
    published_version_id = 'e2140000-0000-4000-8000-000000000002'
WHERE id = 'e2140000-0000-4000-8000-000000000001';

INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent)
SELECT
  profile.id,
  story.id,
  'e2140000-0000-4000-8000-000000000002',
  '{"mode":"scroll","scrollY":420}',
  0.42
FROM stories AS story
JOIN LATERAL (
  SELECT id FROM profiles WHERE account_id = story.account_id ORDER BY created_at, id LIMIT 1
) AS profile ON true
WHERE story.id = 'e2140000-0000-4000-8000-000000000001';

COMMIT;
SQL

assert_query '1' 'SELECT count(*) FROM reading_progress;' 'pre-migration Reader 1 progress'
run_goose up-to 14 >/dev/null

assert_query '0|0|7' "
  SELECT
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'story_segments' AND column_name = 'locator'),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002');
" 'Reader 2 schema and progress reset'

assert_query '3356355f7cdbea17f247fcd38f581fe42cea6d5b3f7965bd9122a6645cd68b71|015af1d8c2b2f0983b6c8cbc952f42dca6e0a2f379dfdb306fd6f34179f50f29|1|015af1d8c2b2f0983b6c8cbc952f42dca6e0a2f379dfdb306fd6f34179f50f29|2|85ec640a768ec54ad57c68d8e9e561a278f426c8f3c7ca7c71cafc9acb489787|5e92e2acdcf286b6d82be228f32aa1743e3a2912d4b5ee6d268a2d460d104942|2|2|3834b9971e0eb29227bb98f3dc156deb06e27c9b40f358b05c52f4df4e3449e8|2' "
  SELECT
    (SELECT content_key FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 1),
    (SELECT content_key FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 2),
    (SELECT content_occurrence FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 2),
    (SELECT content_key FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 3),
    (SELECT content_occurrence FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 3),
    (SELECT content_key FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 4),
    (SELECT content_key FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 5),
    (SELECT content_occurrence FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 6),
    (SELECT chapter_occurrence FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 6),
    (SELECT content_key FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 7),
    (SELECT chapter_occurrence FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal = 7);
" 'SQL and Go canonical key vectors and chapter occurrences'

assert_query '3|4' "
  SELECT
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal < 4 AND chapter_key IS NULL AND chapter_occurrence IS NULL),
    (SELECT count(*) FROM story_segments WHERE story_version_id = 'e2140000-0000-4000-8000-000000000002' AND ordinal >= 4 AND chapter_key = '85ec640a768ec54ad57c68d8e9e561a278f426c8f3c7ca7c71cafc9acb489787' AND chapter_occurrence IN (1, 2));
" 'H2 chapter propagation'

psql_query "
  INSERT INTO reading_progress (profile_id, story_id, story_version_id, locator, percent)
  SELECT
    profile.id,
    story.id,
    version.id,
    '{\"schema\":2,\"segment\":{\"key\":\"5e92e2acdcf286b6d82be228f32aa1743e3a2912d4b5ee6d268a2d460d104942\",\"occurrence\":1,\"ordinal\":5,\"offset\":0.35},\"chapter\":{\"key\":\"85ec640a768ec54ad57c68d8e9e561a278f426c8f3c7ca7c71cafc9acb489787\",\"occurrence\":1}}',
    0.5
  FROM stories AS story
  JOIN story_versions AS version ON version.story_id = story.id AND version.version = 1
  JOIN LATERAL (
    SELECT id FROM profiles WHERE account_id = story.account_id ORDER BY created_at, id LIMIT 1
  ) AS profile ON true
  WHERE story.slug = 'reader-2-migration-vector';
" >/dev/null

expect_locator_rejected non-object '[]'
expect_locator_rejected missing-schema '{"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0}}'
expect_locator_rejected missing-segment '{"schema":2}'
expect_locator_rejected wrong-schema '{"schema":1,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0}}'
expect_locator_rejected reader-one '{"mode":"scroll","scrollY":20}'
expect_locator_rejected invalid-key '{"schema":2,"segment":{"key":"BAD","occurrence":1,"ordinal":1,"offset":0}}'
expect_locator_rejected occurrence-string '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":"1","ordinal":1,"offset":0}}'
expect_locator_rejected occurrence-zero '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":0,"ordinal":1,"offset":0}}'
expect_locator_rejected ordinal-string '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":"1","offset":0}}'
expect_locator_rejected ordinal-zero '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":0,"offset":0}}'
expect_locator_rejected offset-string '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":"0.5"}}'
expect_locator_rejected offset-low '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":-0.1}}'
expect_locator_rejected offset-high '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":1.1}}'
expect_locator_rejected partial-chapter '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0},"chapter":{"key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}'
expect_locator_rejected invalid-chapter-key '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0},"chapter":{"key":"BAD","occurrence":1}}'
expect_locator_rejected chapter-occurrence-zero '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0},"chapter":{"key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","occurrence":0}}'
expect_locator_rejected unknown-field '{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0},"extra":true}'

psql_query 'DELETE FROM reading_progress;' >/dev/null
run_goose down-to 13 >/dev/null
assert_query '1|0|0' "
  SELECT
    (SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'story_segments' AND column_name = 'locator'),
    (SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'story_segments' AND column_name = 'content_key'),
    (SELECT count(*) FROM reading_progress);
" 'Reader 2 Down limitation'
run_goose up-to 14 >/dev/null
assert_query '0|1|0' "
  SELECT
    (SELECT count(*) FROM reading_progress),
    (SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'story_segments' AND column_name = 'content_key'),
    (SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'story_segments' AND column_name = 'locator');
" 'Reader 2 Down and Up rerun'

run_goose up-to 15 >/dev/null

published_address=$(docker port "$container_name" 5432/tcp)
published_port=${published_address##*:}
[[ "$published_port" =~ ^[0-9]+$ ]] || {
  printf 'Docker returned an invalid disposable PostgreSQL port\n' >&2
  exit 1
}

database_url="postgres://$database_user:$database_password@127.0.0.1:$published_port/$database_name?sslmode=disable"
(
  cd "$repo_root/apps/api"
  PP_READER_STORE_TEST_DISPOSABLE=1 \
    PP_READER_STORE_TEST_DATABASE_URL="$database_url" \
    go test ./internal/db -run '^TestReaderStoreIntegration$' -count=1
)

cleanup
container_created=false
network_created=false
trap - EXIT HUP INT TERM
[[ -z $(docker ps -aq --filter label=com.pandapages.disposable=reader-store-integration) ]]
[[ -z $(docker network ls -q --filter label=com.pandapages.disposable=reader-store-integration) ]]
printf 'reader_store_integration=passed\n'
