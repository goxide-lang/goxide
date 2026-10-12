#!/usr/bin/env python3
"""Run existing local acceptance, or verify the deployed artifact and real Worker."""
import argparse
import functools
import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
from urllib.error import URLError
from urllib.parse import parse_qs, urlsplit
from urllib.request import Request, urlopen

from ci_publish_website import ASSETS, ROOT, SITE, validate_artifact


def acceptance(args):
    handler = functools.partial(SimpleHTTPRequestHandler, directory=str(args.dist))
    server = ThreadingHTTPServer(('127.0.0.1', 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        subprocess.run([sys.executable, '-B', str(ROOT / 'website/validation/package-files/run.py'),
            '--url', f'http://127.0.0.1:{server.server_port}',
            '--cli', str(ROOT / 'bin/goxide'), '--go', os.environ['GO_BIN'],
            '--chromium', args.chromium, '--out', str(args.out)], check=True)
    finally:
        server.shutdown()
        thread.join()
        server.server_close()


def live(args):
    from playwright.sync_api import sync_playwright
    manifest = validate_artifact(args.dist, os.environ['GITHUB_SHA'])
    expected = (args.dist / 'examples.json').read_bytes()
    deadline = time.monotonic() + 600
    while True:
        try:
            # Cache bust only the readiness probe. Browser navigation below uses the real URL.
            request = Request(SITE + 'examples.json?ci=' + manifest['websiteCommit'],
                headers={'Cache-Control': 'no-cache'})
            with urlopen(request, timeout=20) as response:
                if response.read() == expected:
                    break
        except URLError:
            pass
        if time.monotonic() >= deadline:
            raise RuntimeError('Pages has not served this exact manifest within 10 minutes; '
                'inspect the target repository Pages deployment, do not republish old artifacts')
        time.sleep(10)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(executable_path=args.chromium,
            headless=True, chromium_sandbox=True)
        try:
            page = browser.new_page()
            errors = []
            workers = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.on('worker', lambda worker: workers.append(worker.url))
            navigation = page.goto(SITE, wait_until='domcontentloaded')
            if not navigation.ok or navigation.body() != (args.dist / 'index.html').read_bytes():
                raise RuntimeError('actual browser navigation differs from approved index.html')
            page.wait_for_function("document.querySelector('#status').textContent.startsWith('转译完成')",
                timeout=60000)
            if page.evaluate('manifest') != manifest:
                raise RuntimeError('running page initialized with a different manifest')
            expected_query = dict(site=[manifest['websiteCommit']], compiler=[manifest['commit']],
                wasm=[manifest['wasmSha256']])
            if not workers or any(parse_qs(urlsplit(url).query) != expected_query for url in workers):
                raise RuntimeError('running page Worker identity differs from approved manifest')
            # Hidden hosting controls need not be publicly served by Pages.
            for name in sorted(ASSETS - {'.nojekyll', '_headers'}):
                served = page.evaluate("""async name => {
                  const response = await fetch(name, {cache:'no-store'});
                  const bytes = await response.arrayBuffer();
                  const hash = await crypto.subtle.digest('SHA-256', bytes);
                  return {ok:response.ok, size:bytes.byteLength,
                    sha256:Array.from(new Uint8Array(hash), b=>b.toString(16).padStart(2,'0')).join('')};
                }""", name)
                approved = (args.dist / name).read_bytes()
                if not served['ok'] or served['size'] != len(approved) or served['sha256'] != hashlib.sha256(approved).hexdigest():
                    raise RuntimeError('online asset differs from approved artifact: ' + name)
            if page.input_value('#output') != manifest['examples'][0]['go']:
                raise RuntimeError('online single-file Worker result differs')
            page.select_option('#examples', str(len(manifest['examples'])))
            page.wait_for_function("document.querySelector('#status').textContent.startsWith('转译完成')",
                timeout=60000)
            outputs = []
            for button in page.locator('#generated-files button').all():
                name = button.inner_text()
                button.click()
                outputs.append(dict(path=name, source=page.input_value('#output')))
            if sorted(outputs, key=lambda item: item['path']) != sorted(
                    manifest['packageExamples'][0]['generatedFiles'], key=lambda item: item['path']):
                raise RuntimeError('online multi-file Worker result differs')
            if errors:
                raise RuntimeError('online page exceptions: ' + repr(errors))
            result = dict(passed=True, browserSandbox=True, websiteCommit=manifest['websiteCommit'],
                compilerCommit=manifest['commit'], publicLibraries=manifest['publicLibraries'],
                wasmSha256=hashlib.sha256((args.dist / 'compiler.wasm').read_bytes()).hexdigest(),
                checks='public asset bytes; real page single-file and multi-file Worker output')
            args.out.mkdir(parents=True, exist_ok=False)
            (args.out / 'summary.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps(result))
            with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as summary:
                print(f'Online page/assets/real Worker verified with Chromium sandbox: '
                    f'[{manifest["websiteCommit"]}]({SITE}); compiler `{manifest["commit"]}`.', file=summary)
        finally:
            browser.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['acceptance', 'live'])
    parser.add_argument('--dist', type=Path, default=ROOT / 'website/dist')
    parser.add_argument('--chromium', required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    (acceptance if args.mode == 'acceptance' else live)(args)


if __name__ == '__main__':
    main()
