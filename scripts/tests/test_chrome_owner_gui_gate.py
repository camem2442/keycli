"""Synthetic console gate tests: no macOS API or private profile accessed."""
import os
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from chrome_owner_gui_gate import ConsoleFacts, GuiNotAdmitted, local_gui_preflight, may_attempt_local_gui, require_local_gui_attempt


class ConsoleGateTests(unittest.TestCase):
    def facts(self, **kw):
        base = dict(darwin=True, euid=501, console_uid=501, ssh_session=False, ci_runner=False)
        base.update(kw)
        return ConsoleFacts(**base)

    def test_gui_candidate_is_not_native_acceptance(self):
        self.assertTrue(may_attempt_local_gui(self.facts()))
        with patch("chrome_owner_gui_gate.observe_local_console", return_value=self.facts()):
            r = local_gui_preflight()
        self.assertEqual(r["status"], "HOLD")
        self.assertEqual(r["reason"], "GUI_OPERATOR_AND_TCC_UNVERIFIED")
        self.assertFalse(r["native_acceptance"])
        self.assertFalse(r["authorizes_execution"])

    def test_linux_never_attempts_owner_gui(self):
        self.assertFalse(may_attempt_local_gui(self.facts(darwin=False)))

    def test_root_never_attempts_owner_gui(self):
        self.assertFalse(may_attempt_local_gui(self.facts(euid=0, console_uid=0)))

    def test_headless_mac_ci_is_denied(self):
        self.assertFalse(may_attempt_local_gui(self.facts(ci_runner=True)))

    def test_ssh_shell_to_owner_account_is_denied(self):
        self.assertFalse(may_attempt_local_gui(self.facts(ssh_session=True)))

    def test_other_console_user_is_denied(self):
        self.assertFalse(may_attempt_local_gui(self.facts(console_uid=502)))
        self.assertFalse(may_attempt_local_gui(self.facts(console_uid=None)))

    def test_launch_guard_refuses_non_gui_without_secret_echo(self):
        with patch("chrome_owner_gui_gate.observe_local_console", return_value=self.facts(ci_runner=True)):
            with self.assertRaises(GuiNotAdmitted) as cm:
                require_local_gui_attempt()
            self.assertNotIn("chrome", str(cm.exception).lower())
            self.assertNotIn("PASSWORD", str(cm.exception))

    def test_launch_guard_allows_only_candidate_not_credentials(self):
        with patch("chrome_owner_gui_gate.observe_local_console", return_value=self.facts()):
            self.assertIsNone(require_local_gui_attempt())


if __name__ == "__main__":
    unittest.main()
