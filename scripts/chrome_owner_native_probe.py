"""Read-only Chrome owner-Mac metadata probe; never opens Chrome or passwords.

This is NOT native GUI acceptance and never reports PASS. It only helps the
existing native-acceptance skill select the minimal Mac GUI/2FA smoke.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import plistlib
import stat
import sys
from chrome_profile_guard import ProfileRejected, verify_profile_root
from chrome_owner_gui_gate import local_gui_preflight


def probe(app: Path, profile_root: Path, *, platform: str | None = None) -> dict:
    def reply(reason: str, metadata_ok: bool = False) -> dict:
        return {"v": 1, "status": "HOLD", "reason": reason,
                "metadata_ok": metadata_ok, "native_acceptance": False,
                "extension_verified": False, "passkey_verified": False}
    if (platform if platform is not None else sys.platform) != "darwin":
        return reply("OWNER_MAC_REQUIRED")
    if not app.is_absolute() or app.is_symlink():
        return reply("CHROME_APP_NOT_VERIFIED")
    plist = app / "Contents" / "Info.plist"
    binary = app / "Contents" / "MacOS" / "Google Chrome"
    try:
        if plist.is_symlink() or binary.is_symlink():
            return reply("CHROME_APP_NOT_VERIFIED")
        if plist.stat().st_size > 1024 * 1024:
            return reply("CHROME_APP_NOT_VERIFIED")
        with plist.open("rb") as handle:
            info = plistlib.load(handle)
        if info.get("CFBundleIdentifier") != "com.google.Chrome" or not isinstance(info.get("CFBundleShortVersionString"), str):
            return reply("CHROME_APP_NOT_VERIFIED")
        if not binary.is_file() or not os.access(binary, os.X_OK):
            return reply("CHROME_APP_NOT_VERIFIED")
    except (OSError, ValueError, TypeError, plistlib.InvalidFileException):
        return reply("CHROME_APP_NOT_VERIFIED")
    try:
        verify_profile_root(profile_root)
    except ProfileRejected:
        return reply("CHROME_PROFILE_NOT_VERIFIED")
    if local_gui_preflight()["reason"] != "GUI_OPERATOR_AND_TCC_UNVERIFIED":
        return reply("OWNER_LOCAL_CONSOLE_REQUIRED")
    return reply("GUI_EXTENSION_AND_2FA_NOT_VERIFIED", metadata_ok=True)


if __name__ == "__main__":
    # This CLI never prints app or profile paths and cannot launch Chrome.
    if len(sys.argv) != 3:
        raise SystemExit("usage: chrome_owner_native_probe.py CHROME_APP PRIVATE_PROFILE_ROOT")
    print(json.dumps(probe(Path(sys.argv[1]), Path(sys.argv[2])), sort_keys=True))
