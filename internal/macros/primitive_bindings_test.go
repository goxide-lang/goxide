package macros

import (
	"go/types"
	"strings"
	"testing"
)

func TestPrimitivePackageBindingsRejectRebinding(t *testing.T) {
	for _, tc := range []struct{ name, declaration, body string }{
		{"true", "const true = 1 == 2", "true"},
		{"false", "const false = 1 == 1", "false"},
		{"int", "type int string", "int(0)"},
		{"string", "type string []byte", "string(65)"},
		{"error", "type error interface{}", "func() error { return nil }()"},
		{"any", "type any string", "any(0)"},
		{"sum-int", "type int string", "Option[int]::Some(0)"},
		{"sum-error", "type error interface{}", "Result[int,error]::Ok(0)"},
		{"sum-nested", "type byte uint32", "Option[map[string][]byte]::None"},
	} {
		// nil is deliberately not supported as a free template name; use a capture.
		tc.body = strings.ReplaceAll(tc.body, "nil", "$v")
		for _, atDefinition := range []bool{false, true} {
			where := "caller"
			if atDefinition {
				where = "definition"
			}
			t.Run(tc.name+"/"+where, func(t *testing.T) {
				defs := map[string][]byte{"definitions.gox": []byte("package ops\npub macro_rules! value { ($v:expr) => {" + tc.body + "}; }\n")}
				caller := map[string][]byte{"caller.gox": []byte("package caller\nimport ops \"example/ops\"\nfunc Run() { _ = ops.value!(nil) }\n")}
				want := "caller package declaration"
				if atDefinition {
					defs["shadow.go"] = []byte("package ops\n" + tc.declaration + "\n")
					want = "definition-site declaration"
				} else {
					caller["shadow.go"] = []byte("package caller\n" + tc.declaration + "\n")
				}
				dep := registry(t, "example/ops", defs, nil)
				p := registry(t, "example/caller", caller, map[string]*Package{"example/ops": dep})
				result, err := ExpandPackage("caller.gox", caller["caller.gox"], nil, p, nil)
				if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "definitions.gox:2:") || !strings.Contains(err.Error(), "caller.gox:3:") {
					t.Fatalf("expected located %s, got %v", want, err)
				}
				if len(result.Source) != 0 {
					t.Fatal("rejected expansion returned output")
				}
			})
		}
	}
}

type primitiveNameImporter struct{}

func (primitiveNameImporter) Import(path string) (*types.Package, error) {
	return types.NewPackage(path, "int"), nil
}

func TestPrimitiveImportsAreFileLocalAndResolveActualPackageNames(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		for _, definition := range []bool{false, true} {
			im := `import int "example/odd"`
			if implicit {
				im = `import "example/odd"`
			}
			defs := map[string][]byte{"definitions.gox": []byte("package ops\npub macro_rules! zero {()=>{int(0)};}\n")}
			caller := map[string][]byte{"caller.gox": []byte("package caller\nimport ops \"example/ops\"\nfunc Run(){_=ops.zero!()}\n")}
			want := "caller file import"
			if definition {
				defs["definitions.gox"] = []byte(strings.Replace(string(defs["definitions.gox"]), "package ops\n", "package ops\n"+im+"\n", 1))
				want = "definition-site import"
			} else {
				caller["caller.gox"] = []byte(strings.Replace(string(caller["caller.gox"]), "package caller\n", "package caller\n"+im+"\n", 1))
			}
			dep := registry(t, "example/ops", defs, nil)
			p := registry(t, "example/caller", caller, map[string]*Package{"example/ops": dep})
			_, err := ExpandPackage("caller.gox", caller["caller.gox"], nil, p, primitiveNameImporter{})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("implicit=%v definition=%v: %v", implicit, definition, err)
			}
		}
	}
	// An import in a different source file cannot shadow the generated alias.
	dep := registry(t, "example/ops", map[string][]byte{"definitions.gox": []byte("package ops\npub macro_rules! zero {()=>{int(0)};}\n")}, nil)
	src := []byte("package caller\nimport ops \"example/ops\"\nfunc Run(){_=ops.zero!()}\n")
	p := registry(t, "example/caller", map[string][]byte{"caller.gox": src, "other.go": []byte("package caller\nimport int \"example/odd\"\n")}, map[string]*Package{"example/ops": dep})
	if _, err := ExpandPackage("caller.gox", src, nil, p, primitiveNameImporter{}); err != nil {
		t.Fatal(err)
	}
}

func TestPrimitiveLocalsCapturesAndDefinitionFunctionsRemainSupported(t *testing.T) {
	run(t, `package main
import "fmt"
import "reflect"
macro_rules! zero {()=>{int(0)};}
macro_rules! enabled {()=>{true};}
macro_rules! convert {($t:ident,$v:expr)=>{$t($v)};}
func main(){
 type int string
 true:=false
 value:=convert!(int,"kept")
 fmt.Printf("%T %v %v %s %s\n",zero!(),zero!(),enabled!(),value,reflect.TypeOf(value).Name())
 _=true
}
`, "int 0 true kept int\n")
	run(t, `package main
import "fmt"
func int(x string) string { return "helper:"+x }
macro_rules! call {()=>{int("ok")};}
func main(){fmt.Println(call!())}
`, "helper:ok\n")
}
