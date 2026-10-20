"""Verified project snapshots and an independently replayed directory archive."""
import hashlib, json, pathlib, tempfile, zipfile


def reexports(ROOT, dist, GO, run, compiler_source):
    project = ROOT/'examples/reexports'
    files = [dict(path=path.relative_to(project).as_posix(), source=path.read_text())
             for path in sorted(project.rglob('*')) if path.is_file() and path.name != 'README.md']
    assert {file['path'] for file in files} == {
        'go.mod', 'go.sum', 'goxide.roots.toml', 'main.gox', 'api/api.gox',
        'impl/impl.go', 'impl/macros.gox'}, 'directory download source inventory changed'
    if compiler_source['kind'] == 'worktree':
        compiler = 'Compiler worktree based on ' + compiler_source['baseCommit'] + '; SHA-256 ' + compiler_source['digest']
    else:
        compiler = 'Compiler: https://github.com/goxide-lang/goxide/tree/' + compiler_source['commit']
    go_version = run(GO, 'env', 'GOVERSION').decode().strip()
    readme = ('# Reproduce the reexports directory example\n\n'
              'Use the matching goxide CLI and ' + go_version + ' (exact version, including the patch). Extract every file together. '
              'The directory uses go.mod and goxide.roots.toml; no build scripts are required.\n\n'
              + compiler + '\n\n'
              + 'Verified toolchain: ' + run(GO, 'version').decode().strip() + '\n\n'
              + '```sh\ngoxide build -o ../reexports .\ngoxide run .\n```\n\n'
              + 'Expected output:\n\n```text\nshared 2 2 true\n42 2 7\n```\n')
    files.append(dict(path='README.md', source=readme))
    path = dist/'project-reexports.zip'
    with zipfile.ZipFile(path, 'w', compression=zipfile.ZIP_DEFLATED) as output:
        for file in files:
            entry = zipfile.ZipInfo(file['path'], date_time=(2026, 1, 1, 0, 0, 0))
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = 0o100644 << 16
            output.writestr(entry, file['source'].encode())
    stdout = 'shared 2 2 true\n42 2 7\n'
    cli = str(ROOT/'bin/goxide')
    # Replay the actual downloadable bytes after relocation into a fresh directory.
    with tempfile.TemporaryDirectory(prefix='goxide-download-replay-') as temporary:
        replay = pathlib.Path(temporary)/'source'
        replay.mkdir()
        with zipfile.ZipFile(path) as source:
            source.extractall(replay)
        metadata = {name: (replay/name).read_bytes() for name in ['go.mod', 'go.sum', 'goxide.roots.toml']}
        run(cli, 'build', '--go', GO, '-o', '../reexports', '.', cwd=replay)
        assert run(cli, 'run', '--go', GO, '.', cwd=replay).decode() == stdout, 'extracted directory output differs'
        assert all((replay/name).read_bytes() == data for name, data in metadata.items()), 'replay changed source dependency metadata'
    download = dict(path=path.name, sha256=hashlib.sha256(path.read_bytes()).hexdigest(),
                    files=[file['path'] for file in files])
    return dict(id='reexports-download', title='目录项目：完整源码下载', files=files,
                runs=[dict(command='goxide run .', stdout=stdout)], download=download,
                note='包含同一目录项目的 Go 模块配置、显式根映射、Goxide 与 Go 源码。网站构建会解压实际 ZIP，在新目录执行 build、run，并核对依赖配置保持不变。')


def minimal(ROOT, GO, run):
    fixture = ROOT/'internal/compile/testdata/p5'
    versions = {marker: json.loads(run(GO, 'list', '-m', '-json', module))['Version']
                for marker, module in [('@STD_VERSION@', 'github.com/goxide-lang/std'),
                                       ('@SERDE_VERSION@', 'github.com/goxide-lang/serde')]}
    files = []
    for path in sorted(fixture.rglob('*')):
        if not path.is_file(): continue
        source = path.read_text()
        for marker, version in versions.items(): source = source.replace(marker, version)
        files.append(dict(path=path.relative_to(fixture).as_posix(), source=source))
    cli = str(ROOT/'bin/goxide')
    stdout = 'initialized\n18 42 5 true\n{"Value":7} shared\n'
    runs = [dict(command='goxide run --offline --locked app', stdout=stdout)]
    with tempfile.TemporaryDirectory(prefix='goxide-web-minimal-') as temporary:
        project = pathlib.Path(temporary)
        for file in files:
            target = project/file['path']; target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(file['source'])
        run(cli, 'build', '--go', GO, 'app', cwd=project)
        lock = project/'app/goxide.lock'
        locked = lock.read_bytes()
        run(cli, 'build', '--go', GO, '--offline', '--locked', 'app', cwd=project)
        assert lock.read_bytes() == locked, 'repeated project build changed its lock'
        assert run(cli, 'run', '--go', GO, '--offline', '--locked', 'app', cwd=project).decode() == stdout
        assert lock.read_bytes() == locked, 'project run changed its lock'
        run(cli, 'test', '--go', GO, '--offline', '--locked', 'app', cwd=project)
        assert lock.read_bytes() == locked, 'project test changed its lock'
    return dict(id='minimal-project', title='多包、path 依赖与公开 Go 依赖',
                files=files, runs=runs,
                note='复用已验收的 P5 工程，展示 app、kit、清单和源码。构建时真实执行 build、run、test，并确认重复构建和运行时锁文件字节不变；此快照展示文件与运行结果，不提供工程 ZIP。')
