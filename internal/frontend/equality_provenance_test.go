package frontend

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestMacroEqualityDerivePreservesCallProvenance(t *testing.T) {
	source := "package p\nmacro_rules! zero {()=>{int(0)};}\n#[derive(PartialEq,Eq)]\ntype Item struct{N int}\nfunc Run()int{return zero!()}\n"
	result, err := Compile("equality.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "equality.go", result.Go, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "Run" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("missing Run function")
	}
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		found = true
		segment := result.Diagnostics.Lookup(fs.Position(call.Fun.Pos()).Offset)
		start := strings.Index(source, "int(0)")
		want := sourcemap.Position("equality.gox", []byte(source), start, start+len("int"))
		if segment == nil || segment.Origin == nil || *segment.Origin != want || len(segment.Stack) != 1 || segment.Stack[0].Macro != "zero" {
			t.Fatalf("expanded int call lost exact macro provenance: %+v; want origin %+v and zero frame", segment, want)
		}
		checkMacroFrame(t, segment.Stack[0].Definition, source, "macro_rules! zero")
		checkMacroFrame(t, segment.Stack[0].Callsite, source, "zero!(")
		return true
	})
	if !found {
		t.Fatalf("missing expanded int call:\n%s", result.Go)
	}
}

func TestMacroEqualityProvenanceMatrix(t *testing.T) {
	for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, derive := range []string{"none", "PartialEq", "PartialEq,Eq"} {
			for _, mode := range []string{"single", "package"} {
				for _, expansion := range []string{"direct", "nested"} {
					t.Run(ending.name+"/"+derive+"/"+mode+"/"+expansion, func(t *testing.T) {
						definitions := "macro_rules! zero {()=>{int(0)};}\n"
						macro := "zero"
						if expansion == "nested" {
							definitions += "macro_rules! outer {()=>{zero!()};}\n"
							macro = "outer"
						}
						declaration := ""
						if derive != "none" {
							declaration = "#[derive(" + derive + ")]\ntype Item struct{N int}\n"
						}
						caller := declaration + "func Run()int{return " + macro + "!()+" + macro + "!()}\nfunc Native()int{return int(0)}\n"
						if derive != "none" {
							caller += "func Compare(a,b Item)bool{return a.equals(b)}\n"
						}
						const callerFile = "caller.gox"
						definitionFile := callerFile
						sources := map[string][]byte{}
						if mode == "single" {
							sources[callerFile] = []byte("package p\n" + definitions + caller)
						} else {
							definitionFile = "definitions.gox"
							sources[definitionFile] = []byte("package p\n" + definitions)
							sources[callerFile] = []byte("package p\n" + caller)
						}
						for file, source := range sources {
							sources[file] = bytes.ReplaceAll(source, []byte("\n"), []byte(ending.text))
						}
						compile := func() Result {
							if mode == "single" {
								result, err := Compile(callerFile, sources[callerFile])
								if err != nil {
									t.Fatal(err)
								}
								return result
							}
							pkg, err := CompilePackage("example/equality", sources, nil)
							if err != nil {
								t.Fatal(err)
							}
							return pkg.Files[callerFile]
						}
						result := compile()
						macroEqualityProvenance(t, result, sources, callerFile, definitionFile, macro, derive != "none")
						again := compile()
						if !bytes.Equal(result.Go, again.Go) || !bytes.Equal(result.SourceMap, again.SourceMap) {
							t.Fatal("repeated compilation changed generated Go or source map")
						}
					})
				}
			}
		}
	}
}

func TestMacroEqualityRepeatedCaptureProvenance(t *testing.T) {
	const source = "package p\nmacro_rules! values {($($x:expr),*)=>{[]int{$(int($x)),*}};}\n#[derive(PartialEq,Eq)]\ntype Item struct{N int}\nfunc Run()[]int{return values!(7,9)}\n"
	result, err := Compile("repeat.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "repeat.go", result.Go, 0)
	if err != nil {
		t.Fatal(err)
	}
	var offsets []int
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Run" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := result.Diagnostics.Lookup(fs.Position(call.Fun.Pos()).Offset)
			start := strings.Index(source, "int($x)")
			want := sourcemap.Position("repeat.gox", []byte(source), start, start+len("int"))
			if callee == nil || callee.Origin == nil || *callee.Origin != want || len(callee.Stack) != 1 || callee.Stack[0].Macro != "values" {
				t.Fatalf("repetition lost template token: %+v", callee)
			}
			arg := call.Args[0]
			for {
				paren, ok := arg.(*ast.ParenExpr)
				if !ok {
					break
				}
				if s := result.Diagnostics.Lookup(fs.Position(paren.Pos()).Offset); s != nil && s.Origin != nil {
					t.Fatalf("synthetic capture parentheses borrowed source: %+v", s)
				}
				arg = paren.X
			}
			argument := result.Diagnostics.Lookup(fs.Position(arg.Pos()).Offset)
			if argument == nil || argument.Origin == nil || !reflect.DeepEqual(argument.Stack, callee.Stack) {
				t.Fatalf("repetition lost captured token: %+v", argument)
			}
			argumentWant := sourcemap.Position("repeat.gox", []byte(source), argument.Origin.Start, argument.Origin.Start+1)
			if *argument.Origin != argumentWant {
				t.Fatalf("repetition capture has incorrect coordinates: %+v", argument)
			}
			checkMacroFrame(t, callee.Stack[0].Definition, source, "macro_rules! values")
			checkMacroFrame(t, callee.Stack[0].Callsite, source, "values!(7,9)")
			offsets = append(offsets, argument.Origin.Start)
			return true
		})
	}
	start := strings.Index(source, "values!(7,9)") + len("values!(")
	if !reflect.DeepEqual(offsets, []int{start, start + 2}) {
		t.Fatalf("repetition captures crossed or lost: %v", offsets)
	}
}

func macroEqualityProvenance(t *testing.T, result Result, sources map[string][]byte, callerFile, definitionFile, macro string, derived bool) {
	t.Helper()
	var exported struct {
		Diagnostics sourcemap.Map `json:"diagnostics"`
	}
	if err := json.Unmarshal(result.SourceMap, &exported); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Diagnostics.Segments, exported.Diagnostics.Segments) || !reflect.DeepEqual(result.Diagnostics.Stages, exported.Diagnostics.Stages) {
		t.Fatal("exported source map disagrees with structured diagnostics")
	}
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "caller.go", result.Go, 0)
	if err != nil {
		t.Fatal(err)
	}
	caller, definitions := string(sources[callerFile]), string(sources[definitionFile])
	callText := macro + "!()"
	callStart := strings.Index(caller, "return "+callText) + len("return ")
	callSites := []int{callStart, callStart + len(callText) + 1}
	checkToken := func(node ast.Node, sourceFile string, start, width int, frames []sourcemap.Expansion) {
		t.Helper()
		want := sourcemap.Position(sourceFile, sources[sourceFile], start, start+width)
		s := result.Diagnostics.Lookup(fs.Position(node.Pos()).Offset)
		if s == nil || s.Origin == nil || *s.Origin != want || !reflect.DeepEqual(s.Stack, frames) {
			t.Errorf("wrong token provenance at %s: %+v; want origin %+v frames %+v", fs.Position(node.Pos()), s, want, frames)
		}
	}
	frame := func(name, callFile string, callOffset int) sourcemap.Expansion {
		start := strings.Index(definitions, "macro_rules! "+name)
		end := start + strings.Index(definitions[start:], "};}") + len("};}")
		return sourcemap.Expansion{
			Macro:              name,
			Definition:         sourcemap.Position(definitionFile, sources[definitionFile], start, end),
			Callsite:           sourcemap.Position(callFile, sources[callFile], callOffset, callOffset+len(name+"!()")),
			DefinitionOriginal: true, CallsiteOriginal: true,
		}
	}
	macroCalls, nativeCalls, helpers := 0, 0, 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "Run":
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if macroCalls >= len(callSites) {
					t.Fatal("unexpected extra macro conversion")
				}
				frames := []sourcemap.Expansion{frame(macro, callerFile, callSites[macroCalls])}
				if macro == "outer" {
					frames = append(frames, frame("zero", definitionFile, strings.Index(definitions, "zero!()")))
				}
				start := strings.Index(definitions, "int(0)")
				checkToken(call.Fun, definitionFile, start, len("int"), frames)
				checkToken(call.Args[0], definitionFile, start+len("int("), 1, frames)
				macroCalls++
				return true
			})
		case "Native":
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					start := strings.Index(caller, "return int(0)") + len("return ")
					checkToken(call.Fun, callerFile, start, len("int"), nil)
					checkToken(call.Args[0], callerFile, start+len("int("), 1, nil)
					nativeCalls++
				}
				return true
			})
		case "EqualsItem":
			helpers++
			start, end := fs.Position(fn.Pos()).Offset, fs.Position(fn.End()).Offset
			for _, token := range result.Diagnostics.Segments {
				if token.Start < start || token.End > end {
					continue
				}
				s := result.Diagnostics.Lookup(token.Start)
				if s.Origin != nil || len(s.Stack) != 0 || s.Kind != "generated/unmapped" {
					t.Errorf("generated equality helper borrowed source provenance: %+v", s)
				}
			}
		}
	}
	wantHelpers := 0
	if derived {
		wantHelpers = 1
	}
	if macroCalls != 2 || nativeCalls != 1 || helpers != wantHelpers {
		t.Fatalf("unexpected coverage: macro=%d native=%d helpers=%d (want %d)", macroCalls, nativeCalls, helpers, wantHelpers)
	}
}
