#!/usr/bin/env python3
"""Focused CLI/real sandboxed browser acceptance for the package playground.

Requires an already-built matching website and native CLI. Never builds, fetches,
installs, deploys, or disables Chromium's sandbox. Evidence stays in --out.
"""
import argparse
import hashlib
import json
import os
import re
import time
from pathlib import Path
import shutil
import subprocess
from urllib.parse import urljoin

from playwright.sync_api import sync_playwright


ROOT = Path(__file__).resolve().parent
WORKER_BATCH = """async ({inputs, manifest}) => {
  const worker = new Worker(`worker.js?compiler=${manifest.commit}&wasm=${manifest.wasmSha256}`);
  const response = () => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('package Worker timeout')), 60000);
    worker.onerror = e => { clearTimeout(timer); reject(new Error(e.message)); };
    worker.onmessage = ({data}) => { clearTimeout(timer); resolve(data); };
  });
  const observations = [];
  try {
    const ready = await response();
    if (ready.type !== 'ready') throw new Error(JSON.stringify(ready));
    for (let id = 0; id < inputs.length; id++) {
      const pending = response();
      worker.postMessage({type:'compile', id, ...inputs[id]});
      const result = await pending;
      observations.push({input:inputs[id], result});
      if (result.type !== 'result' || result.id !== id) throw new Error('Worker protocol mismatch');
    }
    return {complete:true, observations};
  } catch (error) { return {complete:false, observations, error:String(error)}; }
  finally { worker.terminate(); }
}"""

# Hold results from actual Workers; readiness, compilation, and termination remain
# real. Delivering the saved event later deterministically tests obsolete events.
HOLD_RESULTS = """(() => {
  const NativeWorker = window.Worker;
  const state = window.__packageQA = {hold:false, results:[], history:[]};
  window.Worker = class {
    constructor(...args) {
      this.actual = new NativeWorker(...args);
      this.actual.onmessage = event => {
        const deliver = () => this.onmessage?.(event);
        if (event.data.type === 'result') state.history.push({data:event.data, deliver});
        if (event.data.type === 'result' && state.hold) state.results.push({data:event.data, deliver});
        else deliver();
      };
      this.actual.onerror = event => this.onerror?.(event);
    }
    postMessage(...args) { this.actual.postMessage(...args); }
    terminate() { this.actual.terminate(); }
  };
})();"""


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def source_files(directory):
    return [dict(path=p.name, source=p.read_text()) for p in sorted(directory.glob('*.gox'))]


def native_package(args, out, files, label, expected_error=None):
    work = out / label
    work.mkdir()
    source = work / 'source'
    source.mkdir()
    shutil.copyfile(ROOT / 'example/go.mod', source / 'go.mod')
    for item in files:
        (source / item['path']).write_text(item['source'])
    generated = work / 'generated'
    env = dict(os.environ, GOMAXPROCS='4', GOPROXY='off', GOTOOLCHAIN='local', GOWORK='off')
    command = [args.cli, 'transpile', '--go', args.go, '-o', str(generated), str(source)]
    result = subprocess.run(command, env=env, text=True, capture_output=True)
    (work / 'transpile.json').write_text(json.dumps(dict(argv=command, exit=result.returncode,
        stdout=result.stdout, stderr=result.stderr), ensure_ascii=False, indent=2))
    if expected_error:
        check(result.returncode != 0, label + ': native rejection accepted')
        for text in expected_error:
            check(text in result.stderr, label + ': missing native diagnostic ' + text)
        return result.stderr.strip().replace(str(source) + '/', '')
    check(result.returncode == 0, label + ': ' + result.stderr)
    outputs = [dict(path=p.name, source=p.read_text()) for p in sorted(generated.glob('*.go'))]
    check(outputs, label + ': no native generated files')
    command = [args.cli, 'run', '--go', args.go, str(source)]
    result = subprocess.run(command, env=env, text=True, capture_output=True)
    (work / 'run.json').write_text(json.dumps(dict(argv=command, exit=result.returncode,
        stdout=result.stdout, stderr=result.stderr), ensure_ascii=False, indent=2))
    check(result.returncode == 0, label + ': native execution failed: ' + result.stderr)
    check(result.stdout == '42 GOXIDE Item { Value: 21 }\n', label + ': native stdout changed')
    return outputs


def done(page):
    page.locator('#status').filter(has_text=re.compile('^转译完成')).wait_for(timeout=60000)


def generated_files(page):
    result = []
    for button in page.locator('#generated-files button').all():
        name = button.inner_text()
        button.click()
        result.append(dict(path=name, source=page.input_value('#output')))
    return sorted(result, key=lambda item: item['path'])


def source_tab(page, name):
    page.locator('#source-files').get_by_role('button', name=name, exact=True).click()


def ui_checks(page, manifest, evidence):
    package_index = len(manifest['examples'])
    example = manifest['packageExamples'][0]
    page.select_option('#examples', str(package_index))
    done(page)
    check(generated_files(page) == example['generatedFiles'], 'package example generated tabs differ')
    helper = next((f for f in example['generatedFiles'] if f['path'].startswith('goxide_helpers_') and f['path'].endswith('.go')), None)
    check(helper, 'package example has no complete generated helper file')
    page.locator('#generated-files').get_by_role('button', name=helper['path'], exact=True).click()
    page.click('#copy')
    page.locator('#status').filter(has_text=re.compile('^已复制完整 Go 文件')).wait_for()
    check(page.evaluate('navigator.clipboard.readText()') == helper['source'], 'helper copy omitted generated content')
    for item in example['files']:
        source_tab(page, item['path'])
        check(page.input_value('#source') == item['source'], 'source tab lost ' + item['path'])
    source_tab(page, 'main.gox')
    edited = page.input_value('#source').replace('Value: 21', 'Value: 7')
    page.fill('#source', edited)
    source_tab(page, 'helpers.gox')
    source_tab(page, 'main.gox')
    check(page.input_value('#source') == edited, 'switching source tabs lost an edit')
    page.click('#compile')
    done(page)
    check(any('Value: 7' in f['source'] for f in generated_files(page)), 'edited source was not compiled')

    count = page.locator('#source-files button').count()
    for rejected_name in ['main.gox', '../escape.gox']:
        page.fill('#new-file-name', rejected_name)
        page.click('#add-file')
        check(page.locator('#file-error').is_visible(), 'invalid filename was accepted: ' + rejected_name)
        check(page.locator('#source-files button').count() == count, 'invalid add changed source files')

    page.fill('#new-file-name', 'extra.gox')
    page.click('#add-file')
    page.fill('#source', 'package main\nconst extra = 9\n')
    page.click('#compile')
    done(page)
    check(any(f['path'] == 'extra.go' and 'extra = 9' in f['source'] for f in generated_files(page)), 'added file missing')
    source_tab(page, 'extra.gox')
    page.click('#delete-file')
    page.click('#compile')
    done(page)
    check(all(f['path'] != 'extra.go' for f in generated_files(page)), 'deleted file remained in generated output')

    # Introduce a syntax error in a named file, then repair and compile twice.
    source_tab(page, 'helpers.gox')
    original = page.input_value('#source')
    page.fill('#source', original + '\nfunc broken(\n')
    page.click('#compile')
    page.locator('#diagnostic').wait_for(state='visible', timeout=60000)
    check('helpers.gox' in page.inner_text('#diagnostic'), 'diagnostic lost its source filename')
    check(page.input_value('#output') == '', 'failure retained previous generated code')
    page.fill('#source', original)
    page.click('#compile')
    done(page)
    recovered = generated_files(page)
    page.click('#compile')
    done(page)
    check(generated_files(page) == recovered, 'repeat compilation changed output')

    # A legacy example remains on its original single-file entry; old edit state
    # must disappear on selection, and adding/deleting keeps the new package mode.
    page.select_option('#examples', '0')
    done(page)
    check(page.input_value('#source') == manifest['examples'][0]['source'], 'example switch retained edited package')
    check(page.input_value('#output') == manifest['examples'][0]['go'], 'legacy example output changed')
    page.fill('#source', 'package main\nfunc main() {}\n')
    page.click('#compile')
    done(page)
    page.fill('#new-file-name', 'second.gox')
    page.click('#add-file')
    page.fill('#source', 'package main\nconst second = 2\n')
    page.click('#compile')
    done(page)
    page.click('#delete-file')
    page.click('#compile')
    done(page)
    check(len(page.locator('#source-files button').all()) == 1, 'delete back to one source failed')
    check(len(generated_files(page)) >= 1, 'one-file package lost output')
    check('同包' in page.inner_text('#input-mode'), 'delete silently restored legacy single-file semantics')

    # Adding a file follows the package clause, including Go Unicode identifiers.
    page.fill('#source', '/* package decoy */\npackage 示例\nfunc F() {}\n')
    page.click('#compile')
    done(page)
    page.fill('#new-file-name', 'unicode.gox')
    page.click('#add-file')
    check(page.input_value('#source') == 'package 示例\n', 'new file lost Unicode package name')
    page.click('#compile')
    done(page)

    # Read-only dependency tabs expose the exact artifact bytes and identities.
    dependencies = page.locator('#dependencies option')
    check(dependencies.count() > 0, 'dependency browser is empty')
    page.locator('#dependency-browser > summary').click()
    check(page.locator('#dependency-source').evaluate('(e) => e.readOnly'), 'dependency source is editable')
    for index, dependency in enumerate(manifest['dependencies']):
        page.select_option('#dependencies', str(index))
        page.select_option('#dependency-files', index=0)
        check(page.input_value('#dependency-source') == dependency['files'][0]['source'], 'dependency source differs from artifact')
        provenance = page.inner_text('#dependency-provenance')
        for value in [dependency['package'], dependency['version'], dependency['source'], dependency['files'][0]['path'], dependency['files'][0]['sha256']]:
            check(value in provenance, 'dependency identity missing from provenance: ' + value)
    page.select_option('#examples', str(package_index))
    done(page)
    evidence.append('source/output/helper tabs and complete clipboard copy; edit persistence; invalid/add/delete file names; error/recovery/repeat; legacy/package switches; dependency readonly/provenance')


def asset_checks(args, page, manifest, evidence):
    response = page.request.get(urljoin(args.url.rstrip('/') + '/', 'compiler.wasm'))
    check(response.ok, 'served WASM could not be read')
    check(hashlib.sha256(response.body()).hexdigest() == manifest['wasmSha256'], 'served WASM hash differs from manifest')
    for dependency in manifest['dependencies']:
        for file in dependency['files']:
            check(hashlib.sha256(file['source'].encode()).hexdigest() == file['sha256'], 'dependency source hash differs: ' + file['path'])
    native_env = dict(os.environ, GOMAXPROCS='4', GOPROXY='off', GOTOOLCHAIN='local', GOWORK='off')
    version = subprocess.check_output([args.go, 'version'], env=native_env, text=True).strip()
    check(version == manifest['toolchain'], 'manifest toolchain differs from actual native Go')
    for library in manifest['publicLibraries']:
        selected = json.loads(subprocess.check_output([args.go, 'list', '-m', '-json', library['module']],
            env=native_env, cwd=ROOT.parents[2], text=True))
        check(not selected.get('Replace'), 'local replacement is not a published dependency identity')
        check(selected['Version'] == library['version'], 'manifest library version differs from actual go.mod')
    evidence.append('served WASM SHA-256; every dependency source SHA-256; actual Go toolchain and go.mod library version identity')


def mobile_checks(browser, args, manifest, evidence):
    page = browser.new_page(viewport=dict(width=390, height=844), is_mobile=True, has_touch=True)
    try:
        page.goto(args.url)
        done(page)
        page.select_option('#examples', str(len(manifest['examples'])))
        done(page)
        source_tab(page, 'main.gox')
        check(page.input_value('#source'), 'mobile source selector is unusable')
        check(generated_files(page) == manifest['packageExamples'][0]['generatedFiles'], 'mobile generated tabs differ')
        page.locator('#dependency-browser > summary').click()
        page.select_option('#dependencies', index=0)
        page.select_option('#dependency-files', index=0)
        check(page.input_value('#dependency-source'), 'mobile dependency selectors are unusable')
        dimensions = page.evaluate('({width:document.documentElement.clientWidth, scroll:document.documentElement.scrollWidth})')
        check(dimensions['scroll'] <= dimensions['width'] + 1, 'mobile document overflows horizontally: ' + repr(dimensions))
        page.locator('#playground').scroll_into_view_if_needed()
        page.screenshot(path=str(args.out / 'package-playground-mobile.png'))
        evidence.append('390px mobile source/generated/dependency selectors and no document horizontal overflow')
    finally:
        page.close()


def stale_result_checks(browser, args, manifest, evidence):
    context = browser.new_context()
    context.add_init_script(HOLD_RESULTS)
    page = context.new_page()
    try:
        page.goto(args.url)
        done(page)
        page.fill('#source', 'package main\nfunc main() { println(111) }\n')
        page.click('#compile')
        done(page)
        current = generated_files(page)
        page.evaluate('window.__packageQA.history[0].deliver()')
        check(generated_files(page) == current, 'old id on current Worker overwrote latest source')
        page.evaluate('window.__packageQA.hold = true')
        page.fill('#source', 'package main\nfunc main() { println(222) }\n')
        page.click('#compile')
        deadline = time.monotonic() + 60
        while not page.evaluate('() => window.__packageQA.results.length === 1'):
            check(time.monotonic() < deadline, 'timed out waiting for held real Worker result')
            page.wait_for_timeout(50)
        page.evaluate('window.__packageQA.hold = false')
        page.select_option('#examples', str(len(manifest['examples'])))
        done(page)
        current = generated_files(page)
        page.evaluate('window.__packageQA.results.shift().deliver()')
        check(generated_files(page) == current, 'late legacy result overwrote package example')
        check(page.inner_text('#status').startswith('转译完成'), 'late result changed completed status')
        # A delayed clipboard failure from the previous result must not touch
        # the current output or compiler status after switching examples.
        page.evaluate('''Object.defineProperty(navigator, 'clipboard', {configurable:true,
          value:{writeText:() => new Promise((resolve, reject) => {window.__rejectCopy = reject;})}})''')
        page.click('#copy')
        page.select_option('#examples', '0')
        done(page)
        before = page.inner_text('#status')
        page.evaluate('async () => { window.__rejectCopy(new Error("denied")); await Promise.resolve(); }')
        check(page.inner_text('#status') == before, 'late clipboard failure overwrote compiler status')
        check(page.input_value('#output') == manifest['examples'][0]['go'], 'late clipboard failure touched output')
        evidence.append('real Worker saved events with old id and obsolete instance ignored after newer compilation/example completion')
    finally:
        context.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', required=True)
    parser.add_argument('--cli', required=True)
    parser.add_argument('--go', required=True)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--chromium', default='/usr/bin/chromium')
    args = parser.parse_args()
    args.cli, args.go = str(Path(args.cli).resolve()), str(Path(args.go).resolve())
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=False)
    evidence = dict(passed=False, browserSandbox=True, checks=[])
    browser = None
    try:
        files = source_files(ROOT / 'example')
        expected = native_package(args, args.out, files, 'native-lf')
        crlf = [dict(path=f['path'], source=f['source'].replace('\n', '\r\n')) for f in files]
        check(native_package(args, args.out, crlf, 'native-crlf') == expected, 'native CRLF output differs')
        conflicts = [dict(path=name, source='package main\npub enum Mode { Idle, }\n') for name in ['a.gox', 'b.gox']]
        conflict_diagnostic = native_package(args, args.out, conflicts, 'native-conflict', ['a.gox', 'b.gox', 'duplicate'])
        malformed = [dict(path='broken.gox', source='package main\nfunc broken(\n')]
        syntax_diagnostic = native_package(args, args.out, malformed, 'native-syntax', ['broken.gox'])
        evidence['checks'].append('native LF/CRLF package execution; expected stdout; named syntax/conflict diagnostics')
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(executable_path=args.chromium, headless=True, chromium_sandbox=True)
            page = browser.new_page(viewport=dict(width=1440, height=1050))
            page.context.grant_permissions(['clipboard-read', 'clipboard-write'], origin=args.url)
            page_errors = []
            page.on('pageerror', lambda error: page_errors.append(str(error)))
            page.goto(args.url)
            done(page)
            manifest = page.evaluate("async () => (await fetch('examples.json')).json()")
            check(manifest.get('packageExamples'), 'no built package example')
            # Compare served assets with this checkout's generated artifact bytes.
            # A live page alone cannot prove the page, Worker and runtime match.
            for asset in ['examples.json', 'app.js', 'worker.js', 'style.css', 'wasm_exec.js']:
                served = page.request.get(args.url.rstrip('/') + '/' + asset)
                check(served.ok, 'asset HTTP failure: ' + asset)
                expected_asset = (ROOT.parents[1] / 'dist' / asset).read_bytes()
                check(served.body() == expected_asset, 'served asset differs from build: ' + asset)
            evidence['checks'].append('served manifest/app/Worker/style/Go runtime bytes match local build')
            asset_checks(args, page, manifest, evidence['checks'])
            inputs, checks = [], []
            def add(payload, output=None, errors=None):
                inputs.append(payload)
                checks.append((output, errors))
            for _ in range(2):
                for variant in [files, crlf]:
                    add(dict(files=variant), output=expected)
                for invalid, errors in [(conflicts, conflict_diagnostic), (malformed, syntax_diagnostic),
                    ([], []), ([files[0], files[0]], ['duplicate']),
                    ([dict(path=p, source='package main\n') for p in ['a.go']], []),
                    ([dict(path='../escape.gox', source='package main\n')], []),
                    ([dict(path='nested/a.gox', source='package main\n')], []),
                    ([dict(path='large.gox', source='/' * 32769)], []),
                    ([dict(path=f'file{i}.gox', source='package main\n') for i in range(17)], [])]:
                    add(dict(files=invalid), errors=errors)
                    add(dict(files=files), output=expected)
            for example in manifest['examples']:
                add(dict(source=example['source']), output=example['go'])
            batch = page.evaluate(WORKER_BATCH, dict(inputs=inputs, manifest=manifest))
            (args.out / 'worker.json').write_text(json.dumps(batch, ensure_ascii=False, indent=2))
            check(batch['complete'], 'Worker collection failed: ' + batch.get('error', ''))
            check(len(batch['observations']) == len(checks), 'Worker observation count differs')
            for index, (observation, (output, errors)) in enumerate(zip(batch['observations'], checks)):
                result = observation['result']
                if errors is not None:
                    check(result.get('error'), f'Worker case {index}: expected error')
                    check(not result.get('files') and not result.get('go'), f'Worker case {index}: failed output leaked')
                    if isinstance(errors, str):
                        check(result['error'].strip() == errors, f'Worker case {index}: diagnostic differs from normalized CLI: {result["error"]!r} != {errors!r}')
                    else:
                        for text in errors:
                            check(text in result['error'], f'Worker case {index}: missing diagnostic {text}')
                else:
                    check(not result.get('error'), f'Worker case {index}: {result.get("error")}')
                    check(result.get('go') == output if isinstance(output, str) else result.get('files') == output,
                        f'Worker case {index}: output differs from native CLI')
            evidence['workerObservations'] = len(checks)
            evidence['checks'].append('real Worker LF/CRLF byte parity; filename diagnostics; invalid collection bounds; recovery after every failure; repeat; all legacy example outputs')
            ui_checks(page, manifest, evidence['checks'])
            page.screenshot(path=str(args.out / 'package-playground.png'), full_page=True)
            stale_result_checks(browser, args, manifest, evidence['checks'])
            mobile_checks(browser, args, manifest, evidence['checks'])
            check(not page_errors, 'browser page errors: ' + repr(page_errors))
            evidence['manifest'] = {key: manifest[key] for key in ['commit', 'compilerDigest', 'wasmSha256', 'toolchain']}
            evidence['passed'] = True
            browser.close()
            browser = None
    except Exception as error:
        evidence['failure'] = str(error)
        raise
    finally:
        (args.out / 'summary.json').write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(evidence, ensure_ascii=False))


if __name__ == '__main__':
    main()
