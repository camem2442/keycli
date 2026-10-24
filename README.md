# keycli (`kc`)

**Product Goal:** AI 코딩 에이전트와 사람이 토큰·API 키·키체인 암호를 승인 창이나 복사·붙여넣기 없이 필요할 때 바로 꺼내 쓰게 하는, macOS와 헤드리스 Linux 공용 터미널 도구.

> 상태: **설계 문서만 있음.** 코드는 아직 없다. 현재 작업은 [HANDOFF.md](HANDOFF.md).

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

## License

Private.
