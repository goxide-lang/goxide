package protocols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestAssociatedDeclarationsAndGenericBounds(t *testing.T) {
	source := []byte(`package fixture
type ( User struct { Name string }; Other struct{} )
pub trait Build { func Abc() int; func Create(name string) Self }
impl Build for User { func Abc() int { return 7 };func Create(name string) Self { return Self{Name:name} } }
impl Build for Other { func Abc() int { return 9 };func Create(name string) Self { return Self{} } }
func Pair[T Build, U Build](a T,b U) int { return 1 }
`)
	p, err := CollectPackage("example.org/fixture", map[string][]byte{"main.gox": source}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Definitions) != 1 || len(p.Implementations) != 2 || len(p.GenericBindings["main.gox"]["Pair"]) != 2 {
		t.Fatalf("incomplete registry: %#v", p)
	}
	if len(p.Functions[0].SourceParams) != 2 || len(p.Functions[0].HiddenParams) != 2 {
		t.Fatal(p.Functions)
	}
	for _, receiver := range []string{"User", "Other", "T", "U"} {
		a, ok, err := p.LookupAssociated("main.gox", "Pair", receiver, "Create")
		if err != nil || !ok || a.Member.Name != "Create" {
			t.Fatalf("%s: %#v %v", receiver, a, err)
		}
	}
	m := sourcemap.Identity("main.gox", source)
	r, err := p.LowerFile("main.gox", source, m)
	if err != nil {
		t.Fatal(err)
	}
	if !r.GenericFunctions["Pair"] || len(r.AnchorTargets) != 2 {
		t.Fatalf("incomplete lowering evidence: %#v", r)
	}
	out := append(append([]byte(nil), r.Source...), r.LateDeclarations...)
	f, err := parser.ParseFile(token.NewFileSet(), "generated.go", out, 0)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "Pair" && fn.Type.TypeParams.NumFields() != 4 {
			t.Fatalf("hidden carrier parameters missing: %#v", fn.Type.TypeParams)
		}
	}
	if strings.Contains(string(r.Source), "return Self") || !strings.Contains(string(r.Source), "return User{Name:name}") {
		t.Fatalf("Self was not bound to target:\n%s", r.Source)
	}
	// Declaration collection and final lowering can reparse a shifted view.
	shifted := append([]byte("\n\n"), source...)
	if _, err := p.LowerFile("main.gox", shifted, sourcemap.Identity("main.gox", shifted)); err != nil {
		t.Fatal(err)
	}
}

func TestSelfBindingDoesNotRenameValuesOrFields(t *testing.T) {
	source := []byte(`package fixture
type User struct{Self int}
trait Build {func Abc(Self int) int;func Make()Self}
impl Build for User {
 func Abc(Self int)int{return Self}
 func Make()Self{return Self{Self:4}}
}
`)
	p, e := CollectPackage("p", map[string][]byte{"main.gox": source}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	r, e := p.LowerFile("main.gox", source, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"Abc(Self int)int{return Self}", "Make()User{return User{Self:4}}"} {
		if !strings.Contains(string(r.Source), want) {
			t.Fatalf("missing %q:\n%s", want, r.Source)
		}
	}
	if !strings.Contains(string(r.LateDeclarations), "Abc(Self int) int") {
		t.Fatalf("parameter name became a type substitution: %s", r.LateDeclarations)
	}
}

func TestNativeDeclarationsAndBoundBuiltinNames(t *testing.T) {
	sources := map[string][]byte{
		"native.go": []byte("package p; var trait=1;func impl(){};type User struct{};type Reader interface{Read([]byte)(int,error)};func Native[T any](x T)T{return x}"),
		"impl.gox":  []byte("package p; trait Default{func Abc()Self};impl Default for User{func Abc()Self{return Self{}}};func F[T Default](v T)T{return v}"),
	}
	p, e := CollectPackage("p", sources, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Implementations) != 1 || len(p.Functions) != 1 || p.Functions[0].Name != "F" {
		t.Fatalf("native function or bound protocol lost: %#v", p)
	}
	r, e := p.LowerFile("impl.gox", sources["impl.gox"], nil)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(r.Source), "impl Default") || !strings.Contains(string(r.Source), AnchorName) {
		t.Fatalf("bound Default was routed by bare name: %s", r.Source)
	}
	p, e = CollectPackage("p", map[string][]byte{"single.go": append([]byte("package p;type User struct{};"), sources["impl.gox"][len("package p;"):]...)}, Options{Single: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Definitions) != 1 {
		t.Fatal("single display extension changed source language")
	}
}

func TestProtocolIdentityAndVisibility(t *testing.T) {
	source := []byte("package p;type User struct{};trait P{func Abc()Self};pub trait Q{func Abc()Self};impl P for User{func Abc()Self{return Self{}}};func F[T P](v T)T{return v}")
	p, e := CollectPackage("p", map[string][]byte{"a.gox": source}, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if p.Definitions[0].MarkerName == p.Definitions[1].MarkerName {
		t.Fatal("distinct protocols share structural evidence")
	}
	if len(p.SemanticMetadata().Implementations) != 1 || len(p.ExportMetadata().Implementations) != 1 {
		t.Fatal("public target protocol evidence lost")
	}
	if p.Definitions[0].Public || !p.Definitions[0].ExportedEvidence {
		t.Fatal("source-private protocol visibility changed instead of exporting only its evidence")
	}
	if p.Definitions[1].OpsName != OpsName(ID{"p", "Q"}, true) {
		t.Fatal("unstable canonical ABI name")
	}
}

func TestImportedAssociatedLookupIsFileScoped(t *testing.T) {
	a := Associated{File: "a.gox", Receiver: "m::User", Member: Member{Name: "Abc"}, Target: ID{"a", "User"}}
	p, e := CollectPackage("p", map[string][]byte{}, Options{Associated: []Associated{a}})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok, _ := p.LookupAssociated("b.gox", "", "m::User", "Abc"); ok {
		t.Fatal("import route leaked into sibling file")
	}
	if _, ok, _ := p.LookupAssociated("a.gox", "", "m::User", "Abc"); !ok {
		t.Fatal("explicit file binding missing")
	}
}
