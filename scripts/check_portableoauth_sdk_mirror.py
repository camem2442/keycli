#!/usr/bin/env python3
"""Offline integrity gate for the temporary internal->public Go SDK mirror."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "internal/auth/oauth"
CANDIDATE = ROOT / "sdk/portableoauth"
FILES = ("flow.go", "flow_test.go", "runtime_vectors_test.go",
         "testdata/runtime-vectors-v1.json")
VECTOR_BLOB = "6dcc6df704300a8b475e94e834b37bf9e31c9681"
MODULE = "github.com/camem2442/keycli/sdk/portableoauth"
ALLOWED = frozenset(FILES + ("go.mod", "import_test.go", "README.md"))


class MirrorHeld(ValueError):
    pass


def blob(data):
    return hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()


def check():
    observed = set()
    for entry in CANDIDATE.rglob("*"):
        if entry.is_symlink():
            raise MirrorHeld("SCOPE_INVALID")
        if entry.is_file():
            observed.add(entry.relative_to(CANDIDATE).as_posix())
    if observed != ALLOWED:
        raise MirrorHeld("UNDECLARED_COMPILE_INPUT")
    public_test = CANDIDATE / "import_test.go"
    source = public_test.read_text(encoding="utf-8")
    if ('package oauth_test' not in source or
            '"github.com/camem2442/keycli/sdk/portableoauth"' not in source):
        raise MirrorHeld("PUBLIC_IMPORT_NOT_VERIFIED")
    for rel in FILES:
        a, b = SOURCE / rel, CANDIDATE / rel
        if a.is_symlink() or b.is_symlink() or not a.is_file() or not b.is_file():
            raise MirrorHeld("SCOPE_INVALID")
        first, second = a.read_bytes(), b.read_bytes()
        if not first or first != second or len(first) > 100_000:
            raise MirrorHeld("MIRROR_DRIFT")
        if rel.endswith("runtime-vectors-v1.json") and blob(first) != VECTOR_BLOB:
            raise MirrorHeld("VECTOR_DRIFT")
    mod = CANDIDATE / "go.mod"
    if mod.is_symlink() or not mod.is_file():
        raise MirrorHeld("GO_MOD_INVALID")
    text = mod.read_text(encoding="utf-8")
    if text != f"module {MODULE}\n\ngo 1.22\n":
        raise MirrorHeld("GO_MOD_INVALID")
    return {
        "schema": "personal-auth-go-sdk-candidate/v1",
        "candidate_version": "0.1.0",
        "public_module_path": MODULE,
        "mirrored_files": len(FILES),
        "mirror_identical": True,
        "source_commit_attested": False,
        "external_consumer_verification": "NOT_VERIFIED",
        "published": False,
        "tag_created": False,
    }


if __name__ == "__main__":
    try:
        print(json.dumps(check(), sort_keys=True))
    except (MirrorHeld, OSError, ValueError):
        print(json.dumps({"status":"BLOCKED", "reason":"GO_SDK_MIRROR_HELD"}))
        raise SystemExit(2)
