---
status: NOT VERIFIED
scope: proposed-github-integration
---

# GitHub API / Actions / OpenAI WIF

## 두 인증을 별도로 취급

- GitHub 자원(`gh`/`git`/PR/Actions) 접근은 Job의 `GITHUB_TOKEN`, 설치 범위 GitHub App 또는 제한 PAT 사용. 공개 저장소 `ls-remote` 성공은 인증 검증이 아니다.
- OpenAI API 호출은 GitHub Actions OIDC → OpenAI Platform WIF 서비스 계정 매핑을 기본으로 하며, 장기 `OPENAI_API_KEY`를 GitHub Secret에 넣지 않는다.
- Cloudflare 배포용 GitHub Actions는 보호된 Environment의 제한된 Cloudflare API Token을 쓸 수 있다. GitHub OIDC가 Cloudflare Wrangler API Token을 자동 대체한다고 주장하지 않는다.
- 개발자는 `GITHUB_TOKEN`이나 OIDC JWT의 원문을 채팅/로그/커밋에 넣지 않는다. 비신뢰 PR·fork는 발급/배포 주체가 아니다.

## WIF 준비 순서 (운영 설정은 owner 승인 후)

1. 기존 OpenAI Personal API Organization과 실제 과금 대상 프로젝트를 확인한다. 서비스 계정에 제한된 모델·엔드포인트 권한을 부여한다. 프로모션 크레딧 적용 여부는 프로젝트/조직의 실제 billing usage로 별도 검증한다.
2. OpenAI Platform > Organization Settings > Security > Workload Identity Providers에서 GitHub Actions Provider 생성. `iss=https://token.actions.githubusercontent.com`, `aud`는 workflow에서 요청할 정확한 값. GitHub OIDC discovery 사용, JWKS 수동 업로드는 필요하지 않음.
3. 그 Provider에 특정 OpenAI 서비스 계정의 mapping 추가. 최소 claim: `repository=camem2442/workspace-infra`, `ref=refs/heads/main`, `workflow_ref=camem2442/workspace-infra/.github/workflows/ai-eval.yml@refs/heads/main`. 필요 시 `environment=development`와 `iss/aud`도 명시; 실제 토큰 claim을 확인해 문자열을 고정한다. `workflow` 이름만을 privileged mapping에 신뢰하지 않는다.
4. 먼저 워크플로 파일 존재/정확한 Git blob SHA, 이벤트와 `permissions` 검토. 이 파일은 아직 새로 만들지 않았으며 이름은 **계획용 예시**다.
5. Workflow의 특정 trusted Job에만 `id-token: write`와 `contents: read` 부여. GitHub Environment 보호·브랜치 제한. `pull_request_target`와 비신뢰 코드 실행 결합 금지.
6. 인증 시도 전 [공통 WIF 경로 선택](#wif-전송-경로-선택)을 수행한다. 요청 audience는 프로필과 trusted Job에서 일치해야 한다.
7. LIVE probe는 WI-KC-01의 신뢰 호스트 연결과 exact Job·repo·HEAD/base·비용 Gate 이후 실행한다. 실제 교환은 신뢰된 Job의 인증된 실행 영수증으로 인수한다. claim JSON, JWT 디코딩 및 token 존재만 확인한 결과는 `NOT VERIFIED`다.

## WIF 전송 경로 선택

인증 전에 고정 SDK의 기능, 대상 issuer·audience·서비스 계정 mapping 및 허용된 경로를 검사한다. 공식 SDK token provider를 지원하면 자동으로 선택한다. SDK 기능 부재가 사전 확인되고 같은 scope의 직접 WIF token exchange가 허용된 경우 그 경로를 자동 선택한다. 두 경로 모두 요건을 충족하지 못하면 `BLOCKED`다. 이 선택은 오프라인 Plan 이후에 수행한다.

선택은 첫 OIDC/IDCS token 발급이나 교환 시도 전 확정한다. 이후 오류·timeout·401/403/429가 발생해도 다른 경로나 정적 API 키로 자동 전환하지 않는다. 짧은 수명의 token은 해당 경로의 정책과 예산에 따라 메모리에서 처리한다. 영수증에는 선택 경로와 제한된 결과만 남긴다. [OpenAI WIF 공통 계약](https://developers.openai.com/api/docs/guides/workload-identity-federation)

### 최소 워크플로 설계 스케치 (PROPOSED; 자동 설치 금지)

```yaml
name: ai-eval-manual
on:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  evaluate:
    # 실제 사용 전 기존 Linux-first host/비용 게이트 확인
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write
    environment: development
    steps:
      - uses: actions/checkout@v4
      - name: OpenAI WIF scoped evaluation
        # OPENAI_WIF_AUDIENCE / OPENAI_IDENTITY_PROVIDER_ID /
        # OPENAI_SERVICE_ACCOUNT_ID 는 GitHub Variables 로 공급.
        run: node scripts/call-openai-wif.js
```

공식 SDK example을 기반으로 token provider를 작성한다. 이 YAML은 예시 파일이며 아직 `scripts/call-openai-wif.js`가 없다. 임의 PR 오픈으로 GitHub-hosted Runner를 소비하지 않는다. 실제 호스트/Runner/승인된 Workload 비용을 별도 고정한 후 적용한다.

## GitHub Secret 전달 (WIF 불가한 대상만)

후속 K09-02의 기본 경로는 GitHub Actions Secrets REST API다. 고정된 GitHub API origin에서 대상 scope의 public key와 `key_id`를 읽고, 값을 libsodium sealed box 규격으로 메모리에서 암호화한다. 같은 scope의 PUT에 `encrypted_value`와 `key_id`만 보내고, 조직 scope에는 `visibility=selected`와 정확한 `selected_repository_ids`를 추가한다. repo ID·환경·조직 소유 및 선택 목록을 쓰기 전에 확인한다. 저장소·환경·조직별 endpoint를 섞거나 권한이 부족할 때 범위를 넓히지 않는다. [GitHub Actions Secrets REST API](https://docs.github.com/en/rest/actions/secrets)

이 PUT은 지정 scope의 Secret을 즉시 생성·갱신한다. 전용 테스트 repo/env를 쓰고, 원격 관측에서 이름·범위를 확인한 후에만 작업을 기록한다. 값 동일성은 GET 메타데이터와 별도로 필요한 경우 해당 소비자의 제한된 인증 요청으로 확인한다. 공급자 응답이 불명확하면 원장에 `UNKNOWN_EFFECT`를 남기고 재조정한다.

기존 CLI의 scope 차이는 유지한다. `gh secret set DEST --repo OWNER/REPO`는 한 저장소, `--env ENV --repo OWNER/REPO`는 그 저장소의 환경, `--org ORG --repos OWNER/REPO`는 조직 Secret의 선택 저장소다. 기존 `kc push gh`는 `--repos` 기반이다. REST 전송 결과에 따른 gh 자동 fallback은 사용하지 않는다. WIF가 선택된 대상은 인증 교환만 수행한다.

## 수용 테스트

- Offline: 예상 `repository/ref/workflow_ref/aud`와 다른 JWT fixture 전부 거부; environment 유무에 따른 `sub` 변화 검증; 넓은 owner 전체 허가 금지.
- Offline: repo/env/org의 REST 경로와 key scope를 따로 검사하고, 조직 선택 범위가 확대되거나 암호문·비밀값이 출력되는 경우 거부한다. Plan은 secret resolver를 초기화하지 않는다.
- Live 전제: 정확한 trusted Job·실제 인증 교환, 짧은 토큰 수명, 401/403/429 시 권한 확대 없이 FAIL/HOLD, 실제 billing 프로젝트 확인.
- 권한: 성공한 OIDC 교환이 GitHub PR Ready/Merge 또는 workflow_dispatch 허가가 아님. `workspace-infra` canonical proof를 재사용.

## 공식 출처

- https://developers.openai.com/api/docs/guides/workload-identity-federation/github-actions
- https://developers.openai.com/api/docs/guides/workload-identity-federation
- https://docs.github.com/en/actions/reference/security/oidc
- https://cli.github.com/manual/gh_secret_set
