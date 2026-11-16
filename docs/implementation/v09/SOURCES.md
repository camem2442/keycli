---
status: NOT VERIFIED
scope: source-index
---

# 출처·기존 문서 정합성 색인 (2026-11-10)

## KeyCLI 정본 / 기준

- [main@4c972d9](https://github.com/camem2442/keycli/commit/4c972d9815cf0f89f52dbcd91a772376c8c297dd) — 2026-11-10 읽기 기준, 이후 전진 가능.
- [AGENTS.md](../../../AGENTS.md) — 정책 pin, 값 유출·파일 테스트 금지. 업데이트는 별도 PR.
- [CLI 계약](../../contracts/cli.md), [설계](../../contracts/architecture.md), [보안 모델](../../contracts/security-model.md), [전송 계약](../../contracts/transfer.md) — 구현 정본.
- [v0.8 다중 에이전트 로드맵](../../planning/v08-multi-agent-auth-infrastructure.md), [v0.8.0 릴리스 증거](../../planning/v080-release-evidence.md) — 릴리스·host 인수 구분.
- [Cloudflare 합성 Worker 파일럿](../../reviews/2026-11-08-kc-p4-cloudflare-push.md) — `wrangler secret put` 즉시 배포 관측.
- [Credential lifecycle 소유권](../../reviews/2026-11-08-credential-lifecycle-ownership.md) — v0.8 자동 갱신/전역 sweep 비목표.
- 기존 PR: [#29](https://github.com/camem2442/keycli/pull/29) 수신자 증명, [#40](https://github.com/camem2442/keycli/pull/40) 이전 closeout, [#42](https://github.com/camem2442/keycli/pull/42) 정책, [#45](https://github.com/camem2442/keycli/pull/45) CI guard, [#25](https://github.com/camem2442/keycli/pull/25) DO NOT MERGE.

## 인접 저장소 정본

- [workspace-infra Canonical Goal Verification](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/canonical-goal-pr-verification-v1.md)
- [workspace-infra Common Verification](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/common-verification.md)
- [workspace-infra CI Guard #239](https://github.com/camem2442/workspace-infra/pull/239) — 중복 방지 증거, 아직 곧바로 dispatch 권한 아님.
- [workspace-infra Development Auth Cutover #230](https://github.com/camem2442/workspace-infra/pull/230) — 별개 Worker/Access/OAuth 이전.
- [standard-vm Oracle Runner #7](https://github.com/camem2442/standard-vm/pull/7) — Draft / live 미인수. 브랜치의 `docs/oracle-runner.md`에 계획·G1~G7·읽기 전용 인스턴스 상태.

## 공식 공급자 계약

- OpenAI [WIF 공통](https://developers.openai.com/api/docs/guides/workload-identity-federation): API 서비스 계정 vs Codex 관리형 workspace 구분.
- OpenAI [GitHub Actions WIF](https://developers.openai.com/api/docs/guides/workload-identity-federation/github-actions): issuer/audience/claims, job `id-token: write`, SDK subject-token-provider.
- OpenAI [OCI WIF](https://developers.openai.com/api/docs/guides/workload-identity-federation/oracle-cloud): Instance Principal signer → IDCS token → OpenAI WIF; tenant discovery와 issuer 구분.
- GitHub [OIDC reference](https://docs.github.com/en/actions/reference/security/oidc), [gh secret set](https://cli.github.com/manual/gh_secret_set).
- Cloudflare [Wrangler Workers commands](https://developers.cloudflare.com/workers/wrangler/commands/workers/), [Secret doc](https://developers.cloudflare.com/workers/configuration/secrets/), [Versions & deployments](https://developers.cloudflare.com/workers/versions-and-deployments/).
- Oracle [instance principals](https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/callingservicesfrominstances.htm), [Always Free Resources](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm).

## 최신성·출처 수준

링크·API는 문서화 시점의 공식 공개 계약을 사용했다. Provider UI·지원 범위·실제 계정 한도는 변경될 수 있다. KeyCLI GitHub 소스와 PR 본문은 **구현 또는 작성자 보고**, 라이브 공급자 테스트와 동일시하지 않는다. 향후 운영 프로브는 날짜·신뢰된 host·exact source SHA·provider 최신 응답으로 각 항목을 갱신한다. 공개 문서에 Secret, OCID 전체, 토큰·키 fingerprint, billing 개인 식별자 금지.
