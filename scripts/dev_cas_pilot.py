#!/usr/bin/env python3
"""KeyCLI development-only disposable byte selection + CAS rollback pilot.

Never writes the real ~/.local/bin/kc or invokes a selected executable.
This is a cooperative same-UID filesystem fixture, NOT signed provenance,
host admission, process health or KeyCLI operating installation.
"""
from __future__ import annotations

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
import tempfile

SHA40 = re.compile(r"[0-9a-f]{40}\Z")
SHA64 = re.compile(r"[0-9a-f]{64}\Z")
DEV = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-(dev|rc)\.(0|[1-9][0-9]*)\Z")
MARKER = ".kc-dev-pilot.json"
PIN = "selected.json"
LOCK = ".selection.lock"
MAX_BINARY = 32 * 1024 * 1024
MAX_JSON = 16 * 1024
SCHEMA = "keycli.disposable-byte-cas.v1"


class Hold(ValueError):
    pass


def _require(ok, reason):
    if not ok:
        raise Hold(reason)


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _json(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def _read_dir(fd, name, *, limit=MAX_JSON, optional=False, mode=None):
    _require("/" not in name and name not in (".", ".."), "invalid leaf")
    try:
        child = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
    except FileNotFoundError:
        if optional:
            return None
        raise
    try:
        before = os.fstat(child)
        _require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1
                 and before.st_uid == os.geteuid()
                 and not before.st_mode & 0o022
                 and before.st_size <= limit, "unsafe file")
        if mode is not None:
            _require(stat.S_IMODE(before.st_mode) == mode, "unsafe file mode")
        with os.fdopen(child, "rb", closefd=False) as stream:
            value = stream.read(limit + 1)
        after = os.fstat(child)
        identity = lambda x: (x.st_dev, x.st_ino, x.st_size, x.st_mtime_ns,
                              x.st_ctime_ns, x.st_mode, x.st_nlink, x.st_uid)
        _require(identity(before) == identity(after)
                 and len(value) == before.st_size, "file drift")
        return value
    finally:
        os.close(child)


def _fresh(fd, name, content, mode=0o600):
    _require("/" not in name and name not in (".", ".."), "invalid leaf")
    child = os.open(name, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW,
                    0o600, dir_fd=fd)
    try:
        with os.fdopen(child, "wb", closefd=False) as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.fchmod(child, mode)
        os.fsync(child)
    finally:
        os.close(child)
    os.fsync(fd)


def _replace(fd, name, content):
    temp = ".next-" + secrets.token_hex(16)
    _fresh(fd, temp, content)
    try:
        os.replace(temp, name, src_dir_fd=fd, dst_dir_fd=fd)
        os.fsync(fd)
    finally:
        try:
            os.unlink(temp, dir_fd=fd)
        except FileNotFoundError:
            pass


def _root(path):
    root = Path(path)
    trusted_temp = Path(tempfile.gettempdir()).resolve(strict=True)
    _require(root.is_absolute() and root.name.startswith("kc-dev-cas-")
             and root.resolve(strict=True) == root
             and root.is_relative_to(trusted_temp), "not a disposable root")
    fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        s = os.fstat(fd)
        _require(stat.S_ISDIR(s.st_mode) and s.st_uid == os.geteuid()
                 and stat.S_IMODE(s.st_mode) == 0o700, "unsafe root")
        marker = json.loads(_read_dir(fd, MARKER, mode=0o600))
        _require(isinstance(marker, dict)
                 and marker.get("schema") == SCHEMA
                 and marker.get("mode") == "development-fixture"
                 and marker.get("authorizes_operating") is False
                 and isinstance(marker.get("root_id"), str)
                 and SHA64.fullmatch(marker["root_id"]), "invalid root marker")
        return fd, marker["root_id"]
    except BaseException:
        os.close(fd)
        raise


@contextmanager
def _locked(root, *, exclusive=True):
    fd, root_id = _root(root)
    try:
        lock = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW, dir_fd=fd)
        try:
            s = os.fstat(lock)
            _require(stat.S_ISREG(s.st_mode) and s.st_nlink == 1
                     and s.st_uid == os.geteuid()
                     and stat.S_IMODE(s.st_mode) == 0o600, "unsafe lock")
            fcntl.flock(lock, fcntl.LOCK_EX if exclusive else fcntl.LOCK_SH)
            yield fd, root_id
        finally:
            os.close(lock)
    finally:
        os.close(fd)


def _asset(fd, digest):
    raw = _read_dir(fd, "asset-" + digest, limit=MAX_BINARY, mode=0o500)
    _require(_sha(raw) == digest, "staged bytes drift")
    return raw


def _selection(fd, root_id, raw):
    if raw is None:
        return None
    record = json.loads(raw)
    _require(isinstance(record, dict) and record.get("schema") == SCHEMA
             and record.get("root_id") == root_id
             and isinstance(record.get("transaction"), str)
             and SHA64.fullmatch(record["transaction"])
             and isinstance(record.get("source_head"), str)
             and SHA40.fullmatch(record["source_head"])
             and isinstance(record.get("tag"), str)
             and DEV.fullmatch(record["tag"])
             and isinstance(record.get("artifact_sha256"), str)
             and SHA64.fullmatch(record["artifact_sha256"]), "invalid selection")
    _asset(fd, record["artifact_sha256"])
    return record


def _source(path):
    source = Path(path)
    _require(source.is_absolute() and source.resolve(strict=True) == source,
             "source path must be absolute and real")
    prior = source.lstat()
    _require(stat.S_ISREG(prior.st_mode) and prior.st_nlink == 1
             and prior.st_uid == os.geteuid()
             and not prior.st_mode & 0o022 and prior.st_size <= MAX_BINARY,
             "untrusted source file")
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        opened = os.fstat(fd)
        identity = lambda x: (x.st_dev, x.st_ino, x.st_size, x.st_mtime_ns, x.st_ctime_ns)
        _require(identity(opened) == identity(prior), "source moved")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            data = stream.read(MAX_BINARY + 1)
        after = os.fstat(fd)
        _require(identity(opened) == identity(after) and len(data) == prior.st_size,
                 "source changed")
        return data
    finally:
        os.close(fd)


def init(*, base=None):
    """Create one fresh private root under system temp or an explicit base."""
    parent = Path(base if base is not None else tempfile.gettempdir()).resolve(strict=True)
    _require(parent.is_dir()
             and parent.is_relative_to(Path(tempfile.gettempdir()).resolve(strict=True)),
             "temporary base must stay in OS temp")
    root = Path(tempfile.mkdtemp(prefix="kc-dev-cas-", dir=parent)).resolve(strict=True)
    os.chmod(root, 0o700)
    fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        root_id = secrets.token_hex(32)
        _fresh(fd, MARKER, _json({"schema": SCHEMA, "mode": "development-fixture",
                                 "root_id": root_id, "authorizes_operating": False}))
        _fresh(fd, LOCK, b"")
    finally:
        os.close(fd)
    return {"root": str(root), "status": "DISPOSABLE_EMPTY",
            "root_id": root_id, "authorizes_execution": False,
            "authorizes_operating": False}


def stage(root, source, *, expected_sha256, tag, source_head,
          allow_disposable=False):
    """Copy verified local bytes into a private content address, never execute."""
    _require(allow_disposable is True, "explicit disposable stage required")
    _require(isinstance(expected_sha256, str) and SHA64.fullmatch(expected_sha256)
             and isinstance(tag, str) and DEV.fullmatch(tag)
             and isinstance(source_head, str) and SHA40.fullmatch(source_head),
             "invalid reviewed identity shape")
    data = _source(source)
    _require(_sha(data) == expected_sha256, "source binary SHA mismatch")
    with _locked(root) as (fd, _root_id):
        meta = {"schema": SCHEMA, "tag": tag, "source_head": source_head,
                "artifact_sha256": expected_sha256}
        leaf = "asset-" + expected_sha256
        existing = _read_dir(fd, leaf, limit=MAX_BINARY, optional=True, mode=0o500)
        if existing is None:
            _fresh(fd, leaf, data, mode=0o500)
        else:
            _require(existing == data, "content address conflicts")
        name = "stage-" + expected_sha256 + ".json"
        previous = _read_dir(fd, name, optional=True)
        if previous is None:
            _fresh(fd, name, _json(meta))
        else:
            _require(previous == _json(meta), "staged source binding differs")
        _asset(fd, expected_sha256)
        return {"status": "STAGED_DISPOSABLE_BYTES_NOT_TRUSTED",
                "artifact_sha256": expected_sha256,
                "authorizes_execution": False, "authorizes_operating": False}


def select(root, *, artifact_sha256, tag, source_head,
           expected_previous_pin_sha256, allow_disposable=False):
    _require(allow_disposable is True, "explicit disposable CAS required")
    _require(isinstance(artifact_sha256, str) and SHA64.fullmatch(artifact_sha256)
             and isinstance(tag, str) and DEV.fullmatch(tag)
             and isinstance(source_head, str) and SHA40.fullmatch(source_head)
             and (expected_previous_pin_sha256 is None
                  or isinstance(expected_previous_pin_sha256, str)
                  and SHA64.fullmatch(expected_previous_pin_sha256)),
             "invalid CAS input identity")
    with _locked(root) as (fd, root_id):
        binding = json.loads(_read_dir(fd, "stage-"+artifact_sha256+".json"))
        _require(binding == {"schema": SCHEMA, "tag": tag, "source_head": source_head,
                             "artifact_sha256": artifact_sha256}, "unrecognized staged identity")
        _asset(fd, artifact_sha256)
        old = _read_dir(fd, PIN, optional=True)
        _selection(fd, root_id, old)
        _require((_sha(old) if old is not None else None)
                 == expected_previous_pin_sha256, "previous pin CAS changed")
        tx = secrets.token_hex(32)
        new_pin = _json({"schema":SCHEMA,"root_id":root_id,"transaction":tx,
                         "source_head":source_head,"tag":tag,
                         "artifact_sha256":artifact_sha256})
        receipt = _json({"schema":SCHEMA,"root_id":root_id,"transaction":tx,
                         "old_pin_sha256":expected_previous_pin_sha256,
                         "old_pin_b64":base64.b64encode(old).decode() if old is not None else None,
                         "new_pin_sha256":_sha(new_pin)})
        _fresh(fd, "receipt-"+tx+".json", receipt)
        _replace(fd, PIN, new_pin)
        return {"status":"DISPOSABLE_BYTES_SELECTED_NOT_OPERATING",
                "pin_sha256":_sha(new_pin),"receipt_id":tx,
                "receipt_sha256":_sha(receipt),"previous_pin_sha256":expected_previous_pin_sha256,
                "authorizes_execution":False,"authorizes_operating":False}


def status(root):
    with _locked(root, exclusive=False) as (fd, root_id):
        raw = _read_dir(fd, PIN, optional=True)
        record = _selection(fd, root_id, raw)
        return {"status":"DISPOSABLE_SELECTED" if record else "DISPOSABLE_EMPTY",
                "selected":record, "pin_sha256":_sha(raw) if raw is not None else None,
                "artifact_bytes_match":bool(record),
                "authorizes_execution":False,"authorizes_operating":False}


def rollback(root, *, receipt_id, receipt_sha256, allow_disposable=False):
    _require(allow_disposable is True, "explicit disposable rollback required")
    _require(isinstance(receipt_id, str) and SHA64.fullmatch(receipt_id)
             and isinstance(receipt_sha256, str) and SHA64.fullmatch(receipt_sha256),
             "invalid original receipt identity")
    with _locked(root) as (fd, root_id):
        receipt = _read_dir(fd, "receipt-"+receipt_id+".json", mode=0o600)
        _require(_sha(receipt) == receipt_sha256, "receipt digest mismatch")
        record = json.loads(receipt)
        _require(record.get("schema") == SCHEMA and record.get("root_id") == root_id
                 and record.get("transaction") == receipt_id, "foreign receipt")
        current = _read_dir(fd, PIN)
        _selection(fd, root_id, current)
        _require(_sha(current) == record["new_pin_sha256"], "CAS selection changed")
        previous = (base64.b64decode(record["old_pin_b64"], validate=True)
                    if record["old_pin_b64"] is not None else None)
        _require(previous is None or len(previous) <= MAX_JSON, "previous pin too large")
        _require((_sha(previous) if previous is not None else None)
                 == record["old_pin_sha256"], "prior pin identity changed")
        _selection(fd, root_id, previous)
        if previous is None:
            os.unlink(PIN, dir_fd=fd)
            os.fsync(fd)
        else:
            _replace(fd, PIN, previous)
        return {"status":"DISPOSABLE_ROLLBACK_BYTES_NOT_OPERATING",
                "pin_sha256":record["old_pin_sha256"],
                "authorizes_execution":False,"authorizes_operating":False}
