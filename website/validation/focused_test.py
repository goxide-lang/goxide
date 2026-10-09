"""Focused-gate failure controls; no compiler, browser, dependency install or network."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import focused


class FocusedParityTests(unittest.TestCase):
    def test_bytes_identity_and_result_kind_are_required(self):
        expected = dict(id='example-LF', exit=0, go='package main\n')
        focused.compare_result(expected, dict(id='example-LF', go='package main\n'))
        for actual in [dict(id='other-LF', go='package main\n'),
                       dict(id='example-LF', go='package main\r\n'),
                       dict(id='example-LF', error='failure'),
                       dict(id='example-LF', go='package main\n', error='failure')]:
            with self.subTest(actual=actual), self.assertRaises(ValueError):
                focused.compare_result(expected, actual)

    def test_diagnostics_compare_exactly(self):
        expected = dict(id='reject-CRLF', exit=1, error='playground.gox:3:2: bad type')
        focused.compare_result(expected, dict(id=expected['id'], error=expected['error']))
        with self.assertRaisesRegex(ValueError, 'bytes differ'):
            focused.compare_result(expected, dict(id=expected['id'], error='playground.gox:3:3: bad type'))

    def test_failed_run_retains_negative_summary(self):
        with tempfile.TemporaryDirectory() as temporary:
            out = Path(temporary) / 'fresh'
            with mock.patch('sys.argv', ['focused.py', '--out', str(out)]), \
                 mock.patch.object(focused, 'run', side_effect=ValueError('worker unavailable')), \
                 mock.patch('builtins.print'), self.assertRaisesRegex(ValueError, 'worker unavailable'):
                focused.main()
            summary = json.loads((out / 'summary.json').read_text())
            self.assertFalse(summary['passed'])
            self.assertEqual(summary['failure'], 'worker unavailable')
            self.assertEqual(summary['httpWorker']['status'], 'not-run')


class FocusedInputTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        examples = self.root / 'website/examples'
        examples.mkdir(parents=True)
        (examples / 'one.gox').write_bytes(b'package main\r\n')
        (self.root / 'website/catalog-source.json').write_text(json.dumps(dict(compilerCommit='pin', compilerDigest='digest', examples=[dict(id='one', expectedStdout='one\n')])))
        self.manifest = dict(commit='pin', compilerDigest='digest', examples=[dict(id='one', source='package main\r\n', stdout='one\n')])

    def test_catalog_and_manifest_sources_must_match_exactly(self):
        cases = focused.load_cases(self.root, self.manifest, [])
        self.assertEqual(cases[0]['source'], 'package main\r\n')
        self.manifest['examples'][0]['source'] = 'package main\n'
        with self.assertRaisesRegex(ValueError, 'source differs'):
            focused.load_cases(self.root, self.manifest, [])

    def test_missing_or_duplicated_manifest_example_fails(self):
        for examples in [[], self.manifest['examples'] * 2]:
            with self.subTest(examples=examples), self.assertRaisesRegex(ValueError, 'membership differs'):
                focused.load_cases(self.root, dict(self.manifest, examples=examples), [])

    def test_changed_catalog_compiler_identity_fails(self):
        with self.assertRaisesRegex(ValueError, 'compiler identity differs'):
            focused.load_cases(self.root, dict(self.manifest, compilerDigest='other'), [])

    def test_extra_cases_require_independent_behavior_or_diagnostic(self):
        extra = self.root / 'cases.json'
        for case in [dict(id='unchecked', source='package main'),
                     dict(id='unchecked', source='package main', expect='frontend-reject'),
                     dict(id='../escape', source='package main', expectedStdout=''),
                     dict(id='website-one', source='package main', expectedStdout='')]:
            extra.write_text(json.dumps([case]))
            with self.subTest(case=case), self.assertRaises(ValueError):
                focused.load_cases(self.root, self.manifest, [extra])

    def test_extra_source_file_retains_bytes(self):
        (self.root / 'fixture.gox').write_bytes(b'package main\r\n// fixture\r\n')
        extra = self.root / 'cases.json'
        extra.write_text(json.dumps([dict(id='extra', sourcePath='fixture.gox', expectedStdout='')]))
        self.assertEqual(focused.load_cases(self.root, self.manifest, [extra])[-1]['source'], 'package main\r\n// fixture\r\n')

    def test_output_cannot_reuse_evidence_or_hide_module_owner(self):
        existing = self.root / 'existing'
        existing.mkdir()
        with self.assertRaisesRegex(ValueError, 'fresh'):
            focused.reserve_output(existing, self.root / 'checkout')
        (self.root / 'go.mod').symlink_to(self.root / 'missing-go.mod')
        with self.assertRaisesRegex(ValueError, 'module-free'):
            focused.reserve_output(self.root / 'fresh', self.root / 'checkout')
        self.assertFalse((self.root / 'fresh').exists())


if __name__ == '__main__':
    unittest.main()
