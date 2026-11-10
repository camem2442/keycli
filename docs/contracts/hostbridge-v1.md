---
status: partial
owner: delivery
---

# Host bridge — provider effect admission (v1)

`internal/hostbridge` gates every `providerlifecycle.HostAuthorize` callback on a
trusted host decision. It exists because the callback type alone proves nothing: a
caller can pass any closure, and the callback arguments arrive from code paths that
model input can influence.

Server-side evaluation is owned by `workspace-infra`
(`docs/contracts/keycli-host-admission-v1.md`, WI-KC-02). This package is the
KeyCLI-side enforcement point and performs no I/O.

## Flow

```
providerlifecycle.Client.Create…(ctx, gate, …)
        │
        ▼
hostbridge.Bridge.Authorize(ctx, provider, operation, target)
        │  1. classify the operation from a fixed table (unknown → refuse)
        │  2. build Request from host-owned Identity + actual arguments
        │  3. Admitter.Admit(Request)          ← host process, outside model control
        │  4. re-check every bound field, expiry and mutation class
        ▼
nil  → the provider call proceeds
err  → the provider call is refused before any effect
```

## Rules

| Rule | Behaviour |
|---|---|
| Fixed operation table | only `cloudflare/create-account-token`, `cloudflare/delete-exact-account-token`, `github/delete-exact-repository-secret`, `oracle/stage-pending-vault-version`, `oracle/schedule-deprecated-vault-version-deletion`; anything else → `ErrUnknownOperation`, admitter not called |
| Host-owned identity | `GoalID`, `Repository`, `HeadSHA`, `BaseSHA`, `OperationID`, `WriterGeneration`, `HostIdentity`, `Paths`, `BudgetCost`, isolation fields come from `Bridge.Identity`, never from CLI/model text |
| Binding | the decision's `Bound` request must match every field; `TargetDigest` is derived from the real `provider/operation/target` tuple |
| Freshness | `ExpiresAt` must be after now and at most `MaxAdmissionLifetime` (10m) ahead |
| Authority | `Status == ADMITTED`, `AuthorizesExecution`, non-empty `GrantID`, mutation class matching the table |
| No authority creation | the bridge creates no Grant, consumes no budget and writes no journal record |

## Errors

`ErrNoAdmitter`, `ErrUnknownOperation`, `ErrAdmissionRejected`, `ErrAdmissionInvalid`,
`ErrBindingMismatch`, `ErrNilBridge`. All are fail-closed: the caller must treat any
error as "do not execute".

## Not wired yet

`providerlifecycle` still accepts any `HostAuthorize` value. A host that wants the
bridge passes `bridge.HostAuthorize()`; no existing call site was changed in this
source candidate, so current behaviour is unchanged until a host wires it.

## Limits

- A `Decision` is trusted only because it came from an in-process `Admitter`.
  Decisions are never parsed from files, CLI flags or model output.
- Same-UID credential access defeats admission. The workspace-infra evaluator blocks
  mutating actions declared as `same-uid-direct-access`; operator acceptance of a
  real host remains `NOT_VERIFIED`.
- Admission decides authorisation, not provider truth. `ACKNOWLEDGED` is not value
  equality, and reconciliation stays with the delivery journal.

## Verification

```bash
go test ./internal/hostbridge/     # 15 tests, no network, no keychain
```

Covered: nil bridge/admitter, unknown operation, admitted mutation request shape,
blocked decision, admitter error, decisions bound to another target/operation/HEAD,
expired and over-long decisions, mutation-class and grant-binding mismatch,
incomplete identity, cancelled context, operation table, providerlifecycle type
compatibility, digest determinism.
