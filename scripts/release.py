#!/usr/bin/env python3
"""Build one native CLI archive and run the unpacked binary against a real project."""
import gzip
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
os.chdir(ROOT)
tag = os.environ['RELEASE_TAG']
env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off')


def run(*args, **kwargs):
    result = subprocess.run(args, env=env, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, **kwargs)
    if result.returncode:
        print(result.stdout, flush=True)
        result.check_returncode()
    return result.stdout


goos, goarch, goversion = run('go', 'env', 'GOOS', 'GOARCH', 'GOVERSION').split()
target = f'{goos}_{goarch}'
assert target == os.environ['RELEASE_TARGET'], (target, os.environ['RELEASE_TARGET'])
name = f'goxide_{tag}_{target}'
binary = 'goxide.exe' if goos == 'windows' else 'goxide'
dist = ROOT / 'release-dist'
dist.mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(prefix='goxide-release-') as temporary:
    work = Path(temporary).resolve()
    package = work / name
    package.mkdir()
    subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-buildvcs=false',
                    f'-ldflags=-s -w -buildid= -X main.version={tag}',
                    '-o', str(package / binary), './cmd/goxide'],
                   cwd=ROOT, env=dict(env, CGO_ENABLED='0'), check=True)
    shutil.copyfile(ROOT / 'LICENSE', package / 'LICENSE')
    (package / 'README.txt').write_text(
        f'Goxide {tag}\n\n'
        f'Target: {goos}/{goarch}\nRequired Go toolchain: {goversion} (exact version)\n'
        'Pre-1.0 releases are experimental and may contain breaking changes.\n'
        'Put the goxide executable on PATH. Run goxide --version to inspect the build.\n'
        'Install the matching Go toolchain separately; goxide test also needs a C compiler for -race.\n'
        'Quickstart: https://goxide-lang.github.io/learn/\n'
        'Source and license: https://github.com/goxide-lang/goxide\n', encoding='utf-8')
    if goos == 'windows':
        archive = dist / (name + '.zip')
        with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as output:
            for path in sorted(package.iterdir()):
                info = zipfile.ZipInfo(f'{name}/{path.name}', (1980, 1, 1, 0, 0, 0))
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = (0o755 if path.name == binary else 0o644) << 16
                output.writestr(info, path.read_bytes())
    else:
        archive = dist / (name + '.tar.gz')
        with archive.open('wb') as raw, gzip.GzipFile(fileobj=raw, mode='wb', filename='', mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as output:
                for path in sorted(package.iterdir()):
                    data = path.read_bytes()
                    info = tarfile.TarInfo(f'{name}/{path.name}')
                    info.size = len(data)
                    info.mode = 0o755 if path.name == binary else 0o644
                    output.addfile(info, io.BytesIO(data))
    unpack = work / 'unpacked'
    shutil.unpack_archive(archive, unpack)
    cli = str(unpack / name / binary)
    version = run(cli, '--version').strip()
    assert version == f'goxide {tag} ({goversion})', version
    project = work / 'project'
    shutil.copytree(ROOT / 'website/tutorial-projects/gox', project)
    expected = 'order 1001: pending\norder 1002: paid 42\norder 1003: shipped TRACK-7\n'
    run(cli, 'gen', '--offline', '-o', str(work / 'generated'), '.', cwd=project)
    assert run(cli, 'run', '--offline', '--locked', '.', cwd=project) == expected
    executable = work / ('orders.exe' if goos == 'windows' else 'orders')
    run(cli, 'build', '--offline', '--locked', '-o', str(executable), '.', cwd=project)
    assert run(str(executable), cwd=project) == expected
    # The CLI runs project tests with -race, which needs the runner's native C compiler.
    env['CGO_ENABLED'] = '1'
    run(cli, 'test', '--offline', '--locked', '.', cwd=project)
    print(f'PASS {name}: unpacked version, generation, run, build and project test; {goversion}')
