# keycli (`kc`)

**Product Goal:** AI 코딩 에이전트와 사람이 토큰·API 키·키체인 암호를 승인 창이나 복사·붙여넣기 없이 필요할 때 바로 꺼내 쓰게 하는, macOS와 헤드리스 Linux 공용 터미널 도구.

> 상태: **M1 구현됨.** 진행·검증 상태는 [HANDOFF.md](HANDOFF.md).

## 사용 흐름 (설계)

```bash
# 사람: 한 번 저장
kc set openai --clip          # 클립보드 값을 저장하고 클립보드 비움
kc link groq keychain:local.study.ai.groq

# 에이전트: 필요할 때 꺼냄
kc ls                         # 이름만
GH_TOKEN=$(kc get github) gh pr list
kc exec -- npm run deploy     # 환경변수로 주입

# 사람: 복사
kc cp                         # 목록에서 골라 복사, 30초 뒤 자동 삭제
```

## 시작

| 질문 | 문서 |
|---|---|
| 에이전트 작업 규칙 | [AGENTS.md](AGENTS.md) |
| 현재 작업·인계 | [HANDOFF.md](HANDOFF.md) |
| 문서 목차 | [docs/README.md](docs/README.md) |
| CLI·출력·종료 코드 계약 | [docs/contracts/cli.md](docs/contracts/cli.md) |
| 아키텍처·확장 지점 | [docs/contracts/architecture.md](docs/contracts/architecture.md) |
| 보안 모델과 한계 | [docs/contracts/security-model.md](docs/contracts/security-model.md) |

## 이름

저장소·패키지 이름은 `keycli`, 명령은 `kc`. 설치 시 `kc`와 `keycli` 두 이름을 모두 제공한다. 근거는 [결정 0001](docs/decisions/0001-design-choices.md).

## 언어·의존성

순수 Go(`CGO_ENABLED=0`). 값 조회는 네트워크 불필요, `kc push`만 네트워크 사용. cobra, filippo.io/age, BurntSushi/toml, atotto/clipboard. macOS 키체인은 `/usr/bin/security` 직접 호출.

## 설치

**Cloud / Linux (권장, 인증 불필요):** GitHub Release 바이너리 또는 `go install`:

```bash
curl -fsSL https://raw.githubusercontent.com/camem2442/keycli/main/scripts/install.sh | bash
# 또는
go install github.com/camem2442/keycli/cmd/kc@latest
```

`KC_INSTALL_REF=v1.0.0`으로 특정 태그를 고를 수 있다. `gh release download`는 `gh` 로그인 시 선택 경로.

**Mac (로컬 개발):** `bash scripts/lifecycle.sh build-dev --install` → `~/.local/bin/kc` (+ `keycli` symlink). 검증: `./verify.sh --suite release`.

**제거:** `bash scripts/lifecycle.sh uninstall`

## Linux 빠른 시작

```bash
# age identity 생성 (stdout에 한 번 출력; `kc init`은 추후 마일스톤)
export KC_KEY="$(go run ./cmd/kc keygen 2>/dev/null)"

# 또는 age-keygen(1)으로 생성한 identity 한 줄을 KC_KEY에 넣기

printf 'my-token' | kc set demo
kc get demo
kc ls --json
```

macOS에서는 `KC_KEY` 대신 키체인 항목 `kc.age` / 계정 `kc`에 age identity를 저장할 수 있다(**NOT VERIFIED**: 실 Mac).

테스트 전용: `config.toml`에 `allow_test_hooks = true` 또는 `-tags=kc_testhooks` 빌드일 때만 `KC_SECURITY`, `KC_TEST_KEYCHAIN`, `KC_CLIP_*` 오버라이드가 동작한다. Linux CI는 [cmd/fakesecurity](cmd/fakesecurity)를 `KC_SECURITY`로 가리킨다.

## License

[MIT](LICENSE)
