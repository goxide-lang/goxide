package acceptance_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the real compiler and its independently built output. The business
// fixture uses existing derive protocols; nominal type controls are separate.
func TestMatchUnaryCompositeCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()
	sourceFor := func(body string) string {
		t.Helper()
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.gox", matchUnaryModel)
		packagesWrite(t, dir, "model/scalars.go", matchUnaryScalars)
		packagesWrite(t, dir, "app.gox", "package main\nuse model as cfg;\n"+body)
		return dir
	}
	t.Run("generated-module", func(t *testing.T) {
		source := sourceFor(matchUnaryBusiness)
		packagesWrite(t, source, "contexts.gox", matchUnaryContexts)
		packagesWrite(t, source, "flows.gox", matchUnaryFlows)
		first := filepath.Join(t.TempDir(), "generated")
		second := filepath.Join(t.TempDir(), "generated")
		for _, out := range []string{first, second} {
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", out, source)
		}
		maps := 0
		if err := filepath.WalkDir(first, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(first, path)
			if err != nil {
				return err
			}
			a, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(filepath.Join(second, rel))
			if err != nil {
				return err
			}
			if !bytes.Equal(a, b) {
				t.Errorf("nondeterministic artifact %s", rel)
			}
			if strings.HasSuffix(path, ".map.json") {
				maps++
			}
			if strings.HasSuffix(path, ".go") {
				for _, marker := range []string{"__goxide_match_expr_pending", "__goxide_match_expr_operand_context(", "__goxide_match_expr_return(", "__goxide_match_expr_peers(", "__goxide_match_expr_zero["} {
					if bytes.Contains(a, []byte(marker)) {
						t.Errorf("compiler-only atom remains in %s: %s", rel, marker)
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if maps == 0 {
			t.Fatal("missing diagnostic maps")
		}
		packagesWrite(t, first, "acceptance_test.go", matchUnaryAssertions)
		deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
		deliveryCommand(t, first, tool, true, nil, "vet", "./...")
		want := "Plan { Mode: Mode::Workers(3), Enabled: true, Delta: -3, Steps: [99, 4] }\n{\"Mode\":{\"Workers\":3},\"Enabled\":true,\"Delta\":-3,\"Steps\":[99,4]}\n"
		if got := deliveryCommand(t, first, tool, true, nil, "run", "-race", "."); got != want {
			t.Fatalf("business output %q; want %q", got, want)
		}
	})
	for _, tc := range []struct {
		name, body string
		words      []string
	}{
		{"overflow", `type S struct{N uint8};func Bad(c cfg::Mode)S{return S{N:match c{cfg::Mode::Off=>256,cfg::Mode::Workers(_)=>0}}}`, []string{"overflow", "assignable"}},
		{"unary-bool", `func Bad(c cfg::Mode)bool{return !(match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>0})}`, []string{"operator !", "boolean", "not assignable"}},
		{"unary-number", `func Bad(c cfg::Mode)int{return -(match c{cfg::Mode::Off=>true,cfg::Mode::Workers(_)=>false})}`, []string{"operator -", "not assignable"}},
		{"defined-identity", `func Bad(c cfg::Mode)[]cfg::Count{return []cfg::Count{match c{cfg::Mode::Off=>cfg::Other(1),cfg::Mode::Workers(_)=>cfg::Other(2)}}}`, []string{"not assignable", "cannot use", "different Go types"}},
		{"map-key-value-identity", `func Bad(c cfg::Mode)map[cfg::Key]cfg::Value{return map[cfg::Key]cfg::Value{match c{cfg::Mode::Off=>cfg::Value(1),cfg::Mode::Workers(_)=>cfg::Value(2)}:3}}`, []string{"not assignable", "cannot use", "different Go types"}},
		{"non-nilable", `type S struct{N int};func Bad(c cfg::Mode)S{return S{N:match c{cfg::Mode::Off=>nil,cfg::Mode::Workers(_)=>nil}}}`, []string{"cannot use nil", "untyped nil", "not assignable"}},
		{"unknown-field", `type S struct{N int};func Bad(c cfg::Mode)S{return S{Missing:match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}}}`, []string{"unknown struct literal field", "unknown field"}},
		{"duplicate-field", `type S struct{N int};func Bad(c cfg::Mode)S{return S{N:match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2},N:3}}`, []string{"duplicate field"}},
		{"map-key-type", `func Bad(c cfg::Mode)map[[]int]int{return map[[]int]int{match c{cfg::Mode::Off=>[]int{1},cfg::Mode::Workers(_)=>[]int{2}}:3}}`, []string{"invalid map key"}},
		{"duplicate-map-key", `func Bad(c cfg::Mode)map[int]int{return map[int]int{1:match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2},1:3}}`, []string{"duplicate key"}},
		{"array-runtime-key", `func Bad(c cfg::Mode)[2]int{return [2]int{(match c{cfg::Mode::Off=>0,cfg::Mode::Workers(_)=>1}):3}}`, []string{"constant"}},
		{"slice-runtime-key", `func Bad(c cfg::Mode)[]int{return []int{(match c{cfg::Mode::Off=>0,cfg::Mode::Workers(_)=>1}):3}}`, []string{"constant"}},
		{"shift-count", `func Bad(c cfg::Mode)[]uint64{return []uint64{1<<(match c{cfg::Mode::Off=>1.5,cfg::Mode::Workers(_)=>2.5})}}`, []string{"shift count", "non-integer", "must be integer"}},
		{"unaddressable-composite", `type S struct{N int};func(*S)Write([]int){};func Bad(c cfg::Mode){(S{N:0}).Write([]int{match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}})}`, []string{"cannot call pointer method", "not addressable"}},
		{"readonly-container", `type S struct{Values []int};func Bad(c cfg::Mode,r &S){v:=[][]int{match c{cfg::Mode::Off=>r.Values,cfg::Mode::Workers(_)=>r.Values}};v[0][0]=9}`, []string{"read-only", "readonly", "permission", "cannot erase"}},
		{"readonly-erasure", `func Bad(c cfg::Mode,r &int){v:=[]any{match c{cfg::Mode::Off=>r,cfg::Mode::Workers(_)=>r}};_=v}`, []string{"read-only", "readonly", "permission", "cannot erase"}},
	} {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			source := sourceFor(tc.body + "\nfunc main(){}\n")
			output := deliveryCommand(t, outside, cli, false, nil, "build", "-o", filepath.Join(t.TempDir(), "rejected"), source)
			if !strings.Contains(output, "app.gox") {
				t.Fatalf("unlocated rejection: %s", output)
			}
			if strings.Contains(output, "expression match unsupported") {
				t.Fatalf("type negative stopped at unsupported syntax: %s", output)
			}
			found := false
			for _, word := range tc.words {
				found = found || strings.Contains(output, word)
			}
			if tc.name == "defined-identity" {
				found = found && (strings.Contains(output, "Count") || strings.Contains(output, "Alias")) && strings.Contains(output, "Other")
			}
			if tc.name == "map-key-value-identity" {
				found = found && strings.Contains(output, "Key") && strings.Contains(output, "Value")
			}
			if !found {
				t.Fatalf("wrong error category (want one of %q): %s", tc.words, output)
			}
		})
	}
	for name, body := range map[string]string{
		"address-composite": `type S struct{N int};func Bad(c cfg::Mode)*S{return &S{N:match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}}}`,
		"address-value":     `func Bad(c cfg::Mode)*int{return &(match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2})}`,
		"dereference":       `func Bad(c cfg::Mode,p *int)int{return *(match c{cfg::Mode::Off=>p,cfg::Mode::Workers(_)=>p})}`,
		"receive":           `func Bad(c cfg::Mode,ch chan int)int{return <-(match c{cfg::Mode::Off=>ch,cfg::Mode::Workers(_)=>ch})}`,
	} {
		t.Run("boundary/"+name, func(t *testing.T) {
			source := sourceFor(body)
			output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(output, "app.gox") {
				t.Fatalf("unlocated boundary: %s", output)
			}
		})
	}
}

const matchUnaryModel = `package model
use serde;
#[derive(serde::Serialize,serde::Deserialize,Clone,Debug)]
pub enum Mode { Off, Workers(int), }
#[derive(serde::Serialize,serde::Deserialize,Clone,Debug)]
type Plan struct { Mode Mode; Enabled bool; Delta int; Steps []int }
`
const matchUnaryScalars = `package model
type Count int
type Alias = Count
type Other int
type Key string
type Value uint64
type Word = uint64
`
const matchUnaryBusiness = `
use fmt;
use serde::json as serdejson;
#[derive(Error)]
#[error("plan input")]
type Failure struct {
 #[from]
 Cause error
}
func Build(c cfg::Mode)cfg::Plan{return cfg::Plan{
 Mode:c,
 Enabled:!(match c{cfg::Mode::Off=>true,cfg::Mode::Workers(_)=>false}),
 Delta:-(match c{cfg::Mode::Off=>0,cfg::Mode::Workers(n)=>n}),
 Steps:[]int{match c{cfg::Mode::Off=>0,cfg::Mode::Workers(n)=>n},+(match c{cfg::Mode::Off=>1,cfg::Mode::Workers(n)=>n+1})},
}}
func Run(input []byte)(string,string,string,error){
 c,e:=serdejson::Unmarshal(input,cfg::DeserializeMode);if e!=nil{return "","","",FromFailure(e)}
 original:=Build(c);copy:=cfg::ClonePlan(&original);copy.Steps[0]=99
 before:=cfg::DebugPlan(&original);after:=cfg::DebugPlan(&copy)
 wire,e:=serdejson::Marshal(&copy,cfg::SerializePlan);return before,after,string(wire),e
}
func Empty(c cfg::Mode)cfg::Plan{return cfg::Plan{Mode:c,Steps:match c{cfg::Mode::Off=>nil,cfg::Mode::Workers(_)=>[]int{}}}}
func main(){_,debug,wire,e:=Run([]byte("{\"Workers\":3}"));if e!=nil{panic(e)};fmt::Println(debug);fmt::Println(wire)}
`
const matchUnaryContexts = `package main
use model as cfg;
type Wide = uint64
type Context struct{Bits Wide;Count cfg::Alias;Pointer *int}
type Tiny struct{N int}
func Contexts(c cfg::Mode,n uint)Context{type Wide=int8;_=Wide(1);return Context{Bits:+(match c{cfg::Mode::Off=>1<<n,cfg::Mode::Workers(_)=>0}),Count:match c{cfg::Mode::Off=>cfg::Count(7),cfg::Mode::Workers(_)=>cfg::Count(8)},Pointer:match c{cfg::Mode::Off=>nil,cfg::Mode::Workers(_)=>nil}}}
func Unary(c cfg::Mode)(int,int,uint64,bool){a:=-(match c{cfg::Mode::Off=>2,cfg::Mode::Workers(n)=>n});var b int=+(match c{cfg::Mode::Off=>3,cfg::Mode::Workers(n)=>n});var bits uint64=^(match c{cfg::Mode::Off=>0,cfg::Mode::Workers(_)=>1});flag:=!(match c{cfg::Mode::Off=>false,cfg::Mode::Workers(_)=>true});return a,b,bits,flag}
func Local(c cfg::Mode)int{type N int;x:=[]N{match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}};return int(x[0])}
func Maps(c cfg::Mode,n uint)map[cfg::Key]cfg::Value{return map[cfg::Key]cfg::Value{match c{cfg::Mode::Off=>"off",cfg::Mode::Workers(_)=>"on"}:match c{cfg::Mode::Off=>1<<n,cfg::Mode::Workers(_)=>7}}}
func Array(c cfg::Mode)[4]int{return [...]int{2:match c{cfg::Mode::Off=>5,cfg::Mode::Workers(_)=>6},+(match c{cfg::Mode::Off=>7,cfg::Mode::Workers(_)=>8})}}
func NestedLiteral(c cfg::Mode)[][]*Tiny{return [][]*Tiny{{{N:match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}},{N:-(match c{cfg::Mode::Off=>3,cfg::Mode::Workers(_)=>4})}}}}
func sum(s []int)int{n:=0;for _,v:=range s{n+=v};return n}
// This is the former composition-boundary literal, now positively executed.
func CompositionLiteral(c cfg::Mode)int{return sum([]int{match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}})}
func ArmLiteral(c cfg::Mode)[]int{return match c{cfg::Mode::Off=>[]int{match c{cfg::Mode::Off=>3,cfg::Mode::Workers(_)=>4}},cfg::Mode::Workers(n)=>[]int{n}}}
func ShiftCount(c cfg::Mode)[]uint64{return []uint64{1<<(match c{cfg::Mode::Off=>3,cfg::Mode::Workers(_)=>4})}}
func LoopControl(c cfg::Mode)int{count:=0;for i:=0;i<3;i++{v:=[]int{match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>0}};match c{cfg::Mode::Off=>{count+=v[0];break},cfg::Mode::Workers(_)=>{}};count+=100};for i:=0;i<3;i++{v:=[]int{match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>0}};match c{cfg::Mode::Off=>{count+=v[0];continue},cfg::Mode::Workers(_)=>{}};count+=100};return count}
func Goto(c cfg::Mode)int{skip:=true;if skip{goto Done};_=Tiny{N:match c{cfg::Mode::Off=>1,cfg::Mode::Workers(_)=>2}};Done:return 9}
`
const matchUnaryFlows = `package main
use errors;
use model as cfg;
var Trace []string
var stopped=errors::New("checked failure")
func mark(label string,n int)int{Trace=append(Trace,label);return n}
func record(label string){Trace=append(Trace,label)}
func choice(c cfg::Mode)cfg::Mode{record("target");return c}
func guard(n int)bool{record("guard");return n>0}
func checked(ok bool,n int)Result[int,error]{record("checked");if !ok{return Result[int,error]::Err(stopped)};return Result[int,error]::Ok(n)}
type Bundle struct{First int;Values []int;Last int}
func collect(c cfg::Mode,ok bool)Result[Bundle,error]{defer record("defer");b:=Bundle{First:mark("before",1),Values:[]int{-(match choice(c){cfg::Mode::Off=>mark("off",0),cfg::Mode::Workers(n) if guard(n)=>checked(ok,n)?,cfg::Mode::Workers(_)=>0}),mark("element-after",2)},Last:mark("field-after",3)};return Result[Bundle,error]::Ok(b)}
func Collect(c cfg::Mode,ok bool)(Bundle,error){match collect(c,ok){Result[Bundle,error]::Ok(v)=>{return v,nil},Result[Bundle,error]::Err(e)=>{return Bundle{},e}};panic("unreachable")}
func mapOrder(c cfg::Mode)map[int]int{return map[int]int{match choice(c){cfg::Mode::Off=>mark("key1",1),cfg::Mode::Workers(_)=>mark("key1",1)}:match c{cfg::Mode::Off=>mark("value1",2),cfg::Mode::Workers(_)=>mark("value1",2)},mark("key2",3):mark("value2",4)}}
func receiveOrder(c cfg::Mode,ch <-chan int)[]int{return []int{<-ch,match c{cfg::Mode::Off=>receiveMiddle(ch),cfg::Mode::Workers(_)=>0},<-ch}}
func receiveMiddle(ch <-chan int)int{if len(ch)!=1{panic("receive moved across match")};return mark("middle",2)}
func Short(c cfg::Mode)bool{v:=false&&!(match choice(c){cfg::Mode::Off=>false,cfg::Mode::Workers(_)=>true});return v}
func DirectRecover()(out bool){defer func(){v:=[]bool{!(match cfg::Mode::Off{cfg::Mode::Off=>recover()==nil,cfg::Mode::Workers(_)=>true})};out=v[0]}();panic("direct")}
func deferred(c cfg::Mode,ok bool)Result[int,error]{defer record("cleanup");v:=[]int{mark("first",1),match c{cfg::Mode::Off=>checked(ok,2)?,cfg::Mode::Workers(_)=>0},mark("last",3)};defer consume(v);return Result[int,error]::Ok(1)}
func consume([]int){record("registered")}
func Deferred(c cfg::Mode,ok bool)bool{return match deferred(c,ok){Result[int,error]::Ok(_)=>true,Result[int,error]::Err(_)=>false}}
type WithClosure struct{First int;Run func()Result[int,error];Last int}
func Nearest(c cfg::Mode)int{x:=WithClosure{First:match c{cfg::Mode::Off=>mark("outer-first",1),cfg::Mode::Workers(_)=>0},Run:func()Result[int,error]{defer record("inner-defer");v:=[]int{match c{cfg::Mode::Off=>checked(false,1)?,cfg::Mode::Workers(_)=>0},mark("never",9)};return Result[int,error]::Ok(v[0])},Last:match c{cfg::Mode::Off=>mark("outer-last",2),cfg::Mode::Workers(_)=>0}};_=x.Run();return mark("outer",8)}
func Rebind(c cfg::Mode)(int,int){a:=[]int{1};old:=a;replace:=func()int{a=[]int{9};return mark("arm",3)};a[mark("index",0)]=-(match choice(c){cfg::Mode::Off=>replace(),cfg::Mode::Workers(_)=>0});return old[0],a[0]}
func Bounds(c cfg::Mode){a:=[]int{};a[mark("index",2)]=-(match choice(c){cfg::Mode::Off=>mark("arm",3),cfg::Mode::Workers(_)=>0})}
func indexFailure(c cfg::Mode)Result[int,error]{a:=[]int{};a[mark("index",2)]=-(match choice(c){cfg::Mode::Off=>checked(false,1)?,cfg::Mode::Workers(_)=>0});return Result[int,error]::Ok(1)}
func IndexFailure(c cfg::Mode)bool{return match indexFailure(c){Result[int,error]::Ok(_)=>true,Result[int,error]::Err(_)=>false}}
`
const matchUnaryAssertions = `package main
import("testing";"reflect";"errors";"strings";cfg "packages.example/app/model";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func traceIs(t *testing.T,want []string){t.Helper();if !reflect.DeepEqual(Trace,want){t.Fatalf("trace=%v want=%v",Trace,want)};Trace=nil}
func TestBusiness(t *testing.T){for _,tc:=range []struct{input,before,after,wire string}{
 {"{\"Workers\":3}","Plan { Mode: Mode::Workers(3), Enabled: true, Delta: -3, Steps: [3, 4] }","Plan { Mode: Mode::Workers(3), Enabled: true, Delta: -3, Steps: [99, 4] }","{\"Mode\":{\"Workers\":3},\"Enabled\":true,\"Delta\":-3,\"Steps\":[99,4]}"},
 {"\"Off\"","Plan { Mode: Mode::Off, Enabled: false, Delta: 0, Steps: [0, 1] }","Plan { Mode: Mode::Off, Enabled: false, Delta: 0, Steps: [99, 1] }","{\"Mode\":\"Off\",\"Enabled\":false,\"Delta\":0,\"Steps\":[99,1]}"},
 }{before,after,wire,e:=Run([]byte(tc.input));if e!=nil||before!=tc.before||after!=tc.after||wire!=tc.wire{t.Fatal(before,after,wire,e)};decoded,e:=serdejson.Unmarshal([]byte(wire),cfg.DeserializePlan);if e!=nil||cfg.DebugPlan(&decoded)!=after{t.Fatal(decoded,e)}}}
func TestNilEmptyAndErrorPaths(t *testing.T){for _,tc:=range []struct{mode cfg.Mode;nilSlice bool;text string}{{cfg.NewModeOff(),true,"nil"},{cfg.NewModeWorkers(0),false,"[]"}}{original:=Empty(tc.mode);copy:=cfg.ClonePlan(&original);if (original.Steps==nil)!=tc.nilSlice||(copy.Steps==nil)!=tc.nilSlice||!strings.Contains(cfg.DebugPlan(&copy),"Steps: "+tc.text){t.Fatal(original,copy)};wire,e:=serdejson.Marshal(&copy,cfg.SerializePlan);if e!=nil||!strings.Contains(string(wire),"\"Steps\":[]"){t.Fatal(string(wire),e)}};_,_,_,e:=Run([]byte("{\"Workers\":false}"));var detail *serde.Error;if !errors.As(e,&detail)||e.Error()!="plan input"||errors.Unwrap(e)!=error(detail)||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Variant",Name:"Workers"}}){t.Fatalf("cause/path=%#v %v",detail,e)}}
func TestRealContexts(t *testing.T){c:=cfg.NewModeOff();v:=Contexts(c,63);if v.Bits!=uint64(1)<<63||v.Count!=cfg.Count(7)||v.Pointer!=nil{t.Fatal(v)};a,b,bits,flag:=Unary(c);if a!=-2||b!=3||bits!=^uint64(0)||!flag||Local(c)!=1||CompositionLiteral(c)!=1||Goto(c)!=9||LoopControl(c)!=4{t.Fatal(a,b,bits,flag)};m:=Maps(c,63);if m[cfg.Key("off")]!=cfg.Value(1)<<63||!reflect.DeepEqual(Array(c),[4]int{0,0,5,7})||!reflect.DeepEqual(ShiftCount(c),[]uint64{8})||!reflect.DeepEqual(ArmLiteral(c),[]int{3}){t.Fatal("literal context")};nested:=NestedLiteral(c);if nested[0][0]==nested[0][1]||nested[0][0].N!=1||nested[0][1].N!=-3{t.Fatal(nested)}}
func TestSelectionAndFailure(t *testing.T){c:=cfg.NewModeWorkers(3);Trace=nil;v,e:=Collect(c,true);if e!=nil||v.First!=1||v.Last!=3||!reflect.DeepEqual(v.Values,[]int{-3,2}){t.Fatal(v,e)};traceIs(t,[]string{"before","target","guard","checked","element-after","field-after","defer"});_,e=Collect(c,false);if !errors.Is(e,stopped){t.Fatal(e)};traceIs(t,[]string{"before","target","guard","checked","defer"});_,e=Collect(cfg.NewModeWorkers(0),true);if e!=nil{t.Fatal(e)};traceIs(t,[]string{"before","target","guard","element-after","field-after","defer"});if Short(c){t.Fatal("short circuit")};traceIs(t,nil)}
func TestInvalidEnumBeforeGuard(t *testing.T){for _,value:=range []cfg.Mode{{}}{Trace=nil;func(){defer func(){if recover()==nil{t.Errorf("accepted invalid %T",value)}}();_,_=Collect(value,true)}();traceIs(t,[]string{"before","target","defer"})}}
func TestCallsAndReceives(t *testing.T){c:=cfg.NewModeOff();Trace=nil;m:=mapOrder(c);if m[1]!=2||m[3]!=4{t.Fatal(m)};traceIs(t,[]string{"target","key1","value1","key2","value2"});ch:=make(chan int,2);ch<-1;ch<-3;if got:=receiveOrder(c,ch);!reflect.DeepEqual(got,[]int{1,2,3})||len(ch)!=0{t.Fatal(got)};traceIs(t,[]string{"middle"})}
func TestFunctionFrames(t *testing.T){c:=cfg.NewModeOff();Trace=nil;if Nearest(c)!=8{t.Fatal("nearest frame")};traceIs(t,[]string{"outer-first","outer-last","checked","inner-defer","outer"});if !DirectRecover(){t.Fatal("direct recover changed frames")};if Deferred(c,false){t.Fatal("failure lost")};traceIs(t,[]string{"first","checked","cleanup"});if !Deferred(c,true){t.Fatal("success lost")};traceIs(t,[]string{"first","checked","last","registered","cleanup"})}
func TestExistingIndexPlace(t *testing.T){c:=cfg.NewModeOff();Trace=nil;old,current:=Rebind(c);if old!=-3||current!=9{t.Fatal(old,current)};traceIs(t,[]string{"index","target","arm"});func(){defer func(){if recover()==nil{t.Error("bounds panic lost")}}();Bounds(c)}();traceIs(t,[]string{"index","target","arm"});if IndexFailure(c){t.Fatal("failed RHS reached write")};traceIs(t,[]string{"index","target","checked"})}
`
