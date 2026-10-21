package check_test

import (
	"fmt"
	"go/importer"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestMacroGenericTypeCaptureCategories(t *testing.T) {
	for _, kind := range []string{"expr", "ident", "tt", "type"} {
		for _, test := range []struct{ name, template, input string }{
			{"single", "One[$T](0)", "int"},
			{"multiple", "Pair[$T,string](0, \"\")", "int"},
			{"function value", "pair[$T, int]", "int"},
			{"parenthesized", "Pair[($T),string](0, \"\")", "int"},
			{"pointer", "Pair[*$T,string](nil, \"\")", "int"},
			{"reference", "Pair[&$T,string](nil, \"\")", "int"},
			{"path", "Choice[$T]::Some(0)", "int"},
			{"native", "cmp::Compare[$T](1,2)", "int"},
			{"native multiple", "slices::Clone[$T,int](Ints{})", "Ints"},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				source := fmt.Sprintf(`package main
use cmp;
use slices;
type Ints []int
enum Choice[T any] { Some(T) }
func One[T any](value T) T { return value }
func Pair[A any,B any](a A,b B) A { return a }
func pair[A any,B any](a A,b B) A { return a }
macro_rules! call { ($T:%s) => { %s }; }
func f() { _ = call!(%s) }
`, kind, test.template, test.input)
				file, diagnostics := parser.ParseFile("main.gox", []byte(source))
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				expanded, err := expand.Expand([]*ast.File{file}, nil)
				if err != nil {
					t.Fatalf("ambiguous slot rejected before parameter lookup: %v", err)
				}
				resolved := resolve.ResolveExpanded(map[string]*ast.File{"main.gox": file}, expanded, nil)
				if len(resolved.Diagnostics) != 0 {
					t.Fatal(resolved.Diagnostics)
				}
				_, ds := check.CheckWithImporter(map[string]*ast.File{"main.gox": expanded.Files[0]}, resolved, importer.Default(), nil)
				if kind == "type" {
					if len(ds) != 0 {
						t.Fatal(ds)
					}
					return
				}
				for _, d := range ds {
					if d.Code == check.GenericConstraint && strings.Contains(d.Message, "captured types from "+kind+" fragments") {
						if d.Origin.Expn == 0 || len(d.Origin.Stack) == 0 || d.Origin.Span.Start >= d.Origin.Span.End {
							t.Fatalf("capture rejection lost expansion origin: %#v", d)
						}
						return
					}
				}
				t.Fatalf("missing %s fragment rejection: %v", kind, ds)
			})
		}
	}
}
