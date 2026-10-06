from pathlib import Path
import subprocess,os,json,hashlib
v=Path('/tmp/goxide-a04-origin-review-20261006');task=Path('/Users/hxsf/Documents/Codex/2026-10-06/task');env=os.environ|dict(GOTOOLCHAIN='local',GOMAXPROCS='4',GOCACHE=str(task/'cache/go-build'),GOMODCACHE=str(task/'cache/go-mod'),GOPROXY='off',GOSUMDB='off',GOWORK='off',TMPDIR=str(task/'tmp-native'))
binary=Path(os.environ.get('PROBE_BINARY',str(v/'goxide-final')));runtime=Path(os.environ.get('PROBE_SOURCE',str(v/'final-source')));rows=[]
def run(args):
 p=subprocess.run(list(map(str,args)),cwd=v,env=env,text=True,capture_output=True,timeout=90);return dict(exit=p.returncode,stdout=p.stdout,stderr=p.stderr)
def record(name,group,result,ok):
 rows.append(dict(case=name,group=group,result=result,verified=ok));(v/'final-observations.json').write_text(json.dumps(rows,indent=2)+'\n');assert ok,(name,result)
for old in json.loads((v/'results.json').read_text())['observations']:
 if old['kind'] not in ['reserved','accept'] and old['case']!='old-exact-point':continue
 result=run([binary,'transpile',old['file']]);ok=result['exit']==(0 if old['kind']=='accept' else 1)
 if old['kind']=='reserved':ok=ok and 'reserved generated identifier' in result['stderr'] and 'fake.gox:999' not in result['stderr']
 elif old['case']=='old-exact-point':ok=ok and result['stderr']==old['baseline']['stderr']
 record(old['case']+'-'+old['line_endings'],'reservation_or_old_source_control',result,ok)
for old in json.loads((v/'actual-macro-results.json').read_text()):
 result=run([binary,'transpile',old['file']]);record(old['case']+'-'+old['line_endings'],'macro_source',result,result['exit']==1 and result['stderr']==old['candidate']['stderr'])
for old in json.loads((v/'pending-type-name-control.json').read_text()):
 file=v/('fixtures/pending-substring-'+old['case']+'.gox');result=run([binary,'transpile',file]);ok=result['exit']==0
 if old['case']=='old-context':ok=ok and result['stdout']==old['baseline']['stdout']
 record('pending-substring-'+old['case'],'seed_type_identity',result,ok)
result=run([binary,'run','--runtime',runtime,'--go','/opt/homebrew/bin/go',v/'fixtures/mixed-owner-functions.gox']);record('mixed-outer-option-inner-result','runtime_user_frames',result,result['exit']==0 and not result['stdout'] and not result['stderr'])
source='''package main
enum E{A,B,}
macro_rules! payload {($x:expr)=>{$x?};}
func none()Option[int]{return Option[int]::None}
func funcs(e E)[]func()Option[int]{return []func()Option[int]{match e{E::A=>func()Option[int]{v:=[]int{match e{E::A=>payload!(none()),E::B=>2}};return Option[int]::Some(v[0])},E::B=>func()Option[int]{return Option[int]::Some(0)}}}}
func array(e E)[1]func()int{return [1]func()int{match e{E::A=>func()int{return -(match e{E::A=>3,E::B=>4})},E::B=>func()int{return 0}}}}
func mapping(e E)map[int]func()int{return map[int]func()int{(match e{E::A=>1,E::B=>2}):match e{E::A=>func()int{return +(match e{E::A=>9,E::B=>4})},E::B=>func()int{return 0}}}}
func main(){match funcs(E::A)[0](){Option[int]::None=>{},Option[int]::Some(_)=>{panic("inner boundary")},};if array(E::A)[0]()!=-3||mapping(E::A)[1]()!=9{panic("function type treated as function body")}}
'''
file=v/'fixtures/function-type-literals.gox';file.write_text(source);result=run([binary,'run','--runtime',runtime,'--go','/opt/homebrew/bin/go',file]);record('function-type-slice-array-map-and-inner-question','runtime_user_frames',result,result['exit']==0 and not result['stdout'] and not result['stderr'])
result=run([binary,'run','--runtime',runtime,'--go','/opt/homebrew/bin/go',v/'fixtures/pointer-function-literal.gox']);record('pointer-function-type-plus-signature','runtime_user_frames',result,result['exit']==0 and not result['stdout'] and not result['stderr'])
result=run(['env','PROBE_BINARY='+str(binary),'python3',v/'verify_mixed.py']);record('mixed-owner-map-LF-CRLF','macro_owner_ast_range',result,result['exit']==0)
manifest=json.loads((v/os.environ.get('PROBE_MANIFEST','final-source-manifest.json')).read_text());manifest['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest();(v/os.environ.get('PROBE_MANIFEST','final-source-manifest.json')).write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps(dict(observations=len(rows),verified=sum(r['verified'] for r in rows),binary_sha256=manifest['binary_sha256'])))
