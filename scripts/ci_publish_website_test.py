#!/usr/bin/env python3
"""Adversarial release guards using temporary repositories, no network or credentials."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from ci_publish_website import ASSETS, git, publish, validate_artifact


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='goxide-site-test-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'source'
        self.root.mkdir()
        self.dist = self.base / 'dist'
        self.dist.mkdir()
        git(self.root, 'init', '-q')
        git(self.root, 'config', 'user.name', 'Test')
        git(self.root, 'config', 'user.email', 'test@example.invalid')
        (self.root / 'website').mkdir()
        catalog = dict(compilerCommit='1' * 40, compilerDigest='2' * 64)
        (self.root / 'website/catalog-source.json').write_text(json.dumps(catalog))
        git(self.root, 'add', '.')
        git(self.root, 'commit', '-qm', 'previous')
        self.previous = git(self.root, 'rev-parse', 'HEAD')
        (self.root / 'changed').write_text('new main')
        git(self.root, 'add', '.')
        git(self.root, 'commit', '-qm', 'current')
        self.sha = git(self.root, 'rev-parse', 'HEAD')
        for name in ASSETS:
            (self.dist / name).write_text('fixture')
        self.manifest = dict(websiteCommit=self.sha, commit=catalog['compilerCommit'],
            compilerDigest=catalog['compilerDigest'], catalog=catalog,
            wasmSha256=hashlib.sha256((self.dist / 'compiler.wasm').read_bytes()).hexdigest())
        (self.dist / 'examples.json').write_text(json.dumps(self.manifest))
        self.remote = self.base / 'target.git'
        git(self.base, 'init', '--bare', '-q', str(self.remote))
        self.target = self.base / 'target'
        git(self.base, 'clone', '-q', str(self.remote), str(self.target))
        git(self.target, 'config', 'user.name', 'Test')
        git(self.target, 'config', 'user.email', 'test@example.invalid')
        git(self.target, 'checkout', '-qb', 'deploy')
        (self.target / 'examples.json').write_text(json.dumps(dict(websiteCommit=self.previous)))
        git(self.target, 'add', '.')
        git(self.target, 'commit', '-qm', 'old deployment')
        git(self.target, 'push', '-q', 'origin', 'deploy')
        self.old_deployment = self.remote_head()

    def remote_head(self):
        return git(self.base, '--git-dir=' + str(self.remote), 'rev-parse', 'refs/heads/deploy')

    def run_publish(self, current):
        return publish(self.dist, self.sha, self.root, str(self.remote), 'deploy', current,
            None, 'https://example.invalid/ci/1')

    def test_success_preserves_history_and_records_exact_source(self):
        result = self.run_publish(lambda: self.sha)
        self.assertEqual(result['status'], 'published')
        self.assertEqual(git(self.base, '--git-dir=' + str(self.remote),
            'rev-parse', 'refs/heads/deploy^'), self.old_deployment)
        self.assertIn(self.sha, git(self.base, '--git-dir=' + str(self.remote),
            'show', 'refs/heads/deploy:README.md'))

    def test_stale_run_never_pushes(self):
        for sequence in [[self.previous], [self.sha, self.previous]]:
            heads = iter(sequence)
            self.assertEqual(self.run_publish(lambda: next(heads))['status'], 'superseded')
            self.assertEqual(self.remote_head(), self.old_deployment)

    def test_non_fast_forward_is_fatal_without_retry(self):
        calls = 0
        def concurrent_head():
            nonlocal calls
            calls += 1
            if calls == 2:
                (self.target / 'README.md').write_text('concurrent writer')
                git(self.target, 'add', '.')
                git(self.target, 'commit', '-qm', 'concurrent')
                git(self.target, 'push', '-q', 'origin', 'deploy')
            return self.sha
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_publish(concurrent_head)
        self.assertEqual(git(self.base, '--git-dir=' + str(self.remote),
            'show', 'refs/heads/deploy:README.md'), 'concurrent writer')

    def test_newer_or_unrelated_deployed_source_is_not_overwritten(self):
        (self.target / 'examples.json').write_text(json.dumps(dict(websiteCommit='f' * 40)))
        git(self.target, 'add', '.')
        git(self.target, 'commit', '-qm', 'unknown deployed source')
        git(self.target, 'push', '-q', 'origin', 'deploy')
        with self.assertRaisesRegex(ValueError, 'rollback or unrelated'):
            self.run_publish(lambda: self.sha)

    def test_artifact_identity_and_allowlist_fail_closed(self):
        with self.assertRaisesRegex(ValueError, 'different website SHA'):
            validate_artifact(self.dist, self.previous, self.root)
        (self.dist / 'compiler.wasm').write_text('tampered')
        with self.assertRaisesRegex(ValueError, 'WASM hash'):
            validate_artifact(self.dist, self.sha, self.root)
        (self.dist / '.git').mkdir()
        with self.assertRaisesRegex(ValueError, 'allowlist'):
            validate_artifact(self.dist, self.sha, self.root)


if __name__ == '__main__':
    unittest.main()
