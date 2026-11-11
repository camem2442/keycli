---
status: proposal
owner: delivery
---

# Release workflow 계약 (v1)

| 태그 또는 입력 버전 | 채널 | 실행 결과 |
|---|---|---|
| `v0.10.0-dev.1` | dev | `release` 환경의 prerelease 게시 가능 |
| `v0.10.0-rc.1` | rc | `release` 환경의 prerelease 게시 가능 |
| `v0.10.0` 태그 | stable | PR-14 운영 인수와 게시 승인 전까지 `guard`에서 거부 |
| `v0.10.0` 버전으로 수동 실행 | operational | private/internal 저장소의 workflow artifact만 생성 |

## 태그 검증과 dry-run

```bash
gh workflow run release.yml -f tag=v0.10.0-dev.1 -f dry_run=true
```

`classify`는 입력 태그의 객체와 peeled commit을 원격에서 확인한다. `verify`,
`build`, `gate`는 해당 commit을 checkout하고 원격 태그를 다시 대조한다.
수동 실행에서 선택한 워크플로 ref와 태그가 다를 때도 태그의 commit으로 빌드한다.
결속과 artifact 검증에는 별도로 checkout한 워크플로 SHA의 스크립트를 사용한다.
릴리스 대상 소스에도 이 계약의 Gateway 버전 출력과 빌드가 포함되어야 한다.

## 비공개 운영 후보

승인된 private/internal 빌더 저장소에서 기본 브랜치의 workflow를 수동 실행한다.
GitHub의 저장소 workflow 실행 권한을 가진 사용자가 검토한 SHA와 승인 기록을 입력한다.

```bash
gh workflow run release.yml -f tag=v0.10.0 -f candidate_channel=operational \
  -f candidate_source_sha="$REVIEWED_SOURCE_SHA" -f candidate_approval_ref="$APPROVAL_REF"
```

`tag` 입력은 바이너리에 기록할 최종 버전이다. 후보는 태그 생성 없이 선택한 SHA로
빌드하고, `dry_run` 값과 관계없이 게시 단계에 진입할 수 없다. 공개 저장소의
운영 후보 실행은 `classify`에서 거부한다. PR-13 인수 시 workflow run과 artifact ID,
검증된 manifest digest를 함께 기록한다. 승인 참조는 실행자가 입력한 추적 정보이며,
독립 호스트 검증과 서명된 인수 증거는 PR-13/14가 제공한다.

## 빌드와 다운로드 검사

| 자산 | 포함 |
|---|---|
| `kc-{darwin,linux}-{arm64,amd64}` | CLI 4종 |
| `keycli-linux-amd64` | 레거시 이름 호환 |
| `kc-gateway-linux-{arm64,amd64}` | Gateway 2종 |
| `provenance.json` | 소스 SHA, 워크플로 SHA, 버전, 채널, 실행 ID와 시도 번호, 태그 객체 또는 후보 승인 참조 |
| `SHA256SUMS` | 위 자산 전체 |

`verify`에서 `verify.sh --suite all`을 실행한다. `build`가 올린 immutable artifact ID와
manifest digest를 `gate`에 전달한다. `gate`는 파일 집합과 체크섬을 대조한 뒤 각 Linux
아키텍처에서 다운로드한 바이너리의 버전과 전체 commit, Gateway lock report, CLI MCP를
검사한다. dev/rc Gateway는 `KC_PROVIDER_EFFECTS=enabled`와 live provider 요청을
함께 주어도 잠긴다. operational Gateway의 lock report는 runtime 활성화 설정을 반영한다.
실제 provider 연결과 운영 설치는 별도 인수 범위다.

게시 작업은 두 아키텍처 gate를 통과한 artifact ID만 내려받아 checksum을 다시 대조한다.
GitHub attestation은 자산과 워크플로 실행의 출처를 기록한다. 수동 실행에서 태그의
소스 SHA는 체크섬에 포함된 `provenance.json`에서 확인한다. 후보의 provenance는
워크플로가 생성한 메타데이터이며 PR-13/14의 서명된 실계정 인수 증거와 구분한다.

## 저장소 설정

| 대상 | 적용할 규칙 |
|---|---|
| `refs/heads/main` | required checks, force-push와 삭제 금지 |
| `refs/tags/v*` | 태그 업데이트와 삭제 금지 |
| Environment `release` | 게시 권한을 가진 ref와 사용자로 제한 |

원격 확인과 GitHub Release 생성은 별도 요청이다. 게시 중 ref 변경을 막으려면
릴리스 태그의 업데이트와 삭제를 저장소 규칙으로 차단해야 한다.
