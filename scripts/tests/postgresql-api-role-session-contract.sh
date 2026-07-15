#!/usr/bin/env bash

set -euo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly verifier="$repo_root/scripts/postgresql-api-role-verify.sh"
readonly extractor="$repo_root/scripts/lib/postgresql-api-role-session-cookie.awk"
readonly fixture_account='fixture-account-id'
readonly fixture_session='fixture-signed-session-token'

for command_name in awk grep mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done
[[ -x "$verifier" ]] || {
  printf 'API role verifier is unavailable\n' >&2
  exit 1
}
[[ -r "$extractor" ]] || {
  printf 'Session cookie extractor is unavailable\n' >&2
  exit 1
}

test_root=$(mktemp -d "${TMPDIR:-/tmp}/pandapages-session-contract.XXXXXX")
cleanup() {
  rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

test_number=0
printf '1..21\n'

pass_case() {
  test_number=$((test_number + 1))
  printf 'ok %d - %s\n' "$test_number" "$1"
}

fail_case() {
  test_number=$((test_number + 1))
  printf 'not ok %d - %s\n' "$test_number" "$1" >&2
  exit 1
}

assert_no_cookie_in_file() {
  local file=$1
  local content
  content=$(<"$file")
  [[ "$content" != *"$fixture_account"* && "$content" != *"$fixture_session"* ]] ||
    fail_case 'fixture cookie value remained in diagnostic output'
}

expect_accept() {
  local mode=$1
  local label=$2
  local expected=$3
  local headers=$4
  local fixture="$test_root/fixture.headers"
  local output="$test_root/extracted.cookie"
  local error="$test_root/extractor.err"

  printf '%s\n' "$headers" >"$fixture"
  if ! awk -v session_contract="$mode" -f "$extractor" "$fixture" >"$output" 2>"$error"; then
    fail_case "$label"
  fi
  [[ $(<"$output") == "$expected" && ! -s "$error" ]] || fail_case "$label"
  assert_no_cookie_in_file "$error"
  pass_case "$label"
}

expect_reject() {
  local mode=$1
  local label=$2
  local headers=$3
  local fixture="$test_root/fixture.headers"
  local output="$test_root/extracted.cookie"
  local error="$test_root/extractor.err"

  printf '%s\n' "$headers" >"$fixture"
  if awk -v session_contract="$mode" -f "$extractor" "$fixture" >"$output" 2>"$error"; then
    fail_case "$label"
  fi
  [[ ! -s "$output" && ! -s "$error" ]] || fail_case "$label"
  assert_no_cookie_in_file "$output"
  assert_no_cookie_in_file "$error"
  pass_case "$label"
}

expect_cli_reject() {
  local label=$1
  shift
  local output="$test_root/cli.out"
  local error="$test_root/cli.err"

  if "$verifier" "$@" >"$output" 2>"$error"; then
    fail_case "$label"
  fi
  assert_no_cookie_in_file "$output"
  assert_no_cookie_in_file "$error"
  pass_case "$label"
}

expect_accept legacy 'legacy accepts one unlock and one account cookie' \
  "pp_unlocked=1; pp_aid=$fixture_account" \
  $'  HTTP/1.1 200 OK\r\n  Set-Cookie: pp_unlocked=1; Path=/; HttpOnly\r\n  Set-Cookie: pp_aid='"$fixture_account"$'; Path=/; HttpOnly'
expect_reject legacy 'legacy rejects a missing unlock cookie' \
  $'HTTP/1.1 200 OK\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/'
expect_reject legacy 'legacy rejects a wrong unlock value' \
  $'Set-Cookie: pp_unlocked=0; Path=/\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/'
expect_reject legacy 'legacy rejects a missing account cookie' \
  $'Set-Cookie: pp_unlocked=1; Path=/'
expect_reject legacy 'legacy rejects an empty account cookie' \
  $'Set-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_aid=; Path=/; Max-Age=-1'
expect_reject legacy 'legacy rejects duplicate active authentication cookies' \
  $'Set-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/'
expect_reject legacy 'legacy rejects ambiguous active account cookies' \
  $'Set-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/\r\nSet-Cookie: pp_aid=second-fixture-account; Path=/'
expect_reject legacy 'legacy rejects a signed-only response' \
  $'Set-Cookie: pp_session='"$fixture_session"$'; Path=/'
expect_reject legacy 'legacy rejects mixed active signed and legacy cookies' \
  $'Set-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/\r\nSet-Cookie: pp_session='"$fixture_session"$'; Path=/'

expect_accept signed 'signed accepts one nonempty session cookie' \
  "pp_session=$fixture_session" \
  $'HTTP/1.1 200 OK\r\nSet-Cookie: pp_session='"$fixture_session"$'; Path=/; HttpOnly'
expect_reject signed 'signed rejects a missing session cookie' \
  $'HTTP/1.1 200 OK\r\nContent-Type: application/json'
expect_reject signed 'signed rejects an empty session cookie' \
  $'Set-Cookie: pp_session=; Path=/; Max-Age=-1'
expect_reject signed 'signed rejects duplicate active session cookies' \
  $'Set-Cookie: pp_session='"$fixture_session"$'; Path=/\r\nSet-Cookie: pp_session=second-fixture-token; Path=/'
expect_reject signed 'signed rejects a legacy-only response' \
  $'Set-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/'
expect_reject signed 'signed rejects mixed active legacy and signed cookies' \
  $'Set-Cookie: pp_session='"$fixture_session"$'; Path=/\r\nSet-Cookie: pp_unlocked=1; Path=/\r\nSet-Cookie: pp_aid='"$fixture_account"$'; Path=/'
expect_accept signed 'signed permits empty legacy deletion cookies' \
  "pp_session=$fixture_session" \
  $'Set-Cookie: pp_session='"$fixture_session"$'; Path=/\r\nSet-Cookie: pp_unlocked=; Path=/; Max-Age=-1\r\nSet-Cookie: pp_aid=; Path=/; Max-Age=-1'

expect_cli_reject 'CLI rejects an unknown session contract' --session-contract automatic
expect_cli_reject 'CLI rejects an empty session contract' --session-contract ''
expect_cli_reject 'CLI rejects a missing session-contract argument' --session-contract

help_output="$test_root/help.out"
help_error="$test_root/help.err"
"$verifier" --help >"$help_output" 2>"$help_error"
grep -q -- '--session-contract legacy|signed' "$help_output"
grep -Eq -- '--session-contract[[:space:]]+legacy$' "$help_output"
[[ ! -s "$help_error" ]] || fail_case 'help documents the session contract'
pass_case 'help documents the explicit option and legacy default'

grep -q '^session_contract=legacy$' "$verifier" || fail_case 'omitted option defaults to legacy'
grep -q "printf 'api_session_contract=%s" "$verifier" || fail_case 'omitted option defaults to legacy'
pass_case 'omitted session-contract option defaults to legacy'
