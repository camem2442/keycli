# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M1 구현** — cobra CLI, resolver, env·file(age)·keychain 백엔드, `ls/get/exec/set/rm/cp`, 클립보드 어댑터, `verify.sh`.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| 요구·설계 문서 | **COMPLETE** | docs/ |
| M1 CLI (`ls/get/exec/set/rm/cp`) | **COMPLETE** | cmd/kc, internal/cli |
| env·file(age) 백엔드 | **COMPLETE** | Linux 단위 테스트 |
| keychain 백엔드 | **PARTIAL** | FakeRunner Linux 테스트; 실제 `/usr/bin/security` **NOT VERIFIED** |
| 클립보드 (`cp`, `set --clip`) | **PARTIAL** | atotto·어댑터 코드; pbcopy/wl-copy/OSC52 실기 **NOT VERIFIED** |
| macOS age 키(키체인) | **DEFERRED** | `KC_KEY`만 Linux CI에서 검증 |
| `link`, `push`, `export`, `import`, `status`, plugin | **DEFERRED** | roadmap M2+ |
| Target·Ledger·Middleware | **PARTIAL** | 인터페이스·타입만 (architecture 예약) |
| `verify.sh --suite all` (Linux) | **COMPLETE** | Cloud Agent VM |
| workspace-infra 등록 | **DEFERRED** | 변경 없음 |

## 검증

```bash
./verify.sh --suite all   # gofmt, vet, test, darwin/arm64+linux/amd64 cross-build, CGO_ENABLED=0
./verify.sh --suite unit
```

macOS 키체인·클립보드·age 키체인 로드는 수동 **NOT VERIFIED** 체크리스트 필요.

## 설계 편차

[0002-m1-implementation-notes.md](docs/decisions/0002-m1-implementation-notes.md)

## NEXT CONTEXT

M2: `link`, macOS keychain 실기 검증, ai-core 슬롯 형식 대조(`ai-core` 저장소).
