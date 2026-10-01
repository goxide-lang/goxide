#!/usr/bin/env python3
"""Run isolated trait lowering probes; retains raw logs, does not build hgo."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import re
import statistics
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--go', default='go')
p.add_argument('--out', type=Path, required=True)
p.add_argument('--count', type=int, default=5)
p.add_argument('--benchtime', default='200ms')
a = p.parse_args()
go = shutil.which(a.go)
if not go:
    raise SystemExit('Go executable not found; no download is attempted')
out = a.out.resolve()
out.mkdir(parents=True, exist_ok=True)
work = Path(tempfile.mkdtemp(prefix='hgo-trait-paths-'))
shutil.copytree(HERE / 'probe', work / 'probe')
mod = work / 'probe'
(mod / 'go.mod.fixture').rename(mod / 'go.mod')
env = os.environ.copy()
env.update(GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='', GOPROXY='off', GOSUMDB='off', GOCACHE=os.environ.get('GOCACHE', str(work / 'cache')), GOMODCACHE=str(work / 'modcache'), GOMAXPROCS='1')
meta = {'go': go, 'python': platform.python_version(), 'platform': platform.platform(), 'work': str(work), 'gomaxprocs': 1, 'commands': [], 'sources': {str(f.relative_to(HERE)): hashlib.sha256(f.read_bytes()).hexdigest() for f in sorted((HERE / 'probe').glob('*')) if f.is_file()}}
meta['go_env'] = json.loads(subprocess.check_output([go, 'env', '-json', 'GOVERSION', 'GOOS', 'GOARCH', 'CGO_ENABLED'], env=env, text=True))

def run(label, args, cwd=mod, expected_failure=None):
    command = [go, *args]
    completed = subprocess.run(command, cwd=cwd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=240)
    (out / (label + '.log')).write_text(completed.stdout)
    meta['commands'].append({'label': label, 'cwd': str(cwd), 'command': command, 'returncode': completed.returncode, 'expected_failure': expected_failure})
    (out / 'environment.json').write_text(json.dumps(meta, indent=2))
    ok = (completed.returncode != 0 and expected_failure in completed.stdout) if expected_failure else completed.returncode == 0
    print(label, 'PASS' if ok else 'FAIL', flush=True)
    if not ok:
        raise SystemExit(completed.stdout)
run('plain', ['test', '-count=1', '-v', './...'])
run('enhanced', ['test', '-tags=enhanced', '-count=1', '-v', './...'])
run('bench', ['test', '-run=^$', '-bench=.', '-benchmem', '-count='+str(a.count), '-benchtime='+a.benchtime])
run('compiler', ['test', '-run=^$', '-gcflags=-m=2', './...'])
run('build', ['test', '-c', '-o', str(work / 'probe.test')])
run('dispatch', ['tool', 'objdump', '-s', 'example.org/traitpaths.BenchmarkPolymorphic', str(work / 'probe.test')])
run('dictionary-dispatch', ['tool', 'objdump', '-s', 'example.org/traitpaths.BenchmarkLocal.func[678]', str(work / 'probe.test')])
# Three-module release check: dependency replace is not inherited by root.
original, enhanced, adapter, consumer = [work / n for n in ['original', 'enhanced', 'adapter', 'consumer']]
for path in [original, enhanced, adapter, consumer]: path.mkdir()
for path in [original, enhanced]:
    (path / 'go.mod').write_text('module example.org/native\n\ngo 1.27\n')
    (path / 'source.go').write_text('package native\ntype Source struct{}\nfunc (*Source) Step(n int) int { return n }\n')
(enhanced / 'marker.go').write_text('package native\nfunc (*Source) Marker() {}\n')
(adapter / 'go.mod').write_text('module example.org/adapter\n\ngo 1.27\nrequire example.org/native v0.0.0\nreplace example.org/native => ../enhanced\n')
(consumer / 'go.mod').write_text('module example.org/consumer\n\ngo 1.27\nrequire (\nexample.org/native v0.0.0\nexample.org/adapter v0.0.0\n)\nreplace example.org/native => ../original\nreplace example.org/adapter => ../adapter\n')
(consumer / 'main.go').write_text('package main\nimport ("example.org/adapter"; "example.org/native")\nfunc main(){ if adapter.As(&native.Source{}).Step(7)!=7 { panic("changed behavior") } }\n')
(adapter / 'adapter.go').write_text('package adapter\nimport "example.org/native"\ntype Trait interface{Step(int)int;Marker()}\nfunc As(s *native.Source)Trait{return s}\n')
run('publish-marker-original', ['run', '.'], consumer, 'missing method Marker')
pmod = consumer / 'go.mod'
pmod.write_text(pmod.read_text().replace('../original', '../enhanced'))
run('publish-marker-enhanced', ['run', '.'], consumer)
pmod.write_text(pmod.read_text().replace('../enhanced', '../original'))
(adapter / 'adapter.go').write_text('package adapter\nimport "example.org/native"\ntype Trait interface{Step(int)int;Marker()}\ntype View struct{target *native.Source}\nfunc(v View)Step(n int)int{return v.target.Step(n)}\nfunc(View)Marker(){}\nfunc As(s *native.Source)Trait{return View{s}}\n')
run('publish-wrapper-original', ['run', '.'], consumer)
groups = {}
for line in (out / 'bench.log').read_text().splitlines():
    match = re.match(r'(Benchmark\S+)\s+\d+\s+([\d.]+) ns/op\s+(?:[\d.]+ MB/s\s+)?(\d+) B/op\s+(\d+) allocs/op', line)
    if match: groups.setdefault(match[1], []).append((float(match[2]), int(match[3]), int(match[4])))
summary = [{'benchmark': name, 'runs': len(vals), 'median_ns': statistics.median(v[0] for v in vals), 'min_ns': min(v[0] for v in vals), 'max_ns': max(v[0] for v in vals), 'bytes': sorted({v[1] for v in vals}), 'allocs': sorted({v[2] for v in vals})} for name, vals in groups.items()]
(out / 'summary.json').write_text(json.dumps(summary, indent=2))
print('Results:', out, flush=True)
