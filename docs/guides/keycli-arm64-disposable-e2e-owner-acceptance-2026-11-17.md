---
status: native-hold
owner: keycli product / original workspace-infra Goal #545 and native Goal #527
date: 2026-11-17
scope: one native ARM64 development-only disposable experiment; no live kc, publisher, secrets or new runner
---

# KeyCLI ARM64 dev/rc disposable end-to-end: original host acceptance

## Goal and what this is NOT

Use **existing** product-owned `scripts/dev_cas_pilot.py` on [KeyCLI PR #76](https://github.com/camem2442/keycli/pull/76). Do not import a second CAS, M4.6's *keycli.gateway* selector, or a new Goal/Claim/CI scheduler. A successful result is **LOCAL_DEV_ARM64_DISPOSABLE_E2E**, only after independently observed *real* ARM64 execution/rollback and original provenance/host receipts. It is **not** GitHub Release publication, dual-arch G4 verification, live `~/.local/bin/kc` installation, security/provider authority or operating approval.

Owner tracking: [infra #545](https://github.com/camem2442/workspace-infra/issues/545) CD/KeyCLI handoff; [infra #527](https://github.com/camem2442/workspace-infra/issues/527) genuinely native isolated VM and producer; [infra #551](https://github.com/camem2442/workspace-infra/issues/551) retained 2 previous native ARM64 source runs but **no real previous-version/restore proof**. [#76 tests](../../scripts/tests/test-dev-cas-pilot.py) are disposable synthetic source fixtures, not original host receipts. [Full parent plan](https://github.com/camem2442/workspace-infra/blob/main/docs/planning/release-cd-keycli-arm64-dev-e2e-solo-loop-2026-11-17.md) defines E0–E6.

## Host entry / bounded preconditions

Only an *already admitted* one-shot, credential-free Ubuntu/Linux aarch64 host using [standard-vm #16](https://github.com/camem2442/standard-vm/pull/16) owner/Golden and a **fresh writable overlay** is eligible. Owner supplies original exact reviewed source HEAD/TREE, runner/request/run/attempt, offline toolchain and verified producer signature/manifest. Do not use a public-repo resident runner, rooted Flip3/PRoot or `qemu-aarch64` as independent native ARM64.

Require:

- Real **previous** `kc-linux-arm64` (tag `vX.Y.Z-dev.N` or `-rc.N`) and **new** `kc-linux-arm64` with *different verified SHA-256*, separate `SHA256SUMS`, signed producer/source evidence and reviewed source SHA for each. At least the candidate must have its matching `kc-gateway-linux-arm64` and checksum entry if provider-lock is required. No fixture-generated shell script or untrusted `latest` binary qualifies.
- Two already verified, private, same-UID-only readable local asset directories with correct filenames, owner and no symlink/hardlink; externally pin asset hashes and manifest hashes. The files stay in an isolated VM and never enter the user's live PATH.
- Native `uname -s == Linux`, `uname -m == aarch64`, nonroot host, no `KC_PROVIDER_EFFECTS`, `KC_KEY` or cloud secrets, reviewed exact checkout HEAD and clean tree. If any is absent, stop with `HOLD`, not PASS or automatic retry. The **host controller** independently asserts its own one-use Runner and VM stop/overlay removal.
- A private disposable `HOME` under OS temp. The live user's `~/.local/bin/kc`, macOS host services and Keychain must never be read or changed. Execute no untrusted binary solely because hashes match caller-provided strings.

**Pilot source commands** (only **after** original native host/producer admission; these commands are *not* executed by this documentation PR):

```bash
# The owning isolated VM provides OLD_DIR, NEW_DIR, OLD_TAG,
# NEW_TAG, OLD_SOURCE_SHA and NEW_SOURCE_SHA after independent trust checks.
# Never run on the owner's ordinary macOS HOME or a resident phone runner.
set -euo pipefail
test "$(uname -s)" = Linux
test "$(uname -m)" = aarch64
test "$(id -u)" -ne 0
test "$(git rev-parse HEAD)" = "$NEW_SOURCE_SHA"
test -z "$(git status --porcelain=v1)"
test -z "${KC_PROVIDER_EFFECTS:-}${KC_KEY:-}${GH_TOKEN:-}${GITHUB_TOKEN:-}${CLOUDFLARE_API_TOKEN:-}"

# Root is private and disposable; original old kc is copied only into temp HOME.
E2E_HOME="$(mktemp -d "${TMPDIR:-/tmp}/kc-e2e-home.XXXXXXXX")"
chmod 700 "$E2E_HOME"
mkdir -p "$E2E_HOME/.local/bin"
install -m 0500 "$OLD_DIR/kc-linux-arm64" "$E2E_HOME/.local/bin/kc"
test "$(sha256sum "$E2E_HOME/.local/bin/kc" | awk '{print $1}')" = "$OLD_SHA256"

# The real product script executes the new binary in its private PREFIX and
# tests its --version, MCP smoke, optional gateway provider lock and old bytes.
# Its final JSON summary PASS is INSUFFICIENT if a mandatory check was SKIP.
HOME="$E2E_HOME" bash scripts/verify-dev-release.sh \
  --tag "$NEW_TAG" --assets-dir "$NEW_DIR" --require-all-checks | tee "$E2E_HOME/native-checks.log"

# Independently examine exact lines for checksums, host_asset, installed_bytes,
# installed_version, mcp_smoke, provider_lock, existing_install_unchanged.
# Every mandatory row must contain PASS; SKIP or unknown is HOLD.
# The owner separately runs the CAS E2E (below) and retains original receipts.
```

## Reuse the existing product CAS, not another implementation

The qualified owner may invoke the **existing** product library with two original native asset pairs. This example only selects private **non-executable** content-addressed bytes; the executable smoke remains the product verifier above and host-independent runner. It is a proposed owner procedure, not a source acceptance receipt.

```python
# Requires OLD_DIR, NEW_DIR, OLD_TAG, NEW_TAG, OLD_SOURCE_SHA, NEW_SOURCE_SHA
# and a fresh isolated OS temp root. All input artifacts must be pre-approved.
import hashlib, importlib.util, os
from pathlib import Path
spec = importlib.util.spec_from_file_location("kc_cas", "scripts/dev_cas_pilot.py")
cas = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cas)
def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
def select_version(root, folder, tag, head, expected):
    asset = Path(folder) / "kc-linux-arm64"
    sums = Path(folder) / "SHA256SUMS"
    blob = digest(asset)
    cas.stage(root, asset, expected_sha256=blob, tag=tag, source_head=head,
              sums_path=sums, asset_name=asset.name,
              expected_sums_sha256=digest(sums), allow_disposable=True)
    return cas.select(root, artifact_sha256=blob, tag=tag, source_head=head,
                      expected_previous_pin_sha256=expected,
                      allow_disposable=True), blob
root = cas.init()["root"]
prior, old_sha = select_version(root, os.environ["OLD_DIR"],
                                os.environ["OLD_TAG"], os.environ["OLD_SOURCE_SHA"], None)
original_pin = prior["pin_sha256"]
changed, new_sha = select_version(root, os.environ["NEW_DIR"],
                                  os.environ["NEW_TAG"], os.environ["NEW_SOURCE_SHA"], original_pin)
assert old_sha != new_sha
assert cas.status(root)["selected"]["artifact_sha256"] == new_sha
restored = cas.rollback(root, receipt_id=changed["receipt_id"],
                        receipt_sha256=changed["receipt_sha256"], allow_disposable=True)
assert restored["pin_sha256"] == original_pin
assert cas.status(root)["selected"]["artifact_sha256"] == old_sha
try:
    cas.rollback(root, receipt_id=changed["receipt_id"],
                 receipt_sha256=changed["receipt_sha256"], allow_disposable=True)
except (cas.Hold, FileNotFoundError):
    pass  # Replay denied
else:
    raise AssertionError("ROLLBACK_REPLAY_ACCEPTED")
print("LOCAL_CAS_RESTORED_BYTES_NOT_OPERATING", root, old_sha, new_sha)
```

Strict mode rejects missing or skipped checks, but its JSON result still states `original_host_verified=false` and never grants a real deployment. The native owner must independently bind the actual unredacted product log to the admitted run/guest/producer.\n\nThe owner additionally repeats a real **old-version** `--version`/MCP smoke in disposable temp PREFIX after rollback and checks the unchanged old installed SHA in the private HOME. Verify `provider_lock` with the real gateway asset; a missing gateway cannot be promoted to PASS by the script's coarse summary. Stage-only mode 0400 is deliberately never executed directly; execution requires separately admitted copies under the host's native product verifier. Retain the CAS root and original receipt until custody is confirmed; unreviewed cleanup is not an acceptance claim.

## Required original evidence / stop conditions

| Gate | Necessary original evidence | Refuse when |
| --- | --- | --- |
| E0 | Exact product #76 HEAD/base/TREE, original source checks and standalone approved host request | Draft-only local fixtures or changed source |
| E1 | True native Linux ARM64, independently signed source/producer pair, immutable old/new asset+manifest SHA, one-use runner | Missing original receipt, synthetic binary, provider secrets, emulated host |
| E2 | Old actual private binary, different SHA/tag, private old-home sha before/after | Clean empty HOME interpreted as a prior-install PASS |
| E3 | Product #76 stage/select status, exact private CAS old/new pin, journal ID/SHA | Unsafe path, stale writer, missing `SHA256SUMS` |
| E4 | Actual new binary version+MCP+provider-lock, every required row PASS (not SKIP) | Coarse overall PASS only, absent real gateway |
| E5 | Exact restore prior SHA and version/MCP + replay refusal | Only pointer changed, stale/foreign receipt, no old version smoke |
| E6 | Original workflow run/job/Runner + overlay/VM teardown, retained original artifact and raw logs | Cleanup or provenance unobserved |

Completion requires original E0–E6 passed under #545/#527 **and** an independent check that no real owner `~/.local/bin/kc` was touched. Record missing evidence as `NOT_VERIFIED`; do not invent timing, signatures or an actual published Release from this procedure.

This runbook changes no code, product default CI, release workflow, runtime configuration, operating files or host admission. Existing public-repo `[skip ci]` paid-CI fence remains until #73/#75 safely replace it. Merge of #76 source remains HOLD until its original free native/source required checks pass.
