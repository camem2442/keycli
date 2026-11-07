# Mac native acceptance — TUI and Cursor Vertex (Q2)

**Status:** NOT VERIFIED for real Mac. Linux source/unit verification is separate from native GUI admission.
**Source baseline:** `bb19ba0` (2026-11-07); release install pin v0.7.1.
**Scope:** `kc interactive`, secret clipboard auto-clear, `kc wire cursor-vertex`.
**Safety:** Do not touch production Keychain, the live Cursor database, or installed `~/.local/bin/kc` during automated tests.

## A. Linux / isolated CI (source checks)

- [ ] `./verify.sh --suite all` PASS on an unchanged PR HEAD. Confirm `gofmt`, `go vet`, tests, docs, policy and release stages.
- [ ] TUI: no raw clipboard fallback when the auto-clear handler is absent. Handler admission fails **before** any resolver read.
- [ ] Cursor: process observation failure means BLOCKED (not "not running"); invalid URL and DB JSON fail without writes.
- [ ] Cursor: running + dry-run is accepted; running + write + no `--restart` is blocked; stopped write is accepted; running + `--restart` requires the normal close/relaunch path.
- [ ] SQLite fixture round-trip retains unrelated JSON fields, rejects corrupted files, and handles missing row inserts.

Linux test results must **not** be promoted to real Mac acceptance.

## B. Mac GUI admission (operator session, not remote SSH)

1. Confirm `uname -m`, `git rev-parse HEAD`, `kc --version`, `type -a kc`. Build the PR commit into a distinct temporary binary; do not overwrite the installed release executable. Record the binary SHA-256.
2. Use a throwaway secret and isolated kc config/store. Never inspect or print real secret values in logs/receipts. Keep `KC_KEY`, env token values and Keychain content out of the report.
3. TUI: verify raw terminal restoration after Quit/Escape/Ctrl-C, search and selection, denial under active `middleware = ["audit","policy"]`, confirmed deletion only, masked details by default, and an explicit confirmation before revealing a throwaway value.
4. TUI: copy the throwaway secret via the registered auto-clear handler; verify clipboard expiration after the configured timeout. Then replace the clipboard with another value and verify it is **not** deleted. Never test the clipboard with real credentials.
5. TUI: confirm that a missing auto-clear callback fails without attempting to copy (unit test; no live workaround).

## C. Cursor state database: staged before production

1. On a throwaway SQLite fixture, run `kc wire cursor-vertex --dry-run --db <fixture> --base-url https://fixture.invalid` and prove byte-for-byte DB unchanged.
2. Exercise a successful isolated write and read-back with SQLite JSON validation. Verify `useOpenAIKey` is true, `openAIBaseUrl` is updated, and unrelated keys remain untouched.
3. Simulate process-observation failures: missing `pgrep`, timeouts, malformed PID; no DB writes are allowed. Do not terminate any real process during this stage.
4. Native app admission requires explicit operator approval: first save a SQLite-consistent `.backup` of `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb` in a private `0700` folder (backup file `0600`). Treat the backup as sensitive user data, not a Git artifact.
5. While Cursor is open, a real write **without** `--restart` must fail and leave DB unchanged; dry-run remains allowed. Test real `--restart` only with explicit operator approval and a recoverable application state.
6. After a native write, confirm Cursor launches and reads back the configured URL, unrelated settings remain intact, and restore is possible from the backup while Cursor is fully closed. No claim of native pass without this evidence.

## D. Evidence receipt and acceptance

Record **only**: date/time, test host kind, OS, release version and source SHA, temporary binary SHA-256, test case IDs, exit codes, DB integrity result, and backup existence (not location if sensitive). Never store token contents, credentials, or full client DB.

| Layer | Success threshold | Current claim |
|---|---|---|
| Linux isolated source | All selected tests and CI PASS | Await PR CI |
| TUI native | Terminal restoration, policy denial, auto-clear, clipboard replacement PASS | NOT VERIFIED |
| Cursor native | Safe snapshot, fail-closed running state, restart/read-back and recovery PASS | NOT VERIFIED |

If a Mac native gate fails, keep the PR scope separate from v0.7.1 release claims and do not infer production readiness from Linux CI.
