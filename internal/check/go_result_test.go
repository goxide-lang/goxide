package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestGoResultCapturedTypeIdentity(t *testing.T) {
	for _, name := range []string{"Number", "int"} {
		t.Run(name, func(t *testing.T) {
			source := `package p
type Number int
func pair() (Number,error) { return 7,nil }
macro_rules! typed { ($t:ident,$x:expr) => { go_result!($t,$x) }; }
func adapt() Result[Number,error] { return typed!(` + name + `,pair()) }
`
			file, ds := parser.ParseFile("main.gox", []byte(source))
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			expanded, err := expand.Expand([]*ast.File{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolved := resolve.ResolveExpanded(map[string]*ast.File{"main.gox": file}, expanded, nil)
			if len(resolved.Diagnostics) != 0 {
				t.Fatal(resolved.Diagnostics)
			}
			_, diagnostics := check.Check(map[string]*ast.File{"main.gox": expanded.Files[0]}, resolved)
			if name == "Number" {
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				return
			}
			for _, d := range diagnostics {
				if d.Code == "go-result" && strings.Contains(d.Message, "explicit T must be identical") && len(d.Origin.Stack) > 0 {
					return
				}
			}
			t.Fatalf("wrong captured type lost identity rejection or origin: %v", diagnostics)
		})
	}
}
