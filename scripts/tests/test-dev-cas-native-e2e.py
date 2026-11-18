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

    def test_personal_local_requires_sealed_host_and_no_runner_session(self):
        value = sample()
        value.allow_native_disposable_exec = True
        value.personal_local = True
        with patch.object(e2e.platform, "system", return_value="Linux"), \
                patch.object(e2e.platform, "machine", return_value="aarch64"), \
                patch.object(e2e.os, "geteuid", return_value=1001):
            with patch.dict(e2e.os.environ, admitted(), clear=True):
                with self.assertRaisesRegex(e2e.Hold, "PERSONAL_LOCAL_REQUIRES_NO_RUNNER_SESSION"):
                    e2e.preflight(value)
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp).resolve()
                value.old_dir = str(root / "old")
                value.new_dir = str(root / "new")
                for directory in (value.old_dir, value.new_dir):
                    Path(directory).mkdir(mode=0o700)
                with patch.dict(e2e.os.environ, {}, clear=True), \
                        patch.object(e2e.os, "geteuid", return_value=root.stat().st_uid), \
                        patch.object(e2e, "observe_sealed_runtime", return_value={}):
                    with self.assertRaisesRegex(e2e.Hold, "SEALED_RUNTIME_IDENTITY_MISSING"):
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
            root = Path(tmp).resolve()
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

    def test_original_git_head_and_worktree_are_required_before_binary_exec(self):
        e2e.check_original_checkout("b" * 40, "", "b" * 40)
        with self.assertRaisesRegex(e2e.Hold, "CHECKOUT_NEW_SOURCE_DRIFT"):
            e2e.check_original_checkout("c" * 40, "", "b" * 40)
        with self.assertRaisesRegex(e2e.Hold, "CHECKOUT_WORKTREE_DIRTY"):
            e2e.check_original_checkout("b" * 40, " M scripts/dev_cas_native_e2e.py", "b" * 40)

    def test_unavailable_git_source_never_becomes_native_pass(self):
        from unittest.mock import Mock
        with patch.object(e2e.subprocess, "run", return_value=Mock(
                returncode=1, stdout="", stderr="no checkout")):
            with self.assertRaisesRegex(e2e.Hold, "SOURCE_GIT_INSPECTION_UNAVAILABLE"):
                e2e.observe_checkout()

    def test_sealed_rootless_boundary_is_required_not_runner_name_alone(self):
        probe = {"uid": 1001, "gid": 1001, "root_uid": 0,
                 "no_new_privileges": True, "capabilities_zero": True,
                 "credential_paths_absent": True, "interfaces": ["lo"],
                 "mounts": {"/": ["ro"], "/work": ["rw"],
                            "/opt/keycli-go-modcache": ["ro"]},
                 "uid_maps": [[0, 100000, 1001], [1001, 1001, 1]]}
        self.assertIsNone(e2e.check_sealed_runtime(probe))
        for updated in ({"interfaces": ["eth0", "lo"]},
                        {"credential_paths_absent": False},
                        {"capabilities_zero": False},
                        {"no_new_privileges": False},
                        {"root_uid": 1001},
                        {"mounts": {"/": ["rw"], "/work": ["rw"],
                                    "/opt/keycli-go-modcache": ["ro"]}},
                        {"uid_maps": [[0, 0, 4294967295]]}):
            with self.subTest(updated=updated):
                with self.assertRaises(e2e.Hold):
                    e2e.check_sealed_runtime({**probe, **updated})

    def test_sealed_runtime_missing_proof_never_accepts_host(self):
        for observation in (None, {}, {"uid": 1001},
                            {"uid": True, "gid": 1001}):
            with self.subTest(observation=observation):
                with self.assertRaises(e2e.Hold):
                    e2e.check_sealed_runtime(observation)

    def test_real_source_checks_use_kernel_not_caller_receipt(self):
        text = (ROOT / "scripts/dev_cas_native_e2e.py").read_text()
        for piece in ("/proc/self/status", "/proc/self/mountinfo",
                      "/proc/self/uid_map", "socket.if_nameindex",
                      "check_sealed_runtime(observe_sealed_runtime())"):
            self.assertIn(piece, text)
        self.assertNotIn("trust_host_admission=True", text)

    def test_no_network_or_live_install_in_wrapper_source(self):
        code = (ROOT / "scripts/dev_cas_native_e2e.py").read_text()
        self.assertNotIn("gh release download", code)
        self.assertNotIn("requests.get(", code)
        self.assertNotIn("~/.local/bin/kc", code)
        self.assertIn("allow_native_disposable_exec", code)
        self.assertIn("original_host_verified", code)


if __name__ == "__main__":
    unittest.main()
