package parser_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func TestFileLineStartsDistinguishCommentPlacement(t *testing.T) {
	const trailing = "package p\nfunc f() {\n\tx := 1; // comment\n\t_ = x\n}\n"
	standalone := strings.Replace(trailing, "; // comment", ";\n// comment", 1)
	var spans [][]ast.Span
	for _, tc := range []struct {
		name, source string
		want         []uint32
	}{
		{"trailing", trailing, []uint32{0, 10, 21, 41, 48}},
		{"standalone", standalone, []uint32{0, 10, 21, 30, 41, 48}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, ds := parser.ParseFile(tc.name+".gox", []byte(tc.source))
			if len(ds) != 0 {
				t.Fatalf("parse: %v", ds)
			}
			if !reflect.DeepEqual(file.LineStarts, tc.want) {
				t.Fatalf("line starts = %v, want %v", file.LineStarts, tc.want)
			}
			var offsets []ast.Span
			ast.Walk(file, func(node ast.Node) bool {
				offsets = append(offsets, node.Span())
				return true
			})
			spans = append(spans, offsets)
		})
	}
	if len(spans) != 2 || !reflect.DeepEqual(spans[0], spans[1]) {
		t.Fatalf("reproduction requires equal node spans: %v", spans)
	}
}
