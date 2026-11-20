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
- Owner must manually configure any Apple iCloud Passwords extension and approve passkey/2FA dialogs. Bounded explicit interactive wait 0–120 seconds. Optional owner OTP filler receives verified origin/challenge only after owner authority; future KeyCLI #83 integration is still pending.
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

`PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/tests -p test_chrome_owner_bridge.py -v`: 9 synthetic cases PASS in isolated Linux scratch with fake owner/browser. Local system Chromium launched, but navigation to localhost and data URLs was refused `ERR_BLOCKED_BY_ADMINISTRATOR`, so **no real-browser navigation/login E2E** completed. KeyCLI exact-head owning Go suite, Chrome macOS GUI, iCloud Passwords, passkey, live original authority and secure OTP fill remain NOT VERIFIED. **Draft/HOLD.** No installed changes or paid hosted CI.

References: https://developer.chrome.com/docs/devtools/agents/get-started/configuration/ · https://chromewebstore.google.com/detail/icloud-passwords/pejdijmoenmkgeppbflobdenhhabjlaj · https://playwright.dev/docs/auth · https://playwright.dev/docs/chrome-extensions
