#!/usr/bin/env python3
"""Rebuild every published example and the browser compiler from this checkout."""
import argparse, gzip, hashlib, json, os, pathlib, re, shutil, subprocess, tempfile
import catalog, project_snapshots, project_downloads, tutorials, docs_content
from build_support import dependency_context, dependency_source_packages, dependency_sources, go_license_path, standalone_playground, verify_compiler_sources, verify_compiler_source, worktree_compiler_source
ROOT = pathlib.Path(__file__).resolve().parent.parent
os.chdir(ROOT)
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--worktree', action='store_true', help='build current compiler bytes and record their digest and base commit explicitly')
args = parser.parse_args()
GO = os.environ.get('GO_BIN', 'go')
env = dict(os.environ, GOTOOLCHAIN='local', GOMAXPROCS='4', GOWORK='off', GOPROXY='off')
def run(*args, **kwargs):
    return subprocess.check_output(args, env=env, **kwargs)
version = run(GO, 'version').decode().strip()
run(GO, 'mod', 'verify')
website_sha = run('git', 'rev-parse', 'HEAD').decode().strip()
catalog_data = catalog.load()
# Check both inventories before generating any website assets.
if args.worktree:
    compiler_source = worktree_compiler_source(ROOT)
    sha = compiler_source['baseCommit']
    compiler_digest = compiler_source['digest']
else:
    sha = catalog_data['compilerCommit']
    compiler_digest = verify_compiler_sources(ROOT, sha, catalog_data['compilerDigest'])
    compiler_source = dict(kind='commit', commit=sha, digest=compiler_digest, dirty=False)
goroot = run(GO, 'env', 'GOROOT').decode().strip()
license_path = go_license_path(goroot)
catalog.write_index(catalog_data)
context = dependency_context(ROOT)
roots_file = ROOT / context['path']
# Reject fixture drift even when an older module remains in a local cache.
expected_modules = {name: json.loads(run(GO, 'list', '-m', '-json', name))['Version'] for name in ['github.com/goxide-lang/std', 'github.com/goxide-lang/serde']}
for fixture_path in ['website/validation/package-files/example', 'examples/reexports']:
    fixture_modules = json.loads(run(GO, 'mod', 'edit', '-json', cwd=ROOT/fixture_path))
    assert {item['Path']: item['Version'] for item in fixture_modules['Require']} == expected_modules, f'{fixture_path}: public module selections differ from compiler'
    assert not fixture_modules.get('Replace'), f'{fixture_path}: fixture must not replace public dependencies'
patterns = ['std', *sorted({root['module'] + '/...' for root in context['roots'].values() if root['source'] == 'module'})]
exports = ROOT/'internal/typecheck/browserexports'
exports.mkdir(exist_ok=True)
for old in exports.glob('*.a.gz'): old.unlink()
# Use native export archives as compiler metadata, never execute user Go.
raw = run(GO, 'list', '-trimpath', '-export', '-deps', '-json', *patterns).decode()
decoder = json.JSONDecoder(); closure = []; export_records = []
origins = {'unsafe': dict(module='', standard=True, exportSha256='')}
while raw.strip():
    p, end = decoder.raw_decode(raw.lstrip()); raw = raw.lstrip()[end:]
    export_records.append(p)
    if p.get('Export'):
        data = run(GO, 'tool', 'pack', 'p', p['Export'], '__.PKGDEF')
        header = f"{'__.PKGDEF':<16}{0:<12}{0:<6}{0:<6}{'644':<8}{len(data):<10}`\n".encode()
        archive = b'!<arch>\n' + header + data + (b'\n' if len(data)%2 else b'')
        (exports/(p['ImportPath'].replace('/', '_')+'.a.gz')).write_bytes(gzip.compress(archive, mtime=0))
        origins[p['ImportPath']] = dict(module=p.get('Module', {}).get('Path', ''), standard=bool(p.get('Standard')),
            exportSha256=hashlib.sha256(archive).hexdigest())
        closure.append(p['ImportPath'])
(exports/'origins.json').write_text(json.dumps(origins, sort_keys=True, indent=2)+'\n')
context = dependency_context(ROOT, origins)
packages = sorted({binding['package'] for binding in context['bindings'] if origins[binding['package']]['exportSha256']})
for binding in context['bindings']:
    origin = origins[binding['package']]
    assert origin['standard'] == (binding['source'] == 'toolchain') and origin['module'] == binding['module'], 'explicit root differs from actual Go package owner: ' + binding['path']
dist = ROOT/'website/dist'
# Vite produces the single client-route shell and lazy editor/worker assets.
run('npm', 'run', 'build', '--prefix', str(ROOT/'website'))
static_files = ['404.html', 'worker.js', 'proc-macro-worker.js', 'proc-macro-wasi.js', '_headers', '.nojekyll']
assert set(p.name for p in (ROOT/'website/static').iterdir()) == set(static_files), 'unexpected static input'
for name in static_files:
    shutil.copyfile(ROOT/'website/static'/name, dist/name)
index = (dist/'index.html').read_text()
for page in ['rfc.html', 'playground.html', 'local.html']:
    (dist/page).write_text(index)
for page in ['learn', 'docs', 'design']:
    (dist/page).mkdir()
    (dist/page/'index.html').write_text(index)
# Record the precise Vite output graph; audit rejects extras and byte changes.
frontend_assets = {}
for asset in sorted((dist/'assets').rglob('*')):
    if asset.is_file():
        assert re.fullmatch(r'[A-Za-z0-9_.-]+\.(?:js|css|ttf)', asset.name), 'unexpected frontend output'
        frontend_assets[asset.relative_to(dist).as_posix()] = hashlib.sha256(asset.read_bytes()).hexdigest()
(dist/'frontend-assets.json').write_text(json.dumps(dict(websiteCommit=website_sha, assets=frontend_assets), indent=2)+'\n')
(dist/'THIRD-PARTY-LICENSES.txt').write_bytes(run('node', str(ROOT/'website/frontend-licenses.mjs')))
(ROOT/'bin').mkdir(exist_ok=True)
run(GO, 'build', '-o', str(ROOT/'bin/goxide'), './cmd/goxide')
examples = []
for entry in catalog_data['examples']:
    name, title, description = entry['id'], entry['title'], entry['description']
    path = ROOT/'website/examples'/f'{name}.gox'
    source = path.read_bytes()
    # Match the owner-free browser input, rather than the repository's module.
    with standalone_playground(source) as input_path:
        generated_path = input_path.with_suffix('.go')
        run(str(ROOT/'bin/goxide'), 'transpile', '--roots', str(roots_file), '-o', str(generated_path), str(input_path))
        generated = generated_path.read_text()
        if name in ['result','go_result','serde_option']:
            assert 'github.com/goxide-lang/std/sum' in generated
            families = re.findall(r'^type (?:Option|Result)\[[^\n]+', generated, re.MULTILINE)
            assert all(re.search(r'= \w+\.(?:Option|Result)\[', line) for line in families), 'sum families must use shared std types, not copied definitions'
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
    assert all(file['source'].startswith('package main\n') for file in generated_files), 'package output must contain complete Go files'
    stdout = run(str(ROOT/'bin/goxide'), 'run', '--go', GO, '--roots', str(roots_file), str(project)).decode()
    assert stdout == '42 GOXIDE Item { Value: 21 }\nAda Ada\n', 'package example runtime output changed'
    package_examples = [dict(id='package_files', title='同包多文件：宏、派生与静态协议',
                             description='五个可编辑 .gox 文件属于同一个包；每个文件保留独立 use 作用域，共享宏、派生，以及分文件声明的 trait、impl 和泛型函数。',
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
source_packages = dependency_source_packages(context, [
    *[example['go'] for example in examples],
    *[file['source'] for example in package_examples for file in example['generatedFiles']],
    *[file['go'] for file in cli_example['files']],
])
dependencies = dependency_sources(sorted(set(packages) & set(source_packages)), export_records, goroot=goroot,
                                  toolchain_version=run(GO, 'env', 'GOVERSION').decode().strip(),
                                  exports=exports)
for dependency in dependencies:
    dependency['staticPaths'] = [binding['path'] for binding in context['bindings'] if binding['package'] == dependency['package']]
# Negative examples are genuine CLI rejections; only sanitized diagnostics are public.
checks = []
for name in sorted({n for c in catalog_data['items'] for n in c['checks']}):
    path = ROOT/'website/checks/catalog'/f'{name}.gox'
    source = path.read_bytes()
    with standalone_playground(source) as input_path:
        result = subprocess.run([str(ROOT/'bin/goxide'), 'transpile', '--roots', str(roots_file), '-o', str(input_path.with_suffix('.go')), str(input_path)], env=env, capture_output=True, text=True)
    assert result.returncode != 0, f'{name}: expected rejection'
    diagnostic = result.stderr.strip().replace(str(input_path), 'playground.gox')
    assert diagnostic and '/workspace/' not in diagnostic
    checks.append(dict(id=name, source=source.decode(), diagnostic=diagnostic))
cli_options_example = project_snapshots.cli(ROOT, GO, run)
minimal_example = project_downloads.minimal(ROOT, GO, run)
reexports_download = project_downloads.reexports(ROOT, dist, GO, run, compiler_source)
public_catalog = catalog.export(catalog_data, checks)
tutorial_content = tutorials.build(ROOT, dist, GO, run, compiler_source)
documentation = docs_content.build(ROOT, GO, run)
eh = hashlib.sha256()
for archive in sorted(exports.glob('*.a.gz')):
    eh.update(archive.name.encode()); eh.update(archive.read_bytes())
export_target = json.loads(run(GO, 'env', '-json', 'GOOS', 'GOARCH'))
wasm_flags = ['-trimpath', '-buildvcs=false', f"-ldflags=-s -w -buildid= -X main.exportOS={export_target['GOOS']} -X main.exportArch={export_target['GOARCH']}"]
public_libraries = []
for module in ['github.com/goxide-lang/std', 'github.com/goxide-lang/serde']:
    selected = json.loads(run(GO, 'list', '-m', '-json', module))
    assert not selected.get('Replace'), 'public dependency must not use a local replacement'
    metadata = json.loads(run(GO, 'mod', 'download', '-json', module+'@'+selected['Version']))
    commit = metadata.get('Origin', {}).get('Hash', '')
    assert re.fullmatch('[0-9a-f]{40}', commit) and metadata.get('Sum') and metadata.get('GoModSum'), 'verified cached module origin is required'
    assert metadata['Path'] == module and metadata['Version'] == selected['Version']
    public_libraries.append(dict(module=module, commit=commit, version=selected['Version'], sum=metadata['Sum'], goModSum=metadata['GoModSum']))
manifest = dict(pipeline='parser-expand-resolve-check-lower', enumRepresentation='sealed-interface', compilerSource=compiler_source, catalogCompilerCommit=catalog_data['compilerCommit'], publicLibraries=public_libraries, wasmBuildFlags=wasm_flags, exportTrimpath=True, commit=sha, websiteCommit=website_sha, catalog=public_catalog, compilerDigest=compiler_digest, exportsDigest=eh.hexdigest(), exportTarget=export_target, toolchain=version, packages=packages,
                dependencies=dependencies, dependencyContext=context, dependencySourceScope='declared Go module trees and direct imports of published examples; complete native-selected GoFiles/CgoFiles; paths relative to named module or Go distribution',
                exportOrigins=origins, exportOriginsSha256=hashlib.sha256((exports/'origins.json').read_bytes()).hexdigest(),
                importClosure=sorted(closure), maxSourceBytes=32768, maxFiles=16, compileTimeoutMs=10000,
                examples=examples, packageExamples=package_examples, cliExamples=[cli_example], projectExamples=[minimal_example,reexports_download,cli_options_example],
                tutorials=tutorial_content, docs=documentation, design=docs_content.build_design(ROOT))
wasm_env = dict(env, GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
subprocess.run([GO, 'build', *wasm_flags, '-o', str(dist/'compiler.wasm'), './cmd/goxide-playground'], env=wasm_env, check=True)
verify_compiler_source(ROOT, compiler_source)
manifest['wasmSha256'] = hashlib.sha256((dist/'compiler.wasm').read_bytes()).hexdigest()
(dist/'examples.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
shutil.copyfile(pathlib.Path(goroot)/'lib/wasm/wasm_exec.js', dist/'wasm_exec.js')
shutil.copyfile(license_path, dist/'GO-LICENSE.txt')
embed = ROOT/'cmd/goxide-web/dist'
if embed.exists(): shutil.rmtree(embed)
shutil.copytree(dist, embed)
print(json.dumps(dict(compilerSource=compiler_source, websiteCommit=website_sha, wasmBytes=(dist/'compiler.wasm').stat().st_size, examples=len(examples))))
