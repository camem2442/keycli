"""Read-only owner-console preflight for interactive Chrome/Apple login.

A positive console probe permits an *interactive local attempt* only; it is not
proof of Aqua/TCC, browser credentials, native acceptance, or Goal authority.
"""
from __future__ import annotations

from dataclasses import dataclass
import os
import sys


@dataclass(frozen=True)
class ConsoleFacts:
    darwin: bool
    euid: int
    console_uid: int | None
    ssh_session: bool
    ci_runner: bool


def may_attempt_local_gui(facts: ConsoleFacts) -> bool:
    """Refuse remote/headless/privileged attempts even with a valid account."""
    return (facts.darwin and facts.euid > 0 and
            facts.console_uid == facts.euid and
            not facts.ssh_session and not facts.ci_runner)


def observe_local_console() -> ConsoleFacts:
    """Observe the current process and device; no agent-supplied facts."""
    console_uid = None
    if sys.platform == "darwin":
        try:
            console_uid = os.stat("/dev/console").st_uid
        except OSError:
            pass
    return ConsoleFacts(
        darwin=sys.platform == "darwin", euid=os.geteuid(),
        console_uid=console_uid,
        ssh_session=bool(os.environ.get("SSH_CONNECTION") or os.environ.get("SSH_TTY")),
        ci_runner=os.environ.get("GITHUB_ACTIONS") == "true" or bool(os.environ.get("RUNNER_NAME")),
    )


def local_gui_preflight() -> dict[str, object]:
    """Do not upgrade a console observation into a native success receipt."""
    if not may_attempt_local_gui(observe_local_console()):
        return {"status": "HOLD", "reason": "OWNER_LOCAL_CONSOLE_REQUIRED",
                "native_acceptance": False, "authorizes_execution": False}
    return {"status": "HOLD", "reason": "GUI_OPERATOR_AND_TCC_UNVERIFIED",
            "native_acceptance": False, "authorizes_execution": False}


class GuiNotAdmitted(RuntimeError):
    pass


def require_local_gui_attempt() -> None:
    """One cheap prerequisite for the real Playwright launcher, not a Grant."""
    if not may_attempt_local_gui(observe_local_console()):
        raise GuiNotAdmitted("OWNER_LOCAL_CONSOLE_REQUIRED")
