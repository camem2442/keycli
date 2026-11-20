import os
from pathlib import Path
import tempfile
import unittest
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from chrome_profile_guard import ProfileRejected, verify_profile_root


class ChromeProfileGuardTests(unittest.TestCase):
    def test_protected_dedicated_profile_root(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d) / "keycli-owner-chrome"
            root.mkdir(mode=0o700)
            verify_profile_root(root)

    def test_default_chrome_profile_folders_refused(self):
        for segments in (("Library", "Application Support", "Google", "Chrome"),
                         (".config", "google-chrome"), (".config", "chromium")):
            with self.subTest(segments=segments), tempfile.TemporaryDirectory() as d:
                root = Path(d).joinpath(*segments)
                root.mkdir(parents=True, mode=0o700)
                root.chmod(0o700)
                with self.assertRaisesRegex(ProfileRejected, "PERSONAL_CHROME_PROFILE_FORBIDDEN"):
                    verify_profile_root(root)

    def test_symlink_in_ancestor_refused(self):
        with tempfile.TemporaryDirectory() as d:
            base = Path(d)
            real = base / "real"
            real.mkdir(mode=0o700)
            aliased = base / "alias"
            aliased.symlink_to(real, target_is_directory=True)
            nested = aliased / "profiles"
            nested.mkdir(mode=0o700)
            with self.assertRaises(ProfileRejected):
                verify_profile_root(nested)

    def test_world_readable_root_refused(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d) / "owner"
            root.mkdir(mode=0o755)
            with self.assertRaises(ProfileRejected):
                verify_profile_root(root)

    def test_missing_root_refused_without_creation(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d) / "absent"
            with self.assertRaises(ProfileRejected):
                verify_profile_root(root)
            self.assertFalse(root.exists())

    def test_relative_root_refused(self):
        with self.assertRaises(ProfileRejected):
            verify_profile_root(Path("chrome-owner"))


if __name__ == "__main__":
    unittest.main()
