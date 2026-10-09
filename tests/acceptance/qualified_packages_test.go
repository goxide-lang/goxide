package acceptance_test

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/rootconfig"
)

func qualifiedSource(t *testing.T) (string, string) {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	source := filepath.Join(t.TempDir(), "source")
	example := filepath.Join(root, "examples/qualified_packages")
	err := filepath.WalkDir(example, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(example, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(source, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return testdeps.WriteFile(dst, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, source, "go.mod", "module qualified.example/app\ngo 1.27\n")
	return source, root
}

func compileQualifiedModule(t *testing.T, source, destination string) (frontend.ModuleResult, error) {
	t.Helper()
	roots, err := rootconfig.Load(source)
	if err != nil {
		t.Fatal(err)
	}
	return frontend.CompileModuleWithOptions(source, destination, frontend.ModuleOptions{Roots: roots})
}

func TestQualifiedPackagesApplicationRace(t *testing.T) {
	source, _ := qualifiedSource(t)
	dest := filepath.Join(t.TempDir(), "generated")
	if _, err := compileQualifiedModule(t, source, dest); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, dest, "qualified_test.go", qualifiedAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	packagesCommand(t, dest, tool, true, "test", "-race", "-count=1", "-v", "./...")
	binary := filepath.Join(t.TempDir(), "application")
	packagesCommand(t, dest, tool, true, "build", "-buildvcs=false", "-o", binary, ".")
	output := packagesCommand(t, dest, binary, true)
	for _, want := range []string{`"msg":"qualified flow"`, `"mode":"4"`, `"value":8`, `other`} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %s: %s", want, output)
		}
	}
}

func TestQualifiedPackagesRejectWrongIdentity(t *testing.T) {
	source, _ := qualifiedSource(t)
	packagesWrite(t, source, "bad.gox", `package main
use source as a;
use other as b;
func wrong(value b::Mode) string {
 return match value {a::Mode::Idle => "idle",a::Mode::Ready(_) => "ready",}
}
`)
	_, err := compileQualifiedModule(t, source, filepath.Join(t.TempDir(), "generated"))
	if err == nil {
		t.Fatal("matched different packages' Mode values by name")
	}
	if !strings.Contains(err.Error(), "bad.gox") || !strings.Contains(err.Error(), "generated/unmapped") {
		t.Fatalf("lost consumer file: %v", err)
	}
	t.Log(err)
}

func TestQualifiedPackagesRejectUnregisteredSum(t *testing.T) {
	source, _ := qualifiedSource(t)
	packagesWrite(t, source, "fake/fake.go", `package fake
type Result[T,E any] interface{}
func Fetch() Result[int,error] {return nil}
`)
	packagesWrite(t, source, "bad.gox", `package main
mod fake;
func wrong() Result[int,AppError] {
 n:=fake::Fetch()?
 return Result[int,AppError]::Ok(n)
}

`)
	_, err := compileQualifiedModule(t, source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "builtin nominal") {
		t.Fatalf("unregistered lookalike accepted or wrong failure: %v", err)
	}
}

func TestQualifiedPackagesGenerateConcreteValidation(t *testing.T) {
	source, _ := qualifiedSource(t)
	packagesWrite(t, source, "concrete.gox", `package main
use source as p;
func absent(kind string) p::Result[string,error] {
 n:=p::Fetch(kind)?
 _=n
 return p::GoxideNewResultOk[string,error]("ok")
}
func concrete(value p::Result[string,error], seen *int) string {
 out:=match value {p::Result[string,error]::Ok(text)=>text,p::Result[string,error]::Err(_)=>"err"}
 *seen+=1
 return out
}
`)
	dest := filepath.Join(t.TempDir(), "generated")
	if _, err := compileQualifiedModule(t, source, dest); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, dest, "concrete_test.go", `package main
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";p "qualified.example/app/source")
func TestLocalConcreteValidation(t *testing.T){
 if __borrow_std.ValueOk(absent("ok"))!="ok"{t.Fatal("concrete success")}
 if __borrow_std.ValueErr(absent("error"))!=p.Sentinel{t.Fatal("failure propagation")}
 seen:=0
 if concrete(p.GoxideNewResultOk[string,error]("valid"),&seen)!="valid"||seen!=1{t.Fatal("valid match")}
 for _,value:=range []p.Result[string,error]{{}}{
  func(){seen:=0;defer func(){if recover()!="goxide: invalid enum Result"{t.Error("invalid case accepted or wrong panic")};if seen!=0{t.Error("effect before validation")}}();concrete(value,&seen)}()
 }
}
`)
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	packagesCommand(t, dest, tool, true, "test", "-race", "-count=1", "./...")
	packagesCommand(t, dest, tool, true, "vet", "./...")
}

func TestQualifiedPackagesRejectDuplicateFrom(t *testing.T) {
	source, _ := qualifiedSource(t)
	packagesWrite(t, source, "bad.gox", `package main
#[derive(Error)]
pub enum Duplicate {
 #[error("first")]
 #[from]
 First(error),
 #[error("second")]
 #[from]
 Second(error),
}
`)
	_, err := compileQualifiedModule(t, source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "from") {
		t.Fatalf("duplicate From accepted or wrong failure: %v", err)
	}
}

const qualifiedAssertions = `package main
import (
 "errors"
 "testing"
 source "qualified.example/app/source"
 other "qualified.example/app/other"
)
func TestQualifiedIdentityAndAliases(t *testing.T) {
 if describe(source.NewModeReady(5))!="5" || describe(source.NewModeIdle())!="idle" {t.Fatal("source match")}
 if otherDescription(other.NewModeReady("five"))!="five" {t.Fatal("other match")}
 if n,e:=source.Read(otherFileAlias());n!=10||e!=nil{t.Fatal(n,e)}
 if n,e:=source.Read(producerTypeAlias());n!=9||e!=nil{t.Fatal(n,e)}
 if n,e:=source.Read(importedTarget("ok"));n!=9||e!=nil{t.Fatal(n,e)}
 if n,e:=unpack(shadow("ok"));n!=7||e!=nil{t.Fatal(n,e)}
 if n,ok:=source.ReadOption(optional(true));n!=5||!ok{t.Fatal(n,ok)}
 if _,ok:=source.ReadOption(optional(false));ok{t.Fatal("None changed")}
}
func TestImportedErrorAndTypedNil(t *testing.T) {
 if n,e:=unpack(wrapped("ok"));n!=8||e!=nil{t.Fatal(n,e)}
 _,e:=unpack(wrapped("error"));if !errors.Is(e,source.Sentinel){t.Fatal(e)}
 _,e=source.Read(importedTarget("error"));if e!=source.Sentinel{t.Fatal("assignable error changed",e)}
 _,e=unpack(wrapped("typed"));var fault *source.Fault
 if e==nil || errors.Unwrap(e)==nil || !errors.As(e,&fault) || fault!=nil {t.Fatalf("typednil wrapper lost: %#v",e)}
 _,e=source.Read(importedTarget("typed"));if e==nil || !errors.As(e,&fault) || fault!=nil{t.Fatalf("typednil passthrough lost: %#v",e)}
}
func TestImportedNestedValidationBeforeEffect(t *testing.T) {
 continuation=0
 defer func(){if recover()==nil{t.Fatal("invalid nested payload accepted")};if continuation!=0{t.Fatal("effect before validation")}}()
 nestedInvalid()
}
func TestImportedInvalidSumBeforeEffect(t *testing.T) {
 for _,v:=range []source.Result[int,error]{{}} {
  func(){ continuation=0;defer func(){if recover()==nil{t.Fatal("invalid sum accepted")};if continuation!=0{t.Fatal("effect before sum validation")}}();importedInput(v) }()
 }
}
func TestImportedInvalidBeforeMatch(t *testing.T) {
 defer func(){if recover()==nil{t.Fatal("zero imported enum accepted")}}()
 describe(source.Mode{})
}
`
