---
status: proposed
owner: keycli
---
# Chrome owner browser pilot (source-only)

This source-only Playwright Chrome library is a **restricted owner-host component**, NOT an installed browser service or a remote tool. It has no HTTP listener, no MCP/CDP debug port, no credential/OTP export, no arbitrary script/page-content tool, and no real original Grant authority implementation. No actual Apple Passwords or iCloud extension data was read.

## Architecture / source behavior

- Build on KeyCLI browser-intent parent PR #82. Accept only exact site/approved path `session_status` and `reauthenticate`; **deny** `read_page` until a separately reviewed output redactor exists.
- Inject an independent trusted owner `verify_and_reserve` before any Chrome profile or page. The fake unit-test authority is never a production implementation.
- Dedicated 0700, owner-controlled per-site Chrome profile (not default Chrome), branded Chrome via Playwright headed mode, no `--remote-debugging-port` / `--autoConnect`. Restrict page requests to exact HTTPS origin and do not return page text/cookies. This Playwright route is not a complete browser/OS security boundary.
- Owner must manually configure any Apple iCloud Passwords extension and approve passkey/2FA dialogs. Bounded explicit interactive wait 0–120 seconds. The owner-only socketpair sink in `scripts/chrome_owner_totp_fd.py` is compatible with the injected `OwnerOTP` protocol; a trusted Mac host supervisor must bind Go `owner2fa.Fulfill` (#83) to the matched AF_UNIX sender and create an independent MFA-child reservation. The public API does not issue those capabilities. Optional owner OTP filler requires an explicit owner-controlled per-site `mfa_mode=owner_totp` and only receives verified origin/challenge after authority; default Apple/iCloud mode never calls it; future KeyCLI #83 integration is still pending.
- Chrome extension worker and 2FA user presence require actual Mac GUI acceptance. Playwright/Chrome extension compatibility is **NOT VERIFIED** here. No direct extension injection CLI flags in branded Chrome (removed in recent Chrome versions).
- Never supply this browser's `--browserUrl`/CDP to an agent. Official Chrome DevTools MCP is only for a distinct, noncredentialed development profile; connecting to authenticated Chrome grants broad page/session access.

## Reused existing skills (no new Skill Library fork)

- `workspace-infra/skills/branch-lifecycle/SKILL.md` — own branch/PR/stack/head/merge.
- `workspace-infra/skills/keycli-cloud-auth/SKILL.md` — no secret exposure.
- `workspace-infra/skills/device-debt/SKILL.md` — Linux synthetic vs Mac native Keychain/Chrome acceptance.
- `workspace-infra/skills/native-acceptance/SKILL.md` — Mac GUI evidence, avoid stealing focus.
- `workspace-infra/skills/portbridge-one-link/SKILL.md` — PB-405 synthetic browser workflow only, never credentialed Chrome debugger exposure.
- Official `ChromeDevTools/chrome-devtools-mcp/skills/chrome-devtools/SKILL.md` — dev-only, not owner credential browser.

## Verification

`PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/tests -p test_chrome_owner_bridge.py -v`: 10 synthetic cases PASS in isolated Linux scratch with fake owner/browser. Local system Chromium launched, but navigation to localhost and data URLs was refused `ERR_BLOCKED_BY_ADMINISTRATOR`, so **no real-browser navigation/login E2E** completed. KeyCLI exact-head owning Go suite, Chrome macOS GUI, iCloud Passwords, passkey, live original authority and secure OTP fill remain NOT VERIFIED. **Draft/HOLD.** No installed changes or paid hosted CI.

References: https://developer.chrome.com/docs/devtools/agents/get-started/configuration/ · https://chromewebstore.google.com/detail/icloud-passwords/pejdijmoenmkgeppbflobdenhhabjlaj · https://playwright.dev/docs/auth · https://playwright.dev/docs/chrome-extensions

### Go ↔ Chrome ACK guarantee

KeyCLI #83's `owner2fa.FDCodeFiller` sends six ASCII OTP digits through an inherited AF_UNIX socket pair only after independent MFA-child original authority verification. This `OwnerFDTOTP` receiver validates enrolled `owner_totp` mode, same HTTPS origin, one OTP field and the optional explicit submit selector, fills the field, and **only then** replies with a fixed `0x01` ACK. The sender denies incomplete/invalid ACK and never retries uncertain effects. ACK confirms input handling only, **not account authentication**, and cannot be promoted to session success without observing the current account marker on the correct origin.

The two adapters are independently testable in synthetic Linux; they still need a trusted Mac owner supervisor that binds both inherited FDs and a distinct MFA child Grant/Claim/operation ID. Do not provide a socket descriptor, profile path, Apple TOTP or browser code to a remote agent.

## 2026-11-20 source review: service-worker and restored-tab boundary

Playwright's network route handler does **not** intercept fetches owned by a page service worker. The follow-up source child explicitly configures `service_workers="block"` in the persistent Chrome context and refuses already-restored nonblank pages **before a new requested page is opened**. A test exercises exact-origin/subdomain routing and a restored hostile tab. These are defense-in-depth checks, not a complete OS sandbox: an extension background worker may need service-worker support, and browser startup can perform network effects before this library attaches route handlers. Do **not** attempt real Apple Passwords extension/Passkey use until original owner Mac Chrome compatibility, credentials, browser native isolation and trusted host verification are demonstrated. Any failure is `HUMAN_ACTION_REQUIRED`/HOLD; never relax to `service_workers="allow"` or return raw CDP/cookies as a workaround.

Official routing limitation: https://playwright.dev/python/docs/api/class-browsercontext#browser-context-route

### MFA child association and stream fragmentation (2026-11-20)

The owner socket receiver now requires a distinct trusted parent browser request ID and child MFA ID at construction, and checks the actual reauthentication callback matches the parent before reading an OTP. The Go #83 challenge separately requires parent/child IDs and an original trusted independent child grant. The six-byte AF_UNIX frame is read in a bounded loop rather than relying on a single `recv()`, which may legally return fewer bytes. The owner browser checks exact HTTPS origin again after filling and after the optional submit. Only the fixed input-completion ACK is sent; no login success, code or cookie enters the agent response. The parent/child FDs must be paired by a future trusted Mac supervisor, not the agent.

## Service-worker and restored-tab boundary (follow-up)

Playwright `BrowserContext.route` does **not** intercept fetches handled by page service workers. The child PR explicitly sets `service_workers="block"` and refuses restored nonblank tabs after installing the network route. This can interfere with Chrome/iCloud Passwords extension operation; compatibility needs original owner Mac evidence. Do not weaken the routing guard to `allow` for convenience. Profile safety/identity updates in the parent are retained, and neither path constitutes independent OS UID/Seatbelt protection. Browser startup network traffic before route installation is not proven blocked. Original Goal/Grant/Claim custody and real passkey/2FA login remain NOT VERIFIED. Reference: https://playwright.dev/python/docs/api/class-browsercontext#browser-context-route

## Browser intent status contract (Go ↔ Chrome owner adapter)

The parent Go `internal/browserintent.Gate` previously accepted generic `performed`, which can mean an action was merely dispatched and cannot prove a logged-in session. The child now accepts only `session_present` or `no_session` for `session_status`, and `session_present` or `human_auth_required` for `reauthenticate`. A new pure `OutcomeFromOwnerStatus` maps the Python owner's **fixed** `SESSION_PRESENT`, `NO_SESSION`, `HUMAN_ACTION_REQUIRED` to those bounded outcomes. `read_page` remains rejected *before* owner authority or browser effects until a separately approved safe output interface exists. Unknown raw statuses and a generic `performed` can never become a reported authenticated session.

Synthetic local Go 1.23 functional replay of the status and action gate, outside the original repository, passed **11 test paths** (9 action/output variants + 2 standalone parent tests). It is **not** the original keycli owning Go suite or a Chrome/2FA integration test. Real Python↔Go wire transport, trusted original authority and native login remain NOT VERIFIED; no user account used.
