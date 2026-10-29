package lower_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestTraitTemplateUniversalConditionalInherent(t *testing.T) {
	const source = `package main
trait Marker {}
impl[T any] Marker for []T {}
struct Box[T any] { Values []T }
impl[T any] Box[T] where []T: Marker {
    pub func size(self &Self) int { return len(self.Values) }
}
func main() { box:=Box[int]{Values:[]int{7}}; println(box.size()) }
`
	const name = "macrobindingfixture/main.gox"
	generated, err := lowerDeriveSources(map[string]string{name: source})
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", generated[name], 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Type.TypeParams == nil {
			continue
		}
		for _, parameter := range function.Type.TypeParams.List {
			for _, identifier := range parameter.Names {
				if strings.HasSuffix(identifier.Name, "Ops") {
					t.Fatalf("conditional inherent method generated an extra operations type parameter: %s.%s", function.Name, identifier)
				}
			}
		}
	}
	runMacroBindingSource(t, source, "1\n")
}
