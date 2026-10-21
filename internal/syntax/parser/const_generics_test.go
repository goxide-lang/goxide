package parser

import (
	"fmt"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestConstParameterDeclarations(t *testing.T) {
	file, ds := ParseFile("const.gox", []byte(`package p
func Copy[T,U any,const N,M int,V any](values [N]T) [M]U { return Forward[T,N](values) }
impl[T any,const N int] [N]T { func keep(self Self) Self { return self } }
type Array[const N int] [N]int
type Alias[const N int] = [N]int
struct Record[const N int] { Values [N]int }
enum Choice[const N int] { Value([N]int) }
trait Reader[const N int] { func read() [N]int }
`))
	requireClean(t, ds)
	function := file.Items[0].(*ast.FuncDecl)
	params := function.Generics.Params
	if len(params) != 3 || len(params[0].Names) != 2 || len(params[1].Names) != 2 || params[0].Const || !params[1].Const || params[2].Const {
		t.Fatalf("mixed parameter groups = %#v", params)
	}
	if params[1].Names[0].Name != "N" || params[1].Names[1].Name != "M" || params[2].Names[0].Name != "V" {
		t.Fatal("mixed parameter order changed")
	}
	if function.Params[0].Type.(*ast.ArrayType).Len.(*ast.Ident).Name != "N" {
		t.Fatal("array length lost its parameter expression")
	}
	for _, item := range file.Items[1:] {
		var generics *ast.GenericParams
		switch item := item.(type) {
		case *ast.ImplDecl:
			generics = item.Generics
		case *ast.TypeDecl:
			generics = item.Generics
		case *ast.EnumDecl:
			generics = item.Generics
		case *ast.TraitDecl:
			generics = item.Generics
		}
		if generics == nil || !generics.Params[len(generics.Params)-1].Const {
			t.Fatalf("%T lost its const parameter before semantic validation", item)
		}
	}
}

func TestConstGenericArguments(t *testing.T) {
	for _, tc := range []struct{ source, value string }{
		{"Array[int, 3]", "*ast.BasicLit"},
		{"Array[int, -1]", "*ast.UnaryExpr"},
		{"Array[int, N+1]", "*ast.BinaryExpr"},
		{"Array[int, N]", ""},
		{"Array[int, pkg::Size]", ""},
	} {
		t.Run(tc.source, func(t *testing.T) {
			node, ds := ParseType([]byte(tc.source))
			requireClean(t, ds)
			arg := node.(*ast.PathType).Path.Segments[0].Args.Args[1]
			if tc.value == "" {
				if _, ok := arg.Type.(*ast.PathType); !ok || arg.Value != nil {
					t.Fatalf("ambiguous argument = %#v", arg)
				}
			} else if got := fmt.Sprintf("%T", arg.Value); got != tc.value || arg.Type != nil {
				t.Fatalf("value argument = %s / %T, want %s / nil", got, arg.Type, tc.value)
			}
		})
	}
	for _, source := range []string{"Ops[T, 3]::make", "Ops[T, N+1]::make"} {
		node, ds := ParseExpr([]byte(source))
		requireClean(t, ds)
		if node.(*ast.PathExpr).Path.Segments[0].Args.Args[1].Value == nil {
			t.Fatalf("static path lost value argument: %s", source)
		}
	}
	projection, ds := ParseExpr([]byte("pkg.Ops[T, 3]{}"))
	requireClean(t, ds)
	if projection.(*ast.CompositeLit).Type.(*ast.ProjectionType).Args.Args[1].Value == nil {
		t.Fatal("projection lost value argument")
	}
	for _, source := range []string{"Array[Item=[3]int]", "Array[~int | ~int64]"} {
		node, ds := ParseType([]byte(source))
		requireClean(t, ds)
		arg := node.(*ast.PathType).Path.Segments[0].Args.Args[0]
		if arg.Type == nil || arg.Value != nil {
			t.Fatalf("type-only syntax changed: %s", source)
		}
	}
	indexed, ds := ParseExpr([]byte("values[3]"))
	requireClean(t, ds)
	if _, ok := indexed.(*ast.IndexExpr); !ok {
		t.Fatalf("ordinary indexing changed to %T", indexed)
	}
}

func TestConstParameterRecovery(t *testing.T) {
	file, ds := ParseFile("recover.gox", []byte("package p; func broken[const ]() {}; func later() {}"))
	if len(ds) == 0 {
		t.Fatal("missing const parameter name accepted")
	}
	for _, item := range file.Items {
		if function, ok := item.(*ast.FuncDecl); ok && function.Name.Name == "later" {
			return
		}
	}
	t.Fatal("const parameter recovery lost the following declaration")
}
