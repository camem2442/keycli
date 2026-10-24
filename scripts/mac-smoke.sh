#!/usr/bin/env bash
# Real-Mac smoke checklist (throwaway kc.test.* items). Linux: NOT VERIFIED skip.
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "mac-smoke: NOT VERIFIED on Linux — skipped"
  exit 0
fi

KC="${KC:-kc}"
if ! command -v "$KC" >/dev/null 2>&1; then
  echo "mac-smoke: $KC not in PATH" >&2
  exit 1
fi

TEST_ITEM="kc.test.smoke"
cleanup() {
  "$KC" rm "$TEST_ITEM" 2>/dev/null || true
}
trap cleanup EXIT

echo "mac-smoke: version"
"$KC" --version

echo "mac-smoke: set/get $TEST_ITEM"
printf 'smoke-value' | "$KC" set "$TEST_ITEM"
got="$("$KC" get "$TEST_ITEM")"
if [[ "$got" != "smoke-value" ]]; then
  echo "mac-smoke: get mismatch" >&2
  exit 1
fi

echo "mac-smoke: ls --json lists $TEST_ITEM"
"$KC" ls --json | grep -q "$TEST_ITEM"

echo "mac-smoke: cp + clipboard (manual paste optional)"
"$KC" cp "$TEST_ITEM" 2>/dev/null || true

echo "mac-smoke: ai-core read-only (set groq must fail)"
if printf 'x' | "$KC" set groq 2>/dev/null; then
  echo "mac-smoke: expected set groq to fail" >&2
  exit 1
fi

echo "mac-smoke: ok"
