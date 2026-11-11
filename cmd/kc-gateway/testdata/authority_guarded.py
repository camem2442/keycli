"""Connect the normal Gateway process to a synthetic Authority reservation."""
import json
import os
import sys
from pathlib import Path

from authority_execution import gateway, prepare_fixture, verify
from tooling.keycli_guarded_execution import serve_connection


def serve(root, mode):
    state = root / "authority"
    state.mkdir(mode=0o700)
    fixture = prepare_fixture(state, mode)
    preview_path, guarded_path = root / "a.sock", root / "g.sock"
    config_path = root / "authority.json"
    uid = os.geteuid()
    binding = {field: fixture.request[field] for field in gateway.BINDINGS}
    registration = {key: value for key, value in binding.items() if key != "target_digest"}
    for key in ("provider_operation", "canonical_target"):
        registration[key] = fixture.manifest[key]

    def check(wire, lock_fd, deadline):
        outcome = verify(state, root / "journal", mode, wire, lock_fd, deadline)
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
        with preview.accept()[0] as connection:
            gateway.serve_connection(connection, fixture.authority, allowed_uids={uid})
        guarded.settimeout(10)
        serve_connection(guarded.accept()[0], peer_uid=uid, peer_pid=gateway_pid, verify=check)
    assert outcomes == ["GRANT_NOT_REGISTERED" if mode == "revoked" else "approved"], outcomes
    reservation = fixture.authority.claims.reservation(binding["goal_id"], binding["mutation_id"])
    assert reservation["request_digest"] == gateway.intent_digest(binding)
    assert reservation["state"] == "RUNNING"
    assert bool(reservation["execution_claimed"]) == (mode != "revoked")


if __name__ == "__main__":
    serve(Path(sys.argv[1]), sys.argv[2])
