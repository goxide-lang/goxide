package frontend

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveContainersKeepSourceTypes(t *testing.T) {
	for _, kinds := range []string{"Clone", "Debug", "Clone,Debug", "PartialEq,Eq"} {
		t.Run(kinds, func(t *testing.T) {
			source := []byte("package p\n#[derive(" + kinds + ")]\ntype S struct{A []int;B []int;C [][]int;D map[string][2][]int}\n")
			result, err := Compile("containers.gox", source)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(token.NewFileSet(), "containers.go", result.Go, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if spec, ok := n.(*ast.TypeSpec); ok && spec.Assign.IsValid() {
					t.Errorf("unnecessary container alias %s", spec.Name)
				}
				return true
			})
			if strings.Contains(kinds, "Clone") && !bytes.Contains(result.Go, []byte("make([]int, len(")) {
				t.Fatal("lost original slice expression", string(result.Go))
			}
			again, err := Compile("containers.gox", source)
			if err != nil || !bytes.Equal(result.Go, again.Go) || !bytes.Equal(result.SourceMap, again.SourceMap) {
				t.Fatal("nondeterministic derive output", err)
			}
		})
	}
}

func TestDeriveContainerBindingsAndSemantics(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	files := map[string]string{
		"go.mod": "module example.org/derivebindings\ngo 1.27\n",
		"model/model.gox": `package model
#[derive(Clone,Debug)]
type Item struct{Values []int}
func (v Item) First()int{return v.Values[0]}
`,
		"other/model.gox": `package other
#[derive(Clone,Debug)]
type Item struct{Text string}
`,
		"client/other.gox": `package client
import src "example.org/derivebindings/other"
#[derive(Clone,Debug)]
type Other struct{Items []src.Item}
`,
		"local/local_test.go": `package local
import("testing";"reflect";"strings")
func TestBehavior(t *testing.T){
 original:=Local{F0:[][]v{{{N:7}}},F1:[][]out{{{N:8}}},F2:[][]src{{{N:9}}},F3:[][]src1{{{N:10}}},Items:map[string][]item{"z":{{N:11}}}}
 ordinary:=original;ordinary.F0[0][0].N=17
 if original.F0[0][0].N!=17{t.Fatal("ordinary assignment must retain shared containers")}
 copied:=CloneLocal(&original);copied.F0[0][0].N=99;copied.Items["z"][0].N=99
 if original.F0[0][0].N!=17||original.Items["z"][0].N!=11{t.Fatal("Clone shares source containers")}
 if reflect.TypeOf(copied.F0[0][0])!=reflect.TypeOf(v{})||copied.F0[0][0].Tag()!=99{t.Fatal("named type identity or methods changed")}
 if !strings.Contains(DebugLocal(&original),"src { N: 9 }"){t.Fatal(DebugLocal(&original))}
 nilCopy:=CloneLocal(&Local{});emptyCopy:=CloneLocal(&Local{F0:[][]v{},Items:map[string][]item{}})
 if nilCopy.F0!=nil||nilCopy.Items!=nil||emptyCopy.F0==nil||emptyCopy.Items==nil{t.Fatal("nil/empty distinction")}
 e:=MakeChoice();c:=Clonechoice(&e)
 if Debugchoice(&c)!="choice::Items([x { N: 4 }])"{t.Fatal(Debugchoice(&c))}
}
`,
		"client/client_test.go": `package client
import("testing";"strings";m "example.org/derivebindings/model";o "example.org/derivebindings/other")
func TestBehavior(t *testing.T){
 shared:=m.Item{Values:[]int{3}};imported:=ImportSrc{Items:[]map[string][2]m.Item{{"a":{shared,shared}}}}
 cloned:=CloneImportSrc(&imported);cloned.Items[0]["a"][0].Values[0]=42
 if imported.Items[0]["a"][0].First()!=3||cloned.Items[0]["a"][1].First()!=3{t.Fatal("deep Clone or independent element clones changed")}
 if !strings.Contains(DebugImportSrc(&imported),"Item { Values: [3] }"){t.Fatal(DebugImportSrc(&imported))}
 other:=Other{Items:[]o.Item{{Text:"other"}}};if DebugOther(&other)!="Other { Items: [Item { Text: \"other\" }] }"{t.Fatal("file import binding changed")}
}
`,
	}
	// Include every generated local spelling and a suffix candidate. Reserving
	// all type expressions also protects later fields and nested closures.
	names := []string{"v", "out", "src", "src1", "dst", "i", "key", "item", "parts", "keys", "x"}
	var local strings.Builder
	local.WriteString("package local\n")
	for _, name := range names {
		fmt.Fprintf(&local, "#[derive(Clone,Debug)]\ntype %s struct{N int}\nfunc (value %s)Tag()int{return value.N}\n", name, name)
	}
	local.WriteString("#[derive(Clone,Debug)]\ntype Local struct{\n")
	for i, name := range names {
		fmt.Fprintf(&local, "F%d [][]%s\n", i, name)
		files["client/import_"+name+".gox"] = fmt.Sprintf("package client\nimport %s %q\n#[derive(Clone,Debug)]\ntype Import%s struct{Items []map[string][2]%s.Item}\n", name, "example.org/derivebindings/model", strings.ToUpper(name[:1])+name[1:], name)
	}
	local.WriteString("Items map[string][]item\n}\n#[derive(Clone,Debug)]\nenum choice{Items([]x),Empty,}\nfunc MakeChoice()choice{return choice::Items([]x{{N:4}})}\n")
	files["local/local.gox"] = local.String()
	useWrite(t, src, files)
	if _, err := CompileModule(src, dst); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(dst, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && bytes.Contains(data, []byte("__goxide_enum_derive_type_")) {
			t.Errorf("redundant container alias in %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	useGo(t, dst, "test", "-race", "-count=1", "./...")
	useGo(t, dst, "vet", "./...")
}
