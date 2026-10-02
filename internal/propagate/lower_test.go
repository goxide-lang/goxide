package propagate

import (
	"github.com/hxsf/goxide/internal/enums"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedPropagationRace(t *testing.T) {
	src := `package main
import "fmt"
func get(fail bool) Result[int,string] {if fail {return Result[int,string]::Err("bad")};return Result[int,string]::Ok(4)}
func outer(fail bool) Result[string,string] { defer fmt.Print("D"); n:=get(fail)?; return Result[string,string]::Ok(fmt.Sprint(n)) }
func opt() Option[int] {return Option[int]::None}
func check() Option[string] {n:=opt()?;return Option[string]::Some(fmt.Sprint(n))}
func main() {fmt.Print(outer(false),outer(true),check())}
`
	p, e := Prepare("test.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	en, e := enums.Lower("test.hgo", p)
	if e != nil {
		t.Fatal(e)
	}
	out, e := Lower("test.hgo", en.Source, Config{Families: en.Enums})
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), out, 0600)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\ngo 1.27\n"), 0600)
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s\n%s", e, b, out)
	}
	if !strings.HasPrefix(string(b), "DD") {
		t.Fatal(string(b))
	}
}
func TestPrepare(t *testing.T) {
	for _, s := range []string{"x?", "f(x)?", "Result[int,error]::Ok(x)?", "$x?", "$f()?", "(x)?"} {
		p, e := Prepare("x", []byte(s))
		if e != nil {
			t.Fatal(s, e)
		}
		if !strings.HasPrefix(string(p), marker+"(") {
			t.Fatal(s, string(p))
		}
	}
}
func TestControlFlowRace(t *testing.T) {
	src := `package main
import "fmt"
var trace string
func load(fail bool) Result[int,string] { trace+="L"; if fail{return Result[int,string]::Err("bad")};return Result[int,string]::Ok(5) }
func first() int {trace+="F";return 1}
func last() int {trace+="T";return 2}
func combine(a,b,c int) int {trace+="C";return a+b+c}
func order(fail bool) Result[int,string] {defer func(){trace+="D"}();n:=combine(first(),load(fail)?,last());return Result[int,string]::Ok(n)}
func truth() Result[bool,string] {trace+="B";return Result[bool,string]::Err("b")}
func short() Result[bool,string] {v:=false&&truth()?;return Result[bool,string]::Ok(v)}
func closure() Result[int,string] {f:=func() Result[int,string] {n:=load(true)?;return Result[int,string]::Ok(n)};f();trace+="O";return Result[int,string]::Ok(2)}
func deferred() Result[int,string] {defer combine(first(),load(true)?,last());return Result[int,string]::Ok(0)}
func main(){order(false);if trace!="FLTCD"{panic(trace)};trace="";order(true);if trace!="FLD"{panic(trace)};trace="";short();if trace!=""{panic(trace)};closure();if trace!="LO"{panic(trace)};trace="";deferred();if trace!="FL"{panic(trace)};fmt.Print("ok")}
`
	p, e := Prepare("flow.hgo", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	en, e := enums.Lower("flow.hgo", p)
	if e != nil {
		t.Fatal(e)
	}
	out, e := Lower("flow.hgo", en.Source, Config{Families: en.Enums})
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module flow\ngo 1.27\n"), 0600)
	os.WriteFile(filepath.Join(dir, "main.go"), out, 0600)
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s\n%s", e, b, out)
	}
	if string(b) != "ok" {
		t.Fatal(string(b))
	}
}
func TestFromAndRejections(t *testing.T) {
	base := `package p
 type Wrapped struct {Cause error}
 func (w Wrapped) Error() string {return "wrapped"}
 func FromWrapped(e error) Wrapped {return Wrapped{e}}
 func get() Result[int,error] {return Result[int,error]::Err(nil)}
 func f() Result[string,Wrapped] {n:=get()?;_ = n;return Result[string,Wrapped]::Ok("")}
 `
	compile := func(s string, c []Conversion) ([]byte, error) {
		p, e := Prepare("from.hgo", []byte(s))
		if e != nil {
			return nil, e
		}
		en, e := enums.Lower("from.hgo", p)
		if e != nil {
			return nil, e
		}
		return Lower("from.hgo", en.Source, Config{Families: en.Enums, From: c})
	}
	mapping := Conversion{"error", "Wrapped", "FromWrapped"}
	out, e := compile(base, []Conversion{mapping})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(out), "= FromWrapped") || !strings.Contains(string(out), "(__hgo_failure.Value)") {
		t.Fatal(string(out))
	}
	identity := strings.ReplaceAll(base, "Result[string,Wrapped]", "Result[string,error]")
	out, e = compile(identity, []Conversion{{"error", "error", "ShouldNeverRun"}})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(out), "ShouldNeverRun") {
		t.Fatal("From selected before ordinary assignment compatibility")
	}
	for _, cs := range [][]Conversion{nil, {mapping, mapping}, {{"error", "Intermediate", "A"}, {"Intermediate", "Wrapped", "B"}}} {
		if _, e = compile(base, cs); e == nil {
			t.Fatal("accepted missing/ambiguous/chained conversion")
		}
	}
	for _, s := range []string{
		`package p;func f() Option[int] {x:=1?;return Option[int]::Some(x)}`,
		`package p;func get() Option[int]{return Option[int]::None};func f() int{return get()?}`,
		`package p;func get() Option[int]{return Option[int]::None};func f() Result[int,error]{x:=get()?;return Result[int,error]::Ok(x)}`,
		`package p;func get() Option[int]{return Option[int]::None};func f() Option[int]{for range get()? {};return Option[int]::None}`,
	} {
		if _, e = compile(s, nil); e == nil {
			t.Fatal("accepted invalid propagation", s)
		}
	}
}
