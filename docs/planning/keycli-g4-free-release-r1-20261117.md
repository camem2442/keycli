---
status: BLOCKED
owner: keycli
date: 2026-11-17
scope: G4 candidate; source checks only, no release or host admission
---

# G4 — Native free-host development release gate (R1)

KeyCLI G2 PR #73 and merged source integration #69 are predecessors. This PR supplies a standalone, offline release acceptance executable, not a GitHub workflow or release publication. KeyCLI owns the bundle/test code; workspace-infra owns owner-host approval/receipts and standard-vm owns VM lifecycle.

Run scripts/g4-free-release-gate.sh SOURCE_SHA arm64|amd64 only in a separately approved clean nonroot Linux VM whose native architecture matches the argument. The trusted host must independently attest disposable image, credential-free process environment, original run/source/workflow ID, offline Go cache, original logs, runner deletion and teardown. Environment variables in this script are not trusted signing proof.

The script refuses credentials and KC_PROVIDER_EFFECTS; checks exact clean source; runs the existing verify.sh --suite all once; creates an untagged synthetic v0.10.0-rc.999 bundle/provenance; uses existing release-contract.sh provenance, verify-bundle and native verify-binary; then runs verify-dev-release.sh from local assets in a temporary prefix, checking existing installed binaries unchanged. No real tag, publish, provider or installed app activation.

Two distinct native runs are required: Linux ARM64 and Linux AMD64. X86 emulation on ARM is **not** a native AMD64 pass. Verify original signed host/provenance receipt and same selected source SHA; do not silently compare unequal manifests, which include workflow run identity, as proof of equivalence.

The existing release.yml still contains GitHub-hosted verification and publishing. This PR does not modify release.yml, does not complete the G2 no-paid-hosted migration, and cannot alone make G2 PR #73 merge eligible. Protected publication and workflow-host topology are later, independent approval gates.

Source tests: 7 stdlib Python boundary checks under verify.sh --suite docs (bash syntax, invalid args, native host, source, provider lock, existing verifiers, provenance/offline modules). These tests were run in an isolated local container with seven passing; they are not the original VM/Go or GitHub PR HEAD test. Without both real architecture runs, native/host originals, review and current-source gates, keep Draft/HOLD. No CI dispatch, provisioning, tag or secret effects.

## 2026-11-17 native ARM64 vs AMD64 suite selection

The original verify.sh --suite all builds and **executes** a Linux AMD64 release smoke. On a native ARM64 VM this would require QEMU/binfmt and cannot be counted as native ARM64. Therefore the G4 wrapper invokes the full owning all suite on native AMD64 only. On native ARM64 it invokes existing docs, policy and unit suites, both Darwin cross-vet commands and the existing lifecycle.sh release build, then independently checks the **native ARM64** release binary, full bundle and isolated dev release. Both architectures must still pass original host source gates; a skipped AMD64 full run is not masked by ARM64 success. This corrects the source-only G4 proposal; neither native run has been executed.
