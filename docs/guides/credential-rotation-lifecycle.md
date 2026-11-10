---
status: NOT VERIFIED
owner: keycli
date: 2026-11-10
scope: source-only host callback interfaces
---

# K10-02~04 static service key lifecycle (stacked Draft)

This guide describes source candidates stacked after K10-01 [PR #49](https://github.com/camem2442/keycli/pull/49). The new functions are **trusted-host Go APIs only**. There is **no public `kc rotate` CLI**, no provider key issuance, token revocation, automatic promotion, real WIF migration or live Oracle/Cloudflare/GitHub mutation.

## Stages

- K10-02 `NewPreparationPlan` and `Prepare`: immutable metadata-only source/target generation and required consumer list; host admission callback then a **single** provider-issued/staged generation callback inside existing `journal.Store.Dispatch`. The journal may report `UNKNOWN_EFFECT`; no automatic replay.
- K10-03 `Attest`: independent host verifiers must authenticate original stage/promotion records and *every* mandatory consumer’s timely new-generation evidence. It can return `READY_FOR_SEPARATE_REVOKER`, never revocation authority. No self-reported `TrustedByHost` boolean from JSON can bypass the callback.
- K10-04 `Finalize`: re-runs the above verifiers against current evidence, requires a **new, separately privileged revocation Grant** pinned to the exact evidence digest and exact old provider credential handle, then journals **one** callback invocation. Success is `ACKNOWLEDGED`, not independent proof that the provider actually disabled the credential. Uncertain results stay `UNKNOWN_EFFECT`.

## Security boundaries

1. Static service keys only. GitHub ephemeral job tokens, OIDC and OpenAI WIF short-lived tokens are excluded; identity provider / service account rotation has a different contract.
2. Do not put raw credentials, account vault snapshots, environment dumps or secret-derived hashes in plan, journal, tests, stdout, PR or Goal artifacts.
3. The pure `EvaluateRetirement` candidate in #49 is advisory and user-controlled inputs are untrusted. Real trust requires the owning workspace-infra Goal/Claim and authenticated host result; merely registering a closure callback does not create authority.
4. Exactly one provider target + old generation per revocation request; previous generation must remain valid throughout migration and rollback grace. No global sweep, no `Roll` API by default, no deleting a worker/service account on the basis of name-only metadata.
5. `journal.Store` locks a **single private local directory**, not distributed hosts. The true cross-host Writer Claim, token issuer/revoker scope and duplicate suppression are supplied by workspace-infra/provider adapters.
6. `Finalize` rejects synthetic future clock claims (current wall clock ±30s) and requires fresh consumer observations, exact source/target, no unresolved effect, and high-risk grant binding to current evidence. Neither an app-specific revoker nor actual secret-identity CAS is implemented here.
7. The separate host's `Prepare` and `Finalize` effect callbacks MUST verify provider-specific immutable IDs immediately before issuance/revocation. Without provider verification/receipt, operating acceptance is **NOT VERIFIED**.

## Verify

Local isolated Linux amd64 Go 1.23.2 `go test -race -count=1 ./internal/rotation` passed with compile-time journal package stand-in. Each stack step K10-02 / K10-03 / K10-04 was independently compiled/tested on a cumulative source snapshot. Real KeyCLI Go 1.22.12 module and exact-head GitHub CI **NOT VERIFIED**. Required next gates: full repository source/CI, authenticated Goal host admission, safe disposable provider acceptance, and separate Solo Maintainer merge/release authorization.
