---
status: source-candidate
owner: keycli
date: 2026-11-17
goal: product-owned disposable kc dev bytes CAS (continuation of infra R3 #541)
---

# KeyCLI development-only disposable byte preview

This is a **new isolated product-owned staging implementation**, not a general updater or replacement for the installed `~/.local/bin/kc`. It does not execute untrusted release binaries, call GitHub/OCI/Cloudflare, change the real PATH, publish tags, or claim service health. Its positive result is *byte selection within a new owner-private throwaway root*; it is not trusted source/provenance/signature/native/operating acceptance.

## Exact existing owners and dependencies

- [workspace-infra R1 #531](https://github.com/camem2442/workspace-infra/pull/531), [R2 #538](https://github.com/camem2442/workspace-infra/pull/538), [R2-C #540](https://github.com/camem2442/workspace-infra/pull/540), and [R3 #541](https://github.com/camem2442/workspace-infra/pull/541) are the observation, local byte pair and no-authority planning evidence owners. The source-only R3 plan never grants installation.
- [KeyCLI #73](https://github.com/camem2442/keycli/pull/73) zero-paid-auto-CI and [#75](https://github.com/camem2442/keycli/pull/75) release producer remain DRAFT/HOLD, as do native private host [infra #529](https://github.com/camem2442/workspace-infra/pull/529) and [standard-vm #16](https://github.com/camem2442/standard-vm/pull/16). A full dev/rc release is not admitted before original native source and producer receipts.
- Existing `scripts/verify-dev-release.sh` owns real version/MCP/provider-lock smoke in a disposable prefix; it may SKIP provider-lock or prior-install comparisons, and therefore its coarse PASS is not sufficient to assert native operating health. This new selector **never runs that shell script or any kc binary automatically**.

## Source-only primitive

`scripts/dev-disposable-cas.py` uses Python stdlib, explicit opt-in `--allow-disposable-write`, absolute `kc-dev-preview-*` root under a preexisting owner-private mode 0700 parent, and descriptor-relative owner-controlled fsync/flock/no-follow reads. It stores a validated caller-asserted dev/rc byte pair (`kc-linux/darwin-{arm64,amd64}` and `SHA256SUMS`) only after matching both separately supplied SHA-256 digests and a unique selected checksum entry. Executable bytes are stored as **mode 0400, not executable**; no process is started.

`create` refuses adopting any existing or non-private root or operating `~/.local` path. `stage` compares an external exact prior pin digest (CAS), rereads content-addressed bytes, writes an immutable transaction receipt before atomically selecting a new private pin and returns receipt ID/hash. `status` is read-only and never calls a network endpoint. `rollback` requires original receipt ID/hash, compares the still-current selected pin and previous artifact identity, restores previous pin bytes atomically, and rejects replay or unexpected newer selection. Cooperative same-UID source fixture, **not hostile-writer or irreversible schema migration protection**.

Minimal source feedback (no cloud or real installation):

```bash
python3 -B scripts/tests/test-dev-disposable-cas.py
```

Only after an independently verified source/manifest/native host, a *human-owned disposable fixture* may use `create --root /absolute/private-parent/kc-dev-preview-1 --allow-disposable-write`, then `stage` with explicit `--asset-path`, `--sums-path`, dev/rc `--version`, exact `--source-head` and both expected SHA-256 strings, `--expected-previous none`, `--allow-disposable-write`. Store the returned external rollback receipt ID+SHA. Do not call this path from a new unapproved public CI job or install live kc.

## Separate native gate before any real executable activation

1. Verify actual GitHub immutable release, original KeyCLI source HEAD/TREE, signer/producer original receipt and both **true native** ARM64 and AMD64 G4 results; never treat this selector's input hashes as a trusted producer signature.
2. On an independently admitted disposable real host, copy exact selected bytes into a private execute-capable temporary prefix **after** signed native producer approval; only the product owner runs `scripts/verify-dev-release.sh` or a narrower `--version`+MCP smoke, and checks every mandatory result rather than a coarse PASS containing SKIP.
3. Compare existing installed `~/.local/bin/kc` bytes/path/version before and after; the disposable selector never modifies them. Require separate original Goal/Claim and file-state ledger, strong receipt binding and real running process checks before any operating promotion.
4. Exercise rollback, verify the original native binary runs/has correct version/MCP and locked provider state, and reject second rollback, source/hash drift, host credential exposure, crash ambiguity and stale CAS. No operating success from mere restored byte pointer.

### CI cost fence

Current KeyCLI `main` retains `test.yml` with GitHub-hosted Linux jobs on every pull_request, and `release.yml` retains hosted paths. This source is staged on a dedicated feature branch **without opening a new public PR** to avoid inadvertently billing forbidden hosted minutes. Review/PR can be created when KeyCLI G2 noqueue change is integrated and reverified. Branch source and authored tests are NOT an original host PASS, and no release/VM or live kc install has occurred.
