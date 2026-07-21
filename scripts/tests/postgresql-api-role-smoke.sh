#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
verifier="$repo_root/scripts/postgresql-api-role-verify.sh"
session_cookie_contract="$repo_root/scripts/lib/postgresql-api-role-session-cookie.awk"
source_container=${PP_ROLE_TEST_SOURCE_CONTAINER:-}
network=${PP_ROLE_TEST_NETWORK:-}
api_image=${PP_ROLE_TEST_API_IMAGE:-}
database=${PP_ROLE_TEST_DATABASE:-}
admin_user=${PP_ROLE_TEST_ADMIN_USER:-}
application_role=${PP_ROLE_TEST_APPLICATION_ROLE:-}
application_password=${PP_ROLE_TEST_APPLICATION_PASSWORD:-}
migration_role=${PP_ROLE_TEST_MIGRATION_ROLE:-}
migration_password=${PP_ROLE_TEST_MIGRATION_PASSWORD:-}
legacy_role=${PP_ROLE_TEST_LEGACY_ROLE:-}
legacy_password=${PP_ROLE_TEST_LEGACY_PASSWORD:-}
readonly expected_application_name=pandapages-api

for pair in \
  "source container:$source_container" \
  "network:$network" \
  "API image:$api_image" \
  "database:$database" \
  "admin user:$admin_user" \
  "application role:$application_role" \
  "application password:$application_password" \
  "migration role:$migration_role" \
  "migration password:$migration_password" \
  "legacy role:$legacy_role" \
  "legacy password:$legacy_password"; do
  [[ -n ${pair#*:} ]] || {
    printf 'Missing generated role-smoke input: %s\n' "${pair%%:*}" >&2
    exit 1
  }
done

for value in "$source_container" "$network" "$database" "$admin_user" "$application_role" "$migration_role" "$legacy_role"; do
  [[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || {
    printf 'Invalid generated role-smoke identifier\n' >&2
    exit 1
  }
done

[[ -x "$verifier" ]] || {
  printf 'API role verifier is unavailable\n' >&2
  exit 1
}
[[ -r "$session_cookie_contract" ]] || {
  printf 'Signed session cookie contract is unavailable\n' >&2
  exit 1
}

[[ -z ${DOCKER_HOST:-} ]] || {
  printf 'Refusing role smoke test while DOCKER_HOST is set\n' >&2
  exit 1
}

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
if [[ "$docker_context" != default && "$docker_context" != desktop-linux ]] || [[ "$docker_endpoint" != unix://* ]]; then
  printf 'Role smoke test requires a known local Docker Unix socket\n' >&2
  exit 1
fi

docker image inspect "$api_image" >/dev/null 2>&1 || {
  printf 'API role-smoke image is unavailable: %s\n' "$api_image" >&2
  exit 1
}

source_disposable_label=$(docker inspect --format '{{index .Config.Labels "com.pandapages.disposable"}}' "$source_container" 2>/dev/null || true)
[[ "$source_disposable_label" == role-integration ]] || {
  printf 'Role smoke test source must be a disposable role-integration container\n' >&2
  exit 1
}

readonly api_container="${source_container}-api"
api_created=false
manual_session_started=false
account_access_revoked=false
test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-api-role-smoke.XXXXXX")

cleanup() {
  set +e
  if $api_created; then
    docker rm --force "$api_container" >/dev/null 2>&1
  fi
  if $manual_session_started; then
    docker exec "$source_container" psql -X --username="$admin_user" --dbname="$database" --set=ON_ERROR_STOP=1 --command="SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = 'manual-psql' AND pid <> pg_backend_pid();" >/dev/null 2>&1
  fi
  if $account_access_revoked; then
    docker exec -i "$source_container" \
      psql -X --username="$admin_user" --dbname="$database" \
      --set=ON_ERROR_STOP=1 --set=application_role="$application_role" \
      --file=- >/dev/null 2>&1 <<'SQL'
GRANT SELECT, INSERT ON TABLE accounts TO :"application_role";
SQL
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT
trap 'printf "Generated API role smoke failed at line %d\n" "$LINENO" >&2' ERR

stop_api() {
  if $api_created; then
    docker rm --force "$api_container" >/dev/null
    api_created=false
  fi
}

write_api_environment() {
  local output=$1
  local database_role=$2
  local database_password=$3
  local workload_name=$4
  local passcode=${5:-123456}

  {
    printf 'DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
      "$database_role" "$database_password" "$source_container" "$database"
    printf 'PGAPPNAME=%s\n' "$workload_name"
    printf 'PP_PASSCODE=%s\n' "$passcode"
    printf 'PP_ADMIN_KEY=generated-admin-key-not-for-production\n'
    printf 'PP_SESSION_SECRET=generated-session-secret-not-for-production\n'
  } >"$output"
  chmod 0600 "$output"
}

start_api() {
  local database_role=$1
  local database_password=$2
  local workload_name=$3
  local passcode=${4:-123456}
  local environment_file="$test_root/api.env"

  write_api_environment "$environment_file" "$database_role" "$database_password" "$workload_name" "$passcode"
  docker run --detach \
    --name "$api_container" \
    --network "$network" \
    --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --label com.pandapages.disposable=role-api-smoke \
    --env-file "$environment_file" \
    "$api_image" >/dev/null
  api_created=true
}

verify_arguments=(
  --api-container "$api_container"
  --postgres-container "$source_container"
  --database "$database"
  --session-contract signed
  --admin-user "$admin_user"
  --application-role "$application_role"
  --application-name "$expected_application_name"
  --legacy-role "$legacy_role"
  --migration-role "$migration_role"
)

assert_probe_cleanup() {
  local leftover
  leftover=$(docker exec "$api_container" sh -c \
    "find /tmp -maxdepth 1 -name 'pandapages-api-role-verify.*' -print -quit")
  [[ -z "$leftover" ]] || {
    printf 'API role verifier left protected probe material behind\n' >&2
    exit 1
  }
}

probe_api_readiness() {
  local response_file="$test_root/readiness-response.json"
  local error_file="$test_root/readiness-response.err"

  for ((attempt = 0; attempt < 20; attempt++)); do
    if docker exec "$api_container" \
      wget -Y off -T 5 -q -O - http://127.0.0.1:8080/readyz \
      >"$response_file" 2>"$error_file" \
      && grep -Eq '"status"[[:space:]]*:[[:space:]]*"ready"' "$response_file"; then
      return 0
    fi
    sleep 1
  done

  printf 'Application-role readiness probe did not report the current Goose schema\n' >&2
  return 1
}

probe_admin_catalogue() {
  local probe_status=0

  # This disposable-only probe creates one generated draft so catalogue
  # inspection must validate both the stored version and its Reader segments.
  # The signed cookie and proxy key stay inside a protected in-container
  # directory and never enter host arguments or output.
  docker exec -i "$api_container" sh -eu -c '
    umask 077
    probe_dir=$(mktemp -d /tmp/pandapages-api-role-verify.catalogue.XXXXXX)
    cleanup() {
      rm -rf -- "$probe_dir"
    }
    trap cleanup EXIT HUP INT TERM
    case "$probe_dir" in
      /tmp/pandapages-api-role-verify.catalogue.*) ;;
      *) exit 20 ;;
    esac

    test -n "${PP_PASSCODE:-}"
    test -n "${PP_ADMIN_KEY:-}"

    session_cookie_rules="$probe_dir/session-cookie.awk"
    unlock_request="$probe_dir/unlock-request.json"
    unlock_headers="$probe_dir/unlock-headers"
    unlock_response="$probe_dir/unlock-response.json"
    cookie_file="$probe_dir/cookies"
    forbidden_request="$probe_dir/forbidden-request.http"
    forbidden_response="$probe_dir/forbidden-response.http"
    draft_body="$probe_dir/draft-body.json"
    draft_request="$probe_dir/draft-request.http"
    draft_response="$probe_dir/draft-response.http"
    draft_response_body="$probe_dir/draft-response.json"
    catalogue_request="$probe_dir/catalogue-request.http"
    catalogue_response="$probe_dir/catalogue-response.http"
    catalogue_response_body="$probe_dir/catalogue-response.json"

    cat >"$session_cookie_rules"
    test -s "$session_cookie_rules"

    awk '\''BEGIN {
      value = ENVIRON["PP_PASSCODE"]
      if (length(value) != 6) exit 1
      printf "{\"passcode\":\""
      for (position = 1; position <= length(value); position++) {
        character = substr(value, position, 1)
        if (character ~ /[[:cntrl:]]/) exit 1
        if (character == "\\" || character == "\"") printf "\\%s", character
        else printf "%s", character
      }
      printf "\"}\n"
    }'\'' >"$unlock_request"

    wget -Y off -T 5 -S -O "$unlock_response" -o "$unlock_headers" \
      --header="Content-Type: application/json" \
      --post-file="$unlock_request" \
      http://127.0.0.1:8080/api/v1/auth/unlock
    grep -Eq '\''"ok"[[:space:]]*:[[:space:]]*true'\'' "$unlock_response"
    awk -v session_contract=signed \
      -f "$session_cookie_rules" "$unlock_headers" >"$cookie_file"

    # The API boundary must still reject a signed session when the trusted
    # proxy-injected administrator header is absent.
    {
      printf "GET /api/v1/admin/stories HTTP/1.1\r\n"
      printf "Host: 127.0.0.1:8080\r\n"
      printf "Connection: close\r\n"
      printf "Cookie: "
      cat "$cookie_file"
      printf "\r\n\r\n"
    } >"$forbidden_request"
    busybox nc -w 5 127.0.0.1 8080 \
      <"$forbidden_request" >"$forbidden_response"
    grep -Eq '\''^HTTP/1\.[01] 403 '\'' "$forbidden_response"

    cat >"$draft_body" <<JSON
{"slug":"generated-role-catalogue","title":"Generated Role Catalogue","author":null,"markdown":"# Generated Role Catalogue\\n\\nA generated panda opens a book.\\n","language":"en","sourceUrl":null,"rights":{"label":"Generated test content"}}
JSON
    draft_length=$(wc -c <"$draft_body")
    {
      printf "POST /api/v1/admin/stories/draft HTTP/1.1\r\n"
      printf "Host: 127.0.0.1:8080\r\n"
      printf "Content-Type: application/json\r\n"
      printf "Content-Length: %s\r\n" "$draft_length"
      printf "X-PP-Admin-Key: %s\r\n" "$PP_ADMIN_KEY"
      printf "Connection: close\r\n"
      printf "Cookie: "
      cat "$cookie_file"
      printf "\r\n\r\n"
      cat "$draft_body"
    } >"$draft_request"
    busybox nc -w 5 127.0.0.1 8080 \
      <"$draft_request" >"$draft_response"
    grep -Eq '\''^HTTP/1\.[01] 200 '\'' "$draft_response"
    awk '\''{
      sub(/\r$/, "")
      if (body) print
      else if ($0 == "") body = 1
    }'\'' "$draft_response" >"$draft_response_body"
    grep -Eq '\''"slug"[[:space:]]*:[[:space:]]*"generated-role-catalogue"'\'' "$draft_response_body"
    grep -Eq '\''"outcome"[[:space:]]*:[[:space:]]*"created_story"'\'' "$draft_response_body"

    {
      printf "GET /api/v1/admin/stories HTTP/1.1\r\n"
      printf "Host: 127.0.0.1:8080\r\n"
      printf "X-PP-Admin-Key: %s\r\n" "$PP_ADMIN_KEY"
      printf "Connection: close\r\n"
      printf "Cookie: "
      cat "$cookie_file"
      printf "\r\n\r\n"
    } >"$catalogue_request"
    busybox nc -w 5 127.0.0.1 8080 \
      <"$catalogue_request" >"$catalogue_response"
    grep -Eq '\''^HTTP/1\.[01] 200 '\'' "$catalogue_response"
    grep -Eiq '\''^Cache-Control:[[:space:]]*no-store\r?$'\'' "$catalogue_response"
    grep -Eiq '\''^Content-Type:[[:space:]]*application/json\r?$'\'' "$catalogue_response"
    awk '\''{
      sub(/\r$/, "")
      if (body) print
      else if ($0 == "") body = 1
    }'\'' "$catalogue_response" >"$catalogue_response_body"

    grep -Eq '\''"items"[[:space:]]*:[[:space:]]*\['\'' "$catalogue_response_body"
    slug_count=$(awk -F '\''"slug"[[:space:]]*:[[:space:]]*"generated-role-catalogue"'\'' \
      '\''{ count += NF - 1 } END { print count + 0 }'\'' "$catalogue_response_body")
    test "$slug_count" -eq 1
    item_count=$(awk -F '\''"slug"[[:space:]]*:'\'' \
      '\''{ count += NF - 1 } END { print count + 0 }'\'' "$catalogue_response_body")
    test "$item_count" -eq 1
    grep -Eq '\''"status"[[:space:]]*:[[:space:]]*"draft_only"'\'' "$catalogue_response_body"
    grep -Eq '\''"publishedVersion"[[:space:]]*:[[:space:]]*null'\'' "$catalogue_response_body"
    grep -Eq '\''"draftVersion"[[:space:]]*:[[:space:]]*\{'\'' "$catalogue_response_body"
    grep -Eq '\''"versionCount"[[:space:]]*:[[:space:]]*1'\'' "$catalogue_response_body"
    if grep -Eq '\''"(markdown|renderedHtml|segments|locator|accountId|storyId|contentHash)"[[:space:]]*:'\'' \
      "$catalogue_response_body"; then
      exit 21
    fi
  ' <"$session_cookie_contract" >/dev/null 2>&1 || probe_status=$?

  if ((probe_status != 0)); then
    printf 'Application-role Story Studio catalogue probe failed\n' >&2
    return 1
  fi
}

assert_no_generated_credentials() {
  local file=$1
  local content
  local credential

  content=$(<"$file")
  for credential in "$application_password" "$migration_password" "$legacy_password"; do
    [[ "$content" != *"$credential"* ]] || {
      printf 'Generated database credentials appeared in verifier output\n' >&2
      exit 1
    }
  done
}

missing_runtime_environment="$test_root/missing-runtime.env"
{
  printf 'MIGRATION_DATABASE_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' \
    "$migration_role" "$migration_password" "$source_container" "$database"
  printf 'PP_PASSCODE=123456\n'
  printf 'PP_SESSION_SECRET=generated-session-secret-not-for-production\n'
} >"$missing_runtime_environment"
chmod 0600 "$missing_runtime_environment"

if docker run --rm \
  --network "$network" \
  --read-only \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
  --env-file "$missing_runtime_environment" \
  "$api_image" >"$test_root/missing-runtime.out" 2>"$test_root/missing-runtime.err"; then
  printf 'API unexpectedly accepted only the migration credential variable\n' >&2
  exit 1
fi
grep -q 'DATABASE_URL is required' "$test_root/missing-runtime.err"

start_api "$application_role" "$application_password" "$expected_application_name"
probe_api_readiness
if ! "$verifier" "${verify_arguments[@]}" >"$test_root/warm-cache.out" 2>"$test_root/warm-cache.err"; then
  printf 'Positive API role verification failed: ' >&2
  sed 's/^postgresql-api-role-verify: //' "$test_root/warm-cache.err" >&2
  exit 1
fi
grep -q '^api_role_verification=passed$' "$test_root/warm-cache.out"
grep -q '^api_session_contract=signed$' "$test_root/warm-cache.out"
assert_no_generated_credentials "$test_root/warm-cache.out"
assert_no_generated_credentials "$test_root/warm-cache.err"
assert_probe_cleanup
probe_admin_catalogue
assert_probe_cleanup

# Ensure a repeated durable probe still refreshes pg_stat_activity. The
# three-second age gate makes the first verifier's activity too old to satisfy
# the second run.
sleep 5
if ! "$verifier" "${verify_arguments[@]}" --max-activity-age-seconds 3 \
  >"$test_root/repeated-unlock.out" 2>"$test_root/repeated-unlock.err"; then
  printf 'Repeated-unlock API role verification failed: ' >&2
  sed 's/^postgresql-api-role-verify: //' "$test_root/repeated-unlock.err" >&2
  exit 1
fi
grep -q '^api_role_verification=passed$' "$test_root/repeated-unlock.out"
grep -q '^api_session_contract=signed$' "$test_root/repeated-unlock.out"
assert_no_generated_credentials "$test_root/repeated-unlock.out"
assert_no_generated_credentials "$test_root/repeated-unlock.err"
assert_probe_cleanup
stop_api

# A database-backed application probe without access to its required table must
# fail closed and remove its protected request, response, and cookie material.
docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --set=application_role="$application_role" \
  --file=- >/dev/null <<'SQL'
REVOKE SELECT, INSERT ON TABLE accounts FROM :"application_role";
SQL
account_access_revoked=true
start_api "$application_role" "$application_password" "$expected_application_name"
if "$verifier" "${verify_arguments[@]}" \
  >"$test_root/probe-failure.out" 2>"$test_root/probe-failure.err"; then
  printf 'Verifier accepted a failed database-backed application probe\n' >&2
  exit 1
fi
grep -q 'API database-backed unlock and library probe failed' "$test_root/probe-failure.err"
assert_no_generated_credentials "$test_root/probe-failure.out"
assert_no_generated_credentials "$test_root/probe-failure.err"
assert_probe_cleanup
stop_api
docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --set=application_role="$application_role" \
  --file=- >/dev/null <<'SQL'
GRANT SELECT, INSERT ON TABLE accounts TO :"application_role";
SQL
account_access_revoked=false

# An application-role session with the wrong workload name is not qualifying
# evidence, even though the API itself can start and answer the probe.
start_api "$application_role" "$application_password" manual-psql
if "$verifier" "${verify_arguments[@]}" \
  >"$test_root/wrong-workload.out" 2>"$test_root/wrong-workload.err"; then
  printf 'Verifier accepted an application-role session without the API workload identity\n' >&2
  exit 1
fi
grep -Eq 'unexpected database identity|no recent qualifying' "$test_root/wrong-workload.err"
assert_no_generated_credentials "$test_root/wrong-workload.out"
assert_no_generated_credentials "$test_root/wrong-workload.err"
assert_probe_cleanup
stop_api

# Keep a manual application-role psql backend open. The old username-only
# count is now positive, but it must not make a legacy-role API pass.
docker exec -d \
  -e PGAPPNAME=manual-psql \
  "$source_container" \
  psql -X --username="$application_role" --dbname="$database" \
  --command='SELECT pg_sleep(60);'
manual_session_started=true

manual_session_count=0
for ((attempt = 0; attempt < 10; attempt++)); do
  manual_session_count=$(docker exec -i "$source_container" \
    psql -X --username="$admin_user" --dbname="$database" \
    --set=ON_ERROR_STOP=1 --set=application_role="$application_role" \
    --tuples-only --no-align --file=- <<'SQL'
SELECT count(*)
FROM pg_stat_activity
WHERE usename = :'application_role'
  AND application_name = 'manual-psql';
SQL
  )
  [[ "$manual_session_count" =~ ^[1-9][0-9]*$ ]] && break
  sleep 1
done
[[ "$manual_session_count" =~ ^[1-9][0-9]*$ ]] || {
  printf 'Manual application-role regression session did not start\n' >&2
  exit 1
}

start_api "$legacy_role" "$legacy_password" "$expected_application_name"
if "$verifier" "${verify_arguments[@]}" \
  >"$test_root/legacy.out" 2>"$test_root/legacy.err"; then
  printf 'Verifier accepted a legacy-role API because a manual application session existed\n' >&2
  exit 1
fi
grep -q 'forbidden database role' "$test_root/legacy.err"
assert_no_generated_credentials "$test_root/legacy.out"
assert_no_generated_credentials "$test_root/legacy.err"
assert_probe_cleanup
stop_api

docker exec -i "$source_container" \
  psql -X --username="$admin_user" --dbname="$database" \
  --set=ON_ERROR_STOP=1 --set=application_role="$application_role" --file=- <<'SQL' >/dev/null
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE usename = :'application_role'
  AND application_name = 'manual-psql'
  AND pid <> pg_backend_pid();
SQL

manual_session_started=false

printf 'api_role_smoke=passed\n'
