"""Exercise preview, reservation, guarded execution, and finish over Unix sockets."""
import fcntl
import json
import os
import socket
import stat
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from unittest.mock import patch

from authority_execution import authority_fixture, gateway, verify
from test_keycli_gateway_authority import FixtureMutationJournal
from tooling.keycli_guarded_execution import serve_connection


class GatewayJournal(FixtureMutationJournal):
    def __init__(self, snapshot, journal_root, ledger_path):
        super().__init__(snapshot)
        self.root = journal_root
        self.ledger_path = ledger_path
        self.lock_identity = None

    @contextmanager
    def locked(self, *, deadline):
        with super().locked(deadline=deadline):
            fd = os.open(self.root / ".lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
            try:
                info = os.fstat(fd)
                assert stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                assert stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.geteuid()
                while True:
                    gateway._check_deadline(deadline)
                    try:
                        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        time.sleep(min(0.01, gateway._remaining(deadline)))
                identity = (info.st_dev, info.st_ino)
                assert self.lock_identity is None or self.lock_identity == identity
                self.lock_identity = identity
                gateway._check_deadline(deadline)
                yield
            finally:
                os.close(fd)

    def terminal_outcome(self, intent):
        assert self.lock.locked()
        assert self.current["intent"] == intent
        mutation_id = intent["mutation_id"]
        try:
            record = gateway._read(self.root / f"{mutation_id}.json", os.geteuid())
        except FileNotFoundError:
            return "UNKNOWN"
        assert type(record["v"]) is int and record["v"] == 2
        assert record["operation_id"] == mutation_id
        assert record["authority_binding"] == intent
        for field in ("target_digest", "plan_digest", "grant_digest"):
            assert record[field] == intent[field]

        if record["state"] == "ACKNOWLEDGED":
            return self.ledger_outcome(intent, record["proof_digest"])
        if record["state"] == "OBSERVED_NO_EFFECT":
            proof = f"effectgateway|no-effect|{mutation_id}|EXECUTION_FENCE_REJECTED".encode()
            assert record["proof_digest"] == gateway._digest(proof)
            assert not self.ledger_path.exists()
            return "NO_EFFECT"
        assert record["state"] in ("INTENT_WRITTEN", "DISPATCHED", "UNKNOWN_EFFECT")
        assert "proof_digest" not in record
        return "UNKNOWN"

    def ledger_outcome(self, intent, proof_digest):
        try:
            with gateway._safe_file(self.ledger_path, os.geteuid()) as stream:
                raw = stream.read(gateway.MAX_FRAME + 1)
        except FileNotFoundError:
            return "UNKNOWN"
        assert len(raw) <= gateway.MAX_FRAME and raw.endswith(b"\n") and raw.count(b"\n") == 1
        entry = gateway._json(raw)
        # Match the synthetic Go provider's field order and pre-hash encoding.
        unsigned = {
            "seq": 1,
            "at": entry["at"],
            "mutation_id": intent["mutation_id"],
            "target_digest": intent["target_digest"],
            "owner": intent["owner"],
            "session": intent["session"],
            "generation": intent["writer_generation"],
            "prev_hash": "",
            "hash": "",
        }
        digest = gateway._digest(b"|" + gateway._encode(unsigned))
        assert raw == gateway._encode(dict(unsigned, hash=digest)) + b"\n"
        assert proof_digest == digest
        return "ACK"


def serve(root, mode):
    state = root / "authority"
    state.mkdir(mode=0o700)
    fixture = authority_fixture(state)
    journal_root = root / "journal"
    # Simulate an observer that cannot see the provider ledger, even when Go
    # has locally recorded a provider acknowledgment.
    ledger_path = root / ("unobserved-ledger" if mode == "unobserved-ack" else "ledger")
    journal = GatewayJournal(
        fixture.read_journal(fixture.manifest, fixture.request["mutation_id"], deadline=None),
        journal_root, ledger_path,
    )
    effects = []
    fixture.wire_runtime(journal, effects)
    authority = fixture.authority
    authority.read_journal = fixture.read_journal
    preview_path, guarded_path = root / "a.sock", root / "g.sock"
    config_path = root / "authority.json"
    uid = os.geteuid()
    binding = {field: fixture.request[field] for field in gateway.BINDINGS}
    goal_id, mutation_id = binding["goal_id"], binding["mutation_id"]
    assert authority.claims.reservation(goal_id, mutation_id) is None
    registration = {key: value for key, value in binding.items() if key != "target_digest"}
    for key in ("provider_operation", "canonical_target"):
        registration[key] = fixture.manifest[key]

    requests = []
    evaluate = authority.evaluate_until

    def exchange(connection, expected, *, drop_reply=False):
        def capture(request, *, peer_uid, deadline):
            assert {field: request[field] for field in gateway.BINDINGS} == binding
            requests.append(request)
            reply = evaluate(request, peer_uid=peer_uid, deadline=deadline)
            gateway.validate_response(request, reply)
            assert (request["method"], reply["status"], reply["reason"], reply["mutation_state"]) == expected
            assert reply["authorizes_execution"] is False
            if request["method"] in ("admit", "finish") and reply["status"] == "OK":
                assert reply["request_digest"] == gateway.intent_digest(binding)
                if reply["reason"] == "ADMITTED":
                    assert reply["admission_digest"] == journal.decision["admission_digest"]
            if drop_reply:
                connection.shutdown(socket.SHUT_WR)
            return reply

        with patch.object(authority, "evaluate_until", side_effect=capture):
            gateway.serve_connection(connection, authority, allowed_uids={uid})

    def assert_reservation(*, execution_claimed, reservation_state="RUNNING"):
        reservation = authority.claims.reservation(goal_id, mutation_id)
        expected = {
            "goal_id": goal_id,
            "mutation_id": mutation_id,
            "generation": binding["writer_generation"],
            "executor": binding["owner"],
            "session": binding["session"],
            "request_digest": gateway.intent_digest(binding),
            "resource_digest": binding["resource_digest"],
            "decision_digest": journal.decision["admission_digest"],
            "state": reservation_state,
            "budget_confirmed": 1,
            "execution_claimed": int(execution_claimed),
        }
        assert {field: reservation[field] for field in expected} == expected
        assert bool(reservation["ended_at"]) == (reservation_state != "RUNNING")
        assert authority.claims.reservation_inventory() == [reservation]

    def assert_one_spend():
        grant = json.loads(fixture.grant_path.read_text())["grants"][0]
        assert [(row["mutation_id"], row["amount"]) for row in grant["consumed"]] == [(mutation_id, 1)]

    def check(wire, lock_fd, deadline):
        lock = os.fstat(lock_fd)
        assert journal.lock_identity == (lock.st_dev, lock.st_ino)
        outcome = verify(state, journal_root, mode, wire, lock_fd, deadline)
        expected = "GRANT_NOT_REGISTERED" if mode == "revoked" else "approved"
        assert outcome == expected, outcome
        outcomes.append(outcome)
        if mode == "lost-reply":
            return None
        return "DENIED" if mode == "revoked" else "AUTHORIZED"

    outcomes = []
    with gateway.listen(preview_path) as preview, gateway.listen(guarded_path) as guarded:
        fixture.write(config_path, {
            "v": 1, "socket_path": str(preview_path), "socket_uid": uid,
            "registrations": [registration],
            "guarded_execution": {"socket_path": str(guarded_path), "socket_uid": uid, "socket_pid": os.getpid()},
        })
        print(json.dumps({"db": str(fixture.store.path), "config": str(config_path), "binding": binding}), flush=True)
        gateway_pid = int(sys.stdin.readline())
        preview.settimeout(10)
        advisory = ("preview_admission", "ADVISORY", "EXECUTION_FENCE_UNAVAILABLE", None)
        lost_admission_reply = mode == "lost-admission-reply"
        reservation_state = "RUNNING"
        admission_reasons = ("ADMITTED", "DUPLICATE") if lost_admission_reply else ("ADMITTED",)
        for reason in admission_reasons:
            with preview.accept()[0] as connection:
                exchange(connection, advisory)
            if reason == "ADMITTED":
                assert authority.claims.reservation(goal_id, mutation_id) is None
            with preview.accept()[0] as connection:
                exchange(connection, ("admit", "OK", reason, "RUNNING"),
                         drop_reply=lost_admission_reply and reason == "ADMITTED")
            assert journal.current["intent"] == binding
            assert_reservation(execution_claimed=False)
            assert_one_spend()

        if not lost_admission_reply:
            fixture.write(state / "execution.json", {
                "manifest": fixture.manifest,
                "request": fixture.request,
                "decision": journal.decision,
            })
            guarded.settimeout(10)
            serve_connection(guarded.accept()[0], peer_uid=uid, peer_pid=gateway_pid, verify=check)

        if mode in ("current", "revoked", "lost-finish-reply", "unobserved-ack"):
            if mode == "unobserved-ack":
                finish = ("finish", "BLOCKED", "UNRESOLVED_EFFECT", None)
            else:
                reservation_state = "FAILED" if mode == "revoked" else "DONE"
                finish = ("finish", "OK", "FINISHED", reservation_state)
            with preview.accept()[0] as connection:
                exchange(connection, finish, drop_reply=mode == "lost-finish-reply")

        assert sys.stdin.readline() == "complete\n"
        for listener in (preview, guarded):
            listener.setblocking(False)
            try:
                unexpected, _ = listener.accept()
            except BlockingIOError:
                pass
            else:
                unexpected.close()
                raise AssertionError("unexpected Authority connection after the last expected request")

    expected_count = {
        "lost-admission-reply": 4, "lost-reply": 2,
        "current": 3, "revoked": 3, "lost-finish-reply": 3, "unobserved-ack": 3,
    }[mode]
    assert len(requests) == len({request["request_id"] for request in requests}) == expected_count
    assert effects == []
    expected_outcomes = [] if lost_admission_reply else ["GRANT_NOT_REGISTERED" if mode == "revoked" else "approved"]
    assert outcomes == expected_outcomes, outcomes
    assert_reservation(execution_claimed=not lost_admission_reply and mode != "revoked",
                       reservation_state=reservation_state)


if __name__ == "__main__":
    serve(Path(sys.argv[1]), sys.argv[2])
