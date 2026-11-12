#!/usr/bin/env bash
# Source-only contract check: no Keychain, Mac host, UI or physical device.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
probe="$root/scripts/device-debt.sh"
bash -n "$probe"
plan=$(bash "$probe" --plan)
python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); assert d["schema"]=="device-debt-plan.v0"; assert d["executed"]==[]; assert not d["authorizes_device_effect"]; assert not d["authorizes_operating_effect"]; assert all(t["status"]=="NOT_VERIFIED" for k in ("portable_suites","mac_headless_debt","manual_device_debt") for t in d[k])' <<<"$plan"
status=0
bash "$probe" --device >/dev/null 2>&1 || status=$?
[[ "$status" -eq 2 ]] || { echo "FAIL: device mode must be NOT_VERIFIED (2)" >&2; exit 1; }
status=0
bash "$probe" --portable invalid-head >/dev/null 2>&1 || status=$?
[[ "$status" -eq 64 ]] || { echo "FAIL: portable bad SHA must be usage (64)" >&2; exit 1; }
echo 'PASS device-debt offline plan contract (real host NOT_VERIFIED)'
