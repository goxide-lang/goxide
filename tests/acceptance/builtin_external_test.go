package acceptance_test

import (
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/testdeps"
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
func Nested()Result[E,error]{return Result[E,error]::Ok(nil)}
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
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "sums.example/lib/model"
type Maybe[T any]=m.Option[T]
type Outcome[T,E any]=m.Result[T,E]
type Swap[A,B any]=m.Result[B,A]
type Fixed[T any]=m.Result[T,string]
type Numeric[T ~int]=m.Option[T]
type Wrap[T any]=m.Option[struct{T int;V T}]
type Int=m.Option[int]
type Private=m.Option[m.Exposed]
type New m.Option[int]
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
func OptionFlow(ok bool)m::Option[int]{n:=m::Pick(ok)?;return m::Option[int]::Some(n+1)}
func ResultFlow(kind int)m::Result[string,error]{n:=m::Load(kind)?;_=n;return m::Result[string,error]::Ok("done")}
func Wrapped(kind int)Result[int,LocalError]{n:=m::Load(kind)?;return Result[int,LocalError]::Ok(n)}
func Match(v m::Option[int])int{return match v{m::Option[int]::Some(n)=>n,m::Option[int]::None=>0,}}
func Alias()int{v:=r::Numeric[int]::Some(9);return match v{r::Int::Some(n)=>n,r::Maybe[int]::None=>0,}}
func Reorder()int{v:=r::Swap[error,int]::Ok(10);return match v{r::Outcome[int,error]::Ok(n)=>n,r::Swap[error,int]::Err(_)=>0,}}
func Fixed()int{v:=r::Fixed[int]::Ok(11);return match v{r::Fixed[int]::Ok(n)=>n,r::Fixed[int]::Err(_)=>0,}}
func Private()Option[int]{n:=m::Private()?;return Option[int]::Some(n.N)}
func Shape()int{v:=r::Wrap[int]::Some(struct{T int;V int}{T:2,V:3});return match v{r::Wrap[int]::Some(n)=>n.T+n.V,r::Wrap[int]::None=>0,}}
func PrivateMatch()int{return match m::Private(){r::Private::Some(n)=>n.N,r::Private::None=>0,}}
func Forward(){_=m::Secret()}
func Invalid(v m::Result[int,error],seen *bool)Result[int,error]{n:=v?;*seen=true;return Result[int,error]::Ok(n)}
func Nested()Result[int,error]{_=m::Nested()?;return Result[int,error]::Ok(1)}
func NestedNone()Option[int]{v:=m::OptionalNested()?;n:=v?;return Option[int]::Some(n)}
func Guard(v m::Option[int],seen *bool)int{return match v{m::Option[int]::Some(n) if Mark(seen)=>n,_=>0,}}
func Mark(seen *bool)bool{*seen=true;return true}
func Shadow()Result[int,error]{input:=m::Load(0);m:=3;_=m;n:=input?;return Result[int,error]::Ok(n)}
// A new concrete shared-family instance gets a local static validator.
func NewInstance(v m::Option[bool])Option[bool]{n:=v?;return Option[bool]::Some(n)}
func SumLarge(items []int)Result[int,error]{total:=0;for _,item:=range items{total+=m::LoadNumber(item)?};return Result[int,error]::Ok(total)}
`)
	out := filepath.Join(t.TempDir(), "out")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, app)
	// Compare before adding Go-only test files; no test source participates in artifacts.
	compare := func(source, first string) {
		second := filepath.Join(t.TempDir(), "repeat")
		packagesCommand(t, root, cli, true, "transpile", "-o", second, source)
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
import("testing";"github.com/goxide-lang/serde/json")
func TestProducer(t *testing.T){for _,wire:=range []string{"{}",`+"`"+`{"Value":null}`+"`"+`}{v,e:=serdejson.Unmarshal([]byte(wire),DeserializeConfig);if e!=nil||v.Value!=nil{t.Fatal(v,e)}};v,e:=serdejson.Unmarshal([]byte(`+"`"+`{"Value":3}`+"`"+`),DeserializeConfig);if e!=nil||v.Value.(GoxideOptionSome[int]).Value!=3{t.Fatal(v,e)}}
`)
	packagesWrite(t, out, "app_test.go", `package app
import("testing";"errors";m "sums.example/lib/model")
func TestBusiness(t *testing.T){if Match(OptionFlow(true))!=6||Match(OptionFlow(false))!=0||Alias()!=9||Reorder()!=10||Fixed()!=11||PrivateMatch()!=7||Shape()!=5{t.Fatal("business")};if Private().(__goxide_enum_case_Option_Some[int]).Value!=7{t.Fatal("private")};if ResultFlow(0).(m.GoxideResultOk[string,error]).Value!="done"{t.Fatal("result")};if !errors.Is(ResultFlow(1).(m.GoxideResultErr[string,error]).Value,m.Sentinel){t.Fatal("cause")};if e:=ResultFlow(2).(m.GoxideResultErr[string,error]).Value;e==nil{t.Fatal("typed nil lost")};if !errors.Is(Wrapped(1).(__goxide_enum_case_Result_Err[int,LocalError]).Value,m.Sentinel){t.Fatal("from")};if NestedNone()!=nil{t.Fatal("Some(None)")};_=Shadow()}
func TestNewConcreteInstance(t *testing.T){
 if NewInstance(nil)!=nil||!NewInstance(m.GoxideNewOptionSome(true)).(m.GoxideOptionSome[bool]).Value{t.Fatal("new concrete identity")}
 defer func(){if recover()==nil{t.Fatal("new concrete shape check missing")}}()
 NewInstance((*m.GoxideOptionSome[bool])(nil))
}
type forged struct{m.Result[int,error]}
func TestInvalid(t *testing.T){var typed *m.GoxideResultOk[int,error];for _,v:=range []m.Result[int,error]{nil,typed,&m.GoxideResultOk[int,error]{Value:1},forged{}}{func(){seen:=false;defer func(){if recover()==nil||seen{t.Fatal("invalid")}}();Invalid(v,&seen)}()};func(){defer func(){if recover()==nil{t.Fatal("nested validation")}}();Nested()}();func(){seen:=false;var typed *m.GoxideOptionSome[int];defer func(){if recover()==nil||seen{t.Fatal("guard")}}();Guard(typed,&seen)}()}
`)
	// Measure an optimized real consumer of a separately generated producer.
	// Values above the runtime's small-integer boxing cache expose per-item
	// escapes. Race semantics are covered above; allocation gates use plain Go.
	packagesWrite(t, out, "allocation_test.go", `//go:build !race

package app
import "testing"
var allocationSink int
func TestImportedResultAllocations(t *testing.T){
 for _,size:=range []int{1,32,1024}{
  items:=make([]int,size);want:=0
  for i:=range items{items[i]=1000+i;want+=items[i]+1}
  allocations:=testing.AllocsPerRun(100,func(){allocationSink=SumLarge(items).(__goxide_enum_case_Result_Ok[int,error]).Value})
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
import("testing";m "sums.example/lib/model";r "sums.example/lib/relay")
func TestGo(t *testing.T){var v r.Maybe[int]=m.GoxideNewOptionSome[int](4);if v.(m.GoxideOptionSome[int]).Value!=4||m.GoxideNewOptionNone[int]()!=nil{t.Fatal(v)};var p *m.Fault;v2:=m.GoxideNewResultErr[int,error](p);if v2.(m.GoxideResultErr[int,error]).Value==nil{t.Fatal("typednil")}}
`)
	for _, d := range []string{lib, out, plain} {
		packagesCommand(t, d, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, d, tool, true, "vet", "./...")
	}
	for name, tc := range map[string]struct{ body, want string }{
		"fake-family":                {`use library::fake as f;func Bad()Result[int,error]{n:=f::Get()?;return Result[int,error]::Ok(n)}`, "builtin nominal"},
		"private-unnameable":         {`use library::model as m;func Bad()Option[int]{n:=m::Secret()?;return Option[int]::Some(n.N)}`, "not nameable"},
		"external-from-not-implicit": {`use library::model as m;func Bad()m::Result[int,m::RemoteError]{n:=m::Load(1)?;return m::GoxideNewResultOk[int,m::RemoteError](n)}`, "unique explicit From"},
		"cross-family":               {`use library::model as m;func Bad()Option[int]{n:=m::Load(0)?;return Option[int]::Some(n)}`, "cannot propagate"},
		"non-exhaustive":             {`use library::model as m;func Bad(v m::Option[int])int{return match v{m::Option[int]::Some(n)=>n,}}`, "non-exhaustive"},
		"constrained-alias-ctor":     {`use library::relay as r;func Bad(){_=r::Numeric[string]::Some("bad")}`, "does not satisfy"},
		"constrained-alias-pattern":  {`use library::model as m; use library::relay as r;func Bad(v m::Option[string])int{return match v{r::Numeric[string]::Some(_)=>1,r::Numeric[string]::None=>0,}}`, "does not satisfy"},
		"new-defined":                {`use library::relay as r;func Bad()r::New{return r::New::Some(1)}`, "has no field or method Some"},
	} {
		t.Run(name, func(t *testing.T) {
			s := appFor("package app\n" + tc.body)
			msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), s)
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "app.gox:") {
				t.Fatal(msg)
			}
		})
	}
	meta := filepath.Join(lib, "model/.goxide.enums.json")
	original, e := os.ReadFile(meta)
	if e != nil {
		t.Fatal(e)
	}
	t.Run("family-tamper", func(t *testing.T) {
		var a map[string]any
		if e := json.Unmarshal(original, &a); e != nil {
			t.Fatal(e)
		}
		a["Metadata"].(map[string]any)["Families"].([]any)[0].(map[string]any)["Family"] = "Result"
		b, _ := json.Marshal(a)
		if e := testdeps.WriteFile(meta, b, 0600); e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(meta, original, 0600)
		msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), app)
		if !strings.Contains(msg, "metadata") {
			t.Fatal(msg)
		}
	})
	t.Run("missing-and-native", func(t *testing.T) {
		if e := os.Remove(meta); e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(meta, original, 0600)
		msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), app)
		if !strings.Contains(msg, "metadata") {
			t.Fatal(msg)
		}
		packagesCommand(t, plain, tool, true, "test", "-race", "-count=1", "./...")
	})
}
