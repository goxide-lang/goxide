package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCrossModuleBuiltinArtifacts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	module := func(name, lib string) string {
		text := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			text += fmt.Sprintf("require sums.example/lib v0.0.0\nreplace sums.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return text
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", module("sums.example/lib", ""))
	writeAcceptanceSerdeRoots(t, src, "")
	packagesWrite(t, src, "model/model.gox", `package model; use serde;
use errors;
var Sentinel=errors::New("failed")
type Fault struct{}
func(*Fault)Error()string{return "typed"}
type hidden struct{N int}
type Exposed=hidden
#[derive(serde::Serialize,serde::Deserialize)]
type Config struct{Value Option[int]}
type secret struct{N int}
func Private()Option[hidden]{return Option[hidden]::Some(hidden{N:7})}
func Secret()Option[secret]{return Option[secret]::Some(secret{N:8})}
func Load(kind int)Result[int,error]{if kind==1{return Result[int,error]::Err(Sentinel)};if kind==2{var e *Fault;return Result[int,error]::Err(e)};return Result[int,error]::Ok(4)}
func LoadNumber(n int)Result[int,error]{if n<0{return Result[int,error]::Err(Sentinel)};return Result[int,error]::Ok(n+1)}
func Text()Result[string,error]{return Result[string,error]::Ok("ok")}
func Pick(ok bool)Option[int]{if ok{return Option[int]::Some(5)};return Option[int]::None}
func Words()Result[int,string]{return Result[int,string]::Err("bad")}
pub enum E{Good,}
func OptionalE()Option[E]{return Option[E]::Some(E::Good)}
func String()Option[string]{return Option[string]::Some("word")}
func Shape()Option[struct{T int;V int}]{return Option[struct{T int;V int}]::Some(struct{T int;V int}{T:2,V:3})}
func OptionalNested()Option[Option[int]]{return Option[Option[int]]::Some(Option[int]::None)}
#[derive(Error)]
pub enum RemoteError{
 #[error("remote")]
 #[from]
 Cause(error),
}
func Remote()Result[int,RemoteError]{return Result[int,RemoteError]::Ok(1)}
`)
	packagesWrite(t, src, "model/nil.go", `package model
import "github.com/goxide-lang/std/sum"
func Nested()sum.Result[E,error]{return sum.NewOk[E,error](nil)}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import __borrow_std "github.com/goxide-lang/std/sum"

import m "sums.example/lib/model"
type Maybe[T any]=__borrow_std.Option[T]
type Outcome[T,E any]=__borrow_std.Result[T,E]
type Swap[A,B any]=__borrow_std.Result[B,A]
type Fixed[T any]=__borrow_std.Result[T,string]
type Numeric[T ~int]=__borrow_std.Option[T]
type Wrap[T any]=__borrow_std.Option[struct{T int;V T}]
type Int=__borrow_std.Option[int]
type Private=__borrow_std.Option[m.Exposed]
type New __borrow_std.Option[int]
func Get()Int{return m.Pick(true)}
`)
	packagesWrite(t, src, "fake/fake.go", `package fake
type Result[T,E any]interface{}
func Get()Result[int,error]{return nil}
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appFor := func(source string) string {
		d := t.TempDir()
		packagesWrite(t, d, "go.mod", module("sums.example/app", lib))
		writeAcceptanceSerdeRoots(t, d, "sums.example/lib", "model", "relay", "fake")
		// A final source newline is optional, including before imported preludes.
		packagesWrite(t, d, "app.gox", strings.TrimRight(source, "\n"))
		return d
	}
	app := appFor(`package app
use library::model as m; use library::relay as r;
#[derive(Error)]
pub enum LocalError{
 #[error("local")]
 #[from]
 Cause(error),
}
func OptionFlow(ok bool)Option[int]{n:=m::Pick(ok)?;return Option[int]::Some(n+1)}
func ResultFlow(kind int)Result[string,error]{n:=m::Load(kind)?;_=n;return Result[string,error]::Ok("done")}
func Wrapped(kind int)Result[int,LocalError]{n:=m::Load(kind)?;return Result[int,LocalError]::Ok(n)}
func Match(v Option[int])int{return match v{Option[int]::Some(n)=>n,Option[int]::None=>0,}}
func Alias()int{v:=r::Numeric[int]::Some(9);return match v{r::Int::Some(n)=>n,r::Maybe[int]::None=>0,}}
func Reorder()int{v:=r::Swap[error,int]::Ok(10);return match v{r::Outcome[int,error]::Ok(n)=>n,r::Swap[error,int]::Err(_)=>0,}}
func Fixed()int{v:=r::Fixed[int]::Ok(11);return match v{r::Fixed[int]::Ok(n)=>n,r::Fixed[int]::Err(_)=>0,}}
func Private()Option[int]{n:=m::Private()?;return Option[int]::Some(n.N)}
func Shape()int{v:=r::Wrap[int]::Some(struct{T int;V int}{T:2,V:3});return match v{r::Wrap[int]::Some(n)=>n.T+n.V,r::Wrap[int]::None=>0,}}
func PrivateMatch()int{return match m::Private(){r::Private::Some(n)=>n.N,r::Private::None=>0,}}
func Forward(){_=m::Secret()}
func Invalid(v Result[int,error],seen *bool)Result[int,error]{n:=v?;*seen=true;return Result[int,error]::Ok(n)}
func Nested()Result[int,error]{_=m::Nested()?;return Result[int,error]::Ok(1)}
func NestedNone()Option[int]{v:=m::OptionalNested()?;n:=v?;return Option[int]::Some(n)}
func Guard(v Option[int],seen *bool)int{return match v{Option[int]::Some(n) if Mark(seen)=>n,_=>0,}}
func MarkNested(seen *int)bool{*seen+=1;return true}
func GuardNested(v Option[m::E],seen *int)int{return match v{Option[m::E]::Some(_) if MarkNested(seen)=>1,_=>0,}}
func PatternNested(v Option[m::E],seen *int)int{return match v{Option[m::E]::Some(m::E::Good) if MarkNested(seen)=>1,_=>0,}}
func BindNested(v Option[m::E],seen *int)m::E{match v{Option[m::E]::Some(p)=>{*seen+=1;return p},Option[m::E]::None=>{return m::E::Good},}}
func DispatchNested(v m::E,seen *int){match v{m::E::Good if MarkNested(seen)=>{},_=>{},}}
func Mark(seen *bool)bool{*seen=true;return true}
func Shadow()Result[int,error]{input:=m::Load(0);m:=3;_=m;n:=input?;return Result[int,error]::Ok(n)}
// A new concrete shared-family instance retains its static sum identity.
func NewInstance(v Option[bool])Option[bool]{n:=v?;return Option[bool]::Some(n)}
func SumLarge(items []int)Result[int,error]{total:=0;for _,item:=range items{total+=m::LoadNumber(item)?};return Result[int,error]::Ok(total)}
`)
	out := filepath.Join(t.TempDir(), "out")
	compileAcceptanceGraph(t, true, app, out, src)
	// Compare before adding Go-only test files; no test source participates in artifacts.
	compare := func(source, first string) {
		second := filepath.Join(t.TempDir(), "repeat")
		if source == src {
			packagesCommand(t, root, cli, true, "transpile", "-o", second, src)
		} else {
			compileAcceptanceGraph(t, true, source, second, src)
		}
		err := filepath.WalkDir(first, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
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
			if string(a) != string(b) {
				return fmt.Errorf("nondeterministic %s", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	compare(src, lib)
	compare(app, out)
	packagesWrite(t, lib, "model/model_test.go", `package model
import __borrow_std "github.com/goxide-lang/std/sum"
import("testing";"github.com/goxide-lang/serde/json")
func TestProducer(t *testing.T){for _,wire:=range []string{"{}",`+"`"+`{"Value":null}`+"`"+`}{v,e:=serdejson.Unmarshal([]byte(wire),DeserializeConfig);if e!=nil||__borrow_std.TagOption(v.Value)!=0{t.Fatal(v,e)}};v,e:=serdejson.Unmarshal([]byte(`+"`"+`{"Value":3}`+"`"+`),DeserializeConfig);if e!=nil||__borrow_std.ValueSome(v.Value)!=3{t.Fatal(v,e)}}
`)
	packagesWrite(t, out, "app_test.go", `package app
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"errors";"fmt";"strings";m "sums.example/lib/model")
func TestBusiness(t *testing.T){if Match(OptionFlow(true))!=6||Match(OptionFlow(false))!=0||Alias()!=9||Reorder()!=10||Fixed()!=11||PrivateMatch()!=7||Shape()!=5{t.Fatal("business")};if __borrow_std.ValueSome(Private())!=7{t.Fatal("private")};if __borrow_std.ValueOk(ResultFlow(0))!="done"{t.Fatal("result")};if !errors.Is(__borrow_std.ValueErr(ResultFlow(1)),m.Sentinel){t.Fatal("cause")};if e:=__borrow_std.ValueErr(ResultFlow(2));e==nil{t.Fatal("typed nil lost")};if !errors.Is(__borrow_std.ValueErr(Wrapped(1)),m.Sentinel){t.Fatal("from")};if __borrow_std.TagOption(NestedNone())!=0{t.Fatal("Some(None)")};_=Shadow()}
func TestNewConcreteInstance(t *testing.T){
 if __borrow_std.TagOption(NewInstance(__borrow_std.None[bool]()))!=0||!__borrow_std.ValueSome(NewInstance(__borrow_std.NewSome(true))){t.Fatal("new concrete identity")}
}
// The current sum is checked first; a Go-supplied nil user enum is checked only
// when an explicit nested pattern or subsequent match dispatches that value.
func TestInvalid(t *testing.T){
 func(){seen:=false;defer func(){if recover()==nil||seen{t.Fatal("invalid Result reached continuation")}}();Invalid(__borrow_std.Result[int,error]{},&seen)}()
 if got:=__borrow_std.ValueOk(Nested());got!=1{t.Fatal("Result propagation traversed its payload",got)}
 for _,inner:=range []m.E{nil,(*m.Good)(nil)}{
  input:=__borrow_std.NewSome[m.E](inner);seen:=0
  if GuardNested(input,&seen)!=1||seen!=1{t.Fatal("binding-only Option match skipped guard",seen)}
  seen=0;func(){defer func(){if p:=recover();!strings.Contains(fmt.Sprint(p),"nil E")||seen!=0{t.Fatalf("nested pattern panic=%v guard=%d",p,seen)}}();PatternNested(input,&seen)}()
  bound:=BindNested(input,&seen);if bound!=inner||seen!=1{t.Fatal("binding changed nil payload or skipped arm",seen)}
  seen=0;func(){defer func(){if p:=recover();!strings.Contains(fmt.Sprint(p),"nil E")||seen!=0{t.Fatalf("bound enum panic=%v guard=%d",p,seen)}}();DispatchNested(bound,&seen)}()
 }
 matched:=0;if PatternNested(__borrow_std.NewSome[m.E](&m.Good{}),&matched)!=1||matched!=1{t.Fatal("valid imported nested pattern",matched)}
 seen:=false;if Guard(__borrow_std.None[int](),&seen)!=0||seen{t.Fatal("None executed Some guard")}
}
`)
	// Measure an optimized real consumer of a separately generated producer.
	// Values above the runtime's small-integer boxing cache expose per-item
	// escapes. Race semantics are covered above; allocation gates use plain Go.
	packagesWrite(t, out, "allocation_test.go", `//go:build !race

package app
import __borrow_std "github.com/goxide-lang/std/sum"

import "testing"
var allocationSink int
func TestImportedResultAllocations(t *testing.T){
 for _,size:=range []int{1,32,1024}{
  items:=make([]int,size);want:=0
  for i:=range items{items[i]=1000+i;want+=items[i]+1}
  allocations:=testing.AllocsPerRun(100,func(){allocationSink=__borrow_std.ValueOk(SumLarge(items))})
  if allocationSink!=want{t.Fatalf("sum=%d want=%d",allocationSink,want)}
  // One escaping final Result is allowed; no escaping Result per item.
  if allocations>1{t.Fatalf("size=%d allocations=%g; want at most final Result allocation",size,allocations)}
 }
}
`)
	packagesCommand(t, out, tool, true, "test", "-count=1", "-run", "^TestImportedResultAllocations$", ".")
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", module("sums.example/plain", lib))
	packagesWrite(t, plain, "plain_test.go", `package plain
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";m "sums.example/lib/model";r "sums.example/lib/relay")
func TestGo(t *testing.T){var v r.Maybe[int]=__borrow_std.NewSome[int](4);if __borrow_std.ValueSome(v)!=4||__borrow_std.TagOption(__borrow_std.None[int]())!=0{t.Fatal(v)};var p *m.Fault;v2:=__borrow_std.NewErr[int,error](p);if __borrow_std.ValueErr(v2)==nil{t.Fatal("typednil")}}
`)
	for _, d := range []string{lib, out, plain} {
		packagesCommand(t, d, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, d, tool, true, "vet", "./...")
	}
	assertNativeGoRejected(t, out, tool, map[string]nativeGoRejection{
		"option-nil":    {"package app\nimport __borrow_std \"github.com/goxide-lang/std/sum\"\nvar _ __borrow_std.Option[int]=nil\n", "cannot use nil"},
		"forged-result": {"package app\nimport __borrow_std \"github.com/goxide-lang/std/sum\"\ntype forged struct{__borrow_std.Result[int,error]}\nvar _ __borrow_std.Result[int,error]=forged{}\n", "cannot use"},
	})
	for name, tc := range map[string]struct{ body, want string }{
		"fake-family":                {`use library::fake as f;func Bad()Result[int,error]{n:=f::Get()?;return Result[int,error]::Ok(n)}`, "requires Option or Result"},
		"private-unnameable":         {`use library::model as m;func Bad()Option[int]{n:=m::Secret()?;return Option[int]::Some(n.N)}`, "not nameable"},
		"external-from-not-implicit": {`use library::model as m;func Bad()Result[int,m::RemoteError]{n:=m::Load(1)?;return Result[int,m::RemoteError]::Ok(n)}`, "unique explicit From"},
		"cross-family":               {`use library::model as m;func Bad()Option[int]{n:=m::Load(0)?;return Option[int]::Some(n)}`, "return the same Option or Result family"},
		"non-exhaustive":             {`use library::model as m;func Bad(v Option[int])int{return match v{Option[int]::Some(n)=>n,}}`, "does not cover Option::None"},
		"constrained-alias-ctor":     {`use library::relay as r;func Bad(){_=r::Numeric[string]::Some("bad")}`, "does not satisfy"},
		"constrained-alias-pattern":  {`use library::model as m; use library::relay as r;func Bad(v Option[string])int{return match v{r::Numeric[string]::Some(_)=>1,r::Numeric[string]::None=>0,}}`, "does not satisfy"},
		"new-defined":                {`use library::relay as r;func Bad()r::New{return r::New::Some(1)}`, "unknown associated type or member path"},
	} {
		t.Run(name, func(t *testing.T) {
			s := appFor("package app\n" + tc.body)
			msg := compileAcceptanceGraph(t, false, s, filepath.Join(t.TempDir(), "out"), src)
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "app.gox:") {
				t.Fatal(msg)
			}
		})
	}
}
