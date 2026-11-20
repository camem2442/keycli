# KeyCLI session roots consumer — source-only pilot

This repository consumes the [workspace-infra multi-session-roots contract](https://github.com/camem2442/workspace-infra/blob/ba04f6a4acc21d8f449ca8dc915dca8fd6e874a4/docs/contracts/multi-session-roots-v1.md). It does **not** upgrade `dependencies/workspace-policy-lock.json`, automatically install a CLI, or rewrite existing checkout work.

- Read `AGENTS.md` and `HANDOFF.md`, inspect the actual `main` remote head and existing Goal/PR before using a worktree.
- On an admitted host, run `python3 -B <infra>/tooling/workspace_cli.py session inspect --repo <absolute-keycli-checkout>`. The result is host-local and always reports Writer/credential authority UNOBSERVED.
- To allocate or reenter, create an exact SHA-256-digested existing Lifecycle request and submit it through `session new --request <path> --sha256 <sha>` (later `session reenter` when the owning dispatcher supports it). Do not equate a local marker with a Writer Grant.
- Do not create a second KeyCLI Goal/ClaimStore. Keep one writable agent per branch/worktree. Verify with Go 1.22 on Linux; Mac is reserved for Keychain/host native acceptance.
- Product credential effects are independent of worktree ownership: [MockTimer deployment custody](mocktimer-production-custody.md) is a provider-target transaction only, not a branch lock.
- No automatic cleanup; old Git refs, secret journal and host-original storage remain untouched.
