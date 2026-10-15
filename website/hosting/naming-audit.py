"""Reject stale public naming, retaining only explicit migration diagnostics."""
import pathlib,json,re
root=pathlib.Path(__file__).resolve().parents[1]/'dist'
m=json.loads((root/'examples.json').read_text())
assert m['pipeline']=='parser-expand-resolve-check-lower'
assert m['enumRepresentation']=='sealed-interface'
assert 'abiVersions' not in m and 'metadataSchema' not in m
std=next(x for x in m['publicLibraries'] if x['module']=='github.com/goxide-lang/std')
assert std['commit']=='c02a09d8c417b35dc7e432fc484172fab6db6182' and std['version']=='v0.0.0-20261009104839-c02a09d8c417'
legacy=re.compile(r'hgo|HGO|Hgo')
allowed={'projects':'旧 hgo.lock','cli-options':'旧.hgo/HGO_*输入拒绝。'}
for card in m['catalog']['items']:
 text=json.dumps(card,ensure_ascii=False)
 if card['id'] in allowed:text=text.replace(allowed[card['id']],'')
 assert not legacy.search(text),card['id']
rest={k:v for k,v in m.items() if k!='catalog'}
assert not legacy.search(json.dumps(rest,ensure_ascii=False)), 'legacy name in current examples/metadata'
for p in root.rglob('*'):
 if not p.is_file() or p.name in ['compiler.wasm','examples.json'] or p.suffix in ['.zip','.ttf']:continue
 assert not legacy.search(p.read_text()),p.name
assert 'self.goxideCompile' in (root/'worker.js').read_text()
assert any('playground.gox' in p.read_text() for p in (root/'assets').glob('*.js'))
# The new pipeline does not consume or emit the old enum/reference/use metadata.
b=(root/'compiler.wasm').read_bytes()
assert b'hgoCompile' not in b and b'__hgo_' not in b
for token in [b'HgoEnumMetadata',b'HgoReferenceMetadata',b'HgoUseMetadata']:
 assert token not in b,token
print(json.dumps(dict(pipeline=m['pipeline'],std=std['version'],currentText='new names only except explicit rejection notes',wasm='new bridge; no old enum/reference/use metadata'),ensure_ascii=False))
