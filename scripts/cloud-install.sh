#!/usr/bin/env bash
# Cursor Cloud Agent build bootstrap (KC-40 / V07-A3).
# Installs pinned release kc with SHA256 verification. Does not compile from source.
# Optional private ai-core checkout — never fails the install (see cloud-clone-ai-core.sh).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

KC_INSTALL_REF="${KC_INSTALL_REF:-v0.6.0}"
PREFIX="${PREFIX:-${HOME}/.local/bin}"

export KC_INSTALL_REF
bash "$ROOT/scripts/install.sh"

if [[ ! -x "$PREFIX/kc" ]]; then
  echo "cloud-install: missing $PREFIX/kc after install.sh" >&2
  exit 1
fi

if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
  sudo ln -sf "$PREFIX/kc" /usr/local/bin/kc
  sudo ln -sf "$PREFIX/keycli" /usr/local/bin/keycli
fi

export PATH="$PREFIX:/usr/local/bin:${PATH:-}"
if ! command -v kc >/dev/null 2>&1; then
  echo "cloud-install: kc not on PATH" >&2
  exit 1
fi

if ! kc doctor --json >/dev/null; then
  echo "cloud-install: kc doctor --json failed" >&2
  exit 1
fi

# Optional; failures are non-fatal for the environment build.
bash "$ROOT/scripts/cloud-clone-ai-core.sh" || true

echo "cloud-install: complete (KC_INSTALL_REF=${KC_INSTALL_REF})"
