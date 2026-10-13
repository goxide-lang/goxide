package check_test

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func TestManualProtocolSignatureDiagnostics(t *testing.T) {
	for _, impl := range []string{
		"impl Default for T { func default() int { return 0 } }",
		"impl PartialEq for T { func equals(value T, other T) bool { return true } }",
		"impl Eq for T { func unwanted() {} }",
	} {
		source := "package p\ntype T struct{}\n" + impl
		files, resolved := parse(t, source)
		_, diagnostics := check.Check(files, resolved)
		start := strings.Index(source, impl)
		want := ast.Span{Start: uint32(start), End: uint32(len(source))}
		if len(diagnostics) != 1 || diagnostics[0].Code != check.DeriveInvalid || diagnostics[0].Span != want || diagnostics[0].Origin.Span != want {
			t.Fatalf("%s: diagnostics = %#v, want %v", impl, diagnostics, want)
		}
	}
}
