package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// Equality generation must preserve the evidence of untouched macro calls,
// including separate expansion stacks for identical repeated expansions.
func TestMacroEqualityProvenanceCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	outside := t.TempDir()
	packagesWrite(t, outside, "go.mod", "module unrelated.example/cwd\ngo 1.27\n")
	for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, mode := range []string{"single", "multifile"} {
			t.Run(ending.name+"/"+mode, func(t *testing.T) {
				source, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				files := map[string]string{}
				definitionFile := "main.gox"
				if mode == "single" {
					files["main.gox"] = "package main\nimport \"fmt\"\n" + equalityMacroDefinitions + equalityMacroDerive + equalityMacroProgram
				} else {
					definitionFile = "macros.gox"
					files[definitionFile] = "package main\n" + equalityMacroDefinitions
					files["types.gox"] = "package main\n" + equalityMacroDerive
					files["main.gox"] = "package main\nimport \"fmt\"\n" + equalityMacroProgram
					packagesWrite(t, source, "go.mod", "module equality.example/provenance\ngo 1.27\n")
				}
				for name, text := range files {
					files[name] = strings.ReplaceAll(text, "\n", ending.text)
					packagesWrite(t, source, name, files[name])
				}
				first, second := filepath.Join(t.TempDir(), "generated"), filepath.Join(t.TempDir(), "generated")
				for _, output := range []string{first, second} {
					args := []string{"transpile", "-o", output, source}
					if mode == "single" {
						if err := os.MkdirAll(output, 0700); err != nil {
							t.Fatal(err)
						}
						generated := filepath.Join(output, "main.go")
						args = []string{"transpile", "-o", generated, "--source-map", generated + ".map.json", filepath.Join(source, "main.gox")}
					}
					deliveryCommand(t, outside, cli, true, nil, args...)
				}
				for name := range files {
					generated := strings.TrimSuffix(name, ".gox") + ".go"
					for _, suffix := range []string{"", ".map.json"} {
						a := equalityProvenanceRead(t, filepath.Join(first, generated+suffix))
						b := equalityProvenanceRead(t, filepath.Join(second, generated+suffix))
						if !bytes.Equal(a, b) {
							t.Fatalf("repeated transpilation changed %s", generated+suffix)
						}
					}
				}
				equalityMacroMap(t, first, source, definitionFile, files)
				if mode == "single" {
					packagesWrite(t, first, "go.mod", "module equality.example/provenance\ngo 1.27\n")
				}
				packagesWrite(t, first, "equality_test.go", equalityMacroRuntimeAssertions)
				if ending.name == "LF" && mode == "multifile" {
					deliveryCommand(t, first, tool, true, nil, "test", "-race", "-count=1", "./...")
					deliveryCommand(t, first, tool, true, nil, "vet", "./...")
					if output := deliveryCommand(t, first, tool, true, nil, "run", "."); output != "macro equality provenance ok\n" {
						t.Fatalf("unexpected runtime output: %q", output)
					}
				} else {
					deliveryCommand(t, first, tool, true, nil, "test", "-count=1", "./...")
				}

				// The same lowering must keep a rejected nested call's original
				// definition/caller coordinates; it must never make it legal.
				badDefinitions := strings.Replace(equalityMacroDefinitions, "int(0)", `int("bad")`, 1)
				badProgram := "func Run()int{return outer!()}\nfunc main(){}\n"
				if mode == "single" {
					files["main.gox"] = "package main\n" + badDefinitions + equalityMacroDerive + badProgram
				} else {
					files[definitionFile] = "package main\n" + badDefinitions
					files["main.gox"] = "package main\n" + badProgram
				}
				for name, text := range files {
					// types.gox already has the requested line endings.
					files[name] = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", ending.text)
					packagesWrite(t, source, name, files[name])
				}
				args := []string{"transpile", filepath.Join(source, "main.gox")}
				if mode == "multifile" {
					args = []string{"transpile", "-o", filepath.Join(t.TempDir(), "rejected"), source}
				}
				output := deliveryCommand(t, outside, cli, false, nil, args...)
				start := strings.Index(files[definitionFile], `"bad"`)
				want := syntheticOriginPosition(filepath.Join(source, definitionFile), files[definitionFile], start) + ":"
				if !strings.HasPrefix(output, want) || !strings.Contains(output, `cannot convert "bad"`) || !strings.Contains(output, fmt.Sprintf("source span [%d,%d)", start, start+len(`"bad"`))) {
					t.Fatalf("lost conversion rejection's original location %s: %s", want, output)
				}
				frame := func(macro, invocationFile, invocation string) string {
					definition := syntheticOriginPosition(filepath.Join(source, definitionFile), files[definitionFile], strings.Index(files[definitionFile], "macro_rules! "+macro))
					callsite := syntheticOriginPosition(filepath.Join(source, invocationFile), files[invocationFile], strings.Index(files[invocationFile], invocation))
					return "  macro " + macro + " defined at " + definition + "; invoked at " + callsite
				}
				syntheticOriginFrames(t, output, []string{frame("outer", "main.gox", "outer!()"), frame("zero", definitionFile, "zero!()")})
			})
		}
	}
}

func equalityProvenanceRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func equalityMacroMap(t *testing.T, generated, source, definitionFile string, files map[string]string) {
	t.Helper()
	span := func(file, text string, width int, last bool) sourcemap.Span {
		start := strings.Index(files[file], text)
		if last {
			start = strings.LastIndex(files[file], text)
		}
		if start < 0 {
			t.Fatalf("missing fixture token %q in %s", text, file)
		}
		return sourcemap.Position(filepath.Join(source, file), []byte(files[file]), start, start+width)
	}
	definition := func(macro string) sourcemap.Span {
		text := files[definitionFile]
		start := strings.Index(text, "macro_rules! "+macro)
		end := start + strings.Index(text[start:], "\n")
		if text[end-1] == '\r' {
			end--
		}
		return sourcemap.Position(filepath.Join(source, definitionFile), []byte(text), start, end)
	}
	counts := map[string]int{}
	helperTokens := 0
	for name := range files {
		output := filepath.Join(generated, strings.TrimSuffix(name, ".gox")+".go")
		code := equalityProvenanceRead(t, output)
		var exported struct {
			Diagnostics sourcemap.Map `json:"diagnostics"`
		}
		if err := json.Unmarshal(equalityProvenanceRead(t, output+".map.json"), &exported); err != nil {
			t.Fatal(err)
		}
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, output, code, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "EqualsItem" {
				counts[fn.Name.Name]++
				// Check every emitted token, including punctuation, so helper
				// boundaries cannot borrow a nearby macro's origin or stack.
				var scan scanner.Scanner
				scan.Init(fs.File(fn.Pos()), code, nil, 0)
				for {
					pos, kind, _ := scan.Scan()
					if kind == token.EOF || pos >= fn.End() {
						break
					}
					if pos < fn.Pos() || kind == token.SEMICOLON {
						continue
					}
					helperTokens++
					segment := exported.Diagnostics.Lookup(fs.Position(pos).Offset)
					if segment != nil && (segment.Kind != "generated/unmapped" || segment.Origin != nil || len(segment.Stack) != 0) {
						t.Errorf("generated equality helper acquired provenance at %s: %+v", fs.Position(pos), segment)
					}
				}
				continue
			}
			if fn.Name.Name != "Direct" && fn.Name.Name != "First" && fn.Name.Name != "Second" && fn.Name.Name != "Native" {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				counts[fn.Name.Name]++
				id, idOK := call.Fun.(*ast.Ident)
				literal, literalOK := call.Args[0].(*ast.BasicLit)
				if !idOK || !literalOK || literal.Value != "0" {
					t.Fatalf("unexpected fixture call in %s: %#v", fn.Name.Name, call)
				}
				calleeOrigin := span(definitionFile, "int(0)", len("int"), false)
				literalOrigin := span(definitionFile, "0)", 1, false)
				kind := "macro"
				var frames []sourcemap.Expansion
				if fn.Name.Name == "Native" {
					calleeOrigin = span("main.gox", "int(0)", len("int"), true)
					literalOrigin = span("main.gox", "0)", 1, true)
					kind = "source"
				} else if fn.Name.Name == "Direct" {
					frames = []sourcemap.Expansion{{Macro: "zero", Definition: definition("zero"), Callsite: span("main.gox", "zero!()", len("zero!()"), true)}}
				} else {
					frames = []sourcemap.Expansion{
						{Macro: "outer", Definition: definition("outer"), Callsite: span("main.gox", "outer!()", len("outer!()"), fn.Name.Name == "Second")},
						{Macro: "zero", Definition: definition("zero"), Callsite: span(definitionFile, "zero!()", len("zero!()"), false)},
					}
				}
				for _, check := range []struct {
					pos    token.Pos
					origin sourcemap.Span
				}{{id.Pos(), calleeOrigin}, {literal.Pos(), literalOrigin}} {
					segment := exported.Diagnostics.Lookup(fs.Position(check.pos).Offset)
					if segment == nil || segment.Origin == nil || *segment.Origin != check.origin || segment.Kind != kind || len(segment.Stack) != len(frames) {
						t.Errorf("%s at %s: got %+v; want %s origin %+v and %d frames", fn.Name.Name, fs.Position(check.pos), segment, kind, check.origin, len(frames))
						continue
					}
					for i, frame := range frames {
						got := segment.Stack[i]
						if got.Macro != frame.Macro || got.Definition != frame.Definition || got.Callsite != frame.Callsite {
							t.Errorf("%s frame %d: got %+v; want %+v", fn.Name.Name, i, got, frame)
						}
					}
				}
				return true
			})
		}
	}
	for _, name := range []string{"Direct", "First", "Second", "Native", "EqualsItem"} {
		if counts[name] != 1 {
			t.Errorf("expected one %s call/helper; got %d", name, counts[name])
		}
	}
	if helperTokens == 0 {
		t.Error("no generated equality helper tokens inspected")
	}
}

const equalityMacroDefinitions = `macro_rules! zero {()=>{int(0)};}
macro_rules! outer {()=>{zero!()};}
macro_rules! convert {($x:expr)=>{int($x)};}
`

const equalityMacroDerive = `#[derive(PartialEq,Eq)]
type Item struct{N int}
`

const equalityMacroProgram = `func Direct()int{return zero!()}
func First()int{return outer!()}
func Second()int{return outer!()}
func Native()int{return int(0)}
var calls int
func next()int{calls++;return calls}
func Effect()int{return convert!(next())+convert!(next())}
func main(){if Direct()!=0||First()!=0||Second()!=0||Native()!=0||Effect()!=3||calls!=2||!EqualsItem(Item{N:1},Item{N:1})||EqualsItem(Item{N:1},Item{N:2}){panic("macro equality behavior")};fmt.Println("macro equality provenance ok")}
`

const equalityMacroRuntimeAssertions = `package main
import "testing"
func TestMacroEqualityBehavior(t *testing.T){
 if Direct()!=0||First()!=0||Second()!=0||Native()!=0{t.Fatal("zero conversion")}
 calls=0
 if Effect()!=3||calls!=2{t.Fatal("macro argument evaluation count",calls)}
 if !EqualsItem(Item{N:1},Item{N:1})||EqualsItem(Item{N:1},Item{N:2}){t.Fatal("derived equality")}
}
`
