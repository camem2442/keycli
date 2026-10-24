---
status: accepted
date: 2026-10-24
---
# 0002 M1 구현 메모

## config 기본값

`order`에 `env`가 있어도 `[backend.env]` 항목이 없으면 env 백엔드가 인스턴스화되지 않았다. 기본 config에 `env` 타입 백엔드를 명시했다.

## keychain 쓰기 네임스페이스

ai-core 항목(`local.study.ai.*`)은 읽기 전용이다. 사람용 `set`/`rm`은 `kc.secret.<name>` 서비스에만 쓴다. 읽기는 `local.study.ai.<name>`과 `kc.secret.<name>`을 모두 시도한다. macOS `security` 동작은 **NOT VERIFIED**.

## age 파일 키 (macOS)

Linux는 `KC_KEY`만 지원한다. macOS 키체인에서 age 키를 읽는 경로는 M1에서 **DEFERRED**(**NOT VERIFIED**).

## link 명령

로드맵 M2 항목으로 CLI에서 제외했다. keychain 백엔드만 M1에 포함.
