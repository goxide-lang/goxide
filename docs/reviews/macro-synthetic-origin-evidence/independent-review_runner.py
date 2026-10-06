from pathlib import Path
import collections, hashlib, importlib.util, json, os, re, subprocess

BASE=Path(__file__).parent
REPO=Path('/Users/hxsf/workspace/self/goxide-lang/goxide')
SHA='fd8380c39876c96441857a737be8e32c322a0467'
ENV=dict(os.environ,GOTOOLCHAIN='local',GOMAXPROCS='4',GOCACHE='/Users/hxsf/Documents/Codex/2026-10-06/task/cache/go-build',GOMODCACHE='/Users/hxsf/Documents/Codex/2026-10-06/task/cache/go-mod',GOPROXY='off',GOSUMDB='off',GOWORK='off',TMPDIR='/Users/hxsf/Documents/Codex/2026-10-06/task/tmp-native')
spec=importlib.util.spec_from_file_location('matrix',BASE/'baseline_matrix.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

def run(cmd):
 p=subprocess.run([str(x) for x in cmd],cwd='/tmp',env=ENV,text=True,capture_output=True,timeout=40)
 return dict(exit=p.returncode,stdout=p.stdout,stderr=p.stderr)

def frames(text):return re.findall(r'macro (.*?) defined at (.*?); invoked at (.*)',text)
def locate(file,source,token):
 off=source.index(token);return f'{file}:{source[:off].count(chr(10))+1}:{off-source.rfind(chr(10),0,off)}'

def matrix():
 results=[]
 for old in json.loads((BASE/'baseline-matrix.json').read_text())['observations']:
  name,ending=old['case'],old['line_endings']
  input=BASE/'fixtures'/(name+'-'+ending+('.gox' if name in m.CASES else ''))
  now=run([BASE/'goxide-current','transpile','-o',str(input)+'.review-output',input])
  text=now['stderr'].strip();fs=frames(text)
  category='synthetic-known-stack'
  if name=='downstream-len-question':
   category='downstream-control'
   ok=now['exit']!=0 and 'invalid argument:' in text and 'generated/unmapped' in text and 'source span' not in text
   if fs:
    file,source=next(iter(old['files'].items()))
    want=[('m',locate(file,source,'macro_rules! m'),locate(file,source,'m!(value())'))]
    ok=ok and fs==want
  elif old['source_spans'] and all(a<b for a,b in old['source_spans']):
   category='ordinary-real-token'
   ok=now['exit']==old['exit'] and text==old['stderr']
  else:
   if old['frames']:want=[tuple(x) for x in old['frames']]
   else:
    file,source=next(iter(old['files'].items()))
    if name=='gate-nested':
     want=[('m',locate(file,source,'macro_rules! m'),locate(file,source,'m!(value())')),('inner',locate(file,source,'macro_rules! inner'),locate(file,source,'inner!($x)'))]
    else:want=[('m',locate(file,source,'macro_rules! m'),locate(file,source,'m!(value())'))]
   ok=now['exit']!=0 and 'generated/unmapped' in text and 'source span' not in text and fs==want
  record=dict(case=name,line_endings=ending,category=category,passed=ok,baseline=old['stderr'],final=text,frames=fs)
  results.append(record)
 return results

SOURCES={
 'append-capture':('synthetic','cannot use','package main\nmacro_rules! m {($s:expr,$v:expr)=>{append($s,$v)};}\nfunc main(){_=m!([]int{},"bad")}\n'),
 'copy-capture':('synthetic','different element types','package main\nmacro_rules! m {($s:expr,$v:expr)=>{copy($s,$v)};}\nfunc main(){_=m!([]int{},[]string{})}\n'),
 'make-capture':('synthetic','cannot convert','package main\nmacro_rules! m {($n:expr)=>{make([]int,$n)};}\nfunc main(){_=m!("bad")}\n'),
 'make-literal':('real','cannot convert','package main\nmacro_rules! m {()=>{make([]int,"bad")};}\nfunc main(){_=m!()}\n'),
 'nested-append-capture':('synthetic','cannot use','package main\nmacro_rules! inner {($s:expr,$v:expr)=>{append($s,$v)};}\nmacro_rules! m {($s:expr,$v:expr)=>{inner!($s,$v)};}\nfunc main(){_=m!([]int{},"bad")}\n'),
 'enum-question-type':('synthetic-internal','invalid operation','package main\nenum Noise { A, }\nmacro_rules! m {($x:expr)=>{$x+"wrong"};}\nfunc f()Option[int]{n:=Option[int]::Some(7)?;return Option[int]::Some(m!(n))}\nfunc main(){}\n'),
 'refs-readonly':('remaining-refs','refs.Finish','package p\nmacro_rules! bad {($r:ident)=>{$r.N++};}\ntype S struct{N int}\nfunc f(r &S){bad!(r)}\n'),
 'helper-cycle':('remaining-helper','initialization cycle','package main\nmacro_rules! read {()=>{helper()};}\nfunc helper()int{return Value}\nvar Value=read!()\nfunc main(){}\n'),
}

def supplements():
 all=[]
 root=BASE/'supplement';root.mkdir(exist_ok=True)
 (root/'go.mod').write_text(m.MOD);(root/'go.sum').write_bytes((REPO/'go.sum').read_bytes())
 for name,(category,message,source) in SOURCES.items():
  for ending,eol in [('LF','\n'),('CRLF','\r\n')]:
   source=source.replace('\r\n','\n').replace('\n',eol)
   f=root/(name+'-'+ending+'.gox');f.write_bytes(source.encode())
   records={}
   for variant in ['baseline','current']:
    cli=BASE/('goxide-'+variant);out=root/(name+'-'+ending+'-'+variant+'.go');mp=Path(str(out)+'.map.json')
    transpile=run([cli,'transpile','-o',out,'--source-map',mp,f])
    build=run([cli,'build','--runtime',REPO,'-o',root/(name+'-'+ending+'-'+variant+'.bin'),f])
    rec=dict(transpile=transpile,build=build,frames=frames(build['stderr']))
    if out.exists() and transpile['exit']==0:
     go=out.read_bytes();rec['go_sha256']=hashlib.sha256(go).hexdigest()
     match=re.search(r'^\./main.go:(\d+):(\d+):',build['stderr'],re.M)
     if match:
      line,col=map(int,match.groups());lines=go.splitlines(keepends=True);offset=sum(map(len,lines[:line-1]))+col-1
      data=json.loads(mp.read_text());segments=data['diagnostics']['Segments']
      rec['error_segment']=next((s for s in reversed(segments) if s['Start']<=offset<s['End']),None)
      rec['raw_go_diagnostics']='\n'.join(x for x in build['stderr'].splitlines() if x.startswith('./main.go:') or x.startswith('\t./main.go:'))
    records[variant]=rec
   old,now=records['baseline'],records['current'];text=now['build']['stderr']
   ok=now['build']['exit']!=0 and message in text
   if 'go_sha256' in old:ok=ok and old.get('go_sha256')==now.get('go_sha256') and old.get('raw_go_diagnostics')==now.get('raw_go_diagnostics')
   if category in ['synthetic','synthetic-internal']:
    ok=ok and 'generated/unmapped' in text and len(now['frames'])==(2 if name.startswith('nested') else 1)
    if 'error_segment' in now:
     seg=now['error_segment'];ok=ok and seg is not None and seg.get('Origin') is None and len(seg.get('Stack',[]))==(2 if name.startswith('nested') else 1)
   elif category=='real':
    seg=now.get('error_segment');ok=ok and 'generated/unmapped' not in text and seg is not None and seg.get('Origin') is not None and len(now['frames'])==1
   elif category=='remaining-refs':ok=ok and 'intermediate coordinates' in text and old['build']['stderr']==text
   elif category=='remaining-helper':ok=ok and old['build']['stderr']==text
   result=dict(case=name,line_endings=ending,category=category,passed=ok,baseline=old,final=now)
   all.append(result)
 return all

def main():
 data=dict(source_commit=SHA,baseline_commit=m.SHA,matrix=matrix(),supplements=supplements())
 data['all_expectations_met']=all(x['passed'] for x in data['matrix']+data['supplements'])
 (BASE/'results.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps({'all_expectations_met':data['all_expectations_met'],'matrix_count':len(data['matrix']),'supplement_pairs':len(data['supplements']),'failures':[(x['case'],x['line_endings']) for x in data['matrix']+data['supplements'] if not x['passed']]}))
if __name__=='__main__':main()
