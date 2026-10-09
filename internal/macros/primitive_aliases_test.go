package macros

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

func primitiveTypeAliases(t *testing.T, source []byte) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "expanded.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok && spec.Assign.IsValid() {
			if id, ok := spec.Type.(*ast.Ident); ok && primitiveName(id.Name) {
				aliases[spec.Name.Name] = id.Name
			}
		}
		return true
	})
	return aliases
}

func TestPrimitiveAliasesOnlyWhereCallerShadows(t *testing.T) {
	source := `package main
import "fmt"
import "reflect"
macro_rules! twice {($value:expr)=>{func() int {value:=$value;return value+value}()};}
macro_rules! values {($($value:expr),*)=>{[]int{$($value),*}};}
macro_rules! zero {()=>{int(0)};}
macro_rules! convert {($t:ident,$value:expr)=>{$t($value)};}
func main(){
 fmt.Println(twice!(21),values!(1,2,3))
 { type int string; value:=convert!(int,"kept"); fmt.Printf("%T %s %s\n",zero!(),value,reflect.TypeOf(value).Name()) }
 { type int int64; value:=convert!(int,7); fmt.Printf("%T %T %s\n",zero!(),value,reflect.TypeOf(value).Name()) }
 fmt.Println(zero!())
}
`
	first, err := ExpandWithMap("mixed.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if aliases := primitiveTypeAliases(t, first.Source); len(aliases) != 1 {
		t.Fatalf("expected one shared int alias for the two shadowed uses: %v\n%s", aliases, first.Source)
	}
	if !strings.Contains(string(first.Source), "func() int") || !strings.Contains(string(first.Source), "[]int") || !strings.Contains(string(first.Source), "fmt.Println(int(0))") {
		t.Fatalf("unshadowed uses retained aliases: %s", first.Source)
	}
	second, err := ExpandWithMap("mixed.gox", []byte(source))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("expansion changed across repeated runs: %v", err)
	}
	run(t, source, "42 [1 2 3]\nint kept int\nint main.int int\n0\n")
}

func TestPrimitiveAliasesRespectLexicalScope(t *testing.T) {
	for _, tc := range []struct {
		name, functions string
		aliases         int
		want            string
	}{
		{"unrelated", `func main(){ {type int string; _=int("x")}; fmt.Printf("%T\n",zero!()) }`, 0, "int\n"},
		{"later", `func main(){fmt.Printf("%T\n",zero!()); int:=func(int)string{return "local"};fmt.Println(int(1))}`, 0, "int\nlocal\n"},
		{"initializer", `func main(){int:=zero!();fmt.Printf("%T\n",int)}`, 0, "int\n"},
		{"type-before-after", `func main(){fmt.Printf("%T ",zero!());type int string;fmt.Printf("%T %s\n",zero!(),int("local"))}`, 1, "int int local\n"},
		{"type-parameter", `func show[int ~string](value int){fmt.Printf("%T %s\n",zero!(),value)};func main(){show("generic")}`, 1, "int generic\n"},
		{"parameter", `func show(int string){fmt.Printf("%T %s\n",zero!(),int)};func main(){show("parameter")}`, 1, "int parameter\n"},
		{"result", `func show()(int string){fmt.Printf("%T\n",zero!());return "result"};func main(){fmt.Println(show())}`, 1, "int\nresult\n"},
		{"closure", `func main(){int:="outer";func(){fmt.Printf("%T %s\n",zero!(),int)}()}`, 1, "int outer\n"},
		{"range", `func main(){for int:=range []string{"range"}{fmt.Printf("%T %v\n",zero!(),int)}}`, 1, "int 0\n"},
		{"type-switch", `func main(){var value any="switch";switch int:=value.(type){case string:fmt.Printf("%T %s\n",zero!(),int)}}`, 1, "int switch\n"},
		{"if-initializer", `func main(){if int:="if";int!=""{fmt.Printf("%T %s\n",zero!(),int)}}`, 1, "int if\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package main\nimport \"fmt\"\nmacro_rules! zero {()=>{int(0)};}\n" + tc.functions + "\n"
			expanded, err := Expand("scope.gox", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(primitiveTypeAliases(t, expanded)); got != tc.aliases {
				t.Fatalf("got %d aliases, want %d\n%s", got, tc.aliases, expanded)
			}
			run(t, source, tc.want)
		})
	}
}

func TestPrimitiveAliasesKeepUnresolvedBindings(t *testing.T) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "unresolved.go", "package p\ntype generated = int\nfunc f(){type int Missing;_=generated(0)}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{"generated": "int"}
	ResolvePrimitiveAliases("example/p", fs, f, aliases, nil)
	if aliases["generated"] != "int" {
		t.Fatal("an unresolved local type binding lost its definition-site alias")
	}
	call := f.Decls[1].(*ast.FuncDecl).Body.List[1].(*ast.AssignStmt).Rhs[0].(*ast.CallExpr)
	if call.Fun.(*ast.Ident).Name != "generated" {
		t.Fatal("an unresolved local type changed the primitive conversion")
	}
}

func TestPrimitiveAliasesPreserveTemplateOrigins(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("package p\nmacro_rules! zero {()=>{int(0)};}\nfunc f(){_=zero!()}\n", "\n", newline)
		expanded, err := ExpandWithMap("origin.gox", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		if len(primitiveTypeAliases(t, expanded.Source)) != 0 {
			t.Fatalf("unshadowed primitive kept alias: %s", expanded.Source)
		}
		found := false
		for _, origin := range expanded.Origins {
			if string(expanded.Source[origin.OutputStart:origin.OutputEnd]) != "int" {
				continue
			}
			if source[origin.SourceStart:origin.SourceEnd] != "int" || len(origin.Stack) != 1 || origin.Stack[0].Macro != "zero" {
				t.Fatalf("primitive lost definition token or expansion stack: %+v", origin)
			}
			found = true
		}
		if !found {
			t.Fatal("missing primitive token origin")
		}
	}
}

func TestPrimitiveAliasesAcrossDefinitionFilesAndPackages(t *testing.T) {
	for _, external := range []bool{false, true} {
		macroImport, call := "", "zero!()"
		sources := map[string][]byte{
			"runtime.go": []byte("package main\nfunc Caller() int { return 7 }\n"),
		}
		var deps map[string]*Package
		if external {
			macroImport, call = "import ops \"example/ops\"\n", "ops::zero!()"
			deps = map[string]*Package{"example/ops": registry(t, "example/ops", map[string][]byte{
				"definition.gox": []byte("package ops\npub macro_rules! zero {()=>{int(0)};}\n"),
			}, nil)}
		} else {
			sources["definition.gox"] = []byte("package main\nmacro_rules! zero {()=>{int(0)};}\n")
		}
		source := "package main\nimport \"fmt\"\n" + macroImport +
			"func Plain(){fmt.Printf(\"%T %d\\n\"," + call + ",Caller())}\n" +
			"func main(){Plain();type int string;fmt.Printf(\"%T %s\\n\"," + call + ",int(\"caller\"))}\n"
		sources["caller.gox"] = []byte(source)
		p := registry(t, "example/caller", sources, deps)
		expanded, err := ExpandPackage("caller.gox", sources["caller.gox"], nil, p, nil, ExpansionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(primitiveTypeAliases(t, expanded.Source)) != 1 || !strings.Contains(string(expanded.Source), ",int(0),Caller()") {
			t.Fatalf("external=%v: caller scopes or unresolved companion helper prevented precise simplification:\n%s", external, expanded.Source)
		}
		if !external {
			run(t, string(expanded.Source)+"\nfunc Caller() int {return 7}\n", "int 7\nint caller\n")
		}
	}
}

func TestPrimitiveAliasesStaticPathOrigins(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("package p\nimport \"fmt\"\nmacro_rules! zero {()=>{int(0)};}\nfunc f(){fmt::Println(zero!())}\n", "\n", newline)
		expanded, err := ExpandWithMap("static-origin.gox", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		if len(expanded.PrimitiveAliases) != 0 {
			t.Fatalf("static parser view prevented primitive proof: %s", expanded.Source)
		}
		found := false
		for _, origin := range expanded.Origins {
			if string(expanded.Source[origin.OutputStart:origin.OutputEnd]) != "int" {
				continue
			}
			if source[origin.SourceStart:origin.SourceEnd] != "int" || len(origin.Stack) != 1 || origin.Stack[0].Macro != "zero" {
				t.Fatalf("lost copied primitive token: %+v", origin)
			}
			found = true
		}
		if !found {
			t.Fatal("missing proven primitive origin")
		}
	}
}
