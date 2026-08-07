#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$repo_root"

die() {
  printf 'env-example contract failed: %s\n' "$*" >&2
  exit 1
}

[[ -f .env.example ]] || die '.env.example is missing'
git check-ignore -q .env.example && die '.env.example is ignored by Git'

compose_vars=$(
  grep -hEo '\$\{[A-Z][A-Z0-9_]*' docker-compose.yml docker-compose.dev.yml |
    sed 's/^${//' |
    sort -u
)
template_vars=$(
  sed -nE 's/^([A-Z][A-Z0-9_]*)=.*/\1/p' .env.example |
    sort -u
)

missing_from_template=$(comm -23 \
  <(printf '%s\n' "$compose_vars") \
  <(printf '%s\n' "$template_vars"))
[[ -z $missing_from_template ]] ||
  die "Compose variable(s) missing from .env.example: ${missing_from_template//$'\n'/, }"

inactive_in_template=$(comm -13 \
  <(printf '%s\n' "$compose_vars") \
  <(printf '%s\n' "$template_vars"))
[[ -z $inactive_in_template ]] ||
  die "active assignment(s) are not consumed by Compose: ${inactive_in_template//$'\n'/, }"

documented() {
  local name=$1
  grep -Eq "^[[:space:]#]*${name}=" .env.example
}

# These are supported by direct process invocation or fixed by Compose rather
# than imported from the root environment. They must remain documented without
# pretending that root Compose consumes them.
for direct_name in DATABASE_URL VITE_API_BASE PP_SUPABASE_ISSUER PP_SUPABASE_AUDIENCE PP_SUPABASE_JWKS_URL VITE_SUPABASE_URL VITE_SUPABASE_PUBLISHABLE_KEY; do
  documented "$direct_name" || die "$direct_name is not documented"
done

for active_name in SUPABASE_URL SUPABASE_PUBLISHABLE_KEY SUPABASE_JWT_AUDIENCE; do
  grep -Eq "^${active_name}=" .env.example || die "$active_name is not active"
done
printf 'env_example_contract=passed\n'
