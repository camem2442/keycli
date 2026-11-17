"""Disposable KeyCLI dev bytes CAS fixture regressions. No live kc installation."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest


ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location("keycli_disposable_cas", ROOT/"scripts/dev_cas_pilot.py")
cas=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cas)

TAG="v0.10.0-dev.1"
SHA="a"*40
BODY=b'#!/bin/sh\nif [ "$1" = "--version" ]; then echo "kc version v0.10.0-dev.1 (synthetic)"; fi\n'


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


class DevCASPilotTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix="keycli-dev-cas-fixture-")
        self.addCleanup(self.temp.cleanup)
        self.base=Path(self.temp.name).resolve()
        self.source=self.base/"kc-linux-amd64"
        self.source.write_bytes(BODY)
        self.source.chmod(0o600)
        self.sums=self.base/"SHA256SUMS"
        self.sums.write_text(digest(BODY) + "  kc-linux-amd64\n")
        self.sums.chmod(0o600)
        self.root=Path(cas.init(base=self.base)["root"])
        self.blob=digest(BODY)

    def stage(self, *, tag=TAG, source_head=SHA, digest_value=None, source=None):
        return cas.stage(self.root,source or self.source,
                         expected_sha256=digest_value or self.blob,
                         tag=tag,source_head=source_head,
                         sums_path=self.sums, asset_name="kc-linux-amd64",
                         expected_sums_sha256=digest(self.sums.read_bytes()),
                         allow_disposable=True)

    def select(self, *, expected=None, tag=TAG, digest_value=None):
        return cas.select(self.root,artifact_sha256=digest_value or self.blob,
                          tag=tag,source_head=SHA,
                          expected_previous_pin_sha256=expected,
                          allow_disposable=True)

    def test_empty_root_has_no_operating_side_effects(self):
        result=cas.status(self.root)
        self.assertEqual(result["status"],"DISPOSABLE_EMPTY")
        self.assertIsNone(result["pin_sha256"])
        self.assertFalse(result["authorizes_execution"])
        self.assertFalse(result["authorizes_operating"])
        self.assertTrue(str(self.root).startswith(str(self.base)))
        self.assertNotIn(".local/bin",str(self.root))

    def test_stage_is_content_addressed_and_never_runs_binary(self):
        result=self.stage()
        self.assertEqual(result["status"],"STAGED_DISPOSABLE_BYTES_NOT_TRUSTED")
        self.assertFalse(result["authorizes_execution"])
        binary=self.root/("asset-"+self.blob)
        self.assertEqual(binary.read_bytes(),BODY)
        self.assertEqual(stat.S_IMODE(binary.stat().st_mode),0o400)
        self.assertEqual((self.root/("manifest-"+digest(self.sums.read_bytes()))).read_bytes(),
                         self.sums.read_bytes())
        self.assertEqual(cas.status(self.root)["status"],"DISPOSABLE_EMPTY")
        self.assertEqual(self.stage()["artifact_sha256"],self.blob)

    def test_first_cas_pin_and_external_receipt_are_bound(self):
        self.stage()
        applied=self.select()
        self.assertEqual(applied["status"],"DISPOSABLE_BYTES_SELECTED_NOT_OPERATING")
        self.assertEqual(len(applied["receipt_id"]),64)
        self.assertEqual(len(applied["receipt_sha256"]),64)
        observed=cas.status(self.root)
        self.assertEqual(observed["pin_sha256"],applied["pin_sha256"])
        self.assertEqual(observed["selected"]["source_head"],SHA)
        self.assertFalse(observed["authorizes_operating"])

    def test_rollback_from_empty_is_exact_and_no_replay(self):
        self.stage()
        applied=self.select()
        restored=cas.rollback(self.root,receipt_id=applied["receipt_id"],
                              receipt_sha256=applied["receipt_sha256"],
                              allow_disposable=True)
        self.assertEqual(restored["status"],"DISPOSABLE_ROLLBACK_BYTES_NOT_OPERATING")
        self.assertEqual(cas.status(self.root)["status"],"DISPOSABLE_EMPTY")
        with self.assertRaises((cas.Hold,FileNotFoundError)):
            cas.rollback(self.root,receipt_id=applied["receipt_id"],
                         receipt_sha256=applied["receipt_sha256"],
                         allow_disposable=True)

    def test_new_selection_then_restore_exact_previous_pin(self):
        self.stage()
        first=self.select()
        another=b"keycli-dev-v2-synthetic-bytes"
        folder=self.base/"second"
        folder.mkdir()
        second_file=folder/"kc-linux-amd64"
        second_file.write_bytes(another)
        second_file.chmod(0o600)
        another_sha=digest(another)
        second_sums=folder/"SHA256SUMS"
        second_sums.write_text(another_sha+"  kc-linux-amd64\n")
        second_sums.chmod(0o600)
        cas.stage(self.root,second_file,expected_sha256=another_sha,
                  tag="v0.10.0-dev.2",source_head=SHA,
                  sums_path=second_sums,asset_name="kc-linux-amd64",
                  expected_sums_sha256=digest(second_sums.read_bytes()),
                  allow_disposable=True)
        second=cas.select(self.root,artifact_sha256=another_sha,
                          tag="v0.10.0-dev.2",source_head=SHA,
                          expected_previous_pin_sha256=first["pin_sha256"],
                          allow_disposable=True)
        self.assertEqual(cas.status(self.root)["pin_sha256"],second["pin_sha256"])
        restore=cas.rollback(self.root,receipt_id=second["receipt_id"],
                             receipt_sha256=second["receipt_sha256"],
                             allow_disposable=True)
        self.assertEqual(restore["pin_sha256"],first["pin_sha256"])
        self.assertEqual(cas.status(self.root)["selected"]["tag"],TAG)

    def test_stale_cas_or_foreign_receipt_never_overwrites(self):
        self.stage()
        first=self.select()
        with self.assertRaises(cas.Hold):
            self.select(expected=None)
        with self.assertRaises(cas.Hold):
            self.select(expected="f"*64)
        with self.assertRaises(cas.Hold):
            cas.rollback(self.root,receipt_id=first["receipt_id"],
                         receipt_sha256="f"*64,allow_disposable=True)
        self.assertEqual(cas.status(self.root)["pin_sha256"],first["pin_sha256"])

    def test_source_digest_and_channel_validation_fail_closed(self):
        with self.assertRaises(cas.Hold):
            self.stage(digest_value="f"*64)
        for tag in ("v0.10.0", "latest", "v0.10.0-stable.1", "v01.2.3-dev.1"):
            with self.subTest(tag=tag),self.assertRaises(cas.Hold):
                self.stage(tag=tag)
        with self.assertRaises(cas.Hold):
            self.stage(source_head="not-a-commit")

    def test_stage_binding_prevents_same_bytes_different_source(self):
        self.stage()
        with self.assertRaises(cas.Hold):
            self.stage(tag="v0.10.0-dev.2")
        with self.assertRaises(cas.Hold):
            self.stage(source_head="b"*40)

    def test_source_symlink_refused(self):
        link=self.base/"link"
        link.symlink_to(self.source)
        with self.assertRaises(cas.Hold):
            self.stage(source=link)

    def test_corrupt_staged_content_blocks_selection_and_status(self):
        self.stage()
        staged=self.root/("asset-"+self.blob)
        staged.chmod(0o600)
        staged.write_bytes(b"unexpected")
        staged.chmod(0o500)
        with self.assertRaises(cas.Hold):
            self.select()

    def test_root_owner_permissions_matter(self):
        self.root.chmod(0o755)
        try:
            with self.assertRaises(cas.Hold):
                cas.status(self.root)
        finally:
            self.root.chmod(0o700)

    def test_test_generated_fixture_smoke_is_not_a_live_keycli_claim(self):
        self.stage()
        self.select()
        staged=self.root/("asset-"+self.blob)
        self.assertEqual(stat.S_IMODE(staged.stat().st_mode),0o400)
        # The selected asset remains non-executable. Test-only shell bytes are
        # separately made executable OUTSIDE CAS to avoid trusting a digest.
        fixture=self.base/"test-only-executable"
        fixture.write_bytes(BODY)
        fixture.chmod(0o500)
        probe=subprocess.run([str(fixture),"--version"],cwd=self.root,
                             env={"PATH":"/usr/bin:/bin","HOME":str(self.root)},
                             stdin=subprocess.DEVNULL,capture_output=True,text=True,
                             timeout=5,check=True)
        self.assertEqual(probe.stdout.strip(),"kc version v0.10.0-dev.1 (synthetic)")
        self.assertFalse(cas.status(self.root)["authorizes_operating"])

    def test_existing_user_kc_location_is_never_touched(self):
        live=self.base/".local/bin"
        live.mkdir(parents=True)
        preexisting=live/"kc"
        preexisting.write_bytes(b"existing-user-kc-sentinel")
        self.stage()
        pin=self.select()
        cas.rollback(self.root,receipt_id=pin["receipt_id"],
                     receipt_sha256=pin["receipt_sha256"],
                     allow_disposable=True)
        self.assertEqual(preexisting.read_bytes(),b"existing-user-kc-sentinel")

    def test_signed_pair_shape_rejects_wrong_asset_or_missing_manifest_entry(self):
        with self.assertRaises(cas.Hold):
            cas.stage(self.root,self.source,expected_sha256=self.blob,
                      tag=TAG,source_head=SHA,sums_path=self.sums,
                      asset_name="kc-windows-amd64",
                      expected_sums_sha256=digest(self.sums.read_bytes()),
                      allow_disposable=True)
        self.sums.write_text("a"*64 + "  kc-linux-arm64\n")
        self.sums.chmod(0o600)
        with self.assertRaises(cas.Hold):
            self.stage()
        self.assertEqual(cas.status(self.root)["status"],"DISPOSABLE_EMPTY")

    def test_checksum_duplicate_and_traversal_rejected(self):
        good=self.blob + "  kc-linux-amd64\n"
        for row in (good+good, self.blob+"  ../kc-linux-amd64\n",
                    self.blob+"  ./kc-linux-amd64\n", good.rstrip("\n"),
                    "bad\x00checksum\n"):
            with self.subTest(row=row[:35]):
                self.sums.write_text(row)
                self.sums.chmod(0o600)
                with self.assertRaises(cas.Hold):
                    self.stage()

    def test_selected_manifest_corruption_rejects_status_and_restore(self):
        self.stage()
        pin=self.select()
        sums=self.root/("manifest-"+digest(self.sums.read_bytes()))
        sums.chmod(0o600)
        sums.write_bytes(b"tampered")
        sums.chmod(0o400)
        with self.assertRaises(cas.Hold):
            cas.status(self.root)
        with self.assertRaises(cas.Hold):
            cas.rollback(self.root,receipt_id=pin["receipt_id"],
                         receipt_sha256=pin["receipt_sha256"],
                         allow_disposable=True)

    def test_explicit_disposable_consent_required(self):
        with self.assertRaises(cas.Hold):
            cas.stage(self.root,self.source,expected_sha256=self.blob,
                      tag=TAG,source_head=SHA, sums_path=self.sums,
                      asset_name="kc-linux-amd64",
                      expected_sums_sha256=digest(self.sums.read_bytes()))
        self.stage()
        with self.assertRaises(cas.Hold):
            cas.select(self.root,artifact_sha256=self.blob,
                       tag=TAG,source_head=SHA,
                       expected_previous_pin_sha256=None)


if __name__=="__main__":
    unittest.main()
