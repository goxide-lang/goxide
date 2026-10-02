#!/usr/bin/env python3
"""Portable actual-hgo guard benchmarks and separate no-panic mechanism probes."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--go', default=os.environ.get('GO', 'go'))
p.add_argument('--out', type=Path)
p.add_argument('--count', type=int, default=5)
p.add_argument('--benchtime', default='200ms')
p.add_argument('--expect-no-guard', action='store_true', help='require benchmark.go to omit guard calls and success benchmarks to allocate zero')
a = p.parse_args()
go = shutil.which(a.go)
if not go:
    raise SystemExit('Go toolchain not found; pass --go /path/to/go')
out = a.out.resolve() if a.out else Path(tempfile.mkdtemp(prefix='goxide-assignment-audit-'))
out.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, GOWORK='off', GOTOOLCHAIN='local', GOPROXY='off')
env.setdefault('GOCACHE', str(out / 'cache'))
commands = []
def run(command, cwd, log, extra=None):
    commands.append({'argv': [str(x) for x in command], 'cwd': str(cwd), 'extra_env': extra or {}})
    result = subprocess.run(command, cwd=cwd, env=env | (extra or {}), text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (out / log).write_text(result.stdout)
    if result.returncode:
        raise SystemExit(f'{command} failed; see {out / log}\n{result.stdout[-2000:]}')
    return result.stdout
version = run([go, 'version'], ROOT, 'version.txt')
if not version.startswith('go version go'):
    raise SystemExit('Selected go executable is not the Go language toolchain')
run([go, 'build', '-o', str(out / 'hgo'), './cmd/hgo'], ROOT, 'build.txt')
source = out / 'source'
shutil.copytree(HERE / 'source', source)
(source / 'go.mod').write_text(f'module places.example/update\n\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => {json.dumps(str(ROOT))}\n')
generated = out / 'generated'
run([str(out / 'hgo'), 'transpile', '-o', str(generated), str(source)], out, 'transpile.txt')
if a.expect_no_guard:
    code = (generated / 'benchmark.go').read_text()
    if any(marker in code for marker in ('.Catch(', '.Fail(', '.Assignment[')):
        raise SystemExit('benchmark.go unexpectedly retains assignment guards')
for f in HERE.glob('*.go.fixture'):
    shutil.copyfile(f, generated / f.name.removesuffix('.fixture'))
run([go, 'test', '-race', '-count=1', '-v', './...'], generated, 'race.txt')
run([go, 'test', '-race', '-count=1', '-v', '-run', 'TestAudit', '.'], generated, 'legacy.txt', {'GODEBUG': 'panicnil=1'})
run([go, 'vet', './...'], generated, 'vet.txt')
run([go, 'test', '-c', '-gcflags=-m=2', '-o', str(out / 'audit.test')], generated, 'escape.txt')
run([go, 'test', '-run', '^$', '-bench', '.', '-benchmem', '-benchtime=' + a.benchtime, '-count=' + str(a.count)], generated, 'bench.txt', {'GOMAXPROCS': '1'})
rows = {}
for line in (out / 'bench.txt').read_text().splitlines():
    m = re.match(r'Benchmark(\w+)\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op', line)
    if m:
        rows.setdefault(m[1], []).append((float(m[2]), int(m[3]), int(m[4])))
summary = {k: dict(samples=len(v), median_ns=statistics.median(x[0] for x in v), min_ns=min(x[0] for x in v), max_ns=max(x[0] for x in v), bytes=v[0][1], allocations=v[0][2]) for k,v in rows.items()}
(out / 'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
if a.expect_no_guard:
    for name in ('Guard1Success', 'Guard8Success', 'PackedGuardSuccess'):
        samples = rows.get(name, [])
        if len(samples) != a.count or any(size != 0 or allocations != 0 for _, size, allocations in samples):
            raise SystemExit(f'{name} missing samples or unexpectedly allocates: {samples}')
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
status = subprocess.check_output(['git', 'status', '--short'], cwd=ROOT, text=True)
(out / 'manifest.json').write_text(json.dumps(dict(revision=revision, working_tree_status=status, toolchain=version.strip(), expect_no_guard=a.expect_no_guard, commands=commands), indent=2)+'\n')
print(json.dumps(summary, indent=2))
print('Evidence:', out)
