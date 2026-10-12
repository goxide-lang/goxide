package parser_test

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func TestGoDeclarationsAndStatements(t *testing.T) {
	for name, body := range map[string]string{
		"declarations": `
const (A = iota; B; C int = 3)
var (a, b int = 1, 2; c = make(chan<- int))
type (Alias = int; Vec[T any] []T; Point struct { X, Y int; *Other; pkg.T; Tag string ` + "`json:\"tag\"`" + ` })
func (self *Point) M(where, yield int, rest ...string) (x int, err error) { return 1, nil }
func noBody()
type Slice []int
type Array [256]bool
type NamedSize [N]int
type Product [P*C]int
type Constrained[P *C,] []P
`,
		"control": `
func f(ch chan int, value any) {
 ;
 var x int
 const y = 1
 type local = int
 x = y
 x++
 x--
 ch <- x
 go f(ch, value)
 defer f(ch, value)
 if x := 1; x > 0 { x += 1 } else if ; true {} else {}
 for {}
 for x < 4 { break }
 for ; ; { continue }
 for i := 0; i < 2; i++ { _ = i }
 for range 2 {}
 for k, v := range map[string]int{"x": 1} { _, _ = k, v }
 for x = range ch {}
 switch x := 1; x { case 1, 2: x++; fallthrough; default: x-- }
 switch { case true: }
 switch v := value.(type) { case int, *Point: _ = v; case nil: default: }
 switch x := value; x.(type) { case interface { M() }: }
 select { case v, ok := <-ch: _, _ = v, ok; case ch <- 1: default: }
 goto Done
Done:
 return
}
`,
		"expressions": `
func expressions(xs []int, a any) {
 _ = xs[:]
 _ = xs[1:2:3]
 _ = a.(interface { M() })
 _ = (...)
}
`,
	} {
		if name == "expressions" {
			body = strings.ReplaceAll(body, "_ = (...)", `_ = [2]struct { X int }{{1}, {X: 2}}
 _ = func(x int) (int, error) { return x, nil }(1)
 _ = map[[2]int][]int{{1, 2}: {3, 4}}
 _ = []int{1, 2}[:]
 _ = a.(*Point)
 _ = append(xs, xs...)
 _ = (chan int)(nil)`)
		}
		t.Run(name, func(t *testing.T) {
			src := []byte("package p\n" + body)
			f, ds := parser.ParseFile(name, src)
			if len(errorsOnly(ds)) > 0 || len(badNodes(f)) > 0 {
				t.Fatalf("%s\n%v", diagnostics(src, ds), badNodes(f))
			}
			assertRoundTrip(t, f)
		})
	}
}

func TestExtensionDeclarations(t *testing.T) {
	src := []byte(`package p
use r#match as local
pub use a::b::*;
mod child
#[derive(Clone)]
pub struct Point[T] { #[tag(x)] pub X T; Y int }
pub enum E[T] { A, #[default] B(T), }
trait Iter[T] { type Item; unsafe func Next() Self; func Value() int { return 1 } }
impl[T] Iter[T] for Point[T] { type Item = T; func Next() Self { return Self{} } }
impl Point[int] { pub func Read() int { return self.X } }
impl Empty {}
unsafe func raw[T any](x T) T where T: any { return x }
declare func native::Read(p *int) int
declare impl Iter for Native
declare type Native
pub macro_rules! make { ($x:expr) => { $x }; }
make!(type Generated int)
`)
	f, ds := parser.ParseFile("extensions.gox", src)
	if len(errorsOnly(ds)) > 0 || len(badNodes(f)) > 0 {
		t.Fatalf("%s\n%v", diagnostics(src, ds), badNodes(f))
	}
	assertRoundTrip(t, f)
}

func TestReservedAndContextualIdentifiers(t *testing.T) {
	src := []byte(`package r#mod
func (self *T) M(where, yield, mut, ref, as, super, in, macro_rules int) {
 var async int
 var r#await, r#Self int
 _ = async + r#await + r#Self
}
`)
	f, ds := parser.ParseFile("names.gox", src)
	if len(errorsOnly(ds)) > 0 {
		t.Fatal(ds)
	}
	warnings := 0
	for _, d := range ds {
		if d.Severity == parser.Warning {
			warnings++
		}
	}
	if warnings != 2 {
		t.Fatalf("want warnings at both async uses, got %v", ds)
	}
	assertRoundTrip(t, f)
	ids := map[ast.NodeID]bool{}
	ast.Walk(f, func(n ast.Node) bool {
		if n.ID() == 0 || ids[n.ID()] {
			t.Errorf("invalid/duplicate node id %d for %T", n.ID(), n)
		}
		ids[n.ID()] = true
		if n.Span().End < n.Span().Start || int(n.Span().End) > len(src) {
			t.Errorf("invalid span %v", n.Span())
		}
		if i, ok := n.(*ast.Ident); ok && i.Ctx != 0 {
			t.Error("source identifier has nonzero context")
		}
		return true
	})
	for i := 1; i <= len(ids); i++ {
		if !ids[ast.NodeID(i)] {
			t.Fatalf("node IDs are not dense: missing %d", i)
		}
	}
}

func TestRecoveryRetainsLaterDeclarations(t *testing.T) {
	src := []byte(`package p
import "fmt"
type match int
func broken() { x := ; good(); return }
trait T { missing() int; func Good() int }
func healthy(where, yield int) int { return where + yield }
`)
	f, ds := parser.ParseFile("broken.gox", src)
	if len(errorsOnly(ds)) == 0 || len(badNodes(f)) == 0 {
		t.Fatal("missing error diagnostics/nodes")
	}
	found := false
	ast.Walk(f, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "healthy" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("recovery lost later declaration")
	}
}

func TestReservedSyntaxRejected(t *testing.T) {
	for _, src := range []string{
		`package p; trait T { missing() int }`,
		`package p; impl T { missing() int {} }`,
		`package p; mod m {}`,
		`package p; use a::{b,c}`,
		`package p; use super::x`,
		`package p; enum E { A { x int }, }`,
		`package p; enum E { A = 1, }`,
		`package p; #![cfg(x)] func f() {}`,
		`package p; pub(scope) func f() {}`,
		`package p; func f() { let x = 1 }`,
		`package p; use a*`,
		`package p; use super::*`,
		`package p; const X`,
		`package p; impl T { type Item }`,
		`package p; impl T { unsafe type Item = int }`,
		`package p; trait T { unsafe type Item }`,
		`package p; func f[T any]() where {}`,
	} {
		t.Run(src, func(t *testing.T) {
			f, ds := parser.ParseFile("reserved", []byte(src))
			if len(errorsOnly(ds)) == 0 || len(badNodes(f)) == 0 {
				t.Fatalf("must reject with Bad node: %v %v", ds, badNodes(f))
			}
		})
	}
}
