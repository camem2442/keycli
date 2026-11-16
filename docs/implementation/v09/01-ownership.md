---
status: NOT VERIFIED
scope: proposed-architecture
---

# 책임·신뢰 경계와 플랫폼 선택

## 유지할 불변식

1. `keycli`는 로컬 해석·정책·전달 클라이언트이고, 중앙 인증 서버나 Goal 권한 엔진이 아니다.
2. `workspace-infra`는 단일 Writer Claim, 호스트 Admission, 실제 검증, 작업·비용 게이트, 운영 effect 허가를 소유한다. KeyCLI 영수증을 그대로 신뢰하지 않는다.
3. `standard-vm`은 Oracle VM/Runner 라이프사이클과 G1~G7을 소유한다. OCI Runner 권한으로 GitHub Merge/Cloudflare Deploy를 자동 허가하지 않는다.
4. 클라우드는 각 공급자의 secret store / identity를 사용한다. Mac이 꺼져도 런타임 API 호출은 정상 동작해야 한다. 로컬 Vault 전체와 `KC_KEY`는 동기화하지 않는다.
5. 같은 UID에서 `KC_AGENT` / allowlist는 악의적인 에이전트를 격리하지 못한다. 실제 권한은 토큰 scope, 별도 OS 경계, 지정한 호스트 실행 권한이 결정한다.
6. `kc mcp`의 기본은 메타데이터 전용. `--allow-secret-values`를 공용 에이전트 템플릿에 넣지 않는다.

## 플랫폼별 기본값

| 환경 | 우선 선택 | 비밀 전송이 필요한 경우 |
|---|---|---|
| Mac | Keychain / age + `kc exec --require --only` | 별도 제한 키를 이름으로 저장 |
| Cursor Cloud | 플랫폼 runtime secret + KeyCLI env backend | 원본→대상 이름 명시, 저장소 범위 |
| GitHub Actions → OpenAI | GitHub OIDC → OpenAI WIF | WIF 미지원 작업만 제한 API 키 |
| GitHub 저장소 API | Job-scoped GITHUB_TOKEN / 설치 범위 GitHub App | 로컬 fine-grained PAT는 최소 범위 |
| Cloudflare Worker | Worker Secret, 비활성 버전 준비 | `versions secret put/bulk`, promotion 별도 |
| GitHub Actions → Cloudflare | 환경 보호된 Cloudflare API Token | 가장 좁은 Worker/계정 권한 |
| OCI Compute → OpenAI | Instance Principal → IDCS access token → OpenAI WIF | WIF 불가 시 OCI Vault 사용 검토 |
| OCI Compute → OCI API | Instance Principal + OCI IAM | 사용자 API 서명 키 파일 복제 금지 |

WIF는 **OpenAI Platform API 프로젝트의 서비스 계정 매핑**을 우선 대상으로 한다. Codex WIF는 관리형 ChatGPT workspace 권한 모델이 다르며, API 프로젝트 설정이 Codex 로그인·구독 크레딧을 자동 대체하지 않는다.

## 작업 권한과 신뢰 자료

- KeyCLI plan / candidate receipt는 `authoritative=false`. 승인 가능한 사실은 검증된 실제 호스트·정확한 Git HEAD·해당 provider 최신 관측과 결합한 결과뿐이다.
- 권한의 최소 단위는 `repository + head/base + operation + provider + target + env + budget + host + expiry`. 재사용 가능한 일반 승인 문자열은 금지.
- Provider credentials가 유효해도 예약·Writer Claim·제품 검증 없는 remote mutation을 허용하지 않는다.
- 원격 effect timeout은 실패 확정이 아닌 `UNKNOWN_EFFECT`. 새로운 작업을 실행하기 전 GET/조회/재조정.
- Cloudflare 비활성 버전도 Version URL에서 기존 D1/R2/DO/KV에 접근할 수 있으므로 Staging 격리는 별도의 Worker/리소스를 사용한다.

## 세 저장소의 인터페이스

`workspace-infra` → (신뢰된 호스트의 제한된 OperationAuthorization) → `keycli delivery` → (metadata-only Receipt) → `workspace-infra`의 기존 canonical collector. OCI Runner 동작은 `standard-vm`이 수행하고 KeyCLI는 인증 방식·재료의 전달 여부만 다룬다.

배포 효과가 발생하는 `versions deploy`, OCI 인스턴스 생성·기동/종료, GitHub workflow_dispatch·PR Ready/Merge는 KeyCLI의 `stage` 권한에 포함되지 않는다. 신규 `delivery` 패키지는 기존 CLI를 통해 운영권한을 우회하지 못하게 설계한다.
