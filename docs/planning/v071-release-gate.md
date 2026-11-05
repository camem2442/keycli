# v0.7.1 release gate (install pin patch)

**Status:** COMPLETE — install default pin only (post v0.7.0).

| Item | Value |
|------|-------|
| Scope | Default `KC_INSTALL_REF` **v0.7.0 → v0.7.1** in `scripts/install.sh` and `scripts/cloud-install.sh`; release binaries republished for tag `v0.7.1`. |
| Base HEAD (pre-commit) | `cf3544da152451cdacb539f93ee169e1d056aff0` |
| Release commit (tag `v0.7.1`) | `e62896f344bfdbf0ba201b1c61e7d07c5e258be3` |
| Rollback install | `KC_INSTALL_REF=v0.7.0 bash scripts/install.sh` |
| Codex/Claude E2E | **NOT VERIFIED** (unchanged from v0.7.0 gate) |
| mac-smoke on Linux CI | **NOT VERIFIED** (expected) |

Install one-liner:

```bash
KC_INSTALL_REF=v0.7.1 bash scripts/install.sh
```
