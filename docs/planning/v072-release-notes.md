# v0.7.2 — safer MCP defaults

- `kc mcp` now exposes only `list_secrets`, `policy_check`, and `doctor` by default.
- `get_secret` becomes available only with explicit `kc mcp --allow-secret-values`; direct calls without opt-in are denied.
- Includes source-level TUI auto-clear fallback blocking and Cursor active-DB protection fixes from #17.
- Linux CI and installed-binary smoke with synthetic inputs are release gates.
- Real Mac GUI/Keychain and cross-provider E2E remain unverified and are not release claims.

**Security note:** `v0.7.1` defaults to advertising `get_secret`. Use `v0.7.2` for safer MCP defaults once the published asset checksum and tag are verified; using `--allow-secret-values` can expose values to a trusted MCP client's logs/model context.
