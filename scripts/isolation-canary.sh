#!/usr/bin/env bash
# Host isolation canary for KeyCLI provider credentials.
#
# Dry run by default. --execute performs a read-only cross-UID check with a
# SYNTHETIC canary value; it never reads the Keychain, ~/.kc, KC_KEY or any real
# provider token.
#
# Exit codes: 0 PASS, 1 FAIL, 3 NOT_VERIFIED (missing privilege/tool), 64 usage.
set -euo pipefail

MODE="dry-run"
if [[ "${1:-}" == "--execute" ]]; then
  MODE="execute"
elif [[ -n "${1:-}" ]]; then
  echo "usage: $0 [--execute]" >&2
  exit 64
fi

CANARY="kc-isolation-canary-$(date +%s)-$$"
PEER_USER="${KC_ISOLATION_PEER_USER:-nobody}"

plan() {
  cat <<'TXT'
Host isolation canary (dry run)

What --execute does:
  1. create a 0700 temporary directory owned by the current user
  2. write one 0600 file containing a SYNTHETIC canary value
  3. attempt to read it as a different UID via `sudo -n -u <peer>`
     (default peer: nobody; override with KC_ISOLATION_PEER_USER)
  4. assert the read is denied, the canary is absent from the directory listing,
     and the value never appears on stdout/stderr of the peer command

What it never does: read the Keychain, ~/.kc, KC_KEY, environment of other
processes, or any real provider credential. It creates no OS account.

Required for --execute: passwordless sudo for the peer read
(`sudo -n -u <peer> true` must succeed). Otherwise the run reports NOT_VERIFIED.
TXT
}

if [[ "$MODE" == "dry-run" ]]; then
  plan
  echo
  echo "status=NOT_VERIFIED mode=dry-run"
  exit 3
fi

if ! command -v sudo >/dev/null 2>&1; then
  echo "status=NOT_VERIFIED reason=sudo-missing" >&2
  exit 3
fi
if ! sudo -n -u "$PEER_USER" true >/dev/null 2>&1; then
  echo "status=NOT_VERIFIED reason=no-passwordless-sudo-for-peer" >&2
  exit 3
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/kc-isolation.XXXXXX")"
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
chmod 700 "$work"
printf '%s\n' "$CANARY" >"$work/secret-canary"
chmod 600 "$work/secret-canary"

fail=0
check() {
  local name=$1 status=$2
  printf '%-38s %s\n' "$name" "$status"
  [[ "$status" == PASS ]] || fail=1
}

# 1. The peer UID must not be able to read the file.
if sudo -n -u "$PEER_USER" /bin/cat "$work/secret-canary" >"$work/peer.out" 2>"$work/peer.err"; then
  check cross_uid_read DENIED_UNEXPECTEDLY_ALLOWED
  fail=1
else
  check cross_uid_read PASS
fi

# 2. The peer must not be able to list the directory.
if sudo -n -u "$PEER_USER" /bin/ls "$work" >"$work/list.out" 2>&1; then
  check cross_uid_directory_listing DENIED_UNEXPECTEDLY_ALLOWED
  fail=1
else
  check cross_uid_directory_listing PASS
fi

# 3. The canary must not appear in the peer command output.
if grep -q -- "$CANARY" "$work/peer.out" "$work/peer.err" "$work/list.out" 2>/dev/null; then
  check canary_absent_from_output FAIL
  fail=1
else
  check canary_absent_from_output PASS
fi

# 4. Same-UID guard is application-level only; state it explicitly.
check same_uid_boundary APPLICATION_LEVEL_ONLY

if [[ "$fail" -ne 0 ]]; then
  echo "status=FAIL"
  exit 1
fi
echo "status=PASS scope=cross-uid-read-only"
