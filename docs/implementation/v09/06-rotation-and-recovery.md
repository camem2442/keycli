---
status: NOT VERIFIED
scope: proposed-rotation-and-reconciliation
---

# 대상별 자격증명 회전과 effect 재조정

## 상태를 분리한다

**Delivery operation:** `PLANNED → ADMITTED → INTENT_WRITTEN → STAGING → STAGED → VERIFYING → VERIFIED`. 이후 별도 workspace-infra Delivery가 `PROMOTED`를 기록할 수 있다. `PLANNED`는 정책·스키마 검사만 성공했다는 뜻이지 실 인증 PASS가 아니다.

**Credential generation:** `ISSUED → STAGED → ACTIVE → RETIRING → REVOKED`. WIF 구성은 장기 키 세대가 아니며, 단기 access token은 공급자/SDK가 갱신한다. WIF 키 회전과 일반 `kc rotate`를 동일한 작업으로 취급하지 않는다.

**예외:** `BLOCKED`(권한·정책·불일치), `STALE`(HEAD/원격 활성 배포/세대 전진), `UNKNOWN_EFFECT`(타임아웃·관측 불명), `ROLLBACK_REQUIRED`(활성화 후 장애), `REUSE_BLOCKED`(원격 호스트 정리 실패). 임의 예외를 `STAGED`나 `VERIFIED`로 바꾸지 않는다.

## State transition 원칙

| 전이 | 조건 | 금지하는 자동 행동 |
|---|---|---|
| PLANNED → ADMITTED | trusted workspace host, exact target·HEAD·scope·budget | CLI `--approved` 문자열로 우회 |
| ADMITTED → INTENT_WRITTEN | 사전 원장 durable + lock + provider identity | 원장 실패에도 원격 호출 |
| INTENT_WRITTEN → STAGING | 중복 시도 없음, 원격 기준 일치, 필요한 Secret만 resolve | 전체 Vault 로드 |
| STAGING → STAGED | 공급자 성공 + 동일 대상 remote state 관측 | CLI exit 0만 믿기 |
| STAGED → VERIFIED | 실제 scope·인증 기능·버전 불변 검증 | 이름 존재만으로 값 동일 주장 |
| VERIFIED → PROMOTED | 별도 Delivery 권한·검증·Owner 판단 | KeyCLI 독자 `deploy` |
| ACTIVE → RETIRING → REVOKED | 모든 의존 consumer/rollback window 종료, 실제 폐기 확인 | 불명확한 소비자가 있는데 이전 키 삭제 |

## Operation ID와 기록

`operation_id`는 새 의도별 고유 ID로 충돌하지 않도록 생성한다. 생성 시 고정된 `repo, exact SHA, provider account/resource/env, source alias, destination, requested effect, expected remote generation, host admission digest`를 **private journal**에 결속한다. 재실행은 같은 Operation ID를 요구하며, 동일 대상이라도 새 승인·다른 SHA·다른 generation이면 새로운 Operation으로 구분한다.

`~/.kc` 경로의 작업 원장은 0600, 부모 디렉터리 안전성·symlink·동시 쓰기 검사, 독점 잠금, fsync+atomic rename을 요구한다. 공개 receipt는 **비밀값, raw token, 값을 재식별하기 쉬운 해시, 실제 Vault 상태**가 아닌 작업 ID·target ref·remote version ID·source revision·status·policy digest 등 제한된 메타데이터만 가진다. 기존 `ledger.json`의 HMAC 원장은 출처/변경 감지를 유지하되 작업별 journal과 섞어 부정확한 '전송 성공'을 만들지 않는다.

## 재조정의 정확한 알고리즘

1. `INTENT_WRITTEN`만 있고 송신 기록 없음: remote 확인 전 자동 재송신 금지.
2. `STAGING` 또는 네트워크 timeout: 운영 account/Worker/repo/instance의 실제 현재 상태를 읽고, **오직 동일 operation에 의해 생성된 새 버전/비밀**로 연결할 수 있을 때만 STAGED. 구분 불가면 UNKNOWN_EFFECT 유지.
3. 원격 새 버전 확인됨 + 로컬 journal 저장 실패: journal 손실을 성공으로 숨기지 않고 원본 증거로 복구 및 owner 경고; 멱등성 증명 전 재호출 금지.
4. 다중 대상 일부 성공: 대상별 결과로 PARTIAL, 이미 성공한 대상을 무조건 다시 쓰지 않음.
5. 현재 활성 deployment가 계획 값에서 변경: STALE, 이전 계획 폐기/신규 계획 필요.
6. Secret 내용을 공급자 GET이 반환하지 않는 경우: 이름·버전 존재만으로 값 일치 확정 불가. 기능적 최소권한 인증 프로브 또는 공급자 지원 증명을 이용; 미확인은 NOT VERIFIED.

## 회전 전략

1. **Manual-assist**: 사람이 새 제한 키를 발급/KeyCLI에 `set`, KeyCLI가 후보 전달·검증까지 수행.
2. **Provider API가 허용되는 서비스 계정**: 해당 API 발급 정책을 검증한 뒤 새 자격증명 발급 자동화 가능; 운영 기본 비활성·한도 설정.
3. **WIF**: 장기 키 회전이 아니라 identity mapping·JWKS·service account 상태/사용 권한 검증. 서명 키 회전은 IdP 공식 계약을 따른다.
4. **Cloudflare Worker Secret**: 새 version stage, 별도 promotion, 기능 검증 후 이전 키 폐기. 이전 Worker Version rollback과 시크릿 회수 가능성을 함께 따져야 한다.
5. **GitHub Actions**: GITHUB_TOKEN과 OIDC는 Job 수명 중심, KeyCLI가 토큰 복제·장기 저장하지 않는다.
6. **OCI VM**: 단기 Runner/Instance Principal 인증이 정상이라면 OpenAI 장기 키 복제 필요 없음.

## 실패 안전성 회귀

동일 작업 중복 호출, 두 Mac 프로세스 동시 실행, SIGKILL 전후 원장 상태, provider 타임아웃, 네트워크 간헐 실패, 위조된 Receipt, 다른 Worker가 발행한 version, stale HEAD, 비용 게이트 불명확, 시크릿 원문 출력, 미확인 키 폐기, rollback window 이전 revoke를 테스트한다. 실제 사용자 Keychain/Vault/운영 Worker·VM을 테스트 fixture로 사용하지 않는다.
