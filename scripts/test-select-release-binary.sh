#!/usr/bin/env bash
# Regression for host-architecture selection of the release smoke binary.
set -euo pipefail
S="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/select-release-binary.sh"
[[ "$(bash "$S" /d x86_64)" == "/d/kc-linux-amd64" ]]
[[ "$(bash "$S" /d aarch64)" == "/d/kc-linux-arm64" ]]
[[ "$(bash "$S" /d arm64)" == "/d/kc-linux-arm64" ]]
if bash "$S" /d riscv64 2>/dev/null; then echo "unsupported arch must fail" >&2; exit 1; fi
echo "select-release-binary: ok"
