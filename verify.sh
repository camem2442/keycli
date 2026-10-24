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

run_unit() {
  go test ./...
}

run_backend() {
  go test ./internal/backend/... ./internal/resolve/...
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
  export CGO_ENABLED=0
  mkdir -p "$ROOT/dist"
  GOOS=darwin GOARCH=arm64 go build -o "$ROOT/dist/kc-darwin-arm64" ./cmd/kc
  GOOS=linux GOARCH=amd64 go build -o "$ROOT/dist/kc-linux-amd64" ./cmd/kc
  GOOS=linux GOARCH=amd64 go build -o "$ROOT/dist/keycli-linux-amd64" ./cmd/keycli
}

case "$suite" in
  all)
    run_docs
    run_policy
    run_gofmt
    run_vet
    run_unit
    run_release
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
  release)
    run_release
    ;;
  *)
    echo "unknown suite: $suite" >&2
    exit 1
    ;;
esac

echo "verify.sh: suite=$suite ok"
