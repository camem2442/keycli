---
status: historical
owner: keycli-delivery
---

# v0.9/v0.10 RC 증거 (2026-11-10)

## 소스 식별자

| 항목 | 값 |
|---|---|
| landing 브랜치 | `landing/keycli-v010-source-history-20261110` @ `18f3b03` |
| payload 커밋 | `80bdd82b` (Tree `ee735340`) |
| 문서 포함 Tree | `812718c0` (payload + 문서 커밋) |
| host bridge 브랜치 | `opencode/kc-hostbridge-20261110` @ `7f19eb8` (Tree `6c456bb6`) |
| OCI WIF | 같은 브랜치, `internal/identity/ociwif` |
| workspace-infra | `opencode/keycli-delivery-receipt-20261110` @ `b99824a` |
| Go | `go1.22.12 darwin/arm64` (로컬) / CI `linux/amd64` |
| 기준 `origin/main` | `4c972d98` |

## 로컬 릴리스 산출물 (커밋 `7f19eb8`, Go 1.22.12, `CGO_ENABLED=0`)

```
1ac731340f4c8ccf83feb2f1676ddf2904a284372f099df8a1cf5c53f5271bc2  kc-darwin-amd64
ec88c545df45edb9009f9da1d8cbbb5a663033420e94df12e13c108cab1eb414  kc-darwin-arm64
d1c0ba922e5ad601eb8c55001c5f805f2815dade75271c41f97e97a6a9460d76  kc-linux-amd64
6a20633abba7280525117866a49abc777411f7814a095f127eb4f61527236231  kc-linux-arm64
b12144db31ffef5498d5aa06173aaf9e4d0e6a62ae5d02662586810e491d18fb  keycli-linux-amd64
```

주의: 이 해시는 **로컬 빌드**이며 서명·재현 빌드 증명이 아니다. 같은 커밋·같은 Go
버전에서 다시 빌드해 동일해야 하며, Linux CI 산출물과 일치한다고 주장하지 않는다.

## `verify.sh --suite release` 판정 정정

이전 보고에서 "release ok"로 적은 것은 **부정확**했다. macOS에서
`run_release`는:

1. `scripts/lifecycle.sh release` → 4타깃 + `keycli-linux-amd64` + `SHA256SUMS` 생성: **성공**
2. `scripts/verify-mcp-binary.sh dist/kc-linux-amd64` → macOS에서 Linux 바이너리 실행 시도
   → `exit 126` (`exec format error`): **실행 불가**

따라서 로컬 판정은 "산출물 생성 ok / MCP 실행 단계 NOT_VERIFIED(Linux 전용)"이다.
Linux CI(`./verify.sh --suite all`)에서만 이 단계가 유효하다.

## 상태 사다리

| 단계 | 상태 | 근거 |
|---|---|---|
| `SOURCE_VERIFIED` | **PASS** (KeyCLI landing/호스트브리지/OCI, infra WI-KC-01/02) | Tree `ee735340` 3중 재현, 74 + 20 + 16 테스트 |
| `INTEGRATION_VERIFIED` | **PARTIAL** | #61 CI는 코드 동등 범위 재사용, landing PR CI는 미생성 |
| `HOST_VERIFIED` | **NOT_VERIFIED** | 실제 호스트에서 admission 실행 없음 |
| `PROVIDER_VERIFIED` | **NOT_VERIFIED** | provider GET/mutation 0회 |
| `OPERATING_ACCEPTED` | **NOT_VERIFIED** | mac-smoke·실계정 인수 없음 |
| `RELEASED` | **BLOCKED** | 태그·릴리스 권한 없음 |

이 여섯 단계를 섞지 않는다. `SOURCE_VERIFIED`가 `PROVIDER_VERIFIED`를 함의하지 않는다.

## 설치본 경계

- 현재 설치본: `~/.local/bin/kc` = v0.8.0 (`ad30607`), sha256 `fd84b597…`
- **교체하지 않는다.** 이번 후보는 설치 대상이 아니다.
- 롤백: 설치를 건드리지 않았으므로 되돌릴 것이 없다. 릴리스 태그도 생성하지 않았다.

## 릴리스 후보로 인정하기 위한 남은 조건

1. landing PR 생성 + 그 PR의 exact test-merge Tree가 `ee735340`임을 확인
2. Linux CI `./verify.sh --suite all` 성공 (release suite 포함)
3. mac-smoke를 실 Mac에서 1회 (throwaway `kc.test.*`)
4. `TRUSTED_HOST` 승격: 실제 호스트 프로세스가 자격증명을 격리함을 증명
5. v0.9.0 범위 확정(CLI 표면: `delivery plan|status`, `auth probe`) 및 릴리스 노트 승인
6. 태그·릴리스 게시 별도 승인
