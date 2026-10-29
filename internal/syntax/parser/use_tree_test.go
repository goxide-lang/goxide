package parser_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

func TestUseTreeSyntax(t *testing.T) {
	const source = `package p
pub use foo::{self, Item as Alias, nested::{self as ns, *},};
use {foo::A, bar::B};
use foo::{};
use {};
use foo::{r#self};
`
	file, diagnostics := parser.ParseFile("uses.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	want := []string{"pub use foo::{self, Item as Alias, nested::{self as ns, *}}", "use {foo::A, bar::B}", "use foo::{}", "use {}", "use foo::{r#self}"}
	if len(file.Items) != len(want) {
		t.Fatalf("source declarations = %d, want %d", len(file.Items), len(want))
	}
	for i, item := range file.Items {
		if got := strings.TrimSpace(printer.Source(item)); got != want[i] {
			t.Fatalf("use %d = %q, want %q", i, got, want[i])
		}
	}
	seen := map[ast.NodeID]bool{}
	ast.Walk(file, func(n ast.Node) bool {
		if seen[n.ID()] {
			t.Fatalf("raw uses share a node: %T at %v", n, n.Span())
		}
		seen[n.ID()] = true
		return true
	})
	assertRoundTrip(t, file)
}

func TestUseTreeItemRemainsWhole(t *testing.T) {
	const source = "use {foo::{self, A as B, nested::{Item, *}}, bar}"
	item, diagnostics := parser.ParseItem([]byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	use := item.(*ast.UseDecl)
	if use.Tree.Children == nil || len(use.Tree.Children) != 2 {
		t.Fatal("item fragment lost its grouped declaration")
	}
	printed := printer.Source(item)
	again, diagnostics := parser.ParseItem([]byte(printed))
	if len(errorsOnly(diagnostics)) != 0 || treeJSON(item) != treeJSON(again) {
		t.Fatalf("grouped item round trip failed: %v\n%s", diagnostics, printed)
	}
}

func TestUseTreeRejectsInvalidSuffixes(t *testing.T) {
	for _, source := range []string{"use foo::{A B}", "use foo::{A} as x", "use foo::* as x", "use *", "use {*, foo}", "use foo{A}"} {
		t.Run(source, func(t *testing.T) {
			file, diagnostics := parser.ParseFile("uses.gox", []byte("package p\n"+source))
			if len(errorsOnly(diagnostics)) == 0 || len(badNodes(file)) == 0 {
				t.Fatalf("invalid use accepted: %v", diagnostics)
			}
		})
	}
}
