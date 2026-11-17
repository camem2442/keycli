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
        self.source=self.base/"kc-fixture"
        self.source.write_bytes(BODY)
        self.source.chmod(0o600)
        self.root=Path(cas.init(base=self.base)["root"])
        self.blob=digest(BODY)

    def stage(self, *, tag=TAG, source_head=SHA, digest_value=None, source=None):
        return cas.stage(self.root,source or self.source,
                         expected_sha256=digest_value or self.blob,
                         tag=tag,source_head=source_head,allow_disposable=True)

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
        self.assertEqual(stat.S_IMODE(binary.stat().st_mode),0o500)
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
        second_file=self.base/"second"
        second_file.write_bytes(another)
        second_file.chmod(0o600)
        another_sha=digest(another)
        cas.stage(self.root,second_file,expected_sha256=another_sha,
                  tag="v0.10.0-dev.2",source_head=SHA,allow_disposable=True)
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
        fixture=self.root/("asset-"+self.blob)
        # Only an original TEST-GENERATED inert binary is invoked by this
        # unit test; the CAS product module never runs an arbitrary artifact.
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

    def test_explicit_disposable_consent_required(self):
        with self.assertRaises(cas.Hold):
            cas.stage(self.root,self.source,expected_sha256=self.blob,
                      tag=TAG,source_head=SHA)
        self.stage()
        with self.assertRaises(cas.Hold):
            cas.select(self.root,artifact_sha256=self.blob,
                       tag=TAG,source_head=SHA,
                       expected_previous_pin_sha256=None)


if __name__=="__main__":
    unittest.main()
