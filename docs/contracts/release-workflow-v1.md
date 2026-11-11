---
status: proposal
owner: delivery
---

# Release workflow 계약 (v1)

`release.yml`은 "태그가 밀리면 무조건 게시"에서 "검증된 태그만, 개발/RC만, 승인
환경에서만 게시"로 바뀐다. 운영(stable) 태그는 이 워크플로에서 **거부**한다.

## 기존 위험과 변경

| 기존 | 위험 | 변경 |
|---|---|---|
| `push.tags: ["v*"]` | `v1`, `v0.10`, `vnext` 등 오발행 | 정확한 3형식만 트리거, classify가 재검증 |
| 검증 단계 없음 | 게이트 미통과 바이너리 게시 | `verify` job: `./verify.sh --suite all` + lock 검증 |
| `softprops/action-gh-release` 기본값 | dev 태그가 정식 릴리스로 게시 | `gh release create --prerelease --verify-tag` |
| 커밋 결속 없음 | 태그와 소스 불일치 | `tag^{commit}` == `HEAD` 강제, 실패 시 exit 4 |
| provider lock 미확인 | 잠금이 풀린 산출물 게시 | **빌드된 바이너리**로 `-lock-report` 실행, `provider_enabled=false` 강제 |
| checksum 미검증 | 자산 변조·부분 업로드 | `gate` job에서 `sha256sum -c` + 버전 문자열 + MCP smoke |
| 권한 과다 | 워크플로 전체 `contents: write` | 기본 `contents: read`, publish job만 write + `id-token`/`attestations` |
| 승인 없음 | 자동 게시 | `environment: release` (보호 규칙), dry-run 경로 제공 |

## 채널 분류

| 태그 | 채널 | prerelease | 게시 |
|---|---|---|---|
| `v0.10.0-dev.1` | dev | true | 가능 (환경 승인 후) |
| `v0.10.0-rc.1` | rc | true | 가능 (환경 승인 후) |
| `v0.10.0` | stable | false | **거부** (S5-O + 별도 승인 필요) |

stable 거부는 `guard` job에서 즉시 실패한다. 실수로 안정 태그를 밀어도 게시되지 않는다.

## 모의 릴리스(dry-run)

```bash
gh workflow run release.yml -f tag=v0.10.0-dev.1 -f dry_run=true
```

classify → guard → verify → build → gate까지 실행하고 publish는 건너뛴다. 산출물은
워크플로 artifact로 남아 검사할 수 있다. 태그를 만들지 않는다.

## 빌드 산출물

| 자산 | 포함 |
|---|---|
| `kc-{darwin,linux}-{arm64,amd64}` | CLI 4종 |
| `keycli-linux-amd64` | 레거시 이름 호환 |
| `SHA256SUMS` | 위 5개 |
| `kc-gateway-*` | **미게시** (dev.1은 CLI 중심) |

게이트웨이는 아직 릴리스 자산이 아니다. 포함하려면 별도 `gateway-assets` job과
서비스 기동 테스트를 추가해야 한다 (`dev-release-acceptance.md` 참조).

## 검증 위치

| 검사 | 실행 위치 | 근거 |
|---|---|---|
| Go·docs·policy 회귀 | `verify` (ubuntu) | `verify.sh --suite all` |
| tag↔commit 일치 | `verify` | `git rev-parse` 비교 |
| provider lock | `verify` + `gate` | 빌드된 바이너리의 `-lock-report` |
| checksum | `gate` | `sha256sum -c` |
| 버전 문자열 | `gate` | `kc --version` |
| MCP smoke | `gate` | `scripts/verify-mcp-binary.sh` |
| 설치 바이트 일치 | 로컬/별도 | `scripts/verify-dev-release.sh` |

## 증거 사슬

```
tag ──► commit ──► build ──► SHA256SUMS ──► GitHub attestation ──► release asset
                                   │
                                   └─► verify-dev-release.sh (다운로드·설치·실행)
```

- attestation(`actions/attest-build-provenance`)은 "이 자산이 이 워크플로·이 커밋에서
  생성됨"을 결속한다. **코드의 안전성이나 게이트 통과를 증명하지 않는다.**
- 검증 명령: `gh attestation verify <asset> --repo camem2442/keycli`.
- 릴리스 노트에는 항상 `source commit`, `provider lock 상태`, `운영 게이트 HOLD`를 적는다.

## 규칙(ruleset) 권장 — 관리자 설정 필요

| 대상 | 규칙 |
|---|---|
| `refs/heads/main` | required checks: `test / linux`, `verify`; force-push 금지; 삭제 금지 |
| `refs/tags/v*` | 업데이트·삭제 금지(불변 태그), 서명 태그 권장 |
| Environment `release` | required reviewer(본인), 태그 배포만 허용 |

이 설정은 저장소 관리자 권한이 필요해 이번 작업에서 변경하지 않았다. 현재
`origin/main` 보호는 `false`다.

## 한계

- 워크플로는 **커밋된 소스**를 검증하지만, 검증 로직 자체는 태그 시점의 워크플로 파일을
  따른다. 태그를 만드는 사람이 워크플로도 바꿀 수 있으므로, 불변 태그 + required
  checks + 환경 승인이 함께 있어야 의미가 있다.
- attestation은 빌드 출처이며 보안 인수가 아니다.
