#!/usr/bin/env python3
"""Rebuild every published example and the browser compiler from this checkout."""
import hashlib, json, os, pathlib, shutil, subprocess
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
for path in sorted([*ROOT.glob('internal/**/*.go'), *ROOT.glob('cmd/hgo-playground/*.go'), *ROOT.glob('runtime/*.go'), *ROOT.glob('formatting/*.go'), *ROOT.glob('serde/*.go'), *ROOT.glob('serdejson/*.go'), ROOT/'go.mod']):
    if path.name.endswith('_test.go'): continue
    h.update(str(path.relative_to(ROOT)).encode()); h.update(path.read_bytes())
packages = ['fmt', 'errors', 'strconv', 'strings', 'math', 'encoding/json',
            'github.com/hxsf/goxide/runtime', 'github.com/hxsf/goxide/formatting',
            'github.com/hxsf/goxide/serde', 'github.com/hxsf/goxide/serdejson']
exports = ROOT/'internal/typecheck/browserexports'
exports.mkdir(exist_ok=True)
for old in exports.glob('*.a'): old.unlink()
# Use native export archives as compiler metadata, never execute user Go.
raw = run(GO, 'list', '-export', '-deps', '-json', *packages).decode()
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
shutil.copytree(ROOT/'website/static', dist)
(ROOT/'bin').mkdir(exist_ok=True)
run(GO, 'build', '-o', str(ROOT/'bin/hgo'), './cmd/hgo')
examples = []
for name, title, description in [
    ('macro', '声明宏', '有限模式匹配 + 定义点绑定。将重复写法交给编译期。'),
    ('enum', 'enum / match', '闭合变体与 guard；没有覆盖所有分支时给出诊断。'),
    ('result', 'Option / Result / ?', '显式适配 Go 的 (T, error)，沿 Err 分支转换并传播原因。'),
    ('derive', '显式派生与 JSON', 'Default、Debug、Clone、.equals 与 serde JSON 来自同一个模型。')]:
    path = ROOT/'website/examples'/f'{name}.hgo'
    generated = run(str(ROOT/'bin/hgo'), 'transpile', str(path)).decode()
    examples.append(dict(id=name, title=title, description=description, source=path.read_text(), go=generated))
eh = hashlib.sha256()
for archive in sorted(exports.glob('*.a')):
    eh.update(archive.name.encode()); eh.update(archive.read_bytes())
export_target = json.loads(run(GO, 'env', '-json', 'GOOS', 'GOARCH'))
manifest = dict(commit=sha, compilerDigest=h.hexdigest(), exportsDigest=eh.hexdigest(), exportTarget=export_target, toolchain=version, packages=packages,
                importClosure=sorted(closure), maxSourceBytes=32768, compileTimeoutMs=10000,
                examples=examples)
(dist/'examples.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
wasm_env = dict(env, GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
subprocess.run([GO, 'build', '-trimpath', '-o', str(dist/'compiler.wasm'), './cmd/hgo-playground'], env=wasm_env, check=True)
goroot = run(GO, 'env', 'GOROOT').decode().strip()
shutil.copyfile(pathlib.Path(goroot)/'lib/wasm/wasm_exec.js', dist/'wasm_exec.js')
shutil.copyfile(pathlib.Path(goroot)/'LICENSE', dist/'GO-LICENSE.txt')
embed = ROOT/'cmd/hgo-web/dist'
if embed.exists(): shutil.rmtree(embed)
shutil.copytree(dist, embed)
print(json.dumps(dict(commit=sha, compilerDigest=h.hexdigest(), wasmBytes=(dist/'compiler.wasm').stat().st_size, examples=len(examples))))
