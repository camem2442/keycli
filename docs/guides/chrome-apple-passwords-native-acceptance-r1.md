---
status: source-only
owner: keycli
---
# Chrome + Apple Passwords · native owner acceptance plan (Q1 / Q2)

## Current fact and privacy boundary

Apple supports installing the Passwords extension for a third-party browser on macOS through **Passwords → Get Browser Extension**. Set up the extension **manually inside the dedicated per-site Chrome profile**, not the existing personal/default Chrome profile. Do not export Apple Passwords CSV to run this test. Verify Apple account name/credential or passkey only in the private operator session; do not save its content, screenshots, cookies, extension storage or OTP in the Git repository. A Chrome extension can have significant browser privileges, and Playwright's request routing does not intercept every service-worker-controlled request. No CDP debugger port, `--autoConnect`, agent-connected Chrome DevTools MCP, arbitrary JS, profile archive or credential-to-Linux export is permitted in the owner session.

The Chrome worker's `owner_otp` injection is separate from the default Apple/iCloud extension flow. The user-selected `mfa_mode=apple_extension` or `passkey` MUST require native human interaction and must NEVER automatically obtain or fill a separate TOTP seed. `mfa_mode=owner_totp` must be explicitly enrolled by the trusted operator for a low-risk automation account, with distinct MFA child authorization. The Go `owner2fa.FDCodeFiller` and Python `OwnerFDTOTP` form an inherited-socket transport **only**, not a live enrolled authenticator or original Goal/Grant/Claim verifier.

## Native-acceptance route: reuse existing skill, don't fork it

Use `workspace-infra/skills/native-acceptance/SKILL.md` for the **owner-local-gui** lane, `device-debt` for portable/native separation, and `portbridge-one-link` only for synthetic PB-405 or user-facing protected links. Browser auth requires a real foreground owner Chrome window and user presence; it cannot truthfully claim zero focus steal. Native evidence must record this as `allow_focus_change=true` and be scheduled in the current owner Mac single-resource slot. Do not silently touch the user's normal Chrome windows, extensions, passwords or Keychain.

1. On the *actual owner Mac*, establish product HEAD/pinned Chrome installation and a clean, isolated browser profile root (0700) without reusing any default Chrome profile. A read-only preflight may establish installed version and profile metadata, **not** extension/passkey success.
2. In that profile, visit Passwords → Get Browser Extension and install/enable the macOS extension with the human operator present. Record only `extension_present: true/false` and whether manual setup was complete; no raw extension settings.
3. Choose **one disposable test account/site** with permission for the test; require site-specific operator-owned `SitePolicy` selectors and exact HTTPS origin. Confirm password autofill/2FA suggestion behavior by human interaction without recording contents. Test `session_status` and `reauthenticate` independently and ensure URL/redirect confinement.
4. Confirm Chrome can retain the dedicated session across a *single* close/reopen with a private profile, and that account logout/revocation returns `NO_SESSION`; document cookie invalidation rather than dumping Chrome storage.
5. Probe passkey/user verification separately: required prompt must be handled via Apple user verification; unsupported, canceled or cross-origin requests must yield `HUMAN_ACTION_REQUIRED` or blocked, **never** virtual-authenticator success.
6. Run negative tests: no personal profile use; no CDP/extension debug exposure; no cross-origin navigate/unauthorized OTP; reused request ID denied by original journal; stale grant/claim denied; no passwords/OTP/cookies in model/tool response.
7. Produce a native-acceptance **private receipt** bound to exact source HEAD, Chrome build/channel, Mac identity/GUI lane, evidence digest, required user-presence outcome, denied scenarios and cleanup. Only a sanitized metadata summary may enter PR comments.

## Source/operating distinction

Portable synthetic tests (Go RFC 6238/FD and Python owner socket sink/Chrome policy) do not attest Apple extension or Touch ID. Do not promote PR #81/#82/#83/#84 to Ready or merge based on this document alone. Existing KeyCLI and workspace-infra original owning verification, native GUI receipt and independently verified Goal/Grant/Writer authority remain necessary.

Official sources:
- https://support.apple.com/ko-kr/guide/passwords/mchlf7ac261e/mac
- https://support.apple.com/ko-kr/120758
- https://playwright.dev/python/docs/chrome-extensions
- https://playwright.dev/python/docs/network
- https://developer.chrome.com/blog/remote-debugging-port

### Personal Chrome profile prevention

`scripts/chrome_profile_guard.py` is the shared check for both the Chrome worker and read-only Mac preflight. It requires an already-created owner-only private root and forbids symlinked ancestor paths or known default Chrome/Chromium profile directory components. The built-in Mac `/var` → `/private/var` root-owned alias is allowed only when that exact canonical mapping is observed. The guard does **not** isolate another same-UID process or prove extension/passkey operation. Its Linux synthetic tests enforce default-profile refusals; real Mac GUI acceptance is still required.

### Owner-local console admission (source-only, 2026-11-21)

The real `PlaywrightChromeLauncher` checks `chrome_owner_gui_gate.require_local_gui_attempt()` before starting Chrome. It rejects Linux, root, another macOS console user, explicit SSH sessions and GitHub Actions runner contexts. The read-only native metadata preflight uses the same observation and **continues to return HOLD** even when the console identity matches: Touch ID, iCloud Passwords extension, GUI/TCC admission, parent and child original Grants and actual login success all require separate private owner evidence. This guard does NOT isolate a malicious same-UID process and cannot replace a signed Goal/Grant/Claim/Writer, physical Mac acceptance, or human passkey interaction.

### macOS kernel-backed agent/broker UID boundary

`scripts/chrome_owner_os_boundary.py` is a **read-only prerequisite** for an eventual owner-local Unix socket ingress. It reads actual connected socket peer UID via `getpeereid(3)` on macOS, rejects unknown/root/same-UID peers and requires a different explicitly enrolled non-admin agent UID from the browser owner's GUI/Passwords UID. Every outcome remains `HOLD` or `BLOCKED`, `authorizes_execution=false`; this module itself does not open a socket, install a second account, enable login, issue an original Grant or make a Chrome effect. `getpeereid` identifies the caller UID, not the signing or validity of a Goal/Grant/Claim. **Do not treat a same-UID terminal agent as isolated**, even if Chrome profiles are 0700 and request paths are allowlisted. Either move arbitrary-shell Mac agents to a separately controlled standard account (and restrict Mac owner SSH) or keep them off the credential-bearing owner host. The owner-host native process, TCC/Apple extension and signed browser/MFA child authority require separate original checks.

Unit test: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/tests -p test_chrome_owner_os_boundary.py -v`, 8 fake-kernel/policy cases. **Actual Mac kernel getpeereid and OS account isolation NOT VERIFIED**.
