package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// These real CLI errors distinguish a generated payload's cause from a precise
// source token. Duplicate spellings, offsets and basenames cannot lend evidence.
func TestMacroASTProvenanceCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()
	packagesWrite(t, outside, "go.mod", "module ast.example/outside\ngo 1.27\n")
	for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, tc := range []struct {
			name, mode, body, want, precise string
			nested, sibling, frames         bool
		}{
			{"single", "single", "len($x?)", "for built-in len", "", false, false, true},
			{"nested-enum-multiple-questions", "single", "len($x?)", "for built-in len", "", true, false, true},
			{"sibling", "single", "len($x?)", "for built-in len", "", true, true, true},
			{"multifile", "multifile", "len($x?)", "for built-in len", "", true, true, true},
			{"cross-package-same-basename", "cross", "len($x?)", "for built-in len", "", true, true, true},
			{"original-selector", "single", "($x?).Missing", "has no field or method Missing", "Missing", true, false, true},
			{"original-selector-after-rename", "single", "($x?).Missing", "has no field or method Missing", "Missing", true, false, true},
			{"cross-package-original-selector", "cross", "($x?).Missing", "has no field or method Missing", "Missing", true, false, true},
			// Index lowering creates a later freezeValue temp without a single cause.
			// The known macro around it must not fill this deliberately unknown point.
			{"unknown-later-temp", "single", "($x?)[0]", "cannot index", "", true, false, false},
			{"nonmacro-near-known-macro", "native", "len($x?)", "for built-in len", "", false, false, false},
		} {
			t.Run(ending.name+"/"+tc.name, func(t *testing.T) {
				source, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				definitions := "macro_rules! inner {($x:expr)=>{" + tc.body + "};}\n"
				if tc.nested {
					definitions += "macro_rules! outer {($x:expr)=>{inner!($x)};}\n"
				}
				const values = "func value()Option[int]{return Option[int]::Some(1)}\nfunc list()Option[[]int]{return Option[[]int]::Some([]int{1})}\n"
				const earlier = "enum event { A, B(int), }\nfunc before()Option[int]{a:=value()?;b:=value()?;return Option[int]::Some(a+b)}\n"
				name := "inner"
				if tc.nested {
					name = "outer"
				}
				prefix := ""
				if tc.mode == "cross" {
					prefix = "m."
				}
				badCall := prefix + name + "!(value())"
				body := ""
				if tc.name == "original-selector-after-rename" {
					definitions += "macro_rules! size {($x:expr)=>{len($x?)};}\n"
					body = "_=size!(list());"
				}
				if tc.sibling {
					body = "_=" + prefix + name + "!(list());"
				}
				if tc.mode == "native" {
					body = "_=inner!(list());"
					badCall = "len(value()?)"
				}
				caller := "func Run()Option[int]{" + body + "_=" + badCall + ";return Option[int]::None}\nfunc main(){}\n"
				files := map[string]string{}
				defFile := "main.gox"
				switch tc.mode {
				case "single", "native":
					files["main.gox"] = "package main\n" + values + earlier + definitions + caller
				case "multifile":
					defFile = "macros.gox"
					files[defFile] = "package main\n" + definitions
					files["main.gox"] = "package main\n" + values + earlier + caller
				case "cross":
					defFile = "defs/main.gox"
					files[defFile] = "package defs\n" + strings.Repeat("\n", 300) + strings.Replace(definitions, "macro_rules! outer", "pub macro_rules! outer", 1)
					files["main.gox"] = "package main\nimport m \"ast.example/app/defs\"\n" + values + earlier + "macro_rules! inner {($x:expr)=>{len($x?)};}\n" + caller
				}
				for name, src := range files {
					files[name] = strings.ReplaceAll(src, "\n", ending.text)
					packagesWrite(t, source, name, files[name])
				}
				input := filepath.Join(source, "main.gox")
				args := []string{"transpile", input}
				if tc.mode == "multifile" || tc.mode == "cross" {
					packagesWrite(t, source, "go.mod", "module ast.example/app\ngo 1.27\n")
					args = []string{"transpile", "-o", filepath.Join(t.TempDir(), "generated"), source}
				}
				output := deliveryCommand(t, outside, cli, false, nil, args...)
				if !strings.Contains(output, tc.want) {
					t.Fatalf("wrong rejection: %s", output)
				}
				if tc.precise == "" {
					if !strings.Contains(output, "generated/unmapped") || strings.Contains(output, "source span [") {
						t.Fatalf("invented original point: %s", output)
					}
				} else {
					start := strings.Index(files[defFile], tc.precise)
					want := syntheticOriginPosition(filepath.Join(source, defFile), files[defFile], start) + ":"
					if !strings.HasPrefix(output, want) || !strings.Contains(output, fmt.Sprintf("source span [%d,%d)", start, start+len(tc.precise))) {
						t.Fatalf("lost original point %s: %s", want, output)
					}
				}
				loc := func(file, token string) string {
					return syntheticOriginPosition(filepath.Join(source, file), files[file], strings.LastIndex(files[file], token))
				}
				var frames []string
				if tc.frames {
					if tc.nested {
						frames = append(frames, "  macro outer defined at "+loc(defFile, "macro_rules! outer")+"; invoked at "+loc("main.gox", badCall))
					}
					invocation := loc("main.gox", badCall)
					if tc.nested {
						invocation = loc(defFile, "inner!($x)")
					}
					frames = append(frames, "  macro inner defined at "+loc(defFile, "macro_rules! inner")+"; invoked at "+invocation)
				}
				syntheticOriginFrames(t, output, frames)
			})
		}
	}
	t.Run("runtime", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", packagesModule(root))
		packagesWrite(t, source, "main.gox", astProvenanceRuntime)
		packagesWrite(t, source, "model/model.go", astProvenanceModel)
		first := filepath.Join(t.TempDir(), "generated")
		second := filepath.Join(t.TempDir(), "generated")
		for _, out := range []string{first, second} {
			deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", out, source)
		}
		for _, name := range []string{"main.go", "main.go.map.json"} {
			a, e := os.ReadFile(filepath.Join(first, name))
			if e != nil {
				t.Fatal(e)
			}
			b, e := os.ReadFile(filepath.Join(second, name))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("repeat changed %s", name)
			}
		}
		astProvenancePayloads(t, first, filepath.Join(source, "main.gox"))
		packagesWrite(t, first, "provenance_test.go", astProvenanceAssertions)
		deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", ".")
		deliveryCommand(t, first, tool, true, nil, "vet", "./...")
		if out := deliveryCommand(t, first, tool, true, nil, "run", "-race", "."); out != "AB 5\n" {
			t.Fatalf("wrong runtime output %q", out)
		}
	})
}

// Inspect concrete payload uses in each output route, not the spelling of a
// surrounding helper. A changed source-map schema is not necessary for causes.
func astProvenancePayloads(t *testing.T, generated, source string) {
	t.Helper()
	code, err := os.ReadFile(filepath.Join(generated, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(generated, "main.go.map.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		Diagnostics sourcemap.Map `json:"diagnostics"`
	}
	if err = json.Unmarshal(raw, &exported); err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "main.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	check := func(route string, id *ast.Ident, macro, call string) {
		counts[route]++
		segment := exported.Diagnostics.Lookup(fs.Position(id.Pos()).Offset)
		if segment == nil || segment.Origin != nil || segment.Kind != "generated/unmapped" || len(segment.Stack) != 1 {
			t.Errorf("%s payload cause at %s: %#v", route, fs.Position(id.Pos()), segment)
			return
		}
		frame := segment.Stack[0]
		actual := fmt.Sprintf("  macro %s defined at %s:%d:%d; invoked at %s:%d:%d", frame.Macro, frame.Definition.File, frame.Definition.Line, frame.Definition.Column, frame.Callsite.File, frame.Callsite.Line, frame.Callsite.Column)
		want := "  macro " + macro + " defined at " + syntheticOriginPosition(source, astProvenanceRuntime, strings.Index(astProvenanceRuntime, "macro_rules! "+macro)) + "; invoked at " + syntheticOriginPosition(source, astProvenanceRuntime, strings.Index(astProvenanceRuntime, call))
		if actual != want {
			t.Errorf("%s payload cause\nwant %s\ngot %s", route, want, actual)
		}
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch fn.Name.Name {
			case "Expr":
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "len" {
						if arg, ok := call.Args[0].(*ast.Ident); ok {
							check("expr", arg, "length", `length!(list("B"`)
						}
					}
				}
			case "Effect":
				if binary, ok := n.(*ast.BinaryExpr); ok && binary.Op == token.ADD {
					if x, ok := binary.X.(*ast.Ident); ok {
						check("effect-left", x, "payload", `payload!(number("E"`)
					}
					if y, ok := binary.Y.(*ast.Ident); ok {
						check("effect-right", y, "payload", `payload!(number("F"`)
					}
				}
			case "Frame":
				if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
					if id, ok := ret.Results[0].(*ast.Ident); ok && strings.HasPrefix(id.Name, "__goxide_qtmp_") {
						check("frame", id, "payload", `payload!(number("G"`)
					}
				}
			}
			return true
		})
	}
	for _, route := range []string{"expr", "effect-left", "effect-right", "frame"} {
		if counts[route] != 1 {
			t.Errorf("expected one %s payload use; got %d", route, counts[route])
		}
	}
	// Dynamic local callee forces the private-place guard route. The emitted
	// Catch/Fail calls prove this fixture did not silently take the effect route.
	if !bytes.Contains(code, []byte(".Catch(")) || !bytes.Contains(code, []byte(".Fail(")) {
		t.Error("frame fixture lost its guard route")
	}
}

const astProvenanceRuntime = `package main
import "fmt"
import "packages.example/app/model"
var trace string
macro_rules! payload {($x:expr)=>{$x?};}
macro_rules! length {($x:expr)=>{len($x?)};}
func number(mark string,ok bool,n int)Option[int]{trace+=mark;if !ok{return Option[int]::None};return Option[int]::Some(n)}
func list(mark string,ok bool)Option[[]int]{trace+=mark;if !ok{return Option[[]int]::None};return Option[[]int]::Some([]int{1,2})}
func Expr(ok bool)Option[int]{n:=payload!(number("A",ok,3));m:=length!(list("B",true));return Option[int]::Some(n+m)}
func Nearest()Option[int]{f:=func()Option[int]{_=payload!(number("N",false,0));trace+="bad";return Option[int]::Some(1)};_=f();trace+="O";return Option[int]::Some(9)}
func nestedValue()Option[Option[int]]{return Option[Option[int]]::Some(number("N",true,4))}
func Nested()Option[int]{x:=payload!(nestedValue())?;return Option[int]::Some(x)}
func Effect(t *model.Tree,ok bool)Option[int]{t.Count=payload!(number("E",ok,3))+payload!(number("F",true,4));return Option[int]::Some(t.Count)}
func Frame(t *model.Tree,ok bool,d int)Option[int]{defer func(){trace+="D"}();calc:=func(n int)int{return 12/n};t.Count=calc(payload!(number("G",ok,d)));trace+="Z";return Option[int]::Some(t.Count)}
func Short(t *model.Tree,on bool)Option[int]{t.Enabled=on&&payload!(number("S",false,0))>0;return Option[int]::Some(1)}
func Unwrap(x Option[int])(int,bool){match x{Option[int]::Some(n)=>{return n,true},Option[int]::None=>{return 0,false},};panic("unreachable")}
func main(){n,_:=Unwrap(Expr(true));fmt.Println(trace,n)}
`
const astProvenanceModel = `package model
type inner struct{Count int;Enabled bool}
type Tree struct{*inner}
func New(n int)Tree{return Tree{&inner{Count:n}}}
`
const astProvenanceAssertions = `package main
import ("testing";"packages.example/app/model")
func TestBehavior(t *testing.T){
 for _,tc:=range []struct{call func()Option[int];value int;ok bool;trace string}{
 {func()Option[int]{return Expr(true)},5,true,"AB"},
 {func()Option[int]{return Expr(false)},0,false,"A"},
 {Nearest,9,true,"NO"},{Nested,4,true,"N"},
 }{trace="";n,ok:=Unwrap(tc.call());if n!=tc.value||ok!=tc.ok||trace!=tc.trace{t.Fatalf("got %d %v %q; want %d %v %q",n,ok,trace,tc.value,tc.ok,tc.trace)}}
}
func TestPrivateRoutes(t *testing.T){
 tree:=model.New(9)
 for _,tc:=range []struct{call func()Option[int];value int;ok bool;trace string;stored int}{
 {func()Option[int]{return Effect(&tree,true)},7,true,"EF",7},
 {func()Option[int]{return Effect(&tree,false)},0,false,"E",7},
 {func()Option[int]{return Frame(&tree,true,3)},4,true,"GZD",4},
 {func()Option[int]{return Frame(&tree,false,0)},0,false,"GD",4},
 {func()Option[int]{return Short(&tree,false)},1,true,"",4},
 {func()Option[int]{return Short(&tree,true)},0,false,"S",4},
 }{trace="";n,ok:=Unwrap(tc.call());if n!=tc.value||ok!=tc.ok||trace!=tc.trace||tree.Count!=tc.stored{t.Fatalf("got %d %v %q stored=%d",n,ok,trace,tree.Count)}}
 trace="";func(){defer func(){if recover()==nil{t.Error("native division panic was swallowed")}}();_=Frame(&tree,true,0)}()
 if trace!="GD"||tree.Count!=4{t.Fatalf("panic mutated destination or defer: %s %d",trace,tree.Count)}
}
`
