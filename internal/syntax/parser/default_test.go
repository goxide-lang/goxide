package parser_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestDefaultProtocolMemberName(t *testing.T) {
	const source = "package p\ntype T struct{}\nimpl Default for T { func default() T { return T{} } }\n"
	file, diagnostics := parser.ParseFile("default.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	member := file.Items[1].(*ast.ImplDecl).Items[0].(*ast.FuncDecl)
	if member.Name.Name != "default" || member.Name.Raw {
		t.Fatalf("default member lost source spelling: %#v", member.Name)
	}
	assertRoundTrip(t, file)
	for _, declaration := range []string{
		"func default() {}",
		"impl T { func default() T { return T{} } }",
		"impl Other for T { func default() T { return T{} } }",
		"trait Other { func default() T }",
		"impl Default for T { func default() T { default := T{}; return default } }",
	} {
		_, diagnostics := parser.ParseFile("reserved.gox", []byte("package p\ntype T struct{}\n"+declaration))
		if len(errorsOnly(diagnostics)) == 0 {
			t.Errorf("reserved default accepted outside its member-name position: %s", declaration)
		}
	}
}
