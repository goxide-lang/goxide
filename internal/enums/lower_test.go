package enums

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGeneratedExecution(t *testing.T) {
	source := `package main
import "fmt"
enum Inner { Value(int), Empty, }
enum Event { Idle, Item(Inner), Failed(string), }
var calls int
func load() Event { calls++; return Event::Item(Inner::Value(7)) }
func run() (out string) {
 defer func(){ out += "D" }()
 match load() {
 Event::Item(v) if false => { panic(v) },
 _ if calls == 2 => { panic("wrong guard") },
 Event::Item(v) => {
 match v { Inner::Value(n) => { return fmt.Sprint(n) }, Inner::Empty => { return "empty" } }
 },
 Event::Idle => { return "idle" },
 Event::Failed(e) => { return e },
 }
 return "bad"
}
func main() {
 if got:=run();got!="7D"||calls!=1 {panic(got)}
 count:=0
 for i:=0;i<3;i++ {
 match Event::Idle { Event::Idle => { count++; break }, Event::Item(_) => {}, Event::Failed(_) => {} }
 count+=100
 }
 if count!=1 {panic(count)}
 for i:=0;i<3;i++ {
 match Event::Idle { Event::Idle => { count++; continue }, Event::Item(_) => {}, Event::Failed(_) => {} }
 count+=100
 }
 if count!=4 {panic(count)}
 fmt.Print("ok")
}
`
	r, e := Lower("demo.hgo", []byte(source))
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Enums) != 2 || r.Enums[1].Variants[1].Type == nil {
		t.Fatal("missing AST")
	}
	dir := t.TempDir()
	if e = testdeps.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600); e != nil {
		t.Fatal(e)
	}
	if e = testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module enumtest\ngo 1.27\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(goTool(t), "run", "-race", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s\n%s", e, out, r.Source)
	}
	if string(out) != "ok" {
		t.Fatalf("output %s", out)
	}
}
func goTool(t *testing.T) string {
	t.Helper()
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, e := os.Stat(p); e == nil {
		return p
	}
	t.Fatal("Go executable missing")
	return ""
}
func TestRejectedForms(t *testing.T) {
	for name, src := range map[string]string{
		"generic":              "enum E[T] { A(T), }",
		"empty":                "enum E {}",
		"complex":              "enum E { A(*int), }",
		"recursive":            "enum E { A(E), }",
		"duplicate":            "enum E { A, A, }",
		"constructor":          "enum E { A, }; func f(){ _ = E::B }",
		"arity":                "enum E { A(int), };func f(e E){ match e { E::A => {} } }",
		"guard coverage":       "enum E { A, };func f(e E){ match e { E::A if true => {} } }",
		"unreachable":          "enum E { A, };func f(e E){ match e { E::A => {}, E::A => {} } }",
		"wildcard unreachable": "enum E { A, };func f(e E){ match e { _ => {}, E::A => {} } }",
		"nested":               "enum E { A(int), };func f(e E){ match e { E::A(E::A(_)) => {} } }",
		"expression":           "enum E { A, };func f(e E){ x := match e { E::A => {} }; _=x }",
		"reserved":             "var __hgo_enum_saved_1 int",
	} {
		t.Run(name, func(t *testing.T) {
			_, e := Lower("bad.hgo", []byte("package p\n"+src))
			if e == nil {
				t.Fatalf("accepted %s", src)
			}
			if !strings.Contains(e.Error(), "bad.hgo:") {
				t.Fatal(e)
			}
		})
	}
}
func TestLexicalPreservation(t *testing.T) {
	src := "package p\r\n// enum Fake { X, }\r\nvar s = `enum Fake { X, } match x {}`\r\nenum E { A, }\r\nfunc f(e E) {\r\nmatch e { E::A => { _ = \"} => enum\" } }\r\n}\r\n"
	r, e := Lower("lex.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Enums) != 1 || r.Enums[0].Position.Line != 4 || !strings.Contains(string(r.Source), "`enum Fake { X, } match x {}`") {
		t.Fatalf("%+v", r)
	}
}
func TestInvalidCasesBeforeWildcard(t *testing.T) {
	src := `package p
enum Inner { One(int), }
enum E { A, B(Inner), }
var effects int
func check(e E) {
 match e { E::A if false => { effects++ }, _ => { effects++ } }
}
`
	r, e := Lower("input.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module enumtest\ngo 1.27\n"), 0600)
	testdeps.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600)
	tests := `package p
import "testing"
type forged struct { E }
func TestInvalid(t *testing.T) {
 inputs:=[]E{nil, (*__hgo_enum_case_1_E_A)(nil), &__hgo_enum_case_1_E_A{}, forged{}, __hgo_enum_case_1_E_B{Value:nil}}
 for _,v:=range inputs {func(){defer func(){if recover()==nil {t.Error("accepted invalid")}}();check(v)}()}
 if effects != 0 {t.Fatal("wildcard ran before validation")}
 check(__hgo_enum_case_1_E_A{});if effects!=1{t.Fatal(effects)}
}
`
	testdeps.WriteFile(filepath.Join(dir, "main_test.go"), []byte(tests), 0600)
	cmd := exec.Command(goTool(t), "test", "-race", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v %s\n%s", e, out, r.Source)
	}
}
func TestNominalTypeMismatchCompilesAsError(t *testing.T) {
	r, e := Lower("bad.hgo", []byte("package p\nenum A { X, }\nenum B { X, }\nvar bad A = B::X\n"))
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module enumtest\ngo 1.27\n"), 0600)
	testdeps.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600)
	cmd := exec.Command(goTool(t), "test", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	out, e := cmd.CombinedOutput()
	if e == nil || !strings.Contains(string(out), "missing method") {
		t.Fatalf("expected nominal mismatch: %v %s", e, out)
	}
}
func TestBuiltinShadowRejections(t *testing.T) {
	for _, decl := range []string{
		"func panic(v any) {}",
		"var panic = func(v any) {}",
		"var x, panic = 1, func(v any) {}",
		"var (\n panic = func(v any) {}\n)",
		"type int = []string",
		"type (\n int = []string\n)",
		"import int \"fmt\"",
	} {
		t.Run(decl, func(t *testing.T) {
			_, e := Lower("shadow.hgo", []byte("package p\n"+decl+"\nenum E { A(int), }\n"))
			if e == nil || !strings.Contains(e.Error(), "shadows enum-required builtin") {
				t.Fatalf("expected targeted diagnostic: %v", e)
			}
		})
	}
}

func TestPublicEnumIsNotSilentlyLocal(t *testing.T) {
	if _, err := Lower("public.hgo", []byte("package p\npub enum Event { Idle, }\n")); err == nil {
		t.Fatal("pub accepted without cross-package ABI")
	}
}
