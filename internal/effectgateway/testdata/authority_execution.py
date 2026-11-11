"""Exercise PR04 verification under the actual Go journal writer lock."""
import fcntl
import json
import os
import stat
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
    def __init__(self, root, inherited_fd):
        self.root = root
        self.inherited_fd = inherited_fd

    @contextmanager
    def locked(self, *, deadline):
        with os.fdopen(os.open(self.root / ".lock", os.O_RDWR | os.O_NOFOLLOW), "rb") as probe:
            expected = os.fstat(probe.fileno())
            inherited = os.fstat(self.inherited_fd)
            assert stat.S_ISREG(expected.st_mode) and expected.st_nlink == 1
            assert stat.S_IMODE(expected.st_mode) == 0o600 and expected.st_uid == os.geteuid()
            assert (expected.st_dev, expected.st_ino) == (inherited.st_dev, inherited.st_ino)
            try:
                fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                pass
            else:
                raise AssertionError("Go writer lock was available")
            # The trusted parent supplied the held open file description.
            # Another writer's lock would still contend with this descriptor.
            try:
                fcntl.flock(self.inherited_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise ValueError("WRITER_LOCK_NOT_INHERITED") from error
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


def verify(root, journal_root, mode):
    state = gateway._json((root / "execution.json").read_bytes())
    manifest, request = state["manifest"], state["request"]
    claims = SqliteClaimStore(root / "claim.sqlite3")
    intent = {field: request[field] for field in gateway.BINDINGS}
    coordinator = gateway.MutationCoordinator(claims, LockedGoJournal(journal_root, 3))

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
    execution = dict(request, method="verify_execution", request_id="bound-execution")
    deadline = time.monotonic() + 5

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
    print(outcome)


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
    elif action == "verify":
        verify(Path(root), Path(args[0]), args[1])
    elif action == "settlement":
        observe_settlement(Path(root), args[0])
    else:
        raise AssertionError(f"unknown fixture action: {action}")
