#!/usr/bin/env bash
# Offline, source-bound G4 release acceptance on ONE independently admitted native Linux VM.
# Does not publish, issue credentials, install a service, or claim the other architecture.
set -euo pipefail
if [[ "$#" -ne 2 || ! "$1" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'USAGE: g4-free-release-gate.sh SOURCE_SHA arm64|amd64' >&2
  exit 64
fi
source_sha="$1"
arch="$2"
case "$arch" in
  arm64) machine=aarch64 ;;
  amd64) machine=x86_64 ;;
  *) echo 'HOLD: unknown native release architecture' >&2; exit 64 ;;
esac
if [[ "$(uname -s)" != Linux || "$(uname -m)" != "$machine" || "$(id -u)" -eq 0 ]]; then
  echo 'HOLD: nonroot native Linux release host required' >&2
  exit 2
fi
for key in KC_KEY KC_PROVIDER_EFFECTS GITHUB_TOKEN GH_TOKEN OPENAI_API_KEY \
           CLOUDFLARE_API_TOKEN OCI_CLI_KEY_CONTENT AWS_SECRET_ACCESS_KEY; do
  if [[ -n "${!key:-}" ]]; then
    echo "HOLD: credential or provider-effect switch is set: $key" >&2
    exit 2
  fi
done
for cmd in git go gofmt bash python3 rg jq sha256sum; do
  command -v "$cmd" >/dev/null || { echo "HOLD: missing tool $cmd" >&2; exit 2; }
done
for key in GITHUB_RUN_ID GITHUB_RUN_ATTEMPT GITHUB_WORKFLOW_SHA; do
  [[ -n "${!key:-}" ]] || { echo "HOLD: missing host run identity $key" >&2; exit 2; }
done
[[ "$GITHUB_RUN_ID" =~ ^[1-9][0-9]*$ && "$GITHUB_RUN_ATTEMPT" =~ ^[1-9][0-9]*$ &&
   "$GITHUB_WORKFLOW_SHA" =~ ^[0-9a-f]{40}$ &&
   "${GITHUB_REPOSITORY:-}" == camem2442/keycli ]] || {
  echo 'HOLD: invalid original run or repository identity' >&2; exit 2;
}
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
[[ "$(git rev-parse HEAD)" == "$source_sha" && -z "$(git status --porcelain=v1)" ]] || {
  echo 'HOLD: source HEAD drift or dirty checkout' >&2; exit 66;
}
# Synthetic untagged dev/RC candidate. Never call GitHub tag/release write APIs.
export KC_VERSION=v0.10.0-rc.999 KC_COMMIT="$source_sha" KC_BUILD_CHANNEL=rc
export RELEASE_VERSION="$KC_VERSION" SOURCE_SHA="$source_sha" RELEASE_CHANNEL=rc
export TAG_OBJECT="$source_sha" WORKFLOW_SHA="$GITHUB_WORKFLOW_SHA"
export RUNNER_MODE=isolated-vm CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off
export GOPROXY=off GOFLAGS=-mod=readonly
bash verify.sh --suite all
bash scripts/release-contract.sh provenance > dist/provenance.json
(cd dist && sha256sum provenance.json >> SHA256SUMS)
export MANIFEST_SHA="$(sha256sum dist/SHA256SUMS | cut -d ' ' -f1)"
bash scripts/release-contract.sh verify-bundle
bash scripts/release-contract.sh verify-binary "$arch"
bash scripts/verify-dev-release.sh --tag "$RELEASE_VERSION" --assets-dir dist
[[ "$(git rev-parse HEAD)" == "$source_sha" && -z "$(git status --porcelain=v1)" ]] || {
  echo 'HOLD: source changed during release verification' >&2; exit 67;
}
printf 'G4_NATIVE_%s_RELEASE_SCOPED_PASS head=%s manifest=%s\n' "$arch" "$source_sha" "$MANIFEST_SHA"
echo 'NOT VERIFIED: other native architecture, real Mac Keychain, signed host admission, publication, provider effects'
