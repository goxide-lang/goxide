package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMatchCompositionRace(t *testing.T) {
	source := `package fixture
use serde;
use errors;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
pub enum Config { Off, Enabled(int), }
impl Config { pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) } }
type Wide = uint64
func mark(t *[]string,s string,n int)int{*t=append(*t,s);return n}
func checked(t *[]string,ok bool)Result[uint64,error]{mark(t,"checked",0);if !ok{return Result[uint64,error]::Err(errors::New("bad"))};return Result[uint64,error]::Ok(7)}
func finish(a int,n Wide,b int)Result[uint64,error]{return Result[uint64,error]::Ok(uint64(a+b)+n)}
func run(c Config,ok bool,t *[]string)Result[uint64,error]{
 defer func(){mark(t,"defer",0)}()
 return finish(mark(t,"first",1),match c {
  Config::Off=>1,
  Config::Enabled(n)=>match Option[int]::Some(n) {
   Option[int]::Some(n) if mark(t,"guard",n)<0=>99,
   Option[int]::Some(n)=>checked(t,ok)?,
   Option[int]::None=>0,
  },
 },mark(t,"last",2))
}
func wide(n uint)uint64{
 type Wide = int8
 _=Wide(1)
 return takeWide(match Config::Off {Config::Off=>1<<n,_=>0,})
}
func takeWide(n Wide)uint64{return n}
//go:uintptrescapes
func directiveCall(n int)int{return n}
func directiveMatch(c Config)int{return directiveCall(match c{Config::Off=>1,_=>2,})}
func iface(n interface{},p *int)bool{return n==nil && p==nil}
func nils()bool{return iface(match Config::Off {Config::Off=>nil,_=>1,},nil)}
func nested(c Config)int{return match c {Config::Enabled(n)=>match Config::Enabled(n+1){Config::Enabled(n)=>n,_=>0,},_=>0,}}
func nestedCall(c Config)uint64{return takeWide(takeWide(match c {Config::Enabled(n)=>uint64(n),_=>0,}))}
func discard(c Config,t *[]string){r#use(mark(t,"before",1),match c {Config::Off=>mark(t,"arm",2),_=>0,},mark(t,"after",3))}
func r#use(a,b,c int){}
func closure(t *[]string)int{f:=func()Result[uint64,error]{return finish(0,match Config::Off{Config::Off=>checked(t,false)?,_=>0,},mark(t,"wrong",0))};_=f();return mark(t,"outer",42)}
func jumping(c Config){goto Done;r#use(1,match c{Config::Off=>2,_=>3,},4);Done:return}
func wrap(f func()uint64)uint64{return f()}
func inlineClosure(c Config)uint64{return wrap(func()uint64{return takeWide(match c{Config::Off=>7,_=>0,})})}
func shadowedCallback(n uint64)uint64{panic("captured package function")}
func referenceSignatureShadow(c Config,r &int,shadowedCallback (func(int)uint64))uint64{return shadowedCallback(match c{Config::Off=>*r,_=>2,})}
func unpack(r Result[uint64,error])uint64{return match r {Result[uint64,error]::Ok(n)=>n,Result[uint64,error]::Err(_)=>0,}}
`
	enumFormalStdModule(t, source, `package fixture
import("testing";"reflect";"github.com/goxide-lang/serde/json")
func TestConfigSuccessFailureAndSelection(t *testing.T){for _,tt:=range []struct{json string;ok bool;want uint64;trace []string}{{"{\"Enabled\":3}",true,10,[]string{"first","guard","checked","last","defer"}},{"{\"Enabled\":3}",false,0,[]string{"first","guard","checked","defer"}},{"\"Off\"",false,4,[]string{"first","last","defer"}}}{c,e:=serdejson.Unmarshal([]byte(tt.json),(ConfigOps{}).Decode);if e!=nil{t.Fatal(e)};trace:=[]string{};got:=unpack(run(c,tt.ok,&trace));if got!=tt.want||!reflect.DeepEqual(trace,tt.trace){t.Fatalf("got %d trace %v",got,trace)}}}
func TestContextAndNestedBinding(t *testing.T){c,e:=serdejson.Unmarshal([]byte("{\"Enabled\":3}"),(ConfigOps{}).Decode);if e!=nil{t.Fatal(e)};if wide(63)!=uint64(1)<<63||!nils()||nested(c)!=4||nestedCall(c)!=3||directiveMatch(c)!=2{t.Fatal("context or binding changed")}}
func TestStatementAndNearestFunction(t *testing.T){c,e:=serdejson.Unmarshal([]byte("\"Off\""),(ConfigOps{}).Decode);if e!=nil{t.Fatal(e)};jumping(c);if directiveMatch(c)!=1||inlineClosure(c)!=7{t.Fatal("inline closure")};trace:=[]string{};discard(c,&trace);if !reflect.DeepEqual(trace,[]string{"before","arm","after"}){t.Fatal(trace)};trace=nil;if closure(&trace)!=42||!reflect.DeepEqual(trace,[]string{"checked","outer"}){t.Fatal(trace)}}
func TestInvalidBeforeArms(t *testing.T){trace:=[]string{};defer func(){if recover()==nil||!reflect.DeepEqual(trace,[]string{"first","defer"}){t.Fatalf("trace=%v",trace)}}();run(nil,true,&trace)}
func TestReferenceSignatureKeepsLocalCallback(t *testing.T){c,e:=serdejson.Unmarshal([]byte("\"Off\""),(ConfigOps{}).Decode);if e!=nil{t.Fatal(e)};n:=41;if referenceSignatureShadow(c,&n,func(n int)uint64{return uint64(n+1)})!=42{t.Fatal("captured package signature")}}
`, false)
}
func TestMatchCompositionBoundaries(t *testing.T) {
	cases := map[string]string{
		"parenthesized type conversion": `func g(n uint64)uint64{return n};func f(x E)any{type g (int8);return g(match x{E::A=>256,_=>0,})}`,
		"grouped type conversion":       `func g(n uint64)uint64{return n};func f(x E)any{type (g (int8));return g(match x{E::A=>256,_=>0,})}`,
		"reference signature shadow":    `func g(n uint64)uint64{return n};func f(x E,r &int,g (func(int)uint64))uint64{return g(match x{E::A=>1<<63,_=>2,})}`,
		"nested missing arm":            `func f(x E)int{return match x{E::A=>match x{E::A=>1,},_=>2,}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("composition_bad.gox", []byte("package fixture\nenum E{A,B,}\n"+body))
			if err == nil || !strings.Contains(err.Error(), "composition_bad.gox") {
				t.Fatalf("expected located diagnostic: %v", err)
			}
		})
	}
}

func TestMatchCompositionPackagesRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := acceptanceCLI(t)
	source := filepath.Join(t.TempDir(), "source")
	writeStdFormalManifest(t, source, "packages.example/app", "app", nil)
	packagesWrite(t, source, "model/types.gox", `package model
use serde;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
pub enum Mode { Off, Workers(int), }
impl Mode { pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) } }
`)
	packagesWrite(t, source, "app.gox", `package app
pub mod model;
use crate::model as cfg;
func add(a,b int)int{return a+b}
func Composed(m cfg::Mode)int{return add(1,match m {cfg::Mode::Workers(n)=>match cfg::Mode::Workers(n+1){cfg::Mode::Workers(n)=>n,_=>0,},_=>0,})}
func Explicit(m cfg::Mode)int{
 var result int
 match m {cfg::Mode::Workers(n)=>{match cfg::Mode::Workers(n+1){cfg::Mode::Workers(n)=>{result=n},_=>{result=0},}},_=>{result=0},}
 return add(1,result)
}
func Ordered(m cfg::Mode,t *[]int)int{return add(mark(t,1),match m{cfg::Mode::Workers(n)=>mark(t,n),_=>mark(t,2),})}
func mark(t *[]int,n int)int{*t=append(*t,n);return n}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	generated = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", generated, source))
	packagesWrite(t, generated, "app_test.go", `package app
import("testing";"reflect";cfg "packages.example/app/model";"github.com/goxide-lang/serde/json")
func TestCrossPackageConfig(t *testing.T){for n:=0;n<20;n++{m:=&cfg.Workers{Value: n};if Composed(m)!=n+2||Composed(m)!=Explicit(m){t.Fatal(n)}};m,e:=serdejson.Unmarshal([]byte("{\"Workers\":3}"),(cfg.ModeOps{}).Decode);if e!=nil||Composed(m)!=5{t.Fatal(e)}}
func TestCrossPackageInvalid(t *testing.T){for _,m:=range []cfg.Mode{nil}{func(){trace:=[]int{};defer func(){if recover()==nil||!reflect.DeepEqual(trace,[]int{1}){t.Fatalf("invalid %T trace %v",m,trace)}}();Ordered(m,&trace)}()}}
var sink int
func BenchmarkComposed(b *testing.B){m:=&cfg.Workers{Value: 3};b.ReportAllocs();for i:=0;i<b.N;i++{sink=Composed(m)}}
func BenchmarkExplicit(b *testing.B){m:=&cfg.Workers{Value: 3};b.ReportAllocs();for i:=0;i<b.N;i++{sink=Explicit(m)}}
`)
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, generated, tool, true, "vet", "./...")
	packagesCommand(t, generated, tool, true, "test", "-run=^$", "-bench=.", "-benchmem", "-count=3", "-benchtime=100ms", "-cpu=1", ".")
}
