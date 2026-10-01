package deriveextras

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func spec(t *testing.T, src string) *ast.TypeSpec {
	t.Helper()
	f, e := parser.ParseFile(token.NewFileSet(), "input.go", "package fixture\n"+src, 0)
	if e != nil {
		t.Fatal(e)
	}
	return f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
}

func TestGeneratedDerivesRun(t *testing.T) {
	decls := []string{
		"type Child struct { Name string; Count int }",
		"type Parent struct { Child Child; Children []Child; Index map[string][]Child; Fixed [2]int; Empty []int; Nil []int }",
	}
	imports := map[string]string{}
	var code strings.Builder
	for _, decl := range decls {
		code.WriteString(decl + "\n")
		for _, kind := range []string{"Clone", "Debug"} {
			out, im, e := Generate(kind, spec(t, decl))
			if e != nil {
				t.Fatal(e)
			}
			code.WriteString(out)
			for k, v := range im {
				imports[k] = v
			}
		}
	}
	errDecl := "type Wrapped struct { Cause error }"
	out, im, e := GenerateError(spec(t, errDecl), "read failed", "Cause", "Cause")
	if e != nil {
		t.Fatal(e)
	}
	code.WriteString(errDecl + "\n" + out)
	for k, v := range im {
		imports[k] = v
	}
	errDecl = "type Details struct { Op string; Code uint64 }"
	out, im, e = GenerateError(spec(t, errDecl), "{Op}: {{code}} {Code}", "", "")
	if e != nil {
		t.Fatal(e)
	}
	code.WriteString(errDecl + "\n" + out)
	for k, v := range im {
		imports[k] = v
	}
	var source strings.Builder
	source.WriteString("package fixture\nimport (\n")
	aliases := []string{}
	for k := range imports {
		aliases = append(aliases, k)
	}
	sort.Strings(aliases)
	for _, k := range aliases {
		source.WriteString(k + " " + strconv.Quote(imports[k]) + "\n")
	}
	source.WriteString(")\n" + code.String())
	testSource := `package fixture
import("testing";"errors";"strings")
func TestBehavior(t *testing.T){
 v:=Parent{Child:Child{"root",1},Children:[]Child{{"child",2}},Index:map[string][]Child{"z":{{"last",3}},"a":{{"first",4}}},Fixed:[2]int{7,8},Empty:[]int{}}
 c:=CloneParent(&v)
 c.Children[0].Name="changed";c.Index["a"][0].Count=99;c.Fixed[0]=9
 if v.Children[0].Name!="child"||v.Index["a"][0].Count!=4||v.Fixed[0]!=7{t.Fatal("clone aliases source containers")}
 if c.Nil!=nil||c.Empty==nil{t.Fatal("nil versus empty lost")}
 text:=DebugParent(&v)
 if !strings.Contains(text,"Child: Child { Name: \"root\", Count: 1 }"){t.Fatal(text)}
 if strings.Index(text,"\"a\":")>strings.Index(text,"\"z\":"){t.Fatal("map debug unstable",text)}
 cause:=errors.New("root cause");wrapped:=FromWrapped(cause)
 if !errors.Is(wrapped,cause)||wrapped.Error()!="read failed"{t.Fatal(wrapped)}
 if got:=(Details{"load",18446744073709551615}).Error();got!="load: {code} 18446744073709551615"{t.Fatal(got)}
}
`
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module fixture\ngo 1.27\n", "generated.go": source.String(), "generated_test.go": testSource} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generated code: %v\n%s\n%s", e, output, source.String())
	}
}

func TestRejectImplicitProtocols(t *testing.T) {
	for _, typ := range []string{"*int", "sync.Mutex", "interface{}", "chan int", "func()"} {
		for _, kind := range []string{"Clone", "Debug"} {
			if _, _, e := Generate(kind, spec(t, "type Bad struct{ X "+typ+" }")); e == nil {
				t.Errorf("accepted %s %s", kind, typ)
			}
		}
	}
	if _, _, e := Generate("Debug", spec(t, "type Bad struct{ X map[int]string }")); e == nil {
		t.Fatal("accepted unsupported Debug key")
	}
	if _, _, e := Generate("Clone", spec(t, "type Bad[T any] struct{ X T }")); e == nil {
		t.Fatal("accepted generic derive")
	}
	if _, _, e := Generate("Clone", spec(t, "type Bad struct{ Embedded }")); e == nil {
		t.Fatal("accepted embedded field")
	}
	for _, tc := range []struct{ decl, message, source, from string }{
		{"type Bad struct{ Cause string }", "bad", "Cause", ""},
		{"type Bad struct{ Cause error; Info string }", "bad", "Cause", "Cause"},
		{"type Bad struct{ X int }", "{missing}", "", ""},
		{"type Bad struct{ X int }", "{X", "", ""},
		{"type Bad struct{ X int }", "X}", "", ""},
		{"type Bad struct{ X error }", "{X}", "", ""},
	} {
		if _, _, e := GenerateError(spec(t, tc.decl), tc.message, tc.source, tc.from); e == nil {
			t.Errorf("accepted invalid error derive %+v", tc)
		}
	}
}
