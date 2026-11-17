"""Source tests for isolated native KeyCLI CAS adapter; never run real kc."""
import importlib.util
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "native_dev_cas_source", ROOT / "scripts/dev_cas_native_e2e.py")
e2e = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(e2e)


def sample():
    return e2e.parser().parse_args([
        "--old-dir", "/tmp/private-old", "--new-dir", "/tmp/private-new",
        "--old-head", "a" * 40, "--new-head", "b" * 40,
        "--old-tag", "v0.10.0-dev.1", "--new-tag", "v0.10.0-dev.2",
        "--old-sha", "a" * 64, "--new-sha", "b" * 64,
        "--old-sums-sha", "c" * 64, "--new-sums-sha", "d" * 64,
        "--new-gateway-sha", "e" * 64,
    ])


def admitted():
    return dict(RUNNER_NAME="keycli-ephemeral-vm-arm64-abc",
                RUNNER_OS="Linux", RUNNER_ARCH="ARM64",
                GITHUB_RUN_ATTEMPT="1", GITHUB_TOKEN="", GH_TOKEN="")


class NativeSourceBoundaryTests(unittest.TestCase):
    def test_default_refuses_execution(self):
        value = sample()
        with self.assertRaisesRegex(e2e.Hold, "EXPLICIT_NATIVE_OPT_IN_REQUIRED"):
            e2e.preflight(value)

    def test_wrong_host_never_attempts_an_executable(self):
        value = sample()
        value.allow_native_disposable_exec = True
        with patch.object(e2e.platform, "system", return_value="Darwin"), \
                patch.object(e2e.platform, "machine", return_value="arm64"):
            with self.assertRaisesRegex(e2e.Hold, "NATIVE_NONROOT_ARM64_REQUIRED"):
                e2e.preflight(value)

    def test_unknown_runner_refused_even_on_native_arm64(self):
        value = sample()
        value.allow_native_disposable_exec = True
        with patch.object(e2e.platform, "system", return_value="Linux"), \
                patch.object(e2e.platform, "machine", return_value="aarch64"), \
                patch.object(e2e.os, "geteuid", return_value=1001), \
                patch.dict(e2e.os.environ, {**admitted(), "RUNNER_NAME": "flip3-a14-wi"}, clear=True):
            with self.assertRaisesRegex(e2e.Hold, "ONE_SHOT_PRIVATE_RUNNER_REQUIRED"):
                e2e.preflight(value)

    def test_credential_presence_rejected(self):
        value = sample()
        value.allow_native_disposable_exec = True
        with patch.object(e2e.platform, "system", return_value="Linux"), \
                patch.object(e2e.platform, "machine", return_value="aarch64"), \
                patch.object(e2e.os, "geteuid", return_value=1001), \
                patch.dict(e2e.os.environ, {**admitted(), "GH_TOKEN": "should-refuse"}, clear=True):
            with self.assertRaisesRegex(e2e.Hold, "CREDENTIAL_ENVIRONMENT_PRESENT"):
                e2e.preflight(value)

    def test_stable_tag_and_same_source_refused(self):
        value = sample()
        value.allow_native_disposable_exec = True
        with patch.object(e2e.platform, "system", return_value="Linux"), \
                patch.object(e2e.platform, "machine", return_value="aarch64"), \
                patch.object(e2e.os, "geteuid", return_value=1001), \
                patch.dict(e2e.os.environ, admitted(), clear=True):
            value.new_tag = "v0.8.0"
            with self.assertRaisesRegex(e2e.Hold, "NOT_DEV_OR_RC_VERSION"):
                e2e.preflight(value)
            value.new_tag = "v0.10.0-dev.2"
            value.new_head = value.old_head
            with self.assertRaisesRegex(e2e.Hold, "OLD_AND_NEW_MUST_BE_DISTINCT"):
                e2e.preflight(value)

    def test_exact_source_report_only_never_authorizes_effect(self):
        doc = {
            "schema": "keycli.dev-release-strict.v1",
            "status": "PASS_ALL_REQUIRED_CHECKS_NOT_ATTESTED",
            "checks": {key: "PASS" for key in e2e.REQUIRED},
            "problems": [], "original_host_verified": False,
            "authorizes_operating": False,
        }
        report = json.dumps(doc) + "\n" + json.dumps({
            "status": "PASS", "tag": "v0.10.0-dev.2"}) + "\n"
        e2e.strict_receipt(report, "v0.10.0-dev.2")
        doc["checks"]["provider_lock"] = "SKIP"
        with self.assertRaisesRegex(e2e.Hold, "MANDATORY_NATIVE_CHECK_NOT_PASS"):
            e2e.strict_receipt(json.dumps(doc) + "\n" +
                               json.dumps({"status": "PASS", "tag": "v0.10.0-dev.2"}),
                               "v0.10.0-dev.2")

    def test_duplicate_strict_receipt_is_not_a_pass(self):
        doc = {"schema": "keycli.dev-release-strict.v1",
               "status": "PASS_ALL_REQUIRED_CHECKS_NOT_ATTESTED",
               "checks": {n: "PASS" for n in e2e.REQUIRED},
               "problems": [], "authorizes_operating": False,
               "original_host_verified": False}
        lines = json.dumps(doc) + "\n" + json.dumps(doc) + "\n" + json.dumps({
            "status": "PASS", "tag": "v0.10.0-dev.2"})
        with self.assertRaisesRegex(e2e.Hold, "ORIGINAL_PRODUCT_REPORT_MISSING"):
            e2e.strict_receipt(lines, "v0.10.0-dev.2")

    def test_private_source_refuses_symlink_and_unpinned_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            item = root / "kc-linux-arm64"
            item.write_bytes(b"demo-untrusted-bytes")
            item.chmod(0o600)
            sha = e2e.digest(item.read_bytes())
            self.assertEqual(e2e.owner_file(item, sha, name=item.name),
                             b"demo-untrusted-bytes")
            with self.assertRaisesRegex(e2e.Hold, "ASSET_SHA_MISMATCH"):
                e2e.owner_file(item, "0" * 64, name=item.name)
            link = root / "alias"
            link.symlink_to(item)
            with self.assertRaises(e2e.Hold):
                e2e.owner_file(link, sha, name="alias")

    def test_no_network_or_live_install_in_wrapper_source(self):
        code = (ROOT / "scripts/dev_cas_native_e2e.py").read_text()
        self.assertNotIn("gh release download", code)
        self.assertNotIn("requests.get(", code)
        self.assertNotIn("~/.local/bin/kc", code)
        self.assertIn("allow_native_disposable_exec", code)
        self.assertIn("original_host_verified", code)


if __name__ == "__main__":
    unittest.main()
