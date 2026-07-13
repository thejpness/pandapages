#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
sql_dir=${PP_POSTGRES_ROLES_SQL_DIR:-$repo_root/deploy/postgresql-roles}

usage() {
  cat <<'EOF'
Usage:
  scripts/postgresql-roles.sh audit  --container NAME --database NAME [--admin-user ROLE]
  scripts/postgresql-roles.sh apply  --container NAME --database NAME [--admin-user ROLE] --confirm-apply
  scripts/postgresql-roles.sh verify --container NAME --database NAME [--admin-user ROLE]

Audits, applies, or verifies the Panda Pages PostgreSQL role policy through the
running PostgreSQL container's local Unix socket. No password is accepted,
printed, or changed. The apply mode changes only roles, ownership, database
access control, and object privileges; it never drops roles or objects and
never changes application data.

Defaults:
  --admin-user       pandapages
  owner role         pandapages_owner
  migration role     pandapages_migrator
  application role   pandapages_app
  backup role        pandapages_backup

The target must be a dedicated PostgreSQL 18 cluster containing no unrelated
databases. Apply is refused unless --confirm-apply is present.
EOF
}

die() {
  printf 'postgresql-roles: %s\n' "$*" >&2
  exit 1
}

[[ ${1:-} != -h && ${1:-} != --help ]] || {
  usage
  exit 0
}

mode=${1:-}
[[ "$mode" == audit || "$mode" == apply || "$mode" == verify ]] || {
  usage >&2
  exit 2
}
shift

container=""
database=""
admin_user=pandapages
owner_role=pandapages_owner
migration_role=pandapages_migrator
application_role=pandapages_app
backup_role=pandapages_backup
confirmed=false

while (($# > 0)); do
  case "$1" in
    --container)
      container=${2:-}
      shift 2
      ;;
    --database)
      database=${2:-}
      shift 2
      ;;
    --admin-user)
      admin_user=${2:-}
      shift 2
      ;;
    --owner-role)
      owner_role=${2:-}
      shift 2
      ;;
    --migration-role)
      migration_role=${2:-}
      shift 2
      ;;
    --application-role)
      application_role=${2:-}
      shift 2
      ;;
    --backup-role)
      backup_role=${2:-}
      shift 2
      ;;
    --confirm-apply)
      confirmed=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

[[ -n "$container" ]] || die '--container is required'
[[ -n "$database" ]] || die '--database is required'
[[ -z ${DOCKER_HOST:-} ]] || die 'DOCKER_HOST must be unset; remote Docker targets are refused'

[[ "$container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || die 'invalid container name'
[[ "$database" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die 'invalid database name'
for role in "$admin_user" "$owner_role" "$migration_role" "$application_role" "$backup_role"; do
  [[ "$role" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die 'invalid role name'
done

role_names=("$owner_role" "$migration_role" "$application_role" "$backup_role")
for role in "${role_names[@]}"; do [[ "$admin_user" != "$role" ]] || die 'administrative and policy role names must be distinct'; done
for ((left = 0; left < ${#role_names[@]}; left++)); do
  for ((right = left + 1; right < ${#role_names[@]}; right++)); do
    [[ "${role_names[left]}" != "${role_names[right]}" ]] || die 'policy role names must be distinct'
  done
done

[[ "$mode" != apply || "$confirmed" == true ]] || die 'apply requires --confirm-apply'
[[ "$mode" == apply || "$confirmed" == false ]] || die '--confirm-apply is valid only with apply'

case "$mode" in
  audit) sql_file="$sql_dir/audit.sql" ;;
  apply) sql_file="$sql_dir/apply.sql" ;;
  verify) sql_file="$sql_dir/verify.sql" ;;
esac
[[ -r "$sql_file" ]] || die "SQL policy file is unavailable: $sql_file"
command -v docker >/dev/null 2>&1 || die 'docker is required'

docker_context=$(docker context show)
docker_endpoint=$(docker context inspect "$docker_context" --format '{{.Endpoints.docker.Host}}')
[[ "$docker_context" == default || "$docker_context" == desktop-linux ]] ||
  die 'a known local Docker context is required'
[[ "$docker_endpoint" == unix://* ]] || die 'Docker must use a local Unix socket'

running=$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null || true)
[[ "$running" == true ]] || die 'PostgreSQL container is not running'

psql_args=(
  psql -X
  --username="$admin_user"
  --dbname="$database"
  --set=ON_ERROR_STOP=1
  --set=database_name="$database"
  --set=owner_role="$owner_role"
  --set=migration_role="$migration_role"
  --set=application_role="$application_role"
  --set=backup_role="$backup_role"
)

if [[ "$mode" == audit || "$mode" == verify ]]; then
  docker exec -i \
    -e 'PGOPTIONS=-c default_transaction_read_only=on' \
    "$container" "${psql_args[@]}" <"$sql_file"
else
  docker exec -i "$container" "${psql_args[@]}" <"$sql_file"
fi
