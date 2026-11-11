"""Drive the Go Gateway through a reconstructed guarded Authority runtime."""
import json
import os
import sys
from pathlib import Path

from authority_execution import authority_fixture, gateway
from authority_guarded import GatewayJournal
from tooling.keycli_admission_decisions import AdmissionDecisionStore
from tooling.keycli_authority_guard import guarded_verdict
from tooling.keycli_authority_journal import AuthorityJournal
from tooling.keycli_go_journal import GoJournal
from tooling.keycli_guarded_execution import serve_connection


def serve(root, mode):
    registry_root, journal_root, decision_root = (root / name for name in ("authority", "journal", "decisions"))
    for directory in (registry_root, journal_root, decision_root):
        directory.mkdir(mode=0o700)
    (journal_root / ".lock").touch(mode=0o600)
    fixture = authority_fixture(registry_root)
    uid = os.geteuid()
    binding = {key: fixture.request[key] for key in gateway.BINDINGS}
    goal, mutation = binding["goal_id"], binding["mutation_id"]
    ledger = GatewayJournal(None, journal_root, root / "ledger")
    observed = []

    def observe(intent, record, *, deadline):
        gateway._remaining(deadline)
        assert intent == binding and record["authority_binding"] == binding
        assert record["state"] == "ACKNOWLEDGED"
        observed.append(intent["mutation_id"])
        return ledger.ledger_outcome(intent, record["proof_digest"])

    def compose():
        journal = AuthorityJournal(
            GoJournal(journal_root), AdmissionDecisionStore(decision_root, uid),
            fixture.manifest["journal_root_digest"], observe_terminal=observe,
        )
        claims = gateway.SqliteClaimStore(fixture.store.path)
        return gateway.Authority(
            fixture.registry, claims.path, repository_slugs={"keycli": "example/keycli"},
            verify_plan=lambda *_, **kwargs: True, observe_head=lambda *_args, **_kwargs: fixture.head,
            read_journal=fixture.read_journal, guarded_runtime=gateway.GuardedMutationRuntime(claims, journal),
        )

    registration = {key: value for key, value in binding.items() if key != "target_digest"}
    registration.update({key: fixture.manifest[key] for key in ("provider_operation", "canonical_target")})
    advisory_path, guarded_path, config = root / "a.sock", root / "g.sock", root / "authority.json"
    authority = compose()
    verdicts = []

    def verify(wire, fd, deadline):
        verdicts.append(guarded_verdict(authority, wire, fd, deadline, peer_uid=uid))
        return verdicts[-1] if mode == "current" else None

    def exchange(listener):
        with listener.accept()[0] as connection:
            gateway.serve_connection(connection, authority, allowed_uids={uid})

    with gateway.listen(advisory_path) as advisory, gateway.listen(guarded_path) as guarded:
        fixture.write(config, {
            "v": 1, "socket_path": str(advisory_path), "socket_uid": uid, "registrations": [registration],
            "guarded_execution": {"socket_path": str(guarded_path), "socket_uid": uid, "socket_pid": os.getpid()},
        })
        print(json.dumps({"db": str(fixture.store.path), "config": str(config), "binding": binding}), flush=True)
        peer_pid = int(sys.stdin.readline())
        advisory.settimeout(10)
        guarded.settimeout(10)
        exchange(advisory)
        exchange(advisory)
        reservation = authority.claims.reservation(goal, mutation)
        assert reservation["budget_confirmed"] == 1 and reservation["execution_claimed"] == 0
        authority = compose()
        serve_connection(guarded.accept()[0], peer_uid=uid, peer_pid=peer_pid, verify=verify)
        assert verdicts == ["AUTHORIZED"]
        authority = compose()
        if mode == "current":
            exchange(advisory)
        assert sys.stdin.readline() == "complete\n"
        if mode != "current":
            finish = authority.evaluate(dict(fixture.request, method="finish"), peer_uid=uid)
            assert finish["reason"] == "UNRESOLVED_EFFECT", finish
    row = authority.claims.reservation(goal, mutation)
    assert row["execution_claimed"] == 1
    assert row["state"] == ("DONE" if mode == "current" else "RUNNING")
    assert set(observed) == ({mutation} if mode == "current" else set())
    grant = json.loads(fixture.grant_path.read_text())["grants"][0]
    assert [(entry["mutation_id"], entry["amount"]) for entry in grant["consumed"]] == [(mutation, 1)]


if __name__ == "__main__":
    serve(Path(sys.argv[1]), sys.argv[2])
