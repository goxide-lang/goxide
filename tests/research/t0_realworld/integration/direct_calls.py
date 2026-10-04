#!/usr/bin/env python3
"""Controlled generated-code experiment; never edit compiler or metadata.
Usage: direct_calls.py GENERATED_APP NEW_EXPERIMENT_DIRECTORY
Replace only two generated validator variables with direct typed forwarding
functions. Preserve validation, public enum ABI, all match arms and Result values.
The edited artifact is only built with Go; do not feed its stale metadata to hgo.
"""
import pathlib, re, shutil, sys
src, dst = map(pathlib.Path, sys.argv[1:])
if dst.exists():
    raise SystemExit('experiment destination must not exist')
shutil.copytree(src, dst)
p = dst / 'pipeline.go'
s = p.read_text()
pattern = r'var (__hgo_qimport_\w+) = (__hgo_enum_family_import_\w+)\n'
matches = list(re.finditer(pattern, s))
changed = 0
for match in matches:
    alias, middle = match.groups()
    target = re.search(r'var '+re.escape(middle)+r' = (cfg\.HgoInst\w+)\n', s)
    if target:
        s = s.replace(match.group(0), f'func {alias}(v cfg.Result[int,error]) {{ {target.group(1)}(v) }}\n')
        changed += 1
s, n = re.subn(r'var (__hgo_enum_import_\w+_Mode_validate) = cfg.ValidateMode\n',
              r'func \1(v cfg.Mode) { cfg.ValidateMode(v) }\n', s)
if changed != 1 or n != 1:
    raise SystemExit(f'unexpected generated layout: Result={changed}, Mode={n}')
p.write_text(s)
print('Changed two validator bindings into typed forwarding functions:', p)
