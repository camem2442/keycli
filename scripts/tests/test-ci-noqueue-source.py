"""Source-only G2 workflow guard; cannot confer runner or Go acceptance."""
from pathlib import Path
import ast
import io
import json
import re
import textwrap
import unittest
from urllib.request import Request

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

    def test_reviewed_base_sha_is_required_and_checked_twice(self):
        # Execute the actual inline owner guard in a fake-network sandbox.
        # This does not execute the public workflow or attest native CI.
        self.assertIn('      reviewed_base:', self.text)
        self.assertIn('          BASE: ${{ inputs.reviewed_base }}', self.text)
        self.assertIn("('PR', 'HEAD', 'BASE')", self.text)
        script = self.text.split("          python3 -I -B - <<'PY'\n", 1)[1]
        script = script.split("\n          PY", 1)[0]
        tree = ast.parse(textwrap.dedent(script))
        guard = next(n for n in tree.body
                     if isinstance(n, ast.FunctionDef) and n.name == 'check')
        sha, base = 'a'*40, 'b'*40
        payload = {
            'number': 73, 'state': 'open', 'draft': False,
            'user': {'login': 'camem2442'},
            'head': {'sha': sha, 'repo': {'full_name': 'camem2442/keycli'}},
            'base': {'ref': 'main', 'sha': base,
                     'repo': {'full_name': 'camem2442/keycli'}},
        }
        remote = {'ref': 'refs/heads/main', 'object': {'sha': base}}
        def get_pr(request, timeout):
            self.assertEqual(timeout, 15)
            if request.full_url.endswith('/git/ref/heads/main'):
                return io.BytesIO(json.dumps(remote).encode())
            self.assertTrue(request.full_url.endswith('/pulls/73'))
            return io.BytesIO(json.dumps(payload).encode())
        env = {'n': '73', 'sha': sha, 'base': base, 'json': json,
               'Request': Request, 'urlopen': get_pr}
        exec(compile(ast.Module(body=[guard], type_ignores=[]),
                     '<inline-owner-guard>', 'exec'), env)
        check = env['check']
        check()
        for changed in (
            {'sha': 'c'*40}, {'ref': 'other'},
            {'repo': {'full_name': 'attacker/keycli'}},
        ):
            previous = payload['base'].copy()
            payload['base'].update(changed)
            with self.assertRaisesRegex(SystemExit, 'HOLD: PR review identity changed'):
                check()
            payload['base'] = previous
        remote['object']['sha'] = 'c'*40
        with self.assertRaisesRegex(SystemExit, 'HOLD: remote main moved'):
            check()
        remote['object']['sha'] = base
        check()
        self.assertEqual(self.text.count('          check()'), 2)
        self.assertIn('/git/ref/heads/main', self.text)

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
