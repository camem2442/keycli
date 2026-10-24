#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

suite="all"
if [[ "${1:-}" == "--suite" ]]; then
  suite="${2:?usage: verify.sh --suite <name>}"
fi

run_gofmt() {
  unfmt="$(gofmt -l .)"
  if [[ -n "$unfmt" ]]; then
    echo "gofmt needed on:" >&2
    echo "$unfmt" >&2
    exit 1
  fi
}

run_vet() {
  go vet ./...
}

run_vet_darwin() {
  GOOS=darwin GOARCH=arm64 go vet ./...
  GOOS=darwin GOARCH=amd64 go vet ./...
}

run_unit() {
  go test -tags=kc_testhooks ./...
}

run_backend() {
  go test -tags=kc_testhooks ./internal/backend/... ./internal/resolve/...
}

run_keychain() {
  go test -tags=kc_testhooks ./internal/backend/keychain/...
}

run_clip() {
  go test -tags=kc_testhooks ./internal/clip/...
}

run_integration() {
  go test -tags=kc_testhooks ./internal/integration/...
}

run_docs() {
  for f in README.md HANDOFF.md AGENTS.md docs/contracts/*.md; do
    [[ -f "$f" ]] || { echo "missing $f" >&2; exit 1; }
  done
}

run_policy() {
  if rg -n 'AGE-SECRET-KEY-|ghp_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{10,}' --glob '!verify.sh' .; then
    echo "possible secret pattern in tree" >&2
    exit 1
  fi
}

run_release() {
  bash "$ROOT/scripts/lifecycle.sh" release
}

run_mac_smoke() {
  bash "$ROOT/scripts/mac-smoke.sh"
}

case "$suite" in
  all)
    run_docs
    run_policy
    run_gofmt
    run_vet
    run_vet_darwin
    run_unit
    run_release
    run_mac_smoke
    ;;
  docs) run_docs ;;
  policy) run_policy ;;
  unit)
    run_gofmt
    run_vet
    run_unit
    ;;
  backend)
    run_gofmt
    run_vet
    run_backend
    ;;
  keychain)
    run_keychain
    ;;
  clip)
    run_clip
    ;;
  integration)
    run_integration
    ;;
  release)
    run_release
    ;;
  mac-smoke)
    run_mac_smoke
    ;;
  *)
    echo "unknown suite: $suite" >&2
    exit 1
    ;;
esac

echo "verify.sh: suite=$suite ok"
