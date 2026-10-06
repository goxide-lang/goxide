from pathlib import Path
import argparse, json, os, re, shutil, subprocess

BASE=Path(__file__).parent
REPO=Path('/Users/hxsf/workspace/self/goxide-lang/goxide')
ENV=dict(os.environ,GOTOOLCHAIN='local',GOMAXPROCS='4',GOCACHE='/Users/hxsf/Documents/Codex/2026-10-06/task/cache/go-build',GOMODCACHE='/Users/hxsf/Documents/Codex/2026-10-06/task/cache/go-mod',GOPROXY='off',GOSUMDB='off',GOWORK='off',TMPDIR='/Users/hxsf/Documents/Codex/2026-10-06/task/tmp-native')

def source(rule,body='_=m!()',decl=''):
 return 'package main\n'+decl+'\nmacro_rules! m {\n '+rule+';\n}\nfunc main(){'+body+'}\n'

CASES={
 'array-length-scalar':('pass',source('($n:expr)=>{new([$n]int)}','a:=m!(3.0);if len(a)!=3{panic("length")}')),
 'array-length-named-constant':('pass',source('($n:expr)=>{new([$n]int)}','const int Count=3;a:=m!(int);if len(a)!=3{panic("length")}', 'type Count int')),
 'array-length-template-int-constant':('pass',source('()=>{func()*[3]byte{const int=3;return new([int]byte)}()}','a:=m!();if len(a)!=3{panic("length")}')),
 'array-length-whole-capture-local-type':('pass',source('($n:expr)=>{new([$n]int)}','type Local string;a:=m!(len([2]Local{}));if len(a)!=2{panic("length")}')),
 'array-length-nil-pointer-no-eval':('pass',source('($p:expr)=>{new([len(*$p)]int)}','var p *[3]int;a:=m!(p);if len(a)!=3{panic("length")}')),
 'array-length-nil-element-no-eval':('pass',source('($p:expr)=>{new([len([3]int{*$p})]byte)}','var p *int;a:=m!(p);if len(a)!=3{panic("length")}')),
 'array-length-caller-len-shadow':('pass',source('($p:expr)=>{new([len(*$p)]int)}','var p *[3]int;len:=func([3]int)int{panic("caller len")};_=len;a:=m!(p);if cap(a)!=3{panic("length")}')),
 'nested-array-expression':('pass',source('($n:expr)=>{new([2][$n]int)}','a:=m!(1+2);if len(a)!=2||len(a[0])!=3{panic("length")}')),
 'template-local-new-function':('pass',source('($x:expr)=>{func()int{new:=func(v int)int{return v+1};return new($x)}()}','if m!(7)!=8{panic("binding")}')),
 'definition-new-function':('pass',source('($x:expr)=>{new($x)}','if m!(7)!=9{panic("binding")}', 'func new(v int)int{return v+2}')),
 'captured-new-callee-value':('pass',source('($f:ident,$x:expr)=>{$f($x)}','p:=m!(new,7);if *p!=7{panic("binding")}')),
 'complete-caller-allocation-expression':('pass',source('($x:expr)=>{$x}','type Local []int;a:=m!(make(Local,2));if len(a)!=2{panic("length")}')),
 'existing-other-type-slot-gap':('existing-gap',source('($t:expr)=>{func()$t{var v $t;return v}()}','if m!(int)!=0{panic("control")}')),
 'capture-new-entire':('macro-reject',source('($T:expr)=>{new($T)}','_=m!(int)')),
 'capture-new-star':('macro-reject',source('($T:expr)=>{new(*$T)}','_=m!(int)')),
 'capture-new-map-value':('macro-reject',source('($T:expr)=>{new(map[string][]chan $T)}','_=m!(int)')),
 'capture-make-map-key':('macro-reject',source('($T:ident)=>{make(map[$T]int)}','_=m!(string)')),
 'capture-make-tt-element':('macro-reject',source('($T:tt)=>{make([]$T,2)}','_=m!(int)')),
 'capture-array-length-explicit-type':('macro-reject',source('($T:expr)=>{new([len([2]$T{})]int)}','_=m!(int)')),
 'capture-array-length-explicit-type-ident':('macro-reject',source('($T:ident)=>{new([len([2]$T{})]int)}','type Local string;_=m!(Local)')),
 'template-local-int-variable':('macro-reject',source('()=>{func()*int{int:=7;return new(int)}()}')),
 'template-local-int-dereference':('macro-reject',source('()=>{func()*int{var int *int;return new(*int)}()}')),
 'definition-function-int':('macro-reject',source('()=>{new(int)}',decl='func int()string{return "value"}')),
 'fixed-user-type':('macro-reject',source('()=>{new(Item)}',decl='type Item int')),
 'fixed-sum-type':('macro-reject',source('()=>{new(Option[int])}')),
 'new-value-literal':('macro-reject',source('()=>{new(7)}')),
 'new-value-conversion':('macro-reject',source('()=>{new(int(7))}')),
 'new-value-capture':('macro-reject',source('($x:expr)=>{new($x)}','_=m!(7)')),
 'new-value-question':('synthetic-macro-reject','package main\nmacro_rules! m {($x:expr)=>{new($x?)};}\nfunc value()Option[int]{return Option[int]::Some(1)}\nfunc f()Option[int]{_=m!(value());return Option[int]::None}\nfunc main(){}\n'),
 'array-length-nonconstant':('native-reject',source('($n:expr)=>{new([$n]int)}','n:=3;_=m!(n)')),
 'array-length-typed-float':('native-reject',source('($n:expr)=>{new([$n]int)}','const n float64=3;_=m!(n)')),
 'array-length-function-call':('native-reject',source('($p:expr)=>{new([len($p)]int)}','_=m!(f())', 'func f()[3]int{return [3]int{}}')),
 'array-length-whole-caller-len-function':('native-reject',source('($n:expr)=>{new([$n]int)}','len:=func([2]int)int{return 2};_=m!(len([2]int{}))')),
 'nested-allocation-placeholder':('macro-reject','package main\nmacro_rules! inner {($T:expr)=>{new([]$T)};}\nmacro_rules! outer {($T:expr)=>{inner!($T)};}\nfunc main(){_=outer!(int)}\n'),
 'nested-verified-primitive-alias':('pass','package main\nmacro_rules! arr {()=>{[2]int{}};}\nmacro_rules! allocate {()=>{new([len(arr!())]byte)};}\nfunc main(){a:=allocate!();if len(a)!=2{panic("length")}}\n'),
 'nested-captured-type-not-trusted-alias':('macro-reject','package main\nmacro_rules! arr {($T:expr)=>{[2]$T{}};}\nmacro_rules! allocate {($T:expr)=>{new([len(arr!($T))]byte)};}\nfunc main(){_=allocate!(int)}\n'),
}
length='3'
for _ in range(24):length='len(['+length+']int{})'
CASES['deep-array-length-walk']=('pass',source('()=>{new(['+length+']byte)}','a:=m!();if len(a)!=3{panic("length")}'))

def prepare():
 Path(ENV['TMPDIR']).mkdir(parents=True,exist_ok=True)
 for f in ['go.mod','go.sum']:shutil.copyfile(REPO/f,BASE/f)
 for name,(expected,text) in CASES.items():(BASE/(name+'.gox')).write_text(text)
 (BASE/'manifest.json').write_text(json.dumps({n:{'expected':e,'source':n+'.gox'} for n,(e,_) in CASES.items()},indent=2)+'\n')

def run(cli,sha):
 results=[]
 for name,(expected,text) in CASES.items():
  endings=['LF','CRLF'] if expected in ['macro-reject','synthetic-macro-reject'] else ['LF']
  for ending in endings:
   stem=name+'-'+ending;inp=BASE/(stem+'.gox');out=BASE/(stem+'.go')
   inp.write_bytes(text.replace('\n','\r\n' if ending=='CRLF' else '\n').encode())
   cp=subprocess.run([str(cli),'transpile','-o',str(out),'--source-map',str(out)+'.map.json',str(inp)],cwd='/tmp',env=ENV,text=True,capture_output=True,timeout=20)
   item={'case':name,'lineEndings':ending,'expected':expected,'transpileExit':cp.returncode,'diagnostic':cp.stderr.strip()}
   if cp.returncode==0:
    cmd=['go','run','-race',str(out)] if expected in ['pass','existing-gap'] else ['go','build','-o',str(BASE/(stem+'.bin')),str(out)]
    q=subprocess.run(cmd,cwd=BASE,env=ENV,text=True,capture_output=True,timeout=40)
    item.update(nativeExit=q.returncode,stdout=q.stdout,stderr=q.stderr)
   if expected in ['pass','existing-gap']:item['passed']=cp.returncode==0 and item.get('nativeExit')==0
   elif expected=='native-reject':
    item['passed']=(cp.returncode==0 and item.get('nativeExit')!=0) or (cp.returncode!=0 and 'macro:' not in cp.stderr and ('array length' in cp.stderr or 'constant' in cp.stderr))
   elif expected=='synthetic-macro-reject':item['passed']=cp.returncode!=0 and 'new(expr)' in cp.stderr and '[generated/unmapped]' in cp.stderr and 'source span' not in cp.stderr and 'defined at' not in cp.stderr
   else:item['passed']=cp.returncode!=0 and str(inp)+':' in cp.stderr and 'macro' in cp.stderr and 'defined at' in cp.stderr and 'invoked at' in cp.stderr
   if expected in ['macro-reject','synthetic-macro-reject']:
    item['sourcePositions']=re.findall(re.escape(str(inp))+r':(\d+):(\d+)',cp.stderr)
   (BASE/(stem+'.log')).write_text(json.dumps(item,ensure_ascii=False,indent=2)+'\n')
   results.append(item)
   print(json.dumps(item,ensure_ascii=False),flush=True)
 module=BASE/'cross-definition-module';module.mkdir(exist_ok=True)
 files={
  'go.mod':'module review.example/alias\ngo 1.27\n',
  'fixed/array.gox':'package fixed\npub macro_rules! array {()=>{[2]int{}};}\n',
  'ops/allocation.gox':'package ops\nimport f "review.example/alias/fixed"\ntype int string\npub macro_rules! allocate {()=>{new([len(f.array!())]byte)};}\n',
  'main.gox':'package main\nimport ops "review.example/alias/ops"\nfunc main(){a:=ops.allocate!();if len(a)!=2{panic("length")}}\n',
 }
 for n,s in files.items():
  f=module/n;f.parent.mkdir(parents=True,exist_ok=True);f.write_text(s)
 generated=BASE/'cross-definition-generated'
 cp=subprocess.run([str(cli),'transpile','-o',str(generated),str(module)],cwd='/tmp',env=ENV,text=True,capture_output=True,timeout=30)
 item={'case':'cross-definition-verified-alias-not-reinterpreted','expected':'pass','transpileExit':cp.returncode,'diagnostic':cp.stderr.strip()}
 if cp.returncode==0:
  q=subprocess.run(['go','run','-race','.'],cwd=generated,env=ENV,text=True,capture_output=True,timeout=40)
  item.update(nativeExit=q.returncode,stdout=q.stdout,stderr=q.stderr)
 item['passed']=cp.returncode==0 and item.get('nativeExit')==0
 results.append(item);print(json.dumps(item,ensure_ascii=False),flush=True)
 (BASE/'results.json').write_text(json.dumps({'sourceCommit':sha,'results':results},ensure_ascii=False,indent=2)+'\n')
 return all(r['passed'] for r in results)

if __name__=='__main__':
 ap=argparse.ArgumentParser();ap.add_argument('--cli');ap.add_argument('--sha');args=ap.parse_args();prepare()
 if args.cli:raise SystemExit(0 if run(Path(args.cli).resolve(),args.sha) else 1)
 print(f'Prepared {len(CASES)} focused cases; no compiler or runtime executed.')
