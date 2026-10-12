package ast_test

import (
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

func TestWalkVisitsSharedNodesOnce(t *testing.T) {
	doc := &ast.CommentGroup{List: []*ast.Comment{{Text: "// F documents a function."}}}
	attr := &ast.Attr{Path: &ast.Path{Segments: []*ast.PathSegment{{Name: &ast.Ident{Name: "cfg"}}}}}
	fn := &ast.FuncDecl{ItemBase: ast.ItemBase{Doc: doc, Attrs: []*ast.Attr{attr}}, Name: &ast.Ident{Name: "F"}, Body: &ast.Block{List: []ast.Stmt{&ast.ReturnStmt{}}}}
	file := &ast.File{Package: &ast.Ident{Name: "p"}, Items: []ast.Item{fn}, Comments: []*ast.CommentGroup{doc}}
	visited := map[ast.Node]int{}
	ast.Walk(file, func(n ast.Node) bool {
		visited[n]++
		n.(interface{ SetID(ast.NodeID) }).SetID(ast.NodeID(len(visited)))
		return true
	})
	for n, count := range visited {
		if count != 1 {
			t.Errorf("%T visited %d times", n, count)
		}
		if n.ID() == 0 {
			t.Errorf("%T has no ID", n)
		}
	}
	if visited[attr] != 1 || visited[doc] != 1 || visited[fn.Body] != 1 {
		t.Fatal("embedded metadata or body omitted")
	}
	var nilExpr *ast.Ident
	ast.Walk(nilExpr, func(ast.Node) bool { t.Fatal("visited typed nil"); return true })
}

func TestWalkPrune(t *testing.T) {
	tree := &ast.RefExpr{X: &ast.Ident{Name: "x"}}
	count := 0
	ast.Walk(tree, func(ast.Node) bool { count++; return false })
	if count != 1 {
		t.Fatalf("visited %d nodes after pruning", count)
	}
}

func TestIdentityAndSpan(t *testing.T) {
	id := &ast.Ident{Base: ast.Base{NodeID: 7, Loc: ast.Span{Start: 12, End: 18}, Expn: 2}, Name: "match", Raw: true, Ctx: 3}
	if id.ID() != 7 || id.Span() != (ast.Span{Start: 12, End: 18}) {
		t.Fatal("identity or span lost")
	}
	id.SetID(9)
	if id.ID() != 9 || !id.Raw || id.Ctx != 3 || id.Expn != 2 {
		t.Fatal("renumbering changed syntax or hygiene")
	}
}

func TestReservedNodesRetainCategories(t *testing.T) {
	nodes := []ast.Node{&ast.AwaitExpr{}, &ast.LetStmt{}, &ast.LoopStmt{}, &ast.WhileStmt{}, &ast.StructPat{}, &ast.RangePat{}}
	for _, n := range nodes {
		switch n.(type) {
		case ast.Expr, ast.Stmt, ast.Pat:
		default:
			t.Fatalf("reserved node %T has no category", n)
		}
	}
}
