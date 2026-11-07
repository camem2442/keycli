release_tag: v0.7.2

# v0.7.2 — controlled MCP default-security patch release candidate

**Gate:** Source/CI candidate; publication is conditional on the one-shot workflow.
**Baseline before this PR:** keycli `main` @ `3fa1e2c1e248a0b29cfd88d196e6946c15c40a1c`.
**Prior published release:** `v0.7.1`. That binary exposed `get_secret` through `kc mcp` by default.

## Scope

- Publish the already merged [PR #16](https://github.com/camem2442/keycli/pull/16) MCP fix: default `kc mcp` shows metadata-only tools; direct forged `get_secret` requests are denied; `--allow-secret-values` explicitly restores trusted-client behavior.
- Include the previously merged [PR #17](https://github.com/camem2442/keycli/pull/17) source safeguards for TUI and Cursor DB. This tag does **not** certify real Mac acceptance.
- Retain `v0.7.1` as a rollback. No other app, Worker, Cloudflare or user secret should be modified.
- No automatic consumer pin update in this PR. Release consumers must separately admit the new tag.

## Required gates, in order

1. PR exact HEAD Linux CI `./verify.sh --suite all` passes. Review the added workflow and this sentinel line (single-purpose).
2. Merge the candidate into `main`. The one-shot workflow runs **only** on `main` push changing this exact sentinel file; it rechecks source SHA, rejects pre-existing tag/release, and runs full CI again.
3. Build 5 release binaries + `SHA256SUMS`, `sha256sum -c` all assets.
4. Run the **actual installed Linux binary** in an isolated temporary directory: 3 metadata tools by default, forged `get_secret` denied, opt-in tool discovery includes `get_secret`, `doctor` works, and version `v0.7.2` appears. Use no real credentials.
5. Only after steps 2–4 succeed, create the GitHub tag and Release on the exact running `GITHUB_SHA`. Verify six expected asset names. The tag/release creation is part of this job because GitHub `GITHUB_TOKEN`-generated tag events do not start another workflow.
6. Follow-up: inspect published tag SHA, release asset checksum and test logs; then, **in another PR**, pin installer defaults to v0.7.2 after external release is independently confirmed.

A workflow failure before step 5 means **NOT RELEASED**. A partially published release must be inspected and repaired deliberately; do not rerun an already tagged release blindly.

## Non-claims

No Mac native keychain/GUI/clipboard acceptance, live Cursor restart, Cloudflare Worker real push, Codex/Claude one-turn E2E, or trusted cross-host Goal admission is established by this release smoke. No real client secrets are exported.
