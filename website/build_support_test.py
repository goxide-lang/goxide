"""Small identity regressions; no compiler build, dependency install or network."""
import copy
import hashlib
import pathlib
import tempfile
import unittest
from unittest import mock

import build_support
import catalog


def source_digest(files):
    digest = hashlib.sha256()
    for name, data in sorted(files.items(), key=lambda item: pathlib.PurePosixPath(item[0])):
        digest.update(name.encode())
        digest.update(data)
    return digest.hexdigest()


class CompilerIdentityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.files = {'go.mod': b'module fixture\n', 'go.sum': b'',
                      'internal/probe/probe.go': b'package probe\n',
                      'runtime/value.go': b'package runtime\n'}
        for name, data in self.files.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        self.patch = mock.patch.object(build_support.subprocess, 'check_output', side_effect=self.git)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def git(self, args, *, cwd):
        self.assertEqual(cwd, self.root)
        if args == ['git', 'ls-tree', '-r', '--name-only', '-z', 'reviewed']:
            return ('\0'.join([*self.files, 'internal/probe/probe_test.go', 'website/build.py']) + '\0').encode()
        self.assertEqual(args[:2], ['git', 'show'])
        pin, name = args[2].split(':', 1)
        self.assertEqual(pin, 'reviewed')
        return self.files[name]

    def verify(self, digest=None):
        return build_support.verify_compiler_sources(self.root, 'reviewed', digest or source_digest(self.files))

    def test_identical_sources_with_independent_website_changes(self):
        (self.root / 'website').mkdir()
        (self.root / 'website/build.py').write_text('new website tooling')
        (self.root / 'internal/probe/probe_test.go').write_text('new regression')
        self.assertEqual(self.verify(), source_digest(self.files))

    def test_source_change_rejected_even_with_recomputed_digest(self):
        changed = dict(self.files, **{'runtime/value.go': b'package different\n'})
        (self.root / 'runtime/value.go').write_bytes(changed['runtime/value.go'])
        with self.assertRaisesRegex(ValueError, 'source differs'):
            self.verify(source_digest(changed))

    def test_missing_source_rejected_even_with_recomputed_digest(self):
        (self.root / 'runtime/value.go').unlink()
        reduced = {k: v for k, v in self.files.items() if k != 'runtime/value.go'}
        with self.assertRaisesRegex(ValueError, 'missing=.*runtime/value.go'):
            self.verify(source_digest(reduced))

    def test_added_source_rejected_even_with_recomputed_digest(self):
        added = dict(self.files, **{'internal/probe/extra.go': b'package probe\n'})
        (self.root / 'internal/probe/extra.go').write_bytes(added['internal/probe/extra.go'])
        with self.assertRaisesRegex(ValueError, 'added=.*internal/probe/extra.go'):
            self.verify(source_digest(added))

    def test_reviewed_digest_is_not_optional(self):
        with self.assertRaisesRegex(ValueError, 'digest differs'):
            self.verify('0' * 64)

    def test_added_nested_production_package_is_not_outside_inventory(self):
        for name in ['runtime/nested/extra.go', 'formatting/nested/extra.go', 'cmd/goxide/nested/extra.go']:
            with self.subTest(name=name):
                path = self.root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('package extra\n')
                with self.assertRaisesRegex(ValueError, 'inventory differs'):
                    self.verify()
                path.unlink()


class LicenseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()

    def test_standard_distribution(self):
        (self.root / 'LICENSE').write_text('Go license')
        self.assertEqual(build_support.go_license_path(self.root), self.root / 'LICENSE')

    def test_homebrew_distribution_and_resolved_symlink(self):
        (self.root / 'libexec').mkdir()
        (self.root / 'LICENSE').write_text('Go license')
        (self.root / 'current').symlink_to(self.root / 'libexec', target_is_directory=True)
        self.assertEqual(build_support.go_license_path(self.root / 'current'), self.root / 'LICENSE')

    def test_standard_license_wins_inside_libexec(self):
        (self.root / 'libexec').mkdir()
        (self.root / 'libexec/LICENSE').write_text('selected Go license')
        (self.root / 'LICENSE').write_text('parent license')
        self.assertEqual(build_support.go_license_path(self.root / 'libexec'), self.root / 'libexec/LICENSE')

    def test_missing_license_does_not_borrow_unrelated_parent(self):
        (self.root / 'other-go').mkdir()
        (self.root / 'LICENSE').write_text('unrelated license')
        with self.assertRaisesRegex(FileNotFoundError, 'selected toolchain'):
            build_support.go_license_path(self.root / 'other-go')


class ProposalEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.path = self.root / 'website/evidence/proposal.txt'
        self.path.parent.mkdir(parents=True)
        self.content = b'# Proposal\nNot implemented.\n'
        self.path.write_bytes(self.content)
        self.entry = '@proposal:docs/proposal.md'
        self.snapshot = dict(sourceCommit='1' * 40, sourcePath='docs/proposal.md',
                             path='website/evidence/proposal.txt',
                             sha256=hashlib.sha256(self.content).hexdigest(),
                             gitBlob=hashlib.sha1(b'blob ' + str(len(self.content)).encode() + b'\0' + self.content).hexdigest())
        self.data = {'compilerCommit': '2' * 40, 'compilerDigest': '3' * 64,
                     'evidenceRefs': {'proposal': '1' * 40}, 'evidenceSnapshots': {self.entry: self.snapshot}}
        patch = mock.patch.object(catalog, 'ROOT', self.root)
        patch.start()
        self.addCleanup(patch.stop)

    def test_exact_evidence_requires_no_nonancestor_git_object(self):
        with mock.patch.object(catalog.subprocess, 'check_output', side_effect=AssertionError('must not query unrelated Git object')):
            self.assertEqual(catalog.evidence_bytes(self.data, self.entry), self.content)

    def test_mutated_missing_or_wrong_hash_evidence_rejected(self):
        for field in ['sha256', 'gitBlob', 'sourceCommit', 'sourcePath']:
            with self.subTest(field=field):
                data = copy.deepcopy(self.data)
                data['evidenceSnapshots'][self.entry][field] = 'incorrect'
                with self.assertRaises(ValueError):
                    catalog.evidence_bytes(data, self.entry)
        self.path.write_bytes(self.content + b'changed')
        with self.assertRaisesRegex(ValueError, 'hash differs'):
            catalog.evidence_bytes(self.data, self.entry)
        self.path.unlink()
        with self.assertRaises(FileNotFoundError):
            catalog.evidence_bytes(self.data, self.entry)

    def test_reference_without_snapshot_is_not_resolved_from_other_refs(self):
        self.data['evidenceSnapshots'] = {}
        with self.assertRaisesRegex(ValueError, 'snapshot is required'):
            catalog.evidence_bytes(self.data, self.entry)

    def test_moving_reference_cannot_authorize_snapshot_even_when_both_fields_match(self):
        for ref in ['HEAD', 'main', '1' * 7, 'g' * 40, '1' * 40 + '\n']:
            with self.subTest(ref=ref):
                data = copy.deepcopy(self.data)
                data['evidenceRefs']['proposal'] = ref
                data['evidenceSnapshots'][self.entry]['sourceCommit'] = ref
                with self.assertRaisesRegex(ValueError, 'complete 40-digit'):
                    catalog.evidence_bytes(data, self.entry)

    def test_all_identity_fields_require_complete_hashes(self):
        changes = [
            (('compilerCommit',), 'HEAD'),
            (('compilerDigest',), '3' * 63),
            (('evidenceRefs', 'unused'), 'main'),
            (('evidenceSnapshots', self.entry, 'sourceCommit'), '1' * 7),
            (('evidenceSnapshots', self.entry, 'sha256'), '0' * 63),
            (('evidenceSnapshots', self.entry, 'gitBlob'), '0' * 39),
            (('evidenceSnapshots', self.entry, 'sha256'), 'z' * 64),
            (('evidenceSnapshots', self.entry, 'gitBlob'), None),
        ]
        for keys, value in changes:
            with self.subTest(field=keys):
                data = copy.deepcopy(self.data)
                target = data
                for key in keys[:-1]:
                    target = target[key]
                target[keys[-1]] = value
                with self.assertRaisesRegex(ValueError, 'complete .*hexadecimal identity'):
                    catalog.evidence_bytes(data, self.entry)

    def test_public_catalog_does_not_export_private_evidence(self):
        data = dict(self.data, schemaVersion=1, compilerCommit='2' * 40, reviewedAt='today',
                    statusLabels={}, topics={}, items=[dict(id='proposal', evidence=[self.entry])])
        public = catalog.export(data, [])
        self.assertNotIn('evidenceSnapshots', public)
        self.assertNotIn('evidenceRefs', public)
        self.assertNotIn('evidence', public['items'][0])


if __name__ == '__main__':
    unittest.main()
