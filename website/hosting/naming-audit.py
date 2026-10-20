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
allowed={'cli-options':'旧.hgo/HGO_*输入拒绝。'}
for card in m['catalog']['items']:
 text=json.dumps(card,ensure_ascii=False)
 if card['id'] in allowed:text=text.replace(allowed[card['id']],'')
 assert not legacy.search(text),card['id']
rest={k:v for k,v in m.items() if k!='catalog'}
# The published RFCs describe exact legacy inputs that are rejected. Keep those
# statements scoped to their pages; every other string still uses current names.
allowed_design={
 'rfc-0023':[
  '规范入口名为 `build.gox`；显式 entry 仍支持 `.go` / `.gox`，旧 `.hgo` 已移除（RFC 0027）。',
 ],
 'rfc-0027':[
  '- 旧 .hgo 输入必须改名；目录混合新旧后缀、同名 main.gox/main.hgo 均拒绝，先诊断再生成，不覆盖。',
  '- GOXIDE_TARGET_DIR、GOXIDE_TRUST_DIR 取代旧环境变量。CLI flag 优先于新环境变量；设置任何已移除 HGO_* 变量均明确拒绝，即便同时有新变量或 flag，避免迁移时悄悄忽略。',
  '最终版本不安装 hgo shim，不接受 .hgo/build.hgo，不回退 HGO_*。',
  '例如同时设置 HGO_TARGET_DIR=old 和 GOXIDE_TARGET_DIR=new，报迁移错误，要求移除旧变量。',
 ],
}
design_pages={page['id']:page for page in rest['design']['pages']}
for identity,statements in allowed_design.items():
 page=design_pages[identity]
 for statement in statements:
  assert page['markdown'].count(statement)==1, 'legacy rejection statement changed: '+identity
  page['markdown']=page['markdown'].replace(statement,'')
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
