"""Supervise the activated Authority CLI and an independent fixture observer."""
import json
import os
import socket
import subprocess
import sys
import threading
from contextlib import contextmanager
from pathlib import Path

from authority_execution import authority_fixture, gateway
from authority_guarded import GatewayJournal
from test_keycli_gateway_authority import ROOT_DIGEST


def bind_and_exec(root):
    # Linux reports the credentials of the process that called listen().
    # Keep this PID across exec so KeyCLI can pin the real server.
    with gateway.listen(root / "a.sock") as advisory, gateway.listen(root / "g.sock") as guarded:
        print("listeners-ready", flush=True)
        gateway_pid = int(sys.stdin.readline())
        uid = os.geteuid()
        config = root / "service.json"
        config.write_text(json.dumps({
            "v": 1,
            "registry_root": str(root / "authority" / "registry"),
            "claim_db": str(root / "authority" / "claim.sqlite3"),
            "allowed_uids": [uid],
            "repository_slugs": {"keycli": "example/keycli"},
            "socket_path": str(root / "a.sock"),
            "observation": {"socket_path": str(root / "o.sock"), "peer_uid": uid},
            "guarded_execution": {
                "socket_path": str(root / "g.sock"),
                "gateway_uid": uid,
                "gateway_pid": gateway_pid,
                "journal_root": str(root / "journal"),
                "journal_root_digest": ROOT_DIGEST,
                "decision_root": str(root / "decisions"),
            },
        }))
        config.chmod(0o600)
        descriptors = (advisory.fileno(), guarded.fileno())
        for fd in descriptors:
            os.set_inheritable(fd, True)
        os.dup2(sys.stderr.fileno(), sys.stdout.fileno())
        os.execv(sys.executable, [
            sys.executable, "-B", "-m", "tooling.keycli_gateway_authority", "serve",
            "--config", str(config), "--listener-fd", str(descriptors[0]),
            "--guarded-listener-fd", str(descriptors[1]),
        ])


@contextmanager
def cli_process(root):
    with subprocess.Popen(
        [sys.executable, "-B", __file__, str(root), "exec-service"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
    ) as process:
        try:
            assert process.stdout.readline() == "listeners-ready\n"
            yield process
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            for name in ("a.sock", "g.sock"):
                (root / name).unlink(missing_ok=True)


def exchange(path, request):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(5)
        connection.connect(str(path))
        connection.sendall(gateway._encode(request))
        connection.shutdown(socket.SHUT_WR)
        with connection.makefile("rb") as stream:
            reply = gateway._json(stream.read(gateway.MAX_FRAME + 1))
    return gateway.validate_response(request, reply)


def start_cli(process, gateway_pid, path, fixture, expected_state):
    print(gateway_pid, file=process.stdin, flush=True)
    reply = exchange(path, dict(fixture.request, method="status", request_id="cli-ready"))
    assert (reply["status"], reply["reason"], reply["mutation_state"]) == (
        "OK", "STATUS_ONLY", expected_state,
    ), reply


def serve(root, mode):
    authority_root = root / "authority"
    journal_root = root / "journal"
    for path in (authority_root, journal_root, root / "decisions"):
        path.mkdir(mode=0o700)
    (journal_root / ".lock").touch(mode=0o600)
    fixture = authority_fixture(authority_root)
    binding = {key: fixture.request[key] for key in gateway.BINDINGS}
    goal, mutation = binding["goal_id"], binding["mutation_id"]
    ledger = GatewayJournal(None, journal_root, root / "ledger")
    uid = os.geteuid()
    advisory = root / "a.sock"
    observations, failures = [], []
    stop = threading.Event()

    def respond(connection):
        assert gateway._peer_uid(connection) == uid
        connection.settimeout(5)
        with connection.makefile("rb") as stream:
            request = gateway._json(stream.read(gateway.MAX_FRAME + 1))
        if request.get("method") == "observe_terminal":
            record = request["record"]
            assert record["authority_binding"] == binding and record["state"] == "ACKNOWLEDGED"
            assert gateway._read(journal_root / f"{mutation}.json", uid) == record
            outcome = ledger.ledger_outcome(binding, record["proof_digest"])
            assert outcome == "ACK"
            observations.append("ACK")
            response = {**request, "outcome": outcome}
        else:
            for key in ("goal_id", "mutation_id", "operation", "resource_digest", "target_digest", "plan_digest"):
                assert request[key] == binding[key]
            reservation = fixture.authority.claims.reservation(goal, mutation)
            if mode == "restart-unknown" and reservation is not None and reservation["execution_claimed"] == 1:
                # Fail the final fresh observation after the durable claim. The
                # guarded adapter must close without a definitive verdict.
                observations.append("post-claim observer loss")
                return
            observations.append("advisory")
            response = {
                **request,
                "plan_verified": True,
                "observed_head": fixture.head,
                "journal": fixture.read_journal(fixture.manifest, mutation, deadline=None),
            }
        connection.sendall(gateway._encode(response))

    def observe(listener):
        try:
            while not stop.is_set():
                try:
                    connection, _ = listener.accept()
                except socket.timeout:
                    continue
                with connection:
                    respond(connection)
        except Exception as error:
            failures.append(error)

    with gateway.listen(root / "o.sock") as observer:
        observer.settimeout(0.1)
        worker = threading.Thread(target=observe, args=(observer,), daemon=True)
        worker.start()
        try:
            with cli_process(root) as process:
                registration = {key: value for key, value in binding.items() if key != "target_digest"}
                registration.update({key: fixture.manifest[key] for key in ("provider_operation", "canonical_target")})
                config = root / "gateway.json"
                fixture.write(config, {
                    "v": 1, "socket_path": str(advisory), "socket_uid": uid, "registrations": [registration],
                    "guarded_execution": {"socket_path": str(root / "g.sock"), "socket_uid": uid,
                                          "socket_pid": process.pid},
                })
                print(json.dumps({"db": str(fixture.store.path), "config": str(config), "binding": binding}), flush=True)
                gateway_pid = int(sys.stdin.readline())
                # The activated listener already queues requests while the
                # process execs. Only the enrolled Go PID can use this endpoint.
                print(gateway_pid, file=process.stdin, flush=True)
                print("true", flush=True)
                assert sys.stdin.readline() == "complete\n"
                assert process.poll() is None
                # The supervisor has the same UID but is outside the pinned
                # Gateway process identity until it is enrolled on restart.
                with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as wrong_peer:
                    wrong_peer.settimeout(3)
                    wrong_peer.connect(str(advisory))
                    assert wrong_peer.recv(1) == b""

            expected_state = "DONE" if mode == "current" else "RUNNING"
            with cli_process(root) as process:
                start_cli(process, os.getpid(), advisory, fixture, expected_state)
                reply = exchange(advisory, dict(fixture.request, method="finish", request_id="cli-restart-finish"))
                expected = ("OK", "FINISHED", "DONE") if mode == "current" else ("BLOCKED", "UNRESOLVED_EFFECT", None)
                assert (reply["status"], reply["reason"], reply["mutation_state"]) == expected, reply
                assert process.poll() is None
        finally:
            stop.set()
            worker.join(timeout=6)
        assert not worker.is_alive() and not failures, failures

    reservation = fixture.authority.claims.reservation(goal, mutation)
    assert reservation["state"] == expected_state
    assert reservation["execution_claimed"] == reservation["budget_confirmed"] == 1
    assert reservation["request_digest"] == gateway.intent_digest(binding)
    assert "advisory" in observations
    if mode == "current":
        assert observations.count("ACK") == 2 and "post-claim observer loss" not in observations
    else:
        assert "ACK" not in observations and observations.count("post-claim observer loss") == 1
    grant = json.loads(fixture.grant_path.read_text())["grants"][0]
    assert [(entry["mutation_id"], entry["amount"]) for entry in grant["consumed"]] == [(mutation, 1)]


if __name__ == "__main__":
    root, mode = Path(sys.argv[1]), sys.argv[2]
    if mode == "exec-service":
        bind_and_exec(root)
    else:
        serve(root, mode)
