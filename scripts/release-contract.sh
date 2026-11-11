#!/usr/bin/env bash
# Shared checks for the GitHub release jobs. The job supplies the selected identity.
set -euo pipefail

fail() { echo "::error::$*" >&2; exit 1; }

validate_version() {
  local number='(0|[1-9][0-9]*)'
  [[ "${RELEASE_VERSION:-}" =~ ^v${number}\.${number}\.${number}(-(dev|rc)\.${number})?$ ]] \
    || fail "release version must be vMAJOR.MINOR.PATCH, optionally with -dev.N or -rc.N"
}

validate_identity() {
  validate_version
  [[ "${SOURCE_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail "missing or invalid release source SHA"
  case "${RELEASE_CHANNEL:-}:$RELEASE_VERSION" in
    dev:*-dev.*|rc:*-rc.*) ;;
    stable:*-*) fail "stable release requires a final version" ;;
    stable:*) ;;
    operational:*-*) fail "operational candidate requires a final version" ;;
    operational:*)
      [[ "${APPROVAL_REF:-}" =~ ^[A-Za-z0-9][A-Za-z0-9._:/#-]{0,199}$ ]] \
        || fail "operational candidate requires a valid approval reference"
      ;;
    *) fail "release channel does not match the version" ;;
  esac
}

resolve_tag() {
  local ref="refs/tags/$RELEASE_VERSION" matches object peeled
  matches="$(git -c credential.helper= -c 'credential.helper=!gh auth git-credential' \
    ls-remote --exit-code "$REPOSITORY_URL" "$ref" "$ref^{}")" || fail "release tag is missing"
  object="$(awk -v ref="$ref" '$2 == ref { print $1 }' <<<"$matches")"
  peeled="$(awk -v ref="$ref^{}" '$2 == ref { print $1 }' <<<"$matches")"
  [[ "$object" =~ ^[0-9a-f]{40}$ && "${peeled:-$object}" =~ ^[0-9a-f]{40}$ ]] \
    || fail "release tag has invalid object identity"
  printf '%s %s\n' "$object" "${peeled:-$object}"
}

classify() {
  validate_version
  local tag_object="" tag_identity
  case "${RUNNER_MODE:-hosted}" in
    hosted) ;;
    utm-ci)
      [[ "${EVENT:-}" == workflow_dispatch && "${CANDIDATE_CHANNEL:-}" == operational &&
         "${REPOSITORY_VISIBILITY:-}" == private ]] || fail "UTM runner requires a private operational candidate dispatch"
      ;;
    *) fail "unknown runner mode" ;;
  esac
  case "${CANDIDATE_CHANNEL:-}" in
    ""|tagged|operational) ;;
    *) fail "unknown dispatch channel" ;;
  esac
  if [[ "${CANDIDATE_CHANNEL:-}" == operational ]]; then
    [[ "$EVENT" == workflow_dispatch ]] || fail "operational candidate requires manual dispatch"
    case "$REPOSITORY_VISIBILITY" in
      private|internal) ;;
      *) fail "operational candidates require a private or internal repository" ;;
    esac
    [[ "$WORKFLOW_REF" == "refs/heads/$DEFAULT_BRANCH" ]] \
      || fail "operational candidates require the default branch workflow"
    RELEASE_CHANNEL=operational
  else
    [[ -z "${SOURCE_SHA:-}${APPROVAL_REF:-}" ]] \
      || fail "candidate inputs require the operational channel"
    case "$RELEASE_VERSION" in
      *-dev.*) RELEASE_CHANNEL=dev ;;
      *-rc.*) RELEASE_CHANNEL=rc ;;
      *) RELEASE_CHANNEL=stable ;;
    esac
    tag_identity="$(resolve_tag)"
    read -r tag_object SOURCE_SHA <<<"$tag_identity"
    if [[ "$EVENT" == push && "$SOURCE_SHA" != "$EVENT_SHA" && "$tag_object" != "$EVENT_SHA" ]]; then
      fail "release tag no longer points to the pushed commit"
    fi
  fi
  validate_identity
  {
    printf 'version=%s\n' "$RELEASE_VERSION"
    printf 'source_sha=%s\n' "$SOURCE_SHA"
    printf 'channel=%s\n' "$RELEASE_CHANNEL"
    printf 'tag_object=%s\n' "$tag_object"
    printf 'approval_ref=%s\n' "${APPROVAL_REF:-}"
  } >> "$GITHUB_OUTPUT"
}

bind_source() {
  validate_identity
  [[ "$(git rev-parse HEAD)" == "$SOURCE_SHA" ]] || fail "checkout does not match release source SHA"
  if [[ "$RELEASE_CHANNEL" != operational ]]; then
    [[ "${TAG_OBJECT:-}" =~ ^[0-9a-f]{40}$ ]] || fail "missing or invalid tag object"
    [[ "$(resolve_tag)" == "$TAG_OBJECT $SOURCE_SHA" ]] || fail "release tag moved since classification"
  fi
}

verify_binary() {
  validate_identity
  local arch="$1" cli="dist/kc-linux-$1" gateway="dist/kc-gateway-linux-$1"
  local expected="$RELEASE_VERSION ($SOURCE_SHA)"
  chmod +x "$cli" "$gateway"
  local actual
  actual="$("$cli" --version)" || fail "CLI version command failed"
  [[ "$actual" == "kc version $expected" ]] || fail "unexpected CLI version or commit"
  actual="$("$gateway" --version)" || fail "gateway version command failed"
  [[ "$actual" == "$expected" ]] || fail "unexpected gateway version or commit"
  if [[ "$arch" == amd64 ]]; then
    chmod +x dist/keycli-linux-amd64
    actual="$(dist/keycli-linux-amd64 --version)" || fail "legacy CLI version command failed"
    [[ "$actual" == "kc version $expected" ]] || fail "unexpected legacy CLI identity"
  fi
  bash "$(dirname "$0")/verify-mcp-binary.sh" "$cli" "kc version $expected"

  local channel="$RELEASE_CHANNEL" enabled=false
  if [[ "$channel" == operational ]]; then enabled=true; fi
  check_lock() {
    python3 -c 'import json, sys

def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result
try:
    report = json.load(sys.stdin, object_pairs_hook=unique_object)
except (ValueError, TypeError):
    sys.exit(1)
enabled = sys.argv[2] == "true"
valid = (isinstance(report, dict) and type(report.get("schema_version")) is int
         and report.get("schema_version") == 1 and report.get("channel") == sys.argv[1]
         and report.get("requires_env") is True and report.get("unlocked") is enabled
         and report.get("provider_enabled") is enabled)
sys.exit(0 if valid else 1)' "$channel" "$1"
  }
  env -u KC_PROVIDER_EFFECTS "$gateway" -lock-report | check_lock false
  KC_PROVIDER_EFFECTS=enabled "$gateway" -lock-report | check_lock "$enabled"
  # Keep an operational binary locked for the live-path denial probe.
  local probe_env=enabled
  if [[ "$enabled" == true ]]; then probe_env=""; fi
  (
    work="$(mktemp -d)"
    trap 'rm -rf "$work"' EXIT
    if KC_PROVIDER_EFFECTS="$probe_env" "$gateway" -provider live -journal "$work/journal" 2>"$work/live.err"; then
      fail "locked gateway accepted a live provider request"
    fi
    grep -Fq "provider effects are locked" "$work/live.err" || fail "gateway did not report the provider lock"
    [[ ! -e "$work/journal" ]] || fail "locked gateway opened the journal"
  )
}

case "${1:-}" in
  classify) classify ;;
  bind) bind_source ;;
  provenance)
    validate_identity
    jq -n --arg version "$RELEASE_VERSION" --arg source_sha "$SOURCE_SHA" \
      --arg channel "$RELEASE_CHANNEL" --arg approval_ref "${APPROVAL_REF:-}" \
      --arg tag_object "${TAG_OBJECT:-}" \
      --arg runner_mode "${RUNNER_MODE:-hosted}" \
      --arg repository "$GITHUB_REPOSITORY" --arg run_id "$GITHUB_RUN_ID" \
      --arg run_attempt "${BUILD_RUN_ATTEMPT:-$GITHUB_RUN_ATTEMPT}" --arg workflow_sha "$WORKFLOW_SHA" \
      '{schema_version: 1, $version, $source_sha, $channel, $approval_ref,
        $repository, $run_id, $run_attempt, $workflow_sha, $tag_object, $runner_mode}'
    ;;
  verify-binary) verify_binary "$2" ;;
  verify-bundle)
    [[ "${MANIFEST_SHA:-}" =~ ^[0-9a-f]{64}$ ]] || fail "missing artifact manifest digest"
    bash "$0" provenance | cmp - dist/provenance.json || fail "artifact provenance mismatch"
    (
      cd dist
      assets=(kc-darwin-arm64 kc-darwin-amd64 kc-linux-arm64 kc-linux-amd64
              kc-gateway-linux-amd64 kc-gateway-linux-arm64 keycli-linux-amd64 provenance.json)
      expected="$(printf '%s\n' "${assets[@]}" SHA256SUMS | LC_ALL=C sort)"
      actual="$(find . -mindepth 1 -maxdepth 1 -print | sed 's|^./||' | LC_ALL=C sort)"
      [[ "$actual" == "$expected" ]] || fail "unexpected artifact file set"
      sha256sum "${assets[@]}" | cmp - SHA256SUMS || fail "artifact file checksum mismatch"
      [[ "$(sha256sum SHA256SUMS | cut -d ' ' -f 1)" == "$MANIFEST_SHA" ]] \
        || fail "manifest does not match uploaded build"
    )
    ;;
  *) fail "unknown release contract operation" ;;
esac
