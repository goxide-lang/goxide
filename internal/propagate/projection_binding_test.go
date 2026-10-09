package propagate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/hxsf/goxide/internal/typecheck"
)

func TestProjectionBindingRetainsFunctionIdentityAndTypeScope(t *testing.T) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "projection.go", `package p
import s "github.com/goxide-lang/std/sum"
type Payload = int
type __goxide_payload = Payload
func f(v s.Option[Payload]) int { s:=0;Payload:=0;_=s;_=Payload;return 0 }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	l := &lowering{fs: fs, file: file, cfg: Config{Importer: typecheck.NewImporter("input.gox"), PackagePath: "projection.test"}}
	l.check()
	project, err := l.bindProjectionFunction("s.ProjectSome[Payload]", []string{"__goxide_payload"})
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if candidate, ok := decl.(*ast.FuncDecl); ok {
			fn = candidate
		}
	}
	fn.Body.List[len(fn.Body.List)-1] = &ast.ReturnStmt{Results: []ast.Expr{&ast.StarExpr{X: &ast.CallExpr{Fun: parseExpr(project), Args: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: ast.NewIdent("v")}}}}}}
	l.check()
	if len(l.checkErrors) != 0 {
		t.Fatal(l.checkErrors)
	}
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "ProjectSome" {
			return true
		}
		object, ok := l.info.Uses[selector.Sel].(*types.Func)
		if !ok || object.Pkg().Path() != "github.com/goxide-lang/std/sum" || object.Pkg().Scope().Lookup(object.Name()) != object {
			t.Fatal("canonical projection identity erased")
		}
		found = true
		return true
	})
	if !found {
		t.Fatal("missing direct projector call")
	}
}

func TestCompanionObserverBindingKeepsDefiningImport(t *testing.T) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "consumer.go", `package p
import s "fmt"
var _ = s.Sprint
type __goxide_payload = int
func f(v Payload) int {s:=0;int:=0;_=s;_=int;return 0}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	companion, err := parser.ParseFile(fs, "definition.go", `package p
import s "github.com/goxide-lang/std/sum"
type Payload = s.Option[int]
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	l := &lowering{fs: fs, file: file, companions: []*ast.File{companion}, cfg: Config{Importer: typecheck.NewImporter("input.gox"), PackagePath: "observation.test"}}
	l.check()
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if candidate, ok := decl.(*ast.FuncDecl); ok {
			fn = candidate
		}
	}
	for _, helper := range []string{"TagOption", "ValueSome"} {
		bound, err := l.bindCanonicalEnumExpression("s."+helper+"[int]", []string{"__goxide_payload"}, companion, false)
		if err != nil {
			t.Fatal(err)
		}
		call := &ast.CallExpr{Fun: parseExpr(bound), Args: []ast.Expr{ast.NewIdent("v")}}
		fn.Body.List = append(fn.Body.List[:len(fn.Body.List)-1], &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{call}}, &ast.ReturnStmt{Results: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}}})
	}
	l.check()
	if len(l.checkErrors) != 0 {
		t.Fatal(l.checkErrors)
	}
	seen := map[string]bool{}
	ast.Inspect(fn, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		object, ok := l.info.Uses[selector.Sel].(*types.Func)
		if !ok || object.Pkg().Path() != "github.com/goxide-lang/std/sum" || object.Pkg().Scope().Lookup(object.Name()) != object {
			t.Fatal("observer owner was captured or erased")
		}
		seen[object.Name()] = true
		return true
	})
	if !seen["TagOption"] || !seen["ValueSome"] {
		t.Fatal("missing direct observer functions", seen)
	}
}
