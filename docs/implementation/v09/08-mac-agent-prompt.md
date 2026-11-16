---
status: NOT VERIFIED
scope: agent-implementation-prompt
updated: 2026-11-10
---

# Mac 로컬 에이전트 실행 프롬프트 — K09-04 Cloudflare version-only stage

**이전 K09-01 신규 구현 프롬프트는 KeyCLI [PR #47](https://github.com/camem2442/keycli/pull/47) 등장으로 대체됨.** 이 문서는 새 K09-04 작업의 제안 지시서이며 stage·배포 권한이 아니다.

## 독립 실행 지시문

> 목표: KeyCLI K09-04 최소 Cloudflare Worker **비활성 버전 생성 adapter**를 현재 v0.9 K09-01~03 실제 구현에 이어서 설계·구현하되, 시크릿·운영 Worker·실제 Cloudflare 리소스는 건드리지 않는다.
>
> 1. Mac 로컬의 활성 worktree, 브랜치, dirty/WIP, main/PR #47 최신 HEAD를 읽기 전용으로 확인. 보고된 #47 HEAD는 `e61beed451a5807023975f8650a57aa525097070`, Linux run `42755608356` SUCCESS다. 이미 다른 Writer가 변경 중이면 덮어쓰지 말 것. PR #46 문서 브랜치, #47 구현 브랜치, #29/#40/#42/#45와 검증용 #25를 조용히 rebase/병합/수정하지 말 것.
> 2. 작업 시작 전 `AGENTS.md`, 정책 source lock, `docs/contracts/{cli,security-model,transfer}.md`, PR #47의 실제 `internal/delivery`, `internal/delivery/journal`, `internal/target/cloudflare`, `docs/implementation/v09/04-cloudflare-inactive-versions.md`를 조사. 현행 `kc push cloudflare`는 `wrangler secret put`으로 즉시 배포한다. **새 stage**와 구 경로를 혼동하지 말 것.
> 3. 실작업 전 Go 순수 타입·호스트 승인 callback·정확한 target/account/Worker/env ID를 고정. Plan은 Secret resolver/Keychain/Network를 읽지 않아야 하고, `stage` effect는 허가가 실제 호스트에서 검증된 경우만 가능. KeyCLI가 새 권한 서버를 만들지 않는다.
> 4. 구현 단위 K09-04A는 **합성 단일 Secret**의 `wrangler versions secret put`만 허용. `wrangler secret put`, `wrangler versions deploy`, `wrangler deploy`, `wrangler secret bulk` 등 활성 배포 효과 명령은 fake Wrangler에서 0회임을 검증. Secret은 stdin으로만 전달하고 argv, env dump, 디스크·로그·영수증에 원문 금지.
> 5. PR #47의 `journal.Store.Dispatch`와 기존 record/lock/reconcile를 재사용. 로컬 lock을 원격 전역 lock으로 오인하지 말 것. 원격 쓰기 전 durable intent와 dispatch marker, 실패/timeout 후 UNKNOWN_EFFECT와 먼저 GET observe, 새 version은 실제 Provider 관측과 결속. 다중 Secret bulk는 필요 시 후속 별도 작은 PR로 분리.
> 6. 새 Cloudflare 전송 클라이언트는 안전한 adapter 인터페이스 뒤에 숨긴다. 계정·Worker·env 혼동, 멀티 Writer, 시크릿이 없는 경우, permission 401/403, timeout, 로그 누출, 재전송, 현재 활성 배포 ID·traffic weight 변경을 부정 테스트. Version URL 실제 공유 리소스 접근 금지.
> 7. Go 1.22, Linux ARM64/AMD64 소스 테스트 우선. `verify.sh --suite unit/docs/policy`를 영향 범위만 검증하고, 별도 승인 없이 GitHub-hosted CI 실행, 운영 Cloudflare 요청, credential 변경, Oracle VM 생성, Release, PR Ready/Merge를 실행하지 말 것.
> 8. 마지막에 (a) HEAD/base, (b) 새/변경 파일, (c) 실제 실행 테스트·카운트, (d) fake Wrangler invoked argv **이름만**, (e) 본선/배포/계정·Vault 불변 증거, (f) UNKNOWN_EFFECT/재조정, (g) 진짜 Provider 검증의 남은 조건을 보고. 게시·병합은 명시 승인이 있어야 한다.

## 최소 인수 Gate

- `plan`: Keychain/Resolver.Get, 네트워크 및 원격 변경 **0회**.
- `stage`: 가짜 Wrangler `versions secret put` 정확히 1회, `secret put/deploy` **0회**; 비밀값은 stdin으로만.
- Journal: 허가/동일성 검사를 통과하기 전 변경 0; 작업 전 durable intent; 불명 결과 UNKNOWN_EFFECT; 재시도 전에 외부 관측.
- 별도 승인된 disposable Worker 실전 파일럿: 새 version만 생성·활성 배포 불변·오류 없이 삭제 가능할 때만 scoped acceptance.
- 기존 PR #47의 K09-01~03 회귀 방지; **합성 PASS ≠ 운영 승인**.
