#!/usr/bin/env bash
# Install kc to ~/.local/bin (no sudo). Public repo: curl release or go install first.
set -euo pipefail

REF="${KC_INSTALL_REF:-latest}"
PREFIX="${PREFIX:-${HOME}/.local/bin}"
MODULE="github.com/camem2442/keycli/cmd/kc"
GITHUB_REPO="camem2442/keycli"

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

resolve_release_tag() {
  local ref="$1"
  if [[ "$ref" == "latest" || "$ref" == "main" ]]; then
    curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" |
      grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'
  else
    echo "$ref"
  fi
}

install_from_curl() {
  local asset="kc-${PLATFORM_OS}-${PLATFORM_ARCH}"
  local tag tmp url
  tag="$(resolve_release_tag "$REF")"
  if [[ -z "$tag" ]]; then
    return 1
  fi
  tmp="$(mktemp -d)"
  url="https://github.com/${GITHUB_REPO}/releases/download/${tag}/${asset}"
  if ! curl -fsSL "$url" -o "$tmp/$asset"; then
    return 1
  fi
  mkdir -p "$PREFIX"
  install -m 755 "$tmp/$asset" "$PREFIX/kc"
  ln -sf kc "$PREFIX/keycli"
  echo "installed ${asset} from release ${tag} -> $PREFIX"
  return 0
}

install_from_go() {
  if ! command -v go >/dev/null 2>&1; then
    return 1
  fi
  local ver="$REF"
  if [[ "$ver" == "main" ]]; then
    ver="latest"
  fi
  export CGO_ENABLED=0
  go install "${MODULE}@${ver}"
  local gopath
  gopath="$(go env GOPATH)/bin/kc"
  if [[ ! -x "$gopath" ]]; then
    return 1
  fi
  mkdir -p "$PREFIX"
  install -m 755 "$gopath" "$PREFIX/kc"
  ln -sf kc "$PREFIX/keycli"
  echo "installed via go install ${MODULE}@${ver} -> $PREFIX"
  return 0
}

install_from_gh() {
  if ! command -v gh >/dev/null 2>&1; then
    return 1
  fi
  local asset="kc-${PLATFORM_OS}-${PLATFORM_ARCH}"
  local tmp=() tag_args=()
  tmp="$(mktemp -d)"
  if [[ "$REF" != "latest" && "$REF" != "main" ]]; then
    tag_args=("$REF")
  fi
  if ! gh release download "${tag_args[@]}" --repo "$GITHUB_REPO" --pattern "$asset" --dir "$tmp" 2>/dev/null; then
    return 1
  fi
  mkdir -p "$PREFIX"
  install -m 755 "$tmp/$asset" "$PREFIX/kc"
  ln -sf kc "$PREFIX/keycli"
  echo "installed via gh release download -> $PREFIX"
  return 0
}

fail_help() {
  cat >&2 <<EOF
kc install failed.

Try:
  curl -fsSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/scripts/install.sh | bash
  go install ${MODULE}@latest
  bash scripts/lifecycle.sh build-dev --install   # from a git clone

Optional: gh release download (if gh is authenticated).
Fork/private clone: set GOPRIVATE and git credentials, then go install.
EOF
  exit 1
}

detect_platform

if install_from_curl; then exit 0; fi
if install_from_go; then exit 0; fi
if install_from_gh; then exit 0; fi

fail_help
