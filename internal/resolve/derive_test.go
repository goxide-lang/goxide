package resolve_test

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func TestConcreteProtocolImplRejectsTypeArguments(t *testing.T) {
	for _, header := range []string{"impl Default[int] for T", "impl Default for T[int]"} {
		source := "package p\ntype T struct{}\n" + header + " { func default() T { return T{} } }"
		files := parseFiles(t, map[string]string{"p.gox": source})
		result := resolve.Resolve(files, nil)
		start := strings.Index(source, header)
		d := diagnosticAt(t, result, "p.gox", ast.Span{Start: uint32(start), End: uint32(len(source))})
		if d.Code != "derive-manual" || len(result.Derives) != 0 {
			t.Fatalf("invalid protocol registered: %#v, %v", result.Derives, result.Diagnostics)
		}
	}
}
