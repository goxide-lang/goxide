package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnumDeclarationSeparators(t *testing.T) {
	// The formerly rejected expression match is now a real execution regression.
	enumGeneratedModule(t, `package fixture
 type Record struct{N int}; const offset=2
 pub enum E { A, }; func expression(x E) int {return match x {E::A=>1,}}
 pub enum F { B(int), } /* separator comment */ ; enum G { C, }; // trailing comment
 func statement(x F)int{match x{F::B(n)=>{return n+offset},};panic("unreachable")}
 pub enum H { D, } // inserted newline separator
 func newline()H{return H::D}
 func newlineValid()int{return match newline(){H::D=>1,}}
 pub enum Last { End, };`, `package fixture
 import "testing"
 func TestSeparators(t *testing.T){if expression(&A{})!=1||statement(&B{Value:3})!=5||newlineValid()!=1{t.Fatal("separator changed behavior")}}
 `, false)
}

func TestEnumSeparatorPackageCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", packagesModule())
	writeAcceptanceFixtureRoots(t, src, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
	packagesWrite(t, src, "model/types.gox", `package model
pub enum E { A, B(int), } /* preserved between */ ; // preserved after
func Read(e E)int{return match e{E::A=>0,E::B(n)=>n,}}
`)
	packagesWrite(t, src, "model/other.gox", `package model
type Record struct{N int}; enum local { Ready, }; func Ready()E{return E::B(4)}
`)
	packagesWrite(t, src, "app.gox", `package app
use model as m;
func Run()int{return m::Read(m::Ready())}
`)
	out := filepath.Join(t.TempDir(), "out")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, src)
	generated, e := os.ReadFile(filepath.Join(out, "model/types.go"))
	if e != nil {
		t.Fatal(e)
	}
	for _, comment := range []string{"preserved between", "preserved after"} {
		if !strings.Contains(string(generated), comment) {
			t.Fatalf("lost comment: %s", generated)
		}
	}
	packagesWrite(t, out, "app_test.go", `package app
import "testing"
func TestGenerated(t *testing.T){if Run()!=4{t.Fatal(Run())}}
`)
	packagesCommand(t, out, tool, true, "test", "-race", "-count=1", "./...")
	packagesCommand(t, out, tool, true, "vet", "./...")
	for _, src := range []string{
		"package fixture\nenum E { A, }; func bad(e E){match e{E::Missing=>{},}}",
		"package fixture\nenum E { A, }; func bad(e E){match e{E::A if true=>{},}}",
	} {
		_, e := compileAcceptanceLanguageFixture("separator_bad.gox", []byte(src))
		if e == nil || !strings.Contains(e.Error(), "separator_bad.gox:") || strings.Contains(e.Error(), "expected declaration") {
			t.Fatalf("expected enum diagnostic with source position, got %v", e)
		}
	}
}
