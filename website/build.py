#!/usr/bin/env python3
"""Rebuild every published example and the browser compiler from this checkout."""
import hashlib, json, os, pathlib, re, shutil, subprocess, tempfile
import catalog, project_snapshots, workspace_snapshots, workspace_import_snapshots, source_selection_snapshot
ROOT = pathlib.Path(__file__).resolve().parent.parent
os.chdir(ROOT)
GO = os.environ.get('GO_BIN', 'go')
env = dict(os.environ, GOTOOLCHAIN='local', GOMAXPROCS='4', GOWORK='off')
def run(*args, **kwargs):
    return subprocess.check_output(args, env=env, **kwargs)
version = run(GO, 'version').decode().strip()
run(GO, 'mod', 'verify')
website_sha = run('git', 'rev-parse', 'HEAD').decode().strip()
catalog_data = catalog.load()
sha = catalog_data['compilerCommit']
catalog.write_index(catalog_data)
# Bind to actual compiler sources too, including uncommitted adapter changes.
h = hashlib.sha256(); baseline = hashlib.sha256()
for path in sorted([*ROOT.glob('internal/**/*.go'), *ROOT.glob('cmd/goxide-playground/*.go'), *ROOT.glob('cmd/goxide/*.go'), *ROOT.glob('runtime/*.go'), *ROOT.glob('formatting/*.go'), ROOT/'go.mod', ROOT/'go.sum']):
    if path.name.endswith('_test.go'): continue
    relative = str(path.relative_to(ROOT))
    h.update(relative.encode()); h.update(path.read_bytes())
    baseline.update(relative.encode()); baseline.update(run('git', 'show', sha+':'+relative))
assert h.digest() == baseline.digest(), 'compiler differs from catalog baseline'
assert h.hexdigest() == catalog_data['compilerDigest'], 'compiler inventory/digest differs from reviewed release'
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
index = (dist/'index.html').read_text().replace('src="app.js"', f'src="app.js?site={website_sha}"').replace('href="style.css"', f'href="style.css?site={website_sha}"')
(dist/'index.html').write_text(index)
(ROOT/'bin').mkdir(exist_ok=True)
run(GO, 'build', '-o', str(ROOT/'bin/goxide'), './cmd/goxide')
examples = []
for entry in catalog_data['examples']:
    name, title, description = entry['id'], entry['title'], entry['description']
    path = ROOT/'website/examples'/f'{name}.gox'
    generated = run(str(ROOT/'bin/goxide'), 'transpile', str(path)).decode()
    if name in ['result','go_result','serde_option']:
        assert 'github.com/goxide-lang/std/sum' in generated
        families = re.findall(r'^type (?:Option|Result)\[[^\n]+', generated, re.MULTILINE)
        assert families and all('= __goxide_enum_std.' in line for line in families), 'sum families must be aliases to shared std, not copied definitions'
    stdout = run(str(ROOT/'bin/goxide'), 'run', '--go', GO, str(path)).decode()
    assert stdout == entry['expectedStdout'], f'{name}: runtime output changed'
    examples.append(dict(id=name, title=title, description=description, source=path.read_text(), go=generated, stdout=stdout))
# A known repository project, generated and run locally at build time only.
# It is evidence for CLI multi-package behavior, never a browser compiler input.
with tempfile.TemporaryDirectory(prefix='goxide-web-reexports-') as temporary:
    output = pathlib.Path(temporary)/'generated'
    project = ROOT/'examples/reexports'
    run(str(ROOT/'bin/goxide'), 'transpile', '--go', GO, '-o', str(output), str(project))
    cli_example = dict(id='reexports', title='use / pub use：同一存储与定义点',
        files=[dict(path=str(p.relative_to(project)), source=p.read_text(),
                    go=(output/p.relative_to(project).with_suffix('.go')).read_text())
               for p in sorted(project.rglob('*.gox'))],
        stdout=run(str(ROOT/'bin/goxide'), 'run', '--go', GO, str(project)).decode())
# Negative examples are genuine CLI rejections; only sanitized diagnostics are public.
checks = []
for name in sorted({n for c in catalog_data['items'] for n in c['checks']}):
    path = ROOT/'website/checks/catalog'/f'{name}.gox'
    result = subprocess.run([str(ROOT/'bin/goxide'), 'transpile', str(path)], env=env, capture_output=True, text=True)
    assert result.returncode != 0, f'{name}: expected rejection'
    diagnostic = result.stderr.strip().replace(str(path), 'playground.gox')
    assert diagnostic and '/workspace/' not in diagnostic
    checks.append(dict(id=name, source=path.read_text(), diagnostic=diagnostic))
# A complete, deliberately public minimal feature/target project, run without scripts.
with tempfile.TemporaryDirectory(prefix='goxide-catalog-features-') as temporary:
    project = pathlib.Path(temporary)/'project'
    shutil.copytree(ROOT/'website/cli-examples/features', project)
    files = [dict(path=str(p.relative_to(project)), source=p.read_text()) for p in sorted(project.rglob('*')) if p.is_file()]
    feature_runs = []
    for flags, expected in [([], 'pretty: true\n'), (['--no-default-features'], 'pretty: false\n'), (['--locked','--offline'], 'pretty: true\n')]:
        stdout = run(str(ROOT/'bin/goxide'), 'run', '--go', GO, '--bin', 'demo', *flags, str(project)).decode()
        assert stdout == expected
        feature_runs.append(dict(command='goxide run --bin demo '+(' '.join(flags)+' ' if flags else '')+'.', stdout=stdout))
    assert not (project/'go.mod').exists() and not (project/'go.sum').exists()
    assert list((project/'target').rglob('go.mod'))
    files.append(dict(path='goxide.lock', source=(project/'goxide.lock').read_text()))
    generated = sorted({p.read_text() for p in (project/'target').rglob('*.go') if 'pretty:' in p.read_text()})
    assert generated, 'feature target generated Go missing'
    files.extend(dict(path=f'target 生成 Go {i+1}（只读）', source=go) for i,go in enumerate(generated))
    feature_example = dict(id='features', title='feature 与 bin：完整最小工程', files=files, runs=feature_runs)
migration_example = project_snapshots.migration(ROOT, GO, run)
vendor_example = project_snapshots.vendor(ROOT, GO, run)
cli_options_example = project_snapshots.cli(ROOT, GO, run)
public_catalog = catalog.export(catalog_data, checks)
eh = hashlib.sha256()
for archive in sorted(exports.glob('*.a')):
    eh.update(archive.name.encode()); eh.update(archive.read_bytes())
export_target = json.loads(run(GO, 'env', '-json', 'GOOS', 'GOARCH'))
wasm_flags = ['-trimpath', '-buildvcs=false', '-ldflags=-s -w -buildid=']
public_libraries = []
for module, commit in [('github.com/goxide-lang/std','ce5bccb4ff9c7fb96b7f192a3a263051590d9288'),('github.com/goxide-lang/serde','a1b791e96d660c6be620e110c566a9a171631b7e')]:
    selected = json.loads(run(GO, 'list', '-m', '-json', module))
    assert not selected.get('Replace'), 'public dependency must not use a local replacement'
    metadata = json.loads(run(GO, 'mod', 'download', '-json', module+'@'+selected['Version']))
    assert selected['Version'].endswith(commit[:12]) and metadata.get('Sum') and metadata.get('GoModSum')
    public_libraries.append(dict(module=module, commit=commit, version=selected['Version'], sum=metadata['Sum'], goModSum=metadata['GoModSum']))
metadata_schema = int(re.search(r'const enumMetadataVersion = (\d+)', (ROOT/'internal/frontend/enum_artifact.go').read_text()).group(1))
assert metadata_schema == 8
def source_version(path, pattern):
    return int(re.search(pattern, (ROOT/path).read_text()).group(1))
abi_versions = dict(enum=metadata_schema, reference=source_version('internal/refs/metadata.go', r'const MetadataVersion = (\d+)'), use=source_version('internal/frontend/use_artifact.go', r'a.Version != (\d+)'), sum=source_version('internal/enums/builtins.go', r'const SumABI = (\d+)'))
assert abi_versions == dict(enum=8, reference=3, use=2, sum=2)
std_abi = run(GO, 'doc', 'github.com/goxide-lang/std/sum.GoxideSumABI').decode()
assert re.search(r'GoxideSumABI = 2\b', std_abi), std_abi

manifest = dict(abiVersions=abi_versions, metadataSchema=metadata_schema, publicLibraries=public_libraries, wasmBuildFlags=wasm_flags, exportTrimpath=True, commit=sha, websiteCommit=website_sha, catalog=public_catalog, compilerDigest=h.hexdigest(), exportsDigest=eh.hexdigest(), exportTarget=export_target, toolchain=version, packages=packages,
                importClosure=sorted(closure), maxSourceBytes=32768, compileTimeoutMs=10000,
                examples=examples, cliExamples=[cli_example], projectExamples=[feature_example,migration_example,vendor_example,cli_options_example]+workspace_snapshots.build(ROOT, GO)+workspace_import_snapshots.build(ROOT, GO)+[source_selection_snapshot.build(ROOT, GO)])
wasm_env = dict(env, GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
subprocess.run([GO, 'build', *wasm_flags, '-o', str(dist/'compiler.wasm'), './cmd/goxide-playground'], env=wasm_env, check=True)
manifest['wasmSha256'] = hashlib.sha256((dist/'compiler.wasm').read_bytes()).hexdigest()
(dist/'examples.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
goroot = run(GO, 'env', 'GOROOT').decode().strip()
shutil.copyfile(pathlib.Path(goroot)/'lib/wasm/wasm_exec.js', dist/'wasm_exec.js')
shutil.copyfile(pathlib.Path(goroot)/'LICENSE', dist/'GO-LICENSE.txt')
embed = ROOT/'cmd/goxide-web/dist'
if embed.exists(): shutil.rmtree(embed)
shutil.copytree(dist, embed)
print(json.dumps(dict(commit=sha, compilerDigest=h.hexdigest(), wasmBytes=(dist/'compiler.wasm').stat().st_size, examples=len(examples))))
