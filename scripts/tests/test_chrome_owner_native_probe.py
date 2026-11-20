import os
from pathlib import Path
import plistlib
import tempfile
import unittest
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from chrome_owner_native_probe import probe

class NativeProbeTests(unittest.TestCase):
    def fixture(self):
        t=tempfile.TemporaryDirectory();self.addCleanup(t.cleanup)
        root=Path(t.name)
        app=root/"Google Chrome.app"
        (app/"Contents"/"MacOS").mkdir(parents=True)
        (app/"Contents"/"Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier":"com.google.Chrome","CFBundleShortVersionString":"144.0"}))
        b=app/"Contents"/"MacOS"/"Google Chrome"
        b.write_bytes(b"fixture only")
        b.chmod(0o700)
        profile=root/"owner"
        profile.mkdir(mode=0o700)
        profile.chmod(0o700)
        return app,profile
    def test_linux_never_claims_mac(self):
        app,profile=self.fixture()
        self.assertEqual(probe(app,profile,platform="linux")["reason"],"OWNER_MAC_REQUIRED")
    def test_fixture_only_yields_hold_not_native_pass(self):
        app,profile=self.fixture()
        r=probe(app,profile,platform="darwin")
        self.assertEqual(r["status"],"HOLD")
        self.assertTrue(r["metadata_ok"])
        self.assertFalse(r["native_acceptance"])
        self.assertFalse(r["extension_verified"])
    def test_wrong_bundle_id_fails(self):
        app,profile=self.fixture()
        (app/"Contents"/"Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier":"com.evil.Chrome","CFBundleShortVersionString":"144.0"}))
        self.assertEqual(probe(app,profile,platform="darwin")["reason"],"CHROME_APP_NOT_VERIFIED")
    def test_profile_shared_fails(self):
        app,profile=self.fixture()
        profile.chmod(0o755)
        self.assertEqual(probe(app,profile,platform="darwin")["reason"],"CHROME_PROFILE_NOT_VERIFIED")
    def test_no_paths_in_output(self):
        app,profile=self.fixture()
        self.assertNotIn(str(app),str(probe(app,profile,platform="darwin")))
        self.assertNotIn(str(profile),str(probe(app,profile,platform="darwin")))
    def test_symlink_bundle_fails(self):
        app,profile=self.fixture()
        shortcut=app.parent/"shortcut.app"
        shortcut.symlink_to(app)
        self.assertEqual(probe(shortcut,profile,platform="darwin")["reason"],"CHROME_APP_NOT_VERIFIED")

if __name__ == "__main__": unittest.main()
