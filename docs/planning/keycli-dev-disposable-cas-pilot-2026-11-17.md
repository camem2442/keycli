---
status: source-candidate
owner: keycli
date: 2026-11-17
goal: G-RELEASE-CD-BRIDGE-R3 product-owned dev-only handoff
scope: private temporary byte selection and CAS rollback fixture; never current kc
---

# KeyCLI development CAS — product-owned isolated byte pilot

## Intent and exact boundary

Bridge workspace-infra [R2 release observation #538](https://github.com/camem2442/workspace-infra/pull/538), [R2-C local SHA256SUMS #540](https://github.com/camem2442/workspace-infra/pull/540), and [R3 pure decision #541](https://github.com/camem2442/workspace-infra/pull/541) to an actual **product-owned disposable bytes transaction**, without introducing a second cross-repository Goal/Claim/authority store. The original real KeyCLI executable and active ~/.local/bin/kc remain unchanged. Never confuse workspace-infra M4.6 keycli.gateway signed byte pilot with the actual user kc CLI.

The new scripts/dev_cas_pilot.py is a stdlib Python 3.11+ *library*, not an automatic updater or CLI. It exposes init, stage, select, status and rollback as explicitly called APIs. There are **no network, provider, GitHub Release, OCI, Cloudflare, Keychain, actions, launchd or service effects**, and no generic executing arbitrary downloaded binaries.

- init creates a freshly random owner-private 0700 root under the OS temporary directory, a marker and lock file. Other roots (including a spoofed root outside system temp) are refused.
- stage accepts only a bounded, owner-owned, single-link regular local file, exact 64-hex SHA256, 40-hex source HEAD and a dev.N/rc.N semver. It copies bytes into an owner-private executable file with content-addressed filename; the return status is STAGED_DISPOSABLE_BYTES_NOT_TRUSTED. A matching digest without independently authenticated producer/signature still conveys **no trust**.
- select uses an exclusive fcntl/flock, verifies the staged bytes and source binding, checks expected previous pin SHA (CAS), writes and fsyncs the immutable external-ID/digest rollback receipt **before** atomically replacing selected.json. It never replaces a live PATH entry or starts the selected byte file.
- status is read-only and rechecks root/file/link count, selected pin, selected blob bytes and returned pin digest.
- rollback takes the original receipt ID plus SHA256, compares the current exact pin generation and staged bytes, then atomically restores the *exact previous selected.json bytes* or removes only this disposable pin. A stale/foreign/replayed receipt cannot override newer state. Old staged binaries are retained; no unreviewed cleanup.
- All mutations require explicit allow_disposable=True; all outputs state authorizes_execution=false, authorizes_operating=false. This same-UID cooperative fixture is NOT a security sandbox against hostile code sharing the UID.

## Original product tests and native acceptance

Run Python stdlib tests with python3 -B scripts/tests/test-dev-cas-pilot.py from the product repository root, on a separately reviewed free local Linux/VM or Mac owner. Fourteen authored fixture methods cover staging bytes, CAS, two versions, exact restore, replay refusal, tags, source pins, symlink/permission/drift negatives, synthetic inert --version smoke and unchanged user kc sentinel. The only executable run is a **test-generated shell fixture**, not a downloaded or real kc binary; it proves no product native operating state.

Do not bypass KeyCLI scripts/verify-dev-release.sh: it is the owner of actual developer-version and MCP smoke but can return summary PASS with skipped optional gateway/previous-install items. Future original **native** acceptance must require executable owner-produced SHA+manifest, source/producer signatures, actual identity of each mandatory check (no skipped operating-critical check), real Mac/Linux OS ABI, provider-lock negative tests, signed external CAS rollback receipt and original old/new version+MCP smoke before even proposing a development install. No paid GitHub-hosted or permanent public self-hosted runner is authorized.

**No CI billed:** The public repo main test.yml currently triggers GitHub-hosted ubuntu-latest for every PR. This source proposal is deliberately committed with [skip ci] and opened as Draft: GitHub documents that this suppresses push/pull_request workflow runs but a skipped check is **not a passing verification**. Use the existing privately admitted free source/ephemeral VM owners to obtain exact-head original tests. Do not merge until those source and native acceptance boundaries are independently satisfied.

Future work (separate authorization): connect the verified local artifact and original product executable smoke to this isolated selector, preserve prior user-installed kc under an independently issued Goal/Claim and user approval, test interruption/health with exact native receipt, then gradual development-only operating promotion. This PR does none of those effects.
