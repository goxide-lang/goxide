package acceptance_test

import (
	"fmt"
	"github.com/hxsf/goxide/internal/testdeps"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
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
	packagesWrite(t, source, "go.mod", fmt.Sprintf("module qualified.example/app\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => %q\n", filepath.ToSlash(root)))
	return source, root
}

func TestQualifiedPackagesApplicationRace(t *testing.T) {
	source, _ := qualifiedSource(t)
	dest := filepath.Join(t.TempDir(), "generated")
	if _, err := frontend.CompileModule(source, dest); err != nil {
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
import a "qualified.example/app/source"
import b "qualified.example/app/other"
func wrong(value b.Mode) string {
 return match value {a.Mode::Idle => "idle",a.Mode::Ready(_) => "ready",}
}
`)
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
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
import fake "qualified.example/app/fake"
func wrong() Result[int,AppError] {
 n:=fake.Fetch()?
 return Result[int,AppError]::Ok(n)
}

`)
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "builtin nominal") {
		t.Fatalf("unregistered lookalike accepted or wrong failure: %v", err)
	}
}

func TestQualifiedPackagesRequireConcreteValidation(t *testing.T) {
	source, _ := qualifiedSource(t)
	packagesWrite(t, source, "bad.gox", `package main
import p "qualified.example/app/source"
func absent() p.Result[string,error] {
 n:=p.Fetch("ok")?
 _=n
 return p.GoxideNewResultOk[string,error]("ok")
}
`)
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "producer validation metadata") {
		t.Fatalf("missing concrete validator accepted or wrong failure: %v", err)
	}
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
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
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
type forgedResult struct { source.Result[int,error] }
func TestImportedInvalidSumBeforeEffect(t *testing.T) {
 var typed *source.GoxideResultOk[int,error]
 for _,v:=range []source.Result[int,error]{nil,typed,&source.GoxideResultOk[int,error]{Value:2},forgedResult{}} {
  func(){ continuation=0;defer func(){if recover()==nil{t.Fatal("invalid sum accepted")};if continuation!=0{t.Fatal("effect before sum validation")}}();importedInput(v) }()
 }
}
func TestImportedInvalidBeforeMatch(t *testing.T) {
 defer func(){if recover()==nil{t.Fatal("nil imported enum accepted")}}()
 describe(nil)
}
`
