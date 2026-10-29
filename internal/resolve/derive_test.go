package resolve_test

import (
	"github.com/goxide-lang/goxide/internal/resolve"
	"testing"
)

// Protocol spellings no longer register compiler helpers or reserve a type.
func TestFormerBuiltinProtocolsResolveAsOrdinaryTraits(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
trait Default[T any] { func make(value T) Self }
trait PartialEq { func equals(self Self, other Self) bool }
trait Eq where Self: PartialEq {}
type Box[T any] struct { Value T }
impl[T any] Default[T] for Box[T] { func make(value T) Self { return Self{Value:value} } }
impl[T comparable] PartialEq for Box[T] { func equals(self Self, other Self) bool { return self.Value==other.Value } }
impl[T comparable] Eq for Box[T] {}
`})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	scope := result.Files["p.gox"].Scope.Parent
	for _, name := range []string{"DefaultBox", "CloneBox", "DebugBox", "PartialEqBox"} {
		if scope.Bindings[resolve.Key{Name: name}] != nil {
			t.Fatalf("compiler manufactured protocol helper %s", name)
		}
	}
}
