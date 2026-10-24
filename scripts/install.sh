#!/usr/bin/env bash
# Cloud-friendly install: gh release binary, else go install, else fail.
set -euo pipefail

REF="${KC_INSTALL_REF:-main}"
PREFIX="${PREFIX:-${HOME}/.local/bin}"
REPO="${KC_INSTALL_REPO:-github.com/camem2442/keycli}"
MODULE="${REPO}/cmd/kc"

detect_platform() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
  case "$os" in
    darwin | linux) ;;
    *) echo "unsupported OS: $os" >&2; exit 1 ;;
  esac
  PLATFORM_OS=$os
  PLATFORM_ARCH=$arch
}

install_from_release() {
  local asset="kc-${PLATFORM_OS}-${PLATFORM_ARCH}"
  local tmp repo_tag
  tmp="$(mktemp -d)"
  repo_tag=()
  if [[ "$REF" != "main" && "$REF" != "latest" ]]; then
    repo_tag=("$REF")
  fi
  if gh release download "${repo_tag[@]}" --repo "camem2442/keycli" --pattern "${asset}" --dir "$tmp" 2>/dev/null; then
    mkdir -p "$PREFIX"
    install -m 755 "$tmp/$asset" "$PREFIX/kc"
    ln -sf kc "$PREFIX/keycli"
    echo "installed from GitHub release $REF -> $PREFIX"
    return 0
  fi
  return 1
}

install_from_go() {
  if ! command -v go >/dev/null 2>&1; then
    return 1
  fi
  echo "install: go install ${MODULE}@${REF} (private repo needs GOPRIVATE + git auth)" >&2
  export CGO_ENABLED=0
  go install "${MODULE}@${REF}"
  local gopath
  gopath="$(go env GOPATH)/bin/kc"
  if [[ ! -x "$gopath" ]]; then
    echo "go install did not produce $gopath" >&2
    return 1
  fi
  mkdir -p "$PREFIX"
  install -m 755 "$gopath" "$PREFIX/kc"
  ln -sf kc "$PREFIX/keycli"
  echo "installed via go install -> $PREFIX"
  return 0
}

fail_help() {
  cat >&2 <<EOF
kc install failed.

Try one of:
  1) Authenticate gh and ensure release $REF exists:
       gh auth login
       KC_INSTALL_REF=$REF bash scripts/install.sh
  2) Install with Go (private repo: export GOPRIVATE=${REPO} and configure git credentials):
       go install ${MODULE}@${REF}
  3) Build from clone:
       bash scripts/lifecycle.sh build-dev --install

If this repository becomes public, go install works without extra auth.
EOF
  exit 1
}

detect_platform

if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  install_from_release && exit 0
fi

if install_from_go; then
  exit 0
fi

fail_help
