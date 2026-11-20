---
status: proposed
---
# Browser intent R0 — Mac-owner passkey and session delegation

## Scope

Pure Go `internal/browserintent` defines a strict **untrusted** remote request envelope and an interface requiring independent original `workspace-infra` authority. It has no network listener, no passkey extraction, no live Browser, no Authority implementation and no credential access. Do not describe an accepted JSON request as a signed grant. The package's mock tests are not an end-to-end login proof.

Request example (synthetic IDs and site; expiry is illustrative and must be regenerated):

```json
{"v":1,"goal":"goal-0001","claim":"claim-0001","request_id":"request-0001","site":"example.com","action":"session_status","path":"/account","expires_unix":1795172450}
```

Fields not on the exact allowlist, including `password`, `cookie`, `passkey`, `storage_state`, `cdp`, `javascript`, and unsigned `verified`, are rejected. Duplicate keys and trailing JSON are rejected; request length ≤4 KiB; TTL ≤5 minutes; HTTPS site is represented by exact ASCII hostname and a restricted relative path. No arbitrary JS, HTTP header, raw cookie or account export action exists.

## Required runtime chain (not implemented)

1. CLI agent submits typed intent to a separate trusted owner identity.
2. Owner checks exact original `Goal/Grant/Claim` (including agent, site, action, request ID, expiry, writer generation, revocation) via existing infra authority and atomically reserves ID before effect. On absent/mismatched/stale authority, reject.
3. Mac browser worker uses its own isolated per-site profile. Apple Passwords passkeys remain inside Apple's normal WebAuthn/user-verification ceremony. Never copy private key into KeyCLI or Linux.
4. The browser worker reports only fixed `performed` or `human_auth_required`; no cookies, session state, page contents, DOM snapshots, API tokens or secrets in a tool result. Page-data return, if ever needed, requires a separately reviewed scoped redactor.
5. Redirects, popup/window targets, cross-origin navigation, unsafe forms and user-authentication prompts require native owner enforcement, not merely envelope parsing. Payments, password/security changes or external posts require fresh explicit user approval.

## Verification and HOLD

- `go test ./internal/browserintent` with synthetic fixtures on isolated Linux proves only decoding and mock authority call order.
- Mac Safari/Chrome actual WebAuthn, Touch ID, session storage persistence, TCC, executable signing, original independent authority receipt and isolation have **NOT** been verified.
- Never Ready/merge to operational configuration solely on source or mocked tests. No paid hosted CI fallback.
- Apple's Credential Exchange API (`ASCredentialImportManager`) is for participating credential-provider apps; it is **not** a generic way for the CLI to fetch Apple's passkey private material.

References:
- https://developer.apple.com/documentation/authenticationservices/ascredentialimportmanager
- https://developer.apple.com/videos/play/wwdc2025/279/
- https://playwright.dev/docs/auth
- `docs/contracts/security-model.md`
