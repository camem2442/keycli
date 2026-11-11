"""Exercise the PR04 coordinator against the actual Go journal writer flock."""
import fcntl
import json
import sys
import time
from contextlib import contextmanager
from pathlib import Path

from tooling import keycli_gateway_authority as authority
from tooling.goal_execution_store import SqliteClaimStore


def run(root, original):
    goal = original["goal_id"]
    mutation = original["mutation_id"]
    record_path = root / f"{mutation}.json"
    claims = SqliteClaimStore(root.parent / "claim.sqlite3")
    claims.request_claim(goal, original["owner"], "initial", session=original["session"])

    class GoJournalLock:
        """Expose only the journal operation used by takeover without activation."""

        @contextmanager
        def locked(self, *, deadline):
            # Open the existing lock without creating a second lock file.
            with (root / ".lock").open("r+b") as lock:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError:
                    assert claims.status(goal)["generation"] == original["generation"]
                    assert json.loads(record_path.read_text())["state"] == "DISPATCHED"
                    print("lock-contended", flush=True)
                    # Wait in the kernel. The parent subprocess timeout bounds this wait.
                    fcntl.flock(lock, fcntl.LOCK_EX)
                else:
                    raise AssertionError("Go journal lock was available during Apply")
                if time.monotonic() >= deadline:
                    raise TimeoutError("Go journal lock deadline")
                yield

    takeover = {key: original[key] for key in (
        "goal_id", "mutation_id", "provider", "target_digest", "plan_digest", "grant_digest",
    )}
    takeover.update(
        v=1, method="request_takeover", request_id="takeover", operation="fixture-operation",
        owner="successor", session="recovery-only", writer_generation=original["generation"] + 1,
        resource_digest="4" * 64,
    )

    def resolve_successor(request):
        # This is the first callback after lock acquisition, before any claim write.
        record = json.loads(record_path.read_text())
        assert record["state"] == "ACKNOWLEDGED", record
        assert record["proof_digest"] == original["plan_digest"], record
        return {field: request[field] for field in ("owner", "session")}

    result = authority.MutationCoordinator(claims, GoJournalLock()).request_takeover(
        takeover, resolve_successor=resolve_successor, deadline=time.monotonic() + 15,
    )
    assert result["claim_acquired"], result
    status = claims.status(goal)
    assert status["generation"] == takeover["writer_generation"], status
    assert status["owner"] == takeover["owner"], status


if __name__ == "__main__":
    run(Path(sys.argv[1]), json.loads(sys.argv[2]))
