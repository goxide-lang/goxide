#!/usr/bin/env python3
"""Current-compiler CLI/Node GoWASM and optional sandboxed HTTP Worker parity.

This gate deliberately has no historical baseline AST comparison or full UI QA.
Build matching assets with website/build.py first. See README.md for prerequisites.
"""
import argparse
from functools import partial
import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import importlib.metadata
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import threading

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'website'))
from build_support import verify_compiler_sources


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def save(path, data):
    Path(path).write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')


def reserve_output(path, root):
    require(not os.path.lexists(path), '--out must name a fresh directory')
    path = path.resolve()
    require(not path.is_relative_to(root.resolve()), '--out must be outside checkout')
    for parent in (path, *path.parents):
        require(not os.path.lexists(parent / 'go.mod'), 'standalone CLI inputs require module-free output ancestors: ' + str(parent))
    path.mkdir(parents=True)
    return path


def compare_result(expected, actual):
    require(actual.get('id') == expected['id'], 'response identity differs: ' + expected['id'])
    field = 'go' if expected['exit'] == 0 else 'error'
    require(field in actual and ('error' if field == 'go' else 'go') not in actual,
            'response kind differs: ' + expected['id'])
    require(actual[field] == expected[field], 'CLI/WASM bytes differ: ' + expected['id'])


def load_cases(root, manifest, extra):
    catalog = json.loads((root / 'website/catalog-source.json').read_text())
    require(catalog['compilerCommit'] == manifest['commit'] and catalog['compilerDigest'] == manifest['compilerDigest'], 'catalog compiler identity differs from manifest')
    expected = {entry['id']: entry for entry in catalog['examples']}
    require(set(expected) == {path.stem for path in (root / 'website/examples').glob('*.gox')}, 'catalog example membership differs')
    require(len(manifest['examples']) == len(expected) and {entry['id'] for entry in manifest['examples']} == set(expected), 'manifest example membership differs')
    cases = []
    for entry in manifest['examples']:
        name = entry['id']
        source = (root / 'website/examples' / (name + '.gox')).read_bytes().decode()
        require(source == entry['source'], 'manifest example source differs: ' + name)
        require(entry['stdout'] == expected[name]['expectedStdout'], 'manifest example stdout differs: ' + name)
        cases.append(dict(id='website-' + name, source=source, expectedStdout=expected[name]['expectedStdout']))
    for path in extra:
        for case in json.loads(path.read_text()):
            case = dict(case)
            require(('source' in case) != ('sourcePath' in case), 'case needs exactly one source or sourcePath')
            if 'sourcePath' in case:
                case['source'] = (path.parent / case.pop('sourcePath')).read_bytes().decode()
            cases.append(case)
    require(len({case['id'] for case in cases}) == len(cases), 'duplicate case identity')
    for case in cases:
        require(re.fullmatch(r'[a-zA-Z0-9_-]+', case['id']), 'unsafe case identity')
        require(isinstance(case['source'], str), 'case source must be text')
        if case.get('expect') == 'frontend-reject':
            require(bool(case.get('diagnosticContains')), 'rejection needs diagnosticContains')
            require('expectedStdout' not in case, 'frontend rejection cannot have native stdout')
        else:
            require(case.get('expect', 'success') == 'success' and isinstance(case.get('expectedStdout'), str), 'success needs independent expectedStdout')
    return cases


class WorkerHandler(SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/focused-validation.html':
            body = b'<!doctype html><title>Focused Worker validation</title>'
            self.send_response(200)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            super().do_GET()

    def log_message(self, *_):
        pass


def http_worker(root, dist, inputs, expected, manifest, out, chromium):
    from playwright.sync_api import sync_playwright
    # Reuse the existing real-Worker protocol collector and its failure evidence.
    collector = (root / 'website/validation/worker-observations.mjs').read_text()
    collector = collector.split('export async function collectWorkerObservations', 1)[1]
    collector = '(async function collectWorkerObservations' + collector.split('\n\n// Persist', 1)[0] + ')'
    server = ThreadingHTTPServer(('127.0.0.1', 0), partial(WorkerHandler, directory=str(dist)))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    origin = 'http://127.0.0.1:' + str(server.server_port)
    requests = []
    try:
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(executable_path=chromium, headless=True, chromium_sandbox=True)
            try:
                page = browser.new_page()
                page.on('request', lambda request: requests.append(request.url))
                page.goto(origin + '/focused-validation.html')
                served = page.evaluate("async () => await (await fetch('examples.json')).json()")
                require(served == manifest, 'HTTP manifest differs from validated local manifest')
                try:
                    batch = page.evaluate(collector, dict(inputs=inputs, commit=served['commit'], hash=served['wasmSha256']))
                except Exception as error:
                    batch = dict(complete=False, observations=[], failure=dict(stage='page-evaluate', error=str(error)))
                save(out / 'http-worker.json', batch)
                require(batch['complete'] and not batch['failure'], 'HTTP Worker protocol failed; see http-worker.json')
                observations = batch['observations']
                require(len(observations) == len(expected), 'HTTP Worker response count differs')
                for index, (wanted, observation) in enumerate(zip(expected, observations)):
                    result = observation['result']
                    require(result.get('type') == 'result' and result.get('id') == index, 'HTTP Worker response sequence differs')
                    compare_result(wanted, dict(result, id=observation['input']['id']))
                require(all(url.startswith(origin + '/') for url in requests), 'unexpected external browser request')
                return dict(status='passed', observations=len(observations), chromium=browser.version,
                            playwright=importlib.metadata.version('playwright'), sandbox=True, requests=requests)
            finally:
                browser.close()
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


def run(args, out, summary):
    root, dist = ROOT, ROOT / 'website/dist'
    go = args.go
    native_env = dict(os.environ, GOMAXPROCS='4', GOWORK='off', GOPROXY='off', GOTOOLCHAIN='local')
    manifest = json.loads((dist / 'examples.json').read_text())
    verify_compiler_sources(root, manifest['commit'], manifest['compilerDigest'])
    require(digest(dist / 'compiler.wasm') == manifest['wasmSha256'], 'WASM manifest hash differs')
    require(subprocess.check_output(['git', 'status', '--porcelain=v1', '--untracked-files=all'], cwd=root) == b'', 'focused acceptance requires a clean committed checkout')
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root).decode().strip()
    require(head == manifest['websiteCommit'], 'website assets were built from another commit')
    cli = root / 'bin/goxide'
    build_info = subprocess.check_output([go, 'version', '-m', cli], cwd=root, env=native_env).decode()
    require('vcs.revision=' + head in build_info and 'vcs.modified=false' in build_info, 'CLI was not built from this clean commit')
    cases = load_cases(root, manifest, args.cases)
    identity_paths = [cli, dist / 'compiler.wasm', dist / 'wasm_exec.js', dist / 'worker.js', dist / 'examples.json']
    before = {str(path): digest(path) for path in identity_paths}
    summary.update(compilerCommit=manifest['commit'], compilerDigest=manifest['compilerDigest'], websiteCommit=head,
                   toolchain=subprocess.check_output([go, 'version'], env=native_env).decode().strip(),
                   node=subprocess.check_output([args.node, '--version']).decode().strip(),
                   artifacts=before, exportTarget=manifest['exportTarget'], publicLibraries=manifest['publicLibraries'],
                   examples=len(manifest['examples']), cases=len(cases))
    inputs, native_results, cli_results = [], [], []

    def command(argv, name, env=native_env, timeout=120):
        try:
            result = subprocess.run(argv, cwd=root, env=env, capture_output=True, timeout=timeout)
        except subprocess.TimeoutExpired as error:
            (out / (name + '.stdout')).write_bytes(error.stdout or b'')
            (out / (name + '.stderr')).write_bytes(error.stderr or b'')
            raise
        (out / (name + '.stdout')).write_bytes(result.stdout)
        (out / (name + '.stderr')).write_bytes(result.stderr)
        return result

    for case in cases:
        for ending in ['LF', 'CRLF']:
            name = case['id'] + '-' + ending
            folder = out / name
            folder.mkdir()
            source = case['source'].replace('\r\n', '\n')
            if ending == 'CRLF':
                source = source.replace('\n', '\r\n')
            input_path = folder / 'playground.gox'
            input_path.write_bytes(source.encode())
            result = command([str(cli), 'transpile', str(input_path)], name)
            observation = dict(id=name, sourceSha256=digest(input_path), exit=result.returncode)
            if result.returncode:
                observation['error'] = result.stderr.decode().strip().replace(str(input_path), 'playground.gox')
            else:
                observation['go'] = result.stdout.decode()
            cli_results.append(observation)
            save(out / 'cli-results.json', cli_results)
            if case.get('expect') == 'frontend-reject':
                require(result.returncode != 0 and not result.stdout and case['diagnosticContains'] in observation['error'], 'missing expected frontend rejection: ' + name)
            else:
                require(result.returncode == 0, 'CLI transpile failed: ' + name)
                if ending == 'LF':
                    result = command([str(cli), 'run', '--go', go, str(input_path)], name + '-native')
                    native_results.append(dict(id=case['id'], exit=result.returncode, stdout=result.stdout.decode(), stderr=result.stderr.decode()))
                    save(out / 'native-results.json', native_results)
                    require(result.returncode == 0 and result.stdout.decode() == case['expectedStdout'] and result.stderr.decode() == case.get('expectedStderr', ''), 'native behavior differs: ' + name)
            inputs.append(dict(id=name, source=source))
    save(out / 'inputs.json', inputs)
    wasm = command([args.node, str(Path(__file__).with_name('focused-wasm.cjs')), str(dist / 'compiler.wasm'), str(dist / 'wasm_exec.js'), str(out / 'inputs.json'), str(out / 'node-wasm.json')], 'node-wasm', env=dict(native_env, GOMAXPROCS='1'), timeout=180)
    require(wasm.returncode == 0, 'Node GoWASM failed; see node-wasm.stderr')
    wasm_results = json.loads((out / 'node-wasm.json').read_text())
    require(wasm_results['complete'] and len(wasm_results['outputs']) == len(cli_results), 'Node GoWASM response count differs')
    for expected, actual in zip(cli_results, wasm_results['outputs']):
        compare_result(expected, actual)
    summary.update(parityObservations=len(inputs), nativeExecutions=len(native_results), nodeWasm='passed')
    if args.chromium:
        try:
            summary['httpWorker'] = http_worker(root, dist, inputs, cli_results, manifest, out, args.chromium)
        except Exception as error:
            summary['httpWorker'] = dict(status='failed', failure=str(error), sandbox=True)
            raise
    verify_compiler_sources(root, manifest['commit'], manifest['compilerDigest'])
    require(before == {str(path): digest(path) for path in identity_paths}, 'validated assets changed during run')
    require(subprocess.check_output(['git', 'status', '--porcelain=v1', '--untracked-files=all'], cwd=root) == b'', 'source checkout changed during run')
    summary['passed'] = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--cases', type=Path, action='append', default=[])
    parser.add_argument('--go', default=os.environ.get('GO_BIN', 'go'))
    parser.add_argument('--node', default='node')
    parser.add_argument('--chromium', help='enable real HTTP Worker checks with this existing Chromium; its sandbox remains enabled')
    args = parser.parse_args()
    out = reserve_output(args.out, ROOT)
    summary = dict(passed=False, scope='current compiler LF/CRLF CLI/GoWASM parity and native behavior; no historical AST comparison or full browser UI QA', httpWorker=dict(status='not-run'))
    try:
        run(args, out, summary)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        save(out / 'summary.json', summary)
        print(json.dumps(summary, ensure_ascii=False, indent=2), flush=True)


if __name__ == '__main__':
    main()
