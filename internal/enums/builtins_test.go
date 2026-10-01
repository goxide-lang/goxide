package enums

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinSums(t *testing.T) {
	src := `package main
import "errors"
enum Local { A, B(int), }
func run() {
 v:=Option[int]::Some(7)
 match v {Option[int]::Some(n)=>{if n!=7 {panic(n)}},Option[int]::None=>{panic("none")}}
 nil:=42;_ = nil
 var empty Option[string]
 match empty {Option[string]::None=>{},Option[string]::Some(_)=>{panic("some")}}
 e:=errors.New("failure")
 r:=Result[int,error]::Err(e)
 match r {Result[int,error]::Ok(_)=>{panic("ok")},Result[int,error]::Err(got)=>{if got!=e {panic("lost")}}}
}
func main(){run()}
`
	r, e := Lower("sum.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sums\ngo 1.27\n"), 0600)
	c := exec.Command(goTool(t), "run", "-race", ".")
	c.Dir = dir
	c.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("%v %s\n%s", e, out, r.Source)
	}
}
func TestBuiltinRejects(t *testing.T) {
	for _, s := range []string{
		`package p; enum Option { A, }`,
		`package p; func f[T any](x T)Option[T]{return Option[T]::Some(x)}`,
		`package p; var x=Result[int]::Ok(1)`,
		`package p; var x=Option::Some(1)`,
		`package p; type Option int; var x Option[int]`,
		`package p; var x Result[[]Option[int],error]`,
		`package p; enum E {A,}; type Holder struct {Bad E}; var x Result[Holder,error]`,
	} {
		if _, e := Lower("bad.hgo", []byte(s)); e == nil {
			t.Fatal(s)
		}
	}
}
func TestBuiltinNominalAndInvalidCases(t *testing.T) {
	r, e := Lower("sum.hgo", []byte(`package main
var a Option[int]
var b Result[int,string]
var c Result[string,string]
var nested Option[Option[int]]
func main(){}
`))
	if e != nil {
		t.Fatal(e)
	}
	oi, _ := Builtin("Option", []string{"int"})
	ri, _ := Builtin("Result", []string{"int", "string"})
	rs, _ := Builtin("Result", []string{"string", "string"})
	nested, _ := Builtin("Option", []string{"Option[int]"})
	tests := `package main
import "testing"
func panics(t *testing.T,f func()){t.Helper();defer func(){if recover()==nil{t.Fatal("expected invalid case panic")}}();f()}
func TestInvalid(t *testing.T){`
	tests += `panics(t,func(){` + ri.Validator + `(nil)});`
	tests += `panics(t,func(){var p *` + oi.Variants[0].CaseName + `;` + oi.Validator + `(p)});`
	tests += `panics(t,func(){` + nested.Validator + `(` + nested.Variants[0].Constructor + `((*` + oi.Variants[0].CaseName + `)(nil)))});}`
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(tests), 0600)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sums\ngo 1.27\n"), 0600)
	c := exec.Command(goTool(t), "test", "-race", ".")
	c.Dir = dir
	c.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, out)
	}
	os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package main\nvar wrong "+rs.Name+" = "+ri.Variants[1].Constructor+"(\"bad\")"), 0600)
	c = exec.Command(goTool(t), "build", "-buildvcs=false", ".")
	c.Dir = dir
	c.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if out, e := c.CombinedOutput(); e == nil || !strings.Contains(string(out), "wrong type for method") {
		t.Fatalf("nominal failure expected: %v %s", e, out)
	}
}
