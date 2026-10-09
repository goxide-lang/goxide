"""Reject stale public naming, retaining only explicit migration diagnostics."""
import pathlib,json,re
root=pathlib.Path(__file__).resolve().parents[1]/'dist'
m=json.loads((root/'examples.json').read_text())
assert m['abiVersions']==dict(enum=9,reference=3,use=3,sum=3)
assert m['metadataSchema']==9
std=next(x for x in m['publicLibraries'] if x['module']=='github.com/goxide-lang/std')
assert std['commit']=='a75c971aa755e0e9f17b412ca4a8f37f32acc933' and std['version']=='v0.0.0-20261009091333-a75c971aa755'
legacy=re.compile(r'hgo|HGO|Hgo')
allowed={'projects':'旧 hgo.lock','cli-options':'旧.hgo/HGO_*输入拒绝。'}
for card in m['catalog']['items']:
 text=json.dumps(card,ensure_ascii=False)
 if card['id'] in allowed:text=text.replace(allowed[card['id']],'')
 assert not legacy.search(text),card['id']
rest={k:v for k,v in m.items() if k!='catalog'}
assert not legacy.search(json.dumps(rest,ensure_ascii=False)), 'legacy name in current examples/metadata'
for p in root.iterdir():
 if p.name in ['compiler.wasm','examples.json']:continue
 assert not legacy.search(p.read_text()),p.name
assert 'self.goxideCompile' in (root/'worker.js').read_text()
assert 'playground.gox' in (root/'index.html').read_text()
# The binary must retain old marker/extension names to produce rejection diagnostics.
b=(root/'compiler.wasm').read_bytes()
assert b'hgoCompile' not in b and b'__hgo_' not in b
known=[b'HgoEnumMetadata',b'HgoReferenceMetadata',b'HgoUseMetadata',b'HgoSumABI',b'hgo_project_config.go',b'legacy hgo directive',b'legacy .hgo source']
for token in known:assert token in b,token
print(json.dumps(dict(abi=m['abiVersions'],std=std['version'],currentText='new names only except two explicit rejection notes',wasm='new bridge; seven legacy rejection markers/diagnostics retained'),ensure_ascii=False))
