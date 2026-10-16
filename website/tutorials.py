"""Build the four tutorial projects and replay their downloadable source archives."""
import hashlib
import json
import os
import pathlib
import shlex
import shutil
import subprocess
import tempfile
import zipfile


OUTPUT = 'order 1001: pending\norder 1002: paid 42\norder 1003: shipped TRACK-7\n'
PRETTY_OUTPUT = ''.join('[orders] ' + line + '\n' for line in OUTPUT.splitlines())
STAGES = [
    ('go', '从熟悉的 Go 开始', '用 Go 的结构体、状态常量和 switch 写一个订单状态 CLI。'),
    ('mixed', '在 Go 项目中加入 Goxide', '保留 go.mod、Go 入口和 Go 测试，用 enum 与 match 替换状态表示。'),
    ('gox', '用 Goxide 管理整个项目', '业务和测试都使用 .gox，以 goxide.toml 声明模块、包名映射和命令入口。'),
    ('features', '按功能组合项目', '切换 pretty、trace 和 report，并向本地可选 reporter 模块转发 feature。'),
]
PRESETS = [
    dict(id='default', title='默认', flags=[], features=['pretty'], expected=PRETTY_OUTPUT),
    dict(id='minimal', title='最小', flags=['--no-default-features'], features=[], expected=OUTPUT),
    dict(id='trace', title='追踪', flags=['--features=trace'], features=['pretty', 'trace'],
         expected='trace: 3 orders\n' + PRETTY_OUTPUT),
    dict(id='report', title='报告', flags=['--features=report'], features=['pretty', 'report'],
         expected=PRETTY_OUTPUT + 'report: [3 orders]\n'),
]


def source_files(directory):
    return [dict(path=path.relative_to(directory).as_posix(), source=path.read_bytes().decode())
            for path in sorted(directory.rglob('*')) if path.is_file()]


def generated_files(directory):
    return [dict(path=path.relative_to(directory).as_posix(), source=path.read_bytes().decode())
            for path in sorted(directory.rglob('*'))
            if path.is_file() and (path.suffix == '.go' or path.name == 'go.mod')]


def archive(dist, stage, files):
    path = dist / ('tutorial-' + stage + '.zip')
    with zipfile.ZipFile(path, 'w', compression=zipfile.ZIP_DEFLATED) as output:
        for file in sorted(files, key=lambda item: item['path']):
            entry = zipfile.ZipInfo(file['path'], date_time=(2026, 1, 1, 0, 0, 0))
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = 0o100644 << 16
            output.writestr(entry, file['source'].encode())
    return dict(path=path.name, sha256=hashlib.sha256(path.read_bytes()).hexdigest(),
                files=sorted(file['path'] for file in files))


def build(root, dist, go, run, compiler_source):
    root, dist = pathlib.Path(root), pathlib.Path(dist)
    templates = root / 'website/tutorial-projects'
    original = source_files(templates)
    cli = str(root / 'bin/goxide')
    go = shutil.which(go) or go
    # go generate must find this checkout's CLI and the selected Go toolchain.
    env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOPROXY='off', GOSUMDB='off',
               GOMAXPROCS='4', PATH=os.pathsep.join([str(root / 'bin'), str(pathlib.Path(go).parent), os.environ['PATH']]))
    for name in ['GOOS', 'GOARCH', 'GOFLAGS', 'GOXIDE_TARGET_DIR']:
        env.pop(name, None)
    version = run(go, 'version').decode().strip()
    go_version = run(go, 'env', 'GOVERSION').decode().strip()
    if compiler_source['kind'] == 'worktree':
        compiler = ('Compiler worktree based on ' + compiler_source['baseCommit']
                    + '; SHA-256 ' + compiler_source['digest'])
    else:
        compiler = 'Compiler: https://github.com/goxide-lang/goxide/tree/' + compiler_source['commit']

    def invoke(project, args, reject=None):
        result = subprocess.run(args, cwd=project, env=env, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True)
        if reject is not None:
            assert result.returncode != 0 and reject in result.stdout, (
                'expected rejection ' + reject + ': ' + result.stdout)
        elif result.returncode:
            raise RuntimeError(shlex.join(args) + '\n' + result.stdout)
        return result.stdout

    def command(project, args):
        stdout = invoke(project, args)
        shown = ['goxide' if item == cli else 'go' if item == go else item for item in args]
        if '--go' in shown:
            index = shown.index('--go')
            del shown[index:index + 2]
        # Export prints an absolute destination; show its path from the source root.
        stdout = stdout.replace(str(project.resolve()) + '/', '')
        stdout = stdout.replace(str(project.parent.resolve()) + '/', '../')
        return dict(command=shlex.join(shown), stdout=stdout)

    def export(project, destination, flags):
        args = [cli, 'gen', '--go', go, '--offline', '--locked', *flags]
        result = command(project, [*args, '-o', str(destination), '.'])
        return result, generated_files((project / destination).resolve())

    stages = []
    with tempfile.TemporaryDirectory(prefix='goxide-tutorials-') as temporary:
        base = pathlib.Path(temporary).resolve()
        for stage_id, title, description in STAGES:
            project = base / stage_id
            shutil.copytree(templates / stage_id, project)
            files = source_files(project)
            commands, generated, presets = [], [], []
            if stage_id in ['go', 'mixed']:
                if stage_id == 'mixed':
                    commands.append(command(project, [go, 'generate', './...']))
                    generated = [dict(path=path.name, source=path.read_text())
                                 for path in sorted(project.glob('*_gox.go'))]
                    assert generated, 'mixed tutorial generated no Go'
                    invoke(project, [go, 'generate', './...'])
                    assert generated == [dict(path=path.name, source=path.read_text())
                                         for path in sorted(project.glob('*_gox.go'))], 'mixed generation changed bytes'
                commands.append(command(project, [go, 'build', '-o', '../orders', '.']))
                commands.append(command(project, [go, 'run', '.']))
                assert commands[-1]['stdout'] == OUTPUT, stage_id + ': order output changed'
                stdout = commands[-1]['stdout']
                assert invoke(project, [str(base / 'orders')]) == OUTPUT, 'built Go binary output changed'
                commands.append(command(project, [go, 'test', '-count=1', './...']))
                assert all((project / file['path']).read_bytes() == file['source'].encode() for file in files)
            else:
                configurations = PRESETS if stage_id == 'features' else [
                    dict(id='default', title='默认', flags=[], features=[], expected=OUTPUT)]
                # Warm every dependency selection before freezing the shared lock.
                warm_commands, replay_commands = {}, []
                for preset in configurations:
                    flags = preset['flags']
                    warm_build = command(project, [cli, 'build', '--go', go, '--offline', *flags, '.'])
                    warm_run = command(project, [cli, 'run', '--go', go, '--offline', *flags, '.'])
                    assert warm_run['stdout'] == preset['expected']
                    warm_commands[preset['id']] = (warm_build, warm_run)
                    if preset['id'] == 'default':
                        download_lock = (project / 'goxide.lock').read_bytes()
                locked = (project / 'goxide.lock').read_bytes()
                for preset in configurations:
                    flags = preset['flags']
                    destination = '../' + stage_id + '-generated-' + preset['id']
                    gen_command, outputs = export(project, destination, flags)
                    _, repeated = export(project, destination + '-repeat', flags)
                    assert outputs == repeated, stage_id + ': generation changed bytes'
                    (base / 'orders').unlink(missing_ok=True)
                    build_command = command(project, [cli, 'build', '--go', go, '--offline', '--locked',
                                                      *flags, '-o', '../orders', '.'])
                    run_command = command(project, [cli, 'run', '--go', go, '--offline', '--locked', *flags, '.'])
                    assert run_command['stdout'] == preset['expected'], stage_id + ': preset output changed'
                    assert invoke(project, [str(base / 'orders')]) == preset['expected'], 'built Goxide binary output changed'
                    test_command = command(project, [cli, 'test', '--go', go, '--offline', '--locked', *flags, '.'])
                    assert 'ok  \texample.org/orders/cmd/orders\t' in test_command['stdout'], 'Goxide test did not execute tutorial tests'
                    dependencies = [file['source'].splitlines()[0].removeprefix('module ')
                                    for file in outputs if file['path'].startswith('deps/') and file['path'].endswith('/go.mod')]
                    assert dependencies == (['example.org/reporter'] if preset['id'] == 'report' else []), 'optional dependency selection changed'
                    warm_build, warm_run = warm_commands[preset['id']]
                    presets.append(dict(id=preset['id'], title=preset['title'], features=preset['features'],
                                        command=warm_run['command'], stdout=warm_run['stdout'],
                                        files=files, generatedFiles=outputs, dependencies=dependencies))
                    replay_commands.extend([warm_build['command'], run_command['command']])
                    if preset['id'] == 'default':
                        commands = [warm_build, gen_command, build_command, run_command, test_command]
                        generated, stdout = outputs, run_command['stdout']
                    assert (project / 'goxide.lock').read_bytes() == locked, 'locked execution changed the lock'
                # Package the actual default lock, before any location-specific path plan.
                files.append(dict(path='goxide.lock', source=download_lock.decode()))
                if stage_id == 'features':
                    invoke(project, [cli, 'run', '--go', go, '--offline', '--features=unknown', '.'], 'unknown feature')
                    # An absent source proves the disabled optional module is not read.
                    absent = base / 'features-without-reporter'
                    shutil.copytree(templates / stage_id, absent)
                    shutil.rmtree(absent / 'reporter')
                    assert invoke(absent, [cli, 'run', '--go', go, '--offline', '.']) == PRETTY_OUTPUT
                    invoke(absent, [cli, 'run', '--go', go, '--offline', '--features=report', '.'], 'reporter')
                    # Remove only the forwarding edge: the optional module still activates,
                    # but its own pretty feature must now remain off.
                    unforwarded = base / 'features-without-forwarding'
                    shutil.copytree(templates / stage_id, unforwarded)
                    manifest = unforwarded / 'goxide.toml'
                    manifest.write_text(manifest.read_text().replace('"dep:reporter", "reporter/pretty"', '"dep:reporter"'))
                    assert invoke(unforwarded, [cli, 'run', '--go', go, '--offline', '--features=report', '.']) == PRETTY_OUTPUT + 'report: 3 orders\n'

            readme = ('# Orders tutorial: ' + stage_id + '\n\n' + description + '\n\n'
                      + ('Use Go 1.27+.' if stage_id == 'go' else
                         'Use the matching goxide CLI and ' + go_version + ' (exact version, including the patch).')
                      + ' Extract every file together.\n\n'
                      + compiler + '\n\nVerified toolchain: ' + version + '\n\n'
                      + '```sh\n' + '\n'.join(item['command'] for item in commands) + '\n```\n\n'
                      + 'Expected program output:\n\n```text\n' + stdout + '```\n')
            if stage_id == 'features':
                readme += ('\nThe archive contains the default build lock. After moving this local-path project, '
                           + 'or activating report for the first time, run build without --locked to record '
                           + 'the module plan at the current location. Then use --locked --offline there.\n'
                           + '\nFeature presets:\n\n```sh\n'
                           + '\n'.join(replay_commands) + '\n```\n'
                           + '\n`report` activates `dep:reporter` and forwards `reporter/pretty`. '
                           + 'The local reporter is a separate Goxide module in `reporter/`; no workspace is needed.\n')
            files.append(dict(path='README.md', source=readme))
            download = archive(dist, stage_id, files)
            # Exercise the downloadable bytes at a new location with fresh Goxide artifacts.
            replay = base / ('replay-' + stage_id)
            replay.mkdir()
            with zipfile.ZipFile(dist / download['path']) as source:
                source.extractall(replay)
            if stage_id in ['go', 'mixed']:
                if stage_id == 'mixed':
                    invoke(replay, [go, 'generate', './...'])
                    assert generated == [dict(path=path.name, source=path.read_text())
                                         for path in sorted(replay.glob('*_gox.go'))], 'relocated native generation changed bytes'
                invoke(replay, [go, 'build', '-o', '../replayed-orders', '.'])
                assert invoke(replay, [go, 'run', '.']) == stdout
                invoke(replay, [go, 'test', '-count=1', './...'])
            else:
                for preset in configurations:
                    flags = ['--go', go, '--offline', *preset['flags']]
                    previous_lock = json.loads((replay / 'goxide.lock').read_text())
                    invoke(replay, [cli, 'build', *flags, '.'])
                    replay_lock = (replay / 'goxide.lock').read_bytes()
                    current_lock = json.loads(replay_lock)
                    for field in ['git', 'sums', 'plans']:
                        assert all(current_lock[field].get(key) == value for key, value in previous_lock[field].items()), 'relocation changed an existing dependency selection'
                    flags.append('--locked')
                    (base / 'replayed-orders').unlink(missing_ok=True)
                    invoke(replay, [cli, 'build', *flags, '-o', '../replayed-orders', '.'])
                    assert invoke(replay, [cli, 'run', *flags, '.']) == preset['expected']
                    invoke(replay, [cli, 'test', *flags, '.'])
                    assert (replay / 'goxide.lock').read_bytes() == replay_lock, 'locked replay changed the relocated lock'
            assert all((replay / file['path']).read_bytes() == file['source'].encode() for file in files
                       if file['path'] != 'goxide.lock'), 'replay changed downloaded sources or author metadata'
            stage = dict(id=stage_id, title=title, description=description, files=files,
                         generatedFiles=generated, commands=commands, stdout=stdout, download=download)
            if stage_id == 'features':
                stage['presets'] = presets
            stages.append(stage)
    assert source_files(templates) == original, 'tutorial build changed its author projects'
    return dict(stages=stages)
