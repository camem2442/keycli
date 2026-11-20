"""No Mac credentials or physical Mac required for pure OS-boundary policy."""
import pathlib
import socket
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
from chrome_owner_os_boundary import evaluate_peer, inspect_owner_socket, macos_peer_ids, BoundaryDenied


class PeerBoundaryTests(unittest.TestCase):
    def check(self, **changed):
        fields = dict(owner_uid=501, agent_uid=502, peer_uid=502,
                      owner_console_uid=501, owner_is_gui=True)
        fields.update(changed)
        return evaluate_peer(**fields)

    def test_real_cross_uid_is_still_only_a_hold(self):
        p = self.check()
        self.assertEqual(p.status, "HOLD")
        self.assertFalse(p.authorizes_execution)

    def test_same_uid_mac_agent_is_never_isolated(self):
        self.assertEqual(self.check(agent_uid=501, peer_uid=501).reason, "SAME_UID_AGENT_UNISOLATED")

    def test_peer_mismatch_blocks(self):
        self.assertEqual(self.check(peer_uid=503).reason, "PEER_UID_MISMATCH")

    def test_unknown_or_root_peer_blocks(self):
        for peer in (None, 0, True, 501):
            self.assertEqual(self.check(peer_uid=peer).status, "BLOCKED")

    def test_root_or_boolean_user_identity_blocks(self):
        for who in (0, -1, True):
            self.assertEqual(self.check(owner_uid=who).status, "BLOCKED")

    def test_wrong_or_unverified_gui_blocks(self):
        self.assertEqual(self.check(owner_console_uid=502).reason, "OWNER_GUI_UNVERIFIED")
        self.assertEqual(self.check(owner_is_gui=False).reason, "OWNER_GUI_UNVERIFIED")

    def test_linux_cannot_claim_owner_kernel_peer(self):
        a, b = socket.socketpair()
        self.addCleanup(a.close); self.addCleanup(b.close)
        with patch("chrome_owner_os_boundary.sys.platform", "linux"):
            self.assertEqual(inspect_owner_socket(a, expected_agent_uid=502, owner_console_uid=501, owner_is_gui=True).status, "BLOCKED")
            with self.assertRaises(BoundaryDenied):
                macos_peer_ids(a)

    def test_peer_read_failure_blocks_without_fallback(self):
        a, b = socket.socketpair()
        self.addCleanup(a.close); self.addCleanup(b.close)
        with patch("chrome_owner_os_boundary.sys.platform", "darwin"), patch("chrome_owner_os_boundary.macos_peer_ids", side_effect=BoundaryDenied("DENIED")):
            self.assertEqual(inspect_owner_socket(a, expected_agent_uid=502, owner_console_uid=501, owner_is_gui=True).reason, "PEER_ID_UNAVAILABLE")


if __name__ == "__main__":
    unittest.main()
