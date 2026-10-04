package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestMacroPrimitivePackageCollisionsAreLocated(t *testing.T) {
	for _, tc := range []struct{ name, decl, body string }{
		{"constant", "const true = 1 == 2", "true"},
		{"type", "type int string", "int(0)"},
		{"sum", "type int string", "Option[int]::Some(0)"},
	} {
		for _, definition := range []bool{false, true} {
			source := t.TempDir()
			packagesWrite(t, source, "go.mod", "module primitive.example/app\ngo 1.27\n")
			packagesWrite(t, source, "ops/macros.hgo", "package ops\npub macro_rules! value {()=>{"+tc.body+"};}\n")
			packagesWrite(t, source, "app.hgo", "package app\nimport m \"primitive.example/app/ops\"\nfunc Run(){_=m.value!()}\n")
			shadow, want := "shadow.go", "caller package declaration"
			packageName := "app"
			if definition {
				shadow = "ops/shadow.go"
				packageName = "ops"
				want = "definition-site declaration"
			}
			packagesWrite(t, source, shadow, "package "+packageName+"\n"+tc.decl+"\n")
			_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
			if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "macros.hgo:2:") || !strings.Contains(err.Error(), "app.hgo:3:") {
				t.Fatalf("%s definition=%v: %v", tc.name, definition, err)
			}
		}
	}
}

func TestMacroPrimitiveLocalShadowsAndExplicitCapturesRace(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module primitive.example/app\ngo 1.27\n")
	packagesWrite(t, source, "ops/macros.hgo", `package ops
pub macro_rules! zero {()=>{int(0)};}
pub macro_rules! enabled {()=>{true};}
pub macro_rules! convert {($t:ident,$v:expr)=>{$t($v)};}
`)
	packagesWrite(t, source, "app.hgo", `package app
import m "primitive.example/app/ops"
import "reflect"
func Run()(int,bool,string,string){
 type int string
 true:=false;_=true
 value:=m.convert!(int,"kept")
 return m.zero!(),m.enabled!(),string(value),reflect.TypeOf(value).Name()
}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	if _, err := frontend.CompileModule(source, generated); err != nil {
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
