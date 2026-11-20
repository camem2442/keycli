"""Portable owner-browser tests with synthetic site and no real account."""
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from chrome_owner_bridge import (
    Denied, Intent, SitePolicy, is_same_origin, secure_profile, validate, run_owner_flow,
    PlaywrightChromeLauncher,
)

NOW = 1795172400
POLICY = SitePolicy(
    "example.com", "[data-testid='signed-in']", "/login",
    frozenset({"/account", "/login"}), otp_selector="input[name='otp']",
)


def intent(**kw):
    d = dict(v=1, goal="goal-0001", claim="claim-0001", request_id="request-0001",
             site="example.com", action="session_status", path="/account", expires_unix=NOW+60)
    d.update(kw)
    return Intent(**d)


class Authority:
    def __init__(self, allow=True):
        self.allow = allow
        self.calls = 0
    def verify_and_reserve(self, request):
        self.calls += 1
        if not self.allow:
            raise ValueError("a secret that must not leak")


class Locator:
    def __init__(self, count):
        self._count = count
    def count(self):
        return self._count


class Page:
    def __init__(self, markers):
        self.markers = markers
        self.url = "about:blank"
        self.visits = []
        self.waits = []
    def goto(self, target, **kw):
        self.url = target
        self.visits.append(target)
    def locator(self, selector):
        return Locator(self.markers.get(selector, 0))
    def wait_for_selector(self, selector, *, timeout):
        self.waits.append((selector, timeout))
        if self.markers.get(selector, 0) < 1:
            raise TimeoutError("fake timeout")


class Context:
    def __init__(self, markers):
        self.markers = markers
        self.routes = []
        self.pages = [Page({"[data-testid='signed-in']": 1})]
        self.new_calls = 0
        self.closed = False
    def route(self, rule, handler):
        self.routes.append(rule)
    def new_page(self):
        self.new_calls += 1
        self.page = Page(self.markers)
        return self.page
    def close(self):
        self.closed = True


class Launcher:
    def __init__(self, markers):
        self.markers = markers
        self.called = 0
        self.context = None
    def launch(self, profile):
        self.called += 1
        self.context = Context(self.markers)
        return self.context


class OTP:
    def __init__(self, markers):
        self.markers = markers
        self.calls = 0
    def fill_after_owner_check(self, page, policy, request):
        self.calls += 1
        self.markers[policy.account_marker] = 1


class ChromeOwnerTests(unittest.TestCase):
    def test_playwright_lifecycle_closes_correct_object(self):
        class FakePlaywright:
            def __init__(self, fails=False):
                self.stopped = False
                self.fails = fails
                self.chromium = self
            def launch_persistent_context(self, *args, **kwargs):
                if self.fails:
                    raise RuntimeError("synthetic launch fail")
                return object()
            def stop(self):
                self.stopped = True
        class FakeContextManager:
            def __init__(self, playwright):
                self.playwright = playwright
            def start(self):
                return self.playwright
        for fail in (False, True):
            p = FakePlaywright(fail)
            launcher = PlaywrightChromeLauncher()
            with patch("playwright.sync_api.sync_playwright", return_value=FakeContextManager(p)):
                if fail:
                    with self.assertRaises(RuntimeError):
                        launcher.launch(Path("/tmp/synthetic"))
                else:
                    launcher.launch(Path("/tmp/synthetic"))
                    launcher.close()
            self.assertTrue(p.stopped)

    def test_origin_host_confusion(self):
        for bad in ("http://example.com", "https://example.com.evil.test",
                    "https://example.com:8443", "https://a@example.com",
                    "https://example.com.", "file:///tmp", "https://EXAMPLE.com"):
            self.assertFalse(is_same_origin(bad, "example.com"), bad)
        self.assertTrue(is_same_origin("https://example.com/login", "example.com"))

    def test_invalid_request_never_allocates_browser(self):
        with tempfile.TemporaryDirectory() as d:
            os.chmod(d, 0o700)
            for kw in (dict(action="read_page"), dict(path="//evil.test"),
                       dict(path="/%2fadmin"), dict(site="evil.test"),
                       dict(expires_unix=NOW-1), dict(expires_unix=NOW+301),
                       dict(v=True), dict(request_id="x")):
                a, l = Authority(), Launcher({})
                with self.assertRaises(Denied):
                    run_owner_flow(intent(**kw), POLICY, a, l, Path(d), now=NOW)
                self.assertEqual((a.calls, l.called), (0, 0))

    def test_authority_denied_before_profile_and_browser(self):
        with tempfile.TemporaryDirectory() as d:
            os.chmod(d, 0o700)
            a, l = Authority(False), Launcher({})
            with self.assertRaisesRegex(Denied, "AUTHORITY_DENIED") as cm:
                run_owner_flow(intent(), POLICY, a, l, Path(d), now=NOW)
            self.assertNotIn("secret", str(cm.exception))
            self.assertEqual(l.called, 0)
            self.assertEqual(list(Path(d).iterdir()), [])

    def test_profile_permission_and_symlink(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            os.chmod(root, 0o700)
            p = secure_profile(root, "example.com")
            self.assertEqual(p.name, "example.com")
            self.assertEqual(p.stat().st_mode & 0o077, 0)
            other = root/"symlink.test"
            other.symlink_to(p)
            with self.assertRaises(Denied):
                secure_profile(root, "symlink.test")
            os.chmod(root, 0o755)
            with self.assertRaises(Denied):
                secure_profile(root, "example.com")

    def test_session_only_uses_fresh_tab_and_no_page_data(self):
        with tempfile.TemporaryDirectory() as d:
            os.chmod(d, 0o700)
            a, l = Authority(), Launcher({POLICY.account_marker: 1})
            outcome = run_owner_flow(intent(), POLICY, a, l, Path(d), now=NOW)
            self.assertEqual(outcome, "SESSION_PRESENT")
            self.assertEqual(a.calls, 1)
            self.assertEqual(l.context.new_calls, 1)
            self.assertTrue(l.context.closed)
            self.assertEqual(l.context.page.visits, ["https://example.com/account"])

    def test_no_session_and_mfa_owner_only(self):
        with tempfile.TemporaryDirectory() as d:
            os.chmod(d, 0o700)
            a = Authority()
            markers = {POLICY.otp_selector: 1}
            l = Launcher(markers)
            self.assertEqual(run_owner_flow(intent(), POLICY, a, l, Path(d), now=NOW), "NO_SESSION")
            req = intent(action="reauthenticate", path="/login")
            self.assertEqual(run_owner_flow(req, POLICY, a, l, Path(d), now=NOW), "HUMAN_ACTION_REQUIRED")
            otp = OTP(markers)
            self.assertEqual(run_owner_flow(req, POLICY, a, l, Path(d), now=NOW, owner_otp=otp), "SESSION_PRESENT")
            self.assertEqual(otp.calls, 1)
            self.assertNotIn("123456", str(otp.calls))

    def test_human_chrome_extension_interactive_timeout_is_bounded(self):
        with tempfile.TemporaryDirectory() as d:
            os.chmod(d, 0o700)
            req = intent(action="reauthenticate", path="/login")
            a, l = Authority(), Launcher({POLICY.otp_selector: 1})
            self.assertEqual(run_owner_flow(req, POLICY, a, l, Path(d), now=NOW,
                                            human_login_timeout_seconds=30), "HUMAN_ACTION_REQUIRED")
            self.assertEqual(l.context.page.waits, [(POLICY.account_marker, 30000)])
            with self.assertRaises(Denied):
                run_owner_flow(req, POLICY, Authority(), Launcher({}), Path(d), now=NOW,
                               human_login_timeout_seconds=121)

    def test_authority_missing_and_unknown_profile(self):
        with tempfile.TemporaryDirectory() as d:
            os.chmod(d, 0o700)
            l = Launcher({})
            with self.assertRaises(Denied):
                run_owner_flow(intent(), POLICY, None, l, Path(d), now=NOW)
            self.assertEqual(l.called, 0)

if __name__ == "__main__":
    unittest.main()
