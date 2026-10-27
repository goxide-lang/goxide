package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
)

func TestRenameReviewPreservesGoNameAndAssociatedTypes(t *testing.T) {
	for _, test := range []struct{ name, source, selectText, newName string }{
		{"public Go name collision", "package fixture\npub func alpha()int{return 1}\nfunc Beta()int{return 2}\nfunc F()int{return alpha()+Beta()}\n", "alpha()int", "beta"},
		{"associated type family", "package fixture\ntrait P{type Item;func get(self Self)Self::Item}\ntype A struct{}\nimpl P for A{type Item=int;func get(self Self)Self::Item{return 42}}\nfunc F(a A)int{return a.get()}\n", "Item;", "Element"},
		{"method conflicts with field", "package fixture\ntrait P{func value(self Self)int}\ntype A struct{other int}\nimpl P for A{func value(self Self)int{return self.other}}\nfunc F(a A)int{return a.value()}\n", "value(self", "other"},
		{"generic name equals associated suffix", "package fixture\ntrait P{type T;func get(self Self)Self::T}\nfunc F[T P](x T)T::P::T{return x.get()}\n", "T P", "U"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(test.source)})
			if result.Err != nil {
				t.Fatalf("invalid review fixture: %v", result.Err)
			}
			edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(test.source, test.selectText)), test.newName)
			if err != nil {
				return // An unsupported structural rename may be rejected atomically.
			}
			sources := applyQueryEdits(t, result, edits)
			after, err := analysis.Analyze(context.Background(), analysis.Input{Sources: sources, PackagePath: "fixture", Options: analysis.Options{PreserveSyntax: true}})
			if err != nil || after.Err != nil {
				t.Fatalf("rename accepted edits that break the program: %v / %v\nedits=%+v\n%s", err, after.Err, edits, sources["main.gox"])
			}
		})
	}
}
