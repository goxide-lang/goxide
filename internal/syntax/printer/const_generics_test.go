package printer_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

func TestConstGenericRoundTrip(t *testing.T) {
	file, ds := parser.ParseFile("const.gox", []byte(`package p
func Copy[T any,const N int](value [N]T) [N]T { return Forward[T,N](value) }
func Fixed[const N int]() { _ = Ops[int,3]::make(); _ = Ops[int,N+1]::make() }
impl[T any,const N int] [N]T { func keep(self Self) Self { return self } }
`))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	out := printer.Source(file)
	for _, want := range []string{"const N int", "Ops[int, 3]", "Ops[int, N + 1]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("printed source omitted %q:\n%s", want, out)
		}
	}
	next, ds := parser.ParseFile("printed.gox", []byte(out))
	if len(ds) != 0 {
		t.Fatalf("%v\n%s", ds, out)
	}
	consts, values := 0, 0
	ast.Walk(next, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.GenericParam:
			if node.Const {
				consts++
			}
		case *ast.GenericArg:
			if node.Value != nil {
				values++
			}
		}
		return true
	})
	if consts != 3 || values != 2 || printer.Source(next) != out {
		t.Fatalf("roundtrip const parameters/value arguments = %d/%d:\n%s", consts, values, printer.Source(next))
	}
}
