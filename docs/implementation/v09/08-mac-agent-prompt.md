---
status: NOT VERIFIED
scope: agent-implementation-prompt
---

# Mac 로컬 에이전트용 실행 프롬프트 — K09-01 우선

아래 프롬프트는 **별도 Mac 로컬 에이전트**에게 복사해 사용할 구현 지시서다. 이번 문서 게시 자체가 아래 작업을 실행하거나 원격 변경을 허가하지 않는다.

## 복사할 지시문

> 목표: `camem2442/keycli`의 v0.9 구현 K09-01 (값을 읽지 않는 `kc delivery plan`과 strict profile parser)를 별도 branch에서 최소 구현·테스트하라.
>
> 1) 먼저 실제 Mac 워크트리 경로와 git status/branch/HEAD, 원격 main HEAD, local WIP, worktree 목록, `AGENTS.md`와 `dependencies/workspace-policy-lock.json`을 읽기 전용으로 확인하라. 2026-11-10 문서 기준 main은 `4c972d9815cf0f89f52dbcd91a772376c8c297dd`였지만 **현재 main을 독립적으로 재조회**하고 차이가 있으면 새 기준으로 기록하라. 일치하지 않는 workspace-infra policy source pin이면 자동 fallback 금지.
> 2) `docs/implementation/v09/` 전체와 기존 `docs/contracts/{cli,architecture,security-model,transfer}.md`, `docs/planning/v08-multi-agent-auth-infrastructure.md`를 대조하라. v0.9는 아직 구현되지 않은 제안이다. 진행 중인 KeyCLI #29/#40/#42/#45, 검증용 #25를 변경하거나 병합하지 마라.
> 3) 오직 K09-01의 순수 Plan/profiles, CLI help 및 오프라인 fake 회귀만 구현하라. 임시 HOME/KC_CONFIG 외 실제 사용자의 Keychain, `~/.kc`, config, 설치된 바이너리, 실행 중 OpenCode/Cursor 서비스에 손대지 마라. `reset/clean/stash`, force push, branch takeover 금지.
> 4) K09-01 acceptance는 **Plan 시 Secret resolver 초기화·값 조회 0회, 외부 플러그인 실행 0회, network/remote write 0회, 프로필의 정확한 provider/account/target/env 바인딩, 필수 필드 누락·중복 alias·unknown effect·wrong scope fail-closed**다. source alias→destination의 분리 검증 및 metadata-only JSON을 포함하라. 진짜 provider 자격증명은 사용하지 마라.
> 5) `delivery plan`은 민감값을 읽지 않기 위해 기존 `newPushCmd`의 Secret 조회 경로를 재사용하지 마라. 새로운 명령 경로에서 App/Resolver backend 초기화가 발생하지 않는지 독립 probe를 작성하라. 기존 `kc exec --require`, policy v1, MCP metadata-only, Git helper와 stored alias 호환성 보존.
> 6) 승인된 변경 범위만 파일을 수정하고, `go test`와 영향 범위 `verify.sh --suite <suite>`를 Linux-first로 재사용하라. 기존 CI는 `pull_request` 자동 트리거·GitHub-hosted Ubuntu 사용이므로 비용 승인 없이 PR 생성으로 CI를 실행시키지 마라. 로컬에서 확인한 테스트만 PASS라고 기록하라.
> 7) Markdown 계약 정합성을 검사하되 v0.8 정본은 구현 결과를 확인하기 전에 임의 승격하지 마라. 나머지 K09-02/03/04/05, WIF 실테스트, Cloudflare Worker Stage, Oracle VM 생성은 **이번 작업에서 실행하지 않는다**.
> 8) 결과: (a) before/after HEAD와 clean/WIP 보존, (b) 변경 파일·Go 구조, (c) 테스트 명령·정확한 통과 수, (d) 값 없는 synthetic negative 증거, (e) 남은 blocker, (f) 다음 PR의 독립 범위를 보고하라. 원격 브랜치 게시·PR 생성·병합·CI dispatch·릴리스·실제 키 변경은 추가 Owner 허가 없으면 하지 마라.

## 다음 PR의 순서

- K09-02: [GitHub WIF](03-github-wif.md)의 claim assertion + 정확한 GitHub secret scope. 인수 전 WIF 실제 교환 금지.
- K09-03: [회전/재조정](06-rotation-and-recovery.md)의 원장·중복 차단. 기존 기록은 유지.
- K09-04: [Cloudflare 비활성 Version](04-cloudflare-inactive-versions.md)의 fake Wrangler/단일 bulk 버전. 기존 `secret put` 즉시 배포 경로의 운영 위험을 별도로 표시.
- K09-05 및 WI/SV: OCI WIF 신원 프로브, workspace-infra source-bound receipt, standard-vm runner G1~G7를 **각 저장소별 독립 작업**으로 수행.

## 인수 체크

에이전트 자체 보고의 `PASS`는 실제 host proof가 아니며, PR Ready/Merge·권한 확대는 Solo Maintainer의 분리 판단이다. 설계 판단에 이견이 있으면 현행 계약을 우선하고 해당 항목을 `BLOCKED` 또는 `NOT VERIFIED`로 명시하라.
