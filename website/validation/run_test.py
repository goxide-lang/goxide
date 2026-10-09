"""Failure-path tests for the runner; no compiler/browser/download is needed."""
import argparse
import importlib.util
import io
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('website_validation_runner', Path(__file__).with_name('run.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


# These tests exercise the historical runner's error handling, not the current
# language examples. Its unchanged hashes belong to this documented snapshot.
HISTORICAL_INPUTS_COMMIT = 'eb6ddf0ce026251b4e9b791d0271ee05b9438749'
SOURCE_ROOT = Path(__file__).resolve().parents[2]


class RunnerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary_inputs = tempfile.TemporaryDirectory(prefix='goxide-historical-inputs-')
        cls.addClassCleanup(cls.temporary_inputs.cleanup)
        cls.historical_root = Path(cls.temporary_inputs.name)
        inputs = ['website/checks', 'website/examples', 'website/qa.mjs',
                  'website/catalog-source.json', 'website/a04-matrix.json',
                  'website/a01-matrix.json', 'website/validation/legacy-matrix.json']
        # Local immutable Git objects only: no fetch, checkout, compiler or browser.
        archive = subprocess.check_output(
            ['git', 'archive', HISTORICAL_INPUTS_COMMIT, '--', *inputs], cwd=SOURCE_ROOT)
        with tarfile.open(fileobj=io.BytesIO(archive)) as files:
            files.extractall(cls.historical_root, filter='data')
        runner.qa_matrix(cls.historical_root)

    def test_current_source_is_not_historical_validation_evidence(self):
        catalog = json.loads((self.historical_root / 'website/catalog-source.json').read_text())
        self.assertEqual(catalog['compilerCommit'], runner.COMPILER)
        self.assertNotEqual(json.loads((SOURCE_ROOT / 'website/catalog-source.json').read_text())['compilerCommit'], runner.COMPILER)
        with self.assertRaisesRegex(RuntimeError, 'legacy website input changed|preserved A04 input changed'):
            runner.qa_matrix(SOURCE_ROOT)

    def test_output_cannot_select_an_ambient_module_for_native_playground(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary).resolve()
            source = base / 'source'
            source.mkdir()
            module = base / 'unrelated-module'
            module.mkdir()
            declaration = module / 'go.mod'
            declaration.write_text('module unrelated.example/owner\n\ngo 1.27\n')
            nested = module / 'nested'
            nested.mkdir()
            link = base / 'module-link'
            link.symlink_to(nested, target_is_directory=True)
            for parent in (module, nested, link):
                with self.assertRaisesRegex(RuntimeError, 'standalone CLI/WASM comparison'):
                    runner.reserve_output(parent / 'evidence', source)
                self.assertFalse((parent / 'evidence').exists())
            self.assertEqual(declaration.read_text(), 'module unrelated.example/owner\n\ngo 1.27\n')
            standalone = runner.reserve_output(base / 'standalone', source)
            self.assertTrue(standalone.is_dir())

    def test_output_never_overwrites_or_enters_checkout(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'source'
            root.mkdir()
            existing = Path(temporary) / 'prior-run'
            existing.mkdir()
            sentinel = existing / 'evidence'
            sentinel.write_text('must survive')
            for value in (existing, root / 'run'):
                with self.assertRaises(RuntimeError):
                    runner.reserve_output(value, root)
            self.assertEqual(sentinel.read_text(), 'must survive')
            self.assertFalse((root / 'run').exists())
            fresh = runner.reserve_output(Path(temporary) / 'fresh', root)
            self.assertTrue(fresh.is_dir())
            missing = Path(temporary) / 'missing-target'
            dangling = Path(temporary) / 'dangling-output'
            dangling.symlink_to(missing)
            with self.assertRaises(RuntimeError):
                runner.reserve_output(dangling, root)
            self.assertTrue(dangling.is_symlink())
            self.assertFalse(missing.exists())

    def test_failed_command_keeps_stdout_stderr_and_partial_summary(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            obj = runner.Runner(root, root, argparse.Namespace())
            obj.current = root
            obj.phase('wasm-build', lambda: obj.command('expected-build-failure', [sys.executable, '-c', 'import sys;print("before failure");print("reason",file=sys.stderr);sys.exit(7)']))
            self.assertEqual(obj.phases['wasm-build']['status'], 'failed')
            command = json.loads((root / 'commands.json').read_text())[0]
            self.assertEqual(command['exit'], 7)
            self.assertEqual((root / command['stdout']).read_text(), 'before failure\n')
            self.assertEqual((root / command['stderr']).read_text(), 'reason\n')
            obj.phase('web-build', lambda: self.fail('dependent phase ran'), ('wasm-build',))
            self.assertEqual(obj.phases['web-build']['status'], 'blocked')
            self.assertFalse(obj.summarize(final=True))
            self.assertFalse(json.loads((root / 'summary.json').read_text())['passed'])

    def test_missing_executable_preserves_launch_error(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            obj = runner.Runner(root, root, argparse.Namespace())
            obj.current = root
            obj.phase('native-parity', lambda: obj.command('missing', [str(root / 'not-installed')]))
            command = json.loads((root / 'commands.json').read_text())[0]
            self.assertIsNone(command['exit'])
            self.assertIn('error', command)
            self.assertTrue((root / command['stdout']).exists())
            self.assertTrue((root / command['stderr']).exists())
            self.assertEqual(obj.phases['native-parity']['status'], 'failed')

    def test_dirty_checkout_stops_before_tool_or_snapshot_creation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'source'
            root.mkdir()
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            (root / 'untracked').write_text('work in progress')
            out = Path(temporary) / 'evidence'
            out.mkdir()
            obj = runner.Runner(root, out, argparse.Namespace())
            self.assertEqual(obj.run(), 1)
            self.assertFalse((out / 'current').exists())
            summary = json.loads((out / 'summary.json').read_text())
            self.assertIn('clean committed checkout', summary['failure'])
            self.assertTrue(all(p['status'] == 'blocked' for p in summary['phases'].values()))
            self.assertEqual((root / 'untracked').read_text(), 'work in progress')

    def test_clean_empty_commit_in_current_snapshot_fails_identity_gate(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'source'
            root.mkdir()
            out = Path(temporary) / 'evidence'
            out.mkdir()

            def git(directory, *args):
                return subprocess.check_output(['git', '-C', str(directory), *args], stderr=subprocess.PIPE).decode().strip()

            git(root, 'init', '-q')
            (root / 'input.txt').write_text('unchanged tracked source')
            git(root, 'add', 'input.txt')
            commit_options = ['-c', 'user.name=Validation Test', '-c', 'user.email=validation@example.invalid',
                              '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null']
            git(root, *commit_options, 'commit', '-q', '-m', 'initial input')
            initial = git(root, 'rev-parse', 'HEAD')
            for name in ('current', 'baseline'):
                git(out, 'clone', '-q', '--no-hardlinks', str(root), str(out / name))

            class LightweightRunner(runner.Runner):
                # Only replace expensive stage bodies. Git observations and the
                # final identity gate are the real production implementation.
                def preflight(self):
                    self.identity = {'validationCommit': initial}
                    self.source_inputs = self.inventory(self.root)
                    self.baseline_inputs = self.inventory(self.baseline)

                def phase(self, name, action, dependencies=()):
                    self.phases[name] = {'status': 'passed'}
                    if name == 'catalog-qa':
                        git(self.current, *commit_options, 'commit', '-q', '--allow-empty', '-m', 'different identity, same tree')

            obj = LightweightRunner(root, out, argparse.Namespace())
            with mock.patch.object(runner, 'BASELINE', initial):
                self.assertEqual(obj.run(), 1)
            changed = git(out / 'current', 'rev-parse', 'HEAD')
            self.assertNotEqual(changed, initial)
            self.assertEqual(git(out / 'current', 'status', '--porcelain'), '')
            self.assertEqual(git(out / 'current', 'rev-parse', 'HEAD^{tree}'), git(root, 'rev-parse', 'HEAD^{tree}'))
            summary = json.loads((out / 'summary.json').read_text())
            identity = json.loads((out / 'identity.json').read_text())
            self.assertTrue(all(phase['status'] == 'passed' for phase in summary['phases'].values()))
            self.assertFalse(summary['passed'])
            self.assertIn('snapshot', summary['failure'])
            self.assertEqual(identity['snapshotHeadAfter'], changed)
            self.assertEqual(identity['validationCommit'], initial)
            self.assertEqual(git(root, 'rev-parse', 'HEAD'), initial)

    def test_complete_matrix_and_byte_positions(self):
        groups, diagnostics = runner.qa_matrix(self.historical_root)
        self.assertEqual(len(diagnostics), 13)
        self.assertEqual(sum(len(x) for x in groups.values()), 70)
        self.assertEqual(runner.position('é\r\nx', 'x'), (4, 2, 1))
        with self.assertRaises(RuntimeError):
            runner.position('x x', 'x')
        self.assertEqual(runner.position('x x', 'x', occurrence=1), (2, 1, 3))

    def test_browser_count_matches_the_served_manifest(self):
        # The historical matrix remains fixed; browser result completeness
        # follows the exact manifest already checked by start_server.
        for expected, observed in [(11, 11), (12, 12), (12, 11)]:
            with self.subTest(expected=expected, observed=observed), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                obj = runner.Runner(root, root, argparse.Namespace())
                obj.node = 'unused-node'
                obj.server = object()
                obj.manifest = {'examples': [{}] * expected}
                result = dict(examples=observed, errors=[], cards=26,
                              negativeExamples=10, consoleErrors=[])
                with mock.patch.object(obj, 'command', return_value=(0, json.dumps(result).encode(), b'')):
                    if observed != expected:
                        with self.assertRaisesRegex(RuntimeError, 'browser result is incomplete'):
                            obj.browser('catalog-qa')
                    else:
                        obj.browser('catalog-qa')
                        self.assertEqual(json.loads((root / 'catalog-qa-result.json').read_text()), result)

    def test_baseline_comparison_excludes_current_only_inputs(self):
        root = self.historical_root
        groups, _ = runner.qa_matrix(root)
        catalog = json.loads((root / 'website/catalog-source.json').read_text())
        manifest = {'examples': [dict(entry, source=(root / 'website/examples' / (entry['id'] + '.gox')).read_text())
                                 for entry in catalog['examples']]}
        sources = runner.legacy_success_sources(root, manifest, groups)
        self.assertEqual(len(sources), 33)
        self.assertFalse(any('match_a04_' in name or 'macro_a01_' in name or name == 'example/match_composition' for name, _ in sources))
        for name in ('match_a04_unary', 'macro_a01_opaque'):
            contaminated = dict(groups, additional=[name])
            with self.assertRaisesRegex(RuntimeError, 'current-only fixture'):
                runner.legacy_success_sources(root, manifest, contaminated)

    def test_fixture_inventory_rejects_changed_legacy_missing_and_unlisted_inputs(self):
        source = self.historical_root
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name in ('checks', 'examples', 'validation'):
                shutil.copytree(source / 'website' / name, root / 'website' / name)
            for name in ('qa.mjs', 'catalog-source.json', 'a04-matrix.json', 'a01-matrix.json'):
                shutil.copyfile(source / 'website' / name, root / 'website' / name)
            legacy = root / 'website/checks/error_only.gox'
            original = legacy.read_bytes()
            legacy.write_bytes(original + b'\n')
            with self.assertRaisesRegex(RuntimeError, 'legacy website input changed'):
                runner.qa_matrix(root)
            legacy.write_bytes(original)
            added = root / 'website/checks/match_a04_unlisted.gox'
            added.write_text('package main\n')
            with self.assertRaisesRegex(RuntimeError, 'cover all 97 legacy, 8 A04 and 12 A01'):
                runner.qa_matrix(root)
            added.unlink()
            (root / 'website/checks/match_a04_unary.gox').unlink()
            with self.assertRaisesRegex(RuntimeError, 'preserved A04 input changed'):
                runner.qa_matrix(root)

    def test_current_only_matrix_requires_independent_error_and_output_expectations(self):
        source = self.historical_root
        original = (source / 'website/a04-matrix.json').read_text()
        for case in json.loads(original)['checks']:
            if case['expect'] == 'frontend-reject':
                self.assertNotRegex('expression match: unsupported unary operator ! or composite operand', case['message'])
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'website/a04-matrix.json'
            path.parent.mkdir()
            for field, index in [('stdout', 0), ('message', 4)]:
                value = json.loads(original)
                del value['checks'][index][field]
                runner.save(path, value)
                with self.assertRaisesRegex(RuntimeError, 'independent output/error expectation'):
                    runner.a04_matrix(root)
            value = json.loads(original)
            value['checks'][-1] = value['checks'][0]
            runner.save(path, value)
            with self.assertRaisesRegex(RuntimeError, 'unique A04 fixtures'):
                runner.a04_matrix(root)

    def test_current_only_runtime_failure_keeps_actual_output(self):
        root = self.historical_root
        with tempfile.TemporaryDirectory() as temporary:
            out = Path(temporary)
            obj = runner.Runner(root, out, argparse.Namespace())
            obj.current, obj.go = root, 'existing-go'
            obj.a04 = runner.a04_matrix(root)
            obj.identity = {'validationCommit': 'website-revision'}
            def command(name, argv, cwd=None, expected=(0,)):
                if 'runtime' in name:
                    return 7, b'actual partial stdout\n', b'actual runtime failure\n'
                if 'reject_' in name:
                    if 'overflow' in name:
                        message = 'arm is not assignable to the result type: cannot use 256 as uint8 value in array or slice literal (overflows)'
                    elif 'bool_operand' in name:
                        message = 'arm is not assignable to the result type: cannot use 1 as bool value in array or slice literal'
                    else:
                        message = 'enum: non-exhaustive match; guards do not contribute coverage'
                    return 1, b'', message.encode()
                return 0, b'package main\n', b''
            with mock.patch.object(obj, 'command', side_effect=command):
                with self.assertRaisesRegex(RuntimeError, 'current-only native behavior differs'):
                    obj.current_a04('current-compiler')
            self.assertEqual(len(json.loads((out / 'native/a04/observations.json').read_text())), 32)
            runtime = json.loads((out / 'native/a04/runtime.json').read_text())
            self.assertEqual(runtime[0]['exit'], 7)
            self.assertEqual(runtime[0]['stdout'], 'actual partial stdout\n')
            self.assertEqual(runtime[0]['stderr'], 'actual runtime failure\n')
            self.assertFalse((out / 'native/a04/summary.json').exists())

    def test_a01_diagnostics_require_exact_dollar_span_and_nested_frame_order(self):
        root = self.historical_root
        matrix = runner.a01_matrix(root)
        case = next(c for c in matrix['checks'] if c['name'] == 'reject_macro_a01_nested')
        fixture = (root / 'website/checks' / (case['name'] + '.gox')).read_text()
        for source in (fixture, fixture.replace('\n', '\r\n')):
            point = case['point']
            start, line, column = runner.position(source, point['anchor'], point['offset'])
            primary = f"playground.gox:{line}:{column}: {case['message']} (source span [{start},{start + 1}))"
            frames = []
            for frame in case['frames']:
                _, dl, dc = runner.position(source, frame['definition'])
                _, cl, cc = runner.position(source, frame['call'])
                frames.append(f"  macro {frame['macro']} defined at playground.gox:{dl}:{dc}; invoked at playground.gox:{cl}:{cc}")
            correct = '\n'.join([primary, *frames])
            runner.assert_origin_diagnostic(correct, case, source)
            # Same message and matching CLI/WASM output alone are insufficient:
            # claiming the full $T span or swapping valid frames must fail.
            with self.assertRaisesRegex(RuntimeError, 'inaccurate original diagnostic point'):
                runner.assert_origin_diagnostic(correct.replace(f'[{start},{start + 1})', f'[{start},{start + 2})'), case, source)
            with self.assertRaisesRegex(RuntimeError, 'reordered macro stack'):
                runner.assert_origin_diagnostic('\n'.join([primary, *reversed(frames)]), case, source)

    def test_a01_matrix_cannot_weaken_category_rejection_to_parser_error(self):
        root = self.historical_root
        original = (root / 'website/a01-matrix.json').read_text()
        with tempfile.TemporaryDirectory() as temporary:
            copied = Path(temporary)
            (copied / 'website').mkdir()
            for key, value in [('message', 'unsupported template syntax'), ('point', None), ('frames', [])]:
                matrix = json.loads(original)
                matrix['checks'][2][key] = value
                runner.save(copied / 'website/a01-matrix.json', matrix)
                with self.assertRaisesRegex(RuntimeError, 'exact message, dollar token and macro frames'):
                    runner.a01_matrix(copied)

    def test_a01_inaccurate_source_evidence_is_saved_before_failure(self):
        root = self.historical_root
        with tempfile.TemporaryDirectory() as temporary:
            out = Path(temporary)
            obj = runner.Runner(root, out, argparse.Namespace())
            obj.current, obj.go = root, 'existing-go'
            matrix = runner.a01_matrix(root)
            obj.identity = {'validationCommit': 'website-revision'}
            def command(name, argv, cwd=None, expected=(0,)):
                self.assertNotIn('runtime', name)
                if 'reject_' in name:
                    return 1, b'', b'playground.gox:1:1: macro type position does not support captured syntax\n'
                return 0, b'package main\n', b''
            with mock.patch.object(obj, 'command', side_effect=command):
                with self.assertRaisesRegex(RuntimeError, 'inaccurate original diagnostic point'):
                    obj.current_matrix('current-compiler', 'a01', matrix, 2)
            observations = json.loads((out / 'native/a01/observations.json').read_text())
            self.assertEqual(len(observations), 9)
            self.assertEqual(observations[-1]['name'], 'reject_macro_a01_var_expr')
            self.assertIn('playground.gox:1:1:', observations[-1]['diagnostic'])
            self.assertEqual(json.loads((out / 'native/a01/runtime.json').read_text()), [])
            self.assertFalse((out / 'native/a01/summary.json').exists())


if __name__ == '__main__':
    unittest.main()
