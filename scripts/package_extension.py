#!/usr/bin/env python3
"""Package and inspect the universal VSIX without changing the source version."""
import json
import os
from pathlib import Path
import subprocess
import xml.etree.ElementTree as ET
import zipfile

root = Path(__file__).resolve().parents[1]
extension = root / 'editors/vscode'
tag = os.environ['RELEASE_TAG']
prerelease = os.environ['RELEASE_PRERELEASE'] == 'true'
version = (tag.removeprefix('v').split('-', 1)[0].split('+', 1)[0]
           if tag.startswith('v') else json.loads((extension / 'package.json').read_text())['version'])
dist = root / 'release-dist'
dist.mkdir(exist_ok=True)
archive = dist / f'goxide_{tag}.vsix'
command = ['node', 'node_modules/@vscode/vsce/vsce', 'package', version,
           '--no-update-package-json', '--no-git-tag-version', '--out', str(archive)]
if prerelease:
    command.append('--pre-release')
subprocess.run(command, cwd=extension, check=True)

with zipfile.ZipFile(archive) as vsix:
    package = json.loads(vsix.read('extension/package.json'))
    assert package['version'] == version, package['version']
    manifest = ET.fromstring(vsix.read('extension.vsixmanifest'))
    ns = {'v': 'http://schemas.microsoft.com/developer/vsx-schema/2011'}
    identity = manifest.find('v:Metadata/v:Identity', ns)
    assert identity.get('Version') == version
    assert not identity.get('TargetPlatform'), 'VSIX must be universal'
    properties = {p.get('Id'): p.get('Value') for p in manifest.findall('v:Metadata/v:Properties/v:Property', ns)}
    assert (properties.get('Microsoft.VisualStudio.Code.PreRelease') == 'true') == prerelease
    for member in vsix.infolist():
        if member.is_dir():
            continue
        name = Path(member.filename).name.lower()
        assert name not in ('goxide', 'goxide.exe', 'gopls', 'gopls.exe', 'dlv', 'dlv.exe'), member.filename
        with vsix.open(member) as file:
            magic = file.read(4)
        assert magic not in (b'\x7fELF', b'\xfe\xed\xfa\xce', b'\xce\xfa\xed\xfe',
                             b'\xfe\xed\xfa\xcf', b'\xcf\xfa\xed\xfe', b'\xca\xfe\xba\xbe',
                             b'\xbe\xba\xfe\xca') and not magic.startswith(b'MZ'), member.filename
print(f'PASS {archive.name}: version {version}, prerelease={prerelease}, universal, no native binaries')
