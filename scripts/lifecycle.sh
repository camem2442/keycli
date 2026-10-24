#!/usr/bin/env bash
# keycli lifecycle — build, install to ~/.local/bin, release artifacts (no sudo).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PREFIX="${PREFIX:-${HOME}/.local/bin}"
DIST="${ROOT}/dist"
VERSION="${KC_VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${KC_COMMIT:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo none)}"
LDFLAGS="-s -w -X github.com/camem2442/keycli/internal/version.Version=${VERSION} -X github.com/camem2442/keycli/internal/version.Commit=${COMMIT}"

usage() {
  echo "usage: $0 {build-dev [--install]|install|uninstall|release}" >&2
  exit 64
}

build_dev() {
  local do_install=false
  if [[ "${1:-}" == "--install" ]]; then
    do_install=true
    shift
  fi
  mkdir -p "$DIST"
  export CGO_ENABLED=0
  go build -ldflags "$LDFLAGS" -o "$DIST/kc" ./cmd/kc
  go build -ldflags "$LDFLAGS" -o "$DIST/keycli" ./cmd/keycli
  echo "built $DIST/kc ($VERSION $COMMIT)"
  if $do_install; then
    install_bins
  fi
}

install_bins() {
  if [[ ! -x "$DIST/kc" ]]; then
    echo "missing $DIST/kc — run: $0 build-dev" >&2
    exit 1
  fi
  mkdir -p "$PREFIX"
  install -m 755 "$DIST/kc" "$PREFIX/kc"
  ln -sf kc "$PREFIX/keycli"
  echo "installed kc + keycli -> $PREFIX"
}

uninstall_bins() {
  rm -f "$PREFIX/kc" "$PREFIX/keycli"
  echo "removed kc, keycli from $PREFIX"
}

release_artifacts() {
  mkdir -p "$DIST"
  export CGO_ENABLED=0
  local pairs=(
    "darwin arm64 kc-darwin-arm64"
    "darwin amd64 kc-darwin-amd64"
    "linux amd64 kc-linux-amd64"
    "linux arm64 kc-linux-arm64"
  )
  for p in "${pairs[@]}"; do
    read -r goos goarch out <<<"$p"
    GOOS=$goos GOARCH=$goarch go build -ldflags "$LDFLAGS" -o "$DIST/$out" ./cmd/kc
  done
  GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$DIST/keycli-linux-amd64" ./cmd/keycli
  (cd "$DIST" && sha256sum kc-* keycli-* 2>/dev/null | tee SHA256SUMS)
  echo "release artifacts in $DIST (+ SHA256SUMS)"
}

cmd="${1:-}"
shift || true
case "$cmd" in
  build-dev) build_dev "$@" ;;
  install) install_bins ;;
  uninstall) uninstall_bins ;;
  release) release_artifacts ;;
  "" | -h | --help) usage ;;
  *) echo "unknown subcommand: $cmd" >&2; usage ;;
esac
