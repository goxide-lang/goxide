#!/usr/bin/env python3
"""Rebuild every published example and the browser compiler from this checkout."""
import hashlib, json, os, pathlib, re, shutil, subprocess, tempfile
import catalog, project_snapshots, workspace_snapshots, workspace_import_snapshots, source_selection_snapshot
from build_support import dependency_context, dependency_sources, go_license_path, standalone_playground, verify_compiler_sources
ROOT = pathlib.Path(__file__).resolve().parent.parent
os.chdir(ROOT)
GO = os.environ.get('GO_BIN', 'go')
env = dict(os.environ, GOTOOLCHAIN='local', GOMAXPROCS='4', GOWORK='off', GOPROXY='off')
def run(*args, **kwargs):
    return subprocess.check_output(args, env=env, **kwargs)
version = run(GO, 'version').decode().strip()
run(GO, 'mod', 'verify')
website_sha = run('git', 'rev-parse', 'HEAD').decode().strip()
catalog_data = catalog.load()
sha = catalog_data['compilerCommit']
# Check both inventories before generating any website assets.
compiler_digest = verify_compiler_sources(ROOT, sha, catalog_data['compilerDigest'])
goroot = run(GO, 'env', 'GOROOT').decode().strip()
license_path = go_license_path(goroot)
catalog.write_index(catalog_data)
context = dependency_context(ROOT)
roots_file = ROOT / context['path']
packages = list(dict.fromkeys(binding['package'] for binding in context['bindings']))
exports = ROOT/'internal/typecheck/browserexports'
exports.mkdir(exist_ok=True)
for old in exports.glob('*.a'): old.unlink()
# Use native export archives as compiler metadata, never execute user Go.
raw = run(GO, 'list', '-trimpath', '-export', '-deps', '-json', *packages).decode()
decoder = json.JSONDecoder(); closure = []; export_records = []
origins = {}
while raw.strip():
    p, end = decoder.raw_decode(raw.lstrip()); raw = raw.lstrip()[end:]
    export_records.append(p)
    if p.get('Export'):
        data = run(GO, 'tool', 'pack', 'p', p['Export'], '__.PKGDEF')
        header = f"{'__.PKGDEF':<16}{0:<12}{0:<6}{0:<6}{'644':<8}{len(data):<10}`\n".encode()
        (exports/(p['ImportPath'].replace('/', '_')+'.a')).write_bytes(b'!<arch>\n' + header + data + (b'\n' if len(data)%2 else b''))
        origins[p['ImportPath']] = dict(module=p.get('Module', {}).get('Path', ''), standard=bool(p.get('Standard')),
            exportSha256=hashlib.sha256((exports/(p['ImportPath'].replace('/', '_')+'.a')).read_bytes()).hexdigest())
        closure.append(p['ImportPath'])
(exports/'origins.json').write_text(json.dumps(origins, sort_keys=True, indent=2)+'\n')
for binding in context['bindings']:
    origin = origins[binding['package']]
    assert origin['standard'] == (binding['source'] == 'toolchain') and origin['module'] == binding['module'], 'explicit root differs from actual Go package owner: ' + binding['path']
dependencies = dependency_sources(packages, export_records, goroot=goroot,
                                  toolchain_version=run(GO, 'env', 'GOVERSION').decode().strip(),
                                  exports=exports)
for dependency in dependencies:
    dependency['staticPaths'] = [binding['path'] for binding in context['bindings'] if binding['package'] == dependency['package']]
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
    source = path.read_bytes()
    # Match the owner-free browser input, rather than the repository's module.
    with standalone_playground(source) as input_path:
        generated = run(str(ROOT/'bin/goxide'), 'transpile', '--roots', str(roots_file), str(input_path)).decode()
        if name in ['result','go_result','serde_option']:
            assert 'github.com/goxide-lang/std/sum' in generated
            families = re.findall(r'^type (?:Option|Result)\[[^\n]+', generated, re.MULTILINE)
            assert families and all('= __goxide_enum_std.' in line for line in families), 'sum families must be aliases to shared std, not copied definitions'
        stdout = run(str(ROOT/'bin/goxide'), 'run', '--go', GO, '--roots', str(roots_file), str(input_path)).decode()
    assert stdout == entry['expectedStdout'], f'{name}: runtime output changed'
    examples.append(dict(id=name, title=title, description=description, source=source.decode(), go=generated, stdout=stdout))
# A fixed, single user package. Its minimal module selects the same package
# path as CompilePackage("playground", ...); it is build metadata, not an
# editable browser source or authority to load other modules or run scripts.
with tempfile.TemporaryDirectory(prefix='goxide-web-package-') as temporary:
    project = pathlib.Path(temporary)/'playground'
    project.mkdir()
    fixture = ROOT/'website/validation/package-files/example'
    module = (fixture/'go.mod').read_bytes()
    assert module.startswith(b'module playground\n\ngo 1.27\n'), 'unexpected package example module policy'
    for metadata in ['go.mod', 'go.sum']:
        shutil.copyfile(fixture/metadata, project/metadata)
    fixture_modules = json.loads(run(GO, 'mod', 'edit', '-json', cwd=project))
    expected_modules = {name: json.loads(run(GO, 'list', '-m', '-json', name))['Version'] for name in ['github.com/goxide-lang/std', 'github.com/goxide-lang/serde']}
    assert {item['Path']: item['Version'] for item in fixture_modules['Require']} == expected_modules, 'package fixture module selections differ from compiler'
    assert not fixture_modules.get('Replace'), 'package fixture must not replace public dependencies'
    package_files = []
    for path in sorted(fixture.glob('*.gox')):
        source = path.read_bytes()
        (project/path.name).write_bytes(source)
        package_files.append(dict(path=path.name, source=source.decode()))
    output = pathlib.Path(temporary)/'generated'
    run(str(ROOT/'bin/goxide'), 'transpile', '--go', GO, '--roots', str(roots_file), '-o', str(output), str(project))
    generated_files = [dict(path=path.name, source=path.read_bytes().decode())
                       for path in sorted(output.glob('*.go'))]
    assert {pathlib.Path(file['path']).with_suffix('.go').name for file in package_files} <= {file['path'] for file in generated_files}
    assert any(file['path'].startswith('goxide_helpers_') and file['path'].endswith('.go') for file in generated_files), 'package example must expose real helper files'
    stdout = run(str(ROOT/'bin/goxide'), 'run', '--go', GO, '--roots', str(roots_file), str(project)).decode()
    assert stdout == '42 GOXIDE Item { Value: 21 }\n', 'package example runtime output changed'
    package_examples = [dict(id='package_files', title='同包多文件：文件作用域与宏/派生',
                             description='三个可编辑 .gox 文件属于同一个包；每个文件保留独立 use 作用域，共享类型、函数、宏与派生结果。',
                             files=package_files, generatedFiles=generated_files, stdout=stdout)]
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
    source = path.read_bytes()
    with standalone_playground(source) as input_path:
        result = subprocess.run([str(ROOT/'bin/goxide'), 'transpile', '--roots', str(roots_file), str(input_path)], env=env, capture_output=True, text=True)
    assert result.returncode != 0, f'{name}: expected rejection'
    diagnostic = result.stderr.strip().replace(str(input_path), 'playground.gox')
    assert diagnostic and '/workspace/' not in diagnostic
    checks.append(dict(id=name, source=source.decode(), diagnostic=diagnostic))
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
for module in ['github.com/goxide-lang/std', 'github.com/goxide-lang/serde']:
    selected = json.loads(run(GO, 'list', '-m', '-json', module))
    assert not selected.get('Replace'), 'public dependency must not use a local replacement'
    metadata = json.loads(run(GO, 'mod', 'download', '-json', module+'@'+selected['Version']))
    commit = metadata.get('Origin', {}).get('Hash', '')
    assert re.fullmatch('[0-9a-f]{40}', commit) and metadata.get('Sum') and metadata.get('GoModSum'), 'verified cached module origin is required'
    assert metadata['Path'] == module and metadata['Version'] == selected['Version']
    public_libraries.append(dict(module=module, commit=commit, version=selected['Version'], sum=metadata['Sum'], goModSum=metadata['GoModSum']))
metadata_schema = int(re.search(r'const enumMetadataVersion = (\d+)', (ROOT/'internal/frontend/enum_artifact.go').read_text()).group(1))
assert metadata_schema == 9
def source_version(path, pattern):
    return int(re.search(pattern, (ROOT/path).read_text()).group(1))
abi_versions = dict(enum=metadata_schema, reference=source_version('internal/refs/metadata.go', r'const MetadataVersion = (\d+)'), use=source_version('internal/frontend/use_artifact.go', r'const useMetadataVersion = (\d+)'), sum=source_version('internal/enums/builtins.go', r'const SumABI = (\d+)'))
assert abi_versions == dict(enum=9, reference=3, use=3, sum=3)
std_abi = run(GO, 'doc', 'github.com/goxide-lang/std/sum.GoxideSumABI').decode()
assert re.search(r'GoxideSumABI = 3\b', std_abi), std_abi

manifest = dict(abiVersions=abi_versions, metadataSchema=metadata_schema, publicLibraries=public_libraries, wasmBuildFlags=wasm_flags, exportTrimpath=True, commit=sha, websiteCommit=website_sha, catalog=public_catalog, compilerDigest=compiler_digest, exportsDigest=eh.hexdigest(), exportTarget=export_target, toolchain=version, packages=packages,
                dependencies=dependencies, dependencyContext=context, dependencySourceScope='fixed explicit project roots; complete native-selected GoFiles/CgoFiles; paths relative to named module or Go distribution',
                exportOrigins=origins, exportOriginsSha256=hashlib.sha256((exports/'origins.json').read_bytes()).hexdigest(),
                importClosure=sorted(closure), maxSourceBytes=32768, maxFiles=16, compileTimeoutMs=10000,
                examples=examples, packageExamples=package_examples, cliExamples=[cli_example], projectExamples=[feature_example,migration_example,vendor_example,cli_options_example]+workspace_snapshots.build(ROOT, GO)+workspace_import_snapshots.build(ROOT, GO)+[source_selection_snapshot.build(ROOT, GO)])
wasm_env = dict(env, GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
subprocess.run([GO, 'build', *wasm_flags, '-o', str(dist/'compiler.wasm'), './cmd/goxide-playground'], env=wasm_env, check=True)
manifest['wasmSha256'] = hashlib.sha256((dist/'compiler.wasm').read_bytes()).hexdigest()
(dist/'examples.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
shutil.copyfile(pathlib.Path(goroot)/'lib/wasm/wasm_exec.js', dist/'wasm_exec.js')
shutil.copyfile(license_path, dist/'GO-LICENSE.txt')
embed = ROOT/'cmd/goxide-web/dist'
if embed.exists(): shutil.rmtree(embed)
shutil.copytree(dist, embed)
print(json.dumps(dict(commit=sha, websiteCommit=website_sha, compilerDigest=compiler_digest, wasmBytes=(dist/'compiler.wasm').stat().st_size, examples=len(examples))))
