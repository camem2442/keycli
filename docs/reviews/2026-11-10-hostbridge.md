---
status: historical
owner: delivery
---

# WI-KC-02 KeyCLI side — host bridge (2026-11-10)

Branch: `opencode/kc-hostbridge-20261110`, based on the source landing payload
`80bdd82b` (Tree `ee735340`) because the bridge depends on the landed
`providerlifecycle.HostAuthorize` type.

## Added

| Path | Purpose |
|---|---|
| `internal/hostbridge/contract.go` | request/decision types, fixed operation table, target/operation/request digests |
| `internal/hostbridge/bridge.go` | `Authorize` gating + `HostAuthorize()` adapter |
| `internal/hostbridge/bridge_test.go` | 15 tests |
| `docs/contracts/hostbridge-v1.md` | contract, rules, limits |

No existing file was modified. `providerlifecycle` still accepts any
`HostAuthorize` value; a host opts in by passing `bridge.HostAuthorize()`.

## What it prevents

| Attack | Result |
|---|---|
| missing/forged callback | `ErrNoAdmitter`, or binding mismatch when a decision admits another request |
| unlisted provider operation | `ErrUnknownOperation` before any admission call |
| decision bound to another target / operation / HEAD | `ErrBindingMismatch` |
| expired or over-long decision (> 10m) | `ErrAdmissionInvalid` |
| mutation admitted as read-only (or vice versa) | `ErrAdmissionInvalid` |
| decision without a Grant binding | `ErrAdmissionInvalid` |
| incomplete host identity, HEAD == base, bad host label | `ErrAdmissionInvalid`, admitter not called |
| cancelled context | `ErrAdmissionInvalid`, admitter not called |

## Verification

| Command | Result |
|---|---|
| `gofmt -l internal/hostbridge` | clean |
| `go vet ./internal/hostbridge/` | ok |
| `go test -count=1 ./internal/hostbridge/` | 15 tests ok |
| `go test -tags=kc_testhooks ./...` (Go 1.22.12) | 37 패키지 ok, `internal/cursor` 1건 실패 |
| `verify.sh --suite docs` / `--suite policy` | ok |

`internal/cursor.TestWireCursorVertex` fails because a real Cursor process is
running (`Cursor is running: refusing to modify its state database while active`).
The same failure reproduces at `main 4c972d98`; it is an environment condition, not
a regression from this branch.

## Limits

- Decision authenticity depends on the `Admitter` being in-process host code. The
  package never parses a decision from a file, flag or model output.
- No real host wiring, no provider call, no credential access.
- `TRUSTED_HOST` remains `SOURCE_PARTIAL` until the workspace-infra evaluator runs
  in a process that actually holds the provider credentials.
