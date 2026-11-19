"""One-shot operating replacement fixture regressions. Never touches a real kc install."""
from datetime import datetime, timedelta, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("keycli_install_candidate",
                                              ROOT / "scripts/install_candidate.py")
inst = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(inst)

OLD = b"#!/bin/sh\necho 'kc version v0.8.0 (synthetic)'\n"
NEW = b"#!/bin/sh\necho 'kc version v0.10.0 (synthetic)'\n"
PLATFORM = f"{sys.platform}/{os.uname().machine}"
HEALTH = {"doctor_completed": True, "metadata_entry_count": 33}


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


class OperatingInstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="keycli-install-fixture-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()

        self.bin_dir = self.root / "bin"
        self.bin_dir.mkdir(mode=0o700)
        self.target = self.bin_dir / "kc"
        self.target.write_bytes(OLD)
        self.target.chmod(0o755)

        self.entry_dir = self.root / "entry"
        self.entry_dir.mkdir(mode=0o700)
        self.entry = self.entry_dir / "kc"
        os.symlink(self.target, self.entry)

        self.receipts = self.root / "receipts"
        self.receipts.mkdir(mode=0o700)

        self.candidate = self.root / "candidate"
        self.candidate.write_bytes(NEW)
        self.candidate.chmod(0o600)

        self.authorization = self.root / "authorization.json"
        self.write_authorization()

    def write_authorization(self, **overrides):
        now = datetime.now(timezone.utc)
        auth = {
            "schema": inst.AUTH_SCHEMA,
            "effect": inst.EFFECT,
            "nonce": "ab" * 32,
            "owner_statement": "synthetic fixture approval",
            "candidate": {"sha256": digest(NEW)},
            "target": {"path": str(self.target), "entry": str(self.entry),
                       "platform": PLATFORM, "uid": os.geteuid()},
            "previous_sha256": digest(OLD),
            "approved_at": (now - timedelta(minutes=5)).isoformat(),
            "expires_at": (now + timedelta(hours=1)).isoformat(),
        }
        auth.update(overrides)
        self.authorization.write_text(json.dumps(auth))
        self.authorization.chmod(0o600)

    def prepare(self):
        return inst.prepare(self.receipts, target=self.target, entry=self.entry,
                            candidate=self.candidate, authorization=self.authorization,
                            pre_health=HEALTH)

    def apply(self, *, health=HEALTH):
        return inst.apply(self.receipts, post_health=health)

    def target_sha(self):
        return digest(self.target.read_bytes())

    # --- prepare -----------------------------------------------------------

    def test_prepare_records_byte_restore_digest(self):
        record = self.prepare()
        self.assertEqual(record["restored_sha256"], digest(OLD))
        self.assertEqual(record["previous_sha256"], digest(OLD))
        self.assertEqual(record["candidate_sha256"], digest(NEW))
        self.assertFalse(record["authorizes_operating"])
        backup = Path(record["backup"])
        self.assertEqual(backup.read_bytes(), OLD)
        self.assertEqual(backup.stat().st_mode & 0o777, 0o400)
        self.assertEqual((self.receipts / inst.PREFLIGHT).stat().st_mode & 0o777, 0o600)
        self.assertEqual(list(self.receipts.glob(".scratch-*")), [])

    def test_prepare_is_one_shot(self):
        self.prepare()
        with self.assertRaises(inst.Hold):
            self.prepare()

    def test_prepare_rejects_candidate_digest_mismatch(self):
        self.write_authorization(candidate={"sha256": "0" * 64})
        with self.assertRaises(inst.Hold):
            self.prepare()
        self.assertEqual(self.target_sha(), digest(OLD))
        self.assertFalse((self.receipts / inst.CONSUMED).exists())

    def test_prepare_rejects_expired_authorization(self):
        past = (datetime.now(timezone.utc) - timedelta(minutes=1)).isoformat()
        self.write_authorization(expires_at=past)
        with self.assertRaises(inst.Hold):
            self.prepare()

    def test_prepare_rejects_foreign_entry_symlink(self):
        other = self.root / "other-kc"
        other.write_bytes(OLD)
        other.chmod(0o755)
        self.entry.unlink()
        os.symlink(other, self.entry)
        with self.assertRaises(inst.Hold):
            self.prepare()

    def test_prepare_rejects_previous_digest_mismatch(self):
        self.write_authorization(previous_sha256="0" * 64)
        with self.assertRaises(inst.Hold):
            self.prepare()

    # --- apply -------------------------------------------------------------

    def test_apply_replaces_bytes_and_consumes_authorization(self):
        self.prepare()
        receipt = self.apply()
        self.assertEqual(self.target_sha(), digest(NEW))
        self.assertEqual(receipt["observed_sha256"], digest(NEW))
        self.assertEqual(receipt["previous_sha256"], digest(OLD))
        self.assertEqual(receipt["restored_sha256"], digest(OLD))
        self.assertFalse(receipt["authorizes_operating"])
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o755)
        self.assertTrue((self.receipts / inst.CONSUMED).exists())
        self.assertTrue(inst.status(self.receipts)["applied"])

    def test_replay_after_previous_bytes_restored_is_refused_by_consumption(self):
        """The fence must not depend on target bytes still looking installed."""
        self.prepare()
        self.apply()
        self.target.write_bytes(OLD)
        self.target.chmod(0o755)
        self.assertEqual(self.target_sha(), digest(OLD))
        with self.assertRaises(inst.Hold) as caught:
            self.apply()
        self.assertIn("already consumed", str(caught.exception))
        self.assertEqual(self.target_sha(), digest(OLD))
        receipts = list(self.receipts.glob("apply-receipt.json"))
        self.assertEqual(len(receipts), 1)

    def test_apply_refuses_drifted_target_without_consuming(self):
        self.prepare()
        self.target.write_bytes(OLD + b"# drift\n")
        self.target.chmod(0o755)
        with self.assertRaises(inst.Hold):
            self.apply()
        self.assertFalse((self.receipts / inst.CONSUMED).exists())
        self.assertFalse((self.receipts / inst.APPLY_RECEIPT).exists())

    def test_apply_refuses_health_mismatch_before_consuming(self):
        self.prepare()
        with self.assertRaises(inst.Hold):
            self.apply(health={"doctor_completed": True, "metadata_entry_count": 32})
        self.assertFalse((self.receipts / inst.CONSUMED).exists())
        self.assertEqual(self.target_sha(), digest(OLD))

    def test_apply_failure_restores_previous_bytes(self):
        self.prepare()
        real = inst._bin_state

        def drifting(path):
            sha, mode = real(path)
            if sha == digest(NEW):
                raise RuntimeError("injected post-replace verification failure")
            return sha, mode

        with mock.patch.object(inst, "_bin_state", drifting):
            with self.assertRaises(RuntimeError):
                self.apply()
        self.assertEqual(self.target_sha(), digest(OLD))
        failed = json.loads((self.receipts / inst.APPLY_FAILED).read_bytes())
        self.assertEqual(failed["restored_sha256"], digest(OLD))
        self.assertTrue(failed["previous_bytes_active"])
        self.assertFalse((self.receipts / inst.APPLY_RECEIPT).exists())

    # --- rollback ----------------------------------------------------------

    def test_rollback_restores_and_is_one_shot(self):
        self.prepare()
        self.apply()
        result = inst.rollback(self.receipts)
        self.assertEqual(result["restored_sha256"], digest(OLD))
        self.assertEqual(self.target_sha(), digest(OLD))
        self.assertTrue(inst.status(self.receipts)["rolled_back"])
        with self.assertRaises(inst.Hold):
            inst.rollback(self.receipts)

    def test_rollback_refuses_when_operating_bytes_changed(self):
        self.prepare()
        self.apply()
        self.target.write_bytes(b"# someone else\n")
        with self.assertRaises(inst.Hold):
            inst.rollback(self.receipts)

    # --- signed artifact binding -------------------------------------------

    def _keygen(self, name):
        key = self.root / name
        subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key)],
                       check=True, capture_output=True)
        key.chmod(0o600)
        return key

    def build_artifact(self, *, version="kc version v0.10.0 (synthetic)"):
        art = self.root / "artifact"
        art.mkdir(mode=0o700)
        asset_name = inst._default_asset_name()
        asset = art / asset_name
        asset.write_bytes(NEW)
        asset.chmod(0o600)
        (art / "SHA256SUMS").write_text(f"{digest(NEW)}  {asset_name}\n")
        receipt = {"sha256": digest(NEW), "artifact_id": 7, "run_id": 9,
                   "source_sha": "c" * 40, "version": version}
        receipt_path = art / "producer-receipt.json"
        receipt_path.write_text(json.dumps(receipt))
        receipt_path.chmod(0o600)
        key = self._keygen("producer-key")
        subprocess.run(["ssh-keygen", "-Y", "sign", "-f", str(key),
                        "-n", inst.PRODUCER_NAMESPACE, str(receipt_path)],
                       check=True, capture_output=True)
        public = key.with_suffix(".pub").read_text().split()
        (art / "producer.pub").write_text(
            f'{inst.PRODUCER_PRINCIPAL} namespaces="{inst.PRODUCER_NAMESPACE}" '
            f'{public[0]} {public[1]}\n')
        (art / "producer.pub").chmod(0o600)
        self.write_authorization(candidate={
            "sha256": digest(NEW), "artifact_id": 7, "run_id": 9, "source_sha": "c" * 40,
            "asset_name": asset_name,
            "producer_key_sha256": digest((art / "producer.pub").read_bytes())})
        return art, asset_name

    def prepare_with_artifact(self, art):
        return inst.prepare(self.receipts, target=self.target, entry=self.entry,
                            candidate=self.candidate, authorization=self.authorization,
                            pre_health=HEALTH, artifact_dir=art)

    def test_prepare_records_signed_artifact_binding(self):
        art, asset_name = self.build_artifact()
        record = self.prepare_with_artifact(art)
        self.assertTrue(record["artifact"]["signature_verified"])
        self.assertEqual(record["artifact"]["artifact_id"], 7)
        self.assertEqual(record["artifact"]["run_id"], 9)
        self.assertEqual(record["artifact"]["asset_name"], asset_name)
        self.assertEqual(record["artifact"]["receipt_sha256"],
                         digest((art / "producer-receipt.json").read_bytes()))
        self.assertFalse(record["artifact"]["authorizes_operating"])

    def test_prepare_rejects_tampered_receipt(self):
        art, _ = self.build_artifact()
        (art / "producer-receipt.json").write_text(json.dumps(
            {"sha256": digest(NEW), "artifact_id": 7, "run_id": 9,
             "source_sha": "c" * 40, "version": "kc version v9.9.9 (tampered)"}))
        with self.assertRaises(inst.Hold):
            self.prepare_with_artifact(art)
        self.assertEqual(self.target_sha(), digest(OLD))

    def test_prepare_rejects_asset_digest_mismatch(self):
        art, asset_name = self.build_artifact()
        (art / "SHA256SUMS").write_text(f"{'0' * 64}  {asset_name}\n")
        with self.assertRaises(inst.Hold):
            self.prepare_with_artifact(art)

    def test_prepare_rejects_unlisted_producer_key(self):
        art, _ = self.build_artifact()
        other = self._keygen("other-key")
        public = other.with_suffix(".pub").read_text().split()
        (art / "producer.pub").write_text(
            f'{inst.PRODUCER_PRINCIPAL} namespaces="{inst.PRODUCER_NAMESPACE}" '
            f'{public[0]} {public[1]}\n')
        (art / "producer.pub").chmod(0o600)
        with self.assertRaises(inst.Hold):
            self.prepare_with_artifact(art)

    def test_prepare_requires_artifact_for_signed_authorization(self):
        self.write_authorization(candidate={"sha256": digest(NEW), "artifact_id": 7})
        with self.assertRaises(inst.Hold):
            self.prepare()

    # --- status ------------------------------------------------------------

    def test_status_empty_receipts(self):
        result = inst.status(self.receipts)
        self.assertFalse(result["prepared"])
        self.assertFalse(result["consumed"])
        self.assertFalse(result["authorizes_operating"])


if __name__ == "__main__":
    unittest.main()
