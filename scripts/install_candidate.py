#!/usr/bin/env python3
"""KeyCLI one-shot operating binary replacement with an explicit consumption fence.

Contract
--------
- An authorization document binds exactly one candidate digest to one target
  path, one entry symlink, one previous digest and one expiry.
- ``prepare`` proves, without executing anything, that the candidate and the
  previous bytes are what the authorization says and that the previous backup
  restores the previous digest byte-for-byte. The scratch restore digest is
  recorded as ``restored_sha256``.
- ``apply`` consumes the authorization **before** touching the target. A replay
  is refused by the consumption record even when the target bytes have been
  put back to the authorized previous value.
- ``rollback`` restores the backup bytes, verifies the restored digest and is
  itself one-shot.

This tool does not probe health, execute the candidate, sign anything, or
grant operating authority. Health snapshots are supplied by the operator and
must be identical before and after. Receipts are byte facts, not acceptance.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import stat
import subprocess
import sys
import tempfile

SCHEMA = "keycli.operating-install.v1"
AUTH_SCHEMA = "keycli-operating-install-authorization-v1"
EFFECT = "replace_operating_kc_once"
PRODUCER_NAMESPACE = "keycli-private-candidate-v1"
PRODUCER_PRINCIPAL = "keycli-producer"
MAX_BINARY = 64 * 1024 * 1024
MAX_JSON = 32 * 1024
PREFLIGHT = "preflight.json"
CONSUMED = "consumed.json"
APPLY_RECEIPT = "apply-receipt.json"
APPLY_FAILED = "apply-failed.json"
ROLLBACK_RECEIPT = "rollback-receipt.json"


class Hold(ValueError):
    """Refusal. Never a partial success and never a retry hint."""


def _require(ok, reason):
    if not ok:
        raise Hold(reason)


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _json(value):
    return json.dumps(value, sort_keys=True, indent=2).encode() + b"\n"


def _now():
    return datetime.now(timezone.utc)


def _parse_time(value, reason):
    try:
        parsed = datetime.fromisoformat(value)
    except (TypeError, ValueError) as error:
        raise Hold(reason) from error
    _require(parsed.tzinfo is not None, reason)
    return parsed


def _stat_ok(st, *, regular=True):
    mode_ok = not st.st_mode & 0o022
    if regular:
        return (stat.S_ISREG(st.st_mode) and st.st_nlink == 1
                and st.st_uid == os.geteuid() and mode_ok)
    return stat.S_ISDIR(st.st_mode) and st.st_uid == os.geteuid() and mode_ok


def _read(path, *, limit, mode=None):
    path = Path(path)
    _require(path.is_absolute(), "path must be absolute")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        _require(_stat_ok(before) and before.st_size <= limit, "unsafe file")
        if mode is not None:
            _require(stat.S_IMODE(before.st_mode) == mode, "unsafe file mode")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read(limit + 1)
        after = os.fstat(fd)
        identity = lambda x: (x.st_dev, x.st_ino, x.st_size, x.st_mtime_ns,
                              x.st_ctime_ns, x.st_mode, x.st_nlink, x.st_uid)
        _require(identity(before) == identity(after) and len(raw) == before.st_size,
                 "file drifted while reading")
        return raw
    finally:
        os.close(fd)


def _fresh(path, content, mode=0o600):
    path = Path(path)
    try:
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    except FileExistsError as error:
        raise Hold(f"refusing to overwrite {path.name}") from error
    try:
        with os.fdopen(fd, "wb", closefd=False) as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.fchmod(fd, mode)
        os.fsync(fd)
    finally:
        os.close(fd)
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    return path


def _replace(path, content, mode):
    path = Path(path)
    temp = path.parent / (".next-" + secrets.token_hex(16))
    try:
        _fresh(temp, content, mode)
        os.replace(temp, path)
        os.fsync(os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY))
    finally:
        try:
            temp.unlink()
        except FileNotFoundError:
            pass
    return path


def _receipts(path):
    path = Path(path)
    _require(path.is_absolute() and path.resolve(strict=False) == path,
             "receipts dir must be absolute and real")
    st = path.lstat()
    _require(_stat_ok(st, regular=False) and stat.S_IMODE(st.st_mode) == 0o700,
             "receipts dir must be owner-only 0700")
    return path


def _bin_state(path):
    """Return (sha256, mode) for the current target bytes."""
    path = Path(path)
    st = path.lstat()
    _require(_stat_ok(st), "install target must be an owner-owned single-link regular file")
    return _sha(_read(path, limit=MAX_BINARY)), stat.S_IMODE(st.st_mode)


def _entry_ok(entry, target):
    entry, target = Path(entry), Path(target)
    try:
        return entry.is_symlink() and entry.resolve(strict=True) == target.resolve(strict=True)
    except (OSError, RuntimeError):
        return False


def _dir_ok(path):
    path = Path(path)
    st = path.lstat()
    return _stat_ok(st, regular=False)


def _load_authorization(raw, *, target, entry):
    try:
        auth = json.loads(raw)
    except json.JSONDecodeError as error:
        raise Hold("authorization is not JSON") from error
    _require(isinstance(auth, dict), "authorization must be an object")
    _require(auth.get("schema") == AUTH_SCHEMA and auth.get("effect") == EFFECT,
             "authorization schema or effect differs")
    _require(isinstance(auth.get("nonce"), str) and len(auth["nonce"]) == 64
             and all(c in "0123456789abcdef" for c in auth["nonce"]),
             "authorization nonce is invalid")
    _require(isinstance(auth.get("owner_statement"), str) and auth["owner_statement"].strip(),
             "authorization owner statement is missing")
    candidate = auth.get("candidate")
    bound = auth.get("target")
    _require(isinstance(candidate, dict) and isinstance(bound, dict), "authorization shape differs")
    _require(set(candidate) >= {"sha256"} and isinstance(candidate.get("sha256"), str)
             and len(candidate["sha256"]) == 64, "authorization candidate digest is invalid")
    _require(bound.get("path") == str(target) and bound.get("entry") == str(entry),
             "authorization target or entry differs")
    _require(bound.get("platform") == f"{sys.platform}/{os.uname().machine}",
             "authorization platform differs from this host")
    _require(bound.get("uid") == os.geteuid(), "authorization uid differs")
    _require(isinstance(auth.get("previous_sha256"), str) and len(auth["previous_sha256"]) == 64,
             "authorization previous digest is invalid")
    _require(_parse_time(auth.get("approved_at"), "authorization approved_at is invalid")
             <= _now() < _parse_time(auth.get("expires_at"), "authorization expiry is invalid"),
             "authorization is expired or not yet valid")
    return auth


def _sums_digest(raw, asset_name):
    _require(0 < len(raw) <= MAX_JSON and raw.endswith(b"\n") and b"\r" not in raw,
             "invalid SHA256SUMS envelope")
    try:
        lines = raw.decode("ascii", "strict").splitlines()
    except UnicodeError as error:
        raise Hold("invalid SHA256SUMS encoding") from error
    found = None
    for line in lines:
        _require(len(line) > 67 and line[64:66] in ("  ", " *"),
                 "invalid SHA256SUMS row")
        digest, name = line[:64], line[66:]
        _require(all(c in "0123456789abcdef" for c in digest), "invalid SHA256SUMS digest")
        if name == asset_name:
            _require(found is None, "duplicate SHA256SUMS row")
            found = digest
    _require(found is not None, "asset missing from SHA256SUMS")
    return found


def _producer_namespace(key_raw):
    try:
        tokens = key_raw.decode("ascii", "strict").split()
    except UnicodeError as error:
        raise Hold("invalid allowed-signers encoding") from error
    _require(len(tokens) >= 4 and tokens[0] == PRODUCER_PRINCIPAL,
             "allowed signers principal differs")
    _require(tokens[1] == f'namespaces="{PRODUCER_NAMESPACE}"',
             "allowed signers namespace differs")
    _require(tokens[2] == "ssh-ed25519", "producer key type differs")


def _ssh_verify(*, key_raw, signature_raw, payload):
    tool = shutil.which("ssh-keygen")
    _require(tool is not None, "ssh-keygen is required to verify the producer receipt")
    with tempfile.TemporaryDirectory(prefix="kc-verify-") as tmp:
        signers = Path(tmp) / "allowed_signers"
        sig = Path(tmp) / "receipt.sig"
        _fresh(signers, key_raw, 0o600)
        _fresh(sig, signature_raw, 0o600)
        proc = subprocess.run(
            [tool, "-Y", "verify", "-f", str(signers), "-I", PRODUCER_PRINCIPAL,
             "-n", PRODUCER_NAMESPACE, "-s", str(sig)],
            input=payload, capture_output=True, timeout=60)
    _require(proc.returncode == 0, "producer receipt signature did not verify")


def verify_artifact(directory, *, asset_name, candidate_sha256, authorization):
    """Bind the candidate to a signed producer receipt without executing it."""
    directory = Path(directory)
    _require(directory.is_absolute() and directory.resolve(strict=True) == directory,
             "artifact dir must be absolute and real")
    _require(_stat_ok(directory.lstat(), regular=False), "artifact dir is not a safe owner directory")
    sums = _read(directory / "SHA256SUMS", limit=MAX_JSON)
    _require(_sums_digest(sums, asset_name) == candidate_sha256,
             "artifact digest differs from SHA256SUMS")
    receipt_raw = _read(directory / "producer-receipt.json", limit=MAX_JSON)
    signature_raw = _read(directory / "producer-receipt.json.sig", limit=MAX_JSON)
    key_raw = _read(directory / "producer.pub", limit=MAX_JSON)
    _producer_namespace(key_raw)
    candidate = authorization["candidate"]
    _require(isinstance(candidate.get("producer_key_sha256"), str)
             and _sha(key_raw) == candidate["producer_key_sha256"],
             "producer key differs from the authorized key digest")
    _ssh_verify(key_raw=key_raw, signature_raw=signature_raw, payload=receipt_raw)
    try:
        receipt = json.loads(receipt_raw)
    except json.JSONDecodeError as error:
        raise Hold("producer receipt is not JSON") from error
    _require(isinstance(receipt, dict), "producer receipt must be an object")
    _require(receipt.get("sha256") == candidate_sha256,
             "producer receipt candidate digest differs")
    for field in ("artifact_id", "run_id", "source_sha"):
        _require(field in candidate and receipt.get(field) == candidate[field],
                 f"authorization {field} differs from the producer receipt")
    _require(isinstance(receipt.get("source_sha"), str) and len(receipt["source_sha"]) == 40,
             "producer receipt source SHA is invalid")
    _require(isinstance(receipt.get("version"), str) and receipt["version"].strip(),
             "producer receipt version is missing")
    return {
        "asset_name": asset_name,
        "asset_sha256": candidate_sha256,
        "receipt_sha256": _sha(receipt_raw),
        "signature_sha256": _sha(signature_raw),
        "producer_key_sha256": _sha(key_raw),
        "artifact_id": receipt["artifact_id"],
        "run_id": receipt["run_id"],
        "source_sha": receipt["source_sha"],
        "version": receipt["version"],
        "signature_verified": True,
        "authorizes_operating": False,
    }


def _default_asset_name():
    machine = os.uname().machine
    return f"kc-{sys.platform}-{'arm64' if machine == 'arm64' else machine}"


def prepare(receipts, *, target, entry, candidate, authorization, pre_health,
            artifact_dir=None):
    """Verify byte facts and stage the one-shot preflight record."""
    rdir = _receipts(receipts)
    target, entry, candidate = Path(target), Path(entry), Path(candidate)
    for path in (target, entry):
        _require(path.is_absolute(), "target and entry must be absolute")
        _require(_dir_ok(path.parent), "target or entry directory is not a safe owner directory")
    _require(_entry_ok(entry, target), "entry symlink does not resolve to the target")
    auth_raw = _read(authorization, limit=MAX_JSON, mode=0o600)
    auth = _load_authorization(auth_raw, target=target, entry=entry)
    old_sha, old_mode = _bin_state(target)
    _require(old_sha == auth["previous_sha256"],
             "target bytes differ from the authorized previous digest")
    candidate_raw = _read(candidate, limit=MAX_BINARY)
    new_sha = _sha(candidate_raw)
    _require(new_sha == auth["candidate"]["sha256"],
             "candidate bytes differ from the authorized candidate digest")
    _require(new_sha != old_sha, "candidate is identical to the installed bytes")
    _require(isinstance(pre_health, dict) and pre_health, "pre-install health snapshot required")

    candidate_meta = auth["candidate"]
    signed = any(name in candidate_meta for name in
                 ("artifact_id", "run_id", "source_sha", "producer_key_sha256"))
    _require(artifact_dir is not None or not signed,
             "authorization requires a verified artifact directory")
    artifact = None
    if artifact_dir is not None:
        asset_name = candidate_meta.get("asset_name") or _default_asset_name()
        artifact = verify_artifact(artifact_dir, asset_name=asset_name,
                                   candidate_sha256=new_sha, authorization=auth)

    backup = rdir / ("kc-backup-" + old_sha)
    old_raw = _read(target, limit=MAX_BINARY)
    if backup.exists():
        _require(_read(backup, limit=MAX_BINARY, mode=0o400) == old_raw,
                 "existing backup bytes differ from the target")
    else:
        _fresh(backup, old_raw, 0o400)

    # Byte-level restore proof: the backup must reproduce the previous digest
    # over the candidate bytes inside a disposable scratch directory.
    scratch_dir = rdir / (".scratch-" + secrets.token_hex(8))
    scratch_dir.mkdir(mode=0o700)
    try:
        scratch = scratch_dir / "kc"
        _fresh(scratch, candidate_raw, old_mode)
        _replace(scratch, old_raw, old_mode)
        restored_sha = _sha(_read(scratch, limit=MAX_BINARY))
    finally:
        for leaf in scratch_dir.iterdir():
            leaf.unlink()
        scratch_dir.rmdir()
    _require(restored_sha == old_sha, "backup bytes do not restore the previous binary")

    transaction = secrets.token_hex(32)
    record = {
        "schema": SCHEMA,
        "transaction": transaction,
        "authorization_sha256": _sha(auth_raw),
        "nonce": auth["nonce"],
        "candidate_sha256": new_sha,
        "candidate_path": str(candidate),
        "previous_sha256": old_sha,
        "previous_mode": old_mode,
        "target": str(target),
        "entry": str(entry),
        "backup": str(backup),
        "restored_sha256": restored_sha,
        "pre_health": pre_health,
        "artifact": artifact,
        "prepared_at": _now().isoformat(),
        "expires_at": auth["expires_at"],
        "authorizes_operating": False,
    }
    record["authorization_path"] = str(Path(authorization))
    _fresh(rdir / PREFLIGHT, _json(record))
    return record


def _preflight(rdir):
    raw = _read(rdir / PREFLIGHT, limit=MAX_JSON, mode=0o600)
    record = json.loads(raw)
    _require(isinstance(record, dict) and record.get("schema") == SCHEMA
             and record.get("authorizes_operating") is False, "preflight record is invalid")
    return record, _sha(raw)


def _consume(rdir, *, transaction, nonce, authorization_sha256):
    """Write the one-time consumption record before any target mutation."""
    if (rdir / CONSUMED).exists():
        raise Hold("authorization already consumed")
    _fresh(rdir / CONSUMED, _json({
        "schema": SCHEMA,
        "transaction": transaction,
        "nonce": nonce,
        "authorization_sha256": authorization_sha256,
        "consumed_at": _now().isoformat(),
    }))


def apply(receipts, *, post_health):
    """Replace the target once, then keep the previous bytes restorable."""
    rdir = _receipts(receipts)
    record, _ = _preflight(rdir)
    target, entry = Path(record["target"]), Path(record["entry"])
    _require(_entry_ok(entry, target), "entry symlink no longer resolves to the target")
    _require(_now() < _parse_time(record["expires_at"], "preflight expiry is invalid"),
             "authorization expired after preflight")
    auth_raw = _read_path_for(record)
    _require(_sha(auth_raw) == record["authorization_sha256"],
             "authorization changed after preflight")
    auth = _load_authorization(auth_raw, target=target, entry=entry)
    _require(auth["nonce"] == record["nonce"], "authorization nonce changed after preflight")
    old_sha, _ = _bin_state(target)
    _require(old_sha == record["previous_sha256"],
             "target bytes changed since preflight")
    backup_raw = _read(record["backup"], limit=MAX_BINARY, mode=0o400)
    _require(_sha(backup_raw) == record["previous_sha256"], "backup bytes changed since preflight")
    candidate_raw = _read(record["candidate_path"], limit=MAX_BINARY)
    _require(_sha(candidate_raw) == record["candidate_sha256"],
             "candidate bytes changed since preflight")
    _require(isinstance(post_health, dict) and post_health == record["pre_health"],
             "post-install health differs from the pre-install snapshot")

    _consume(rdir, transaction=record["transaction"], nonce=record["nonce"],
             authorization_sha256=record["authorization_sha256"])
    try:
        _replace(target, candidate_raw, record["previous_mode"])
        observed_sha, observed_mode = _bin_state(target)
        _require(observed_sha == record["candidate_sha256"], "installed bytes drifted")
        receipt = {
            "schema": SCHEMA,
            "result": "REPLACED_OPERATING_BYTES",
            "transaction": record["transaction"],
            "target": str(target),
            "entry": str(entry),
            "authorization_sha256": record["authorization_sha256"],
            "previous_sha256": record["previous_sha256"],
            "observed_sha256": observed_sha,
            "previous_mode": record["previous_mode"],
            "observed_mode": observed_mode,
            "backup": record["backup"],
            "restored_sha256": record["restored_sha256"],
            "post_health": post_health,
            "applied_at": _now().isoformat(),
            "authorizes_operating": False,
        }
        _fresh(rdir / APPLY_RECEIPT, _json(receipt))
        return receipt
    except BaseException:
        restore_raw = _read(record["backup"], limit=MAX_BINARY, mode=0o400)
        _replace(target, restore_raw, record["previous_mode"])
        restored_sha, _ = _bin_state(target)
        _fresh(rdir / APPLY_FAILED, _json({
            "schema": SCHEMA,
            "result": "RESTORED_PREVIOUS_BYTES",
            "transaction": record["transaction"],
            "target": str(target),
            "restored_sha256": restored_sha,
            "previous_bytes_active": restored_sha == record["previous_sha256"],
            "failed_at": _now().isoformat(),
            "authorizes_operating": False,
        }))
        raise


def _read_path_for(record):
    """Read the authorization file recorded at preflight time."""
    path = record.get("authorization_path")
    _require(isinstance(path, str) and path, "preflight does not record the authorization path")
    return _read(path, limit=MAX_JSON, mode=0o600)


def rollback(receipts):
    """Restore the previous bytes once, verified by digest."""
    rdir = _receipts(receipts)
    receipt = json.loads(_read(rdir / APPLY_RECEIPT, limit=MAX_JSON, mode=0o600))
    _require(isinstance(receipt, dict) and receipt.get("schema") == SCHEMA
             and receipt.get("result") == "REPLACED_OPERATING_BYTES",
             "apply receipt is invalid")
    target = Path(receipt["target"])
    _require(_entry_ok(Path(receipt["entry"]), target),
             "entry symlink no longer resolves to the target")
    current_sha, _ = _bin_state(target)
    _require(current_sha == receipt["observed_sha256"],
             "operating bytes changed since apply")
    previous_raw = _read(receipt["backup"], limit=MAX_BINARY, mode=0o400)
    _require(_sha(previous_raw) == receipt["previous_sha256"],
             "backup bytes differ from the applied previous digest")
    marker = rdir / ("rollback-" + receipt["transaction"] + ".json")
    if marker.exists():
        raise Hold("rollback already performed for this transaction")
    _fresh(marker, _json({"schema": SCHEMA, "transaction": receipt["transaction"],
                          "at": _now().isoformat()}))
    _replace(target, previous_raw, receipt["previous_mode"])
    restored_sha, restored_mode = _bin_state(target)
    _require(restored_sha == receipt["previous_sha256"], "restored bytes drifted")
    result = {
        "schema": SCHEMA,
        "result": "RESTORED_PREVIOUS_BYTES",
        "transaction": receipt["transaction"],
        "target": str(target),
        "restored_sha256": restored_sha,
        "restored_mode": restored_mode,
        "authorizes_operating": False,
    }
    _fresh(rdir / ROLLBACK_RECEIPT, _json(result))
    return result


def status(receipts):
    rdir = _receipts(receipts)
    pre = rdir / PREFLIGHT
    return {
        "schema": SCHEMA,
        "consumed": (rdir / CONSUMED).exists(),
        "prepared": pre.exists(),
        "applied": (rdir / APPLY_RECEIPT).exists(),
        "rolled_back": (rdir / ROLLBACK_RECEIPT).exists(),
        "authorizes_operating": False,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("phase", choices=("prepare", "apply", "rollback", "status"))
    parser.add_argument("--receipts", required=True)
    parser.add_argument("--target")
    parser.add_argument("--entry")
    parser.add_argument("--candidate")
    parser.add_argument("--authorization")
    parser.add_argument("--health-json", help="pre/post health snapshot as JSON")
    parser.add_argument("--artifact-dir", help="verified artifact directory with a signed producer receipt")
    args = parser.parse_args(argv)
    health = json.loads(Path(args.health_json).read_bytes()) if args.health_json else None
    try:
        if args.phase == "prepare":
            missing = [name for name in ("target", "entry", "candidate", "authorization")
                       if getattr(args, name) is None]
            _require(not missing, "prepare requires target, entry, candidate, authorization")
            _require(health is not None, "prepare requires health-json")
            result = prepare(args.receipts, target=args.target, entry=args.entry,
                             candidate=args.candidate, authorization=args.authorization,
                             pre_health=health, artifact_dir=args.artifact_dir)
        elif args.phase == "apply":
            _require(health is not None, "apply requires health-json")
            result = apply(args.receipts, post_health=health)
        elif args.phase == "rollback":
            result = rollback(args.receipts)
        else:
            result = status(args.receipts)
    except Hold as error:
        print(f"install_candidate: refused: {error}", file=sys.stderr)
        return 3
    print(json.dumps(result, sort_keys=True, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
