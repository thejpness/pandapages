# Extract only the authentication cookies required by the explicitly selected
# Panda Pages session contract. Cookie values are written solely to the
# caller-selected output; failures are deliberately silent.

function active_cookie(name, value, pair) {
  if (value == "") return

  if (name == "pp_unlocked") {
    legacy_unlock_count++
    if (value == "1") legacy_unlock_valid++
    return
  }
  if (name == "pp_aid") {
    legacy_account_count++
    legacy_account_pair = pair
    return
  }
  if (name == "pp_session") {
    signed_session_count++
    signed_session_pair = pair
  }
}

{
  line = $0
  sub(/\r$/, "", line)

  separator = index(line, ":")
  if (separator == 0) next

  header_name = substr(line, 1, separator - 1)
  sub(/^[ \t]+/, "", header_name)
  sub(/[ \t]+$/, "", header_name)
  if (tolower(header_name) != "set-cookie") next

  cookie_pair = substr(line, separator + 1)
  sub(/^[ \t]+/, "", cookie_pair)
  attribute = index(cookie_pair, ";")
  if (attribute > 0) cookie_pair = substr(cookie_pair, 1, attribute - 1)
  sub(/[ \t]+$/, "", cookie_pair)

  equals = index(cookie_pair, "=")
  if (equals <= 1) next

  cookie_name = substr(cookie_pair, 1, equals - 1)
  cookie_value = substr(cookie_pair, equals + 1)
  active_cookie(cookie_name, cookie_value, cookie_pair)
}

END {
  if (session_contract == "legacy") {
    if (legacy_unlock_count != 1 || legacy_unlock_valid != 1) exit 1
    if (legacy_account_count != 1 || signed_session_count != 0) exit 1
    printf "pp_unlocked=1; %s", legacy_account_pair
    exit 0
  }

  if (session_contract == "signed") {
    if (signed_session_count != 1) exit 1
    if (legacy_unlock_count != 0 || legacy_account_count != 0) exit 1
    printf "%s", signed_session_pair
    exit 0
  }

  exit 2
}
