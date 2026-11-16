"""G4 source boundary checks: do not imply that product release gates ran."""
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/g4-free-release-gate.sh'

class G4ContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = SCRIPT.read_text(encoding='utf-8')

    def test_bash_parses(self):
        self.assertEqual(subprocess.run(['bash', '-n', str(SCRIPT)]).returncode, 0)

    def test_bad_args_rejected_before_any_host_action(self):
        self.assertEqual(subprocess.run(['bash', str(SCRIPT), 'invalid', 'arm64'],
                                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode, 64)

    def test_native_architecture_and_nonroot_required(self):
        for token in ('machine=aarch64', 'machine=x86_64', '$(id -u)', '$(uname -m)',
                      '$(uname -s)'):
            self.assertIn(token, self.source)

    def test_requires_clean_exact_head_and_disables_provider_effects(self):
        for token in ('git rev-parse HEAD', 'git status --porcelain=v1', 'KC_PROVIDER_EFFECTS',
                      'CLOUDFLARE_API_TOKEN', 'OPENAI_API_KEY'):
            self.assertIn(token, self.source)

    def test_uses_existing_release_oracles_not_mimicked_pass(self):
        for token in ('bash verify.sh --suite all', 'release-contract.sh provenance',
                      'release-contract.sh verify-bundle', 'release-contract.sh verify-binary',
                      'verify-dev-release.sh --tag'):
            self.assertIn(token, self.source)
        self.assertNotIn('gh release create', self.source)

    def test_host_provenance_and_offline_module_lock(self):
        for token in ('GITHUB_WORKFLOW_SHA', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT',
                      'GOTOOLCHAIN=local', 'GOPROXY=off', 'GOFLAGS=-mod=readonly'):
            self.assertIn(token, self.source)

    def test_scoped_receipt_not_other_architecture_or_mac(self):
        self.assertIn('NOT VERIFIED: other native architecture', self.source)

if __name__ == '__main__':
    unittest.main()
