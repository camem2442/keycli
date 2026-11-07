#!/usr/bin/env bash
# Run an actual, isolated Linux kc binary as an MCP client. No real credentials.
set -euo pipefail
BIN="${1:?usage: verify-mcp-binary.sh /absolute/path/to/kc [expected-version]}"
VERSION="${2:-}"
[[ -x "$BIN" ]] || { echo "binary missing: $BIN" >&2; exit 1; }
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/home"
if [[ -n "$VERSION" ]]; then
  "$BIN" --version | grep -F "$VERSION" >/dev/null
fi
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_secret","arguments":{"name":"github"}}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"doctor","arguments":{}}}' \
  | env -i HOME="$TMP/home" PATH="$PATH" "$BIN" mcp >"$TMP/default.jsonl" 2>"$TMP/default.err"
printf '%s\n' '{"jsonrpc":"2.0","id":4,"method":"tools/list"}' \
  | env -i HOME="$TMP/home" PATH="$PATH" "$BIN" mcp --allow-secret-values >"$TMP/optin.jsonl" 2>"$TMP/optin.err"
python3 - "$TMP/default.jsonl" "$TMP/optin.jsonl" <<'PY'
import json
import sys
from pathlib import Path

def responses(path):
    lines = Path(path).read_text().splitlines()
    return {item["id"]: item for item in map(json.loads, lines)}

normal = responses(sys.argv[1])
optin = responses(sys.argv[2])
assert set(normal) == {1, 2, 3}, f"wrong default MCP response IDs: {set(normal)}"
assert set(optin) == {4}, f"wrong opt-in MCP response IDs: {set(optin)}"
default_tools = {t["name"] for t in normal[1]["result"]["tools"]}
optin_tools = {t["name"] for t in optin[4]["result"]["tools"]}
assert default_tools == {"list_secrets", "policy_check", "doctor"}, default_tools
assert optin_tools == default_tools | {"get_secret"}, optin_tools
forged = normal[2]["result"]
assert forged.get("isError") is True, "forged get_secret was not denied"
assert "disabled" in forged["content"][0]["text"], "missing default denial message"
assert normal[3]["result"].get("isError") is not True, "doctor failed"
print("mcp-binary-smoke: metadata-only default; forged get_secret denied; explicit opt-in advertised; doctor OK")
PY
