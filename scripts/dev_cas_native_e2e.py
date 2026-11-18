#!/usr/bin/env python3
"""Opt-in isolated native dev CAS smoke. Never installs to a user's PATH.

Host/producer signatures, Goal Grant, runner token isolation and guest teardown
remain owned by the original private host; this source cannot attest them.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import socket
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SHA40 = re.compile(r"[0-9a-f]{40}\Z")
SHA64 = re.compile(r"[0-9a-f]{64}\Z")
DEV = re.compile(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-(?:dev|rc)\.(0|[1-9]\d*)\Z")
REQUIRED = frozenset(("checksums", "host_asset", "installed_bytes",
                      "installed_version", "mcp_smoke", "provider_lock",
                      "existing_install_unchanged"))
CREDENTIALS = ("GITHUB_TOKEN", "GH_TOKEN", "KC_KEY", "KC_PROVIDER_EFFECTS",
               "CLOUDFLARE_API_TOKEN", "AWS_ACCESS_KEY_ID", "OCI_CLI_KEY_FILE")


class Hold(ValueError):
    pass


def require(ok, reason):
    if not ok:
        raise Hold(reason)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def owner_file(path, expected_sha, *, name):
    item = Path(path)
    require(item.is_absolute() and item.name == name, "UNEXPECTED_ASSET_PATH")
    require(item.resolve(strict=True) == item, "SYMLINK_OR_NONCANONICAL_ASSET")
    info = item.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
            and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077 and 0 < info.st_size <= 32 * 1024 * 1024,
            "NONPRIVATE_OR_UNSAFE_ASSET")
    data = item.read_bytes()
    require(info.st_size == len(data) and digest(data) == expected_sha,
            "ASSET_SHA_MISMATCH")
    require(item.lstat().st_mtime_ns == info.st_mtime_ns, "ASSET_DRIFT")
    return data


def check_original_checkout(observed_head, dirty, expected_head):
    """Never execute a dev binary from a different/mutated source tree."""
    require(type(observed_head) is str and observed_head == expected_head,
            "CHECKOUT_NEW_SOURCE_DRIFT")
    require(type(dirty) is str and dirty == "", "CHECKOUT_WORKTREE_DIRTY")


def observe_checkout():
    env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"),
           "HOME": tempfile.gettempdir(),
           "GIT_TERMINAL_PROMPT": "0", "GIT_CONFIG_NOSYSTEM": "1",
           "GIT_CONFIG_GLOBAL": "/dev/null",
           "GIT_LFS_SKIP_SMUDGE": "1", "GIT_OPTIONAL_LOCKS": "0"}
    def git(*cmd):
        done = subprocess.run(["git", "-C", str(ROOT), *cmd],
                              stdin=subprocess.DEVNULL, capture_output=True,
                              env=env, text=True, timeout=15, check=False)
        require(done.returncode == 0 and len(done.stdout) < 8192,
                "SOURCE_GIT_INSPECTION_UNAVAILABLE")
        return done.stdout.strip()
    return git("rev-parse", "HEAD"), git("status", "--porcelain=v1")


def check_sealed_runtime(probe):
    """Mandatory direct kernel proof before executing any untrusted kc bytes.

    This is deliberately stricter than an ephemeral runner name: the private
    G2/G4 host must launch this inside its pre-probed rootless Podman sandbox.
    A source-supplied or self-reported identity alone never suffices.
    """
    require(type(probe) is dict, "SEALED_RUNTIME_UNOBSERVED")
    require(probe.get("uid") == 1001 and probe.get("gid") == 1001
            and probe.get("root_uid") == 0
            and probe.get("no_new_privileges") is True,
            "SEALED_RUNTIME_IDENTITY_MISSING")
    require(probe.get("capabilities_zero") is True
            and probe.get("credential_paths_absent") is True
            and probe.get("interfaces") == ["lo"],
            "SEALED_RUNTIME_SECRET_OR_NETWORK_EXPOSURE")
    mounts = probe.get("mounts")
    require(type(mounts) is dict and "ro" in mounts.get("/", [])
            and "rw" in mounts.get("/work", [])
            and "ro" in mounts.get("/opt/keycli-go-modcache", []),
            "SEALED_RUNTIME_MOUNT_BOUNDARY_MISSING")
    mappings = probe.get("uid_maps")
    require(type(mappings) is list and len(mappings) >= 2
            and all(type(r) is list and len(r) == 3
                    and all(type(v) is int and v >= 0 for v in r)
                    for r in mappings),
            "SEALED_RUNTIME_USER_NAMESPACE_UNPROVED")


def observe_sealed_runtime():
    """Read current kernel facts, never trust caller-supplied JSON or env."""
    lines = Path("/proc/self/status").read_text(encoding="ascii").splitlines()
    fields = dict(line.split(":", 1) for line in lines if ":" in line)
    caps = ["CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"]
    require(all(k in fields for k in caps + ["NoNewPrivs"]),
            "SEALED_RUNTIME_PROC_STATUS_INCOMPLETE")
    mounts = {}
    mount_types = {}
    for line in Path("/proc/self/mountinfo").read_text(encoding="ascii").splitlines():
        info, filesystem = line.split(" - ", 1)
        columns = info.split()
        target = re.sub(r"\\([0-7]{3})", lambda match: chr(int(match[1], 8)), columns[4])
        require(target not in mounts, "AMBIGUOUS_RUNTIME_MOUNT")
        mounts[target] = columns[5].split(",")
        mount_types[target] = filesystem.split()[0]
    uid_map = []
    for line in Path("/proc/self/uid_map").read_text(encoding="ascii").splitlines():
        fields3 = [int(z) for z in line.split()]
        uid_map.append(fields3)
    absent = all(not os.path.lexists(p) for p in (
        "/home/runner", "/run/secrets", "/run/host",
        "/run/podman/podman.sock", "/var/run/docker.sock"))
    return {
        "uid": os.getuid(), "gid": os.getgid(),
        "root_uid": Path("/").stat().st_uid,
        "no_new_privileges": fields["NoNewPrivs"].strip() == "1",
        "capabilities_zero": all(int(fields[k].strip(), 16) == 0 for k in caps),
        "credential_paths_absent": absent,
        "interfaces": sorted(name for _, name in socket.if_nameindex()),
        "mounts": mounts, "mount_types": mount_types, "uid_maps": uid_map,
    }


def check_readonly_checkout(mounts, root):
    parents = (root, *root.parents)
    enclosing = next((mounts[str(path)] for path in parents if str(path) in mounts), ())
    require("ro" in enclosing and all(
        "ro" in options for path, options in mounts.items()
        if root in Path(path).parents
    ), "SOURCE_CHECKOUT_MUST_BE_READ_ONLY")


def check_mount_profile(mounts, types, root):
    require(types.get("/tmp") == "tmpfs" and "rw" in mounts.get("/tmp", ()),
            "DISPOSABLE_SCRATCH_MOUNT_REQUIRED")
    scratch = {"/": None, "/work": None, str(root): None,
               "/opt/keycli-go-modcache": None, "/tmp": {"tmpfs"}}
    virtual = {
        "/proc": {"proc", "tmpfs"},
        "/sys": {"sysfs", "cgroup", "cgroup2", "tmpfs"},
        "/dev": {"tmpfs", "devtmpfs", "devpts", "mqueue", "proc"},
        "/run": {"tmpfs"},
    }
    runtime_files = {"/etc/group", "/etc/hostname", "/etc/passwd",
                     "/etc/resolv.conf", "/run/.containerenv"}
    masked_proc_files = {"/proc/kcore", "/proc/keys", "/proc/latency_stats",
                         "/proc/timer_list"}
    for target, kind in types.items():
        if target in scratch:
            permitted = scratch[target]
            require(permitted is None or kind in permitted, "UNEXPECTED_RUNTIME_MOUNT")
        elif target in runtime_files or (target in masked_proc_files and kind == "devtmpfs"):
            require("ro" in mounts[target], "RUNTIME_FILE_MUST_BE_READ_ONLY")
        else:
            require(any((target == prefix or target.startswith(prefix + "/")) and kind in kinds
                        for prefix, kinds in virtual.items()), "UNEXPECTED_RUNTIME_MOUNT")


def preflight(args):
    require(args.allow_native_disposable_exec is True, "EXPLICIT_NATIVE_OPT_IN_REQUIRED")
    require(platform.system() == "Linux" and platform.machine() == "aarch64"
            and os.geteuid() != 0, "NATIVE_NONROOT_ARM64_REQUIRED")
    if args.personal_local:
        require(not any(os.environ.get(key) for key in (
            "RUNNER_NAME", "RUNNER_OS", "RUNNER_ARCH", "GITHUB_ACTIONS",
            "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT")),
            "PERSONAL_LOCAL_REQUIRES_NO_RUNNER_SESSION")
    else:
        require(os.environ.get("RUNNER_OS") == "Linux"
                and os.environ.get("RUNNER_ARCH") == "ARM64"
                and re.fullmatch(r"keycli-ephemeral-vm-arm64-[A-Za-z0-9-]{1,37}",
                                 os.environ.get("RUNNER_NAME", ""))
                and os.environ.get("GITHUB_RUN_ATTEMPT") == "1",
                "ONE_SHOT_PRIVATE_RUNNER_REQUIRED")
    require(not any(os.environ.get(key) for key in CREDENTIALS),
            "CREDENTIAL_ENVIRONMENT_PRESENT")
    for value in (args.old_head, args.new_head):
        require(SHA40.fullmatch(value) is not None, "SOURCE_HEAD_INVALID")
    for value in (args.old_sha, args.new_sha, args.old_sums_sha, args.new_sums_sha,
                  args.new_gateway_sha):
        require(SHA64.fullmatch(value) is not None, "PINNED_SHA_INVALID")
    for value in (args.old_tag, args.new_tag):
        require(DEV.fullmatch(value) is not None, "NOT_DEV_OR_RC_VERSION")
    require(args.old_head != args.new_head and args.old_sha != args.new_sha
            and args.old_tag != args.new_tag, "OLD_AND_NEW_MUST_BE_DISTINCT")
    for path in (args.old_dir, args.new_dir):
        directory = Path(path)
        require(directory.is_absolute()
                and directory.resolve(strict=True) == directory
                and stat.S_ISDIR(directory.lstat().st_mode)
                and directory.lstat().st_uid == os.geteuid()
                and not directory.lstat().st_mode & 0o077,
                "ASSET_DIRECTORY_NOT_PRIVATE")

    probe = observe_sealed_runtime()
    check_sealed_runtime(probe)
    check_readonly_checkout(probe["mounts"], ROOT)
    check_mount_profile(probe["mounts"], probe["mount_types"], ROOT)
    current_head, dirty = observe_checkout()
    check_original_checkout(current_head, dirty, args.new_head)


def load_cas():
    spec = importlib.util.spec_from_file_location(
        "keycli_private_dev_cas", ROOT / "scripts/dev_cas_pilot.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def native_run(cmd, *, phase, home, timeout):
    env = {"PATH": "/usr/bin:/bin", "HOME": str(home),
           "XDG_CONFIG_HOME": str(home / ".config"),
           "LANG": "C", "LC_ALL": "C", "TMPDIR": "/tmp"}
    result = subprocess.run(cmd, cwd=ROOT, env=env, capture_output=True,
                            stdin=subprocess.DEVNULL, text=True,
                            timeout=timeout, check=False)
    require(len(result.stdout) + len(result.stderr) <= 32768,
            "NATIVE_OUTPUT_TOO_LARGE")
    print(json.dumps({"phase": phase, "exit_code": result.returncode,
                      "stdout": result.stdout, "stderr": result.stderr}),
          file=sys.stderr, flush=True)
    require(result.returncode == 0, "NATIVE_COMMAND_FAILED")
    return result.stdout


def assert_version(output, expected):
    match = re.fullmatch(r"kc version (\S+) \([^\s()]+\)", output.rstrip("\n"))
    require(match is not None and match[1] == expected, "WRONG_NATIVE_VERSION")


def strict_receipt(output, tag):
    documents = []
    for line in output.splitlines():
        if line.startswith("{"):
            try:
                documents.append(json.loads(line))
            except (ValueError, TypeError) as exc:
                raise Hold("NATIVE_REPORT_MALFORMED") from exc
    strict = [x for x in documents if type(x) is dict
              and x.get("schema") == "keycli.dev-release-strict.v1"]
    product = [x for x in documents if type(x) is dict
               and x.get("status") == "PASS" and x.get("tag") == tag]
    require(len(strict) == 1 and len(product) == 1, "ORIGINAL_PRODUCT_REPORT_MISSING")
    r = strict[0]
    require(r.get("status") == "PASS_ALL_REQUIRED_CHECKS_NOT_ATTESTED"
            and set(r.get("checks", {})) == REQUIRED
            and all(isinstance(v, str) and
                    (v == "PASS" or (v.startswith("PASS (") and v.endswith(")")))
                    for v in r["checks"].values())
            and r.get("problems") == [], "MANDATORY_NATIVE_CHECK_NOT_PASS")
    require(r.get("authorizes_operating") is False
            and r.get("original_host_verified") is False,
            "SOURCE_RECEIPT_OVERCLAIMS_AUTHORITY")


def stage_and_select(cas, root, directory, *, tag, head, sha, sums_sha, previous):
    asset = Path(directory) / "kc-linux-arm64"
    sums = Path(directory) / "SHA256SUMS"
    cas.stage(root, asset, expected_sha256=sha, tag=tag, source_head=head,
              sums_path=sums, asset_name="kc-linux-arm64",
              expected_sums_sha256=sums_sha, allow_disposable=True)
    return cas.select(root, artifact_sha256=sha, tag=tag, source_head=head,
                      expected_previous_pin_sha256=previous, allow_disposable=True)


def execute(args):
    preflight(args)
    old_data = owner_file(Path(args.old_dir) / "kc-linux-arm64",
                          args.old_sha, name="kc-linux-arm64")
    old_sums = owner_file(Path(args.old_dir) / "SHA256SUMS",
                          args.old_sums_sha, name="SHA256SUMS")
    new_pins = {"kc-linux-arm64": args.new_sha,
                "kc-gateway-linux-arm64": args.new_gateway_sha,
                "SHA256SUMS": args.new_sums_sha}
    new_assets = {
        name: owner_file(Path(args.new_dir) / name, sha, name=name)
        for name, sha in new_pins.items()
    }
    cas = load_cas()
    temp = Path(tempfile.mkdtemp(prefix="kc-native-e2e-", dir="/tmp"))
    temp.chmod(0o700)
    old_snapshot = temp / "old"
    new_snapshot = temp / "new"
    for directory, files in (
        (old_snapshot, {"kc-linux-arm64": old_data, "SHA256SUMS": old_sums}),
        (new_snapshot, new_assets),
    ):
        directory.mkdir(mode=0o700)
        for name, data in files.items():
            fd = os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
            with os.fdopen(fd, "wb") as stream:
                stream.write(data)
    home = temp / "home"
    (home / ".local/bin").mkdir(parents=True, mode=0o700)
    root = cas.init(base=temp)["root"]
    previous = stage_and_select(
        cas, root, old_snapshot, tag=args.old_tag,
        head=args.old_head, sha=args.old_sha,
        sums_sha=args.old_sums_sha, previous=None)
    first_pin = previous["pin_sha256"]
    installed = home / ".local/bin/kc"
    installed.write_bytes(old_data)
    installed.chmod(0o500)
    assert_version(native_run([str(installed), "--version"],
                              phase="old-version-before", home=home, timeout=10), args.old_tag)
    native_run(["bash", str(ROOT / "scripts/verify-mcp-binary.sh"),
                str(installed), args.old_tag], phase="old-mcp-before", home=home, timeout=30)

    print(json.dumps({"phase": "old-selection", "receipt": previous,
                      "status": cas.status(root)}), file=sys.stderr, flush=True)
    changed = None
    restored = False
    try:
        changed = stage_and_select(
            cas, root, new_snapshot, tag=args.new_tag,
            head=args.new_head, sha=args.new_sha,
            sums_sha=args.new_sums_sha, previous=first_pin)
        selected = cas.status(root)
        print(json.dumps({"phase": "new-selection", "receipt": changed,
                          "status": selected}), file=sys.stderr, flush=True)
        require(selected["selected"]["artifact_sha256"] == args.new_sha,
                "NEW_CAS_SELECTION_FAILED")
        report = native_run(
            ["bash", str(ROOT / "scripts/verify-dev-release.sh"),
             "--tag", args.new_tag, "--assets-dir", str(new_snapshot),
             "--expected-host-sha256", args.new_sha,
             "--expected-gateway-sha256", args.new_gateway_sha,
             "--expected-manifest-sha256", args.new_sums_sha,
             "--require-all-checks"], phase="new-strict-smoke", home=home, timeout=120)
        strict_receipt(report, args.new_tag)
        require(digest(installed.read_bytes()) == args.old_sha,
                "PREVIOUS_INSTALLATION_BYTES_CHANGED")
    finally:
        if changed is not None:
            back = cas.rollback(root, receipt_id=changed["receipt_id"],
                                receipt_sha256=changed["receipt_sha256"],
                                allow_disposable=True)
            print(json.dumps({"phase": "rollback", "receipt": back,
                              "status": cas.status(root)}), file=sys.stderr, flush=True)
            restored = (back["pin_sha256"] == first_pin
                        and cas.status(root)["selected"]["artifact_sha256"] == args.old_sha)
    require(restored, "PREVIOUS_CAS_RESTORE_FAILED")
    try:
        cas.rollback(root, receipt_id=changed["receipt_id"],
                     receipt_sha256=changed["receipt_sha256"], allow_disposable=True)
    except (cas.Hold, FileNotFoundError) as exc:
        print(json.dumps({"phase": "rollback-replay", "refusal": str(exc)}),
              file=sys.stderr, flush=True)
    else:
        raise Hold("ROLLBACK_REPLAY_ACCEPTED")

    assert_version(native_run([str(installed), "--version"],
                              phase="old-version-after", home=home, timeout=10), args.old_tag)
    native_run(["bash", str(ROOT / "scripts/verify-mcp-binary.sh"),
                str(installed), args.old_tag], phase="old-mcp-after", home=home, timeout=30)
    require(digest(installed.read_bytes()) == args.old_sha,
            "PREVIOUS_VERSION_DRIFT_AFTER_ROLLBACK")
    return {"schema": "keycli.native-dev-cas-local.v1",
            "status": "LOCAL_DEV_NATIVE_CAS_RESTORED_NOT_ATTESTED",
            "execution_mode": "personal-local" if args.personal_local else "private-runner",
            "old_source": args.old_head, "new_source": args.new_head,
            "old_sha256": args.old_sha, "new_sha256": args.new_sha,
            "old_pin_sha256": first_pin, "restored": restored,
            "cas_root": root, "original_host_verified": False,
            "producer_verified": False, "authorizes_merge": False,
            "authorizes_operating": False, "authorizes_install": False}


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ("old-dir", "new-dir", "old-head", "new-head",
                 "old-tag", "new-tag", "old-sha", "new-sha",
                 "old-sums-sha", "new-sums-sha", "new-gateway-sha"):
        p.add_argument("--" + name, required=True)
    p.add_argument("--allow-native-disposable-exec", action="store_true")
    p.add_argument("--personal-local", action="store_true",
                   help="Use a sealed local disposable guest without GitHub Runner metadata")
    return p


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        result = execute(args)
    except Exception as e:
        # This endpoint is a local host tool; failure never warrants an
        # automatic execution retry. Do not leak private file paths.
        result = {"schema": "keycli.native-dev-cas-local.v1",
                  "status": "HOLD_NO_AUTOMATIC_RETRY",
                  "error_type": type(e).__name__,
                  "reason": str(e) if isinstance(e, Hold) else None,
                  "original_host_verified": False,
                  "producer_verified": False,
                  "authorizes_operating": False, "authorizes_merge": False}
    print(json.dumps(result, sort_keys=True))
    return 0 if result["status"] == "LOCAL_DEV_NATIVE_CAS_RESTORED_NOT_ATTESTED" else 3


if __name__ == "__main__":
    raise SystemExit(main())
