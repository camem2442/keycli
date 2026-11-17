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

## Existing Golden evidence, and why it is not yet this experiment

The original native ARM64 source jobs [44039046216](https://github.com/camem2442/workspace-infra/actions/runs/44039046216)
and [44039759883](https://github.com/camem2442/workspace-infra/actions/runs/44039759883)
are **SUCCESS** for the previously reviewed KeyCLI #73 source and original
sealed Golden host. As of this review the retained GitHub ZIP artifacts
`keycli-g2g4-arm64-44039046216-1` and
`keycli-g2g4-arm64-44039759883-1` are only ~3.6 KB each;
Goal #551 explicitly states they retain logs, **not installable candidate
binaries or an old-version restoration receipt**.

The canonical [standard-vm ARM64 one-shot runner](https://github.com/camem2442/standard-vm/blob/main/docs/keycli-g2g4-runner-20261117.md)
reuses the verified sealed Golden with a *fresh writable overlay and a new
ephemeral runner/request per execution*. Its existing
`--purpose keycli-g2g4-arm64` route invokes the **private**
`keycli-g2g4-free-host-owner.yml` workflow for an exact reviewed public
KeyCLI PR, native G2/G4 source verification and teardown. That existing
source workflow has not been shown to execute the **#76 development CAS E2E**,
and cannot be relabeled as #76 acceptance merely because the same Golden
VM or arm64 CPU is used. The original host owner (#527) must separately
admit and run the **actual #76 product-only procedure** under an independently
reviewed credential-free sandbox and then bind the original run/job/runner and
teardown proof to Goal #545.

Exact native host requirements: the preserved Golden base digest from
Goal #551 is
`724b7cee5b509c56da1eae156941d53374b91fd77f4d66dd5ca2e2107c85c669`,
subject to independent host re-verification before use. Do not copy the
expired historical runner, reusable writable overlay, or native job receipt
into a new run. Freshly produce **two distinct real dev/rc binaries** and
matching SHA256SUMS from reviewed source in the admitted guest; the original
jobs did not preserve those files for reuse.

## Concrete native dev/rc asset production — original owner only

At this observation, GitHub's public KeyCLI Release API lists **only stable**
releases `v0.1.0`–`v0.8.0`, with no dev/rc prerelease asset pair.
The latest stable `v0.8.0` has a Linux ARM64 binary and
`SHA256SUMS`, but its Release has `immutable=false`. A stable Release
is **not** an eligible `dev_cas_pilot.DEV` pin and cannot be relabeled into
a trustable `v0.10.0-dev.N` candidate by changing metadata.

The existing [KeyCLI product-owned builder](../../scripts/lifecycle.sh)
already provides the precise version/channel linker flags. For an inexpensive
ARM64-only sandbox pilot, reuse **its two native Go build commands** rather
than running the full four-platform release matrix; the full release build
remains a separate G4 producer gate. The full product release script creates `kc-linux-arm64`, `kc-gateway-linux-arm64` and the
complete `SHA256SUMS`. On an **independently admitted credential-free
aarch64 guest** with exact two reviewed source trees and pre-provisioned
read-only Go module cache, the host may run the following once in **each**
source tree. It must bind the actual source SHA/TREE and approved dev tag;
this example is a host plan, not an executed receipt:

```bash
set -euo pipefail
test "$(uname -s)" = Linux
test "$(uname -m)" = aarch64
test "$(id -u)" -ne 0
test -z "$(git status --porcelain=v1)"
test "$(git rev-parse HEAD)" = "$REVIEWED_SOURCE_SHA"
# The owner supplies the actual approved DEV_TAG for this exact source.
case "$DEV_TAG" in *-dev.*|*-rc.*) ;; *) exit 2;; esac
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0
export KC_BUILD_CHANNEL="${DEV_TAG##*-}"
case "$DEV_TAG" in *-dev.*) KC_BUILD_CHANNEL=dev ;; *-rc.*) KC_BUILD_CHANNEL=rc ;; esac
export KC_VERSION="$DEV_TAG"
export KC_COMMIT="$REVIEWED_SOURCE_SHA"
# Execute only the ARM64 build commands already owned by lifecycle.sh.
# Do not build the unrelated Darwin/AMD64 release matrix for this pilot.
OUT="$(mktemp -d "${TMPDIR:-/tmp}/kc-arm64-assets.XXXXXXXX")"
chmod 700 "$OUT"
LDFLAGS="-s -w -X github.com/camem2442/keycli/internal/version.Version=$KC_VERSION -X github.com/camem2442/keycli/internal/version.Commit=$KC_COMMIT"
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/kc-linux-arm64" ./cmd/kc
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS -X github.com/camem2442/keycli/internal/effectgateway.channel=$KC_BUILD_CHANNEL" -o "$OUT/kc-gateway-linux-arm64" ./cmd/kc-gateway
(cd "$OUT" && sha256sum kc-linux-arm64 kc-gateway-linux-arm64 > SHA256SUMS && sha256sum --check --strict SHA256SUMS)
# Move only this private asset pair + source-bound provenance to owner custody.
```

Each old/new build must use **independent immutable source/manifest
identities**, an approved dev/rc label consistent with the build channel,
and a **different** actual Linux ARM64 binary SHA. Merely varying the
`KC_VERSION` linker label on an otherwise identical build does **not**
prove a previous code version or a meaningful rollback regression; the
owner must record a previously approved source/tree and its genuine
version/MCP behavior. Before any native smoke, preserve original build
stdout/stderr/exit and the two asset+manifest digests outside the short-lived
guest and ensure the test binary cannot read runner registration/session
credentials. For `-rc.` channel set `KC_BUILD_CHANNEL=rc`; never
substitute stable or operational.

This product build is **not** a Release upload, publishing authority or
service install. It must run under existing standard-vm #16/#527 owner
admission, not automatically from a public PR or a persistent rooted phone.

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
