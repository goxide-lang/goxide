#!/usr/bin/env python3
"""Run the committed website's complete local QA without modifying its checkout.

This tool never fetches Git objects, installs tools, downloads dependencies or
publishes assets. See README.md for the cache and existing-tool prerequisites.
"""
import argparse
import datetime
import difflib
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import time
import traceback
import urllib.request

BASELINE = 'fd8380c39876c96441857a737be8e32c322a0467'
COMPILER = 'afa3f0a52b2d8206d75df38d10833e537ea7a0dd'
COMPILER_DIGEST = '2412f1a252c1dd195d1a4d5deed55e2dcd605e65d18edb0b838da36b1976203c'
PHASES = ('wasm-build', 'web-build', 'native-parity', 'web-race', 'web-vet',
          'browser-qa', 'catalog-qa')
ROOT = Path(__file__).resolve().parents[2]
sys.dont_write_bytecode = True
BASE_CHECKS = ['error_only', 'typed_nil', 'reject_wrong_type', 'pointer_error',
               'named_error', 'slice_error', 'reject_value_error', 'reject_arity',
               'reject_legacy_cfg', 'reject_macro_selector', 'reject_macro_prepared',
               'reject_derive_origin', 'reject_enum_derive_origin', 'macro_imported_helper']


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def save(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def executable(value):
    found = shutil.which(value)
    require(found, 'existing executable not found: ' + value)
    return str(Path(found).resolve())


def require_standalone_output(out):
    # Native playground and QA inputs live below out. A configured ancestor
    # module would select a different CLI entry from the standalone WASM input.
    for parent in (out, *out.parents):
        require(not os.path.lexists(parent / 'go.mod'),
                '--out ancestors must be free of go.mod for standalone CLI/WASM comparison: ' + str(parent))


def reserve_output(value, source):
    requested = Path(value).expanduser()
    require(not os.path.lexists(requested), '--out must name a new directory: ' + str(requested))
    out = requested.resolve()
    require(not out.is_relative_to(source.resolve()), '--out must be outside the source checkout')
    # lexists also rejects dangling symlinks. Never reuse, clean or overwrite an
    # earlier run; this makes failed evidence and stale status impossible to hide.
    require(not os.path.lexists(out), '--out must name a new directory: ' + str(out))
    require(out.parent.is_dir(), '--out parent must already exist: ' + str(out.parent))
    require_standalone_output(out)
    out.mkdir()
    return out


def qa_matrix(root):
    qa = (root / 'website/qa.mjs').read_text()
    groups = {}
    for name in ('appendChecks', 'copyChecks', 'allocationChecks', 'astPositiveChecks'):
        match = re.search(r'const ' + name + r' = (\[[\s\S]*?\]);', qa)
        require(match, 'missing QA group ' + name)
        groups[name] = re.findall(r"['\"]([^'\"]+)['\"]", match.group(1))
    diagnostics = []
    for name in ('originDiagnosticChecks', 'astDiagnosticChecks'):
        match = re.search(r'const ' + name + r' = (\[[\s\S]*?\n\]);', qa)
        require(match, 'missing diagnostic group ' + name)
        diagnostics += json.loads(match.group(1))
    names = BASE_CHECKS + sum(groups.values(), []) + [c['name'] for c in diagnostics]
    legacy = json.loads((root / 'website/validation/legacy-matrix.json').read_text())
    require(len(names) == len(set(names)) == 97 and set(names) == set(legacy['fixtures']),
            'legacy QA must retain exactly its original 97 fixtures')
    for name, sha in legacy['files'].items():
        require((root / name).is_file() and digest(root / name) == sha,
                'legacy website input changed: ' + name)
    a04 = a04_matrix(root)
    a01 = a01_matrix(root)
    extra = [c['name'] for matrix in (a04, a01) for c in matrix['checks']]
    actual = {p.stem for p in (root / 'website/checks').glob('*.gox')}
    require(len(actual) == 117 and len(extra) == len(set(extra)) and
            set(names + extra) == actual and not set(names).intersection(extra),
            'QA must cover all 97 legacy, 8 A04 and 12 A01 fixtures exactly once')
    require(len(diagnostics) == 13, 'expected 13 diagnostic fixtures')
    examples = json.loads((root / 'website/catalog-source.json').read_text())['examples']
    require(len(examples) == 11 and {e['id'] for e in examples} == set(legacy['examples'] + a04['examples']),
            'expected 10 preserved catalog examples and one current-only A04 example')
    require({c['id'] for c in examples} == {p.stem for p in (root / 'website/examples').glob('*.gox')},
            'catalog example inventory differs from source files')
    return groups, diagnostics


def a04_matrix(root):
    value = json.loads((root / 'website/a04-matrix.json').read_text())
    require(value['schemaVersion'] == 1, 'unsupported A04 matrix schema')
    checks = value['checks']
    require(len(checks) == len({c['name'] for c in checks}) == 8, 'expected 8 unique A04 fixtures')
    require(sum(c['expect'] == 'success' for c in checks) == 4 and
            sum(c['expect'] == 'frontend-reject' for c in checks) == 4, 'expected four A04 successes and four frontend rejections')
    for case in checks:
        require(re.fullmatch(r'(?:reject_)?match_a04_[a-z_]+', case['name']), 'invalid A04 fixture name')
        require(('stdout' in case) if case['expect'] == 'success' else bool(case.get('message')),
                'A04 fixture lacks an independent output/error expectation')
    require(value['examples'] == ['match_composition'], 'expected one separately named A04 example')
    return value


def a01_matrix(root):
    value = json.loads((root / 'website/a01-matrix.json').read_text())
    require(value['schemaVersion'] == 1 and value['examples'] == [], 'unsupported A01 matrix schema/examples')
    checks = value['checks']
    require(len(checks) == len({c['name'] for c in checks}) == 12, 'expected 12 unique A01 fixtures')
    require(sum(c['expect'] == 'success' for c in checks) == 2 and
            sum(c['expect'] == 'frontend-reject' for c in checks) == 10, 'expected two A01 successes and ten category rejections')
    for case in checks:
        require(re.fullmatch(r'(?:reject_)?macro_a01_[a-z_]+', case['name']), 'invalid A01 fixture name')
        if case['expect'] == 'success':
            require(bool(case.get('stdout')), 'A01 success lacks independent stdout')
        else:
            require(case.get('message') == 'macro type position does not support captured syntax' and
                    isinstance(case.get('point'), dict) and case['point'].get('token') == '$' and bool(case.get('frames')),
                    'A01 category rejection requires exact message, dollar token and macro frames')
            source = (root / 'website/checks' / (case['name'] + '.gox')).read_text()
            point = case['point']
            start, _, _ = position(source, point['anchor'], point['offset'])
            require(source.encode()[start:start + 1] == b'$', 'A01 diagnostic anchor is not the template dollar token')
            for frame in case['frames']:
                position(source, frame['definition'])
                position(source, frame['call'], occurrence=frame.get('callOccurrence'))
    preserved = value['preservedA04']
    a04 = a04_matrix(root)
    expected = {'website/a04-matrix.json', 'website/examples/match_composition.gox'}
    expected.update('website/checks/' + case['name'] + '.gox' for case in a04['checks'])
    require(re.fullmatch('[0-9a-f]{40}', preserved['sourceCommit']) and set(preserved['files']) == expected,
            'A01 must preserve the complete A04 fixture/example/matrix inventory')
    for name, sha in preserved['files'].items():
        require((root / name).is_file() and digest(root / name) == sha, 'preserved A04 input changed: ' + name)
    return value


def legacy_success_sources(root, manifest, groups):
    """Only the unchanged legacy inputs are valid baseline/current comparisons."""
    legacy = json.loads((root / 'website/validation/legacy-matrix.json').read_text())
    examples = {entry['id']: entry for entry in manifest['examples']}
    names = ['error_only', 'typed_nil', 'pointer_error', 'named_error', 'slice_error', 'macro_imported_helper']
    names += [n for group in groups.values() for n in group if not n.startswith('reject_')]
    require(set(names) <= set(legacy['fixtures']), 'current-only fixture entered baseline comparison')
    sources = [('example/' + name, examples[name]['source']) for name in legacy['examples']]
    sources += [('check/' + name, (root / 'website/checks' / (name + '.gox')).read_text()) for name in names]
    require(len(sources) == 33 and len({name for name, _ in sources}) == 33,
            'incomplete frozen successful-Go comparison matrix')
    return sources


def position(source, anchor, offset=0, occurrence=None):
    positions = [m.start() for m in re.finditer(re.escape(anchor), source)]
    require(positions and (occurrence is not None or len(positions) == 1),
            'ambiguous or missing source anchor: ' + anchor)
    prefix = source[:positions[0 if occurrence is None else occurrence] + offset]
    return len(prefix.encode()), prefix.count('\n') + 1, len(prefix.rsplit('\n', 1)[-1].encode()) + 1


def assert_origin_diagnostic(error, case, source):
    primary, *frames = error.splitlines()
    require(case['message'] in primary, 'wrong diagnostic: ' + error)
    if case['point']:
        point = case['point']
        start, line, column = position(source, point['anchor'], point['offset'])
        require(primary.startswith(f'playground.gox:{line}:{column}:') and
                primary.endswith(f"(source span [{start},{start + len(point['token'].encode())}))") and
                'generated/unmapped' not in primary, 'inaccurate original diagnostic point: ' + error)
    else:
        require(primary.endswith('[generated/unmapped]') and 'source span' not in primary,
                'synthetic point claimed an origin: ' + error)
    expected = []
    for frame in case['frames']:
        _, dl, dc = position(source, frame['definition'])
        _, cl, cc = position(source, frame['call'], occurrence=frame.get('callOccurrence'))
        expected.append(f"  macro {frame['macro']} defined at playground.gox:{dl}:{dc}; invoked at playground.gox:{cl}:{cc}")
    require(frames == expected, 'inaccurate or reordered macro stack: ' + error)


class Runner:
    def __init__(self, root, out, args):
        self.root, self.out, self.args = root, out, args
        self.current, self.baseline = out / 'current', out / 'baseline'
        self.env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='',
                        GOPROXY='off', GONOPROXY='none', GOMAXPROCS='4', GIT_OPTIONAL_LOCKS='0',
                        PYTHONDONTWRITEBYTECODE='1', PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD='1')
        self.commands = []
        self.phases = {name: {'status': 'pending'} for name in PHASES}
        self.identity = {}
        self.current_phase = 'preflight'
        self.server = None
        self.server_command = None
        self.started = now()

    def command(self, name, argv, cwd=None, expected=(0,)):
        argv = [str(v) for v in argv]
        logdir = self.out / 'logs' / self.current_phase
        logdir.mkdir(parents=True, exist_ok=True)
        prefix = logdir / ('%04d-' % len(self.commands) + re.sub(r'[^\w.-]+', '-', name))
        record = {'name': name, 'phase': self.current_phase, 'command': argv,
                  'cwd': str(cwd or self.current), 'startedAt': now(),
                  'stdout': str(prefix.with_suffix('.stdout').relative_to(self.out)),
                  'stderr': str(prefix.with_suffix('.stderr').relative_to(self.out))}
        self.commands.append(record)
        try:
            with (self.out / record['stdout']).open('wb') as stdout, (self.out / record['stderr']).open('wb') as stderr:
                result = subprocess.run(argv, cwd=cwd or self.current, env=self.env,
                                        stdout=stdout, stderr=stderr)
            record['exit'] = result.returncode
        except BaseException as error:
            record.update(exit=None, error=str(error))
            raise
        finally:
            record['finishedAt'] = now()
            save(prefix.with_suffix('.json'), record)
            save(self.out / 'commands.json', self.commands)
        require(expected is None or result.returncode in expected,
                name + ' exited ' + str(result.returncode) + '; see ' + str(prefix))
        return result.returncode, (self.out / record['stdout']).read_bytes(), (self.out / record['stderr']).read_bytes()

    def read(self, name, argv, cwd=None):
        return self.command(name, argv, cwd)[1].decode().strip()

    def git(self, *argv, root=None):
        return self.read('git-' + argv[0], ['git', *argv], root or self.root)

    def inventory(self, root):
        paths = self.command('tracked-files', ['git', 'ls-files', '-z'], root)[1].decode().split('\0')
        result = []
        for name in paths:
            if not name:
                continue
            path = root / name
            require(path.is_file() and not path.is_symlink(), 'tracked input must be a regular file: ' + name)
            result.append({'path': name, 'bytes': path.stat().st_size, 'sha256': digest(path)})
        return result

    def preflight(self):
        require(sys.version_info >= (3, 11), 'website snapshots require Python 3.11+ (tomllib)')
        require_standalone_output(self.out.resolve())
        require(self.git('status', '--porcelain=v1', '--untracked-files=all') == '',
                'formal validation requires a clean committed checkout (including untracked files)')
        head = self.git('rev-parse', 'HEAD')
        tree = self.git('rev-parse', 'HEAD^{tree}')
        for sha in (head, COMPILER, BASELINE):
            self.git('cat-file', '-e', sha + '^{commit}')
        self.go, self.node = executable(self.args.go), executable(self.args.node)
        self.chromium = executable(self.args.chromium)
        self.env.update(GO_BIN=self.go, CHROMIUM=self.chromium)
        for key, value in (('GOCACHE', self.args.go_cache), ('GOMODCACHE', self.args.mod_cache)):
            if value:
                path = Path(value).expanduser().resolve()
                require(path.is_dir(), key + ' must be an existing cache directory')
                self.env[key] = str(path)
        (self.out / 'tmp').mkdir()
        self.env['TMPDIR'] = str(self.out / 'tmp')
        self.env['QA_URL'] = 'http://127.0.0.1:' + str(self.args.port)
        self.goenv = json.loads(self.read('go-env', [self.go, 'env', '-json', 'GOROOT', 'GOCACHE', 'GOMODCACHE', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'GOPROXY', 'GONOPROXY', 'GOSUMDB', 'GONOSUMDB', 'GOPRIVATE'], self.root))
        require(self.goenv['CGO_ENABLED'] == '1', 'race verification requires an existing CGO toolchain')
        versions = {'go': self.read('go-version', [self.go, 'version'], self.root),
                    'node': self.read('node-version', [self.node, '--version'], self.root),
                    'chromium': self.read('chromium-version', [self.chromium, '--version'], self.root),
                    'python': sys.version, 'git': self.git('--version')}
        require(re.search(r'go1\.27(?:\.|\s)', versions['go']), 'this acceptance requires an installed Go 1.27 toolchain')
        catalog = json.loads((self.root / 'website/catalog-source.json').read_text())
        require(catalog['compilerCommit'] == COMPILER and catalog['compilerDigest'] == COMPILER_DIGEST,
                'catalog must retain the separately reviewed compiler pin/digest')
        self.groups, self.diagnostics = qa_matrix(self.root)
        self.a04 = a04_matrix(self.root)
        self.a01 = a01_matrix(self.root)
        preserved = self.a01['preservedA04']
        for name, expected in preserved['files'].items():
            original = self.command('preserved-a04-source', ['git', 'show', preserved['sourceCommit'] + ':' + name], self.root)[1]
            require(hashlib.sha256(original).hexdigest() == expected,
                    'preserved A04 hash differs from declared source commit: ' + name)
        modules = Path(self.args.playwright_node_modules).expanduser().resolve()
        lock = json.loads((self.root / 'website/package-lock.json').read_text())
        installed = {}
        for name in ('playwright', 'playwright-core'):
            package = modules / name / 'package.json'
            require(package.is_file(), 'existing installed package required: ' + str(package))
            value = json.loads(package.read_text())
            require(value['version'] == lock['packages']['node_modules/' + name]['version'],
                    name + ' differs from the committed package lock')
            installed[name] = {'version': value['version'], 'packageSha256': digest(package),
                               'input': str(package.parent)}
        # Import the committed helper, with pycache disabled, to use exactly the
        # same closed inventory proof as the production build.
        spec = importlib.util.spec_from_file_location('website_build_support', self.root / 'website/build_support.py')
        support = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(support)
        actual_digest = support.verify_compiler_sources(self.root, COMPILER, COMPILER_DIGEST)
        require(actual_digest == COMPILER_DIGEST, 'unexpected production source digest')
        license_path = support.go_license_path(Path(self.goenv['GOROOT']))
        self.source_inputs = self.inventory(self.root)
        save(self.out / 'source-inputs.json', self.source_inputs)
        self.identity = {'validationCommit': head, 'validationTree': tree,
                         'compilerCommit': COMPILER, 'compilerDigest': actual_digest,
                         'baselineCommit': BASELINE, 'sourceCheckout': str(self.root),
                         'sourceStatusBefore': '', 'sourceInputs': 'source-inputs.json',
                         'sourceInputsSha256': digest(self.out / 'source-inputs.json'),
                         'toolVersions': versions, 'goEnvironment': self.goenv,
                         'executables': {name: {'path': path, 'sha256': digest(path)} for name, path in
                                         [('go', self.go), ('node', self.node), ('chromium', self.chromium)]},
                         'playwright': installed, 'packageLockSha256': digest(self.root / 'website/package-lock.json'),
                         'goLicense': {'path': str(license_path), 'sha256': digest(license_path)},
                         'environment': {key: self.env[key] for key in ('GOTOOLCHAIN', 'GOWORK', 'GOFLAGS', 'GOPROXY', 'GONOPROXY', 'TMPDIR')},
                         'playgroundInputModule': {'owner': None, 'outputAncestorsChecked': True,
                                                   'temporaryDirectory': self.env['TMPDIR']},
                         'matrix': {'fixtures': 117, 'legacyFixtures': 97, 'a04Fixtures': 8, 'a01Fixtures': 12,
                                    'examples': 11, 'legacyExamples': 10, 'a04Examples': 1,
                                    'nativeDiagnosticObservations': 52, 'a04NativeObservations': 32,
                                    'successfulGoComparisons': 33, 'pairedNativePrograms': 5,
                                    'a04CurrentNativePrograms': 5, 'a01NativeObservations': 48,
                                    'a01CurrentNativePrograms': 2, 'a01DiagnosticAnchors': 10},
                         'dependencyInstall': 'none; pre-existing cached Go dependencies and installed tools only',
                         'deployment': 'none'}
        save(self.out / 'identity.json', self.identity)
        for target, sha in ((self.current, head), (self.baseline, BASELINE)):
            # A local copy with independent objects needs no fetch and remains
            # usable after the original checkout moves. Only committed blobs
            # are materialized, never ignored/untracked files.
            self.command('clone-' + target.name, ['git', 'clone', '--no-hardlinks', '--no-checkout', str(self.root), str(target)], self.out)
            self.command('checkout-' + target.name, ['git', '-c', 'advice.detachedHead=false', 'checkout', '--detach', sha], target)
            require(self.git('rev-parse', 'HEAD', root=target) == sha, 'snapshot identity mismatch')
            require(self.git('status', '--porcelain=v1', '--untracked-files=all', root=target) == '', 'snapshot is not clean')
            inputs = self.inventory(target)
            if target == self.baseline:
                self.baseline_inputs = inputs
            save(self.out / (target.name + '-inputs.json'), inputs)
        require(self.inventory(self.current) == self.source_inputs, 'committed snapshot differs from clean source')
        for name in installed:
            # Copy the explicitly selected installation. No npm execution or
            # symlink back to a previous task's source/QA/compiler is permitted.
            shutil.copytree(modules / name, self.current / 'website/node_modules' / name)
        save(self.out / 'playwright-inputs.json', [
            {'path': str(path.relative_to(self.current)), 'sha256': digest(path)}
            for path in sorted((self.current / 'website/node_modules').rglob('*')) if path.is_file()])
        self.env['GOXIDE_BIN'] = str(self.current / 'bin/goxide')
        self.identity['snapshotInputs'] = {name: digest(self.out / (name + '-inputs.json')) for name in ('current', 'baseline', 'playwright')}
        save(self.out / 'identity.json', self.identity)

    def phase(self, name, action, dependencies=()):
        self.current_phase = name
        missing = [p for p in dependencies if self.phases[p]['status'] != 'passed']
        if missing:
            self.phases[name] = {'status': 'blocked', 'dependencies': missing}
            self.summarize()
            return
        self.phases[name] = {'status': 'running', 'startedAt': now()}
        self.summarize()
        print(name + ': running', flush=True)
        try:
            action()
        except BaseException as error:
            log = self.out / 'logs' / name / 'failure.txt'
            log.parent.mkdir(parents=True, exist_ok=True)
            log.write_text(traceback.format_exc())
            self.phases[name].update(status='failed', error=str(error), errorLog=str(log.relative_to(self.out)))
            if isinstance(error, (KeyboardInterrupt, SystemExit)):
                raise
        else:
            self.phases[name]['status'] = 'passed'
        finally:
            self.phases[name]['finishedAt'] = now()
            self.summarize()
            print(name + ': ' + self.phases[name]['status'], flush=True)

    def wasm_build(self):
        self.command('build-support-tests', [sys.executable, '-B', 'website/build_support_test.py'])
        self.command('build-website', [sys.executable, '-B', 'website/build.py'])
        manifest = json.loads((self.current / 'website/dist/examples.json').read_text())
        require(manifest['commit'] == COMPILER and manifest['websiteCommit'] == self.identity['validationCommit'], 'manifest mixes compiler/tool identities')
        require(manifest['compilerDigest'] == COMPILER_DIGEST, 'manifest source digest differs')
        require(manifest['wasmSha256'] == digest(self.current / 'website/dist/compiler.wasm'), 'WASM content hash differs')
        require(len(manifest['examples']) == 11, 'missing legacy/A04 built examples')
        for path in (self.current / 'website/dist').iterdir():
            if path.is_file():
                require(path.read_bytes() == (self.current / 'cmd/goxide-web/dist' / path.name).read_bytes(), 'web embedded asset differs: ' + path.name)
        self.manifest = manifest
        save(self.out / 'built-manifest.json', manifest)
        self.identity['build'] = {key: manifest[key] for key in ('commit', 'websiteCommit', 'compilerDigest', 'wasmSha256', 'exportsDigest', 'exportTarget', 'toolchain', 'publicLibraries', 'metadataSchema', 'abiVersions')}
        self.identity['build']['wasmBytes'] = (self.current / 'website/dist/compiler.wasm').stat().st_size
        save(self.out / 'identity.json', self.identity)

    def native(self):
        folder = self.out / 'native'
        folder.mkdir()
        baseline = self.out / 'baseline-goxide'
        cli = self.current / 'bin/goxide'
        oracle = self.out / 'compare-ast'
        self.command('baseline-build', [self.go, 'build', '-o', baseline, './cmd/goxide'], self.baseline)
        self.command('oracle-controls', [self.go, 'test', '-count=1', '-run', '^TestASTOracle', './website/validation'])
        self.command('oracle-build', [self.go, 'build', '-o', oracle, './website/validation'])
        binary_info = {}
        for name, path in (('baseline', baseline), ('current', cli), ('oracle', oracle)):
            binary_info[name] = {'sha256': digest(path), 'buildInfo': self.read(name + '-build-info', [self.go, 'version', '-m', path])}
        require('vcs.revision=' + BASELINE in binary_info['baseline']['buildInfo'], 'baseline executable lacks exact VCS revision')
        require('vcs.modified=false' in binary_info['baseline']['buildInfo'], 'baseline build used modified source')
        require('vcs.revision=' + self.identity['validationCommit'] in binary_info['current']['buildInfo'], 'current executable lacks tool/source checkout identity')
        save(folder / 'binary-identities.json', binary_info)
        input_file = folder / 'playground.gox'
        records = []
        save(folder / 'origin-diagnostics.json', records)
        for case in self.diagnostics:
            fixture = (self.current / 'website/checks' / (case['name'] + '.gox')).read_text()
            for ending, source in [('LF', fixture), ('CRLF', fixture.replace('\n', '\r\n'))]:
                input_file.write_bytes(source.encode())
                previous = None
                for repeat in range(2):
                    code, stdout, stderr = self.command(case['name'] + '-' + ending + '-' + str(repeat), [cli, 'transpile', input_file], expected=None)
                    error = stderr.decode().strip().replace(str(input_file), 'playground.gox')
                    record = {'name': case['name'], 'ending': ending, 'repeat': repeat, 'exit': code, 'sourceSha256': hashlib.sha256(source.encode()).hexdigest(), 'diagnostic': error}
                    records.append(record)
                    save(folder / 'origin-diagnostics.json', records)
                    require(code != 0 and not stdout, 'diagnostic input unexpectedly succeeded: ' + case['name'])
                    assert_origin_diagnostic(error, case, source)
                    require(repeat == 0 or error == previous, 'non-deterministic diagnostic: ' + error)
                    previous = error
        require(len(records) == 52, 'incomplete native diagnostic observations')
        sources = legacy_success_sources(self.current, self.manifest, self.groups)
        comparisons = []
        save(folder / 'successful-go-parity.json', comparisons)
        for name, source in sources:
            input_file.write_bytes(source.encode())
            outputs, files = [], []
            for label, binary in [('baseline', baseline), ('current', cli)]:
                _, data, _ = self.command('transpile-' + label + '-' + name, [binary, 'transpile', input_file])
                path = folder / 'successful-go' / label / (name + '.go')
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
                outputs.append(data)
                files.append(path)
            equal = outputs[0] == outputs[1]
            diff_path = None
            if not equal:
                path = folder / 'go-byte-diffs' / (name + '.diff')
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(''.join(difflib.unified_diff(outputs[0].decode().splitlines(True), outputs[1].decode().splitlines(True), fromfile='baseline/' + name, tofile='current/' + name)))
                diff_path = str(path.relative_to(self.out))
            code, proof, _ = self.command('ast-' + name, [oracle, *files], expected=None)
            proof_path = folder / 'ast-proof' / (name + '.json')
            proof_path.parent.mkdir(parents=True, exist_ok=True)
            proof_path.write_bytes(proof)
            entry = {'name': name, 'sourceSha256': hashlib.sha256(source.encode()).hexdigest(),
                     'baselineGoSha256': digest(files[0]), 'currentGoSha256': digest(files[1]),
                     'baselineBytes': len(outputs[0]), 'currentBytes': len(outputs[1]),
                     'byteEqual': equal, 'astExit': code, 'astEqual': code == 0,
                     'astProof': str(proof_path.relative_to(self.out)), 'diff': diff_path}
            comparisons.append(entry)
            save(folder / 'successful-go-parity.json', comparisons)
            require(code == 0 and json.loads(proof)['identical'], 'AST semantics differ: ' + name)
        runtime_names = ['macro_append_question', 'macro_copy_question', 'macro_allocation_question'] + self.groups['astPositiveChecks']
        runtime_sources = [(name, (self.current / 'website/checks' / (name + '.gox')).read_bytes()) for name in runtime_names]
        runtime_sources.append(('example/result', next(v['source'].encode() for v in self.manifest['examples'] if v['id'] == 'result')))
        require(len(runtime_sources) == 5, 'incomplete paired-native matrix')
        runtime = []
        save(folder / 'runtime-parity.json', runtime)
        for name, source in runtime_sources:
            input_file.write_bytes(source)
            observations = []
            runtime.append({'name': name, 'sourceSha256': hashlib.sha256(source).hexdigest(), 'observations': observations})
            for label, binary in [('baseline', baseline), ('current', cli)]:
                code, stdout, stderr = self.command('runtime-' + label + '-' + name, [binary, 'run', '--go', self.go, input_file], expected=None)
                observations.append({'compiler': label, 'exit': code, 'stdout': stdout.decode(), 'stderr': stderr.decode()})
                save(folder / 'runtime-parity.json', runtime)
                require(code == 0, 'native program failed: ' + name + '/' + label)
            require(observations[0]['stdout'] == observations[1]['stdout'] and observations[0]['stderr'] == observations[1]['stderr'], 'native behavior differs: ' + name)
        save(folder / 'summary.json', {'baselineCommit': BASELINE, 'compilerCommit': COMPILER,
             'validationCommit': self.identity['validationCommit'], 'diagnosticObservations': len(records),
             'byteEqualCount': sum(v['byteEqual'] for v in comparisons),
             'byteDifferentCount': sum(not v['byteEqual'] for v in comparisons),
             'astEqualCount': sum(v['astEqual'] for v in comparisons), 'pairedNativePrograms': len(runtime),
             'oracleSourceSha256': digest(self.current / 'website/validation/compare_ast.go'),
             'oracleTestSha256': digest(self.current / 'website/validation/compare_ast_test.go')})
        self.current_a04(cli)
        self.current_matrix(cli, 'a01', self.a01, 2)

    def current_a04(self, cli):
        self.current_matrix(cli, 'a04', self.a04, 5)

    def current_matrix(self, cli, label, matrix, expected_programs):
        """Exercise current-only inputs; retain each matrix's separate evidence."""
        folder = self.out / 'native' / label
        folder.mkdir(parents=True)
        input_file = folder / 'playground.gox'
        observations, runtime = [], []
        save(folder / 'observations.json', observations)
        save(folder / 'runtime.json', runtime)
        runtime_sources = []
        for case in matrix['checks']:
            fixture = (self.current / 'website/checks' / (case['name'] + '.gox')).read_text()
            for ending, source in [('LF', fixture), ('CRLF', fixture.replace('\n', '\r\n'))]:
                input_file.write_bytes(source.encode())
                previous = None
                for repeat in range(2):
                    code, stdout, stderr = self.command(label + '-' + case['name'] + '-' + ending + '-' + str(repeat),
                                                      [cli, 'transpile', input_file], expected=None)
                    error = stderr.decode().strip().replace(str(input_file), 'playground.gox')
                    observations.append({'name': case['name'], 'ending': ending, 'repeat': repeat,
                                         'sourceSha256': hashlib.sha256(source.encode()).hexdigest(),
                                         'expected': case['expect'], 'exit': code,
                                         'goSha256': hashlib.sha256(stdout).hexdigest(), 'diagnostic': error})
                    save(folder / 'observations.json', observations)
                    if case['expect'] == 'success':
                        require(code == 0 and stdout, 'current compiler rejected valid current-only input: ' + case['name'])
                    else:
                        require(code != 0 and not stdout and re.search(case['message'], error, re.IGNORECASE),
                                'wrong/missing current-only frontend rejection: ' + case['name'] + ': ' + error)
                        if case.get('point'):
                            assert_origin_diagnostic(error, case, source)
                    value = (code, stdout, error)
                    require(previous is None or value == previous, 'non-deterministic current-only observation: ' + case['name'])
                    previous = value
            if case['expect'] == 'success':
                runtime_sources.append(('check/' + case['name'], fixture, case['stdout']))
        catalog_examples = {entry['id']: entry for entry in
                            json.loads((self.current / 'website/catalog-source.json').read_text())['examples']}
        for name in matrix['examples']:
            runtime_sources.append(('example/' + name, (self.current / 'website/examples' / (name + '.gox')).read_text(),
                                    catalog_examples[name]['expectedStdout']))
        require(len(observations) == len(matrix['checks']) * 4 and len(runtime_sources) == expected_programs,
                'incomplete current-only matrix: ' + label)
        for name, source, expected_stdout in runtime_sources:
            input_file.write_bytes(source.encode())
            code, stdout, stderr = self.command(label + '-runtime-' + name, [cli, 'run', '--go', self.go, input_file], expected=None)
            runtime.append({'name': name, 'compilerCommit': COMPILER,
                            'sourceSha256': hashlib.sha256(source.encode()).hexdigest(), 'exit': code,
                            'stdout': stdout.decode(), 'stderr': stderr.decode(), 'expectedStdout': expected_stdout})
            save(folder / 'runtime.json', runtime)
            require(code == 0 and stdout.decode() == expected_stdout and stderr == b'',
                    'current-only native behavior differs: ' + name)
        save(folder / 'summary.json', {'compilerCommit': COMPILER, 'validationCommit': self.identity['validationCommit'],
                                      'currentOnly': True, 'baselineCompared': False,
                                      'observations': len(observations), 'nativePrograms': len(runtime)})

    def start_server(self):
        # Do not accidentally validate someone else's existing service.
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', self.args.port))
        logdir = self.out / 'logs/server'
        logdir.mkdir(parents=True, exist_ok=True)
        argv = [str(self.current / 'bin/goxide-web'), '-listen', '127.0.0.1:' + str(self.args.port)]
        self.server_command = {'command': argv, 'startedAt': now(), 'log': 'logs/server/server.log'}
        save(logdir / 'command.json', self.server_command)
        with (logdir / 'server.log').open('wb') as log:
            self.server = subprocess.Popen(argv, cwd=self.current, env=self.env, stdout=log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 15
        while True:
            require(self.server.poll() is None, 'QA server exited before readiness')
            try:
                with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(self.env['QA_URL'] + '/examples.json', timeout=1) as response:
                    value = json.load(response)
                require(value == self.manifest, 'QA service did not serve this run\'s exact manifest')
                return
            except (OSError, TimeoutError):
                if time.monotonic() >= deadline:
                    raise
                time.sleep(.1)

    def browser(self, script):
        if script == 'qa':
            self.command('worker-observation-tests', [self.node, 'website/validation/worker-observations_test.mjs'])
        if self.server is None:
            self.start_server()
        _, stdout, _ = self.command(script, [self.node, 'website/' + script + '.mjs'])
        value, _ = json.JSONDecoder().raw_decode(stdout.decode())
        require(value['examples'] == len(self.manifest['examples']) and value['errors'] == [],
                'browser result is incomplete')
        if script == 'qa':
            require(value['originChecks']['fixtures'] == 8 and value['astChecks']['fixtures'] == 5, 'browser diagnostic matrix changed')
            require(value['originChecks']['cliObservations'] + value['astChecks']['cliObservations'] == 52, 'browser native observations missing')
            require(value['originChecks']['uiLfObservations'] + value['astChecks']['uiLfObservations'] == 26, 'UI LF observations missing')
            require(value['originChecks']['rawWorkerObservations'] + value['astChecks']['rawWorkerObservations'] == 52, 'raw Worker LF/CRLF observations missing')
            a04 = value['a04Checks']
            require(a04['fixtures'] == 8 and len(a04['positive']) == len(a04['frontendReject']) == 4,
                    'A04 browser fixture matrix changed')
            require(a04['cliObservations'] == 32 and a04['uiLfObservations'] == 16 and
                    a04['rawWorkerObservations'] == 32 and a04['recoveryAfterEach'], 'A04 parity/recovery observations missing')
            require(a04['currentOnlyExamples'] == ['match_composition'], 'A04 example missing')
            a01 = value['a01Checks']
            require(a01['fixtures'] == 12 and a01['diagnosticAnchorChecks'] == 10 and
                    set(a01['positive']) == {c['name'] for c in self.a01['checks'] if c['expect'] == 'success'} and
                    set(a01['frontendReject']) == {c['name'] for c in self.a01['checks'] if c['expect'] == 'frontend-reject'},
                    'A01 browser fixture/diagnostic matrix changed')
            require(a01['cliObservations'] == 48 and a01['uiLfObservations'] == 24 and
                    a01['rawWorkerObservations'] == 48 and a01['recoveryAfterEach'] and
                    a01['currentOnlyExamples'] == [], 'A01 parity/recovery observations missing')
        else:
            require(value['cards'] == 26 and value['negativeExamples'] == 10 and value['consoleErrors'] == [], 'catalog matrix changed')
        save(self.out / (script + '-result.json'), value)

    def stop_server(self):
        if self.server is not None:
            self.server.terminate()
            try:
                self.server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.server.kill()
                self.server.wait()
            self.server_command.update(finishedAt=now(), exit=self.server.returncode, stoppedByRunner=True)
            save(self.out / 'logs/server/command.json', self.server_command)
            self.server = None

    def summarize(self, error=None, final=False):
        passed = final and error is None and all(v['status'] == 'passed' for v in self.phases.values())
        value = {'startedAt': self.started, 'updatedAt': now(), 'passed': passed,
                 'complete': final, 'phases': self.phases, 'identity': self.identity,
                 'failure': error, 'commands': 'commands.json',
                 'boundaries': {'cliWasm': 'same-current-compiler byte equality, separately from old/new AST comparison',
                                'nativeTypeRejects': 'final Go type checking is native, not WASM',
                                'lineEndings': 'UI LF; raw real Worker LF/CRLF',
                                'nativeRuntime': 'five legacy paired programs, not all 33 outputs; five A04 and two A01 current-only programs',
                                'macroCategories': 'A01 explicit template type slots only; opaque expression controls, no general type fragment or ambiguous conversion/single-argument generic claim',
                                'multiFileAndEffectFrame': 'native acceptance evidence is separate from single-file browser QA',
                                'deployment': 'none', 'hostedCI': 'not run by this command'}}
        if final:
            value['evidence'] = [{'path': str(p.relative_to(self.out)), 'bytes': p.stat().st_size, 'sha256': digest(p)}
                                 for directory in ('logs', 'native', 'current/website/qa-artifacts')
                                 for p in sorted((self.out / directory).rglob('*')) if p.is_file()]
        save(self.out / 'summary.json', value)
        return passed

    def run(self):
        error = None
        try:
            self.preflight()
            self.phase('wasm-build', self.wasm_build)
            self.phase('web-build', lambda: self.command('build-web', [self.go, 'build', '-tags', 'goxideweb', '-trimpath', '-o', 'bin/goxide-web', './cmd/goxide-web']), ('wasm-build',))
            self.phase('native-parity', self.native, ('wasm-build',))
            self.phase('web-race', lambda: self.command('web-race', [self.go, 'test', '-race', '-tags', 'goxideweb', '-count=1', './cmd/goxide-web', './internal/typecheck']), ('wasm-build',))
            self.phase('web-vet', lambda: self.command('web-vet', [self.go, 'vet', '-tags', 'goxideweb', './cmd/goxide-web', './internal/typecheck']), ('wasm-build',))
            self.phase('browser-qa', lambda: self.browser('qa'), ('wasm-build', 'web-build'))
            self.phase('catalog-qa', lambda: self.browser('catalog-qa'), ('wasm-build', 'web-build'))
        except BaseException as exc:
            error = str(exc) or type(exc).__name__
            (self.out / 'failure.txt').write_text(traceback.format_exc())
        finally:
            try:
                self.stop_server()
            except BaseException as exc:
                error = (error + '; ' if error else '') + 'server cleanup: ' + str(exc)
            for value in self.phases.values():
                if value['status'] in ('pending', 'running'):
                    value.update(status='blocked', reason='run terminated; inspect failure.txt')
            try:
                self.current_phase = 'final-identity'
                if self.identity:
                    status = self.git('status', '--porcelain=v1', '--untracked-files=all')
                    self.identity['sourceStatusAfter'] = status
                    self.identity['sourceHeadAfter'] = self.git('rev-parse', 'HEAD')
                    require(status == '' and self.identity['sourceHeadAfter'] == self.identity['validationCommit'] and self.inventory(self.root) == self.source_inputs,
                            'source checkout changed during validation')
                    self.identity['snapshotStatusAfter'] = self.git('status', '--porcelain=v1', '--untracked-files=all', root=self.current)
                    self.identity['snapshotHeadAfter'] = self.git('rev-parse', 'HEAD', root=self.current)
                    require(self.identity['snapshotStatusAfter'] == '' and self.identity['snapshotHeadAfter'] == self.identity['validationCommit'] and self.inventory(self.current) == self.source_inputs, 'validation altered snapshot HEAD or tracked inputs')
                    self.identity['baselineStatusAfter'] = self.git('status', '--porcelain=v1', '--untracked-files=all', root=self.baseline)
                    self.identity['baselineHeadAfter'] = self.git('rev-parse', 'HEAD', root=self.baseline)
                    require(self.identity['baselineStatusAfter'] == '' and self.identity['baselineHeadAfter'] == BASELINE and self.inventory(self.baseline) == self.baseline_inputs, 'validation altered baseline snapshot inputs')
            except BaseException as exc:
                error = (error + '; ' if error else '') + str(exc)
            finally:
                if self.identity:
                    # Preserve the observed mismatched identity as well as the
                    # successful one; failure must not leave only initial data.
                    save(self.out / 'identity.json', self.identity)
            passed = self.summarize(error, final=True)
        print(('PASS' if passed else 'FAIL') + ': ' + str(self.out / 'summary.json'), flush=True)
        return 0 if passed else 1


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', required=True, help='new directory outside the source checkout; parent must exist')
    parser.add_argument('--chromium', required=True, help='existing Chromium executable; never installed/downloaded')
    parser.add_argument('--playwright-node-modules', required=True, help='existing node_modules containing locked playwright and playwright-core')
    parser.add_argument('--go', default='go', help='existing Go 1.27 executable')
    parser.add_argument('--node', default='node', help='existing Node.js executable')
    parser.add_argument('--go-cache', help='existing GOCACHE override; otherwise inherits go env')
    parser.add_argument('--mod-cache', help='existing complete GOMODCACHE override; never downloads missing modules')
    parser.add_argument('--port', type=int, default=18084, help='unused loopback port for this run')
    args = parser.parse_args(argv)
    if not 1024 <= args.port <= 65535:
        parser.error('--port must be between 1024 and 65535')
    return args


def main(argv=None):
    args = arguments(argv)
    try:
        out = reserve_output(args.out, ROOT)
    except RuntimeError as error:
        print(str(error), file=sys.stderr)
        return 2
    save(out / 'request.json', vars(args))
    return Runner(ROOT, out, args).run()


if __name__ == '__main__':
    sys.exit(main())
