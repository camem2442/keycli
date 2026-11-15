"""Exercise the PR04 coordinator against the actual Go journal writer flock."""
import fcntl
import json
import sys
import time
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from pathlib import Path

from tooling import keycli_gateway_authority as authority
from tooling.goal_execution_store import SqliteClaimStore


def run(root, original, mode):
    goal = original["goal_id"]
    mutation = original["mutation_id"]
    record_path = root / f"{mutation}.json"
    claims = SqliteClaimStore(root.parent / "claim.sqlite3")
    now = datetime.now(timezone.utc)
    claims.request_claim(goal, original["owner"], "initial", session=original["session"], now=now)
    request = {key: original[key] for key in (
        "goal_id", "mutation_id", "owner", "session", "provider", "target_digest", "plan_digest", "grant_digest",
    )}
    request.update(
        v=1, method="admit", request_id="admission", operation="fixture-operation",
        writer_generation=original["generation"], resource_digest="4" * 64,
    )
    intent = {key: request[key] for key in authority.BINDINGS}
    if mode == "crash":
        # Seed the exact SQL reservation a synthetic dispatch would leave behind.
        # The Go Gateway owns the actual dispatch marker under test.
        admitted = claims.admit_mutation(
            goal, request["writer_generation"], mutation, now=now, executor=request["owner"],
            session=request["session"], resource_digest=request["resource_digest"],
            request_digest=authority.intent_digest(request),
        )
        assert admitted["reason"] == "ADMITTED", admitted

    def read_record():
        record = json.loads(record_path.read_text())
        assert record["operation_id"] == mutation, record
        for field in ("target_digest", "plan_digest", "grant_digest"):
            assert record[field] == original[field], record
        return record

    class GoJournalLock:
        """Fixture adapter for the actual Go lock and immutable dispatch evidence."""

        def __init__(self):
            self.contended = False
            self.recovery_checks = 0
            self.reconciliations = 0

        @contextmanager
        def locked(self, *, deadline):
            # Open the existing lock without creating a second lock file.
            with (root / ".lock").open("r+b") as lock:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError:
                    assert not self.contended, "unexpected contention after Go process exited"
                    assert claims.status(goal)["generation"] == original["generation"]
                    assert read_record()["state"] == "DISPATCHED"
                    self.contended = True
                    print("lock-contended", flush=True)
                    # Wait in the kernel. The parent subprocess timeout bounds this wait.
                    fcntl.flock(lock, fcntl.LOCK_EX)
                else:
                    assert self.contended, "Go journal lock was available during Apply"
                if time.monotonic() >= deadline:
                    raise TimeoutError("Go journal lock deadline")
                yield

        def recovery_clear(self, goal_id):
            assert goal_id == goal
            self.recovery_checks += 1
            return self.terminal_outcome(intent) != "UNKNOWN"

        def terminal_outcome(self, original_intent):
            assert original_intent == intent
            record = read_record()
            if not record.get("proof_digest"):
                return "UNKNOWN"
            return {"ACKNOWLEDGED": "ACK", "OBSERVED_APPLIED": "ACK", "OBSERVED_NO_EFFECT": "NO_EFFECT"}.get(
                record["state"], "UNKNOWN",
            )

        def reconcile(self, original_intent):
            self.reconciliations += 1
            return self.terminal_outcome(original_intent)

    takeover = dict(
        request, method="request_takeover", request_id="takeover",
        owner="successor", session="recovery-only", writer_generation=original["generation"] + 1,
    )

    def resolve_successor(request):
        # This is the first callback after lock acquisition, before any claim write.
        record = read_record()
        if mode == "crash":
            assert record["state"] == "DISPATCHED" and not record.get("proof_digest"), record
        else:
            assert record["state"] == "ACKNOWLEDGED", record
            assert record["proof_digest"] == original["plan_digest"], record
        return {field: request[field] for field in ("owner", "session")}

    def unexpected_activation():
        raise AssertionError("an abandoned dispatch activated the successor")

    journal = GoJournalLock()
    coordinator = authority.MutationCoordinator(claims, journal)
    deadline = time.monotonic() + 15
    takeover_at = now + claims.lease_ttl + timedelta(seconds=1) if mode == "crash" else now
    result = coordinator.request_takeover(
        takeover, resolve_successor=resolve_successor, now=takeover_at, deadline=deadline,
        activation=unexpected_activation if mode == "crash" else None,
    )
    assert result["claim_acquired"], result
    status = claims.status(goal)
    assert status["generation"] == takeover["writer_generation"], status
    assert status["owner"] == takeover["owner"], status
    if mode == "crash":
        assert result["drain"]["orphaned"] == [mutation], result
        assert journal.recovery_checks == 1
        assert not result["resume_ready"] and "activation" not in result, result
        recovery = coordinator.recover(
            dict(takeover, method="verify_execution"), original_intent=intent,
            authorize_read=lambda *_: True, activation=unexpected_activation, now=takeover_at, deadline=deadline,
        )
        assert journal.reconciliations == 1
        assert recovery == {"ok": False, "reason": "UNRESOLVED_EFFECT"}, recovery
        record = claims.mutation_record(goal, mutation, include_evidence=True)
        assert record["state"] == "ORPHANED" and record["evidence"] is None, record
        status = claims.status(goal)
        assert status["state"] == "RECONCILING" and not status["resume_ready"], status


if __name__ == "__main__":
    run(Path(sys.argv[1]), json.loads(sys.argv[2]), sys.argv[3])
