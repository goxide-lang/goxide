package printer_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func ident(name string) *ast.Ident { return &ast.Ident{Name: name} }
func typ(name string) ast.Type {
	return &ast.PathType{Path: &ast.Path{Segments: []*ast.PathSegment{{Name: ident(name)}}}}
}

func TestWrittenForms(t *testing.T) {
	fields := []*ast.Field{{Names: []*ast.Ident{ident("x")}, Type: typ("int")}}
	tests := []struct {
		name string
		node ast.Node
		want string
	}{
		{"raw", &ast.Ident{Name: "match", Raw: true}, "r#match"},
		{"Go struct", &ast.TypeDecl{Name: ident("S"), Type: &ast.StructType{Fields: fields}}, "type S struct {\nx int;\n}"},
		{"struct sugar", &ast.TypeDecl{Name: ident("S"), Type: &ast.StructType{Fields: fields}, Form: ast.StructSugar}, "struct S {\nx int;\n}"},
		{"short closure", &ast.FuncLit{Form: ast.Bar, Params: []*ast.Param{{Names: []*ast.Ident{ident("yield")}}}, Expr: ident("yield")}, "|yield| yield"},
		{"Go closure", &ast.FuncLit{Params: []*ast.Param{{Names: []*ast.Ident{ident("where")}, Type: typ("int")}}, Results: []*ast.Param{{Type: typ("int")}}, Body: &ast.Block{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{ident("where")}}}}}, "func(where int) int {\nreturn where;\n}"},
		{"parenthesized result", &ast.FuncType{Results: []*ast.Param{{Type: typ("int")}}, ResultParen: true}, "func() (int)"},
		{"qualified method", &ast.QualifiedSelectorExpr{X: ident("x"), Sel: ident("m"), Trait: typ("Trait")}, "x.m::<Trait>"},
		{"mutable reference", &ast.RefExpr{Mut: true, X: ident("x")}, "&mut x"},
		{"propagation", &ast.TryExpr{X: &ast.CallExpr{Fun: ident("f")}}, "f()?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := printer.Source(tt.node); got != tt.want {
				t.Fatalf("got %q; want %q", got, tt.want)
			}
		})
	}
}

func TestTokenTrees(t *testing.T) {
	tree := &ast.MacroExpr{Path: &ast.Path{Segments: []*ast.PathSegment{{Name: ident("m")}}}, Args: &ast.TokenTree{Delim: ast.Paren, Tokens: []ast.TokenOrTree{
		{Token: &token.Token{Kind: token.DOLLAR, Text: "$"}},
		{Tree: &ast.TokenTree{Delim: ast.Bracket, Tokens: []ast.TokenOrTree{{Token: &token.Token{Kind: token.IDENT, Text: "match", Raw: true}}}}},
		{Token: &token.Token{Kind: token.SEMICOLON, Text: "\n", Implicit: true}},
	}}}
	got := printer.Source(tree)
	if !strings.Contains(got, "m!(") || !strings.Contains(got, "[r#match]") || !strings.Contains(got, "\n") {
		t.Fatalf("token tree or raw syntax lost: %q", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestFprintError(t *testing.T) {
	if err := printer.Fprint(failingWriter{}, ident("x")); err == nil {
		t.Fatal("writer error hidden")
	}
}
func TestSourceNil(t *testing.T) {
	var n *ast.BadExpr
	if got := printer.Source(n); got != "" {
		t.Fatalf("typed nil printed %q", got)
	}
}

func TestPrinterUsesTree(t *testing.T) {
	file, ds := parser.ParseFile("mutation.gox", []byte("package p; func F() int { return 1 }"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	fn := file.Items[0].(*ast.FuncDecl)
	fn.Name.Name = "Changed"
	fn.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.BasicLit).Value = "42"
	out := printer.Source(file)
	if !strings.Contains(out, "func Changed()") || !strings.Contains(out, "return 42") || strings.Contains(out, "func F()") {
		t.Fatalf("printer did not use the tree: %s", out)
	}
	if _, ds := parser.ParseFile("printed.gox", []byte(out)); len(ds) != 0 {
		t.Fatal(ds)
	}
}
func TestCommentsAndDirectives(t *testing.T) {
	src := `//go:build example

package p

// Unattached explanation.

//goxide:check strict
// S contains one field.
type S struct {
 // X is a field.
 X int
}

func F() {
 // Statement comment.
 f()
 // Tail comment.
}
`
	file, ds := parser.ParseFile("comments.gox", []byte(src))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	output := printer.Source(file)
	next, ds := parser.ParseFile("printed.gox", []byte(output))
	if len(ds) != 0 {
		t.Fatalf("%v\n%s", ds, output)
	}
	if len(file.Comments) != len(next.Comments) || len(file.Directives) != len(next.Directives) {
		t.Fatalf("comment groups or directives lost\n%s", output)
	}
	for i, g := range file.Comments {
		if len(g.List) != len(next.Comments[i].List) {
			t.Fatalf("group %d changed\n%s", i, output)
		}
		for j, c := range g.List {
			if c.Text != next.Comments[i].List[j].Text {
				t.Fatal("comment text changed")
			}
		}
	}
	decl := next.Items[0].(*ast.TypeDecl)
	if decl.Doc == nil || len(decl.Directives) != 1 || decl.Type.(*ast.StructType).Fields[0].Doc == nil {
		t.Fatalf("documentation attachments lost\n%s", output)
	}
}

func TestTokenTreeMacroAdjacency(t *testing.T) {
	source := `package p
macro_rules! outer { () => { inner!(x); pkg::other![y] }; }
`
	file, ds := parser.ParseFile("macro.gox", []byte(source))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	out := printer.Source(file)
	if !strings.Contains(out, "inner!(x)") || !strings.Contains(out, "pkg::other![y]") {
		t.Fatalf("nested macro adjacency lost: %s", out)
	}
	if _, ds := parser.ParseFile("printed.gox", []byte(out)); len(ds) != 0 {
		t.Fatal(ds)
	}
}
func TestAdjacentReferenceTokens(t *testing.T) {
	for _, src := range []string{"& &x", "& &mut x"} {
		tree, ds := parser.ParseExpr([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		out := printer.Source(tree)
		if _, ds := parser.ParseExpr([]byte(out)); len(ds) != 0 {
			t.Fatalf("%q became %q: %v", src, out, ds)
		}
	}
	tree, ds := parser.ParseType([]byte("& &T"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if out := printer.Source(tree); strings.Contains(out, "&&") {
		t.Fatalf("reference tokens merged: %s", out)
	}
}

func TestInlineBlockComments(t *testing.T) {
	source := `package p
func F(x /* parameter */ int) { f(/* first */ x); x = x /* plus */ + 1 }
`
	file, ds := parser.ParseFile("comments.gox", []byte(source))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	out := printer.Source(file)
	if _, ds := parser.ParseFile("printed.gox", []byte(out)); len(ds) != 0 {
		t.Fatalf("%v\n%s", ds, out)
	}
}

func TestConstructedExpressionPrecedence(t *testing.T) {
	tree := &ast.BinaryExpr{X: ident("a"), Op: token.MUL, Y: &ast.BinaryExpr{X: ident("b"), Op: token.ADD, Y: ident("c")}}
	if got := printer.Source(tree); got != "a * (b + c)" {
		t.Fatalf("precedence lost: %s", got)
	}
	call := &ast.CallExpr{Fun: &ast.StarExpr{X: ident("f")}}
	if got := printer.Source(call); got != "(*f)()" {
		t.Fatalf("callee precedence lost: %s", got)
	}
}

func TestLabeledEmptyStatements(t *testing.T) {
	for _, src := range []string{"package p; func F() { Label: }", "package p; func F() { Label: ; }"} {
		file, ds := parser.ParseFile("label.gox", []byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		out := printer.Source(file)
		next, ds := parser.ParseFile("printed.gox", []byte(out))
		if len(ds) != 0 {
			t.Fatalf("%v\n%s", ds, out)
		}
		original := file.Items[0].(*ast.FuncDecl).Body.List
		printed := next.Items[0].(*ast.FuncDecl).Body.List
		if len(original) != len(printed) {
			t.Fatalf("extra empty statement: %s", out)
		}
		before := original[0].(*ast.LabeledStmt).Stmt.(*ast.EmptyStmt)
		after := printed[0].(*ast.LabeledStmt).Stmt.(*ast.EmptyStmt)
		if before.Implicit != after.Implicit {
			t.Fatalf("implicit empty form changed: %s", out)
		}
	}
}

func TestGenericConstraintDisambiguation(t *testing.T) {
	source := `package p
type Constrained[P *C,] []P
func f[T any](x T) T where T: any { return x }
`
	file, ds := parser.ParseFile("generic.gox", []byte(source))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	out := printer.Source(file)
	if !strings.Contains(out, "[P *C,]") || !strings.Contains(out, "where T: any") {
		t.Fatalf("constraint disambiguation lost: %s", out)
	}
	next, ds := parser.ParseFile("printed.gox", []byte(out))
	if len(ds) != 0 {
		t.Fatalf("%v\n%s", ds, out)
	}
	if next.Items[0].(*ast.TypeDecl).Generics == nil {
		t.Fatalf("generic parsed as array: %s", out)
	}
}
