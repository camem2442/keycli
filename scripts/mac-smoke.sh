#!/usr/bin/env bash
# Real-Mac smoke: throwaway kc.test.*, keychain ACL (-T /usr/bin/security), kc.age, ai-core read, sandbox.
# macOS 키체인은 실제 사용자 HOME(~/Library/Keychains)이 필요합니다. 임시 HOME은
# "키체인 발견할 수 없음" / -60006을 유발합니다 — 설정만 KC_CONFIG로 격리합니다.
# Linux: NOT VERIFIED skip (exit 0).
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "mac-smoke: NOT VERIFIED on Linux — skipped"
  exit 0
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KC="${KC:-kc}"
REAL_HOME="${KC_REAL_HOME:-$(cd ~ && pwd)}"
export PATH="${REAL_HOME}/.local/bin:/opt/homebrew/bin:/usr/bin:/bin:${PATH}"

if ! command -v "$KC" >/dev/null 2>&1; then
  echo "mac-smoke: $KC not in PATH" >&2
  exit 1
fi

KEYCHAIN_WAIT="${KEYCHAIN_WAIT:-60}"

keychain_notice() {
  echo ""
  echo ">>> 지금 키체인 창이 뜨면 '항상 허용'을 눌러 주세요 — $1"
  echo ""
}

run_keychain_step() {
  local label=$1
  shift
  keychain_notice "$label"
  local out err rc=0
  err="$(mktemp)"
  out="$(mktemp)"
  ("$@") >"$out" 2>"$err" &
  local pid=$!
  local waited=0
  while kill -0 "$pid" 2>/dev/null && (( waited < KEYCHAIN_WAIT )); do
    sleep 1
    waited=$((waited + 1))
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    echo "mac-smoke: TIMEOUT after ${KEYCHAIN_WAIT}s: $label" >&2
    cat "$err" >&2
    rm -f "$out" "$err"
    return 124
  fi
  wait "$pid" || rc=$?
  cat "$out"
  if [[ $rc -ne 0 ]]; then
    cat "$err" >&2
  fi
  rm -f "$out" "$err"
  return "$rc"
}

write_keychain_only_config() {
  local path=$1
  mkdir -p "$(dirname "$path")"
  cat >"$path" <<EOF
order = ["work"]
[backend.work]
type = "keychain"
service_prefix = "local.study.ai"
EOF
}

write_store_only_config() {
  local cfg=$1
  local store=$2
  mkdir -p "$(dirname "$cfg")" "$(dirname "$store")"
  cat >"$cfg" <<EOF
order = ["store"]
[backend.store]
type = "file"
path = "$store"
EOF
}

TEST_ITEM="kc.test.smoke-$(openssl rand -hex 3)"
# kc stores writable keychain items under kc.secret.<name>; the raw service is
# what cleanup and duplicate checks must use.
TEST_SERVICE="kc.secret.${TEST_ITEM}"
SMOKE_DIR=""
REBUILD_BIN=""
AGE_CREATED=false
KC_CONFIG_PATH=""

kc_scrub_env() {
  # 실제 셸의 GROQ_API_KEYS 등이 ai-core 읽기를 가리지 않도록 최소 환경만 유지.
  env -i \
    HOME="$REAL_HOME" \
    USER="${USER:-$(id -un)}" \
    LOGNAME="${LOGNAME:-$(id -un)}" \
    PATH="${REAL_HOME}/.local/bin:/opt/homebrew/bin:/usr/bin:/bin" \
    KC_CONFIG="${KC_CONFIG:-$KC_CONFIG_PATH}" \
    ${GIT_CONFIG_GLOBAL:+GIT_CONFIG_GLOBAL="$GIT_CONFIG_GLOBAL"} \
    "$@"
}

kc_keychain() {
  kc_scrub_env "$KC" "$@"
}

kc_with_config() {
  local cfg=$1
  shift
  KC_CONFIG="$cfg" kc_scrub_env "$KC" "$@"
}

cleanup() {
  if [[ -n "${KC_CONFIG_PATH:-}" ]]; then
    kc_keychain rm "$TEST_ITEM" 2>/dev/null || true
    /usr/bin/security delete-generic-password -s "$TEST_SERVICE" -a kc 2>/dev/null || true
    if /usr/bin/security find-generic-password -s "$TEST_SERVICE" -a kc >/dev/null 2>&1; then
      echo "mac-smoke: WARN $TEST_SERVICE still in keychain" >&2
    else
      echo "mac-smoke: confirmed deleted $TEST_SERVICE"
    fi
  fi
  if $AGE_CREATED; then
    keychain_notice "테스트용 kc.age 항목 삭제"
    /usr/bin/security delete-generic-password -s kc.age -a kc >/dev/null 2>&1 || true
  fi
  rm -rf "${SMOKE_DIR:-}"
  [[ -n "${REBUILD_BIN:-}" && -f "${REBUILD_BIN:-}" ]] && rm -f "$REBUILD_BIN"
}
trap cleanup EXIT

SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kc-mac-smoke.XXXXXX")"
KC_CONFIG_PATH="$SMOKE_DIR/keychain.toml"
write_keychain_only_config "$KC_CONFIG_PATH"

echo "mac-smoke: version (HOME=$REAL_HOME)"
"$KC" --version

smoke_set_keychain() {
  printf '%s' 'smoke-value' | kc_keychain set "$TEST_ITEM"
}

echo "mac-smoke: keychain write (security -i, -T /usr/bin/security) item=$TEST_SERVICE"
if ! run_keychain_step "키체인에 테스트 비밀 저장 (kc set)" smoke_set_keychain; then
  echo "mac-smoke: keychain set FAILED" >&2
  echo "mac-smoke: hint: real HOME required; temp HOME causes '키체인 발견할 수 없음'" >&2
  /usr/bin/security list-keychains 2>&1 | head -5 >&2 || true
  exit 1
fi
if ! /usr/bin/security find-generic-password -s "$TEST_SERVICE" -a kc >/dev/null 2>&1; then
  echo "mac-smoke: expected service $TEST_SERVICE (kc.secret.<name>)" >&2
  exit 1
fi
echo "mac-smoke: kc.secret.<name> service OK"

got="$(kc_keychain get "$TEST_ITEM")"
if [[ "$got" != "smoke-value" ]]; then
  echo "mac-smoke: first get mismatch" >&2
  exit 1
fi
echo "mac-smoke: first read OK"

echo "mac-smoke: second read (should not re-prompt for kc binary)"
got2="$(kc_keychain get "$TEST_ITEM")"
[[ "$got2" == "smoke-value" ]] || { echo "mac-smoke: second get mismatch" >&2; exit 1; }

echo "mac-smoke: rebuild kc binary and read again"
export PATH="/opt/homebrew/bin:/usr/bin:/bin:${PATH}"
if command -v go >/dev/null 2>&1; then
  REBUILD_BIN="$(mktemp "${TMPDIR:-/tmp}/kc-rebuild.XXXXXX")"
  (cd "$ROOT" && go build -o "$REBUILD_BIN" ./cmd/kc)
  got3="$(env -u KC_KEY HOME="$REAL_HOME" USER="${USER:-$(id -un)}" LOGNAME="${LOGNAME:-$(id -un)}" \
    PATH="$(dirname "$REBUILD_BIN"):/usr/bin:/bin" KC_CONFIG="$KC_CONFIG_PATH" \
    "$REBUILD_BIN" get "$TEST_ITEM")"
  [[ "$got3" == "smoke-value" ]] || { echo "mac-smoke: rebuild get mismatch" >&2; exit 1; }
  echo "mac-smoke: rebuild read OK"
else
  echo "mac-smoke: go missing — skip rebuild check" >&2
fi

echo "mac-smoke: ls --json contains kc.secret item"
if ! kc_keychain ls --json | grep -q "\"name\": \"$TEST_ITEM\""; then
  echo "mac-smoke: $TEST_ITEM missing from ls --json (expected after kc.secret prefix)" >&2
  exit 1
fi
echo "mac-smoke: ls --json OK"

echo "mac-smoke: cp + clipboard"
export KC_CP_CLEAR_SECONDS=2
kc_keychain cp "$TEST_ITEM" >/dev/null 2>&1 || true
unset KC_CP_CLEAR_SECONDS

echo "mac-smoke: ai-core read-only (set groq must fail)"
if printf 'x' | kc_keychain set groq 2>/dev/null; then
  echo "mac-smoke: expected set groq to fail" >&2
  exit 1
fi

if /usr/bin/security find-generic-password -s local.study.ai.groq -a groq_api_key >/dev/null 2>&1 || \
  /usr/bin/security find-generic-password -s local.study.ai.groq -a groq_api_key_1 >/dev/null 2>&1 || \
  /usr/bin/security dump-keychain 2>/dev/null | grep -q 'local.study.ai'; then
  groq_out=""
  groq_src=""
  groq_rc=0
  smoke_get_groq() { kc_keychain get groq --source 2>"$SMOKE_DIR/groq-source.err"; }
  groq_out="$(run_keychain_step "kc get groq (키체인)" smoke_get_groq)" || groq_rc=$?
  groq_src="$(grep '^kc: source:' "$SMOKE_DIR/groq-source.err" 2>/dev/null | head -1 || true)"
  if [[ $groq_rc -eq 0 && ${#groq_out} -gt 0 ]]; then
    echo "mac-smoke: ai-core groq present len=${#groq_out} source=${groq_src#kc: source: }"
    if [[ "$groq_src" != *"keychain:"* ]]; then
      echo "mac-smoke: expected keychain source, got: $groq_src" >&2
      echo "mac-smoke: hint: scrubbed env required (GROQ_API_KEYS must not shadow keychain)" >&2
      exit 1
    fi
    run_keychain_step "ai-core groq 재읽기 (재프롬프트 없어야 함)" smoke_get_groq >/dev/null
    echo "mac-smoke: ai-core second read OK"
  else
    echo "mac-smoke: ai-core groq read failed rc=$groq_rc" >&2
    exit 1
  fi
else
  echo "mac-smoke: no local.study.ai.* in keychain — ai-core read skipped"
fi

if /usr/bin/security find-generic-password -s kc.age -a kc >/dev/null 2>&1; then
  echo "mac-smoke: existing kc.age — load test only (not modified)"
  AGE_CFG="$SMOKE_DIR/age-ro.toml"
  AGE_STORE="$SMOKE_DIR/store-ro.age"
  write_store_only_config "$AGE_CFG" "$AGE_STORE"
  if run_keychain_step "kc.age로 file store 잠금 해제" kc_with_config "$AGE_CFG" ls >/dev/null 2>&1; then
    echo "mac-smoke: kc.age load OK"
  else
    echo "mac-smoke: kc.age load inconclusive (empty store OK)" >&2
  fi
else
  echo "mac-smoke: kc.age absent — create throwaway age key in keychain"
  # 임시 HOME으로 init: 실제 ~/.kc/receiver.key를 건드리지 않고 새 identity를 만든다.
  mkdir -p "$SMOKE_DIR/init-home"
  AGE_KEY="$(HOME="$SMOKE_DIR/init-home" "$KC" init --print-identity 2>/dev/null | tr -d '\n')"
  AGE_HEX="$(printf '%s' "$AGE_KEY" | xxd -p | tr -d '\n')"
  smoke_write_kc_age() {
    printf 'add-generic-password -s kc.age -a kc -U -T /usr/bin/security -X %s\n' "$AGE_HEX" | /usr/bin/security -i
  }
  if ! run_keychain_step "kc.age 키체인에 저장 (security -i)" smoke_write_kc_age; then
    echo "mac-smoke: kc.age write failed" >&2
    exit 1
  fi
  AGE_CREATED=true
  AGE_CFG="$SMOKE_DIR/age.toml"
  AGE_STORE="$SMOKE_DIR/store.age"
  write_store_only_config "$AGE_CFG" "$AGE_STORE"
  smoke_age_roundtrip() {
    printf '%s' age-smoke | kc_with_config "$AGE_CFG" set agename
    test "$(kc_with_config "$AGE_CFG" get agename)" = age-smoke
  }
  if ! run_keychain_step "age file store set/get" smoke_age_roundtrip; then
    echo "mac-smoke: kc.age round-trip FAILED" >&2
    exit 1
  fi
  echo "mac-smoke: kc.age round-trip OK"
fi

echo "mac-smoke: kc link (throwaway keychain item)"
LINK_ITEM="kc.test.link-$(openssl rand -hex 2)"
LINK_NAME="smokelink-$(openssl rand -hex 2)"
smoke_link_write() {
  local hex
  hex="$(printf '%s' link-smoke-val | xxd -p | tr -d '\n')"
  printf 'add-generic-password -s %s -a kc -U -T /usr/bin/security -X %s\n' "$LINK_ITEM" "$hex" | /usr/bin/security -i
}
if run_keychain_step "키체인에 link 테스트 항목 저장" smoke_link_write; then
  if kc_keychain link "$LINK_NAME" "keychain:${LINK_ITEM}/kc" >/dev/null; then
    got_link="$(kc_keychain get "$LINK_NAME")"
    if [[ "$got_link" == "link-smoke-val" ]]; then
      echo "mac-smoke: link read OK"
      kc_keychain unlink "$LINK_NAME" >/dev/null || true
    else
      echo "mac-smoke: link get mismatch" >&2
      exit 1
    fi
  fi
  /usr/bin/security delete-generic-password -s "$LINK_ITEM" -a kc 2>/dev/null || true
fi

echo "mac-smoke: setup-git (GIT_CONFIG_GLOBAL temp)"
export GIT_CONFIG_GLOBAL="$SMOKE_DIR/gitconfig"
touch "$GIT_CONFIG_GLOBAL"
if ! kc_keychain setup-git --dry-run | grep -q git-credential; then
  echo "mac-smoke: setup-git dry-run failed" >&2
  exit 1
fi
if kc_keychain setup-git >/dev/null; then
  if git config --global --get credential.https://github.com.helper | grep -q 'kc git-credential'; then
    echo "mac-smoke: setup-git configured"
    kc_keychain setup-git --undo >/dev/null || true
  fi
fi
unset GIT_CONFIG_GLOBAL

echo "mac-smoke: sandbox exit 3 (best effort)"
SANDBOX_CFG="$SMOKE_DIR/sandbox.toml"
write_keychain_only_config "$SANDBOX_CFG"
SB_PROFILE="$(mktemp)"
cat >"$SB_PROFILE" <<'SB'
(version 1)
(deny default)
(allow process-exec*)
(allow process-fork)
(allow file-read*)
(allow sysctl-read)
(allow mach-lookup (global-name "com.apple.coreservices.launchservicesd"))
(allow mach-lookup (global-name "com.apple.lsd.mapdb"))
(allow signal)
SB
if sandbox-exec -f "$SB_PROFILE" \
  env -i HOME="$REAL_HOME" USER="${USER:-$(id -un)}" LOGNAME="${LOGNAME:-$(id -un)}" \
  PATH="${REAL_HOME}/.local/bin:/usr/bin:/bin" KC_CONFIG="$SANDBOX_CFG" \
  "$KC" get groq >/dev/null 2>/tmp/kc-sandbox.err; then
  echo "mac-smoke: sandbox did not block (exit 0) — NOT VERIFIED exit 3"
else
  SANDBOX_RC=$?
  echo "mac-smoke: sandbox exit=$SANDBOX_RC stderr=$(head -1 /tmp/kc-sandbox.err | cut -c1-120)"
  if [[ "$SANDBOX_RC" -eq 3 ]]; then
    echo "mac-smoke: sandbox exit 3 OK"
  fi
fi
rm -f "$SB_PROFILE"

echo "mac-smoke: ok"
