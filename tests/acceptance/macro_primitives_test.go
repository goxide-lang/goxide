package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/rootconfig"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestMacroPrimitivePackageCollisionsAreLocated(t *testing.T) {
	for _, tc := range []struct{ name, decl, body string }{
		{"constant", "const true = 1 == 2", "true"},
		{"type", "type int string", "int(0)"},
		{"sum", "type int string", "Option[int]::Some(0)"},
	} {
		for _, definition := range []bool{false, true} {
			site := "caller"
			if definition {
				site = "definition"
			}
			t.Run(tc.name+"/"+site, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", "module primitive.example/app\ngo 1.27\n")
				writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
					"ops": {Module: "primitive.example/app", Package: "primitive.example/app/ops"},
				})
				packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! value {()=>{"+tc.body+"};}\n")
				body := "func Run(){_=m::value!()}\n"
				accepted := tc.name == "constant" && !definition
				if accepted {
					body = "func Run()bool{return m::value!()}\nfunc Caller()bool{return true}\n"
				}
				packagesWrite(t, source, "app.gox", "package app\nuse ops as m;\n"+body)
				shadow, want, packageName := "shadow.go", "caller package declaration", "app"
				if definition {
					shadow, want, packageName = "ops/shadow.go", "definition-site declaration", "ops"
				}
				packagesWrite(t, source, shadow, "package "+packageName+"\n"+tc.decl+"\n")
				generated := filepath.Join(t.TempDir(), "generated")
				err := testdeps.Module(source, generated, nil)
				if !accepted {
					if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "macros.gox:2:") || !strings.Contains(err.Error(), "app.gox:3:") {
						t.Fatalf("%s definition=%v: %v", tc.name, definition, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				again := filepath.Join(t.TempDir(), "generated")
				if err := testdeps.Module(source, again, nil); err != nil {
					t.Fatal(err)
				}
				singleExternalSameTree(t, generated, again)
				packagesWrite(t, generated, "cmd/check/main.go", `package main
import("fmt";"primitive.example/app")
func main(){fmt.Println(app.Run(),app.Caller())}
`)
				tool := filepath.Join(runtime.GOROOT(), "bin/go")
				packagesCommand(t, generated, tool, true, "build", "./...")
				if output := packagesCommand(t, generated, tool, true, "run", "./cmd/check"); output != "true false\n" {
					t.Fatalf("macro definition and caller constant bindings changed: %q", output)
				}
			})
		}
	}
}

func TestMacroPrimitiveLocalShadowsAndExplicitCapturesRace(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module primitive.example/app\ngo 1.27\n")
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"ops": {Module: "primitive.example/app", Package: "primitive.example/app/ops"},
	})
	packagesWrite(t, source, "ops/macros.gox", `package ops
pub macro_rules! zero {()=>{int(0)};}
pub macro_rules! enabled {()=>{true};}
pub macro_rules! convert {($t:ident,$v:expr)=>{$t($v)};}
`)
	packagesWrite(t, source, "app.gox", `package app
use ops as m;
use reflect;
func Run()(int,bool,string,string){
 type int string
 true:=false;_=true
 value:=m::convert!(int,"kept")
 return m::zero!(),m::enabled!(),string(value),reflect::TypeOf(value).Name()
}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	if err := testdeps.Module(source, generated, nil); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, generated, "app_test.go", `package app
import "testing"
func TestBindings(t *testing.T){n,enabled,value,name:=Run();if n!=0||!enabled||value!="kept"||name!="int"{t.Fatal(n,enabled,value,name)}}
`)
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "./...")
	packagesCommand(t, generated, tool, true, "vet", "./...")
}
