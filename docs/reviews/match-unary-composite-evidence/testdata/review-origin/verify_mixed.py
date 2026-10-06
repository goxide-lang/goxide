import json, subprocess, os
from pathlib import Path
v=Path('/tmp/goxide-a04-origin-review-20261006');env=os.environ|dict(GOTOOLCHAIN='local',GOMAXPROCS='4',GOCACHE='/Users/hxsf/Documents/Codex/2026-10-06/task/cache/go-build',GOMODCACHE='/Users/hxsf/Documents/Codex/2026-10-06/task/cache/go-mod',GOPROXY='off',GOSUMDB='off',GOWORK='off',TMPDIR='/Users/hxsf/Documents/Codex/2026-10-06/task/tmp-native')
def run(args):
 p=subprocess.run(list(map(str,args)),cwd=v,env=env,text=True,capture_output=True); assert p.returncode==0,(args,p.stdout,p.stderr); return p.stdout
binary=os.environ.get('PROBE_BINARY',str(v/'goxide-candidate-v2'));rows=[]
raw=(v/'fixtures/mixed-owner-functions.gox').read_text()
for eol,sep in [('LF','\n'),('CRLF','\r\n')]:
 source=raw.replace('\n',sep).encode();f=v/f'fixtures/mixed-owner-functions-{eol}.gox';f.write_bytes(source);g=v/f'mixed-{eol}.go';m=v/f'mixed-{eol}.map.json'
 run([binary,'transpile','-o',g,'--source-map',m,f]);generated=g.read_bytes();segments=json.loads(m.read_text())['diagnostics']['Segments'];ranges=json.loads(run(['/opt/homebrew/bin/go','run',v/'ast_ranges.go','--',g]));assert len(ranges)==1,ranges
 bounds=ranges[0];expected={}
 def span(a,b):return dict(File=str(f),Start=a,End=b,Line=source[:a].count(b'\n')+1,Column=a-source.rfind(b'\n',0,a))
 for name,arg in [('outerPayload','outerValue(ok)'),('innerPayload','innerValue(false)')]:
  start=source.index(('macro_rules! '+name).encode());end=source.index(sep.encode(),start);call=source.index((name+'!('+arg+')').encode());expected[name]=dict(Macro=name,Definition=span(start,end),Callsite=span(call,call+len(name+'!('+arg+')')),DefinitionOriginal=True,CallsiteOriginal=True)
 counts={'outerPayload':0,'innerPayload':0};origin=0;generated_point=0
 for s in segments:
  if not s.get('Stack'):continue
  owner='innerPayload' if bounds['start']<=s['Start']<bounds['end'] else 'outerPayload';assert s['Stack']==[expected[owner]],(owner,s);counts[owner]+=1
  if s.get('Origin'):
   o=s['Origin'];assert source[o['Start']:o['End']]==generated[s['Start']:s['End']],s;origin+=1
  if generated[s['Start']:s['End']].startswith(b'__goxide_'):
   assert not s.get('Origin'),s;generated_point+=1
 assert all(counts.values()) and origin>0 and generated_point>0
 rows.append(dict(line_endings=eol,function_ast_range=bounds,stack_tokens=counts,precise_macro_source_tokens=origin,generated_identifiers_without_false_point=generated_point,verified=True))
(v/'mixed-map-verification.json').write_text(json.dumps(dict(binary=binary,checks=rows),indent=2)+'\n');print(json.dumps(rows))
