package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func requireClean(t *testing.T, diagnostics []Diagnostic) {
	t.Helper()
	for _, d := range diagnostics {
		if d.Severity == Error {
			t.Fatalf("unexpected diagnostic: %v", d)
		}
	}
}
func TestExpressionAmbiguities(t *testing.T) {
	cases := []struct{ source, want string }{
		{"match x { Some(v) => v, None => 0 }", "*ast.MatchExpr"},
		{"match (T{}) { _ => 1 }", "*ast.MatchExpr"},
		{"Result[int, error]::Ok(v)", "*ast.CallExpr"},
		{"values[index]", "*ast.IndexExpr"},
		{"|x| x + 1", "*ast.FuncLit"},
		{"a | b", "*ast.BinaryExpr"},
		{"&mut x", "*ast.RefExpr"},
		{"&mut", "*ast.RefExpr"},
		{"name!(x, [unparsed +], {$value})", "*ast.MacroExpr"},
		{"a != b", "*ast.BinaryExpr"},
		{"x.m::<Trait>(args)", "*ast.CallExpr"},
		{"x.m::<Trait[T]]", ""},
		{"x.m::<Trait[T]>", "*ast.QualifiedSelectorExpr"},
		{"Trait::m(x)", "*ast.CallExpr"},
		{"Self::new()", "*ast.CallExpr"},
		{"f[<-chan int]::x", "*ast.PathExpr"},
		{"Result[Item=int]::new()", "*ast.CallExpr"},
		{"pkg.T[int]{}", "*ast.CompositeLit"},
		{"super{}", "*ast.CompositeLit"},
		{"super!()", "*ast.MacroExpr"},
		{"f()?.field[1:2:3]", "*ast.SliceExpr"},
		{"[]T{{x: 1}, {x: 2}}", "*ast.CompositeLit"},
		{"func(x, y int) (int, error) { return x+y,nil }", "*ast.FuncLit"},
		{"unsafe f(x)", "*ast.UnsafeExpr"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			n, ds := ParseExpr([]byte(tc.source))
			if tc.want == "" {
				if len(ds) == 0 {
					t.Fatal("expected syntax error")
				}
				return
			}
			requireClean(t, ds)
			if got := fmt.Sprintf("%T", n); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	x, ds := ParseExpr([]byte("Result[int,error]::Ok"))
	requireClean(t, ds)
	path := x.(*ast.PathExpr).Path
	if len(path.Segments) != 2 || len(path.Segments[0].Args.Args) != 2 {
		t.Fatal("generic index was not reinterpreted as path")
	}
	for _, source := range []string{"&mut x", "&mut"} {
		x, ds := ParseExpr([]byte(source))
		requireClean(t, ds)
		if x.(*ast.RefExpr).Mut != (source == "&mut x") {
			t.Fatalf("wrong mut interpretation for %s", source)
		}
	}
	for _, source := range []string{"name !()", "name! ()"} {
		_, ds := ParseExpr([]byte(source))
		if len(ds) == 0 {
			t.Fatalf("macro requires adjacency: %s", source)
		}
	}
}
func TestPatterns(t *testing.T) {
	cases := []struct{ src, want string }{
		{"_", "*ast.WildPat"}, {"None", "*ast.BindPat"}, {"ref mut value", "*ast.BindPat"},
		{"Result[T,E]::Ok(ref value)", "*ast.TuplePat"}, {"A | B", "*ast.OrPat"},
		{"-1", "*ast.LitPat"}, {"true", "*ast.LitPat"}, {"nil", "*ast.LitPat"}, {"E::Unit", "*ast.PathPat"},
		{"r#match", "*ast.BindPat"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			n, ds := ParsePattern([]byte(tc.src))
			requireClean(t, ds)
			if fmt.Sprintf("%T", n) != tc.want {
				t.Fatalf("unexpected node %T", n)
			}
		})
	}
	n, ds := ParsePattern([]byte("@"))
	if len(ds) == 0 {
		t.Fatal("expected error")
	}
	if _, ok := n.(*ast.BadPat); !ok {
		t.Fatalf("expected BadPat got %T", n)
	}
}
func TestTypesAndSignatures(t *testing.T) {
	for _, src := range []string{
		"int", "r#type", "pkg::T[A,B]", "Iterator[Item = int]", "T::Iterator::Item", "pkg.T",
		"&T", "&mut T", "*T", "[]T", "[3]int", "[...]int", "map[string][]int", "chan<- int", "<-chan int",
		"func(x, y int, rest ...string) (where int, yield error)",
		"struct { x, y int; pub z string `json:\"z\"`; *T; pkg.T }",
		"interface { Read(p []byte) (n int, err error); ~int | ~string; io.Reader }",
		"(T)", "m!([$token])", "pkg.T[int]", "Self::Item", "func(x,y []int)",
	} {
		t.Run(src, func(t *testing.T) { _, ds := ParseType([]byte(src)); requireClean(t, ds) })
	}
	n, ds := ParseType([]byte("func(x, y, z int)"))
	requireClean(t, ds)
	names := n.(*ast.FuncType).Params[0].Names
	if len(names) != 3 || names[0].Name != "x" || names[1].Name != "y" || names[2].Name != "z" {
		t.Fatalf("grouped names out of order: %#v", names)
	}
	n, ds = ParseType([]byte("@"))
	if len(ds) == 0 {
		t.Fatal("expected error")
	}
	if _, ok := n.(*ast.BadType); !ok {
		t.Fatalf("expected BadType got %T", n)
	}
}
func TestContextualNames(t *testing.T) {
	src := `package p
func (self *T) M(where int, yield func(int) bool, mut int, ref int, as int, super int, in int, macro_rules int) {
 self.x = where
 yield(mut)
}
func r#match(r#mod int) { r#use := r#mod; _ = r#use }
`
	_, ds := ParseFile("names.gox", []byte(src))
	requireClean(t, ds)
	_, ds = ParseFile("future.gox", []byte("package p; func f(async int) { await := async; _ = await }"))
	warnings := 0
	for _, d := range ds {
		if d.Severity == Warning {
			warnings++
		}
	}
	if warnings < 2 {
		t.Fatalf("wanted future keyword warnings, got %v", ds)
	}
	_, ds = ParseFile("raw.gox", []byte("package p; func f(r#async int) { _ = r#async }"))
	if len(ds) != 0 {
		t.Fatalf("raw names should suppress warning: %v", ds)
	}
}
func TestMatchRecoveryAndLastSemicolon(t *testing.T) {
	src := "match x {\n A => 1,\n B => 2\n}"
	n, ds := ParseExpr([]byte(src))
	requireClean(t, ds)
	if len(n.(*ast.MatchExpr).Arms) != 2 {
		t.Fatal("lost arms")
	}
	p := newParser([]byte("match x { @ => 1, B => 2 }"))
	n = p.expr()
	ds = p.diagnostics
	if len(ds) == 0 {
		t.Fatal("expected pattern error")
	}
	arms := n.(*ast.MatchExpr).Arms
	if len(arms) != 2 {
		t.Fatal("recovery lost later arm")
	}
	if _, ok := arms[0].Pat.(*ast.BadPat); !ok {
		t.Fatalf("expected BadPat got %T", arms[0].Pat)
	}
	for _, src := range []string{"match x { A => 1; B => 2 }", "match x { A => 1; }"} {
		_, ds = ParseExpr([]byte(src))
		if len(ds) == 0 {
			t.Fatal("only an inserted semicolon may terminate the last match arm")
		}
	}
}
func TestAttributeAndMacroTrees(t *testing.T) {
	p := newParser([]byte("#[default]\n#[cfg(any(a, [b], {c}))]\nfunc"))
	attrs := p.attrs()
	requireClean(t, p.diagnostics)
	if len(attrs) != 2 || attrs[0].Path.Segments[0].Name.Name != "default" || attrs[1].Args.Delim != ast.Paren {
		t.Fatal("wrong attributes")
	}
	if !p.at(token.FUNC) {
		t.Fatal("attribute consumed following declaration")
	}
	p = newParser([]byte("#[cfg(])"))
	_ = p.attrs()
	if len(p.diagnostics) == 0 {
		t.Fatal("mismatched token tree was accepted")
	}
	_, ds := ParseType([]byte("super::T"))
	found := false
	for _, d := range ds {
		found = found || strings.Contains(d.Message, "not supported")
	}
	if !found {
		t.Fatal("super:: must diagnose reserved root")
	}
}

func TestBarResultAndCompositeBody(t *testing.T) {
	n, ds := ParseExpr([]byte("|x| T{}"))
	requireClean(t, ds)
	f := n.(*ast.FuncLit)
	if _, ok := f.Expr.(*ast.CompositeLit); !ok {
		t.Fatalf("wanted composite body, got %#v", f)
	}
	n, ds = ParseExpr([]byte("|x int| int {return x}"))
	requireClean(t, ds)
	f = n.(*ast.FuncLit)
	if f.Result == nil || f.Body == nil || f.Expr != nil {
		t.Fatalf("wanted annotated block body, got %#v", f)
	}
}
func TestMalformedFragmentsTerminate(t *testing.T) {
	atoms := []string{"(", ")", "[", "]", "{", "}", "@", "?", "::", "=>", "|", "&", "func", "match", "Self", "r#match", "T", "1", ",", ";", "#["}
	for i, a := range atoms {
		for j, b := range atoms {
			src := a + " " + b + " " + atoms[(i*7+j*3)%len(atoms)]
			t.Run(fmt.Sprintf("%d_%d", i, j), func(t *testing.T) {
				p := newParser([]byte(src))
				_ = p.expr()
				if p.pos > len(p.tokens) {
					t.Fatal("expression consumed beyond EOF")
				}
				p = newParser([]byte(src))
				_ = p.typ()
				if p.pos > len(p.tokens) {
					t.Fatal("type consumed beyond EOF")
				}
				p = newParser([]byte(src))
				_ = p.pattern()
				if p.pos > len(p.tokens) {
					t.Fatal("pattern consumed beyond EOF")
				}
			})
		}
	}
}

func TestWhereRequiresColonAndAttributeBalance(t *testing.T) {
	for _, src := range []string{"package p; func F[T any]() where T any {}", "package p; func F[T any]() where {}", "package p; #[foo )] func F() {}"} {
		_, ds := ParseFile("invalid.gox", []byte(src))
		if !hasNewErrors(ds, 0) {
			t.Fatalf("expected syntax error for %s", src)
		}
	}
}

func TestWhereAsOrdinaryResultType(t *testing.T) {
	_, ds := ParseFile("where.gox", []byte("package p; type where int; func f() where {return 1}; func generic[T any]() r#where {return 1}"))
	requireClean(t, ds)
	_, ds = ParseExpr([]byte("func() where {return 1}"))
	requireClean(t, ds)
	_, ds = ParseType([]byte("func() where"))
	requireClean(t, ds)
}
