# v0.7 operational evidence (KC-55, KC-59, KC-40)

**Purpose:** PR/review receipts for Mac operational verify, post-merge alias smoke, and Cloud Environment activate + fresh Agent smoke.  
**No secrets** in this document (no tokens, no secret values, no API key material).

**Related:** [v07-release-gate.md](./v07-release-gate.md)

---

## KC-55 — Mac v0.7 operational verification

| Field | Value |
|-------|--------|
| Date | 2026-10-28 |
| Host | SJ Mac (GUI session) |
| keycli checkout | `~/Developer/programs/utils/keycli` @ `main` (`1503ecb`, tag `v0.7.0` present) |
| Installed `kc` | `KC_INSTALL_REF=v0.7.0 bash scripts/install.sh` → `~/.local/bin/kc` (sha256 verified) |
| Cursor Cloud env (push target) | `52cae156-cf87-11f1-89ee-7aa6f0729b11` |

| Step | Result | Exit |
|------|--------|------|
| `kc doctor --json` | profile **`mac-gui`** | 0 |
| `bash verify.sh --suite mac-smoke` | ok | 0 |
| `kc exec --require --only github -- gh auth status` | logged in | 0 |
| `kc push cursor` E2E | dry-run + real PUT; list API confirmed test name; DELETE cleanup | 0 |
| `kc push gh` (repo `camem2442/keycli`, app `actions`) | dry-run / set / delete | 0 / 0 / 0 |
| `bash scripts/check-rules.sh` | ok | 0 |

**E2E test secret name:** `KC55_E2E_TEST` (value from **env** backend only, not committed).  
**Note:** KC-57 later merged alias `cursor_api_key` for push resolver; KC-59 re-verifies after merge.

**Status:** **COMPLETE** (Mac operational path for v0.7.0).

---

## KC-59 — Post-merge Mac push alias smoke (after KC-57)

| Field | Value |
|-------|--------|
| Date | 2026-10-28 |
| `main` after pull | **`c14354d`** (KC-57 cursor API key alias) |
| `KC_CURSOR_API_KEY` | unset |
| Store | `cursor_api_key` only |

| Check | Result |
|-------|--------|
| Binary | `go build -o /tmp/kc-smoke ./cmd/kc` from updated `main` |
| Command | `kc push cursor --env-id 52cae156-cf87-11f1-89ee-7aa6f0729b11 KC57_ALIAS_SMOKE --dry-run` |
| Test value | `KC57_ALIAS_SMOKE` via **env** backend only |
| Outcome | exit **0**, dry-run preview (no real PUT) |
| Unit tests | `./internal/config/...` + `./internal/target/cursor/...` PASS |

**Ops footgun:** `/Users/k/bin/kc` may still be a pre–KC-57 build; use `~/.local/bin/kc` or reinstall from `main`.

**Status:** **COMPLETE** (dry-run only).

---

## KC-40 — Cloud Environment build, Activate, fresh Agent smoke

### Agent build (Dashboard)

| Field | Value |
|-------|--------|
| Build ID | `bld-20261027-6404dc8d-36fb-4dcd-9c23-ccda49f33ed1` |
| Type | Agent (manual) |
| Ref at build | `camem2442/keycli` @ **`1503ecb`** (`docs: README v0.7.0 release link`) |
| Build status | **SUCCEEDED** (~2026-10-28 03:00–03:01 KST) |
| Install script | `bash scripts/cloud-install.sh` |
| Install log (excerpt) | `installed kc-linux-amd64 from release v0.7.0 -> /home/ubuntu/.local/bin (sha256 verified)`; `cloud-install: complete (KC_INSTALL_REF=v0.7.0)` |

Environment dashboard: `https://cursor.com/dashboard/cloud-agents/environments/e/52cae156-cf87-11f1-89ee-7aa6f0729b11`

### Fresh Cloud Agent VM (post-activate smoke)

| Check | Result |
|-------|--------|
| Boot snapshot | `bld-20261027-6404dc8d-36fb-4dcd-9c23-ccda49f33ed1` (not prior `bld-20261025-…`) |
| `kc --version` | `v0.7.0` (`f3b95ae`) |
| `kc doctor --json` | `"profile": "linux-cloud"` |
| `/usr/local/bin/kc` | symlink → `~/.local/bin/kc` |
| `kc exec --only github -- gh api user -q .login` | **`camem2442`**, exit 0 (token not logged) |

**Status:** **COMPLETE** (Cloud Agent install + runtime smoke).

---

## Cursor Project extended reports (owner-only)

Full step logs and agent run IDs live in the **Cursor Project** store (not in this repo). Owner can attach from Project **Context**:

- KC-55 full report: `internal/kc55-mac-v07-verify-report.md`
- KC-59: `internal/kc57-post-merge-smoke.md`
- KC-40 smoke: `internal/kc40-fresh-cloud-smoke.md`
- Close rollup: `internal/kc58-v07-close-status.md`

For GitHub reviewers, the tables above are sufficient receipts unless audit requires raw logs.

---

## Commits referenced

| SHA | Topic |
|-----|--------|
| `c14354d` | KC-57 — `cursor_api_key` alias for Cursor push API key |
| `1503ecb` | README v0.7.0 link (Cloud build ref) |
| `46d1d61` | Release gate doc after KC-57 (example `main` tip; confirm at merge) |

Tag: **`v0.7.0`**
