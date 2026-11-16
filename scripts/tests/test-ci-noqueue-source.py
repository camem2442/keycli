"""Source-only G2 workflow guard; cannot confer runner or Go acceptance."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class NoQueueContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = (ROOT / '.github/workflows/test.yml').read_text(encoding='utf-8')

    def test_auto_event_jobs_skipped(self):
        before, jobs = self.text.split('\njobs:\n', 1)
        for event in ('push:', 'pull_request:', 'workflow_dispatch:'):
            self.assertIn(event, before)
        self.assertEqual(re.findall(r'^  ([a-z][a-z0-9-]*):\s*$', jobs, re.M),
                         ['isolated-source'])
        self.assertIn("github.event_name == 'workflow_dispatch'", jobs)
        self.assertIn('inputs.admitted_isolated_vm == true', jobs)

    def test_no_paid_or_public_resident_runner(self):
        self.assertNotIn('ubuntu-latest', self.text)
        self.assertNotIn('ubuntu-24.04-arm', self.text)
        self.assertNotIn('runs-on: [self-hosted, Linux, ARM64, android', self.text)
        self.assertIn('runs-on: [self-hosted, Linux, ARM64, keycli-ephemeral-vm]',
                      self.text)

    def test_public_repositories_never_allocate_selfhosted_job(self):
        # Owner-only manual mode is also disabled in this PUBLIC repository.
        # Actual native source execution is owned by PRIVATE Infra #529.
        self.assertIn('      false\n', self.text)
        self.assertIn('github.event_name == \'workflow_dispatch\'', self.text)

    def test_owner_manual_branch_scope(self):
        for required in ("github.repository == 'camem2442/keycli'",
                         "github.ref == 'refs/heads/main'",
                         "github.actor == 'camem2442'",
                         "github.triggering_actor == 'camem2442'"):
            self.assertIn(required, self.text)

    def test_stale_pr_rechecked_before_and_after(self):
        self.assertEqual(self.text.count('          check()'), 2)
        for required in ("p.get('head',{}).get('sha')!=sha",
                         "p.get('draft') is not False",
                         "p.get('base',{}).get('ref')!='main'"):
            self.assertIn(required, self.text)

    def test_queue_coalescing_and_checkout_safety(self):
        self.assertIn('github.event.pull_request.number || inputs.pr_number', self.text)
        self.assertIn('cancel-in-progress: false', self.text)
        self.assertIn('checkout@11d5960a326750d5838078e36cf38b85af677262',
                      self.text)
        self.assertIn('persist-credentials: false', self.text)

    def test_offline_suite_no_duplication(self):
        self.assertIn("for suite in ('docs','policy','unit'):", self.text)
        self.assertIn("'GOPROXY':'off'", self.text)
        self.assertIn("'GOMODCACHE':'/opt/keycli-go-modcache'", self.text)
        self.assertNotIn('sudo apt-get', self.text)


if __name__ == '__main__':
    unittest.main()
