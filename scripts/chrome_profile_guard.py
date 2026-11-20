"""Shared pure-stdlib profile-root guard for owner Chrome and native preflight.

This protects against accidentally picking the personal/default Chrome data
folder; it is NOT a same-UID sandbox, credential isolation, or an OS grant.
"""
from __future__ import annotations

import os
from pathlib import Path
import stat
import sys


class ProfileRejected(ValueError):
    pass


_UNSAFE_SEQUENCES = (
    ("library", "application support", "google", "chrome"),
    (".config", "google-chrome"),
    (".config", "chromium"),
    ("appdata", "local", "google", "chrome", "user data"),
)


def _contains_component_sequence(parts: tuple[str, ...], candidate: tuple[str, ...]) -> bool:
    return any(parts[i:i + len(candidate)] == candidate
               for i in range(len(parts) - len(candidate) + 1))


def verify_profile_root(root: Path) -> None:
    """Refuse symlink ancestors, default Chrome roots and shared directories.

    The root must be created ahead of time by the trusted owner process. Never
    chmod an existing directory or infer an agent-supplied profile path.
    """
    if not root.is_absolute():
        raise ProfileRejected("DEDICATED_PROFILE_REQUIRED")
    try:
        canonical = root.resolve(strict=True)
        if canonical != root:
            # macOS has a root-owned /var → /private/var alias. A canonical
            # resolution changing anything else (including nested aliases)
            # is still denied. Owner code can also pass canonical /private/var.
            allowed_var_alias = (
                sys.platform == "darwin" and str(root).startswith("/var/") and
                str(canonical) == "/private" + str(root)
            )
            if not allowed_var_alias:
                raise ProfileRejected("DEDICATED_PROFILE_REQUIRED")
        st = root.stat(follow_symlinks=False)
    except (OSError, RuntimeError):
        raise ProfileRejected("DEDICATED_PROFILE_REQUIRED") from None
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != os.geteuid() or stat.S_IMODE(st.st_mode) & 0o077:
        raise ProfileRejected("DEDICATED_PROFILE_REQUIRED")
    parts = tuple(part.casefold() for part in root.parts)
    if any(_contains_component_sequence(parts, sequence) for sequence in _UNSAFE_SEQUENCES):
        raise ProfileRejected("PERSONAL_CHROME_PROFILE_FORBIDDEN")
