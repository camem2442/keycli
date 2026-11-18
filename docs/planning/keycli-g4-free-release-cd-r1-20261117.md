---
status: proposal
owner: keycli
updated: 2026-11-17
scope: G4 release source safety and future free isolated publication, not operational grant
---

# KeyCLI G4 — zero automatic hosted release jobs, guarded free producer

This is a **stacked** proposal on [KeyCLI G2 noqueue PR #73](https://github.com/camem2442/keycli/pull/73), not a replacement for its unmerged one-shot VM admission. Do **not** retarget or merge into main before #73 and a separately reviewed G4 release-path producer are accepted.

## Current G4 source-only HOLD

Before this candidate, release.yml ran on version-tag pushes and could schedule GitHub-hosted x64/arm64 and publication jobs. The source change removes tag-triggered execution and all hosted runs-on routes. All six jobs target only the future **independently registered one-shot** self-hosted Linux ARM64 runner label keycli-ephemeral-vm.

**The classify job is unconditionally disabled (if: false)**. Every downstream release job depends on classify, so neither a tag push nor manual workflow_dispatch can allocate a runner, build, create a GitHub Release, attest a binary or publish assets. This is a consciously non-operational bridge: it prevents accidental billed-release activity while the actual ephemeral producer and stable acceptance are missing. A skipped job is **not** a verified release.

The older release-contract.sh remains unchanged; it still rejects tagged dev/rc on the public UTM route and requires private operational evidence. That mismatch is deliberately preserved as a **future blocker**, not bypassed with a new run mode or fabricated approval. The original stable release guard still refuses stable publication without PR-14 operational acceptance and explicit release publication approval.

The updated scripts/verify-release-contract.py source test asserts no push trigger, no hosted runners, six isolated runner assignments, hard disabled classify dependency, and unchanged stable-release guard. No real Go/native release test or runner registration was performed by this PR.

## Next implementation sequence and actual gates

1. **G2 first:** accept [#73](https://github.com/camem2442/keycli/pull/73) only after its original exact-head source evidence, actual independent ephemeral VM admission, and no-paid-hosted G2 source contract. Its current test.yml is still source-hold, not an accepted builder.
2. **G4-A source admission:** choose a trusted ephemeral isolated builder that accepts reviewed public source without persistent self-hosted token or provider secrets. Prove teardown and runner registration lifecycle, per-run capability and cost, separate from rooted Flip3. Required matrix: Darwin native smoke separately, Linux ARM64 actual execution and independent AMD64 or explicit emulation label with limited claim.
3. **G4-B release control:** remove classify if:false **only in a separately reviewed PR** after a real eligible builder is present; replace obsolete RUNNER_MODE and public tagged-source restriction with a narrowly scoped manual + reviewed SHA + immutable tag contract. Keep producer/privileged GitHub publication credentials separated where feasible. Inspect each workflow runner before checkout; no hosted fallback. Rework build-once artifact identity, original SHA and manifest, fresh private/public attestation availability and release API protection.
4. **G4-C dev/rc pilot:** manually publish one non-stable immutable prerelease only after both platform execution gates and exact verified release intent. Record GitHub Release ID/asset IDs, manifest, producer receipt, original native evidence; do not update personal installed kc or provider configurations.
5. **G4-D stable automation:** a trusted reconciler may request publication only when exact source, PR-14 and stable publication authorization are independently proven at decision time. A tag or source CI status alone is insufficient. Require change-free source and all original required checks; use a signed, bounded approval intent. Only after this gate can stable release automation be considered live.

## Safety and costs

Never use a standing Flip3 CI runner as the public-repository release publisher; PRoot is not an independent isolation boundary. Never assume a free VM/runner is online, or that self-hosted billing is literally zero (host/network/electricity remain outside GitHub-hosted minutes). No automatic fallback. Do not create/overwrite tags or Releases, touch Keychain/provider credentials, remove rollback binaries, or activate a service in this source-only phase. Any attempt to weaken the stable guard, reenable auto-tag publication, enable a writable release token on a shared host or merge G4 before original owning gates is HOLD.

Cross-repo future consumer: [workspace-infra Release CD R1](https://github.com/camem2442/workspace-infra/pull/531) produces unapproved desired-state plans, not release authority.

Original references: [release contract](../contracts/release-workflow-v1.md); [GitHub self-hosted security](https://docs.github.com/en/actions/reference/security/secure-use); [GitHub workflows syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#concurrency).
