#!/usr/bin/env python3
"""KeyCLI disposable *development bytes* selector with cooperative CAS/rollback.

Never touches ~/.local/bin/kc, executes downloaded bytes, publishes a Release,
uses provider/Keychain credentials, or claims OS process health. Source/producer
signatures and native running verification belong to independent owning gates.
"""
from __future__ import annotations

import argparse
import base64
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import stat
import sys

SCHEMA = "keycli.dev-disposable-cas.v1"
MAX_BINARY = 32 * 1024 * 1024
MAX_SUMS = 64 * 1024
MAX_META = 16 * 1024
SHA40 = re.compile(r"[0-9a-f]{40}\Z")
SHA64 = re.compile(r"[0-9a-f]{64}\Z")
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
PREVIEW_VERSION = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-(dev|rc)\.(0|[1-9][0-9]*)\Z")
ASSETS = {"kc-darwin-arm64", "kc-darwin-amd64", "kc-linux-arm64", "kc-linux-amd64"}
SAFE_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")


def _require(ok, reason):
    if not ok:
        raise ValueError(reason)


def _hash(payload):
    return hashlib.sha256(payload).hexdigest()


def _json(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode("utf-8")


def _out(status, **fields):
    return dict(schema=SCHEMA, status=status, authorizes_execution=False,
                authorizes_install=False, authorizes_operating=False,
                actual_service_healthy=False, provider_effects_enabled=False,
                **fields)


def _parent(root):
    path = Path(root)
    _require(path.is_absolute() and path.name.startswith("kc-dev-preview-")
             and path.resolve(strict=False) == path, "PRIVATE_DISPOSABLE_ROOT_REQUIRED")
    _require(not path.is_relative_to(Path.home().resolve() / ".local"),
             "REFUSE_REAL_INSTALL")
    parent = path.parent
    st = parent.lstat()
    _require(stat.S_ISDIR(st.st_mode) and st.st_uid == os.geteuid()
             and stat.S_IMODE(st.st_mode) == 0o700
             and parent.resolve(strict=True) == parent, "PRIVATE_PARENT_REQUIRED")
    return path


def _read_file(path, limit):
    p = Path(path)
    _require(p.is_absolute() and p.resolve(strict=True) == p, "UNSAFE_SOURCE_PATH")
    before = p.lstat()
    _require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1
             and not before.st_mode & 0o022 and 0 < before.st_size <= limit,
             "UNSAFE_SOURCE_FILE")
    fd = os.open(p, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    try:
        opened = os.fstat(fd)
        _require((opened.st_dev, opened.st_ino, opened.st_size, opened.st_mtime_ns)
                 == (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns),
                 "CHANGED_SOURCE")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            body = stream.read(limit + 1)
        after = os.fstat(fd)
        _require(len(body) == before.st_size and
                 (opened.st_dev, opened.st_ino, opened.st_size, opened.st_mtime_ns)
                 == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns),
                 "CHANGED_SOURCE")
        return body
    finally:
        os.close(fd)


def _read(fd, name, limit=MAX_META, *, optional=False, mode=None):
    try:
        file_fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
    except FileNotFoundError:
        if optional:
            return None
        raise
    try:
        before = os.fstat(file_fd)
        _require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1
                 and before.st_uid == os.geteuid() and
                 not before.st_mode & 0o022 and before.st_size <= limit,
                 "UNSAFE_PRIVATE_FILE")
        if mode is not None:
            _require(stat.S_IMODE(before.st_mode) == mode, "PRIVATE_MODE_DRIFT")
        with os.fdopen(file_fd, "rb", closefd=False) as stream:
            value = stream.read(limit + 1)
        after = os.fstat(file_fd)
        _require(len(value) == before.st_size and
                 (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
                 == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns),
                 "CHANGED_PRIVATE_FILE")
        return value
    finally:
        os.close(file_fd)


def _fresh(fd, name, body, mode):
    child = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                    mode, dir_fd=fd)
    try:
        with os.fdopen(child, "wb", closefd=False) as stream:
            stream.write(body)
            stream.flush()
        os.fsync(child)
    finally:
        os.close(child)
    os.fsync(fd)


def _select(fd, value):
    name = ".tmp-" + secrets.token_hex(16)
    _fresh(fd, name, value, 0o600)
    try:
        os.replace(name, "selected.json", src_dir_fd=fd, dst_dir_fd=fd)
        os.fsync(fd)
    finally:
        try:
            os.unlink(name, dir_fd=fd)
        except FileNotFoundError:
            pass


@contextmanager
def _locked(root, *, exclusive):
    path = _parent(root)
    st = path.lstat()
    _require(stat.S_ISDIR(st.st_mode) and st.st_uid == os.geteuid()
             and stat.S_IMODE(st.st_mode) == 0o700, "UNSAFE_DISPOSABLE_ROOT")
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        identity = json.loads(_read(fd, ".marker", mode=0o600))
        _require(identity.get("schema") == SCHEMA
                 and isinstance(identity.get("root_id"), str)
                 and HEX32.fullmatch(identity["root_id"]), "INVALID_ROOT_MARKER")
        lock = os.open(".lock", os.O_RDWR | os.O_NOFOLLOW, dir_fd=fd)
        try:
            st = os.fstat(lock)
            _require(stat.S_ISREG(st.st_mode) and st.st_uid == os.geteuid()
                     and st.st_nlink == 1 and stat.S_IMODE(st.st_mode) == 0o600,
                     "UNSAFE_LOCK")
            fcntl.flock(lock, fcntl.LOCK_EX if exclusive else fcntl.LOCK_SH)
            yield fd, identity["root_id"]
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)
            os.close(lock)
    finally:
        os.close(fd)


def create(root, *, allow_disposable_write=False):
    _require(allow_disposable_write is True, "EXPLICIT_DISPOSABLE_CONSENT_REQUIRED")
    path = _parent(root)
    path.mkdir(mode=0o700, exist_ok=False)
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        root_id = secrets.token_hex(16)
        _fresh(fd, ".marker", _json({"schema": SCHEMA, "root_id": root_id}), 0o600)
        _fresh(fd, ".lock", b"", 0o600)
        return _out("DISPOSABLE_EMPTY", root=str(path), root_id=root_id)
    finally:
        os.close(fd)


def _selected(fd, root_id):
    raw = _read(fd, "selected.json", optional=True, mode=0o600)
    if raw is None:
        return None, None
    obj = json.loads(raw)
    _require(isinstance(obj, dict) and set(obj) == {
        "schema", "root_id", "version", "source_head", "asset_name", "asset_sha256",
        "sums_sha256", "txn",
    } and obj["schema"] == SCHEMA and obj["root_id"] == root_id
             and isinstance(obj["version"], str) and PREVIEW_VERSION.fullmatch(obj["version"])
             and isinstance(obj["source_head"], str) and SHA40.fullmatch(obj["source_head"])
             and obj["asset_name"] in ASSETS and isinstance(obj["asset_sha256"], str)
             and SHA64.fullmatch(obj["asset_sha256"])
             and isinstance(obj["sums_sha256"], str) and SHA64.fullmatch(obj["sums_sha256"])
             and isinstance(obj["txn"], str) and HEX32.fullmatch(obj["txn"]),
             "INVALID_SELECTED_IDENTITY")
    artifact = _read(fd, "artifact-" + obj["asset_sha256"], MAX_BINARY, mode=0o400)
    _require(_hash(artifact) == obj["asset_sha256"], "INSTALLED_BYTES_DRIFT")
    return raw, obj


def status(root):
    with _locked(root, exclusive=False) as (fd, root_id):
        raw, obj = _selected(fd, root_id)
        return _out("DISPOSABLE_STAGED" if obj else "DISPOSABLE_EMPTY",
                    pin_sha256=_hash(raw) if raw is not None else None,
                    asset_sha256=obj["asset_sha256"] if obj else None,
                    version=obj["version"] if obj else None,
                    byte_identity_checked=bool(obj))


def _entries(raw):
    try:
        value = raw.decode("utf-8", "strict")
    except UnicodeError as exc:
        raise ValueError("INVALID_SUMS_ENCODING") from exc
    _require(value and value.endswith("\n") and "\r" not in value
             and "\x00" not in value, "INVALID_SUMS_FORMAT")
    entries = {}
    for line in value.splitlines():
        _require(len(line) >= 67 and SHA64.fullmatch(line[:64])
                 and line[64:66] in ("  ", " *")
                 and SAFE_NAME.fullmatch(line[66:])
                 and line[66:] not in entries, "INVALID_SUMS_ENTRY")
        entries[line[66:]] = line[:64]
    return entries


def stage(root, *, asset_path, sums_path, version, source_head,
          expected_asset_sha256, expected_sums_sha256, expected_previous,
          allow_disposable_write=False):
    _require(allow_disposable_write is True, "EXPLICIT_DISPOSABLE_CONSENT_REQUIRED")
    _require(isinstance(version, str) and PREVIEW_VERSION.fullmatch(version)
             and isinstance(source_head, str) and SHA40.fullmatch(source_head),
             "DEVELOPMENT_SOURCE_IDENTITY_REQUIRED")
    asset_name = Path(asset_path).name
    _require(asset_name in ASSETS and isinstance(expected_asset_sha256, str)
             and SHA64.fullmatch(expected_asset_sha256)
             and isinstance(expected_sums_sha256, str)
             and SHA64.fullmatch(expected_sums_sha256)
             and (expected_previous is None or isinstance(expected_previous, str)
                  and SHA64.fullmatch(expected_previous)),
             "SOURCE_HASH_OR_CAS_PREIMAGE_INVALID")
    binary = _read_file(asset_path, MAX_BINARY)
    sums = _read_file(sums_path, MAX_SUMS)
    _require(_hash(binary) == expected_asset_sha256 and
             _hash(sums) == expected_sums_sha256 and
             _entries(sums).get(asset_name) == expected_asset_sha256,
             "LOCAL_BYTES_OR_SUMS_MISMATCH")
    with _locked(root, exclusive=True) as (fd, root_id):
        previous, _ = _selected(fd, root_id)
        _require((_hash(previous) if previous is not None else None) == expected_previous,
                 "SELECTION_CAS_MISMATCH")
        artifact_name = "artifact-" + expected_asset_sha256
        old = _read(fd, artifact_name, MAX_BINARY, optional=True, mode=0o400)
        if old is None:
            _fresh(fd, artifact_name, binary, 0o400)
        else:
            _require(old == binary, "CONTENT_ADDRESS_CONFLICT")
        txn = secrets.token_hex(16)
        selected = _json({"schema": SCHEMA, "root_id": root_id, "version": version,
                          "source_head": source_head, "asset_name": asset_name,
                          "asset_sha256": expected_asset_sha256,
                          "sums_sha256": expected_sums_sha256, "txn": txn})
        receipt = _json({"schema": SCHEMA, "root_id": root_id, "txn": txn,
                         "previous_b64": base64.b64encode(previous).decode() if previous else None,
                         "previous_sha256": expected_previous,
                         "selected_sha256": _hash(selected)})
        _fresh(fd, "receipt-" + txn, receipt, 0o600)
        _select(fd, selected)
        return _out("DISPOSABLE_BYTES_SELECTED_NOT_RUNNING",
                    pin_sha256=_hash(selected), asset_sha256=expected_asset_sha256,
                    receipt_id=txn, receipt_sha256=_hash(receipt),
                    previous_sha256=expected_previous)


def rollback(root, *, receipt_id, receipt_sha256, allow_disposable_write=False):
    _require(allow_disposable_write is True, "EXPLICIT_DISPOSABLE_CONSENT_REQUIRED")
    _require(isinstance(receipt_id, str) and HEX32.fullmatch(receipt_id)
             and isinstance(receipt_sha256, str) and SHA64.fullmatch(receipt_sha256),
             "EXTERNAL_ROLLBACK_RECEIPT_REQUIRED")
    with _locked(root, exclusive=True) as (fd, root_id):
        raw = _read(fd, "receipt-" + receipt_id, mode=0o600)
        _require(_hash(raw) == receipt_sha256, "ROLLBACK_RECEIPT_HASH_DRIFT")
        receipt = json.loads(raw)
        _require(isinstance(receipt, dict) and set(receipt) == {
            "schema", "root_id", "txn", "previous_b64", "previous_sha256", "selected_sha256"
        } and receipt["schema"] == SCHEMA and receipt["root_id"] == root_id
                 and receipt["txn"] == receipt_id, "FOREIGN_ROLLBACK_RECEIPT")
        current, _ = _selected(fd, root_id)
        _require(current is not None and _hash(current) == receipt["selected_sha256"],
                 "SELECTION_DRIFT_NO_BLIND_ROLLBACK")
        prior = (base64.b64decode(receipt["previous_b64"], validate=True)
                 if receipt["previous_b64"] is not None else None)
        _require(prior is None or len(prior) <= MAX_META, "OVERSIZED_PRIOR_PIN")
        _require((_hash(prior) if prior is not None else None)
                 == receipt["previous_sha256"], "PREVIOUS_PIN_HASH_DRIFT")
        if prior is not None:
            # Check that the previously installed artifact still exists and
            # matches the immutable signed-development selection preimage.
            obj = json.loads(prior)
            _require(obj.get("root_id") == root_id and obj.get("schema") == SCHEMA
                     and obj.get("asset_name") in ASSETS and
                     isinstance(obj.get("asset_sha256"), str)
                     and SHA64.fullmatch(obj["asset_sha256"]), "FOREIGN_PRIOR_PIN")
            old = _read(fd, "artifact-" + obj["asset_sha256"], MAX_BINARY, mode=0o400)
            _require(_hash(old) == obj["asset_sha256"], "PRIOR_ARTIFACT_DRIFT")
            _select(fd, prior)
        else:
            os.unlink("selected.json", dir_fd=fd)
            os.fsync(fd)
        return _out("DISPOSABLE_BYTES_ROLLED_BACK_NOT_RUNNING",
                    restored_pin_sha256=receipt["previous_sha256"])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    init = sub.add_parser("create")
    init.add_argument("--root", required=True, type=Path)
    init.add_argument("--allow-disposable-write", action="store_true")
    st = sub.add_parser("status")
    st.add_argument("--root", required=True, type=Path)
    act = sub.add_parser("stage")
    for k in ("root", "asset-path", "sums-path"):
        act.add_argument("--" + k, required=True, type=Path)
    for k in ("version", "source-head", "expected-asset-sha256", "expected-sums-sha256"):
        act.add_argument("--" + k, required=True)
    act.add_argument("--expected-previous", default="none")
    act.add_argument("--allow-disposable-write", action="store_true")
    rev = sub.add_parser("rollback")
    rev.add_argument("--root", required=True, type=Path)
    rev.add_argument("--receipt-id", required=True)
    rev.add_argument("--receipt-sha256", required=True)
    rev.add_argument("--allow-disposable-write", action="store_true")
    args = parser.parse_args(argv)
    try:
        if args.action == "create":
            result = create(args.root, allow_disposable_write=args.allow_disposable_write)
        elif args.action == "status":
            result = status(args.root)
        elif args.action == "stage":
            result = stage(args.root, asset_path=args.asset_path, sums_path=args.sums_path,
                           version=args.version, source_head=args.source_head,
                           expected_asset_sha256=args.expected_asset_sha256,
                           expected_sums_sha256=args.expected_sums_sha256,
                           expected_previous=None if args.expected_previous == "none"
                           else args.expected_previous,
                           allow_disposable_write=args.allow_disposable_write)
        else:
            result = rollback(args.root, receipt_id=args.receipt_id,
                              receipt_sha256=args.receipt_sha256,
                              allow_disposable_write=args.allow_disposable_write)
    except (OSError, ValueError, TypeError, KeyError, UnicodeError, OverflowError):
        result = _out("HOLD_DISPOSABLE_SOURCE_OR_CAS_INVALID")
    print(json.dumps(result, sort_keys=True))
    return 0 if result["status"].startswith("DISPOSABLE_") else 2


if __name__ == "__main__":
    raise SystemExit(main())
