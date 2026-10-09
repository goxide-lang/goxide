#!/usr/bin/env python3
"""Isolated evidence; never modifies compiler ABI. Pass a writable output dir."""
import json, os, pathlib, subprocess, sys
root = pathlib.Path(__file__).resolve().parents[2]
out = pathlib.Path(sys.argv[1]).resolve()
out.mkdir(parents=True, exist_ok=True)
results = []
def write(base, name, text):
    p = base / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text(text)
def run(label, cwd, args, success=True, env=None):
    p = subprocess.run(args, cwd=cwd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    write(out, label+'.log', p.stdout)
    results.append(dict(label=label, code=p.returncode, expected_success=success))
    if (p.returncode == 0) != success: raise RuntimeError(label+': '+p.stdout)
    return p.stdout
cli = out/'goxide'
run('build-cli',root,['go','build','-o',str(cli),'./cmd/goxide'])
# Actual goxide: two packages define distinct sum identities; ? reconstructs target.
src=out/'goxide-source'
write(src,'go.mod','module fixture.example/current\ngo 1.27\n')
write(src,'a/a.gox','package a\nfunc Make()Result[int,error]{return Result[int,error]::Ok(7)}\n')
write(src,'b/b.gox','package b\nuse a;\nfunc Make()Result[int,error]{n:=a::Make()?;return Result[int,error]::Ok(n)}\n')
write(src,'goxide.roots.toml','[roots.a]\nmodule="fixture.example/current"\npackage="fixture.example/current/a"\n')
gen=out/'goxide-generated'
run('goxide-transpile',root,[str(cli),'transpile','-o',str(gen),str(src)])
write(gen,'b/b_test.go','''package b
import("testing";"reflect";"fixture.example/current/a")
func TestDifferent(t *testing.T){if reflect.TypeOf(a.Make())==reflect.TypeOf(Make()){t.Fatal("unexpected identical cases")};if Make().(GoxideResultOk[int,error]).Value!=7{t.Fatal("propagate")}}
''')
run('goxide-generated-race',gen,['go','test','-race','./...'])
run('goxide-generated-vet',gen,['go','vet','./...'])
write(gen,'b/negative.go','package b\nimport "fixture.example/current/a"\nvar _ Result[int,error] = a.Make()\n')
run('goxide-cross-package-assignment-rejected',gen,['go','test','./b'],False)
(gen/'b/negative.go').unlink()
# Actual constructor/pattern/result-expression cases, not Go mechanism claims.
cases={
'explicit':('func F()Option[int]{return Option[int]::Some(1)}',True),
'infer-adapter':('func n()(int,error){return 1,nil};func F()Result[int,error]{return go_result!(n())}',True),
'return-shorthand':('func F()Option[int]{return Option::Some(1)}',False),
'local-some':('func F(){x:=Option::Some(1);_=x}',False),
'return-err':('func F(e error)Result[int,error]{return Result::Err(e)}',False),
'return-none':('func F()Option[int]{return Option::None}',False),
'assignment-context':('func F(){var x Option[int]=Option::Some(1);_=x}',False),
'call-context':('func take(x Option[int]){};func F(){take(Option::Some(1))}',False),
'pattern-shorthand':('func F(x Option[int]){match x {Option::Some(n)=>{_=n},Option::None=>{},}}',False),
'bare-pattern':('func F(x Option[int]){match x {Some(n)=>{_=n},None=>{},}}',False),
'concrete-match-inference':('func F(x Option[int])int{n:=match x {Option[int]::Some(v)=>v,Option[int]::None=>0,};return n}',True),
'wildcard-match':('func F(x Option[int]){match x {_=>{},}}',True),
'underdetermined-none':('func F(){x:=Option::None;_=x}',False),
'underdetermined-ok':('func F(){x:=Result::Ok(1);_=x}',False),
}
for name,(body,ok) in cases.items():
    f=out/'goxide-cases'/(name+'.gox');write(f.parent,f.name,'package p\n'+body+'\n')
    text=run('goxide-'+name,root,[str(cli),'transpile',str(f)],ok)
    if ok:write(out/'goxide-cases',name+'.generated.go',text)
# Proposed shared Go ABI mechanism only, deliberately outside repo production.
std=out/'std';prod=out/'producer';consumer=out/'consumer'
write(std,'go.mod','module fixture.example/std\ngo 1.27\n')
write(std,'sum/sum.go','''package sum
type Option[T any] interface{option(T)}
type Some[T any] struct{Value T}
func(Some[T])option(T){}
func NewSome[T any](v T)Option[T]{return Some[T]{v}}
func None[T any]()Option[T]{return nil}
func ValidateOption[T any](v Option[T]){switch v.(type){case nil,Some[T]:return;default:panic("invalid Option")}}
type Result[T,E any] interface{result(T,E)}
type Ok[T,E any] struct{Value T}
type Err[T,E any] struct{Value E}
func(Ok[T,E])result(T,E){}
func(Err[T,E])result(T,E){}
func NewOk[T,E any](v T)Result[T,E]{return Ok[T,E]{v}}
func NewErr[T,E any](e E)Result[T,E]{return Err[T,E]{e}}
func ValidateResult[T,E any](v Result[T,E]){switch v.(type){case Ok[T,E],Err[T,E]:return;default:panic("invalid Result")}}
''')
write(prod,'go.mod',f'module fixture.example/producer\ngo 1.27\nrequire fixture.example/std v0.0.0\nreplace fixture.example/std => {std}\n')
write(prod,'p.go','''package producer
import s "fixture.example/std/sum"
type Result[T,E any] = s.Result[T,E]
type GoxideResultOk[T,E any] = s.Ok[T,E]
func Make()Result[int,error]{return s.NewOk[int,error](7)}
''')
write(consumer,'go.mod',f'module fixture.example/consumer\ngo 1.27\nrequire (\nfixture.example/std v0.0.0\nfixture.example/producer v0.0.0\n)\nreplace fixture.example/std => {std}\nreplace fixture.example/producer => {prod}\n')
write(consumer,'c.go','''package consumer
import(s "fixture.example/std/sum";p "fixture.example/producer")
type Result[T,E any] = s.Result[T,E]
var Shared Result[int,error] = p.Make()
var SharedCase p.GoxideResultOk[int,error] = s.Ok[int,error]{Value:8}
''')
write(consumer,'c_test.go','''package consumer
import("testing";s "fixture.example/std/sum";p "fixture.example/producer")
type Embedded struct{s.Result[int,error]}
func mustPanic(t *testing.T,f func()){t.Helper();defer func(){if recover()==nil{t.Fatal("accepted invalid value")}}();f()}
func TestShared(t *testing.T){if Shared.(s.Ok[int,error]).Value!=7{t.Fatal("identity")};var _ s.Result[int,error]=p.Make();s.ValidateOption(s.None[int]());s.ValidateOption(s.NewSome(0));a:=[]int{1};v:=s.NewSome(a);a[0]=2;if v.(s.Some[[]int]).Value[0]!=2{t.Fatal("lost shallow copy")};mustPanic(t,func(){s.ValidateResult[int,error](nil)});mustPanic(t,func(){s.ValidateResult[int,error](&s.Ok[int,error]{})});mustPanic(t,func(){var p *s.Ok[int,error];s.ValidateResult[int,error](p)});mustPanic(t,func(){s.ValidateResult[int,error](Embedded{})})}
''')
for label,base in [('std',std),('producer',prod),('consumer',consumer)]:
    run(label+'-race',base,['go','test','-race','./...']);run(label+'-vet',base,['go','vet','./...'])
for name,text in {
'phantom':'var _ s.Result[int,string] = s.Ok[int,error]{Value:1}',
'fake-marker':'type Fake struct{};func(Fake)result(int,error){};var _ s.Result[int,error]=Fake{}',
'alias-method':'type Alias = s.Ok[int,error];func(Alias)Extra(){}',
'no-return-context-inference':'func inferred()s.Result[int,error]{return s.NewOk(1)}',
}.items():
    write(consumer,'negative.go','package consumer\nimport s "fixture.example/std/sum"\n'+text+'\n')
    run('go-'+name,consumer,['go','test','./...'],False)
    (consumer/'negative.go').unlink()
env=dict(os.environ,GOOS='js',GOARCH='wasm',CGO_ENABLED='0')
run('shared-wasm-build',consumer,['go','build','./...'],env=env)
run('shared-export-data',consumer,['go','list','-export','-deps','./...'])
write(out,'results.json',json.dumps(results,indent=2)+'\n')
print(json.dumps(dict(checks=len(results),passed=True,artifacts=str(out))))
