"""Run the procedural-macro tutorial and freeze its browser distribution."""
import hashlib
import json
import pathlib
import shutil
import subprocess
import tempfile
import tomllib
import zipfile

from tutorials import archive, generated_files, source_files


OUTPUT = '42 42 Order\n'


def build(root, dist, go, run, compiler_source):
    root, dist = pathlib.Path(root), pathlib.Path(dist)
    template = root / 'website/tutorial-projects/procedural'
    original = source_files(template)
    cli = str(root / 'bin/goxide')
    sdk = json.loads(run(go, 'list', '-m', '-json', 'github.com/goxide-lang/syntax'))
    assert f'version = "{sdk["Version"]}"' in (template / 'macros/goxide.toml').read_text(), 'tutorial SDK pin differs from compiler'

    def command(project, *args):
        return run(cli, args[0], '--go', go, *args[1:], cwd=project).decode()

    generation_stdout = {}
    def generate(project, output):
        generation_stdout[output] = command(project, 'gen', '--offline', '--locked', '-o', str(output), '.').replace(str(output), '../generated')
        return generated_files(output)

    def check(project, output):
        command(project, 'build', '--offline', '-o', str(output), '.')
        locked = (project / 'goxide.lock').read_bytes()
        assert run(str(output), cwd=project).decode() == OUTPUT, 'procedural tutorial binary output differs'
        assert command(project, 'run', '--offline', '--locked', '.') == OUTPUT, 'procedural tutorial output differs'
        tests = command(project, 'test', '--offline', '--locked', '.')
        assert 'ok  \tplayground\t' in tests, 'procedural tutorial test did not execute'
        assert (project / 'goxide.lock').read_bytes() == locked, 'procedural tutorial changed its lock'
        return tests

    with tempfile.TemporaryDirectory(prefix='goxide-procedural-tutorial-') as temporary:
        base = pathlib.Path(temporary).resolve()
        project = base / 'project'
        shutil.copytree(template, project)
        tests = check(project, base / 'procedural')
        generated = generate(project, base / 'generated')
        assert generate(project, base / 'generated-repeat') == generated, 'procedural tutorial generation changed bytes'
        files = [*original, dict(path='goxide.lock', source=(project / 'goxide.lock').read_text())]
        commands = [
            dict(command='goxide build --offline .', stdout=''),
            dict(command='goxide gen --offline --locked -o ../generated .', stdout=generation_stdout[base / 'generated']),
            dict(command='goxide run --offline --locked .', stdout=OUTPUT),
            dict(command='goxide test --offline --locked .', stdout=tests),
        ]
        identity = (compiler_source.get('commit') or compiler_source['baseCommit']) + '; SHA-256 ' + compiler_source['digest']
        readme = ('# Procedural macros: function, attribute and derive\n\n'
                  'The consumer and macro producer are separate Goxide modules. '
                  'The macro module uses the published syntax SDK and compiles to restricted WASI.\n\n'
                  'Compiler: ' + identity + '\n\nVerified toolchain: ' + run(go, 'version').decode().strip() + '\n\n'
                  '```sh\ngoxide build .\ngoxide run --offline --locked .\ngoxide test --offline --locked .\n'
                  'goxide gen --offline --locked -o ../generated .\n```\n\n'
                  'First build may download the pinned SDK; after moving the project, build without --locked '
                  'to record its new local path. Subsequent locked commands preserve that lock.\n\n'
                  'Expected output:\n\n```text\n' + OUTPUT + '```\n\n'
                  '`twice!` evaluates its expression once. `#[constant(expr)]` replaces a function body. '
                  '`#[derive(TypeName)]` parses a concrete type and adds its public typeName method.\n')
        files.append(dict(path='README.md', source=readme))
        download = archive(dist, 'procedural', files)
        replay = base / 'replay'
        with zipfile.ZipFile(dist / download['path']) as source:
            source.extractall(replay)
        check(replay, base / 'replayed-procedural')
        assert generate(replay, base / 'replayed-generated') == generated, 'relocated procedural generation changed bytes'
        assert all((replay / file['path']).read_text() == file['source'] for file in files if file['path'] != 'goxide.lock'), 'procedural replay changed author files'
    stage = dict(id='procedural', title='编写三类过程宏', description='把函数式、属性和 derive 宏写成独立模块，用真实项目运行和测试展开结果。',
                 files=files, generatedFiles=generated, commands=commands, stdout=OUTPUT, download=download)
    assert source_files(template) == original, 'procedural tutorial changed its author project'
    return stage


def browser(root, dist, go, run, catalog, bundle_tool, bundle_directory):
    """Give every editable input the same package identity and fixed sources."""
    cli = str(root / 'bin/goxide')
    context_path = root / 'website/validation/browser/goxide.toml'
    context = context_path.read_bytes()
    dependencies = tomllib.loads(context.decode())['dependencies']
    for alias in ['std', 'serde']:
        selected = json.loads(run(go, 'list', '-m', '-json', dependencies[alias]['go']))
        assert dependencies[alias]['version'] == selected['Version'] and not selected.get('Replace'), 'browser consumer differs from public ' + alias + ' pin'

    def command(project, *args, **kwargs):
        return run(cli, args[0], '--go', go, *args[1:], cwd=project, **kwargs).decode()

    with tempfile.TemporaryDirectory(prefix='goxide-browser-consumer-') as temporary:
        base = pathlib.Path(temporary).resolve()
        project = base / 'project'
        project.mkdir()
        (project / 'goxide.toml').write_bytes(context)
        shutil.copytree(root / 'website/tutorial-projects/procedural/macros', project / 'macros')

        def inputs(files):
            for previous in project.glob('*.gox'):
                previous.unlink()
            for file in files:
                (project / file['path']).write_text(file['source'])

        def generate(name):
            output, repeated = base / name, base / (name + '-repeat')
            command(project, 'gen', '--offline', '--locked', '-o', str(output), '.')
            command(project, 'gen', '--offline', '--locked', '-o', str(repeated), '.')
            generated = generated_files(output)
            assert generated_files(repeated) == generated, name + ': generation changed bytes'
            return [dict(path=file['path'].removeprefix('module/'), source=file['source'])
                    for file in generated if file['path'].startswith('module/')
                    and '/' not in file['path'].removeprefix('module/') and file['path'].endswith('.go')]

        examples = {}
        for entry in catalog['examples']:
            name = entry['id']
            source = (root / 'website/examples' / (name + '.gox')).read_text()
            inputs([dict(path='playground.gox', source=source)])
            # Only the first build populates the dependency lock. Every later
            # source uses the same locked graph and exact package identity.
            if not examples:
                command(project, 'build', '--offline', '.')
                locked = (project / 'goxide.lock').read_bytes()
            user_files = generate(name)
            assert [file['path'] for file in user_files] == ['playground.go'], name + ': unexpected browser output files'
            stdout = command(project, 'run', '--offline', '--locked', '.')
            assert stdout == entry['expectedStdout'], name + ': runtime output changed'
            assert (project / 'goxide.lock').read_bytes() == locked, name + ': locked graph changed'
            examples[name] = dict(source=source, go=user_files[0]['source'], stdout=stdout)
        fixture = root / 'website/validation/package-files/example'
        package_files = [dict(path=path.name, source=path.read_text()) for path in sorted(fixture.glob('*.gox'))]
        inputs(package_files)
        generated_package = generate('package_files')
        assert {pathlib.Path(file['path']).with_suffix('.go').name for file in package_files} <= {file['path'] for file in generated_package}
        package_stdout = command(project, 'run', '--offline', '--locked', '.')
        assert package_stdout == '42 GOXIDE Item { Value: 21 }\nAda Ada\n', 'package example runtime output changed'
        package_example = dict(id='package_files', title='同包多文件：宏、派生与静态协议',
                               description='五个可编辑 .gox 文件属于同一个包；每个文件保留独立 use 作用域，共享宏、派生，以及分文件声明的 trait、impl 和泛型函数。',
                               files=package_files, generatedFiles=generated_package, stdout=package_stdout)
        # The checked library exposes all three registrations, independently of
        # which consumer example was compiled last. Runtime roots are frozen too.
        run(str(bundle_tool), '-project', str(project), '-output', str(bundle_directory), '-go', go)
        assert (project / 'goxide.lock').read_bytes() == locked, 'browser macro snapshot changed its locked graph'
        checks = []
        for name in sorted({name for card in catalog['items'] for name in card['checks']}):
            source = (root / 'website/checks/catalog' / (name + '.gox')).read_text()
            inputs([dict(path='playground.gox', source=source)])
            try:
                command(project, 'gen', '--offline', '--locked', '-o', str(base / ('reject-' + name)), '.', stderr=subprocess.PIPE)
            except subprocess.CalledProcessError as error:
                diagnostic = error.stderr.decode().strip().replace('playground/playground.gox', 'playground.gox').replace(str(project), 'playground')
                assert diagnostic and str(base) not in diagnostic, name + ': diagnostic has a local path'
            else:
                raise AssertionError(name + ': expected rejection')
            checks.append(dict(id=name, source=source, diagnostic=diagnostic))

        # This edit introduces types absent from the selected package example.
        # Its exact native output proves the browser is not limited to the
        # dependency instances generated when the fixed snapshot was built.
        edit_source = (root / 'website/validation/browser/edits/serde_new_types.gox').read_text()
        inputs([dict(path='playground.gox', source=edit_source)])
        edit_files = generate('edit_serde_new_types')
        assert [file['path'] for file in edit_files] == ['playground.go']
        edit_stdout = command(project, 'run', '--offline', '--locked', '.')
        assert edit_stdout == '{"Counts":[1,2]} None\n{"Limit":null,"Counts":[1,2]} None\n{"Limit":7,"Counts":[1,2]} Some(7)\n'
        assert (project / 'goxide.lock').read_bytes() == locked, 'editing changed the locked browser graph'
        editing_checks = [dict(id='serde_new_types', example='serde_option', source=edit_source,
                               go=edit_files[0]['source'], stdout=edit_stdout)]

    snapshot_bytes = (bundle_directory / 'manifest.json').read_bytes()
    snapshot = json.loads(snapshot_bytes)
    assert snapshot['package'] == 'playground' and snapshot['artifacts'], 'empty or mismatched browser macro bundle'
    assets = []
    for artifact in snapshot['artifacts']:
        digest = artifact['sha256']
        source = bundle_directory / 'proc-macros' / (digest + '.wasm')
        assert hashlib.sha256(source.read_bytes()).hexdigest() == digest, 'macro executable digest differs'
        destination = dist / 'proc-macros' / source.name
        destination.parent.mkdir(exist_ok=True)
        shutil.copyfile(source, destination)
        assets.append(dict(path='proc-macros/' + source.name, sha256=digest,
                           module=artifact['library']['module'], package=artifact['library']['package'],
                           entries=[dict(name=entry['name'], kind=entry['kind']) for entry in artifact['library']['entries']]))
    bundle = dict(sha256=hashlib.sha256(snapshot_bytes).hexdigest(), package=snapshot['package'], artifacts=assets,
                  consumer=dict(path=context_path.relative_to(root).as_posix(), source=context.decode(), sha256=hashlib.sha256(context).hexdigest()))
    return examples, package_example, checks, bundle, editing_checks
