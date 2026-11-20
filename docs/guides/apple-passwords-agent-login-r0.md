---
status: proposed
---
# Apple Passwords → KeyCLI → browser agents: bounded R0

## What exists in this PR

- Offline, read-only-by-default `kc import-apple`: a **single** Apple Passwords CSV row, exact host and `web.<host>` name, owner-only regular 0600/0400 file, no symlinks/hardlinks, 1 MiB max; no logs containing credentials.
- Synthetic unit fixtures for URL origin confusion, multi-row export, notes/OTPAuth, file permissions, symlinks, and secret-redacted errors.
- Optional Mac-terminal `--write --acknowledge-same-uid-risk`: stores `{origin,username,password}` JSON **as a single entry in the existing age file backend** using atomic `SetIfAbsent`. It must not be enabled in an untrusted agent/same-UID runtime. This operator confirmation is not a security boundary.

Example (synthetic, no real user data in repository):

```sh
kc import-apple --file "$HOME/Private/selected.csv" --site example.com --name web.example.com
# operator-only in an OS identity with no agent processes, only after security review:
# kc import-apple ... --write --acknowledge-same-uid-risk
```

**Do not export the entire Apple vault**. Export one selected password into a local folder excluded from sync/backups, restrict permissions to 0600, delete the plaintext source after successful transfer. The tool never deletes source files automatically. Notes and OTPAuth are rejected (not silently discarded) and passkeys are out of scope. Passwords app CSV is plaintext; Sign in with Apple grants are not exported.

## Hard boundary / operating HOLD

The existing KeyCLI resolver, `kc get`, `kc exec` and opt-in MCP secret output can reveal vault entries to processes with the same OS user and vault unlock rights. The `KC_AGENT` policy alone is **not** a security boundary. No production personal-login import or agent auto-use is approved by this R0 PR. Keep the source and the agent runtime in distinct OS identities until original trusted host, OS peer isolation, and actual native Mac evidence are verified.

## Q2: Mac browser/passkey path (not yet implemented)

1. Apple Passwords remains the passkey authenticator. KeyCLI does not import or export WebAuthn private keys through CLI.
2. Only an independently admitted Mac-native browser worker may initiate an OS-backed WebAuthn request. User presence/verification is respected; no synthetic approval/Touch ID bypass.
3. The browser worker exposes bounded named actions (`inspect_page`, `fill_form`, `submit_login`, etc.) instead of arbitrary JS, filesystem access, cookies, or raw CDP. Authenticated browser state never enters a model tool response.
4. Each action must be checked against original `workspace-infra` Goal/Grant and writer claim immediately before execution. The current KeyCLI source does not implement such a live verifier for browser actions.
5. Use a separate browser profile per site; keep all cookie and storage-state bytes on the owner host. No reuse of default personal browser profile. Read-only operations can use previously authorized sessions; account security changes/payment require new human approval.

## Q3: Linux / Flip3 split (not yet implemented)

- Remote Linux/Flip3 may send a typed request with `goal`, `action`, `site`, `path` and a request ID; it must **never** receive the secret value, Apple passkey private material, storage-state JSON, browser debug port or raw cookies.
- Mac or a separate owner-auth host verifies original signed authority and performs the authorized browser action in its isolated profile. Reply is sanitized structured results only.
- Remote network reachability (Tailscale/SSH) does **not** imply possession of grant or owner-credential authority. No SSH agent forwarding of private Apple credential/keychain access.
- A Linux mock can test denied actions and replay/expiry without any Mac account or real cookies; Mac native acceptance remains separately gated.

## Prerequisites before Ready / production

- Exact HEAD test receipts for `go test ./internal/applecsv/...`, full repository verification, and per-OS owner gate; no real account or real passkey data in CI.
- Separate-UID or host process isolation proof, original grant pinning, auth-result redaction, cookie/session lifetime, revocation, source user-confirmation, and cross-host deny tests.
- Independent audit of import defaults and native WebAuthn session handling.

References:
- Apple: https://support.apple.com/guide/passwords/mchl35b12625/mac
- Apple CSV schema: https://developer.apple.com/documentation/safariservices/importing-data-exported-from-safari
- Apple exchange API: https://developer.apple.com/documentation/authenticationservices/ascredentialimportmanager
- Playwright session security: https://playwright.dev/docs/auth
- Existing KeyCLI trust boundary: `docs/contracts/security-model.md`
