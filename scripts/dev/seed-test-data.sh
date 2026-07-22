#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: PP_ALLOW_TEST_SEED=1 PP_TEST_SEED_DATABASE=pandapages \
  scripts/dev/seed-test-data.sh [--with-progress | --remove]

Install deterministic, clearly labelled test fixtures into the running local
development PostgreSQL service or an explicitly labelled disposable test
container. Normal migrations never invoke this command.

Options:
  --with-progress  Install the reader fixture and one Default-profile progress row.
  --remove         Remove only the deterministic explicit fixture IDs.
  --help           Show this help text.

Required environment:
  PP_ALLOW_TEST_SEED=1
  PP_TEST_SEED_DATABASE=pandapages

Optional test-only environment:
  PP_TEST_SEED_CONTAINER  Explicit container name. It must be running and carry
                          an approved Panda Pages test-seed target label.
USAGE
}

mode=seed
include_progress=false

while (($# > 0)); do
  case "$1" in
    --with-progress)
      include_progress=true
      ;;
    --remove)
      mode=remove
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ "$mode" == remove && "$include_progress" == true ]]; then
  printf '%s\n' '--with-progress cannot be combined with --remove' >&2
  exit 2
fi

[[ ${PP_ALLOW_TEST_SEED:-} == 1 ]] || {
  printf 'Refusing test seed without PP_ALLOW_TEST_SEED=1\n' >&2
  exit 1
}

database=${PP_TEST_SEED_DATABASE:-}
[[ -n "$database" ]] || {
  printf 'PP_TEST_SEED_DATABASE must be set explicitly\n' >&2
  exit 1
}
[[ "$database" == pandapages ]] || {
  printf 'Refusing unexpected test-seed database name\n' >&2
  exit 1
}

for command_name in docker; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'DOCKER_HOST must be unset; remote Docker targets are refused\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] || [[ "$docker_endpoint" != unix://* ]]; then
  printf 'A known local Docker Unix-socket context is required\n' >&2
  exit 1
fi

container=${PP_TEST_SEED_CONTAINER:-}
if [[ -z "$container" ]]; then
  mapfile -t candidates < <(
    docker ps \
      --filter label=com.pandapages.test-seed-target=local-development \
      --format '{{.ID}}'
  )
  [[ ${#candidates[@]} -eq 1 ]] || {
    printf 'Exactly one running Panda Pages local-development database is required\n' >&2
    exit 1
  }
  container=${candidates[0]}
fi

[[ "$container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || {
  printf 'Invalid test-seed container identifier\n' >&2
  exit 1
}

container_running=$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null || true)
[[ "$container_running" == true ]] || {
  printf 'The required local/disposable PostgreSQL container is not running\n' >&2
  exit 1
}

target_label=$(docker inspect --format '{{index .Config.Labels "com.pandapages.test-seed-target"}}' "$container")
case "$target_label" in
  local-development|disposable-fixture-integration)
    ;;
  *)
    printf 'Container is not an approved Panda Pages test-seed target\n' >&2
    exit 1
    ;;
esac

if ! database_state=$(
  docker exec "$container" \
    psql -X --username=pandapages --dbname="$database" \
      --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="
        SELECT current_database()
          || '|' || (to_regclass('public.stories') IS NOT NULL)::text
          || '|' || COALESCE((
            SELECT is_applied::text
            FROM goose_db_version
            WHERE version_id = 15
            ORDER BY id DESC
            LIMIT 1
          ), 'false');
      " 2>/dev/null
); then
  printf 'Unable to verify the local/disposable test-seed database\n' >&2
  exit 1
fi
[[ "$database_state" == 'pandapages|true|true' ]] || {
  printf 'Target must be the fully migrated local/disposable pandapages database\n' >&2
  exit 1
}

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repo_root

if [[ "$mode" == remove ]]; then
  sql_file="$repo_root/scripts/dev/remove-test-data.sql"
else
  sql_file="$repo_root/scripts/dev/test-data.sql"
fi

docker exec -i "$container" \
  psql -X --username=pandapages --dbname="$database" \
    --set=ON_ERROR_STOP=1 --quiet --file=- <"$sql_file"

if [[ "$mode" == seed && "$include_progress" == true ]]; then
  docker exec -i "$container" \
    psql -X --username=pandapages --dbname="$database" \
      --set=ON_ERROR_STOP=1 --quiet --file=- \
      <"$repo_root/scripts/dev/test-progress.sql"
fi

if [[ "$mode" == remove ]]; then
  printf 'test_seed=removed target=local_or_disposable\n'
elif [[ "$include_progress" == true ]]; then
  printf 'test_seed=installed progress=installed target=local_or_disposable\n'
else
  printf 'test_seed=installed progress=absent target=local_or_disposable\n'
fi
