#!/usr/bin/env python3
"""Fail closed on accidental publish files, oversized assets or local path leaks."""
import hashlib,json,pathlib,re
if not __debug__:
 raise SystemExit('audit refuses Python optimization; checks must remain enabled')
root=pathlib.Path(__file__).resolve().parents[1]/'dist'
allowed={'index.html','404.html','style.css','app.js','worker.js','_headers','.nojekyll','examples.json','compiler.wasm','wasm_exec.js','GO-LICENSE.txt'}
assert {p.name for p in root.iterdir()}==allowed,'publish directory differs from explicit allowlist'
report=[]
patterns={
 'local-path':rb'/(?:workspace|home|Users|tmp|usr/local/go|opt/codex)/',
 'windows-user-path':rb'[A-Za-z]:\\+(?:Users|workspace|Go)\\+',
 'private-key':rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
 'github-token':rb'(?:ghp_|github_pat_)[A-Za-z0-9_]{30,}',
 'aws-access-key':rb'AKIA[0-9A-Z]{16}',
}
for p in sorted(root.iterdir()):
 assert p.is_file() and not p.is_symlink(),p.name
 b=p.read_bytes();assert len(b)<=25*1024*1024,f'{p.name}: exceeds CF 25 MiB'
 for label,pattern in patterns.items():
  assert not re.search(pattern,b),f'{p.name}: detected {label} (contents not printed)'
 report.append({'file':p.name,'bytes':len(b),'sha256':hashlib.sha256(b).hexdigest()})
# Decode WASM sections; custom debug/name sections are not needed for publication.
b=(root/'compiler.wasm').read_bytes();assert b[:8]==b'\0asm\x01\0\0\0'
def leb(i):
 value=shift=0
 while True:
  byte=b[i];i+=1;value|=(byte&127)<<shift
  if not byte&128:return value,i
  shift+=7
custom=[];i=8
while i<len(b):
 section=b[i];length,start=leb(i+1);end=start+length
 assert end<=len(b)
 if section==0:
  n,j=leb(start);name=b[j:j+n].decode();custom.append(name)
  assert not name.startswith('.debug') and name!='name',f'unnecessary WASM custom section: {name}'
 i=end
print(json.dumps({'files':report,'totalBytes':sum(p['bytes'] for p in report),'wasmCustomSections':custom,'limitBytes':25*1024*1024,'scope':'allowlist and known-pattern audit; not proof against reverse engineering or every possible secret'},indent=2))
