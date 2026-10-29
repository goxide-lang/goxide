package resolve_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestConstParametersResolveInArraysAndArguments(t *testing.T) {
	files := parseFiles(t, map[string]string{"const.gox": `package p
use limits;
type Ops[T any] struct {}
func Forward[T any,const N int](value [N]T) [N]T { return value }
func Template[T any,const N int](value [N]T) [N]T {
 _ = Forward[T,N](value)
 _ = Forward[T,N+1](value)
 _ = Forward[T,limits::Size](value)
 _ = Ops[T,N]::make()
 _ = Ops[T,N+1]::make()
 return value
}
impl[T any,const N int] [N]T { func keep(self Self) Self { return self } }
`})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["const.gox"]
	seen := map[*resolve.Symbol]bool{}
	for _, item := range file.AST.Items {
		var params *ast.GenericParams
		switch item := item.(type) {
		case *ast.FuncDecl:
			params = item.Generics
		case *ast.ImplDecl:
			params = item.Generics
		}
		if params == nil {
			continue
		}
		typeBinding := file.Definitions[params.Params[0].Names[0].ID()]
		binding := file.Definitions[params.Params[1].Names[0].ID()]
		if typeBinding == nil || typeBinding.Kind != resolve.TypeParam || binding == nil || binding.Kind != resolve.ConstParam || seen[binding] {
			t.Fatal("type and const parameter bindings were not separated")
		}
		seen[binding] = true
		uses := 0
		ast.Walk(item, func(node ast.Node) bool {
			name, ok := node.(*ast.Ident)
			if ok && name.Name == "N" && file.Definitions[name.ID()] == nil {
				uses++
				if file.References[name.ID()].Symbol != binding {
					t.Errorf("const reference at %v did not bind to its declaration", name.Span())
				}
			}
			return true
		})
		if uses == 0 {
			t.Fatal("const parameter uses not visited")
		}
	}
	if len(seen) != 3 {
		t.Fatalf("const declarations = %d, want 3", len(seen))
	}
}
