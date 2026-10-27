# keycli (`kc`)

**Product Goal:** AI 코딩 에이전트와 사람이 토큰·API 키·키체인 암호를 승인 창이나 복사·붙여넣기 없이 필요할 때 바로 꺼내 쓰게 하는, macOS와 헤드리스 Linux 공용 터미널 도구.

> 상태: **v0.7.0** [출시](https://github.com/camem2442/keycli/releases/tag/v0.7.0) — `kc exec --require`, consumer bootstrap manifest, cloud install scripts. v0.6.0: [릴리스](https://github.com/camem2442/keycli/releases/tag/v0.6.0). 인수·GO/NO-GO: [v07-release-gate.md](docs/planning/v07-release-gate.md), [HANDOFF.md](HANDOFF.md), [roadmap](docs/planning/roadmap.md).

## 사용 흐름 (설계)

```bash
# 사람: 한 번 저장
kc set openai --clip          # 클립보드 값을 저장하고 클립보드 비움
kc link groq keychain:local.study.ai.groq

# 에이전트: 필요할 때 꺼냄
kc ls --json                  # 이름·출처 메타만 (정책 거부 이름은 기본 숨김)
kc audit --since 1h           # 읽기 감사 로그 (값 없음)
kc exec --only github -- gh pr list
kc rules --write AGENTS.md     # 에이전트 규칙 블록 삽입
kc doctor --json

# 사람: 복사
kc cp                         # 목록에서 골라 복사, 30초 뒤 자동 삭제

# 맥 → 클라우드
kc push cursor --env-id … TOKEN   # Cursor 환경 시크릿
kc push gh --user MY_TOKEN        # gh secret (stdin)
kc export --format dotenv A B       # 클립보드 dotenv
kc export A --to age1… | ssh vm kc import
```

## 시작

| 질문 | 문서 |
|---|---|
| 에이전트 작업 규칙 | [AGENTS.md](AGENTS.md) |
| 현재 작업·인계 | [HANDOFF.md](HANDOFF.md) |
| 문서 목차 | [docs/README.md](docs/README.md) |
| CLI·출력·종료 코드 계약 | [docs/contracts/cli.md](docs/contracts/cli.md) |
| 아키텍처·확장 지점 | [docs/contracts/architecture.md](docs/contracts/architecture.md) |
| 외부 백엔드 플러그인 | [docs/contracts/plugin-protocol.md](docs/contracts/plugin-protocol.md) |
| 보안 모델과 한계 | [docs/contracts/security-model.md](docs/contracts/security-model.md) |
| 에이전트별 설정 | [docs/guides/agents.md](docs/guides/agents.md) |

## 이름

저장소·패키지 이름은 `keycli`, 명령은 `kc`. 설치 시 `kc`와 `keycli` 두 이름을 모두 제공한다. 근거는 [결정 0001](docs/decisions/0001-design-choices.md).

## 언어·의존성

순수 Go(`CGO_ENABLED=0`). 값 조회는 네트워크 불필요. 네트워크는 `internal/target/`의 `push`와 `export --to-github`만 사용. cobra, filippo.io/age, BurntSushi/toml, atotto/clipboard. macOS 키체인은 `/usr/bin/security` 직접 호출.

## 설치

**Cloud / Linux (권장, 인증 불필요):** GitHub Release 바이너리 또는 `go install`:

```bash
curl -fsSL https://raw.githubusercontent.com/camem2442/keycli/main/scripts/install.sh | bash
# 또는
go install github.com/camem2442/keycli/cmd/kc@latest
```

`KC_INSTALL_REF=v1.0.0`으로 특정 태그를 고를 수 있다. release 바이너리 경로(curl·`gh release download`)는 릴리스 `SHA256SUMS`와 대조한 뒤에만 설치하며, 합계 파일이 없거나 불일치하면 중단한다. `gh release download`는 `gh` 로그인 시 선택 경로.

**Mac (로컬 개발):** `bash scripts/lifecycle.sh build-dev --install` → `~/.local/bin/kc` (+ `keycli` symlink). 검증: `./verify.sh --suite release`.

**Cursor Cloud Agent:** `bash scripts/cloud-install.sh` (릴리스 `v0.6.0` + SHA256, `kc doctor --json`). 상세: [docs/guides/cloud-agent.md](docs/guides/cloud-agent.md).

**제거:** `bash scripts/lifecycle.sh uninstall`

## Linux 빠른 시작

```bash
# age identity: 없으면 ~/.kc/receiver.key(0600)에 만들고, 있으면 그대로 사용한다.
# --print-identity는 identity(비밀값)를 stdout으로 낸다 — 로그에 남기지 말 것.
export KC_KEY="$(go run ./cmd/kc init --print-identity 2>/dev/null)"

# 또는 age-keygen(1)으로 생성한 identity 한 줄을 KC_KEY에 넣기

printf 'my-token' | kc set demo
kc get demo
kc ls --json
```

`kc init`(별칭 `keygen`)은 `KC_KEY` → `~/.kc/receiver.key` → 키체인 `kc.age` 순으로 **기존 identity**를 찾아 그 공개 recipient(`age1…`)를 stdout에 출력한다. identity가 없을 때만 새로 만든다. 따라서 스크립트에서 recipient가 필요하면 `kc init`을 반복해도 같은 값이 나온다.

macOS에서는 `KC_KEY` 대신 키체인 항목 `kc.age` / 계정 `kc`에 age identity를 저장할 수 있다(**NOT VERIFIED**: 실 Mac).

테스트 전용: `config.toml`에 `allow_test_hooks = true` 또는 `-tags=kc_testhooks` 빌드일 때만 `KC_SECURITY`, `KC_TEST_KEYCHAIN`, `KC_CLIP_*` 오버라이드가 동작한다. 실제 클립보드를 쓰는 clip 테스트는 `kc_testhooks` 태그에서만 컴파일되므로 태그 없는 `go test ./...`는 클립보드를 건드리지 않는다. Linux CI는 [cmd/fakesecurity](cmd/fakesecurity)를 `KC_SECURITY`로 가리킨다.

## License

[MIT](LICENSE)
