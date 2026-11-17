"""Strict development release status parsing: never infer native or operating PASS."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "dev_release_strict.py"
SPEC = importlib.util.spec_from_file_location("kc_dev_release_strict", SCRIPT)
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)
REQUIRED = gate.REQUIRED


def records(replacement=None):
    rows = {name: "PASS" for name in REQUIRED}
    rows.update(replacement or {})
    return "".join(f"{key}\t{value}\n" for key, value in rows.items()).encode()


class StrictNativeReceiptTests(unittest.TestCase):
    def test_complete_individual_checks_are_only_non_attested_source_feedback(self):
        verdict = gate.evaluate(records({"host_asset": "PASS (kc-linux-arm64)"}))
        self.assertEqual(verdict["status"], "PASS_ALL_REQUIRED_CHECKS_NOT_ATTESTED")
        self.assertEqual(verdict["problems"], [])
        for flag in ("original_host_verified", "producer_verified", "authorizes_execution",
                     "authorizes_install", "authorizes_operating", "authorizes_merge"):
            self.assertIs(verdict[flag], False)

    def test_missing_previous_install_is_never_accepted(self):
        self.assertIn("NOT_PASS_EXISTING_INSTALL_UNCHANGED",
                      gate.evaluate(records({"existing_install_unchanged": "SKIP (clean HOME)"}))["problems"])

    def test_missing_provider_gateway_is_never_accepted(self):
        self.assertIn("NOT_PASS_PROVIDER_LOCK",
                      gate.evaluate(records({"provider_lock": "SKIP (gateway absent)"}))["problems"])

    def test_failure_and_untrusted_pass_prefix_never_accepted(self):
        for value in ("FAIL", "PASS spoof", "PASS (", "PASSAGE", "RUNNING", ""):
            with self.subTest(value=value):
                self.assertEqual(gate.evaluate(records({"mcp_smoke": value}))["status"],
                                 "HOLD_MANDATORY_CHECKS")

    def test_missing_and_duplicate_rows_refused(self):
        rows = records()
        self.assertIn("MISSING_MCP_SMOKE",
                      gate.evaluate(rows.replace(b"mcp_smoke\tPASS\n", b""))["problems"])
        self.assertIn("INVALID_DUPLICATE_OR_UNKNOWN_CHECK",
                      gate.evaluate(rows + b"checksums\tPASS\n")["problems"])

    def test_unknown_rows_and_malformed_payload_refused(self):
        for payload in (records() + b"unknown\tPASS\n", records() + b"checksums PASS\n",
                        b"", b"x" * (gate.MAX_BYTES + 1), records() + b"\xff"):
            with self.subTest(payload=payload[:20]):
                self.assertEqual(gate.evaluate(payload)["status"], "HOLD_MANDATORY_CHECKS")

    def test_file_cli_is_bounded_and_receipt_is_non_authoritative(self):
        with tempfile.TemporaryDirectory() as temp:
            report = Path(temp) / "checks.tsv"
            report.write_bytes(records())
            self.assertEqual(gate.main([str(report)]), 0)
            report.write_bytes(records({"provider_lock": "SKIP"}))
            self.assertEqual(gate.main([str(report)]), 3)

    def test_no_real_keycli_binary_or_provider_effect_in_source(self):
        source = SCRIPT.read_text()
        self.assertNotIn("subprocess", source)
        self.assertNotIn("gh release", source)
        self.assertNotIn("KC_PROVIDER_EFFECTS=enabled", source)
        self.assertEqual(len(REQUIRED), 7)


if __name__ == "__main__":
    unittest.main()
