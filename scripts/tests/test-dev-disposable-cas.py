#!/usr/bin/env python3
"""Portable original KeyCLI development byte CAS regression; no real kc effects."""
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "dev_disposable_cas", ROOT / "scripts/dev-disposable-cas.py")
cas = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cas)

SHA = "a" * 40
VERSION = "v0.10.0-dev.1"


class DevDisposableCASTests(unittest.TestCase):
    def setUp(self):
        self.owned = tempfile.TemporaryDirectory(prefix="keycli-dev-cas-fixture-")
        self.addCleanup(self.owned.cleanup)
        self.parent = Path(self.owned.name).resolve()
        self.root = self.parent / "kc-dev-preview-test"
        self.asset = self.parent / "kc-linux-amd64"
        self.sums = self.parent / "SHA256SUMS"
        self.data = b"\x7fELFportable-unsigned-development-fixture"
        self.asset.write_bytes(self.data)
        self.asset.chmod(0o600)
        self.write_sums()
        cas.create(self.root, allow_disposable_write=True)

    def write_sums(self, contents=None):
        if contents is None:
            contents = cas._hash(self.data) + "  " + self.asset.name + "\n"
        self.sums.write_text(contents)
        self.sums.chmod(0o600)

    def stage(self, *, prior=None, **changes):
        args = dict(asset_path=self.asset, sums_path=self.sums, version=VERSION,
                    source_head=SHA, expected_asset_sha256=cas._hash(self.data),
                    expected_sums_sha256=cas._hash(self.sums.read_bytes()),
                    expected_previous=prior, allow_disposable_write=True)
        args.update(changes)
        return cas.stage(self.root, **args)

    def test_one_disposable_stage_and_rollback_preserves_live_install(self):
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")
        first = self.stage()
        state = cas.status(self.root)
        self.assertEqual(state["status"], "DISPOSABLE_STAGED")
        self.assertEqual(state["pin_sha256"], first["pin_sha256"])
        self.assertEqual(state["asset_sha256"], cas._hash(self.data))
        artifact = self.root / ("artifact-" + cas._hash(self.data))
        self.assertEqual(artifact.read_bytes(), self.data)
        self.assertEqual(artifact.stat().st_mode & 0o777, 0o400)
        for key in ("authorizes_execution", "authorizes_install",
                    "authorizes_operating", "actual_service_healthy",
                    "provider_effects_enabled"):
            self.assertIs(first[key], False, key)
        undone = cas.rollback(self.root, receipt_id=first["receipt_id"],
                              receipt_sha256=first["receipt_sha256"],
                              allow_disposable_write=True)
        self.assertEqual(undone["status"], "DISPOSABLE_BYTES_ROLLED_BACK_NOT_RUNNING")
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")
        self.assertTrue(artifact.exists())
        with self.assertRaises(ValueError):
            cas.rollback(self.root, receipt_id=first["receipt_id"],
                         receipt_sha256=first["receipt_sha256"],
                         allow_disposable_write=True)

    def test_two_disposable_versions_restore_exact_prior_state(self):
        first = self.stage()
        second = self.stage(prior=first["pin_sha256"])
        self.assertNotEqual(first["pin_sha256"], second["pin_sha256"])
        cas.rollback(self.root, receipt_id=second["receipt_id"],
                     receipt_sha256=second["receipt_sha256"],
                     allow_disposable_write=True)
        self.assertEqual(cas.status(self.root)["pin_sha256"], first["pin_sha256"])
        cas.rollback(self.root, receipt_id=first["receipt_id"],
                     receipt_sha256=first["receipt_sha256"],
                     allow_disposable_write=True)
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")

    def test_explicit_write_consent_required_and_no_source_effects(self):
        with self.assertRaises(ValueError):
            cas.stage(self.root, asset_path=self.asset, sums_path=self.sums,
                      version=VERSION, source_head=SHA,
                      expected_asset_sha256=cas._hash(self.data),
                      expected_sums_sha256=cas._hash(self.sums.read_bytes()),
                      expected_previous=None)
        with self.assertRaises(ValueError):
            cas.rollback(self.root, receipt_id="0" * 32,
                         receipt_sha256="f" * 64)
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")

    def test_bad_preimage_cannot_replace_selected_bytes(self):
        first = self.stage()
        with self.assertRaises(ValueError):
            self.stage(prior=None)
        with self.assertRaises(ValueError):
            self.stage(prior="0"*64)
        self.assertEqual(cas.status(self.root)["pin_sha256"], first["pin_sha256"])

    def test_wrong_checksums_and_stable_version_fail_closed(self):
        for changes in ({"expected_asset_sha256":"0"*64},
                        {"expected_sums_sha256":"0"*64},
                        {"version":"v0.10.0"},
                        {"version":"latest"},
                        {"source_head":"bad"}):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                self.stage(**changes)
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")

    def test_checksum_duplicates_and_traversal_are_rejected(self):
        good = cas._hash(self.data) + "  " + self.asset.name + "\n"
        for text in (good + good, cas._hash(self.data)+"  ../kc-linux-amd64\n",
                     cas._hash(self.data)+"  ./kc-linux-amd64\n",
                     good.rstrip("\n"), "broken\x00checksum\n"):
            with self.subTest(text=text[:30]):
                self.write_sums(text)
                with self.assertRaises(ValueError):
                    self.stage()
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")

    def test_symlinked_source_does_not_enter_selector(self):
        link = self.parent / "kc-linux-arm64"
        link.symlink_to(self.asset)
        with self.assertRaises(ValueError):
            self.stage(asset_path=link)
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")

    def test_tampered_receipt_refuses_rollback_without_writing_new_pin(self):
        first = self.stage()
        with self.assertRaises(ValueError):
            cas.rollback(self.root, receipt_id=first["receipt_id"],
                         receipt_sha256="0"*64, allow_disposable_write=True)
        self.assertEqual(cas.status(self.root)["pin_sha256"], first["pin_sha256"])
        with self.assertRaises((ValueError, OSError)):
            cas.rollback(self.root, receipt_id="0"*32,
                         receipt_sha256=first["receipt_sha256"],
                         allow_disposable_write=True)
        self.assertEqual(cas.status(self.root)["pin_sha256"], first["pin_sha256"])

    def test_candidate_corruption_is_not_operating_health(self):
        first = self.stage()
        path = self.root / ("artifact-" + cas._hash(self.data))
        path.chmod(0o600)
        path.write_bytes(b"tampered")
        path.chmod(0o400)
        with self.assertRaises(ValueError):
            cas.status(self.root)
        with self.assertRaises(ValueError):
            cas.rollback(self.root, receipt_id=first["receipt_id"],
                         receipt_sha256=first["receipt_sha256"],
                         allow_disposable_write=True)

    def test_no_adoption_of_existing_nonprivate_or_live_path(self):
        with self.assertRaises(ValueError):
            cas.create(self.root, allow_disposable_write=True)
        with self.assertRaises(ValueError):
            cas.status(self.parent / "not-preview")
        self.assertEqual(cas.status(self.root)["status"], "DISPOSABLE_EMPTY")


if __name__ == "__main__":
    unittest.main()
