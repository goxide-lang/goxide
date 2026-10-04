#!/usr/bin/env python3
"""Rebuild every published example and the browser compiler from this checkout."""
import hashlib, json, os, pathlib, shutil, subprocess, tempfile
ROOT = pathlib.Path(__file__).resolve().parent.parent
os.chdir(ROOT)
GO = os.environ.get('GO_BIN', 'go')
env = dict(os.environ, GOTOOLCHAIN='local', GOMAXPROCS='4')
def run(*args, **kwargs):
    return subprocess.check_output(args, env=env, **kwargs)
version = run(GO, 'version').decode().strip()
sha = run('git', 'rev-parse', 'HEAD').decode().strip()
# Bind to actual compiler sources too, including uncommitted adapter changes.
h = hashlib.sha256()
for path in sorted([*ROOT.glob('internal/**/*.go'), *ROOT.glob('cmd/hgo-playground/*.go'), *ROOT.glob('runtime/*.go'), *ROOT.glob('formatting/*.go'), ROOT/'go.mod', ROOT/'go.sum']):
    if path.name.endswith('_test.go'): continue
    h.update(str(path.relative_to(ROOT)).encode()); h.update(path.read_bytes())
packages = ['fmt', 'errors', 'io', 'strconv', 'strings', 'math', 'encoding/json',
            'github.com/hxsf/goxide/runtime', 'github.com/hxsf/goxide/formatting',
            'github.com/goxide-lang/std/sum', 'github.com/goxide-lang/serde', 'github.com/goxide-lang/serde/json']
exports = ROOT/'internal/typecheck/browserexports'
exports.mkdir(exist_ok=True)
for old in exports.glob('*.a'): old.unlink()
# Use native export archives as compiler metadata, never execute user Go.
raw = run(GO, 'list', '-trimpath', '-export', '-deps', '-json', *packages).decode()
decoder = json.JSONDecoder(); closure = []
while raw.strip():
    p, end = decoder.raw_decode(raw.lstrip()); raw = raw.lstrip()[end:]
    if p.get('Export'):
        data = run(GO, 'tool', 'pack', 'p', p['Export'], '__.PKGDEF')
        header = f"{'__.PKGDEF':<16}{0:<12}{0:<6}{0:<6}{'644':<8}{len(data):<10}`\n".encode()
        (exports/(p['ImportPath'].replace('/', '_')+'.a')).write_bytes(b'!<arch>\n' + header + data + (b'\n' if len(data)%2 else b''))
        closure.append(p['ImportPath'])
dist = ROOT/'website/dist'
if dist.exists(): shutil.rmtree(dist)
# Explicit publish allowlist: no source tree, RFCs, configs or accidental files.
static_files = ['index.html', '404.html', 'style.css', 'app.js', 'worker.js', '_headers', '.nojekyll']
assert set(p.name for p in (ROOT/'website/static').iterdir()) == set(static_files), 'unexpected static input'
dist.mkdir()
for name in static_files:
    shutil.copyfile(ROOT/'website/static'/name, dist/name)
(ROOT/'bin').mkdir(exist_ok=True)
run(GO, 'build', '-o', str(ROOT/'bin/hgo'), './cmd/hgo')
examples = []
for name, title, description in [
    ('macro', '声明宏', '有限模式匹配 + 定义点绑定。将重复写法交给编译期。'),
    ('enum', 'enum / match', '闭合变体与 guard；没有覆盖所有分支时给出诊断。'),
    ('result', 'Option / Result / ?', '显式适配 Go 的 (T, error)，沿 Err 分支转换并传播原因。'),
    ('go_result', 'Go 返回值适配宏', '按原生签名推导 T/E；普通版舍弃失败值，保值版保留部分值与原始 E。'),
    ('derive', '显式派生与 JSON', 'Default、Debug、Clone、.equals 与 serde JSON 来自同一个模型。')]:
    path = ROOT/'website/examples'/f'{name}.hgo'
    generated = run(str(ROOT/'bin/hgo'), 'transpile', str(path)).decode()
    examples.append(dict(id=name, title=title, description=description, source=path.read_text(), go=generated))
# A known repository project, generated and run locally at build time only.
# It is evidence for CLI multi-package behavior, never a browser compiler input.
with tempfile.TemporaryDirectory(prefix='hgo-web-reexports-') as temporary:
    output = pathlib.Path(temporary)/'generated'
    project = ROOT/'examples/reexports'
    run(str(ROOT/'bin/hgo'), 'transpile', '-go', GO, '-o', str(output), str(project))
    cli_example = dict(id='reexports', title='use / pub use：同一存储与定义点',
        files=[dict(path=str(p.relative_to(project)), source=p.read_text(),
                    go=(output/p.relative_to(project).with_suffix('.go')).read_text())
               for p in sorted(project.rglob('*.hgo'))],
        stdout=run(str(ROOT/'bin/hgo'), 'run', '-go', GO, str(project)).decode())
eh = hashlib.sha256()
for archive in sorted(exports.glob('*.a')):
    eh.update(archive.name.encode()); eh.update(archive.read_bytes())
export_target = json.loads(run(GO, 'env', '-json', 'GOOS', 'GOARCH'))
wasm_flags = ['-trimpath', '-buildvcs=false', '-ldflags=-s -w -buildid=']
manifest = dict(wasmBuildFlags=wasm_flags, exportTrimpath=True, commit=sha, compilerDigest=h.hexdigest(), exportsDigest=eh.hexdigest(), exportTarget=export_target, toolchain=version, packages=packages,
                importClosure=sorted(closure), maxSourceBytes=32768, compileTimeoutMs=10000,
                examples=examples, cliExamples=[cli_example])
(dist/'examples.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
wasm_env = dict(env, GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
subprocess.run([GO, 'build', *wasm_flags, '-o', str(dist/'compiler.wasm'), './cmd/hgo-playground'], env=wasm_env, check=True)
goroot = run(GO, 'env', 'GOROOT').decode().strip()
shutil.copyfile(pathlib.Path(goroot)/'lib/wasm/wasm_exec.js', dist/'wasm_exec.js')
shutil.copyfile(pathlib.Path(goroot)/'LICENSE', dist/'GO-LICENSE.txt')
embed = ROOT/'cmd/hgo-web/dist'
if embed.exists(): shutil.rmtree(embed)
shutil.copytree(dist, embed)
print(json.dumps(dict(commit=sha, compilerDigest=h.hexdigest(), wasmBytes=(dist/'compiler.wasm').stat().st_size, examples=len(examples))))
