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
| `internal/hostbridge` | fixed local Authority socket, with an exact bound intent and a final execution check |
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

Replay protection is durable: `MutationID` is the journal operation id. A
replay matching the recorded intent returns `DUPLICATE` with zero provider calls.
Reusing the same id with a different intent returns HTTP 409
(`MUTATION_CONFLICT`). `ExpiresAt` bounds the window in which a captured
envelope could be attempted at all.

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
| duplicate operation id | recorded intent matches | `DUPLICATE` (0 calls) |
| conflicting operation id | recorded intent differs | `MUTATION_CONFLICT` (0 calls) |
| pending operation on same target | unresolved intent owns the target | `ErrTargetPending` |

`UNKNOWN_EFFECT` is never settled by the gateway. Legacy journal `Reconcile`
can close version 1 records with an operation-bound proof. Authority-bound
records require a future reconciliation path that verifies the original binding.

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

## Configuring the local Authority client

Start `kc-gateway` with `-authority-config /absolute/path/authority.json` and
its existing claim, session, journal, and ledger flags. This selects local
Authority admission for every submission. An explicitly empty or invalid
configuration aborts startup before key issuance. The legacy invocation
without this flag still runs the synthetic ledger harness. The live adapter
remains unavailable, and the Authority-configured invocation refuses the
synthetic reconcile and lost-response flags.

The host supervisor creates the configuration from trusted registration data.
Use a regular, single-link file with mode `0600` owned by the gateway's
effective UID. The file is limited to 64 KiB and is read once at startup;
restart to change a registration or the endpoint. All path components must
be absolute, clean and free of symlinks. Ancestors must belong to root, the
gateway UID, or (for the socket) the pinned service UID. Shared writable
ancestors require the sticky bit; the immediate parent cannot be shared
writable. On macOS the config file and directory checks also reject extended
ACL entries that can grant access. A socket that already exists at startup
must be an owner-only Unix socket owned by the pinned UID. Each exchange
checks the socket and authenticates the connected kernel peer independently.

The JSON document contains exactly `v` (integer `1`), `socket_path`,
`socket_uid` (numeric), and a nonempty `registrations` array. Each entry
contains exactly these fields:

| Fields | Meaning |
|---|---|
| `goal_id`, `mutation_id` | Exact registration lookup key; duplicates are rejected |
| `operation`, `writer_generation`, `owner`, `session` | Registered operation and the writer claim bound to this mutation |
| `provider`, `provider_operation`, `canonical_target` | Host-owned inputs used to compute the writer's target digest |
| `resource_digest`, `plan_digest`, `grant_digest` | Host-supplied 64-character lowercase hex bindings |

A writer cannot supply the socket, registry, or raw canonical target. Both
Authority calls use the same per-submission binding. The Go client sends
`preview_admission` before journal dispatch. By default it sends
`preview_execution` inside the journal callback. These v1 methods evaluate authority without reserving
a mutation, spending budget, or dispatching an effect. The client requires
a server supporting the preview methods and does not fall back to the
mutating `admit` or `verify_execution` methods.

V1 advisory replies carry `authorizes_execution: false`. The gateway
records a local no-effect result when an advisory reply is the final
execution check. Provider execution requires a shared journal owner and
an execution handoff that remains fenced through dispatch and settlement.

The Go journal uses record version 2 for these submissions. The
`authority_binding` object in the durable intent and dispatch marker is the
same metadata projection sent to Authority: goal, writer identity, provider,
registered operation, mutation id, and resource, target, plan, and grant
digests. Reads verify that the mutation id and overlapping digests match the
outer intent. An exact replay must match the whole binding. Version 1 records
remain readable and do not provide an Authority binding.

The opt-in cross-language test
`TestGatewayPythonTakeoverWaitsForJournal` runs the real Go journal and the
Python takeover coordinator with a synthetic provider. The fixture checks
that takeover waits on the Go journal's existing lock while the effect is
running. It reads the persisted acknowledgement before advancing the claim.
`TestGatewayPythonTakeoverAfterProviderCrash` kills the synthetic
Gateway while its callback holds the same lock. It checks that the
durable dispatch remains unresolved, replay and same-target effects
stay fenced, and takeover cannot activate the successor without an
independent observation. Set `KEYCLI_TEST_WORKSPACE_INFRA` to the
workspace-infra preview checkout to run both lock tests and the
Go/Python Unix preview regression.

### Guarded execution IPC

A trusted host can explicitly compose `LocalAuthority.Execution` with
`hostbridge.NewGuardedExecution` using a fixed filesystem Unix endpoint and
the verifier's kernel UID and PID. The startup registration loader
continues to configure the v1 advisory client. A configured guarded exchange
requires the version 2 method and has no fallback to a preview or legacy
mutating method.

The guarded client sends one EOF-framed JSON object and one `SCM_RIGHTS`
descriptor, borrowed from the already-held Go dispatch lock. The object has
`v: 2`, `method: "verify_guarded_execution"`, a fresh 32-character lowercase
hex `request_id`, the exact `authority_binding` fields flattened into the
object, and `expires_at` in UTC RFC3339 with optional fractional seconds.
The expiry is the earlier of the signed Gateway request and the v1
admission advisory. The client limits the exchange to three seconds, the
caller's remaining deadline, and the remaining admission lifetime.

The verifier must authenticate the Gateway, verify the received descriptor
shares the held open file description, and check the current durable v2
dispatch and exact Authority reservation under that guard. Claim transitions
for participating goals must use the same guard or the journal takeover path.
The verifier closes its duplicate before returning a verdict and never
unlocks the shared open file description. It sends one EOF-framed object with
exactly `request` and `verdict`. The request object echoes every request
field and its value, including the nonce, binding and expiry. Duplicate
and additional fields are invalid. Each frame is limited to 64 KiB.

- `AUTHORIZED` attests that the verifier claimed the exact reservation under
  the guard. Only the Gateway invokes the provider, after it rechecks the
  claim, signing key and expiry. If that recheck fails, the Gateway can
  prove zero provider calls, while the claimed Authority reservation remains
  for coordinated settlement.
- `DENIED` attests that the verifier made no claim or handoff for this
  request. A valid denial or a failure before the first send attempt
  permits a local no-effect record.
- Any send failure after an attempt, lost or invalid reply, or deadline
  expiry before a valid verdict leaves the dispatch at `UNKNOWN_EFFECT`
  with zero provider calls. The unresolved operation remains fenced for
  reconciliation. A verifier with an uncertain outcome must never send
  `DENIED`.

`TestGatewayBoundPythonExecution` composes the normal Go dispatch path with
a synthetic Python verifier and provider callback. It exercises the live
Unix descriptor transfer, kernel peer credentials, reservation and journal
validation, and journal settlement through the provider callback. Revocation,
binding mismatch, separately opened descriptors, peer mismatch, and invalid,
lost or stalled replies are covered. Set `KEYCLI_TEST_WORKSPACE_INFRA` to the
workspace-infra checkout containing the Python mutation coordinator fixtures.
