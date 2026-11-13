---
status: partial
owner: keycli-delivery
date: 2026-11-13
---

# HA × KeyCLI R0 — signed intent binding

Source-only integration stacked on KeyCLI #66. It does NOT enable real
provider operations, install a binary, or change the released v0.8.0 CLI.

## Authority and identity

The signed gateway Request binds goal, mutation, provider, **concrete operation**,
action class, target digest, plan and grant digests, Writer generation, owner,
session and expiry. Its exact operation name cannot be changed after HMAC signing.

The trusted-host GatewayBinding must independently obtain immutable raw provider
resource identity (not a caller-supplied digest), original Plan/Grant values,
current repository HEAD and live Writer mutation lease. It verifies that the
HostBridge's own Goal, operation ID, generation and source are identical.
A mandatory CheckCurrent callback must re-read original authority and mutation
lease before dispatch and again just before a provider call. An inert callback
or a forged host proof is outside this source-only guarantee; operation
qualification requires a genuinely trusted host.

The HostBridge keeps the existing fixed operation/action table and original
Admitter; this package does not create new authority, ClaimStore, scheduler,
credential store, or provider. A missing callback or mismatch refuses the effect.

The Gateway defaults to real-provider mode locked by binary build identity
AND runtime environment. A trusted source-test harness may explicitly set
Synthetic=true with its own in-memory provider. This is an application-layer
gate; an adversarial same-UID actor who can construct a Go provider interface
is not isolated by this flag. Real provider adapters, OS isolation, and
credential host admission remain out of scope.

The effectclient CLI and synthetic Python S3 client include explicit
operation in their signed v0.10 Draft wire envelope. Refresh old S3 fixtures;
this does not change the released KeyCLI v0.8 CLI contract.

## Tests and next receipts

Go source command:
    go test -count=1 ./internal/hostbridge ./internal/effectgateway ./internal/delivery/journal

The hostbridge R0 test instantiates the real Gateway, real Bridge, normal
journal and a local synthetic provider. It checks one effect, duplicate
suppression, operation/action/target/plan/grant/owner/session/generation/HEAD
mismatch, required independent host callback, effect-boundary stale lease,
tampered HMAC and dev provider lock. This is not proof of an independent
production Grant verifier or cross-host identity until run and inspected
under an owner-admitted Linux/Flip3 host.

States remain separate: SOURCE_TEST_PASS, HOST_OBSERVED,
PRODUCT_VERIFIED, OPERATING_ACCEPTED. No status promotion by document.
