#!/usr/bin/env python3
"""Fail closed on accidental publish files, oversized assets or local path leaks."""
import hashlib,json,pathlib,re,zipfile
if not __debug__:
 raise SystemExit('audit refuses Python optimization; checks must remain enabled')
root=pathlib.Path(__file__).resolve().parents[1]/'dist'
allowed={'rfc.html','playground.html','local.html','index.html','404.html','frontend-assets.json','THIRD-PARTY-LICENSES.txt','worker.js','_headers','.nojekyll','examples.json','compiler.wasm','wasm_exec.js','GO-LICENSE.txt','project-reexports.zip'}
frontend_record=json.loads((root/'frontend-assets.json').read_text())
assert frontend_record['websiteCommit']==json.loads((root/'examples.json').read_text())['websiteCommit'], 'frontend/compiler website identities differ'
frontend=frontend_record['assets']
assert all(not p.is_symlink() for p in root.rglob('*')), 'publish symlink'
assert {p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_dir()}=={'assets'}, 'unexpected publish directory'
assert all(re.fullmatch(r'assets/[A-Za-z0-9_.-]+\.(?:js|css|ttf)', name) for name in frontend), 'unexpected frontend asset path'
assert {p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file()}==allowed|set(frontend),'publish directory differs from explicit allowlist'
for name,digest in frontend.items():
 assert hashlib.sha256((root/name).read_bytes()).hexdigest()==digest, 'frontend asset digest differs: '+name
report=[]
patterns={
 'local-path':rb'/(?:workspace|home|Users|tmp|usr/local/go|opt/codex)/',
 'windows-user-path':rb'[A-Za-z]:\\+(?:Users|workspace|Go)\\+',
 'private-key':rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
 'github-token':rb'(?:ghp_|github_pat_)[A-Za-z0-9_]{30,}',
 'aws-access-key':rb'AKIA[0-9A-Z]{16}',
}
for project in json.loads((root/'examples.json').read_text())['projectExamples']:
 if 'download' not in project:continue
 download=project['download'];path=root/download['path']
 assert path.name == 'project-reexports.zip'
 assert hashlib.sha256(path.read_bytes()).hexdigest()==download['sha256'], 'project archive digest differs'
 with zipfile.ZipFile(path) as archive:
  assert archive.namelist()==download['files'], 'project archive inventory differs'
  for name in archive.namelist():
   assert not pathlib.PurePosixPath(name).is_absolute() and '..' not in pathlib.PurePosixPath(name).parts
   for label,pattern in patterns.items():
    assert not re.search(pattern,archive.read(name)),f'project archive: detected {label}'
for p in sorted(p for p in root.rglob('*') if p.is_file()):
 assert p.is_file() and not p.is_symlink(),p.name
 b=p.read_bytes();assert len(b)<=25*1024*1024,f'{p.name}: exceeds CF 25 MiB'
 for label,pattern in patterns.items():
  assert not re.search(pattern,b),f'{p.name}: detected {label} (contents not printed)'
 report.append({'file':p.relative_to(root).as_posix(),'bytes':len(b),'sha256':hashlib.sha256(b).hexdigest()})
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
