---
status: historical
owner: delivery
---

# OCI instance principal → OpenAI WIF (source-only, 2026-11-10)

Branch: `opencode/kc-hostbridge-20261110` (based on the landing payload `80bdd82b`).
Baseline document consulted: OpenAI "Configuring workload identity federation for
Oracle Cloud Infrastructure".

## What was implemented

| Path | Purpose |
|---|---|
| `internal/identity/ociwif/ociwif.go` | claim verification, pinned endpoint, single-exchange acquire, opaque credential |
| `internal/identity/ociwif/ociwif_test.go` | 16 synthetic tests (OCI-01…OCI-15 + missing component) |
| `docs/contracts/oci-openai-wif-v1.md` | contract, flow, limits |

Existing code was checked first: `internal/identity/githubwif` already implements
GitHub Actions WIF, and this package mirrors its shape (`Signer`/`Exchanger`
host adapters, redacted response type, `Authorize` restricted to
`api.openai.com`). No GitHub WIF code was duplicated or modified, and no OCI WIF
implementation existed before.

## Design decisions

1. **No audience rewriting.** The OpenAI guide is explicit: use an audience that
   exists in the issued Oracle token. `ErrWrongAudience` rejects a mismatch instead
   of substituting `https://api.openai.com/v1`.
2. **Endpoint cannot be supplied by a caller.** `ExchangeRequest` has no endpoint
   field; `ValidateExchangeEndpoint` accepts only
   `https://auth.openai.com/oauth/token`.
3. **Mapping is checked before the provider call.** An empty identity provider or
   service account returns `ErrMappingMismatch` without emitting a request.
4. **One exchange per Acquire, no retry.** `ErrExchangeUnknown` is the terminal
   result; reconciliation belongs to the journal/admission layer (H22).
5. **Claims come from the host signer, not from decoding untrusted JWTs.** The
   package never parses a token it did not obtain through `Signer`.

## Verification

| Command | Result |
|---|---|
| `gofmt -l internal/identity/ociwif` | clean |
| `go vet ./internal/identity/ociwif/` | ok |
| `go test ./internal/identity/ociwif/` (Go 1.22.12) | 16 tests ok |
| `go test -tags=kc_testhooks ./...` | 38 packages ok, `internal/cursor` fails on a running-Cursor guard (pre-existing at `main`) |
| `verify.sh --suite docs` / `--suite policy` | ok |

## Verdict

```
OCI_WIF_SOURCE            = PASS (source-only)
OCI_INSTANCE_PRINCIPAL_LIVE = NOT_VERIFIED
OPENAI_WIF_LIVE           = NOT_VERIFIED
OCI_VM_EFFECTS            = 0
```

The host adapters (IMDS + OCI SDK signer, OpenAI exchange) are intentionally not
implemented here: they require a real tenancy and a real provider mapping, and
neither was authorized for this task.

## standard-vm boundary

`camem2442/standard-vm` #7 owns runner/VM lifecycle. This work creates no instance,
no VCN change and no runner registration. `G4 runner deregistered` and
`G6 OCI instance stopped` stay separate provider facts.
