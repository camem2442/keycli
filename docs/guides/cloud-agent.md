---
status: implemented
---
# Cursor Cloud Agent 환경 (KC-40 / V07-A3)

keycli Cloud Agent **빌드**는 릴리스 바이너리만 설치한다. 소스 컴파일(`build-dev`)은 로컬 개발용이다. private **ai-core**는 선택 사항이며, clone 실패로 **install이 실패하지 않는다**.

## 빌드 시 (`install`)

저장소 루트의 [`.cursor/environment.json`](../../.cursor/environment.json) 또는 대시보드 install 명령:

```bash
bash scripts/cloud-install.sh
```

동작:

1. `KC_INSTALL_REF`(기본 `v0.6.0`)로 [`scripts/install.sh`](../scripts/install.sh) 실행 — GitHub release + `SHA256SUMS` 검증.
2. `kc`를 `~/.local/bin`에 두고 `/usr/local/bin`에 symlink (login PATH).
3. `kc doctor --json` smoke (exit 0).

### 환경 변수 (대시보드 Secrets / Variables)

| 변수 | 필수 | 설명 |
|------|------|------|
| `KC_INSTALL_REF` | 아니오 | 설치할 release tag (기본 `v0.6.0`). |
| `KC_CLOUD_CLONE_AI_CORE` | 아니오 | `1`이면 install 중 ai-core clone 시도. 기본 `0` (keycli만). |
| `KC_CLOUD_AI_CORE_DIR` | 아니오 | clone 경로 (기본 `~/ai-core`). |
| `GITHUB_TOKEN` | ai-core 시 | `repo` 권한으로 `camem2442/ai-core` 읽기. 값을 커밋·로그에 넣지 않는다. |

`GITHUB_TOKEN`은 [x-access-token URL](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) 패턴으로만 사용한다 (`scripts/cloud-clone-ai-core.sh`). bare `https://github.com/...` clone은 private repo에서 실패할 수 있다.

대시보드에서 ai-core를 쓰려면 **Repository dependencies**에 `camem2442/ai-core`를 추가해 빌드/런타임 토큰 scope를 맞춘다.

## 로컬 개발용 (빌드 아님)

```bash
cd /path/to/keycli
bash scripts/lifecycle.sh build-dev --install
export PATH="$HOME/.local/bin:$PATH"
kc doctor --json
```

`build-dev`는 워킹 트리 버전 문자열을 쓰며, Cloud snapshot **install** 경로와 분리한다.

## 런타임 (에이전트 세션)

install에 ai-core를 넣지 않았으면:

```bash
export KC_CLOUD_CLONE_AI_CORE=1   # GITHUB_TOKEN은 대시보드 Secret
bash scripts/cloud-clone-ai-core.sh
```

또는 `kc`에 GitHub 시크릿이 있으면:

```bash
kc exec --only github -- git clone --origin github \
  https://github.com/camem2442/ai-core.git ~/ai-core
```

검증:

```bash
test -f ~/ai-core/.cursor/rules/keycli.mdc
cd ~/ai-core && git remote -v   # remote 이름은 github
```

## 드래프트 빌드 검증

PR 브랜치를 검증할 때 (DB-managed 환경):

```text
cursor-cloud-trigger-environment-build
  environmentJson.install = bash scripts/cloud-install.sh
  refs = [{ repoUrl: github.com/camem2442/keycli, ref: <branch> }]
```

성공 후 `propose-environment-json` + 대시보드 **Save**. Mac install smoke는 이 문서 범위 밖 (**NOT VERIFIED**).

## 관련

- [agents.md](./agents.md) — `kc push cursor`, transfer
- Project store: `docs/cloud-ai-core-checkout.md`
