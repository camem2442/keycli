---
status: partial
owner: delivery
---

# Effect gateway (v1, single-process)

`internal/effectgateway` is the only component allowed to call a provider adapter.
It implements the boundary decided in
`workspace-infra/docs/contracts/effect-fencing-decision-v1.md` (option A).

It reuses, without rewriting:

| Existing piece | Use |
|---|---|
| `internal/delivery/journal` | durable intent, duplicate/target-pending latch, `UNKNOWN_EFFECT`, reconcile |
| `internal/hostbridge` | `Authorize(provider, operation, target)` admission before any effect |
| `goal_execution_store.SqliteClaimStore` (Python) | claim generation/session/lease — consumed through `ClaimAuthority`, not re-implemented |

## Request

Writers submit `Envelope{Request, Signature}`. The signature is HMAC-SHA256 over a
length-prefixed canonical field list, keyed by a **per-session key** the host
provisions at claim time (`KeyRing`). A `bound_executor` string is never treated as
authentication.

| Field | Rule |
|---|---|
| `GoalID`, `MutationID` | non-empty; mutation id follows the journal contract |
| `Provider`, `Action` | route to the host bridge; unknown operations are refused there |
| `TargetDigest`, `PlanDigest`, `GrantDigest` | 64 lowercase hex |
| `Generation` | positive integer, must equal the current claim |
| `Session`, `Owner` | must equal the current claim session/owner |
| `ExpiresAt` | required; must be in the future and at most 10 minutes ahead |

Replay protection is durable: `MutationID` is the journal operation id, so a
replayed envelope returns `DUPLICATE` with zero provider calls. `ExpiresAt` bounds
the window in which a captured envelope could be attempted at all.

## Order of checks

```
signature → expiry → claim snapshot → host admission → durable intent
        → effect-boundary claim re-check → provider call → settle / latch
```

Any failure before the provider call leaves the journal at `OBSERVED_NO_EFFECT`
with a gateway-local proof (it records "no provider call was made", not a provider
observation).

## State transitions

| Journal state | Meaning | Gateway receipt |
|---|---|---|
| `ACKNOWLEDGED` | provider acknowledged (not value equality) | `APPLIED` |
| `OBSERVED_NO_EFFECT` | no provider call (claim changed, or provider reported none) | `NO_EFFECT` |
| `UNKNOWN_EFFECT` | provider may have applied; result lost | `UNKNOWN_EFFECT` |
| duplicate operation id | already recorded | `DUPLICATE` (0 calls) |
| pending operation on same target | unresolved intent owns the target | `ErrTargetPending` |

`UNKNOWN_EFFECT` is never settled by the gateway. Only journal `Reconcile` with an
operation-bound proof can close it.

## Guarantees

| Guarantee | How |
|---|---|
| One effect per mutation id | journal `Dispatch` rejects duplicates before the effect callback |
| One in-flight operation per target per journal root | journal target-pending latch |
| Serialised execution | journal writer `flock` (non-blocking: callers get `ErrGatewayBusy`) |
| Stale writer cannot act | claim generation/session/owner compared twice (admission + effect boundary) |
| Fail-closed on authority failure | claim read error aborts before any journal write |
| No automatic retry | `UNKNOWN_EFFECT` is terminal for the gateway |

## Residual risk (explicit)

1. **Same-UID access.** The provider adapter is an in-process Go interface. A
   same-UID process can call it directly, bypassing the gateway. No OS boundary is
   claimed here.
2. **TOCTOU narrowed, not eliminated.** The claim is re-read immediately before the
   provider call, but it is still a separate read from the claim DB. A takeover in
   the microseconds between that read and the provider call is not intercepted.
   Closing it fully requires holding the claim transaction across the provider call
   (rejected: long DB transaction) or a provider-side fencing token (unavailable).
3. **No exactly-once.** Cloudflare, GitHub and OCI expose no idempotency key or
   fencing token. The gateway guarantees "at most once per mutation id" locally and
   leaves ambiguity as `UNKNOWN_EFFECT`.
4. **Cross-host.** This is a single-process gateway on one host. A second host with
   its own journal root is not fenced (`CROSS_HOST_EFFECT_FENCING=NOT_VERIFIED`).

## Not in scope

Provider adapters, transport, host IPC server, key provisioning, claim store
wiring. Those are host responsibilities; the gateway only defines the seam.
