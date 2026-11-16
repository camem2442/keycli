---
status: BLOCKED
owner: keycli
date: 2026-11-16
scope: G2 source-only candidate; no runner dispatch
---

# G2 No-queue workflow after KeyCLI v0.10 integration

This source change supersedes the old automatic-run approaches in historical KeyCLI PR #70 and #71; their branches and reviews remain intact. It does not alter the already-proposed private source-fingerprint gate in workspace-infra PR #387, which is still pinned to the old KeyCLI PR #70 revision.

PR and main push events have only one job; its explicit event predicate is workflow_dispatch, so automatic events SKIP with zero runner allocation and zero paid hosted minutes. Skipped is not a product PASS. Per-PR concurrency with cancel-in-progress false retains running jobs and replaces obsolete pending requests when manually dispatched.

The manual owner-only input requires a reviewed PR number and exact SHA, independent positive admission of a one-shot nonroot Linux ARM64 VM labeled keycli-ephemeral-vm, and an offline module cache. The workflow compares PR owner/head/base before and after the scoped docs, policy and unit Go suite. This does not run on the persistent Android Flip3, register runners, issue a token, invoke a provider, or change installed KeyCLI.

**DRAFT/HOLD:** The KeyCLI-scoped ephemeral VM is NOT VERIFIED or registered. This proposal also removes the previous automatic dual-architecture Linux release jobs; that release verification must be replaced by independently validated free-host G4 before Ready/merge. No source Go or actual VM run was executed here, and no Mac native, Keychain or real provider success is claimed. Workflow YAML and policy must be independently tested on the exact HEAD, with original suite receipts and effective branch checks. Automatic paid fallback remains forbidden.

Ownership stays split: KeyCLI product source gate; workspace-infra host/Goal/CI admission and original receipts; standard-vm ephemeral VM lifecycle and teardown.

## Static regression gate (candidate)

The docs owning suite now invokes six Python stdlib source-contract tests for the
manual-only event, isolated runner, exact PR identity, GitHub concurrency,
checkout credentials and offline no-duplicate suite. **These tests have not
been executed on the changed PR HEAD**; their source presence is not PASS.


## G4 release source candidate #74 (2026-11-17)

[KeyCLI PR #74](https://github.com/camem2442/keycli/pull/74) implements a **separate**, offline native Linux ARM64/AMD64 release-contract acceptance script using existing `verify.sh --suite all`, checksums, native binary smoke and a temporary development installation. Its seven source tests have run locally on exact Git blobs. It does **not** register an ephemeral runner, publish a release, validate both architectures in original hosts or replace the still-hosted `release.yml`.

Consequently, **#73 remains DRAFT/HOLD**. Before merging its removal of the old automatic release Jobs, the owner must separately accept the real arm64 + native x64 host receipts, protected no-paid workflow topology, and exact current reviewed source. Source tests for #73 6/6 and #74 7/7 do not satisfy these operating gates. Never switch the public KeyCLI repository onto the persistent Flip3 runner or treat a user-set admission checkbox as host proof.

## G2+G4 exact-source joint candidate (2026-11-17)

- The current integration source now includes the three unique G4 release-gate blobs from #74 **byte-for-byte**, while retaining #73's workflow and six G2 source contract tests. `verify.sh --suite docs` registers both the six G2 and seven G4 source-test scripts without replacing its original test gates.
- The source join has two Git parents (#73 G2 and #74 G4), making the old branches reviewable as historical ownership. Do **not** merge #74 separately after this exact branch's integration.
- Operating host/runner owner is [workspace-infra Goal #527](https://github.com/camem2442/workspace-infra/issues/527). The public KeyCLI repository must not enroll the resident Flip3 Android runner for public code. Prefer the private workspace-infra manual owner workflow with one-shot VM and separate original host receipts.
- The existing public workflow in this branch remains **manual-only** for source testing, and does not by itself prove independent VM admission. A user-set admission checkbox is not a host attestation. The new G4 shell requires one native ARM64 or AMD64 host at a time; two distinct original native receipts, release/publisher host plan, and current merge/ruleset review remain required.
- Current status: **DRAFT/HOLD**. Exact source/blobs do not equal a real KeyCLI release, tested VM, native Mac or production provider operation.
