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
6. Node/Python OpenAI SDK의 공식 WIF subject-token-provider 패턴을 적용. SDK가 요청 audience로 OIDC JWT를 발급받아 교환하고 필요 시 단기 토큰 재발급. 토큰 파일 영속 저장·임의 env dump 금지.
7. 실제 provider 거래의 성공은 **신뢰된 Job의 인증된 실행 영수증**으로만 인수한다. 단순 claim JSON 파일, JWT 디코딩, Token presence는 PASS가 아니다.

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

단일 리포지터리: `gh secret set DEST --repo OWNER/REPO`. 조직 Secret: `gh secret set DEST --org ORG --repos OWNER/REPO`. 두 플래그의 의미는 다르고 자동 fallback을 하면 안 된다. `kc push gh` 기존 구현은 `--repos` 기반이므로 현재 코드를 단일 리포지터리 등록 완료의 증거로 삼지 않는다.

v0.9 `delivery` 계약은 `provider/github-actions`, `target/repository`, `target/environment`, `secret_name`을 서로 다른 필드로 받는다. 전달 전 실제 repo와 환경이 일치하는지 검사하고, 값은 stdin/native API body로만 전달한다. 써야 하는 비밀값이 없으면 `stage` 대신 WIF 계획만 반환한다.

## 수용 테스트

- Offline: 예상 `repository/ref/workflow_ref/aud`와 다른 JWT fixture 전부 거부; environment 유무에 따른 `sub` 변화 검증; 넓은 owner 전체 허가 금지.
- Offline: repo/org CLI 서로 다른 argv 생성; 값은 argv/receipt 없음; Dry-run은 KeyCLI secret resolver 미호출.
- Live 전제: 정확한 trusted Job·실제 인증 교환, 짧은 토큰 수명, 401/403/429 시 권한 확대 없이 FAIL/HOLD, 실제 billing 프로젝트 확인.
- 권한: 성공한 OIDC 교환이 GitHub PR Ready/Merge 또는 workflow_dispatch 허가가 아님. `workspace-infra` canonical proof를 재사용.

## 공식 출처

- https://developers.openai.com/api/docs/guides/workload-identity-federation/github-actions
- https://developers.openai.com/api/docs/guides/workload-identity-federation
- https://docs.github.com/en/actions/reference/security/oidc
- https://cli.github.com/manual/gh_secret_set
