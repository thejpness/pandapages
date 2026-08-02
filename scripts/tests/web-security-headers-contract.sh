#!/usr/bin/env bash
set -euo pipefail

readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly nginx_config="$repo_root/apps/web/nginx.conf"
readonly index_html="$repo_root/apps/web/index.html"

policy="$(sed -n 's/^[[:space:]]*add_header Content-Security-Policy "\(.*\)" always;$/\1/p' "$nginx_config")"
if [[ -z "$policy" ]]; then
  echo "production Nginx configuration has no Content-Security-Policy header" >&2
  exit 1
fi

for required in \
  "default-src 'self'" \
  "script-src 'self'" \
  "style-src 'self'" \
  "img-src 'self'" \
  "font-src 'self'" \
  "connect-src 'self'" \
  "worker-src 'self'" \
  "manifest-src 'self'" \
  "object-src 'none'" \
  "base-uri 'self'" \
  "frame-ancestors 'none'" \
  "form-action 'self'"; do
  if [[ "$policy" != *"$required"* ]]; then
    echo "production CSP is missing: $required" >&2
    exit 1
  fi
done

for forbidden in "unsafe-eval" "script-src 'self' 'unsafe-inline'" "*" "http://" "https://"; do
  if [[ "$policy" == *"$forbidden"* ]]; then
    echo "production CSP contains forbidden source: $forbidden" >&2
    exit 1
  fi
done

for header in X-Content-Type-Options Referrer-Policy Permissions-Policy; do
  if ! grep -Eq "^[[:space:]]*add_header $header " "$nginx_config"; then
    echo "production Nginx configuration is missing $header" >&2
    exit 1
  fi
done

if grep -nP '<script(?![^>]*\bsrc=)' "$index_html"; then
  echo "index.html contains an inline script that production CSP would block" >&2
  exit 1
fi

echo "production web security-header contract passed"
