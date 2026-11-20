---
status: proposed-security-gap-roadmap
owner: keycli
date: 2026-11-20
baseline_main: 5650d0a92cfe4f15cf2f086e20bb7cfbf7e0553f
scope: docs-only-not-operational
---

# KeyCLI: Apple 암호·패스키·에이전트 로그인 보안 경계 비교

**목적:** 개인 Mac에서 Apple 암호 앱을 기본 credential/passkey 저장소로 유지하면서, AI 에이전트가 사이트에 접근할 때 비밀값을 모델·CLI·MCP·로그로 넘기지 않고 요청→독립 승인→Mac 소유 브라우저의 인증→상태 반환을 분리한다. **KeyCLI는 Apple Passwords나 1Password for Agents의 보안 특성을 구현 완료한 상태가 아니다.** 동일 UID의 정책 검사는 격리가 아니다.

**기준 관측:** keycli main 5650d0a92cfe4f15cf2f086e20bb7cfbf7e0553f, 정식 설치 릴리스 v0.8.0. [현재 로드맵](keycli-current-roadmap-2026-11-16.md)은 소스 v0.10과 운영/인수를 구분한다. 본 문서는 **미병합 Draft #81·#82·#83·#84**가 이후 병합됐다는 주장이 아니다.

## A. 공식 1Password for Agents가 실제 제공하는 것

2026-11-20 공식 지원 문서 기준, 승인 가능한 **Personal/Private vault의 Login** 항목에 한해 에이전트가 요청하면 **사용자가 1Password 앱에서 해당 항목을 승인**하고, 에이전트 환경의 브라우저 확장이 자격 증명을 필드에 직접 채운다. 이 제품은 사용자 이름·암호·OTP는 다루지만 **passkeys와 SSO(Login with provider)는 아직 지원하지 않는다**. 개별 승인은 한정된 기간(문서상 최대 30일) 재사용될 수 있고, 연결 해제로 철회할 수 있다. 승인과 함께 서비스 접속 뒤의 행위까지 보호하는 것은 아니다.

참고: https://support.1password.com/1password-for-agents/ · https://support.1password.com/1password-for-agents-security/ · CLI: https://1password.com/developers/cli

KeyCLI가 모방할 **속성**은 에이전트의 임의 비밀 조회 금지, 사람/독립 승인, 사이트·항목 범위, 제한 시간, 브라우저 내 비밀 입력, 회수 가능성이다. **1Password의 confidential-computing enclave·키 분할 암호화 모델과 동등하다는 주장은 하지 않는다.**

## B. 현재 기능과 격차

표기: **MAIN** = 현재 main 동작, **DRAFT** = 미병합 코드 제안, **EXTERNAL** = OS/Apple 소유, **HOLD** = 운영 미검증, **NONE** = 미구현. 별도 표기 없는 DRAFT는 실제 개인 비밀번호·패스키를 읽거나 사용한 적이 없다.

| 요구·기능 | Apple / 1Password 공식 성격 | KeyCLI 실제 상태 | 결정/남은 장벽 |
|---|---|---|---|
| Apple 암호·패스키 보관 | Apple Passwords/iCloud Keychain 소유 | **EXTERNAL** | Apple 저장소 유지, 전량 복제하지 않음 |
| Mac Keychain 단일 secret 조회 | OS Keychain·1Password CLI | **MAIN** | 정책은 실수 방지, 같은 UID 방어 아님 |
| age/env/file 보관 및 kc exec 주입 | 1Password CLI op run 유사 | **MAIN** | 개발 API 자격증명용, 웹 계정 암호 기본 export 금지 |
| 에이전트별 이름 allow/deny·감사 | 1Password 접근 통제 | **MAIN**, 약한 경계 | KC_AGENT/config 변조·직접 Keychain 접근 가능성 명시 |
| Apple Passwords CSV 내보내기 | Apple Mac 기능, **평문 파일** | **EXTERNAL** | 패스키 자체/Apple ID/OTP seed 대량 이관으로 취급 금지 |
| CSV의 단일 계정 수용·기본 읽기 전용 | Apple export 계정 항목 | **DRAFT #81** | 최대 1 MiB, owner only 0400/0600, 1레코드/host 엄격 검증 |
| import 명시 쓰기·원본 보존 | 사용자 이관 | **DRAFT #81** | interactive Mac, acknowledge same-UID risk, SetIfAbsent; 대량 이관 불허 |
| 요청 사이트·경로·활동의 typed intent | 에이전트 요청 인터페이스 | **DRAFT #82** | 작은 JSON/중복·추가 키 거부/짧은 TTL, 내용·cookie 미포함 |
| 독립 VerifyAndReserve before browser | 1Password per-item 승인 | **DRAFT #82** | 원본 Goal/Grant/Claim + actor/site/path TTL/1회 binding 실제 검증 필요 |
| 사람 소유 TOTP filler | 1Password for Agents OTP | **DRAFT #83** | seed는 모델·CLI로 전송 안 함; 고위험 계정은 사람 UI |
| Apple passkey/Touch ID 인증 | Apple 시스템 사용자 확인 | **EXTERNAL + HOLD** | macOS 네이티브/브라우저 인증 UI 위임, 우회/가짜 생체 승인 불가 |
| Chrome owner-only 격리 프로필 | 브라우저 세션과 로그인 UI | **DRAFT #84** | 0700 프로필/사이트 allowlist, **OS 사용자 격리는 아님** |
| Chrome 내 Apple 암호 확장 실제 autofill | Apple 브라우저 확장 | **HOLD** | 원본 Mac GUI·확장/AutoFill/2FA 실측 전 기능 주장 금지 |
| 사용자 interactive 2FA 기간 | 별도 확인이 필요한 단계 | **DRAFT #84** | 0–120s owner window, 무응답→HUMAN_ACTION_REQUIRED |
| 쿠키/DOM/스크린샷/CDP 모델 비노출 | 비밀·세션 정보 최소화 | **DRAFT #82/#84** | API 응답 화이트리스트와 브라우저 외부 우회 방어 실측 |
| Apple passkey private key 자체 export/import | WebAuthn authenticator 소유 | **NONE / 금지** | private key를 kc, 클라우드, 에이전트로 복제하지 않음 |
| origin/RP ID에 강결합된 WebAuthn | Apple AuthenticationServices | **EXTERNAL** | macOS 별도 confirmation, third-party domain 사용 제약 확인 |
| OTP·재인증 이후 세션 관리/로그아웃 | 웹 서비스와 브라우저 소유 | **DRAFT / HOLD** | TTL, tab/profile identity, 종료·철회·로그아웃 측정 |
| 승인 철회·변경된 계정·재사용 차단 | 1Password agent integration revoke | **HOLD** | durable nonce/claim revoke + 현재 호스트 원본 확인 |
| 독립 OS 실행자 / credential ACL | 호스트 보안 경계 | **HOLD** | 새 사용자 필수 아님; 같으면 OS 샌드박스·권한 분리 검증 필수 |
| 실제 웹사이트 로그인 완료 증거 | 사이트 세션이 최종 소유 | **NOT VERIFIED** | 동일 출처·세션 결과 원본, 로그에 비밀·cookie 없음 |

중요: Apple 공식 문서의 **CSV 내보내기**는 사이트 비밀번호 목록에 관한 것이며 passkey material export의 증거가 아니다. passkey는 원본 Apple authenticator가 생성·보관하고 서비스의 RP ID/사용자 확인이 필요하다.
공식: https://support.apple.com/en-ca/guide/passwords/mchl35b12625/3.0/mac/27 · https://support.apple.com/guide/passwords/the-passwords-app-mchl901b1b95/mac · https://developer.apple.com/documentation/authenticationservices/supporting-passkeys

## C. 최소 위협 모델 (반드시 부정 케이스로 시험)

| 공격/경합 | 실패 시 보호해야 할 것 | 필요 증거 |
|---|---|---|
| 에이전트가 kc get, 환경변수, shell로 우회 | 승인되지 않은 암호·시드·cookie | agent worker는 Keychain/age/Chrome/CDP 직접 접근 불가, 별도 OS 경계 |
| same UID 에이전트가 설정/파일을 읽고 변조 | OS 사용자 계정 전체 | Owner broker와 agent process의 실제 권한 차이, protected files/peer attest |
| 모델이 URL로 다른 사이트·서브도메인을 요청 | 사이트 간 암호 채우기 | HTTPS origin/RP ID 정확 비교, wildcard/query/redirect/iframe deny |
| TOTP 시드·코드가 모델·로그로 유출 | 일회용 로그인 코드 | native filler 직행, 반환은 fixed status, seed read 전 authority check |
| 이전 Grant·Claim·nonce 재사용 | 무승인 재로그인 | source head/host/site/action/session/TTL 재확인, one-shot reservation |
| 세션 생성 뒤 lost reply/동시 writer | 중복 로그인·부분 효과 | durable UNKNOWN, 새 효과 retry 없음, owner 상태 read-only reconcile |
| 프로필 symlink/hardlink·다른 UID·기존 개인 프로필 혼입 | 기본 개인 Chrome 및 iCloud 데이터 | 0700 owner path, lstat·inode 검증, 기존 프로필 우회 접근 차단 |
| Passkey 2FA 시스템 프롬프트를 스크립트로 우회 | 사용자 확인 경계 | 자동 성공 금지; 시스템 UI 확인을 사람 입력으로 기록 |
| 사이트 로그인 후 악의적 페이지 내용·공격 | credential/세션 탈취 | agent 활동 scope 별도 제한; 자격증명 주입만으로 이후 행동 안전 보장 불가 |
| 분실된 노트북·로그/백업 CSV 잔류 | 비밀번호 평문 파일 | CSV 사용 전/후 임시 수명·제거 계획·사용자 동의, 로그/커밋에 값 0 |

## D. 단계별 개발·수용 로드맵 (운영 효과는 Gate 뒤에만)

| 단계 | 재사용할 현재 소스 | 산출할 최소 기능 | 성공/보류 경계 |
|---|---|---|---|
| **S0 — 출처 동결** | main v0.8 pin / Draft #81–#84 | Apple/1Password 공식 보안 조건·현재 기능 인벤토리 고정 | 원본 버전·정확 SHA, 소스와 설치본 구분 |
| **S1 — 순수 계약** | #81 CSV, #82 intent, #83 OTP | malformed/unknown/TTL/OTP/지정 사이트 실패 우선 거부 | 동일 clean HEAD의 KeyCLI verify docs/policy/Go owning tests |
| **S2 — 호스트 권한 격리** | workspace-infra 원본 Goal/Grant/Claim, keycli HostBridge/Gateway | 실 owner broker vs agent 분리, peer/replay/seatbelt/same UID 차단 | synthetic만으로 실제 Mac 권한 격리 PASS 주장 금지 |
| **S3 — 읽기 전용 브라우저** | #84 dedicated Chrome owner profile | `session_status`만 허용. `read_page`는 별도 출력 redactor 검토 전까지 명시적으로 거부하며, 부정 테스트로 확인한다. CDP/cookie/HTML 모델 출력 금지 | 단일 사이트 Mac native headed 1회, site mismatch negative, `read_page` 거부 확인 |
| **S4 — 사용자 개입 로그인** | Apple Passwords extension/Touch ID, #83 native filler | 대화형 사람 승인 → 브라우저 정상 세션 → 정형 status | 실제 Mac 1 low-risk 계정, 비밀 비노출, 2FA 미수용 사이트는 human required |
| **S5 — 제한된 무인 재사용** | 독립 Grant/Claim TTL·회수, original ledger | 사전 승인된 저위험 세션 재사용·종료/철회 | time bound, prior-grant revoke, second-agent deny, loss/reconcile, real login/logout |
 
**중단 조건:** 금융·결제·계정 소유 변경·복구 코드/관리자 계정, unknown-origin, OS 격리 미검증, Apple 암호 전체 CSV 자동 가져오기, OTP/패스키 모델 원문 전달, 무인 Touch ID 우회, 실 서비스 자격증명 이용한 부정 테스트. 허가된 소규모 disposable 계정만 실기기 시험한다.

## E. 제품 책임과 사용자 경험

- **KeyCLI:** credential ref, metadata-only audit, typed request, bounded policy enforcement, gateway identity; 새 Vault/중앙 Authority/임의 브라우저 서버 만들지 않는다.
- **Apple Passwords / Browser:** password/passkey/OTP UI와 민감 세션 값 보관; passkey 비밀키 내보내기 시도 금지.
- **workspace-infra:** 원본 Goal/Grant/Writer/Claim 및 호스트 입장, deny/grant 전개, trusted host.
- **FlowSlate(미구현):** 사용자에게 사이트·요청 액션·승인/대기/거부 상태를 표시하는 인터페이스; credential 접근이나 2FA 생성 중복 금지.
- **Agent:** 제한된 요청 및 승인 뒤 결과 기반 작업만. 브라우저 백엔드·CDP·쿠키·암호·OTP·Passkey assertion을 받지 않는다.

**정책 결정:** 추가 Mac 사용자 생성은 기본 전제가 아니다. 하지만 **전용 Chrome 프로필과 같은 UID의 파일 권한만으로 강한 격리를 달성했다고 주장하지 않는다**. 별도 사용자 대신 사용할 실제 OS 경계(Seatbelt, sandbox, 비밀 접근 경로·peer 검사)가 없거나 우회 가능하면 OS 인증을 에이전트에게 위임하지 않고 사람의 Apple UI를 유지한다.

## F. 1차 출처·관련 Draft

- 1Password for Agents 기능 및 미지원 passkeys: https://support.1password.com/1password-for-agents/
- 1Password for Agents 승인/암호 전달·보안 경계: https://support.1password.com/1password-for-agents-security/
- 1Password 개발자 CLI: https://1password.com/developers/cli
- Apple Mac 암호 CSV: https://support.apple.com/en-ca/guide/passwords/mchl35b12625/3.0/mac/27
- Apple Passwords: https://support.apple.com/guide/passwords/the-passwords-app-mchl901b1b95/mac
- Apple passkeys/API: https://developer.apple.com/documentation/authenticationservices/supporting-passkeys
- Chrome debugger permission: https://developer.chrome.com/docs/extensions/reference/api/debugger
- [#81 bounded CSV](https://github.com/camem2442/keycli/pull/81) · [#82 browser intent](https://github.com/camem2442/keycli/pull/82) · [#83 owner TOTP](https://github.com/camem2442/keycli/pull/83) · [#84 owner Chrome](https://github.com/camem2442/keycli/pull/84)
- 현재 KeyCLI 경계: [security model](../contracts/security-model.md) · [current roadmap](keycli-current-roadmap-2026-11-16.md).

**이번 범위:** 문서 1건과 목차 링크만 변경. 비밀번호/OTP/패스키/계정·브라우저/권한/릴리스·PR #81–#84 변경 없음. 이것은 보안 인수 영수증이 아니다.
