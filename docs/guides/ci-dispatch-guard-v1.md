# CI duplicate pre-dispatch guard — source-only candidate

Status: NOT VERIFIED for real GitHub token/host; no repository state changes are performed by the command.

## Ownership

`workspace-infra/tooling/ci_dispatch_guard.py` is the canonical policy/GET collector. `kc ci-guard` is a thin opt-in adapter that resolves only the `github_agent` secret through the existing KeyCLI policy, checks a SHA256 pin on that script, then runs `python3 -I -B` with **only** `GH_TOKEN` injected to the child. It does **not** call GitHub workflow dispatch, Ready, or merge and does not create a new token store. The selected Python and `gh` executables remain trusted-host dependencies, not authenticated by the script digest.

## Read-only use

Pin a reviewed and locally materialized script and separately supply an exact JSON request.

```sh
sha256sum /absolute/owned/workspace-infra/tooling/ci_dispatch_guard.py
KC_AGENT=codex kc ci-guard \
  --script /absolute/owned/workspace-infra/tooling/ci_dispatch_guard.py \
  --script-sha256 <64-hex-reviewed-digest> \
  --request /absolute/path/ci-request.json
```

macOS: use `shasum -a 256` instead of `sha256sum`. Use `--snapshot /absolute/path/snapshot.json` for synthetic/offline inspection; this never requests a token and always yields INSPECTION_ONLY (not an authenticated duplicate block). Do not run `kc ci-guard` inside CI or agent hosts that have not established explicit `github_agent` read permission.

Minimal request (all fields required):

```json
{
  "schema_version": 1,
  "repository": "camem2442/mac-apps",
  "pr_number": 37,
  "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "base_sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "workflow_path": ".github/workflows/ci.yml",
  "workflow_sha": "cccccccccccccccccccccccccccccccccccccccc",
  "scope_digest": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
  "environment_digest": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
  "required_jobs": ["all-linux"]
}
```

These example hashes are placeholders, never use them as production inputs. Observe request fields from the current PR and Git blob, and derive scope/environment digests from a reviewed fixed manifest, not arbitrary agent prompts.

Provider artifact adoption in each consumer workflow is a **separate PR**. Today old runs without the canonical artifact MUST report NO_VERIFIED_DUPLICATE, so they do not falsely block new required tests. `--script-sha256` protects the Python source bytes at the time of local read, not malicious host binaries, concurrent file modification, direct GitHub UI or an untrusted token issuer.

### Exit codes

- 10: `DUPLICATE_CONFIRMED` — cooperating caller blocks manual re-dispatch for the same verified result.
- 11: `HOLD_ACTIVE` — matching run still in flight; caller must not start a duplicate.
- 0: `NO_VERIFIED_DUPLICATE` or `INSPECTION_ONLY` — does NOT authorize dispatch; apply existing Owner and work/budget grants.
- 2: `HOLD` — any missing/stale/invalid provider binding or script execution failure, no dispatch.

Expected future assertion: a live same-HEAD run with authenticated matching exact-artifact receipt, matching PR/base/workflow/scope/environment and terminal required job success returns 10. Unknown, draft-skip, cancelled, failed, provider 403, oversized response or SHA drift never produces 10.
