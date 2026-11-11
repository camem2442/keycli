"""Serve the workspace-infra synthetic Authority fixture for one Gateway submit."""
import json
import os
import sys
from pathlib import Path
from unittest.mock import patch

from test_keycli_gateway_authority import (
    BASE, GOAL, FixtureMutationJournal, GatewayAuthorityTests, gateway,
)


def serve(root, changed_head):
    fixture = GatewayAuthorityTests()
    try:
        fixture.setUp()
        journal = FixtureMutationJournal(
            fixture.read_journal(fixture.manifest, fixture.request["mutation_id"], deadline=None)
        )
        effects = []
        fixture.wire_runtime(journal, effects)
        authority = fixture.authority
        authority.read_journal = fixture.read_journal
        fixture.journal_reads.clear()
        db_before = fixture.store.path.read_bytes()
        grant_before = fixture.grant_path.read_bytes()

        socket_path = root / "a.sock"
        config_path = root / "authority.json"
        registration = {field: fixture.request[field] for field in gateway.BINDINGS
                        if field != "target_digest"}
        for field in ("provider_operation", "canonical_target"):
            registration[field] = fixture.manifest[field]
        fixture.write(config_path, {
            "v": 1, "socket_path": str(socket_path), "socket_uid": os.geteuid(),
            "registrations": [registration],
        })

        requests, replies = [], []
        evaluate = authority.evaluate_until

        def capture(request, *, peer_uid, deadline):
            if request["method"] == "preview_execution":
                intent = {field: request[field] for field in gateway.BINDINGS}
                path = root / "journal" / f"{intent['mutation_id']}.json"
                record = json.loads(path.read_text())
                fixture.assertEqual(record["v"], 2)
                fixture.assertEqual(record["state"], "DISPATCHED")
                fixture.assertEqual(record["authority_binding"], intent)

                def read_execution_journal(manifest, mutation_id, *, deadline):
                    snapshot = fixture.read_journal(manifest, mutation_id, deadline=deadline)
                    snapshot["open_operations"] = [mutation_id]
                    return gateway.execution_journal(
                        snapshot, {"state": record["state"], "intent": record["authority_binding"]}, intent,
                    )

                authority.read_journal = read_execution_journal
            reply = evaluate(request, peer_uid=peer_uid, deadline=deadline)
            requests.append(request)
            replies.append(reply)
            return reply

        with gateway.listen(socket_path) as listener, \
                patch.object(authority, "evaluate_until", side_effect=capture), \
                patch.object(authority, "_evaluate_mutation") as mutate, \
                patch.object(gateway, "spend_bound_budget") as spend:
            listener.settimeout(10)
            print(json.dumps({
                "db": str(fixture.store.path),
                "config": str(config_path),
                "binding": {field: fixture.request[field] for field in gateway.BINDINGS},
            }), flush=True)
            for method in ("preview_admission", "preview_execution"):
                if method == "preview_execution" and changed_head:
                    fixture.head = BASE
                connection, _ = listener.accept()
                with connection:
                    gateway.serve_connection(connection, authority, allowed_uids={os.geteuid()})

            fixture.assertEqual([request["method"] for request in requests],
                                ["preview_admission", "preview_execution"])
            fixture.assertRegex(requests[0]["request_id"], r"^[a-f0-9]{32}$")
            fixture.assertRegex(requests[1]["request_id"], r"^[a-f0-9]{32}$")
            fixture.assertNotEqual(requests[0]["request_id"], requests[1]["request_id"])
            for request in requests:
                fixture.assertEqual({field: request[field] for field in gateway.BINDINGS},
                                    {field: fixture.request[field] for field in gateway.BINDINGS})
            advisory = ("ADVISORY", "EXECUTION_FENCE_UNAVAILABLE")
            expected_execution = ("BLOCKED", "HEAD_CHANGED") if changed_head else advisory
            fixture.assertEqual([(reply["status"], reply["reason"]) for reply in replies],
                                [advisory, expected_execution])
            for reply in replies:
                fixture.assertIs(reply["authorizes_execution"], False)
                fixture.assertIsNone(reply["mutation_state"])
            mutate.assert_not_called()
            spend.assert_not_called()

        mutation_id = fixture.request["mutation_id"]
        fixture.assertEqual(fixture.journal_reads, [mutation_id, mutation_id])
        fixture.assertEqual(effects, [])
        fixture.assertIsNone(fixture.store.mutation_record(GOAL, mutation_id))
        fixture.assertEqual(fixture.store.path.read_bytes(), db_before)
        fixture.assertEqual(fixture.grant_path.read_bytes(), grant_before)
    finally:
        fixture.doCleanups()


if __name__ == "__main__":
    serve(Path(sys.argv[1]), changed_head=sys.argv[2] == "changed-head")
