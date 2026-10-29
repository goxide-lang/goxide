package parser

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestConvertedGenericArgumentSpans(t *testing.T) {
	source := "Ops[T,*U,(V),N+1]::make"
	node, ds := ParseExpr([]byte(source))
	requireClean(t, ds)
	args := node.(*ast.PathExpr).Path.Segments[0].Args.Args
	for i, want := range []string{"T", "*U", "(V)", "N+1"} {
		var value ast.Node = args[i].Type
		if args[i].Value != nil {
			value = args[i].Value
		}
		for _, node := range []ast.Node{args[i], value} {
			span := node.Span()
			if got := source[span.Start:span.End]; got != want {
				t.Fatalf("argument %d %T span = %q, want %q", i, node, got, want)
			}
		}
	}
}
