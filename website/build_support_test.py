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


class StandalonePlaygroundTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.scratch = self.root / 'scratch'
        self.scratch.mkdir()
        patch = mock.patch.object(build_support.tempfile, 'gettempdir', return_value=str(self.scratch))
        patch.start()
        self.addCleanup(patch.stop)

    def test_exact_bytes_and_name_without_changing_configured_source(self):
        owner = self.root / 'configured'
        owner.mkdir()
        module = owner / 'go.mod'
        module.write_bytes(b'module example.org/original\n')
        source = owner / 'example.gox'
        content = 'package main\r\n// 中文\r\nfunc main() {}'.encode()
        source.write_bytes(content)
        before = {p.name: p.read_bytes() for p in owner.iterdir()}
        with build_support.standalone_playground(source.read_bytes()) as copied:
            self.assertEqual(copied.name, 'playground.gox')
            self.assertEqual(copied.read_bytes(), content)
            self.assertEqual(copied.parent.parent, self.scratch)
            self.assertTrue(copied.is_absolute())
            self.assertNotEqual(copied, source)
        self.assertFalse(copied.parent.exists())
        self.assertEqual({p.name: p.read_bytes() for p in owner.iterdir()}, before)

    def test_exception_still_removes_temporary_source(self):
        with self.assertRaisesRegex(RuntimeError, 'compiler failure'):
            with build_support.standalone_playground(b'package main\n') as copied:
                self.assertTrue(copied.is_file())
                raise RuntimeError('compiler failure')
        self.assertFalse(copied.parent.exists())
        self.assertEqual(list(self.scratch.iterdir()), [])

    def test_module_at_temp_root_or_ancestor_rejected_before_creation(self):
        for directory in (self.scratch, self.root):
            with self.subTest(directory=directory):
                module = directory / 'go.mod'
                module.write_bytes(b'module example.org/ambient\n')
                with mock.patch.object(build_support.tempfile, 'TemporaryDirectory',
                                       side_effect=AssertionError('must reject before creating a directory')):
                    with self.assertRaisesRegex(ValueError, 'module-free temporary directory'):
                        with build_support.standalone_playground(b'package main\n'):
                            self.fail('module context was accepted')
                self.assertEqual(module.read_bytes(), b'module example.org/ambient\n')
                module.unlink()
        # Native owner discovery uses Lstat: a dangling go.mod is not standalone.
        module = self.scratch / 'go.mod'
        module.symlink_to(self.scratch / 'missing-module')
        with self.assertRaisesRegex(ValueError, 'module-free temporary directory'):
            with build_support.standalone_playground(b'package main\n'):
                self.fail('dangling module context was accepted')
        self.assertTrue(module.is_symlink())

    def test_symlink_cannot_hide_real_module_ancestor(self):
        owner = self.root / 'configured'
        owner.mkdir()
        (owner / 'go.mod').write_text('module example.org/ambient\n')
        real_temp = owner / 'tmp'
        real_temp.mkdir()
        link = self.scratch / 'innocent-temp'
        link.symlink_to(real_temp, target_is_directory=True)
        with mock.patch.object(build_support.tempfile, 'gettempdir', return_value=str(link)):
            with self.assertRaisesRegex(ValueError, 'module-free temporary directory'):
                with build_support.standalone_playground(b'package main\n'):
                    self.fail('symlink hid real module context')
        self.assertEqual(list(real_temp.iterdir()), [])

    def test_symlink_to_module_free_root_uses_real_input_path(self):
        owner = self.root / 'configured'
        owner.mkdir()
        (owner / 'go.mod').write_text('module example.org/logical-only\n')
        link = owner / 'tmp'
        link.symlink_to(self.scratch, target_is_directory=True)
        with mock.patch.object(build_support.tempfile, 'gettempdir', return_value=str(link)):
            with build_support.standalone_playground(b'package main\r\n') as copied:
                self.assertEqual(copied.parent.parent, self.scratch)
                self.assertEqual(copied, copied.resolve())
                self.assertEqual(copied.read_bytes(), b'package main\r\n')
        self.assertFalse(copied.parent.exists())


class CompilerIdentityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.files = {'go.mod': b'module fixture\n', 'go.sum': b'',
                      'internal/probe/probe.go': b'package probe\n',
                      'cmd/goxide/main.go': b'package main\n',
                      build_support.DEPENDENCY_CONTEXT: b'[roots.fmt]\nsource="toolchain"\npackage="fmt"\n'}
        for name, data in self.files.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        self.patch = mock.patch.object(build_support.subprocess, 'check_output', side_effect=self.git)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def git(self, args, *, cwd):
        self.assertEqual(cwd, self.root)
        if args == ['git', 'rev-parse', 'HEAD']:
            return b'a' * 40 + b'\n'
        if args[:-1] == ['git', 'ls-tree', '-r', '--name-only', '-z'] and args[-1] in ['reviewed', 'a' * 40]:
            return ('\0'.join([*self.files, 'internal/probe/probe_test.go', 'website/build.py']) + '\0').encode()
        self.assertEqual(args[:2], ['git', 'show'])
        pin, name = args[2].split(':', 1)
        self.assertIn(pin, ['reviewed', 'a' * 40])
        return self.files[name]

    def verify(self, digest=None):
        return build_support.verify_compiler_sources(self.root, 'reviewed', digest or source_digest(self.files))

    def test_identical_sources_with_independent_website_changes(self):
        (self.root / 'website').mkdir()
        (self.root / 'website/build.py').write_text('new website tooling')
        (self.root / 'internal/probe/probe_test.go').write_text('new regression')
        self.assertEqual(self.verify(), source_digest(self.files))

    def test_source_change_rejected_even_with_recomputed_digest(self):
        changed = dict(self.files, **{'cmd/goxide/main.go': b'package different\n'})
        (self.root / 'cmd/goxide/main.go').write_bytes(changed['cmd/goxide/main.go'])
        with self.assertRaisesRegex(ValueError, 'source differs'):
            self.verify(source_digest(changed))

    def test_missing_source_rejected_even_with_recomputed_digest(self):
        (self.root / 'cmd/goxide/main.go').unlink()
        reduced = {k: v for k, v in self.files.items() if k != 'cmd/goxide/main.go'}
        with self.assertRaisesRegex(ValueError, 'missing=.*cmd/goxide/main.go'):
            self.verify(source_digest(reduced))

    def test_added_source_rejected_even_with_recomputed_digest(self):
        added = dict(self.files, **{'internal/probe/extra.go': b'package probe\n'})
        (self.root / 'internal/probe/extra.go').write_bytes(added['internal/probe/extra.go'])
        with self.assertRaisesRegex(ValueError, 'added=.*internal/probe/extra.go'):
            self.verify(source_digest(added))

    def test_reviewed_digest_is_not_optional(self):
        with self.assertRaisesRegex(ValueError, 'digest differs'):
            self.verify('0' * 64)

    def test_changed_embedded_dependency_context_rejected(self):
        (self.root / build_support.DEPENDENCY_CONTEXT).write_text('[roots.undeclared]\n')
        with self.assertRaisesRegex(ValueError, 'compiler source differs'):
            self.verify()

    def test_worktree_identity_distinguishes_source_bytes_from_base_commit(self):
        before = build_support.worktree_compiler_source(self.root)
        self.assertEqual(before, dict(kind='worktree', baseCommit='a' * 40,
                                     digest=source_digest(self.files), dirty=False))
        (self.root / 'internal/probe/probe_test.go').write_text('test changes only')
        self.assertEqual(build_support.worktree_compiler_source(self.root), before)
        (self.root / 'cmd/goxide/main.go').write_bytes(b'package changed\n')
        after = build_support.worktree_compiler_source(self.root)
        self.assertTrue(after['dirty'])
        self.assertEqual(after['baseCommit'], before['baseCommit'])
        self.assertNotEqual(after['digest'], before['digest'])
        with self.assertRaisesRegex(ValueError, 'worktree changed'):
            build_support.verify_compiler_source(self.root, before)
        build_support.verify_compiler_source(self.root, after)

    def test_worktree_inventory_records_added_and_removed_sources(self):
        original = build_support.worktree_compiler_source(self.root)
        added = self.root / 'internal/probe/added.go'
        added.write_text('package probe\n')
        changed = dict(self.files, **{'internal/probe/added.go': added.read_bytes()})
        self.assertEqual(build_support.worktree_compiler_source(self.root)['digest'], source_digest(changed))
        added.unlink()
        (self.root / 'cmd/goxide/main.go').unlink()
        del changed['internal/probe/added.go']
        del changed['cmd/goxide/main.go']
        current = build_support.worktree_compiler_source(self.root)
        self.assertTrue(current['dirty'])
        self.assertEqual(current['digest'], source_digest(changed))
        self.assertNotEqual(current['digest'], original['digest'])

    def test_added_nested_production_package_is_not_outside_inventory(self):
        for name in ['internal/nested/extra.go', 'cmd/goxide/nested/extra.go', 'cmd/goxide-playground/nested/extra.go']:
            with self.subTest(name=name):
                path = self.root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('package extra\n')
                with self.assertRaisesRegex(ValueError, 'inventory differs'):
                    self.verify()
                path.unlink()


class DependencyContextTests(unittest.TestCase):
    def test_groups_do_not_infer_packages_and_published_bytes_are_exact(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            path = root / build_support.DEPENDENCY_CONTEXT
            path.parent.mkdir(parents=True)
            source = b'[roots.std]\nmodule="example.org/std"\nnamespaces={value="example.org/std/value"}\n'
            path.write_bytes(source)
            context = build_support.dependency_context(root)
            self.assertEqual(context['source'].encode(), source)
            self.assertEqual(context['sha256'], hashlib.sha256(source).hexdigest())
            self.assertEqual(context['bindings'], [dict(path='std::value', package='example.org/std/value', module='example.org/std', source='module')])


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


class DependencySourcesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.compiler = self.root / 'compiler'
        self.goroot = self.root / 'toolchain'
        self.module = self.root / 'cached-module'
        self.exports = self.root / 'exports'
        for directory in (self.compiler, self.goroot, self.module, self.exports):
            directory.mkdir()
            (directory / 'LICENSE').write_text('license\n')
        self.records = []

    def package(self, path, directory, **identity):
        directory.mkdir(parents=True)
        content = b'package dependency\r\n// complete helper\r\nfunc helper() {}\r\n'
        (directory / 'source.go').write_bytes(content)
        (directory / 'source_test.go').write_text('must not publish tests')
        (directory / 'inactive.go').write_text('must not publish another target')
        archive = path.replace('/', '_') + '.a'
        (self.exports / archive).write_bytes(b'exact native export archive for ' + path.encode())
        record = dict(ImportPath=path, Dir=str(directory), Export='/unused/native-export.a',
                      GoFiles=['source.go'], TestGoFiles=['source_test.go'],
                      IgnoredGoFiles=['inactive.go'], **identity)
        self.records.append(record)
        return record, content

    def snapshot(self, packages):
        return build_support.dependency_sources(
            packages, self.records, goroot=self.goroot,
            toolchain_version='go1.27.1', exports=self.exports)

    def test_fixed_scope_complete_bytes_and_archive_identity(self):
        record, content = self.package('example.org/library/value', self.module / 'value',
                                      Module=dict(Path='example.org/library', Dir=str(self.module),
                                                  Version='v1.2.3', Sum='h1:module', GoModSum='h1:mod'))
        self.package('transitive', self.goroot / 'src/transitive', Standard=True, Goroot=True)
        cgo = b'package dependency\n// a complete cgo source\n'
        (self.module / 'value/cgo.go').write_bytes(cgo)
        record['CgoFiles'] = ['cgo.go']
        result = self.snapshot(['example.org/library/value'])
        self.assertEqual(len(result), 1)
        dependency = result[0]
        self.assertEqual(dependency['version'], 'v1.2.3')
        self.assertEqual(dependency['source'], 'example.org/library@v1.2.3')
        self.assertEqual(dependency['sum'], 'h1:module')
        self.assertEqual(dependency['goModSum'], 'h1:mod')
        self.assertEqual([file['path'] for file in dependency['files']],
                         ['example.org/library/value/cgo.go', 'example.org/library/value/source.go'])
        for file, expected in zip(dependency['files'], [cgo, content]):
            self.assertEqual(file['source'].encode(), expected)
            self.assertEqual(file['sha256'], hashlib.sha256(expected).hexdigest())
        archive = self.exports / 'example.org_library_value.a'
        self.assertEqual(dependency['exportSha256'], hashlib.sha256(archive.read_bytes()).hexdigest())
        self.assertEqual(dependency['licenses'][0]['path'], 'example.org/library/LICENSE')

    def test_toolchain_sources_have_their_actual_identity(self):
        self.package('fmt', self.goroot / 'src/fmt', Standard=True, Goroot=True)
        standard, = self.snapshot(['fmt'])
        self.assertEqual(standard['source'], 'Go distribution go1.27.1')
        self.assertEqual(standard['version'], 'go1.27.1')
        self.assertEqual(standard['files'][0]['path'], 'GOROOT/src/fmt/source.go')

    def test_compiler_sources_cannot_be_published_as_support_dependencies(self):
        self.package('example.org/compiler/runtime', self.compiler / 'runtime',
                     Module=dict(Path='example.org/compiler', Main=True, Dir=str(self.compiler)))
        with self.assertRaisesRegex(ValueError, 'compiler main module is not a published dependency'):
            self.snapshot(['example.org/compiler/runtime'])

    def test_missing_or_unexported_dependency_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'export record missing'):
            self.snapshot(['missing'])
        record, _ = self.package('fmt', self.goroot / 'src/fmt', Standard=True, Goroot=True)
        del record['Export']
        with self.assertRaisesRegex(ValueError, 'export record missing'):
            self.snapshot(['fmt'])

    def test_module_replacement_or_missing_checksum_is_rejected(self):
        record, _ = self.package('example.org/library/value', self.module / 'value',
                                Module=dict(Path='example.org/library', Dir=str(self.module),
                                            Version='v1.2.3', Sum='h1:module', GoModSum='h1:mod'))
        record['Module']['Replace'] = dict(Dir='/unreviewed')
        with self.assertRaisesRegex(ValueError, 'replacements are not publishable'):
            self.snapshot(['example.org/library/value'])
        del record['Module']['Replace']
        del record['Module']['Sum']
        with self.assertRaisesRegex(ValueError, 'verified module identity'):
            self.snapshot(['example.org/library/value'])

    def test_missing_source_and_path_escape_fail_instead_of_partial_snapshot(self):
        record, _ = self.package('fmt', self.goroot / 'src/fmt', Standard=True, Goroot=True)
        record['GoFiles'] = ['missing.go']
        with self.assertRaises(FileNotFoundError):
            self.snapshot(['fmt'])
        record['GoFiles'] = ['../../../compiler/LICENSE']
        with self.assertRaisesRegex(ValueError, 'source escapes'):
            self.snapshot(['fmt'])
        (self.goroot / 'src/fmt/link.go').symlink_to(self.compiler / 'LICENSE')
        record['GoFiles'] = ['link.go']
        with self.assertRaisesRegex(ValueError, 'source escapes'):
            self.snapshot(['fmt'])


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
