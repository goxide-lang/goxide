"""Compare old/new producer-consumer pairs; HGO_OLD_BIN must select ae61ec6."""
import json, os, pathlib, subprocess, tempfile

checkout=pathlib.Path(__file__).resolve().parents[3]
root=pathlib.Path(tempfile.mkdtemp(prefix='hgo-readability-abi-'))
print('ROOT',root,flush=True)
go=os.environ.get('GO_BIN','/workspace/goxide-environment/go/bin/go')
env=dict(os.environ,PATH=str(pathlib.Path(go).parent)+':'+os.environ['PATH'],GOCACHE='/tmp/t0-gocache',GOPATH='/tmp/t0-gopath',GOTOOLCHAIN='local',GOPROXY='off',GOWORK='off')
def run(args,cwd=None):
 p=subprocess.run(args,cwd=cwd,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 print(p.stdout,end='',flush=True)
 if p.returncode: raise RuntimeError((args,p.returncode))
def write(base,name,text):
 p=base/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(text)
new=str(root/'hgo')
old=os.environ['HGO_OLD_BIN']
run([go,'build','-o',new,'./cmd/hgo'],cwd=checkout)
src=root/'producer-src'
write(src,'go.mod','module readable.example/model\ngo 1.27\n')
write(src,'model/a.hgo','''package model
func Step(n int)Result[int,error]{return Result[int,error]::Ok(n+1)}
func Slice(n int)Option[[]int]{if n<0{return Option[[]int]::None};if n==0{return Option[[]int]::Some(nil)};return Option[[]int]::Some([]int{n})}
func Nested()Result[Option[int],error]{return Result[Option[int],error]::Ok(Option[int]::Some(3))}
''')
write(src,'model/b.hgo','''package model
func Other(n int)Result[int,error]{return Result[int,error]::Ok(n+2)}
func None()Option[[]int]{return Option[[]int]::None}
''')
producers={}
for label,cli in [('old',old),('new',new)]:
 out=root/('producer-'+label);run([cli,'transpile','--go',go,'-o',str(out),str(src)]);producers[label]=out
 run([go,'test','-race','./...'],cwd=out);run([go,'vet','./...'],cwd=out)
a=json.loads((producers['old']/'model/.hgo.enums.json').read_text())
b=json.loads((producers['new']/'model/.hgo.enums.json').read_text())
assert a['Metadata']==b['Metadata'], 'export metadata changed'
assert a['Content']!=b['Content'], 'source fingerprint unexpectedly unchanged'
print('ABI Metadata identical; Content differs',flush=True)
for producer,prod in producers.items():
 for consumer,cli in [('old',old),('new',new)]:
  app=root/('app-'+producer+'-'+consumer)
  write(app,'go.mod',f'module readable.example/app\ngo 1.27\nrequire readable.example/model v0.0.0\nreplace readable.example/model => {prod}\n')
  write(app,'main.hgo','''package main
import m "readable.example/model/model"
func Read(n int)int{return match m.Slice(n){m.Option[[]int]::None=>-1,m.Option[[]int]::Some(v)=>len(v)}}
func Sum()Result[int,error]{a:=m.Step(10)?;b:=m.Other(20)?;return Result[int,error]::Ok(a+b)}
func Nested()int{return match m.Nested(){m.Result[m.Option[int],error]::Ok(v)=>match v{m.Option[int]::None=>0,m.Option[int]::Some(n)=>n},m.Result[m.Option[int],error]::Err(_)=>-1}}
func main(){}
''')
  # The base test deliberately exercises None-first matching of noncomparable payloads.
  write(app,'main_test.go','''package main
import "testing"
func TestValues(t *testing.T){for i:=-2;i<5;i++{want:=1;if i<0{want=-1}else if i==0{want=0};if got:=Read(i);got!=want{t.Fatal(i,got,want)}};if Sum().(HgoResultOk[int,error]).Value!=33||Nested()!=3{t.Fatal("propagation")}}
''')
  out=root/('out-'+producer+'-'+consumer);run([cli,'transpile','--go',go,'-o',str(out),str(app)]);run([go,'test','-race','-count=1','./...'],cwd=out);run([go,'vet','./...'],cwd=out)
  print('PASS producer',producer,'consumer',consumer,flush=True)
print('ALL FOUR ABI PAIRS PASS',flush=True)
