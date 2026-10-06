from pathlib import Path
import subprocess,json,os,hashlib,shutil,sys
p=Path('/tmp/goxide-a04-review-20261006');cand=p/sys.argv[1];dest=cand/'function-shapes';dest.mkdir()
shapes={
'slice-pointer':('func Run()[]*func()int{return []*func()int{match E::A{E::A=>nativePointer(),E::B=>nil}}}', 'x:=Run();if len(x)!=1||x[0]==nil||(*x[0])()!=7{t.Fatal(x)}'),
'slice-parenthesized':('func Run()[](func()int){return [](func()int){match E::A{E::A=>nativeValue(),E::B=>nil}}}', 'x:=Run();if len(x)!=1||x[0]==nil||x[0]()!=7{t.Fatal(x)}'),
'map-pointer-value':('func Run()map[string]*func()int{return map[string]*func()int{"x":match E::A{E::A=>nativePointer(),E::B=>nil}}}', 'x:=Run();if len(x)!=1||x["x"]==nil||(*x["x"])()!=7{t.Fatal(x)}'),
'map-pointer-key':('func Run()map[*func()int]int{return map[*func()int]int{(match E::A{E::A=>nativePointer(),E::B=>nil}):1}}', 'x:=Run();if len(x)!=1{t.Fatal(x)};for f,n:=range x{if f==nil||(*f)()!=7||n!=1{t.Fatal(x)}}'),
'array-pointer':('func Run()[1]*func()int{return [1]*func()int{match E::A{E::A=>nativePointer(),E::B=>nil}}}', 'x:=Run();if x[0]==nil||(*x[0])()!=7{t.Fatal(x)}'),
'returned-real-function':('func Run()func()int{return match E::A{E::A=>func()int{return -(match E::A{E::A=>7,E::B=>0})},E::B=>nil}}','f:=Run();if f==nil||f()!=-7{t.Fatal("function owner") }'),
}
env=os.environ.copy();env.update(GOROOT='/opt/homebrew/Cellar/go/1.27.1/libexec',GOTOOLCHAIN='local',GOWORK='off',GOPROXY='off',GOCACHE='/private/tmp/goxide-a04-20261006-ukafjzho/environment/go-build',GOMODCACHE='/private/tmp/goxide-a04-20261006-ukafjzho/environment/go-mod',TMPDIR='/private/tmp/goxide-a04-20261006-ukafjzho/environment/tmp')
res=[]
for key,(body,check) in shapes.items():
 src=p/'function-shape-cases'/key;src.mkdir(parents=True,exist_ok=True);(src/'probe.gox').write_text('package probe\npub enum E{A,B,}\n'+body+'\n');(src/'native.go').write_text('package probe\nfunc nativeValue()func()int{return func()int{return 7}}\nfunc nativePointer()*func()int{f:=nativeValue();return &f}\n')
 for f in ('go.mod','go.sum'):shutil.copy2(p/'flow-probe/source'/f,src/f)
 out=dest/key;out.mkdir();gen=out/'generated';argv=[str(cand/'goxide'),'transpile','-o',str(gen),str(src)];c=subprocess.run(argv,cwd=src,env=env,capture_output=True);(out/'transpile.stdout.log').write_bytes(c.stdout);(out/'transpile.stderr.log').write_bytes(c.stderr)
 r={'id':key,'sourceSha256':hashlib.sha256((src/'probe.gox').read_bytes()).hexdigest(),'transpile':{'argv':argv,'cwd':str(src),'exitCode':c.returncode},'expected':'runtime-pass'}
 if c.returncode==0:
  (gen/'assertions_test.go').write_text('package probe\nimport "testing"\nfunc TestShape(t *testing.T){'+check+'}\n');argv=['/opt/homebrew/bin/go','test','-race','-count=1','-v','.'];t=subprocess.run(argv,cwd=gen,env=env,capture_output=True);(out/'test.stdout.log').write_bytes(t.stdout);(out/'test.stderr.log').write_bytes(t.stderr);r['runtime']={'argv':argv,'cwd':str(gen),'exitCode':t.returncode};print(t.stdout.decode(),t.stderr.decode(),flush=True)
 (out/'result.json').write_text(json.dumps(r,indent=2)+'\n');res.append(r);print(key,'transpile',c.returncode,'runtime',r.get('runtime',{}).get('exitCode'),c.stderr.decode().strip(),flush=True)
(dest/'results.json').write_text(json.dumps(res,indent=2)+'\n')
