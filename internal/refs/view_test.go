package refs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func TestReferenceSyntaxViewPreservesOffsetsAndHostExpressions(t *testing.T) {
	for _, source := range []string{
		"func f(r &T, w &mut T) int ",
		"func (r &mut T) f(v func(&T) int) &mut T ",
		"func f(v [mut & 3]int, r &mut T) int ",
	} {
		view := GoView([]byte(source))
		if len(view) != len(source) {
			t.Fatal("parser view changed offsets")
		}
		if _, err := parser.ParseFile(token.NewFileSet(), "view.go", "package p\n"+string(view)+"{}", 0); err != nil {
			t.Fatalf("%s: %v\n%s", source, err, view)
		}
		if strings.Contains(source, "mut & 3") && !strings.Contains(string(view), "mut & 3") {
			t.Fatal("binary AND rewritten")
		}
	}
	for _, source := range []string{`a & mut`, `mut&x`, `"&mut r"`, "`&mut r`"} {
		if got := string(GoView([]byte(source))); got != source {
			t.Fatalf("host expression changed: %q => %q", source, got)
		}
	}
}

func TestWritableReferencePositionSyntax(t *testing.T) {
	source := `package p
func f(mut int, x int)int{n:=1;p:=&/*write*/mut n;*p=3;return n+(mut&x)+(x & mut)}
func create() &mut int{n:=1;return &mut(n)}
`
	if _, err := Lower("syntax.gox", []byte(source)); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		`package p;func f(p mut& int){}`,
		`package p;func read(p &int){};func f(){n:=1;p:=mut&n;_=p}`,
		`package p;func f(){n:=1;p:=&mut;_=n;_=p}`,
	} {
		if _, err := Lower("old.gox", []byte(source)); err == nil {
			t.Fatalf("accepted old/incomplete reference: %s", source)
		}
	}
}

func TestPreparedReferenceEvidenceNeverAdoptsRawPointer(t *testing.T) {
	prepared, err := Prepare("prepared.gox", []byte(`package p;type E struct{N int};func f(e E,r &E,raw *E){w:=&mut e;_=w;_=r;_=raw;_=&e}`))
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "prepared.go", prepared.Source, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{}).Check("example/p", fs, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || info.Uses[id] == nil {
			return true
		}
		if id.Name != "w" && id.Name != "r" && id.Name != "raw" {
			return true
		}
		_, permission, accepted := PreparedReferenceElement(id, info, pkg)
		if id.Name == "raw" {
			if accepted {
				t.Fatal("native pointer adopted")
			}
		} else {
			want := Read
			if id.Name == "w" {
				want = Write
			}
			if !accepted || permission != want {
				t.Fatalf("lost %s reference", id.Name)
			}
		}
		found[id.Name] = true
		return true
	})
	if len(found) != 3 {
		t.Fatalf("incomplete evidence probe: %v", found)
	}
}
