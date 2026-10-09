#!/usr/bin/env python3
"""Actual goxide switch lowering, generated race tests, and non-race allocation checks."""
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
p.add_argument('--go', default='go')
p.add_argument('--out', type=Path)
p.add_argument('--count', type=int, default=5)
p.add_argument('--benchtime', default='200ms')
a = p.parse_args()
go = shutil.which(a.go)
if not go:
    raise SystemExit('Go toolchain missing')
out = a.out.resolve() if a.out else Path(tempfile.mkdtemp(prefix='goxide-switch-audit-'))
out.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, GOWORK='off', GOTOOLCHAIN='local', GOPROXY='off')
commands = []
def run(argv, cwd, log, extra=None):
    commands.append(dict(argv=[str(x) for x in argv], cwd=str(cwd), extra_env=extra or {}))
    result = subprocess.run(argv, cwd=cwd, env=env | (extra or {}), text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (out / log).write_text(result.stdout)
    if result.returncode:
        raise SystemExit(f'{log} failed: {result.stdout[-3000:]}')
    return result.stdout
version = run([go, 'version'], ROOT, 'version.txt')
if not version.startswith('go version go'):
    raise SystemExit('Not the Go compiler')
run([go, 'build', '-o', str(out / 'goxide'), './cmd/goxide'], ROOT, 'build.txt')
generated = out / 'generated'
generated.mkdir()
run([str(out / 'goxide'), 'transpile', '-o', str(generated / 'generated.go'), str(ROOT / 'examples/prototype/switch_propagation.gox')], ROOT, 'transpile.txt')
code = (generated / 'generated.go').read_text()
if any(x in code for x in ('.Catch(', '.Fail(', '.Assignment[')):
    raise SystemExit('Unexpected switch guard')
support = run([go, 'list', '-m', '-f', '{{.Path}} {{.Version}}', 'github.com/goxide-lang/std', 'github.com/goxide-lang/serde'], ROOT, 'support-modules.txt')
(generated / 'go.mod').write_text('module switchfixture\n\ngo 1.27\n\nrequire (\n' + support + ')\n')
(generated / 'go.sum').write_text(''.join(line for line in (ROOT / 'go.sum').read_text().splitlines(keepends=True) if line.startswith(('github.com/goxide-lang/std ', 'github.com/goxide-lang/serde '))))
shutil.copyfile(HERE / 'switch_test.go.fixture', generated / 'switch_test.go')
run([go, 'test', '-race', '-count=1', '-v', './...'], generated, 'race.txt')
run([go, 'vet', './...'], generated, 'vet.txt')
run([go, 'test', '-run', '^$', '-bench', '.', '-benchmem', '-count='+str(a.count), '-benchtime='+a.benchtime], generated, 'bench.txt', {'GOMAXPROCS':'1'})
rows = {}
for line in (out / 'bench.txt').read_text().splitlines():
    m = re.match(r'Benchmark(\w+)\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op', line)
    if m:
        rows.setdefault(m[1], []).append((float(m[2]), int(m[3]), int(m[4])))
for name in ('PropagatedSuccess','ExplicitSuccess','PropagatedFailure','ExplicitFailure'):
    samples=rows.get(name, [])
    if len(samples)!=a.count or any(size or allocs for _,size,allocs in samples):
        raise SystemExit(f'{name} missing or allocating: {samples}')
summary={name:dict(samples=len(v), median_ns=statistics.median(x[0] for x in v), bytes=v[0][1], allocations=v[0][2]) for name,v in rows.items()}
(out / 'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
status=subprocess.check_output(['git','status','--short'],cwd=ROOT,text=True)
(out / 'manifest.json').write_text(json.dumps(dict(revision=revision,working_tree_status=status,toolchain=version.strip(),commands=commands),indent=2)+'\n')
print(json.dumps(summary,indent=2))
