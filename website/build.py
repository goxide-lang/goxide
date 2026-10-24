#!/usr/bin/env python3
"""Rebuild every published example and the browser compiler from this checkout."""
import argparse, gzip, hashlib, json, os, pathlib, re, shutil, subprocess, tempfile
import catalog, project_snapshots, project_downloads, tutorials, docs_content, procedural
from build_support import dependency_context, dependency_source_packages, dependency_sources, go_license_path, verify_compiler_sources, verify_compiler_source, worktree_compiler_source
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
# Reject fixture drift even when an older module remains in a local cache.
expected_modules = {name: json.loads(run(GO, 'list', '-m', '-json', name))['Version'] for name in ['github.com/goxide-lang/std', 'github.com/goxide-lang/serde']}
for fixture_path in ['website/validation/package-files/example', 'examples/reexports']:
    fixture_modules = json.loads(run(GO, 'mod', 'edit', '-json', cwd=ROOT/fixture_path))
    assert {item['Path']: item['Version'] for item in fixture_modules['Require']} == expected_modules, f'{fixture_path}: public module selections differ from compiler'
    assert not fixture_modules.get('Replace'), f'{fixture_path}: fixture must not replace public dependencies'
# Verify public provenance before running the expensive example and tutorial builds.
public_libraries = []
for module in ['github.com/goxide-lang/std', 'github.com/goxide-lang/serde', 'github.com/goxide-lang/syntax']:
    selected = json.loads(run(GO, 'list', '-m', '-json', module))
    assert not selected.get('Replace'), 'public dependency must not use a local replacement'
    metadata = json.loads(run(GO, 'mod', 'download', '-json', module+'@'+selected['Version']))
    commit = metadata.get('Origin', {}).get('Hash', '')
    assert re.fullmatch('[0-9a-f]{40}', commit) and metadata.get('Sum') and metadata.get('GoModSum'), 'verified cached module origin is required'
    assert metadata['Path'] == module and metadata['Version'] == selected['Version']
    public_libraries.append(dict(module=module, commit=commit, version=selected['Version'], sum=metadata['Sum'], goModSum=metadata['GoModSum']))
patterns = ['std', 'github.com/goxide-lang/syntax/...', *sorted({root['module'] + '/...' for root in context['roots'].values() if root['source'] == 'module'})]
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
run(GO, 'run', './scripts/build.go', '-o', str(ROOT/'bin/goxide'))
run(GO, 'run', './scripts/build.go', '-package', './website/macro_bundle.go', '-o', str(ROOT/'bin/website-macro-bundle'))
bundle_temporary = tempfile.TemporaryDirectory(prefix='goxide-web-macro-bundle-')
bundle_directory = pathlib.Path(bundle_temporary.name)
proc_tutorial = procedural.build(ROOT, dist, GO, run, compiler_source)
browser_examples, package_example, checks, macro_bundle, editing_checks = procedural.browser(
    ROOT, dist, GO, run, catalog_data, ROOT/'bin/website-macro-bundle', bundle_directory)
examples = [dict(id=entry['id'], title=entry['title'], description=entry['description'],
                 **browser_examples[entry['id']]) for entry in catalog_data['examples']]
package_examples = [package_example]
for example in examples:
    if example['id'] in ['result', 'go_result', 'serde_option']:
        generated = example['go']
        assert 'github.com/goxide-lang/std/sum' in generated
        families = re.findall(r'^type (?:Option|Result)\[[^\n]+', generated, re.MULTILINE)
        assert all(re.search(r'= \w+\.(?:Option|Result)\[', line) for line in families), 'sum families must use shared std types, not copied definitions'
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
cli_options_example = project_snapshots.cli(ROOT, GO, run)
minimal_example = project_downloads.minimal(ROOT, GO, run)
reexports_download = project_downloads.reexports(ROOT, dist, GO, run, compiler_source)
public_catalog = catalog.export(catalog_data, checks)
tutorial_content = tutorials.build(ROOT, dist, GO, run, compiler_source)
tutorial_content['stages'].append(proc_tutorial)
documentation = docs_content.build(ROOT, GO, run, compiler_source)
eh = hashlib.sha256()
for archive in sorted(exports.glob('*.a.gz')):
    eh.update(archive.name.encode()); eh.update(archive.read_bytes())
export_target = json.loads(run(GO, 'env', '-json', 'GOOS', 'GOARCH'))
wasm_flags = ['-trimpath', '-buildvcs=false', f"-ldflags=-s -w -buildid= -X main.exportOS={export_target['GOOS']} -X main.exportArch={export_target['GOARCH']}"]
manifest = dict(pipeline='parser-expand-resolve-check-lower', enumRepresentation='sealed-interface', compilerSource=compiler_source, catalogCompilerCommit=catalog_data['compilerCommit'], publicLibraries=public_libraries, wasmBuildFlags=wasm_flags, exportTrimpath=True, commit=sha, websiteCommit=website_sha, catalog=public_catalog, compilerDigest=compiler_digest, exportsDigest=eh.hexdigest(), exportTarget=export_target, toolchain=version, packages=packages,
                dependencies=dependencies, dependencyContext=context, dependencySourceScope='declared Go module trees and direct imports of published examples; complete native-selected GoFiles/CgoFiles; paths relative to named module or Go distribution',
                exportOrigins=origins, exportOriginsSha256=hashlib.sha256((exports/'origins.json').read_bytes()).hexdigest(),
                importClosure=sorted(closure), maxSourceBytes=32768, maxFiles=16, compileTimeoutMs=70000,
                macroBundle=macro_bundle, editingChecks=editing_checks,
                examples=examples, packageExamples=package_examples, cliExamples=[cli_example], projectExamples=[minimal_example,reexports_download,cli_options_example],
                tutorials=tutorial_content, docs=documentation, design=docs_content.build_design(ROOT))
wasm_env = dict(env, GOOS='js', GOARCH='wasm', CGO_ENABLED='0')
overlay = bundle_directory/'overlay.json'
overlay.write_text(json.dumps({'Replace': {str(ROOT/'cmd/goxide-playground/proc_bundle.json'): str(bundle_directory/'manifest.json')}}))
subprocess.run([GO, 'build', '-overlay', str(overlay), *wasm_flags, '-o', str(dist/'compiler.wasm'), './cmd/goxide-playground'], env=wasm_env, check=True)
verify_compiler_source(ROOT, compiler_source)
manifest['wasmSha256'] = hashlib.sha256((dist/'compiler.wasm').read_bytes()).hexdigest()
(dist/'examples.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+'\n')
shutil.copyfile(pathlib.Path(goroot)/'lib/wasm/wasm_exec.js', dist/'wasm_exec.js')
shutil.copyfile(license_path, dist/'GO-LICENSE.txt')
embed = ROOT/'cmd/goxide-web/dist'
if embed.exists(): shutil.rmtree(embed)
shutil.copytree(dist, embed)
bundle_temporary.cleanup()
print(json.dumps(dict(compilerSource=compiler_source, websiteCommit=website_sha, wasmBytes=(dist/'compiler.wasm').stat().st_size, examples=len(examples))))
