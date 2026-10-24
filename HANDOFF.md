# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**설치·릴리스 UX** — `scripts/lifecycle.sh`, `scripts/install.sh`, `verify.sh` `release`/`mac-smoke`, GitHub Actions `v*` 릴리스, keychain `-T /usr/bin/security`.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1 | **COMPLETE** | `./verify.sh --suite all` (Linux) |
| Linux CI (fake security/clip/integration) | **COMPLETE** | `-tags=kc_testhooks`, `cmd/fakesecurity` |
| 설치 (`lifecycle.sh`, `install.sh`) | **COMPLETE** | `~/.local/bin`, `PREFIX` |
| 릴리스 산출물 + checksum | **COMPLETE** | `./verify.sh --suite release` |
| `kc --version` (ldflags) | **COMPLETE** | `scripts/lifecycle.sh build-dev` |
| keychain 쓰기 ACL (`-T /usr/bin/security`) | **COMPLETE** (코드) | [backends.md](docs/contracts/backends.md) |
| **NOT VERIFIED (실 Mac)** | | |
| Keychain ACL·프롬프트·`kc.age`·ai-core 실읽기 | **NOT VERIFIED** | `./verify.sh --suite mac-smoke` (Linux에서는 skip exit 0) |
| `link`, `push`, export/import/status | **DEFERRED** | roadmap |

## 검증

```bash
./verify.sh --suite all          # Linux CI
./verify.sh --suite release      # cross-build + SHA256SUMS (keyatlas와 동일 패턴)
./verify.sh --suite mac-smoke    # Darwin: throwaway kc.test.* ; Linux: NOT VERIFIED skip
```

Mac 개발 루프: `bash scripts/lifecycle.sh build-dev --install` 후 `./verify.sh --suite mac-smoke`.

## License

저장소 **public** 전환에 맞춰 [LICENSE](LICENSE)는 **MIT**(2026 camem2442)로 두었다. owner가 다른 라이선스를 원하면 교체하면 된다.

## NEXT CONTEXT

M2: `link`, `kc doctor`. Owner Mac에서 `mac-smoke` green + 키체인 쓰기 승인 후 ACL 재확인.
