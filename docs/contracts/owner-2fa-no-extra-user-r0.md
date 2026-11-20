---
status: proposed
owner: keycli
---

# Q1 · Owner-only 2FA without adding a Mac user

This is an offline source-only Go package. It DOES NOT implement Apple Passwords access, an SSH server, a browser runner, a real Goal/Grant verifier, or real OTP entry. It adds no CLI/MCP methods and no Apple credentials. Do not enable user secrets until native owner-host acceptance.

## Operating boundary
The existing Mac account may keep Apple Passwords, passkeys, Safari and an isolated per-site browser profile. Mac owner host must verify signed original Goal/Grant/Claim/Writer and atomically reserve request IDs before using any MFA seed. A remote Linux/Flip3 agent must NEVER receive a generic Mac shell, keychain access, arbitrary JS, browser debugging port, cookies, browser storage state, OTP output or OTP seed. A same-UID agent on Mac IS NOT isolated by KeyCLI policy, terminal confirmation, a local TCP socket or file modes. If the Mac agent must keep a general shell, consider an OS-enforced separate identity instead.

## Second factor routes
Apple Passwords TOTP suggestion and Apple passkey / Touch ID need normal native user interaction when requested. SMS, email, push and recovery codes also require an owner action by default. Optional fully automated RFC6238 TOTP is only for an explicitly enrolled, low-risk automation account whose seed is kept in a separate owner-host authenticator store not exposed through kc get / kc exec / MCP. No bulk Apple CSV OTPAuth import. The Go Fulfill implementation demands injected owner-host Authority, SeedProvider, Filler; absent authority denies before reading a seed. It returns fixed status only, never an OTP. Filler must independently recheck browser origin and live challenge, because request schema checks alone are not trust. No automatic retry after uncertain browser submission.

## Tests / gates
Synthetic RFC 6238 public vectors; nil/denied authority; no seed fetch after deny; human factors; unknown factor/origin/time refusal; fixed output; near-code-window refusal. Local isolated Linux package test and race check passed. Full source HEAD owning CI, native Mac, original Grant/Claim verification, private authenticator storage, browser user verification and one-site E2E are NOT VERIFIED. Keep PR Draft / HOLD. No paid GitHub-hosted fallback.

References: https://support.apple.com/guide/passwords/mchl873a6e72/mac ; https://www.rfc-editor.org/rfc/rfc6238 ; https://developer.apple.com/documentation/authenticationservices/supporting-passkeys/ ; docs/contracts/security-model.md.
