#!/usr/bin/env python3
"""Disposable, stdlib-only mechanism experiments. NOT a runner or sandbox."""
import hashlib
import http.server
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import threading
import time

HERE = Path(__file__).resolve().parent
GO = os.environ.get('BUILD_GOX_RESEARCH_GO', '/workspace/goxide-environment/go/bin/go')
results = []
def record(name, evidence, kind='observed'):
    results.append(dict(name=name, kind=kind, evidence=evidence))
def run(args, **kw):
    result = subprocess.run([str(x) for x in args], text=True, capture_output=True, **kw)
    if result.returncode:
        raise RuntimeError(f'{args}: {result.stderr}')
    return result
def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()

with tempfile.TemporaryDirectory(prefix='build-gox-research-') as tmp:
    root = Path(tmp)
    env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='', GOPROXY='off', GOSUMDB='off', CGO_ENABLED='0', GOENV='off')
    version = run([GO, 'version'], env=env).stdout.strip()
    assert 'go1.27.1 ' in version, version
    host = json.loads(run([GO, 'env', '-json', 'GOHOSTOS', 'GOHOSTARCH'], env=env).stdout)
    env.update(GOOS=host['GOHOSTOS'], GOARCH=host['GOHOSTARCH'], GOCACHE=str(root/'go-cache'), GOPATH=str(root/'go-path'))
    record('toolchain', version)
    shutil.copyfile(HERE/'probe.go.fixture', root/'probe.go')
    binary = root/'probe'
    run([GO, 'build', '-buildvcs=false', '-o', binary, root/'probe.go'], env=env)
    script_env = dict(env, TARGET_OS='linux', TARGET_ARCH='arm64')
    evidence = run([binary, 'host'], env=script_env).stdout.strip()
    assert 'target=linux/arm64' in evidence
    assert f"host={host['GOHOSTOS']}/{host['GOHOSTARCH']}" in evidence
    record('host-vs-target', evidence)
    run([GO, 'build', '-buildvcs=false', '-o', root/'target-arm64', root/'probe.go'], env=dict(env, GOOS='linux', GOARCH='arm64'))
    elf = (root/'target-arm64').read_bytes()[:20]
    assert elf[:4] == b'\x7fELF' and int.from_bytes(elf[18:20], 'little') == 183
    record('cross-go', 'Built Linux ARM64 ELF e_machine=183; never executed; not an hgo cross-target test')
    schema = root/'schema.json'; schema.write_text('{"name":"User"}')
    output = root/'output'; output.mkdir()
    run([binary, 'generate', schema, output/'schema.go'], env=env)
    run([GO, 'test', output/'schema.go'], env=env)
    record('schema', 'Self-authored host Go generator produced target Go source; Go compiled generated file')
    outside = root/'outside-output-marker'
    run([binary, 'outside', outside], cwd=output, env=env)
    assert outside.exists()
    record('not-filesystem-sandbox', 'Child wrote outside cwd/output, within disposable fixture; OUT_DIR/cwd are conventions')
    class Quiet(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
        def log_message(self, *args): pass
    with http.server.ThreadingHTTPServer(('127.0.0.1', 0), Quiet) as server:
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        code = run([binary, 'network', f'http://127.0.0.1:{server.server_port}/'], env=env).stdout.strip()
        server.shutdown(); thread.join()
    assert code == '200'
    record('offline-not-network-sandbox', 'GOPROXY=off GOSUMDB=off: self-authored Go child reached loopback HTTP 200; no external request')
    evidence = run([binary, 'context', root/'descendant'], env=env).stdout.strip()
    assert evidence == 'parent_failed=true descendant_survived=true', evidence
    record('command-context', evidence)
    stage = root/'stage'; stage.mkdir()
    published = root/'committed'; published.mkdir(); (published/'old').write_text('last good')
    child = subprocess.Popen([str(binary), 'partial', str(stage)], env=env, start_new_session=True)
    try:
        ready_deadline = time.monotonic() + 5
        while not (stage/'partial.go').exists() and child.poll() is None and time.monotonic() < ready_deadline:
            time.sleep(0.01)
        assert (stage/'partial.go').exists(), 'partial fixture did not become ready'
        child.wait(timeout=0.1)
        raise AssertionError('expected timeout')
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL); child.wait()
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL); child.wait()
    assert (stage/'partial.go').exists() and (published/'old').read_text() == 'last good'
    shutil.rmtree(stage)
    fresh = root/'fresh'; fresh.mkdir(); (fresh/'complete').write_text('valid')
    os.rename(fresh, root/'new-immutable-key')
    assert (root/'new-immutable-key'/'complete').read_text() == 'valid'
    record('atomic-stage', 'Timeout left private partial; harness removed owned stage; old commit intact; complete fresh directory renamed to absent immutable destination (same filesystem)')
    # Deliberately a model, not read tracking or production cache.
    model = dict(source='s1', inputs={'schema': schema.read_text()}, env={'MODE': ['unset']}, target='linux/amd64', features=['a'], unit='lib', policy='trusted-unconfined')
    key = digest(model)
    keys = {key}
    for field, value in [('target','linux/arm64'), ('features',['b']), ('unit','bin:cli'), ('policy','sandbox-v1')]:
        changed = dict(model); changed[field] = value; keys.add(digest(changed))
    assert len(keys) == 5
    assert digest(dict(model, env={'MODE':['set','']})) != key
    undeclared = root/'hidden'; undeclared.write_text('one'); undeclared.write_text('two')
    assert digest(model) == key
    record('cache-model', 'Target/features/unit/policy and unset-vs-empty change keys; omitted input does NOT change key', 'model')
    directory = root/'inputs'; directory.mkdir(); (directory/'a').write_text('one')
    def inventory():
        entries = []
        def walk(base):
            for p in sorted(base.iterdir()):
                if p.is_symlink():
                    raise ValueError('model rejects symlink')
                relative = p.relative_to(directory).as_posix()
                if p.is_dir():
                    entries.append((relative, 'directory')); walk(p)
                elif p.is_file():
                    entries.append((relative, hashlib.sha256(p.read_bytes()).hexdigest()))
                else:
                    raise ValueError('model rejects special file')
        walk(directory)
        return entries
    before = digest(inventory()); (directory/'b').write_text('two'); assert digest(inventory()) != before
    (directory/'b').unlink(); assert digest(inventory()) == before
    nested = directory/'nested'; nested.mkdir(); (nested/'c').write_text('three')
    nested_key = digest(inventory()); (nested/'c').write_text('four')
    assert digest(inventory()) != nested_key
    (directory/'escape').symlink_to(outside)
    try:
        inventory()
        raise AssertionError('symlink should be rejected')
    except ValueError as error:
        assert 'symlink' in str(error)
    record('directory-path-model', 'Recursive inventory model: add/delete/nested content changes alter digest; lexical in-root symlink points outside and is rejected', 'model')
    sentinel = root/'user-file'; sentinel.write_text('retain')
    owned = root/'owned'; owned.mkdir(); (owned/'escape').symlink_to(sentinel)
    shutil.rmtree(owned); assert sentinel.read_text() == 'retain'
    record('clean-mechanism', 'Python rmtree removed owned fixture with symlink without deleting external sentinel; NOT proof of future Go clean implementation')
    for name, args in [('unshare-net', ['unshare','-Urn','true']), ('bwrap-net', ['bwrap','--unshare-user','--unshare-net','--ro-bind','/','/','--','/bin/true'])]:
        if not shutil.which(args[0]):
            record(name, 'unavailable', 'capability'); continue
        probe = subprocess.run(args, text=True, capture_output=True, timeout=5)
        record(name, {'exit':probe.returncode, 'stderr':probe.stderr.strip()[:500]}, 'capability')
    # go build must not silently run author generation directives.
    pkg = root/'generate-package'; pkg.mkdir()
    (pkg/'go.mod').write_text('module example.invalid/research\n\ngo 1.27\n')
    (pkg/'main.go').write_text('//go:generate '+str(binary)+' outside '+str(pkg/'generated-marker')+'\npackage main\nfunc main() {}\n')
    run([GO, 'build', '-buildvcs=false', '-o', pkg/'app', '.'], cwd=pkg, env=env)
    assert not (pkg/'generated-marker').exists()
    run([GO, 'generate', '.'], cwd=pkg, env=env)
    assert (pkg/'generated-marker').exists()
    record('generate-explicit', 'go build did not execute directive; explicit go generate executed self-authored fixture')
print(json.dumps({'scope':'mechanisms and model only; no production runner; all temp fixtures removed', 'results':results}, indent=2, ensure_ascii=False))
