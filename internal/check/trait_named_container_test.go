package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestTraitSelfPreservesNamedContainerPermissions(t *testing.T) {
	for _, declaration := range []string{
		"type Value []int",
		"type Value map[string]int",
		"type Value [2]int",
		"type Value []&Item",
		"type Value map[string]&Item",
	} {
		t.Run(declaration, func(t *testing.T) {
			files, resolved := parse(t, `package p
struct Item { N int }
trait Compare { func equals(self Self, other Self) bool }
`+declaration+`
impl Compare for Value { func equals(self Self, other Self) bool { return len(self)==len(other) } }
func compare(a Value,b Value)bool{return Compare::equals(a,b)}
`)
			if _, diagnostics := check.Check(files, resolved); len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
		})
	}
	files, resolved := parse(t, `package p
struct Item { N int }
type Value []&Item
trait Change { func change(self Self, other Self) }
impl Change for Value { func change(self Self, other Self) { other[0].N=2 } }
`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "cannot write through") {
			return
		}
	}
	t.Fatalf("named readonly elements lost their permissions: %v", diagnostics)
}
