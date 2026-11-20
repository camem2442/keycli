"""Owner-only Chrome browser bridge for bounded agent login actions.

No IPC, HTTP listener, CDP endpoint, MCP server, cookie export or secret
lookup is provided. Its caller must be an independently authenticated owner
process that verifies original Goal/Grant/Claim and reserves request IDs.

Google Chrome is launched with a dedicated persistent profile. iCloud Passwords
may be installed interactively by the owner; credentials and passkey ceremonies
are NEVER read via the bridge. Owner-only MFA filler is an optional injected
interface (KeyCLI owner2fa source PR #83), not a way to return codes to agents.
"""
from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Protocol
from urllib.parse import urlsplit
import os
import re
import stat
import time
from chrome_profile_guard import ProfileRejected, verify_profile_root
from chrome_owner_gui_gate import require_local_gui_attempt

_ACTIONS = frozenset({"session_status", "reauthenticate"})
_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
_ID = re.compile(r"[A-Za-z0-9_-]{8,128}\Z")
_PATH = re.compile(r"/(?:[A-Za-z0-9/_-]|\.(?!\.))*\Z")


class Denied(ValueError):
    """Opaque denial. Never echo untrusted site text or secret data."""


@dataclass(frozen=True)
class Intent:
    v: int
    goal: str
    claim: str
    request_id: str
    site: str
    action: str
    path: str
    expires_unix: int


@dataclass(frozen=True)
class SitePolicy:
    # A trusted owner file / compiled fixture; never supplied by an agent.
    site: str
    account_marker: str
    login_path: str
    allowed_paths: frozenset[str]
    otp_selector: str | None = None
    otp_submit_selector: str | None = None
    mfa_mode: str = "apple_extension"  # owner_totp requires explicit per-site enrolment


class OwnerAuthority(Protocol):
    def verify_and_reserve(self, request: Intent) -> None:
        """Original signed authority + writer claim + atomic replay reservation."""


class OwnerOTP(Protocol):
    def fill_after_owner_check(self, page: object, policy: SitePolicy, request: Intent) -> None:
        """Owner-host only. Must recheck origin; never return seed or code."""


class ChromeContext(Protocol):
    pages: list
    def new_page(self): ...
    def route(self, pattern: str, handler: Callable): ...
    def close(self) -> None: ...


class BrowserLauncher(Protocol):
    def launch(self, profile: Path) -> ChromeContext: ...


def _sync_playwright():
    """Indirection so stdlib-only tests can substitute a fake launcher."""
    from playwright.sync_api import sync_playwright
    return sync_playwright()


class PlaywrightChromeLauncher:
    """Branded Chrome, no raw CDP port; Playwright retained inside owner host.

    Import is lazy: portable policy tests require only the Python stdlib.
    Runs a headed user-interactive Chrome (never headless automated Touch ID).
    """
    def __init__(self):
        self._playwright_cm = None

    def launch(self, profile: Path) -> ChromeContext:
        # The registered headless mac-ci runner is not an Apple/Chrome GUI owner.
        require_local_gui_attempt()
        p = _sync_playwright().start()
        self._playwright_cm = p
        try:
            return p.chromium.launch_persistent_context(
                str(profile), channel="chrome", headless=False,
                accept_downloads=False, ignore_https_errors=False,
                # Playwright's route() cannot intercept fetches owned by page
                # service workers. Deny this unobservable network path.
                # Chrome extension compatibility needs real Mac acceptance.
                service_workers="block",
                timeout=15000,
            )
        except Exception:
            p.stop()
            self._playwright_cm = None
            raise

    def close(self):
        if self._playwright_cm is not None:
            self._playwright_cm.stop()
            self._playwright_cm = None


def _valid_site(site: str) -> bool:
    return (isinstance(site, str) and 3 <= len(site) <= 253 and "." in site
            and all(_LABEL.fullmatch(label) for label in site.split(".")))


def _valid_path(path: str) -> bool:
    return (isinstance(path, str) and 1 <= len(path) <= 512
            and bool(_PATH.fullmatch(path)) and not path.startswith("//")
            and ".." not in path and "\\" not in path and "%" not in path)


def validate(request: Intent, policy: SitePolicy, now: int) -> None:
    if (type(request.v) is not int or request.v != 1
            or type(request.expires_unix) is not int
            or not now < request.expires_unix <= now + 300
            or any(not isinstance(v, str) or not _ID.fullmatch(v)
                   for v in (request.goal, request.claim, request.request_id))
            or not _valid_site(request.site) or request.site != policy.site
            or request.action not in _ACTIONS or not _valid_path(request.path)
            or request.path not in policy.allowed_paths
            or not _valid_path(policy.login_path)
            or not policy.account_marker
            or policy.mfa_mode not in {"apple_extension", "owner_totp", "passkey", "manual"}
            or request.action == "reauthenticate" and policy.login_path not in policy.allowed_paths):
        raise Denied("REQUEST_REJECTED")


def is_same_origin(url: str, site: str) -> bool:
    try:
        u = urlsplit(url)
        return (u.scheme == "https" and u.hostname == site and u.username is None
                and u.password is None and u.port is None and u.netloc == site)
    except ValueError:
        return False


def secure_profile(root: Path, site: str) -> Path:
    """Only owner-controlled profile roots, no symlinks or shared directories.

    This protects accidental profile choice, not same-UID adversarial access.
    It does not modify the user's normal Chrome profile.
    """
    if not _valid_site(site) or not root.is_absolute():
        raise Denied("PROFILE_REJECTED")
    try:
        verify_profile_root(root)
    except ProfileRejected:
        raise Denied("PROFILE_REJECTED") from None
    current = root
    # Caller must create the root as a protected directory first. We never
    # create arbitrary user-supplied parents or silently chmod existing paths.
    if not current.exists() or current.is_symlink():
        raise Denied("PROFILE_REJECTED")
    st = current.stat(follow_symlinks=False)
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != os.geteuid() or stat.S_IMODE(st.st_mode) & 0o077:
        raise Denied("PROFILE_REJECTED")
    profile = current / site
    if profile.is_symlink():
        raise Denied("PROFILE_REJECTED")
    if profile.exists():
        st = profile.stat(follow_symlinks=False)
        if not stat.S_ISDIR(st.st_mode) or st.st_uid != os.geteuid() or stat.S_IMODE(st.st_mode) & 0o077:
            raise Denied("PROFILE_REJECTED")
    else:
        try:
            profile.mkdir(mode=0o700)
        except OSError:
            raise Denied("PROFILE_REJECTED") from None
    return profile


def _has_marker(page, selector: str) -> bool:
    # Status only. Never return page text or cookies to an agent.
    return page.locator(selector).count() > 0


def _limit_requests(context: ChromeContext, site: str) -> None:
    def deny_cross_site(route):
        if is_same_origin(route.request.url, site):
            route.continue_()
        else:
            route.abort()
    context.route("**/*", deny_cross_site)


def run_owner_flow(
    request: Intent, policy: SitePolicy, authority: OwnerAuthority,
    launcher: BrowserLauncher, profile_root: Path, now: int | None = None,
    owner_otp: OwnerOTP | None = None,
    human_login_timeout_seconds: int = 0,
) -> str:
    """Execute one bounded action and return fixed status, never page data.

    The complete browser session is opened *after* independent owner authority
    verification/reservation. No callable default Authority is supplied.
    """
    stamp = int(time.time()) if now is None else now
    validate(request, policy, stamp)
    if (type(human_login_timeout_seconds) is not int or not 0 <= human_login_timeout_seconds <= 120
            or stamp + human_login_timeout_seconds > request.expires_unix):
        raise Denied("HUMAN_WAIT_REJECTED")
    if authority is None or launcher is None:
        raise Denied("AUTHORITY_MISSING")
    try:
        authority.verify_and_reserve(request)
    except Exception:
        raise Denied("AUTHORITY_DENIED") from None
    profile = secure_profile(profile_root, request.site)
    context = None
    try:
        context = launcher.launch(profile)
        _limit_requests(context, request.site)
        # A persistent profile may reopen tabs from an earlier crashed run.
        # Do not treat or navigate these tabs as the requested session.
        if any(getattr(existing, "url", None) != "about:blank"
               for existing in context.pages):
            raise Denied("RESTORED_PAGE_REJECTED")
        page = context.new_page()  # Never act on an existing personal browser tab.
        target_path = request.path if request.action == "session_status" else policy.login_path
        page.goto("https://" + policy.site + target_path, wait_until="domcontentloaded", timeout=10000)
        if not is_same_origin(page.url, request.site):
            raise Denied("CROSS_ORIGIN")
        if _has_marker(page, policy.account_marker):
            return "SESSION_PRESENT"
        if request.action == "session_status":
            return "NO_SESSION"
        if (policy.mfa_mode == "owner_totp" and policy.otp_selector
                and _has_marker(page, policy.otp_selector) and owner_otp is not None):
            if not is_same_origin(page.url, request.site):
                raise Denied("CROSS_ORIGIN")
            try:
                owner_otp.fill_after_owner_check(page, policy, request)
            except Exception:
                # An uncertain submit must not be replayed automatically.
                raise Denied("MFA_OWNER_FAILED") from None
            if not is_same_origin(page.url, request.site):
                raise Denied("CROSS_ORIGIN")
            if _has_marker(page, policy.account_marker):
                return "SESSION_PRESENT"
            # A failed or pending MFA submit must not retry automatically.
            return "HUMAN_ACTION_REQUIRED"
        # A human can operate iCloud Passwords, extension UI, Touch ID and OTP
        # only in an explicitly requested bounded interactive Mac session.
        if human_login_timeout_seconds > 0:
            try:
                page.wait_for_selector(policy.account_marker, timeout=human_login_timeout_seconds * 1000)
            except Exception:
                return "HUMAN_ACTION_REQUIRED"
            return "SESSION_PRESENT" if is_same_origin(page.url, request.site) and _has_marker(page, policy.account_marker) else "HUMAN_ACTION_REQUIRED"
        return "HUMAN_ACTION_REQUIRED"
    except Denied:
        raise
    except Exception:
        # Do not bubble Playwright exception text (URLs, DOM, auth details).
        raise Denied("CHROME_OWNER_FAILED") from None
    finally:
        if context is not None:
            try:
                context.close()
            except Exception:
                pass
        if hasattr(launcher, "close"):
            try:
                launcher.close()
            except Exception:
                pass
