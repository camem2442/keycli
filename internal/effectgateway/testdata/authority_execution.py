"""Serve one synthetic PR04 verification with the Go writer lock over Unix IPC."""
import array
import fcntl
import json
import os
import re
import socket
import stat
import struct
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import test_keycli_gateway_authority as fixtures
from tooling.goal_execution_store import SqliteClaimStore

gateway = fixtures.gateway

if not __debug__:
    raise RuntimeError("fixture assertions must be enabled")


def prepare(root, mode):
    fixture = fixtures.GatewayAuthorityTests()
    # Keep the upstream fixture in the Go test's private scratch directory
    # so the verifier can reopen the same registry, grant and SQLite store.
    directory = SimpleNamespace(name=str(root), cleanup=lambda: None)
    tempfile = SimpleNamespace(TemporaryDirectory=lambda: directory)
    with patch.object(fixtures, "tempfile", tempfile):
        fixture.setUp()
    _, _, _, _, decision, admit, _, _, _ = fixture.mutation_fixture()
    overrides = {"resource_digest": "e" * 64} if mode == "reservation-mismatch" else {}
    result = admit(**overrides)
    assert result["reason"] == "ADMITTED", result
    fixture.write(root / "execution.json", {
        "manifest": fixture.manifest,
        "request": fixture.request,
        "decision": decision,
    })
    print(json.dumps({field: fixture.request[field] for field in gateway.BINDINGS}))


class LockedGoJournal:
    def __init__(self, root, received_fd):
        self.root = root
        self.received_fd = received_fd

    @contextmanager
    def locked(self, *, deadline):
        with os.fdopen(os.open(self.root / ".lock", os.O_RDWR | os.O_NOFOLLOW), "rb") as probe:
            expected = os.fstat(probe.fileno())
            received = os.fstat(self.received_fd)
            assert stat.S_ISREG(expected.st_mode) and expected.st_nlink == 1
            assert stat.S_IMODE(expected.st_mode) == 0o600 and expected.st_uid == os.geteuid()
            assert (expected.st_dev, expected.st_ino) == (received.st_dev, received.st_ino)
            try:
                fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                pass
            else:
                raise AssertionError("Go writer lock was available")
            # SCM_RIGHTS must have supplied the held open file description.
            # Another writer's lock would still contend with this descriptor.
            try:
                fcntl.flock(self.received_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise ValueError("WRITER_LOCK_NOT_SHARED") from error
            gateway._check_deadline(deadline)
            yield
            # Never unlock the parent's open file description.

    def execution_view(self, intent):
        open_operations, acknowledged, current = [], [], None
        for path in self.root.glob("*.json"):
            record = gateway._json(path.read_bytes())
            mutation = record["operation_id"]
            assert path.name == f"{mutation}.json"
            if record["target_digest"] != intent["target_digest"]:
                continue
            if record["state"] in ("INTENT_WRITTEN", "DISPATCHED", "UNKNOWN_EFFECT"):
                open_operations.append(mutation)
            if record["state"] in ("ACKNOWLEDGED", "OBSERVED_APPLIED"):
                acknowledged.append(mutation)
            if mutation == intent["mutation_id"]:
                assert record["v"] == 2 and "proof_digest" not in record
                persisted = record["authority_binding"]
                assert persisted["mutation_id"] == mutation
                for field in ("target_digest", "plan_digest", "grant_digest"):
                    assert persisted[field] == record[field]
                current = {"state": record["state"], "intent": persisted}
        return {
            "root_digest": fixtures.ROOT_DIGEST,
            "target_digest": intent["target_digest"],
            "open_operations": open_operations,
            "acknowledged_operations": acknowledged,
        }, current


def verify(root, journal_root, mode, wire, lock_fd, deadline):
    state = gateway._json((root / "execution.json").read_bytes())
    manifest, request = state["manifest"], state["request"]
    claims = SqliteClaimStore(root / "claim.sqlite3")
    intent = {field: request[field] for field in gateway.BINDINGS}
    execution = dict(wire, method="verify_execution")
    coordinator = gateway.MutationCoordinator(claims, LockedGoJournal(journal_root, lock_fd))

    if mode == "revoked":
        registry = gateway._read(manifest["goal_registry"], os.geteuid())
        grant = Path(registry["goals"][0]["authority_ref"])
        grant.write_text(json.dumps({"schema_version": 1, "grants": []}))

    def refresh():
        goal, grants, digest = gateway._authority(manifest, os.geteuid())
        claim = claims.status(request["goal_id"])
        return {
            "goal": goal,
            "grants": grants,
            "grant_digest": digest,
            "claim": {
                "goal_id": request["goal_id"], "state": claim["state"],
                "generation": claim["generation"], "owner": claim["owner"], "session": claim["owner_session"],
            },
            "repository_slugs": {"keycli": "example/keycli"},
            "observed_head": fixtures.HEAD,
            "journal_root": fixtures.ROOT_DIGEST,
        }

    handoffs = []

    def check():
        return coordinator.verify_execution(
            execution, intent=intent, host_request=gateway._host_request(manifest, request["mutation_id"]),
            decision=state["decision"], refresh=refresh, handoff=lambda: handoffs.append("handoff"),
            deadline=deadline,
        )

    try:
        result = check()
        outcome = "approved" if result["valid"] else result["reason"]
    except ValueError as denied:
        outcome = str(denied)
    if outcome == "approved":
        assert check() == {"valid": False, "reason": "EXECUTION_ALREADY_CLAIMED"}
        assert handoffs == ["handoff"]
    else:
        assert not handoffs
    return outcome


def check_client(connection, uid, pid):
    assert gateway._peer_uid(connection) == uid
    if sys.platform == "darwin":
        # SOL_LOCAL, LOCAL_PEERPID
        peer_pid = struct.unpack("i", connection.getsockopt(0, 2, struct.calcsize("i")))[0]
    else:
        credentials = connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize("iII"))
        peer_pid = struct.unpack("iII", credentials)[0]
    assert peer_pid == pid


@contextmanager
def receive_guard(connection, deadline):
    received = array.array("i")
    frame = bytearray()
    try:
        while True:
            connection.settimeout(gateway._remaining(deadline))
            chunk, control, flags, _ = connection.recvmsg(
                min(4096, gateway.MAX_FRAME + 1 - len(frame)),
                socket.CMSG_SPACE(2 * received.itemsize),
            )
            invalid_control = False
            for level, kind, data in control:
                if (level, kind) != (socket.SOL_SOCKET, socket.SCM_RIGHTS):
                    invalid_control = True
                    continue
                complete = len(data) - len(data) % received.itemsize
                first = len(received)
                received.frombytes(data[:complete])
                for descriptor in received[first:]:
                    os.set_inheritable(descriptor, False)
                invalid_control |= complete != len(data)
            assert not invalid_control and not flags & (socket.MSG_CTRUNC | socket.MSG_TRUNC)
            if not chunk:
                break
            frame.extend(chunk)
            assert len(frame) <= gateway.MAX_FRAME
        if not frame and not received:
            yield None, None
            return
        assert len(received) == 1
        wire = gateway._json(frame)
        gateway._exact(wire, {"v", "method", "request_id", *gateway.BINDINGS})
        assert type(wire["v"]) is int and wire["v"] == 1
        assert wire["method"] == "synthetic_guarded_execution"
        assert re.fullmatch(r"[0-9a-f]{32}", wire["request_id"])
        yield wire, received[0]
    finally:
        for descriptor in received:
            os.close(descriptor)


def serve_guarded(root, journal_root, mode, socket_path):
    uid, parent_pid = os.geteuid(), os.getppid()
    deadline = time.monotonic() + 10
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as server:
        server.bind(str(socket_path))
        socket_path.chmod(0o600)
        gateway._check_socket_path(socket_path, uid)
        server.listen(1)
        print("verifier-ready", flush=True)
        server.settimeout(gateway._remaining(deadline))
        connection, _ = server.accept()
        with connection:
            if mode == "wrong-peer":
                # The client closes immediately on its credential mismatch.
                # A closed Darwin peer may already have lost LOCAL_PEERPID.
                with receive_guard(connection, deadline) as (wire, lock_fd):
                    assert wire is None and lock_fd is None
                return
            check_client(connection, uid, parent_pid)
            with receive_guard(connection, deadline) as (wire, lock_fd):
                assert wire is not None
                outcome = verify(root, journal_root, mode, wire, lock_fd, deadline)
            # Closing our duplicate leaves the Go lock held through settlement.
            if mode in {"lost-reply", "stalled-reply", "wrong-reply-binding", "wrong-reply-id", "duplicate-reply-field"}:
                assert outcome == "approved"
            if mode == "lost-reply":
                return
            if mode == "stalled-reply":
                time.sleep(gateway._remaining(deadline))
                return
            if mode == "wrong-reply-binding":
                wire["operation"] = "other-operation"
            if mode == "wrong-reply-id":
                wire["request_id"] = "f" * 32 if wire["request_id"] != "f" * 32 else "0" * 32
            reply = gateway._encode({"request": wire, "result": outcome})
            if mode == "duplicate-reply-field":
                reply = b'{"result":"approved",' + reply[1:]
            connection.settimeout(gateway._remaining(deadline))
            connection.sendall(reply)


def observe_settlement(root, mutation):
    path = root / f"{mutation}.json"
    with (root / ".lock").open("rb") as probe:
        try:
            fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            assert gateway._json(path.read_bytes())["state"] == "DISPATCHED"
            print("lock-contended", flush=True)
        else:
            raise AssertionError("Go writer released the lock before provider returned")
        fcntl.flock(probe, fcntl.LOCK_EX)
        settled = gateway._json(path.read_bytes())
        assert settled["state"] == "ACKNOWLEDGED" and settled["proof_digest"] == settled["plan_digest"]


if __name__ == "__main__":
    action, root, *args = sys.argv[1:]
    if action == "prepare":
        prepare(Path(root), args[0])
    elif action == "serve_guarded":
        serve_guarded(Path(root), Path(args[0]), args[1], Path(args[2]))
    elif action == "settlement":
        observe_settlement(Path(root), args[0])
    else:
        raise AssertionError(f"unknown fixture action: {action}")
