#!/usr/bin/env python3
"""Run the published core probes with a caller-provided compiler; never download tools/modules."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--compiler', type=Path, required=True)
parser.add_argument('--compiler-source', type=Path, required=True,
                    help='Matching goxide checkout; supplies pinned Go dependencies/checksums and runtime sources')
parser.add_argument('--go', default='go')
parser.add_argument('--out', type=Path, required=True, help='Must not exist')
parser.add_argument('--case', action='append', dest='selected', help='Optional case id from cases.json; repeatable')
args = parser.parse_args()
base = Path(__file__).resolve().parent
compiler = args.compiler.resolve()
source = args.compiler_source.resolve()
out = args.out.resolve()
if out.exists():
    parser.error('--out must be a new directory; old successful output is never reused')
if not compiler.is_file() or not (source/'go.mod').is_file():
    parser.error('compiler and compiler-source/go.mod must already exist')
catalog = json.loads((base/'cases.json').read_text())['cases']
selected = set(args.selected or [c['id'] for c in catalog])
if selected - {c['id'] for c in catalog}:
    parser.error('unknown case id')

def sha(data):
    return hashlib.sha256(data).hexdigest()

mod = 'module review.example/probe\n\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\n'
mod += 'replace github.com/hxsf/goxide => '+json.dumps(str(source))+'\n'
for line in (source/'go.mod').read_text().splitlines():
    fields = line.split()
    if len(fields) >= 2 and fields[0] in ('github.com/goxide-lang/std', 'github.com/goxide-lang/serde'):
        mod += '\nrequire '+fields[0]+' '+fields[1]+'\n'
sums = '\n'.join(line for line in (source/'go.sum').read_text().splitlines()
                 if line.startswith('github.com/goxide-lang/'))+'\n'
env = os.environ.copy()
env.update(GOTOOLCHAIN='local', GOWORK='off', GOPROXY='off')
goroot = subprocess.check_output([args.go, 'env', 'GOROOT'], env=env, text=True).strip()
env['GOROOT'] = goroot
out.mkdir(parents=True)
identity = {'compiler': str(compiler), 'compilerSha256': sha(compiler.read_bytes()),
            'compilerSource': str(source), 'environment': {k:env.get(k) for k in
            ('GOROOT','GOTOOLCHAIN','GOWORK','GOPROXY','GOSUMDB','GOCACHE','GOMODCACHE','TMPDIR')}}
identity['buildInfo'] = subprocess.check_output([args.go,'version','-m',str(compiler)],env=env,text=True)
for label, command in [('sourceHead',['git','rev-parse','HEAD']),
                       ('sourceStatus',['git','status','--porcelain=v1','--untracked-files=all'])]:
    value = subprocess.run(command, cwd=source, capture_output=True, text=True)
    identity[label] = value.stdout.strip() if value.returncode == 0 else None
(out/'identity.json').write_text(json.dumps(identity,indent=2)+'\n')

def execute(argv, cwd, directory, name):
    cp = subprocess.run(argv, cwd=cwd, env=env, capture_output=True)
    (directory/(name+'.stdout.log')).write_bytes(cp.stdout)
    (directory/(name+'.stderr.log')).write_bytes(cp.stderr)
    return {'argv':argv, 'cwd':str(cwd), 'exitCode':cp.returncode,
            'stdoutSha256':sha(cp.stdout), 'stderrSha256':sha(cp.stderr)}

results = []
for case in catalog:
    if case['id'] not in selected:
        continue
    original = base/case['directory']
    directory = out/case['id']
    directory.mkdir(parents=True)
    src = directory/'source'
    src.mkdir()
    for name, expected in case['files'].items():
        data = (original/name).read_bytes()
        if sha(data) != expected:
            raise ValueError('published case checksum mismatch: '+case['id']+'/'+name)
        if name != 'assertions_test.go':
            (src/name).write_bytes(data)
    (src/'go.mod').write_text(mod)
    (src/'go.sum').write_text(sums)
    generated = directory/'generated'
    row = {'id':case['id'], 'inputHashes':case['files']}
    row['transpile'] = execute([str(compiler),'transpile','-o',str(generated),str(src)],src,directory,'transpile')
    if row['transpile']['exitCode'] == 0:
        shutil.copy2(original/'assertions_test.go',generated/'assertions_test.go')
        row['runtime'] = execute([args.go,'test','-race','-count=1','-v','.'],generated,directory,'test')
    row['passed'] = row['transpile']['exitCode'] == 0 and row.get('runtime',{}).get('exitCode') == 0
    (directory/'result.json').write_text(json.dumps(row,indent=2)+'\n')
    results.append(row)
    print(case['id'], 'PASS' if row['passed'] else 'FAIL', flush=True)
(out/'results.json').write_text(json.dumps({'scope':'published core probes','cases':results},indent=2)+'\n')
sys.exit(0 if results and all(r['passed'] for r in results) else 1)
