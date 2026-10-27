#!/usr/bin/env bash
# Rules consistency gate (V07-B2): marker block and required phrases in repo rules files.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BEGIN='<!-- kc-rules-begin -->'
END='<!-- kc-rules-end -->'
need_phrase='kc exec --require --only'

check_file() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  if ! grep -q "$BEGIN" "$f" || ! grep -q "$END" "$f"; then
    echo "check-rules: missing kc marker block in $f" >&2
    return 1
  fi
  if ! grep -q "$need_phrase" "$f"; then
    echo "check-rules: expected phrase '$need_phrase' in $f" >&2
    return 1
  fi
  if grep -q 'docs/contracts/cli.md' "$f" && ! grep -q 'github.com/camem2442/keycli' "$f"; then
    echo "check-rules: stale relative docs link in $f (use keycli GitHub URLs)" >&2
    return 1
  fi
}

fail=0
for f in "$ROOT/.cursor/rules/keycli.mdc" "$ROOT/AGENTS.md" "$ROOT/CLAUDE.md"; do
  if [[ -f "$f" ]] && grep -q "$BEGIN" "$f"; then
    check_file "$f" || fail=1
  fi
done
if [[ ! -f "$ROOT/.cursor/rules/keycli.mdc" ]]; then
  echo "check-rules: missing .cursor/rules/keycli.mdc" >&2
  fail=1
fi
exit "$fail"
