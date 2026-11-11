#!/usr/bin/env python3
"""Exercise the release job wiring and fail-closed release operations."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / "scripts/release-contract.sh"
WORKFLOW = ROOT / ".github/workflows/release.yml"
VERSION = "v0.10.0-rc.2"
ASSETS = (
    "kc-darwin-arm64", "kc-darwin-amd64", "kc-linux-arm64", "kc-linux-amd64",
    "kc-gateway-linux-amd64", "kc-gateway-linux-arm64", "keycli-linux-amd64",
    "provenance.json",
)


class ReleaseContractTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.repo = Path(temp.name)
        self.git("init", "-q")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "release@example.invalid")
        self.git("commit", "-qm", "tagged source", "--allow-empty")
        self.source = self.git("rev-parse", "HEAD")
        self.git("tag", "-a", VERSION, "-m", "reviewed annotated tag")
        self.tag_object = self.git("rev-parse", f"refs/tags/{VERSION}")
        self.git("commit", "-qm", "unrelated dispatch source", "--allow-empty")
        self.workflow_sha = self.git("rev-parse", "HEAD")
        self.output = self.repo / "github-output"
        self.env = {
            "RELEASE_VERSION": VERSION,
            "SOURCE_SHA": self.source,
            "TAG_OBJECT": self.tag_object,
            "RELEASE_CHANNEL": "rc",
            "GITHUB_OUTPUT": str(self.output),
            "GITHUB_REPOSITORY": "example/keycli",
            "GITHUB_RUN_ID": "42",
            "GITHUB_RUN_ATTEMPT": "1",
            "WORKFLOW_SHA": self.workflow_sha,
            "REPOSITORY_URL": str(self.repo),
            "EVENT": "workflow_dispatch",
            "EVENT_SHA": self.workflow_sha,
            "WORKFLOW_REF": "refs/heads/main",
            "DEFAULT_BRANCH": "main",
            "REPOSITORY_VISIBILITY": "public",
            "APPROVAL_REF": "",
            "BUILD_RUN_ATTEMPT": "",
            "CANDIDATE_CHANNEL": "",
        }

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, text=True).strip()

    def contract(self, action, *arguments, fails=False, **overrides):
        result = subprocess.run(
            ["bash", str(CONTRACT), action, *arguments], cwd=self.repo,
            env={**os.environ, **self.env, **overrides}, capture_output=True, text=True,
        )
        if fails:
            self.assertNotEqual(result.returncode, 0, result.stdout)
        else:
            self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout

    def test_policy_scan_fails_closed_on_scan_error(self):
        fake_bin = self.repo / "bin"
        fake_bin.mkdir()
        fake_rg = fake_bin / "rg"
        fake_rg.write_text("#!/bin/sh\nexit \"$FAKE_RG_STATUS\"\n")
        fake_rg.chmod(0o755)
        for status, expected_success in (("1", True), ("0", False), ("2", False), ("127", False)):
            with self.subTest(status=status):
                result = subprocess.run(
                    ["bash", str(ROOT / "verify.sh"), "--suite", "policy"],
                    cwd=ROOT,
                    env={**os.environ, "PATH": f"{fake_bin}:{os.environ['PATH']}", "FAKE_RG_STATUS": status},
                    capture_output=True, text=True,
                )
                self.assertEqual(result.returncode == 0, expected_success, result.stderr)

    def test_workflow_uses_one_source_and_uploaded_bundle(self):
        jobs = dict(re.findall(r"^  ([a-z-]+):\n(.*?)(?=^  [a-z-]+:\n|\Z)",
                               WORKFLOW.read_text().split("\njobs:\n", 1)[1], re.M | re.S))
        for job in ("verify", "build", "gate"):
            with self.subTest(job=job):
                self.assertRegex(jobs[job], r"needs: \[classify, ")
                self.assertIn("ref: ${{ needs.classify.outputs.source_sha }}", jobs[job])
                self.assertIn("ref: ${{ github.workflow_sha }}", jobs[job])
                self.assertIn("../release-control/scripts/release-contract.sh bind", jobs[job])
        self.assertIn("artifact-id }}", jobs["build"])
        for job in ("gate", "publish"):
            self.assertIn("artifact-ids: ${{ needs.build.outputs.artifact_id }}", jobs[job])
            self.assertIn("needs.build.outputs.manifest_sha", jobs[job])
        self.assertIn("release-contract.sh verify-binary", jobs["gate"])
        self.assertNotIn("go build", jobs["gate"])
        self.assertIn("stable)", jobs["guard"])
        self.assertIn("channel == 'dev' || needs.classify.outputs.channel == 'rc'", jobs["publish"])
        self.assertNotIn("actions/checkout", jobs["publish"])

    def test_dispatch_ref_cannot_replace_annotated_tag_source(self):
        self.contract("classify", SOURCE_SHA="", CANDIDATE_CHANNEL="tagged")
        self.assertEqual(
            dict(line.split("=", 1) for line in self.output.read_text().splitlines()),
            {"version": VERSION, "source_sha": self.source, "channel": "rc",
             "tag_object": self.tag_object, "approval_ref": ""},
        )
        self.contract("bind", fails=True)
        self.git("checkout", "-q", "--detach", self.source)
        self.contract("bind")
        self.contract("classify", fails=True, SOURCE_SHA="", EVENT="push")
        self.contract("classify", SOURCE_SHA="", EVENT="push", EVENT_SHA=self.source)
        self.contract("classify", SOURCE_SHA="", EVENT="push", EVENT_SHA=self.tag_object)

    def test_lightweight_tag(self):
        tag = "v0.10.0-dev.1"
        self.git("tag", tag, self.source)
        self.contract("classify", SOURCE_SHA="", RELEASE_VERSION=tag)
        self.assertIn(f"tag_object={self.source}\n", self.output.read_text())
        self.git("checkout", "-q", "--detach", self.source)
        self.contract("bind", RELEASE_VERSION=tag, RELEASE_CHANNEL="dev", TAG_OBJECT=self.source)

    def test_empty_and_multiline_identity_is_rejected(self):
        for key, value in (("RELEASE_VERSION", ""), ("SOURCE_SHA", ""), ("TAG_OBJECT", ""),
                           ("RELEASE_CHANNEL", ""), ("RELEASE_VERSION", VERSION + "\nchannel=operational")):
            with self.subTest(key=key, value=value):
                self.git("checkout", "-q", "--detach", self.source)
                self.contract("bind", fails=True, **{key: value})
        self.contract("classify", fails=True, SOURCE_SHA="", RELEASE_VERSION=VERSION + "\nversion=v1.0.0")
        self.assertFalse(self.output.exists())

    def test_moved_replaced_and_deleted_tag_are_rejected(self):
        self.git("checkout", "-q", "--detach", self.source)
        self.git("tag", "-fa", VERSION, self.source, "-m", "replaced annotation")
        self.contract("bind", fails=True)
        self.git("tag", "-f", VERSION, self.workflow_sha)
        self.contract("bind", fails=True)
        self.git("tag", "-d", VERSION)
        self.contract("bind", fails=True)

    def test_candidate_needs_private_default_branch_and_approval_reference(self):
        candidate = {
            "CANDIDATE_CHANNEL": "operational",
            "RELEASE_VERSION": "v0.10.0",
            "APPROVAL_REF": "review/24",
            "REPOSITORY_VISIBILITY": "private",
            "RELEASE_CHANNEL": "operational",
            "TAG_OBJECT": "",
        }
        self.contract("classify", fails=True, **{**candidate, "REPOSITORY_VISIBILITY": "public"})
        self.contract("classify", fails=True, **{**candidate, "WORKFLOW_REF": "refs/heads/unreviewed"})
        self.contract("classify", fails=True, **{**candidate, "APPROVAL_REF": "review/24\nchannel=dev"})
        self.contract("classify", **candidate)
        self.assertIn("tag_object=\n", self.output.read_text())
        self.git("checkout", "-q", "--detach", self.source)
        self.contract("bind", **candidate)

    def test_bundle_rejects_tampering_even_with_recomputed_checksums(self):
        dist = self.repo / "dist"
        dist.mkdir()
        for name in ASSETS:
            (dist / name).write_text(name)
        provenance = self.contract("provenance")
        self.assertEqual(json.loads(provenance)["source_sha"], self.source)
        self.assertEqual(json.loads(provenance)["workflow_sha"], self.workflow_sha)
        (dist / "provenance.json").write_text(provenance)

        def manifest():
            data = "".join(f"{hashlib.sha256((dist / name).read_bytes()).hexdigest()}  {name}\n"
                           for name in ASSETS).encode()
            (dist / "SHA256SUMS").write_bytes(data)
            return hashlib.sha256(data).hexdigest()

        expected = manifest()
        self.contract("verify-bundle", MANIFEST_SHA=expected)
        (dist / "unexpected").touch()
        self.contract("verify-bundle", fails=True, MANIFEST_SHA=expected)
        (dist / "unexpected").unlink()
        (dist / "kc-gateway-linux-amd64").write_text("different artifact")
        self.contract("verify-bundle", fails=True, MANIFEST_SHA=expected)
        changed = manifest()
        self.assertNotEqual(expected, changed)
        self.contract("verify-bundle", fails=True, MANIFEST_SHA=expected)
        (dist / "provenance.json").write_text(provenance.replace(self.source, self.workflow_sha))
        self.contract("verify-bundle", fails=True, MANIFEST_SHA=manifest())

    def test_dev_release_checks_non_executable_gateway(self):
        import platform

        repo = self.repo / "acceptance"
        scripts = repo / "scripts"
        scripts.mkdir(parents=True)
        shutil.copy(ROOT / "scripts/verify-dev-release.sh", scripts)
        (scripts / "verify-mcp-binary.sh").write_text("#!/bin/sh\nexit 0\n")
        assets = repo / "assets"
        assets.mkdir()
        machine = platform.machine().lower()
        arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[machine]
        system = platform.system().lower()
        cli = assets / f"kc-{system}-{arch}"
        gateway = assets / f"kc-gateway-{system}-{arch}"
        cli.write_text(f"#!/bin/sh\necho 'kc version {VERSION}'\n")
        gateway.write_text("#!/bin/sh\necho '{\"provider_enabled\":false}'\n")
        cli.chmod(0o644)
        gateway.chmod(0o644)
        def write_manifest():
            (assets / "SHA256SUMS").write_text("".join(
                f"{hashlib.sha256(asset.read_bytes()).hexdigest()}  {asset.name}\n"
                for asset in (cli, gateway)
            ))
        write_manifest()
        env = {**os.environ, "HOME": str(self.repo / "empty-home")}
        def check_release():
            return subprocess.run(
                ["bash", str(scripts / "verify-dev-release.sh"), "--tag", VERSION,
                 "--assets-dir", str(assets)], env=env, capture_output=True, text=True,
            )
        result = check_release()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertRegex(result.stdout, r"provider_lock\s+PASS")
        self.assertEqual(gateway.stat().st_mode & 0o777, 0o644)
        gateway.write_text(
            "#!/bin/sh\n"
            "if [ \"${KC_PROVIDER_EFFECTS:-}\" = enabled ]; then \n"
            "  echo '{\"provider_enabled\":true}'; \n"
            "else echo '{\"provider_enabled\":false}'; fi\n"
        )
        write_manifest()
        result = check_release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertRegex(result.stdout, r"provider_lock\s+FAIL")

    def test_actual_gateway_build_and_runtime_lock(self):
        commit = self.source
        version_pkg = "github.com/camem2442/keycli/internal/version"
        gateway_pkg = "github.com/camem2442/keycli/internal/effectgateway"
        for channel in ("dev", "rc", "operational"):
            with self.subTest(channel=channel):
                binary = self.repo / f"gateway-{channel}"
                version = "v0.10.0" if channel == "operational" else f"v0.10.0-{channel}.1"
                ldflags = (f"-X {version_pkg}.Version={version} -X {version_pkg}.Commit={commit} "
                           f"-X {gateway_pkg}.channel={channel}")
                subprocess.run(["go", "build", "-ldflags", ldflags, "-o", str(binary),
                                "./cmd/kc-gateway"], cwd=ROOT, check=True)
                self.assertEqual(subprocess.check_output([binary, "--version"], text=True).strip(),
                                 f"{version} ({commit})")
                for runtime in ("", "enabled"):
                    env = {**os.environ, "KC_PROVIDER_EFFECTS": runtime}
                    report = json.loads(subprocess.check_output([binary, "-lock-report"], env=env))
                    unlocked = channel == "operational" and runtime == "enabled"
                    self.assertEqual(report["channel"], channel)
                    self.assertEqual(report["provider_enabled"], unlocked)
                    self.assertEqual(report["unlocked"], unlocked)
                    self.assertTrue(report["requires_env"])
                    journal = self.repo / "journal"
                    live = subprocess.run([binary, "-provider", "live", "-journal", journal],
                                          env=env, capture_output=True, text=True)
                    self.assertNotEqual(live.returncode, 0)
                    self.assertIn("live provider adapter is unavailable" if unlocked else
                                  "provider effects are locked", live.stderr)
                    self.assertFalse(journal.exists())
                if channel == "rc":
                    arch = subprocess.check_output(["go", "env", "GOARCH"], text=True).strip()
                    dist = self.repo / "dist"
                    dist.mkdir()
                    shutil.copyfile(binary, dist / f"kc-gateway-linux-{arch}")
                    for asset, package in ((f"kc-linux-{arch}", "./cmd/kc"),
                                           ("keycli-linux-amd64", "./cmd/keycli")):
                        subprocess.run(["go", "build", "-ldflags", ldflags, "-o", str(dist / asset),
                                        package], cwd=ROOT, check=True)
                    self.contract("verify-binary", arch, RELEASE_VERSION=version)
                    self.contract("verify-binary", arch, fails=True, RELEASE_VERSION=VERSION)


if __name__ == "__main__":
    unittest.main(verbosity=2)
