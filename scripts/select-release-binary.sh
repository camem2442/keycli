#!/usr/bin/env bash
# Print the dist/ Linux kc binary that matches the host CPU, so the execution smoke
# never needs x86-64 emulation. Unsupported architectures fail; they are never skipped.
set -euo pipefail
dist="${1:?usage: select-release-binary.sh <dist-dir> [uname-m-override]}"
machine="${2:-$(uname -m)}"
case "$machine" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "select-release-binary: unsupported host architecture: $machine" >&2; exit 1 ;;
esac
printf '%s/kc-linux-%s\n' "$dist" "$arch"
