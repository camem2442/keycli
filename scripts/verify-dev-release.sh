#!/usr/bin/env bash
# Dev release acceptance without touching the real installation.
#
# Verifies a published (or locally built) development release:
#   * every asset matches SHA256SUMS
#   * the host asset installs to a temporary PREFIX byte-for-byte
#   * the installed binary reports the tag, passes the MCP smoke and, if the
#     gateway asset exists, reports provider effects locked
#   * the pre-existing kc install is unchanged
#
# Usage:
#   scripts/verify-dev-release.sh --tag v0.10.0-dev.1
#   scripts/verify-dev-release.sh --tag v0.10.0-dev.1 --assets-dir dist
#   scripts/verify-dev-release.sh --tag v0.10.0-dev.1 --assets-dir dist --use-installer
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAG=""
REPO="camem2442/keycli"
ASSETS_DIR=""
USE_INSTALLER=false
ALLOW_ARCH_SKIP=false
WORK="$(mktemp -d "${TMPDIR:-/tmp}/kc-dev-release.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) TAG="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --assets-dir) ASSETS_DIR="${2:?}"; shift 2 ;;
    --use-installer) USE_INSTALLER=true; shift ;;
    --allow-arch-skip) ALLOW_ARCH_SKIP=true; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 64 ;;
  esac
done

[[ -n "$TAG" ]] || { echo "--tag is required" >&2; exit 64; }
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(dev|rc)\.[0-9]+)?$ ]] || { echo "not a release tag: $TAG" >&2; exit 64; }
[[ "$TAG" == *-dev.* || "$TAG" == *-rc.* ]] || { echo "this acceptance script is for pre-releases only: $TAG" >&2; exit 64; }

sha_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; esac
case "$os" in darwin|linux) ;; *) os=unknown ;; esac
HOST_ASSET="kc-${os}-${arch}"
GATEWAY_ASSET="kc-gateway-${os}-${arch}"

fail=0
note() { printf '%-34s %s\n' "$1" "$2"; }
check() {
  case "$2" in
    PASS*) note "$1" "$2" ;;
    SKIP*) note "$1" "$2" ;;
    *)     note "$1" "$2"; fail=1 ;;
  esac
}

PREEXISTING="${HOME}/.local/bin/kc"
PREEXISTING_SHA=""
if [[ -f "$PREEXISTING" ]]; then PREEXISTING_SHA="$(sha_of "$PREEXISTING")"; fi

mkdir -p "$WORK/assets"
if [[ -n "$ASSETS_DIR" ]]; then
  cp "$ASSETS_DIR"/* "$WORK/assets/" 2>/dev/null
  note assets "local: $ASSETS_DIR"
else
  if ! gh release download "$TAG" -R "$REPO" --pattern '*' --dir "$WORK/assets" >/dev/null 2>&1; then
    note assets "download failed"; echo '{"status":"BLOCK","reason":"assets-unavailable"}'; exit 3
  fi
  note assets "downloaded: $REPO@$TAG"
fi

[[ -f "$WORK/assets/SHA256SUMS" ]] || { note sha256sums "missing"; echo '{"status":"BLOCK","reason":"no-sha256sums"}'; exit 3; }

# 1. every listed asset must match
if (cd "$WORK/assets" && { command -v sha256sum >/dev/null 2>&1 && sha256sum -c SHA256SUMS || shasum -a 256 -c SHA256SUMS; }) >"$WORK/sha.log" 2>&1; then
  check checksums PASS
else
  check checksums FAIL; tail -3 "$WORK/sha.log"
fi

# 2. host asset must exist
if [[ -f "$WORK/assets/$HOST_ASSET" ]]; then
  check host_asset "PASS ($HOST_ASSET)"
else
  if $ALLOW_ARCH_SKIP; then check host_asset "SKIP (no $HOST_ASSET)"; else check host_asset "FAIL (no $HOST_ASSET)"; fi
fi

# 3. install into a temporary prefix and compare bytes
PREFIX="$WORK/bin"
mkdir -p "$PREFIX"
if [[ -f "$WORK/assets/$HOST_ASSET" ]]; then
  if $USE_INSTALLER; then
    if KC_INSTALL_REF="$TAG" PREFIX="$PREFIX" bash "$ROOT/scripts/install.sh" >"$WORK/install.log" 2>&1; then
      check installer PASS
    else
      check installer FAIL; tail -3 "$WORK/install.log"
    fi
  else
    cp "$WORK/assets/$HOST_ASSET" "$PREFIX/kc"
  fi
  chmod 755 "$PREFIX/kc"
  cmp -s "$WORK/assets/$HOST_ASSET" "$PREFIX/kc" && check installed_bytes PASS || check installed_bytes FAIL

  version_out="$("$PREFIX/kc" --version 2>&1 || true)"
  if [[ "$version_out" == *"$TAG"* ]]; then check installed_version PASS; else check installed_version "FAIL ($version_out)"; fi

  if bash "$ROOT/scripts/verify-mcp-binary.sh" "$PREFIX/kc" "$TAG" >"$WORK/mcp.log" 2>&1; then
    check mcp_smoke PASS
  else
    check mcp_smoke FAIL; tail -3 "$WORK/mcp.log"
  fi
fi

# 4. downloaded assets may lack executable permissions; test a private copy.
if [[ -f "$WORK/assets/$GATEWAY_ASSET" ]]; then
  gateway="$WORK/gateway"
  if cp "$WORK/assets/$GATEWAY_ASSET" "$gateway" && chmod 700 "$gateway"; then
    report="$(env -u KC_PROVIDER_EFFECTS "$gateway" -lock-report 2>&1)"
    locked_status=$?
    enabled_report="$(KC_PROVIDER_EFFECTS=enabled "$gateway" -lock-report 2>&1)"
    enabled_status=$?
    if [[ "$locked_status" -eq 0 && "$enabled_status" -eq 0 &&
          "$report" == *'"provider_enabled":false'* &&
          "$enabled_report" == *'"provider_enabled":false'* ]]; then
      check provider_lock PASS
    else
      check provider_lock "FAIL (gateway lock check failed)"
    fi
  else
    check provider_lock "FAIL (gateway copy or chmod failed)"
  fi
else
  note provider_lock "SKIP (gateway asset not published)"
fi

# 5. existing installation must be untouched
if [[ -n "$PREEXISTING_SHA" ]]; then
  after="$(sha_of "$PREEXISTING")"
  [[ "$PREEXISTING_SHA" == "$after" ]] && check existing_install_unchanged PASS \
    || check existing_install_unchanged "FAIL (changed: $PREEXISTING_SHA -> $after)"
else
  note existing_install_unchanged "SKIP (no $PREEXISTING)"
fi

if [[ $fail -eq 0 ]]; then
  printf '{"status":"PASS","tag":"%s","host_asset":"%s"}\n' "$TAG" "$HOST_ASSET"
  exit 0
fi
printf '{"status":"FAIL","tag":"%s","host_asset":"%s"}\n' "$TAG" "$HOST_ASSET"
exit 3
