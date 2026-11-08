# KC-CURSOR-CLOUD-P3-01 — Cursor Cloud 파일럿 영수증 검토 (2026-11-08)

- 게이트: `KC-CURSOR-CLOUD-P3-01`
- 실행: Cursor Cloud, Linux VM (`cursor`, kernel 6.12.113+, x86_64)
- 바이너리: 릴리스 `v0.7.2` / `kc-linux-amd64`
- 저장소: `camem2442/keycli`
- 판정: **PASS** (allowlist 계약 범위) — 단, 아래 두 항목은 **NOT VERIFIED**로 남긴다

## 1. 제출 영수증

```json
{
  "head_sha": "167814a5f35567888240a347a19637c92dc3e933",
  "pin_sha": "043029f",
  "pin_match": false,
  "worktree_clean": true,
  "release_tag": "v0.7.2",
  "binary_sha256": "3702c11070bc4c53975b5e75eb85ff7d17cb45974e06581f541276c17ab7c458",
  "checksum_verified": true,
  "policy_digest": "sha256:9c9b1ef17b5fbde400d7f5da0e0e9b89c8165c42bbeee71009dd323e92045ade",
  "cases": [
    {"id": "A_allowed",   "name": "kc.policy.pilot",   "exit": 0, "child_ran": true},
    {"id": "B_forbidden", "name": "kc.policy.blocked", "exit": 5, "child_ran": false},
    {"id": "C_missing",   "name": "kc.policy.absent",  "exit": 2, "child_ran": false}
  ],
  "leak_check": {"synthetic_value_in_output": false, "synthetic_value_in_audit": false},
  "audit": {"records": 7, "outcomes": ["denied", "notfound", "ok"]},
  "cleanup": {"names_removed": true}
}
```

## 2. 독립 확인

| 항목 | 확인 방법 | 결과 |
| --- | --- | --- |
| `binary_sha256` | 게시된 `SHA256SUMS`를 직접 내려받아 대조 | **일치** — `3702c110…c458` |
| `release_tag` 자산 존재 | `gh release view v0.7.2` | `kc-linux-amd64`, `SHA256SUMS` 존재 |
| 케이스 3종 | 영수증 값과 프롬프트 기대값 대조 | A(0,true) / B(5,false) / C(2,false) **일치** |
| audit outcome 집합 | 기대 `ok`/`denied`/`notfound` | **일치** |
| `policy_digest` | allow 줄 정규화 여러 방식으로 재계산 | **재현 실패 (NOT VERIFIED)** |
| `head_sha` | 저장소 이력 대조 | `167814a` = PR #30 이전 main. `043029f`보다 4커밋 뒤 |

`dist/SHA256SUMS`(로컬 빌드 산출물)와 게시본은 값이 다르다. 이 검토는 **게시된 릴리스 자산**을 기준으로 했다.

## 3. 해석

- A/B/C는 정책 계약의 핵심을 그대로 확인한다: 허용은 자식을 실행하고, **거부(5)와 누락(2)은 자식을 실행하지 않는다**.
- 합성 시크릿만 사용했고, 값이 출력·audit에 남지 않았다고 보고됐다.
- `v0.7.2`는 `deny` 기능 이전 바이너리이므로, 이 PASS는 **allowlist 계약**에 한정된다.

## 4. 남긴 NOT VERIFIED

1. **`policy_digest` 재현 불가** — 영수증의 해시를 allow 줄에서 재계산할 수 없었다. 프롬프트의 "sorted lines" 정의가 모호했던 것이 원인일 수 있다. 해시 입력을 그대로 출력하면 닫힌다.
2. **`pin_match=false`** — 저장소가 `043029f`가 아닌 `167814a`에서 체크아웃됐다. 이 파일럿은 릴리스 바이너리만 사용하므로 결과에는 영향이 없지만, 영수증을 소스 커밋에 묶지는 못한다.
3. 다중 에이전트 동시 적용, `deny` 기능, 실제 provider 자격증명, `kc push`, Cloudflare 변경 — 모두 **NOT VERIFIED**.

## 5. 판정

| 항목 | 값 |
| --- | --- |
| 게이트 | **PASS** (allowlist 계약, 단일 Linux VM) |
| 범위 | 합성 시크릿, `kc exec --require --only` 3케이스 |
| 남은 확인 | `policy_digest` 재현, `pin_match` 정렬, `deny` 별도 라운드 |
