#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
env_file="$repo_root/.env"
compose_file="$repo_root/docker-compose.dev.yml"
database=pandapages
admin_user=pandapages

usage() {
  cat <<'EOF'
Usage:
  scripts/dev/bootstrap-postgresql.sh

Bootstraps the local Panda Pages PostgreSQL development database:

  1. starts PostgreSQL;
  2. creates/normalizes the least-privilege database roles;
  3. provisions local migrator/application passwords from the ignored .env;
  4. runs all Goose migrations;
  5. reapplies the final role policy;
  6. verifies the resulting role/privilege contract.

The command is idempotent and does not delete the PostgreSQL volume or
application data.

To prove a truly fresh bootstrap manually:
  docker compose -f docker-compose.dev.yml down --remove-orphans
  docker volume rm pandapages_pgdata
  make db-bootstrap
EOF
}

die() {
  printf 'db-bootstrap: %s\n' "$*" >&2
  exit 1
}

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
  "")
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

[[ -z ${DOCKER_HOST:-} ]] ||
  die 'DOCKER_HOST must be unset; remote Docker targets are refused'

command -v docker >/dev/null 2>&1 || die 'docker is required'
command -v python3 >/dev/null 2>&1 || die 'python3 is required'

[[ -r "$env_file" ]] ||
  die 'root .env is required; copy .env.example and provide local credentials'
[[ -r "$compose_file" ]] || die 'docker-compose.dev.yml is unavailable'
[[ -r "$repo_root/secrets/pg_password.txt" ]] ||
  die 'secrets/pg_password.txt is required'

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
[[ "$docker_context" == default || "$docker_context" == desktop-linux ]] ||
  die 'a known local Docker context is required'
[[ "$docker_endpoint" == unix://* ]] ||
  die 'Docker must use a local Unix socket'

compose=(
  docker compose
  --project-directory "$repo_root"
  --env-file "$env_file"
  -f "$compose_file"
)

"${compose[@]}" config --quiet
"${compose[@]}" up -d postgres

container_id=$("${compose[@]}" ps -q postgres)
[[ -n "$container_id" ]] || die 'PostgreSQL container was not created'

container_name=$(docker inspect --format '{{.Name}}' "$container_id")
container_name=${container_name#/}
[[ -n "$container_name" ]] ||
  die 'PostgreSQL container name could not be resolved'

health=""
for _ in $(seq 1 60); do
  health=$(docker inspect --format \
    '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
    "$container_id")
  case "$health" in
    healthy)
      break
      ;;
    exited|dead|unhealthy)
      die "PostgreSQL became $health during startup"
      ;;
  esac
  sleep 1
done
[[ "$health" == healthy ]] ||
  die 'PostgreSQL did not become healthy within 60 seconds'

role_apply=(
  "$repo_root/scripts/postgresql-roles.sh"
  apply
  --container "$container_name"
  --database "$database"
  --admin-user "$admin_user"
  --confirm-apply
)

role_verify=(
  "$repo_root/scripts/postgresql-roles.sh"
  verify
  --container "$container_name"
  --database "$database"
  --admin-user "$admin_user"
)

printf 'db-bootstrap: applying initial PostgreSQL role policy\n'
"${role_apply[@]}"

printf 'db-bootstrap: provisioning local role passwords\n'

python3 - "$env_file" "$repo_root/secrets/pg_password.txt" <<'PY' |
from pathlib import Path
from urllib.parse import unquote, urlsplit
import sys

env_path = Path(sys.argv[1])
bootstrap_secret_path = Path(sys.argv[2])

values: dict[str, str] = {}
for raw in env_path.read_text(encoding="utf-8").splitlines():
    line = raw.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    key, value = line.split("=", 1)
    key = key.strip()
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        value = value[1:-1]
    values[key] = value

targets = (
    ("MIGRATION_DATABASE_URL", "pandapages_migrator"),
    ("APP_DATABASE_URL", "pandapages_app"),
)

passwords: dict[str, str] = {}

for variable, expected_user in targets:
    raw_url = values.get(variable)
    if not raw_url:
        raise SystemExit(f"db-bootstrap: {variable} is missing or empty in .env")

    parsed = urlsplit(raw_url)

    if parsed.scheme not in {"postgres", "postgresql"}:
        raise SystemExit(f"db-bootstrap: {variable} must use postgres/postgresql URL syntax")
    if unquote(parsed.username or "") != expected_user:
        raise SystemExit(
            f"db-bootstrap: {variable} must authenticate as {expected_user}"
        )
    if parsed.hostname != "postgres":
        raise SystemExit(
            f"db-bootstrap: {variable} host must be the Compose service name postgres"
        )
    if parsed.port not in {None, 5432}:
        raise SystemExit(f"db-bootstrap: {variable} must use PostgreSQL port 5432")
    if parsed.path != "/pandapages":
        raise SystemExit(f"db-bootstrap: {variable} database must be pandapages")

    password = unquote(parsed.password or "")
    if not password:
        raise SystemExit(f"db-bootstrap: {variable} password is empty")
    if any(ord(ch) < 32 or ord(ch) == 127 for ch in password):
        raise SystemExit(
            f"db-bootstrap: {variable} password contains control characters"
        )

    passwords[expected_user] = password

bootstrap_password = bootstrap_secret_path.read_text(encoding="utf-8").rstrip("\r\n")
if not bootstrap_password:
    raise SystemExit("db-bootstrap: PostgreSQL bootstrap password secret is empty")

if passwords["pandapages_migrator"] == passwords["pandapages_app"]:
    raise SystemExit(
        "db-bootstrap: application and migration database passwords must be distinct"
    )

for role, password in passwords.items():
    if password == bootstrap_password:
        raise SystemExit(
            f"db-bootstrap: {role} password must differ from the bootstrap password"
        )

def sql_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"

for role in ("pandapages_migrator", "pandapages_app"):
    print(f"ALTER ROLE {role} PASSWORD {sql_literal(passwords[role])};")
PY
docker exec -i "$container_name" \
  psql -X \
  --username="$admin_user" \
  --dbname="$database" \
  --set=ON_ERROR_STOP=1 \
  >/dev/null

printf 'db-bootstrap: running Goose migrations\n'
"${compose[@]}" run --rm migrate

printf 'db-bootstrap: applying final PostgreSQL role policy\n'
"${role_apply[@]}"

printf 'db-bootstrap: verifying PostgreSQL role policy\n'
"${role_verify[@]}"

printf 'db-bootstrap: ready\n'
