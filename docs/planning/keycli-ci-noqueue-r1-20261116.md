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
