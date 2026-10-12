"""Complete source archives, replayed independently before publication."""
import hashlib, json, pathlib, tempfile, zipfile

def archive(dist, name, files, runs, GO, run, cli, prepare='goxide build --bin demo --locked .', builds=()):
    compiler = json.loads((dist.parent/'catalog-source.json').read_text())['compilerCommit']
    readme = ('# Reproduce this project\n\nUse the matching goxide CLI and Go 1.27+. '
              'Extract all files together and run these commands in this directory. '
              'The target directory is generated; no build scripts are required.\n\n'
              + 'Compiler: https://github.com/goxide-lang/goxide/tree/' + compiler + '\n\n'
              + 'Verified toolchain: ' + run(GO, 'version').decode().strip() + '\n\n```sh\n'
              + prepare + '  # first build may download pinned support dependencies\n'
              + '\n'.join([entry['command'] for entry in runs] + list(builds)) + '\n```\n')
    files = [*files, dict(path='README.md', source=readme)]
    path = dist/name
    with zipfile.ZipFile(path, 'w', compression=zipfile.ZIP_DEFLATED) as output:
        for file in files:
            entry = zipfile.ZipInfo(file['path'], date_time=(2026, 1, 1, 0, 0, 0))
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = 0o100644 << 16
            output.writestr(entry, file['source'].encode())
    # Test the actual downloadable bytes in a fresh directory, not the build fixture.
    with tempfile.TemporaryDirectory(prefix='goxide-download-replay-') as temporary:
        project = pathlib.Path(temporary)
        with zipfile.ZipFile(path) as source:
            source.extractall(project)
        locked = (project/'goxide.lock').read_bytes()
        args = prepare.split()[1:]
        run(cli, args[0], '--go', GO, *args[1:], cwd=project)
        assert (project/'goxide.lock').read_bytes() == locked, 'initial locked build changed download lock'
        for entry in runs:
            args = entry['command'].split()[1:]
            actual = run(cli, args[0], '--go', GO, *args[1:], cwd=project).decode()
            assert actual == entry['stdout'], name + ': extracted project output differs'
            assert (project/'goxide.lock').read_bytes() == locked, 'replay changed download lock'
        for command in builds:
            args = command.split()[1:]
            run(cli, args[0], '--go', GO, *args[1:], cwd=project)
            assert (project/'goxide.lock').read_bytes() == locked, 'library build changed download lock'
        assert not (project/'go.mod').exists() and not (project/'go.sum').exists()
    return dict(path=name, sha256=hashlib.sha256(path.read_bytes()).hexdigest(),
                files=[file['path'] for file in files])

def targets(ROOT, dist, GO, run):
    fixture = ROOT/'examples/project'
    names = ['goxide.toml', 'goxide.lock'] + [str(p.relative_to(fixture))
        for directory in ['src', 'examples'] for p in sorted((fixture/directory).glob('*.gox'))]
    files = [dict(path=name, source=(fixture/name).read_text()) for name in names]
    runs = [dict(command='goxide run --bin hello --offline --locked .', stdout='Hello, goxide!\n'),
            dict(command='goxide run --bin hello --no-default-features --offline --locked .', stdout='hello\n'),
            dict(command='goxide run --bin inspect --offline --locked .', stdout='pretty: true\n'),
            dict(command='goxide run --example greet --offline --locked .', stdout='example: Hello, goxide!\n')]
    download = archive(dist, 'project-targets.zip', files, runs, GO, run, str(ROOT/'bin/goxide'),
                       prepare='goxide build --bin hello --locked .',
                       builds=('goxide build --lib --offline --locked .',))
    return dict(id='targets', title='lib、双 bin 与 example：完整工程', files=files, runs=runs,
                download=download, note='下载包含完整清单、锁文件和全部源码。Go 支持库按锁文件解析；首次在本机先执行 goxide build --bin hello --locked . 下载依赖，再使用下列 offline 命令。')
