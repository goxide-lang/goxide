package frontend

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestMacroPrimitiveAliasesAfterDSLLowering(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		source := "package p\nstruct Item { Value int }\nmacro_rules! zero {()=>{int(0)};}\nfunc Run(){"
		if shadow {
			source += "type int string; _=int(\"caller\");"
		}
		source += "_=zero!()}\n"
		result, err := Compile("primitive.gox", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, "primitive.go", result.Go, 0)
		if err != nil {
			t.Fatal(err)
		}
		aliases := 0
		ast.Inspect(f, func(n ast.Node) bool {
			if spec, ok := n.(*ast.TypeSpec); ok && strings.HasPrefix(spec.Name.Name, "goxideMacroGenerated") {
				aliases++
			}
			return true
		})
		want := 0
		if shadow {
			want = 1
		}
		if aliases != want {
			t.Fatalf("shadow=%v: got %d aliases, want %d\n%s", shadow, aliases, want, result.Go)
		}
		if !shadow {
			found := false
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || id.Name != "int" {
					return true
				}
				segment := result.Diagnostics.Lookup(fs.Position(id.Pos()).Offset)
				if segment == nil || segment.Origin != nil || len(segment.Stack) != 1 || segment.Stack[0].Macro != "zero" {
					t.Fatalf("renamed primitive must retain macro context without claiming an unchanged point: %+v", segment)
				}
				checkMacroFrame(t, segment.Stack[0].Definition, source, "macro_rules! zero")
				checkMacroFrame(t, segment.Stack[0].Callsite, source, "zero!()")
				found = true
				return true
			})
			if !found {
				t.Fatal("missing restored primitive conversion")
			}
		}
	}
}

func TestMacroPrimitiveAliasesWithDeriveAttributes(t *testing.T) {
	source := "package p\n#[derive(PartialEq,Eq)]\ntype Item struct { Value int }\nmacro_rules! zero {()=>{int(0)};}\nfunc Run(){_=zero!()}\n"
	result, err := Compile("derived.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Go), "goxideMacroGenerated") || !strings.Contains(string(result.Go), "int(0)") {
		t.Fatalf("derive attributes prevented primitive simplification:\n%s", result.Go)
	}
}
