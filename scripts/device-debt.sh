#!/usr/bin/env bash
# Read-only device debt report + explicit portable owning verifier bridge.
# Separate from real Mac/TCC/Keychain/HomeKit acceptance. No device side effects.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
mode="--plan"
if [[ "$#" -gt 0 ]]; then mode="$1"; fi

case "$mode" in
  --plan)
    cat <<'DEVICE_DEBT_JSON'
{
  "schema": "device-debt-plan.v0",
  "product": "keycli",
  "mode": "plan",
  "executed": [],
  "portable_suites": [
    {
      "name": "unit",
      "status": "NOT_VERIFIED"
    },
    {
      "name": "backend",
      "status": "NOT_VERIFIED"
    },
    {
      "name": "integration",
      "status": "NOT_VERIFIED"
    }
  ],
  "mac_headless_debt": [
    {
      "name": "Darwin keychain adapter build and read-only preflight",
      "status": "NOT_VERIFIED"
    }
  ],
  "manual_device_debt": [
    {
      "name": "real login Keychain ACL prompt with throwaway kc.test entry",
      "status": "NOT_VERIFIED"
    },
    {
      "name": "stale item cleanup / rollback",
      "status": "NOT_VERIFIED"
    },
    {
      "name": "service scoped permissions; no actual user secrets",
      "status": "NOT_VERIFIED"
    }
  ],
  "authorizes_device_effect": false,
  "authorizes_operating_effect": false
}
DEVICE_DEBT_JSON
    ;;
  --portable)
    if [[ "$#" -ne 2 || ! "$2" =~ ^[0-9a-f]{40}$  ]]; then
      echo 'USAGE: device-debt.sh --portable EXACT_GIT_HEAD' >&2
      exit 64
    fi
    if [[ "$(uname -s)" != Linux || "$(id -u)" -eq 0 ]]; then
      echo 'NOT_VERIFIED: portable run requires admitted nonroot Linux environment' >&2
      exit 2
    fi
    cd "$root"
    if [[ "$(git rev-parse HEAD)" != "$2"  ]]; then
      echo 'HOLD: exact source mismatch' >&2
      exit 66
    fi
    if [[ -n "$(git status --porcelain=v1)" ]]; then
      echo 'HOLD: dirty source checkout' >&2
      exit 67
    fi
    echo "PORTABLE_SOURCE_HEAD=$2"
    # Owning verifier remains the only test implementation; no live device call.
    bash verify.sh --suite unit
  bash verify.sh --suite backend
  bash verify.sh --suite integration
    if [[ -n "$(git status --porcelain=v1)" ]]; then
      echo 'HOLD: source changed during verification' >&2
      exit 68
    fi
    echo 'PORTABLE_VERIFIER_FINISHED_SCOPED; Mac/device/operating NOT_VERIFIED'
    ;;
  --mac-preflight)
    if [[ "$(uname -s)" != Darwin ]]; then
      echo 'NOT_VERIFIED: real Mac required' >&2
    else
      echo 'NOT_VERIFIED: Mac adapter / app / TCC checks require owning Mac admission'
    fi
    exit 2
    ;;
  --device)
    echo 'NOT_VERIFIED: device tests are manual/owner-admitted; see --plan debt.'
    echo 'No real Mac, Keychain, TCC, USB, Zigbee or operating effects performed.'
    exit 2
    ;;
  *)
    echo 'USAGE: device-debt.sh [--plan | --portable HEAD | --mac-preflight | --device]' >&2
    exit 64
    ;;
esac
