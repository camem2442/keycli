#!/usr/bin/env bash
# Re-check the landed source against the recorded candidate blob manifest.
#
# Usage: bash verify-source-parity.sh [git-ref]
#   git-ref defaults to HEAD.
#
# Prints "path expected actual" for every mismatch and exits 1 when any path
# differs. Paths that no longer exist are reported with actual=missing.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$ROOT" rev-parse --show-toplevel)"
MANIFEST="$ROOT/exact-source-manifest.json"
REF="${1:-HEAD}"

[[ -f "$MANIFEST" ]] || { echo "missing manifest: $MANIFEST" >&2; exit 1; }

python3 - "$REPO" "$REF" "$MANIFEST" <<'PY'
import json, subprocess, sys

repo, ref, manifest_path = sys.argv[1:4]
manifest = json.load(open(manifest_path))

def blob(path, rev):
    p = subprocess.run(["git", "-C", repo, "rev-parse", f"{rev}:{path}"],
                       capture_output=True, text=True)
    return p.stdout.strip() if p.returncode == 0 else "missing"

# Later PRs supersede earlier ones, so keep only the final expected blob per path.
expected, owner = {}, {}
for pr in manifest["owning_prs"]:
    for entry in pr["files"]:
        expected[entry["path"]] = entry["blob"]
        owner[entry["path"]] = pr["pr"]

mismatches, checked = [], 0
for path in sorted(expected):
    checked += 1
    actual = blob(path, ref)
    if actual != expected[path]:
        mismatches.append((path, expected[path], actual, owner[path]))

print(f"ref={ref} paths_checked={checked} mismatches={len(mismatches)}")
for path, exp, act, pr in mismatches:
    print(f"{path}\texpected={exp}\tactual={act}\towner_pr=#{pr}")
print(f"candidate_tree={manifest['candidate']['tree']}")
sys.exit(1 if mismatches else 0)
PY
