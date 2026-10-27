#!/usr/bin/env bash
# Optional checkout of private camem2442/ai-core for Cloud Agents (KC-40).
# Safe to run from install (opt-in) or agent sessions. Never prints secrets.
set -euo pipefail

AI_CORE_DIR="${KC_CLOUD_AI_CORE_DIR:-${HOME}/ai-core}"
REPO_SLUG="camem2442/ai-core"
CLONE_AI_CORE="${KC_CLOUD_CLONE_AI_CORE:-0}"

if [[ "$CLONE_AI_CORE" != "1" ]]; then
  if [[ -d "$AI_CORE_DIR/.git" ]]; then
    echo "ai-core: present at ${AI_CORE_DIR}"
  else
    echo "ai-core: skipped (set KC_CLOUD_CLONE_AI_CORE=1 and GITHUB_TOKEN to clone)"
  fi
  exit 0
fi

if [[ -d "$AI_CORE_DIR/.git" ]]; then
  echo "ai-core: already at ${AI_CORE_DIR}"
  exit 0
fi

if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  if git clone --origin github \
    "https://x-access-token:${GITHUB_TOKEN}@github.com/${REPO_SLUG}.git" \
    "$AI_CORE_DIR"; then
    echo "ai-core: cloned to ${AI_CORE_DIR}"
    exit 0
  fi
  echo "ai-core: clone failed (check token scope for ${REPO_SLUG})" >&2
  exit 0
fi

if command -v kc >/dev/null 2>&1; then
  if kc exec --only github -- git clone --origin github \
    "https://github.com/${REPO_SLUG}.git" "$AI_CORE_DIR" 2>/dev/null; then
    echo "ai-core: cloned via kc exec to ${AI_CORE_DIR}"
    exit 0
  fi
fi

echo "ai-core: skipped (no GITHUB_TOKEN and kc github secret unavailable)" >&2
exit 0
