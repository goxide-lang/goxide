package matchexpr

import (
	"go/ast"
	"go/parser"
	"strings"
	"testing"
)

func TestFunctionTypeRoleExcludesRealLiteral(t *testing.T) {
	// The length is intentionally a syntax-only input. Its function is a real
	// scope even though a later type checker rejects a nonconstant array size.
	source := `[func() int { return 1 }()]func() int{}`
	expr, err := parser.ParseExpr(source)
	if err != nil {
		t.Fatal(err)
	}
	found := map[int]bool{}
	markFunctionTypes(expr.(*ast.CompositeLit).Type, nil, 0, found)
	want := strings.LastIndex(source, "func")
	if len(found) != 1 || !found[want] {
		t.Fatalf("function type offsets %v, want only %d", found, want)
	}
}

func TestFunctionSignatureMarksOnlyNestedTypes(t *testing.T) {
	for _, header := range []string{
		"func f() func() int ",
		"func() func() int ",
		"func (x Receiver) f(v func() int) func() int ",
		"func f(v func() m::Item) func() m::Item ",
		"func (x m::Receiver) f(v func() m::Item) func() m::Item ",
		"func f(v &m::Item, w &mut m::Item) func() int ",
		"func f(v func(&mut m::Item) int) &mut m::Item ",
	} {
		t.Run(header, func(t *testing.T) {
			ft, adjustment := functionSignature(header)
			if ft == nil {
				t.Fatal("signature not parsed")
			}
			found := map[int]bool{}
			markFunctionTypes(ft, ft, adjustment, found)
			want := map[int]bool{}
			for at := 1; at < len(header); at++ {
				if strings.HasPrefix(header[at:], "func") {
					want[at] = true
				}
			}
			if len(found) != len(want) || found[0] {
				t.Fatalf("function type offsets %v, want %v", found, want)
			}
			for at := range want {
				if !found[at] {
					t.Fatalf("missing nested type at %d: %v", at, found)
				}
			}
		})
	}
}
