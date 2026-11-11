---
status: proposal
owner: delivery
---

# 개발 릴리스 설치 인수 (v0.10.0-dev.N)

목표: 게시된 개발 자산을 **기존 v0.8.0 설치본을 건드리지 않고** 검증한다.

## 자동 스크립트

`scripts/verify-dev-release.sh` — 릴리스 자산만 사용하며 실토큰·Keychain·설치본을
변경하지 않는다.

```bash
# 게시된 릴리스 (네트워크)
scripts/verify-dev-release.sh --tag v0.10.0-dev.1

# 로컬 빌드 자산으로 오프라인 검증
scripts/verify-dev-release.sh --tag v0.10.0-dev.1 --assets-dir dist

# 실제 installer까지 검증 (install.sh 사용)
scripts/verify-dev-release.sh --tag v0.10.0-dev.1 --use-installer
```

검사 항목:

| 항목 | 방법 | 실패 시 |
|---|---|---|
| SHA256SUMS | `sha256sum -c` (전 자산) | 중단 |
| 호스트 자산 존재 | `uname` → `kc-<os>-<arch>` | FAIL (또는 `--allow-arch-skip`) |
| 설치 바이트 일치 | 자산 → 임시 PREFIX 복사 후 `cmp` | FAIL |
| 버전 문자열 | `<prefix>/kc --version` == tag | FAIL |
| MCP smoke | `scripts/verify-mcp-binary.sh` | FAIL |
| Provider 잠금 | `kc-gateway -lock-report` → `provider_enabled=false` | FAIL |
| 기존 설치 불변 | `~/.local/bin/kc` sha256 before/after | FAIL |

종료: `PASS` exit 0, `FAIL` exit 3. JSON 한 줄을 출력한다.

## 플랫폼 매트릭스

| 플랫폼 | 방법 | 현재 |
|---|---|---|
| macOS arm64 | 자산 다운로드 → 임시 PREFIX → `--version`·MCP·(선택) mac-smoke | 스크립트로 **PASS** (오프라인 자산) |
| macOS amd64 | 빌드·arch 확인, 실행 가능 환경에서 smoke | 미실행 |
| Linux amd64 | GitHub runner 또는 Lima에서 설치·MCP smoke | 다음 단계 |
| Linux arm64 | Lima Ubuntu aarch64에서 설치·실행 | 다음 단계 |
| Android Termux | Go linux/arm64 바이너리는 Bionic TLS 정렬로 실행 불가 → Python 클라이언트만 | 별도 계약 |

macOS live(B10)에서 `mac-smoke.sh`는 실제 Keychain 항목을 만든다. 이 인수 범위에서는
**실행하지 않는다**. 실 Keychain 시험은 별도 승인 범위다.

## 실제 실행 결과 (오프라인, 2026-11-11)

```
assets                             local: .tmp/assets
checksums                          PASS
host_asset                         PASS (kc-darwin-arm64)
installed_bytes                    PASS
installed_version                  PASS
mcp_smoke                          PASS
provider_lock                      PASS   ← gateway 자산을 넣은 경우
existing_install_unchanged         PASS
{"status":"PASS","tag":"v0.10.0-dev.1","host_asset":"kc-darwin-arm64"}
```

부정 통제: 자산 1바이트 변조 → `checksums FAIL`, `{"status":"FAIL"}`, exit 3.

## 게시 후 필수 확인

1. `gh release view v0.10.0-dev.1 --json isPrerelease,assets` → `isPrerelease=true`
2. `gh attestation verify kc-darwin-arm64 --repo camem2442/keycli`
3. 위 스크립트를 네트워크 모드로 재실행 (`--tag`, `--use-installer`)
4. 기존 설치본 `~/.local/bin/kc` 해시 불변 확인

## Gateway 자산 정책

현재 워크플로는 `kc-gateway`를 게시하지 않는다. 게시하려면:

- 별도 빌드 job + `SHA256SUMS` 항목 추가
- 서비스 기동 smoke (`systemd` unit 없이 `-lock-report` + `/health`)
- `-provider live`가 거부되는지 확인

그 전까지 dev.1은 **CLI 중심 개발 릴리스**로 표기한다.
