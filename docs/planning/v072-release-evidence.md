# v0.7.2 MCP safe-default release evidence

**Status:** COMPLETE — GitHub Release + isolated **Linux AMD64** installed-binary acceptance only.
**Publication:** 2026-11-07 00:37:37 UTC.
**Tag:** [v0.7.2](https://github.com/camem2442/keycli/releases/tag/v0.7.2) → commit `9606c53219de0292886859f2714d80184953c60a` (lightweight).
**Owning workflow:** [GitHub Actions #42319270781](https://github.com/camem2442/keycli/actions/runs/42319270781) — `v0.7.2 one-shot gated release` SUCCESS.

## Actually observed in the release job

1. Rejected existing tag/release, asserted checkout HEAD = running GitHub SHA.
2. Ran `./verify.sh --suite all` on exact tagged source; Linux CI and Go tests passed.
3. Cross-built five release binaries; generated `SHA256SUMS`, verified all six files (five binaries + manifest).
4. Ran real `kc-linux-amd64` in an isolated temporary install path, version `v0.7.2`:
   - default MCP advertised exactly `list_secrets`, `policy_check`, `doctor`;
   - forged direct `get_secret` returned isError/disabled with no secret exposure;
   - opt-in `--allow-secret-values` advertised `get_secret`;
   - `doctor` completed.
5. Published the tag and six GitHub Release assets, **re-downloaded all six published assets**, checked the published `SHA256SUMS`, verified the remote Linux asset bytes matched the tested local artifact, installed again to an isolated fresh prefix and reran the MCP smoke successfully.

Published release asset digest (provider metadata; independently checked against published `SHA256SUMS`):
- `kc-linux-amd64`: SHA256 `3702c11070bc4c53975b5e75eb85ff7d17cb45974e06581f541276c17ab7c458`
- `kc-darwin-arm64`: SHA256 `c7ad856377911016de36c9680f9dcbc8867fe507396b0dfc16f40146db280386`
- `SHA256SUMS`: SHA256 `a3e238d3ef1831d3e07daeddf0102f9bc91aaec6dfeeef614b18b1292d6691f3`

No live secrets, Keychain, Mac GUI, deployed Cloudflare Worker, or Codex/Claude model turn was used in the release smoke. **Mac binary execution/GUI/native DB, real MCP clients and cloud agents remain NOT VERIFIED**. The published `v0.7.1` (and older) MCP default still exposed `get_secret`; that historical behavior is not retroactively fixed.

## Installer adoption (separate PR)

A second PR pins `scripts/install.sh` and `scripts/cloud-install.sh` defaults to `v0.7.2`, updates docs and removes the one-shot release workflow after it succeeds. Existing user-installed binaries and previously built Cloud Agent snapshots do not update automatically. The keycli `.kc/consumer-profile.yaml` stays at its existing explicit pin until separate consumer/host acceptance. A default install pin is not itself evidence that all installed machines were updated.

Rollback: explicitly set `KC_INSTALL_REF=v0.7.1` for previous CLI behavior, **but not for safe-default MCP**.
