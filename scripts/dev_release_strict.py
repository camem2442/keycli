#!/usr/bin/env python3
"""Fail-closed per-check verdict for KeyCLI's opt-in strict dev/rc smoke.

The owning shell emits one tab-separated row for each required native check.
This parser cannot authenticate a producer, VM, original runner or Goal Grant.
"""
from __future__ import annotations

import json
from pathlib import Path
import re
import sys

REQUIRED = (
    "checksums", "host_asset", "installed_bytes", "installed_version",
    "mcp_smoke", "provider_lock", "existing_install_unchanged",
)
MAX_BYTES = 8192
PRINTABLE = re.compile(r"[\x20-\x7e]{1,200}\Z")


def evaluate(raw: bytes) -> dict:
    observations = {}
    problems = []
    if not isinstance(raw, bytes) or not 0 < len(raw) <= MAX_BYTES:
        problems.append("MISSING_OR_OVERSIZED_REPORT")
        raw = b""
    try:
        text = raw.decode("ascii", "strict")
    except UnicodeError:
        problems.append("INVALID_ENCODING")
        text = ""
    for line in text.splitlines():
        field, separator, value = line.partition("\t")
        if (not separator or field not in REQUIRED or field in observations
                or not PRINTABLE.fullmatch(value)):
            problems.append("INVALID_DUPLICATE_OR_UNKNOWN_CHECK")
            continue
        observations[field] = value
    for field in REQUIRED:
        value = observations.get(field)
        if value is None:
            problems.append("MISSING_" + field.upper())
        elif value != "PASS" and not (value.startswith("PASS (") and value.endswith(")")):
            problems.append("NOT_PASS_" + field.upper())
    return {
        "schema": "keycli.dev-release-strict.v1",
        "status": ("PASS_ALL_REQUIRED_CHECKS_NOT_ATTESTED" if not problems
                   else "HOLD_MANDATORY_CHECKS"),
        "checks": {name: observations.get(name) for name in REQUIRED},
        "problems": sorted(set(problems)),
        "original_host_verified": False,
        "producer_verified": False,
        "authorizes_execution": False,
        "authorizes_install": False,
        "authorizes_operating": False,
        "authorizes_merge": False,
    }


def main(argv=None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    if len(argv) != 1:
        print("usage: dev_release_strict.py REQUIRED_CHECK_LOG", file=sys.stderr)
        return 64
    try:
        file = Path(argv[0])
        if not file.is_file() or file.is_symlink() or file.stat().st_size > MAX_BYTES:
            raise ValueError("invalid report file")
        result = evaluate(file.read_bytes())
    except (OSError, ValueError):
        result = evaluate(b"")
    print(json.dumps(result, sort_keys=True))
    return 0 if result["status"] == "PASS_ALL_REQUIRED_CHECKS_NOT_ATTESTED" else 3


if __name__ == "__main__":
    raise SystemExit(main())
