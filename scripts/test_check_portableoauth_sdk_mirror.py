"""Go SDK candidate closed-file-set gate; no Go toolchain/network required."""
from pathlib import Path
import shutil
import sys
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_portableoauth_sdk_mirror as gate


class MirrorCandidateTests(unittest.TestCase):
    def workspace(self, path):
        parent = path / "source"
        candidate = path / "candidate"
        for rel in gate.FILES:
            left, right = parent / rel, candidate / rel
            left.parent.mkdir(parents=True, exist_ok=True)
            right.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(gate.SOURCE / rel, left)
            shutil.copyfile(gate.CANDIDATE / rel, right)
        for rel in ("go.mod", "README.md", "import_test.go"):
            shutil.copyfile(gate.CANDIDATE / rel, candidate / rel)
        return parent, candidate

    def test_exact_candidate_reports_mirror_not_source_attestation(self):
        with TemporaryDirectory() as raw:
            source, candidate = self.workspace(Path(raw))
            with patch.object(gate, "SOURCE", source), patch.object(gate, "CANDIDATE", candidate):
                receipt = gate.check()
            self.assertEqual(receipt["mirrored_files"], len(gate.FILES))
            self.assertTrue(receipt["mirror_identical"])
            self.assertFalse(receipt["source_commit_attested"])
            self.assertFalse(receipt["tag_created"])
            self.assertFalse(receipt["published"])

    def test_new_compiled_file_or_missing_file_holds_candidate(self):
        with TemporaryDirectory() as raw:
            source, candidate = self.workspace(Path(raw))
            with patch.object(gate, "SOURCE", source), patch.object(gate, "CANDIDATE", candidate):
                (candidate / "Surprise.go").write_text("package oauth\n")
                with self.assertRaises(gate.MirrorHeld):
                    gate.check()
                (candidate / "Surprise.go").unlink()
                (candidate / "import_test.go").unlink()
                with self.assertRaises(gate.MirrorHeld):
                    gate.check()

    def test_core_drift_module_metadata_and_symlink_refused(self):
        with TemporaryDirectory() as raw:
            source, candidate = self.workspace(Path(raw))
            with patch.object(gate, "SOURCE", source), patch.object(gate, "CANDIDATE", candidate):
                code = candidate / "flow.go"
                code.write_text(code.read_text() + "\n// drift\n")
                with self.assertRaises(gate.MirrorHeld):
                    gate.check()
                shutil.copyfile(source / "flow.go", code)
                mod = candidate / "go.mod"
                mod.write_text("module latest\n")
                with self.assertRaises(gate.MirrorHeld):
                    gate.check()
                shutil.copyfile(gate.ROOT / "sdk/portableoauth/go.mod", mod)
                (candidate / "README.md").unlink()
                (candidate / "README.md").symlink_to(code)
                with self.assertRaises(gate.MirrorHeld):
                    gate.check()


if __name__ == "__main__":
    unittest.main()
