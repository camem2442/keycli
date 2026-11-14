"""Exercise a lost admission reply with the guarded Authority journal."""
import json
import os
import socket
import sys
from pathlib import Path
from unittest.mock import patch

from authority_execution import authority_fixture, gateway
from tooling.keycli_admission_decisions import AdmissionDecisionStore
from tooling.keycli_authority_journal import AuthorityJournal
from tooling.keycli_go_journal import GoJournal


root, journal_root, endpoint = map(Path, sys.argv[1:])
root.mkdir(mode=0o700)
journal_root.mkdir(mode=0o700)
(journal_root / ".lock").touch(mode=0o600)
decisions = root / "decisions"
decisions.mkdir(mode=0o700)
fixture = authority_fixture(root)
uid = os.geteuid()
binding = {field: fixture.request[field] for field in gateway.BINDINGS}


def compose():
    claims = gateway.SqliteClaimStore(fixture.store.path)
    journal = AuthorityJournal(GoJournal(journal_root), AdmissionDecisionStore(decisions, uid),
                               fixture.manifest["journal_root_digest"],
                               observe_terminal=lambda *_args, **_kwargs: "UNKNOWN")
    return gateway.Authority(
        fixture.registry, claims.path, repository_slugs={"keycli": "example/keycli"},
        verify_plan=lambda *_args, **_kwargs: True,
        observe_head=lambda *_args, **_kwargs: fixture.head,
        read_journal=fixture.read_journal, guarded_runtime=gateway.GuardedMutationRuntime(claims, journal),
    )


with gateway.listen(endpoint) as listener:
    listener.settimeout(8)
    print(json.dumps(binding), flush=True)
    authority = compose()
    for method, reason, state in (
        ("preview_admission", "EXECUTION_FENCE_UNAVAILABLE", None),
        ("admit", "ADMITTED", "RUNNING"),
        ("finish", "FINISHED", "FAILED"),
        ("finish", "FINISHED", "FAILED"),
    ):
        if method == "finish":
            authority = compose()
        evaluate = authority.evaluate_until
        with listener.accept()[0] as connection:
            def capture(request, *, peer_uid, deadline):
                assert request["method"] == method
                assert {field: request[field] for field in gateway.BINDINGS} == binding
                reply = evaluate(request, peer_uid=peer_uid, deadline=deadline)
                assert (reply["reason"], reply["mutation_state"]) == (reason, state), reply
                if method == "admit":
                    connection.shutdown(socket.SHUT_WR)
                return reply

            with patch.object(authority, "evaluate_until", side_effect=capture):
                gateway.serve_connection(connection, authority, allowed_uids={uid})
    row = authority.claims.reservation(binding["goal_id"], binding["mutation_id"])
    assert (row["state"], row["execution_claimed"], row["budget_confirmed"]) == ("FAILED", 0, 1)
