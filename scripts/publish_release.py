#!/usr/bin/env python3
"""Publish the complete set of native archives after the release jobs pass."""
import hashlib
import json
import os
from pathlib import Path
import subprocess

tag = os.environ['RELEASE_TAG']
commit = os.environ['RELEASE_COMMIT']
go = os.environ['RELEASE_GO']
prerelease = os.environ['RELEASE_PRERELEASE'] == 'true'
dist = Path('release-dist')
targets = ['darwin_amd64', 'darwin_arm64', 'linux_amd64', 'linux_arm64', 'windows_amd64']
names = [f'goxide_{tag}_{target}' + ('.zip' if target.startswith('windows') else '.tar.gz') for target in targets]
assert sorted(p.name for p in dist.iterdir()) == sorted(names), 'incomplete or unexpected release assets'
(dist / 'checksums.txt').write_text(''.join(
    f'{hashlib.sha256((dist / name).read_bytes()).hexdigest()}  {name}\n' for name in sorted(names)))
notes = Path('release-notes.md')
notes.write_text(
    f'Goxide {tag}：将 `.gox` 编译成普通 Go 的命令行工具。\n\n'
    + ('这是实验性预发布，**不是正式稳定发行**。语言、CLI、项目配置和生成代码后续仍可发生破坏性变更，'
       '即使版本只增加 patch 或 minor。\n\n' if prerelease else '')
    + f'- 源码提交：`{commit}`\n'
    + f'- 配套 Go：**`{go}`，必须完全匹配，包括补丁号**；Go 工具链需单独安装。\n'
    + '- 平台：macOS / Linux amd64、arm64；Windows amd64。\n'
    + '- 解压对应平台归档，将 `goxide`（Windows 为 `goxide.exe`）放入 PATH，用 `goxide --version` 检查。\n'
    + '- `goxide test` 使用 race detector，另需该平台的 C 编译器。\n'
    + '- 每个归档均通过原生 runner 上的解压、版本、生成、运行、构建和项目测试；SHA-256 见 `checksums.txt`。\n\n'
    + '[快速开始](https://goxide-lang.github.io/learn/) · [语言设计](https://goxide-lang.github.io/design/)\n', encoding='utf-8')
view = subprocess.run(['gh', 'release', 'view', tag, '--json', 'isDraft'], capture_output=True, text=True)
if view.returncode == 0:
    if not json.loads(view.stdout)['isDraft']:
        raise SystemExit('Release is already published; refusing to replace public assets')
else:
    subprocess.run(['gh', 'release', 'create', tag, '--verify-tag', '--draft', '--title', f'Goxide {tag}',
                    '--notes-file', str(notes), '--prerelease=' + str(prerelease).lower(), '--latest=false'], check=True)
subprocess.run(['gh', 'release', 'upload', tag, *[str(dist / name) for name in names],
                str(dist / 'checksums.txt'), '--clobber'], check=True)
assets = json.loads(subprocess.check_output(['gh', 'release', 'view', tag, '--json', 'assets'], text=True))['assets']
expected = {name: (dist / name).stat().st_size for name in names + ['checksums.txt']}
assert len(assets) == len(expected) and {asset['name']: asset['size'] for asset in assets} == expected, 'remote release assets differ; draft kept unpublished'
subprocess.run(['gh', 'release', 'edit', tag, '--draft=false', '--notes-file', str(notes),
                '--prerelease=' + str(prerelease).lower(), '--latest=' + str(not prerelease).lower()], check=True)
