package frontend

import (
	"bytes"
	"github.com/hxsf/goxide/internal/testdeps"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainMatchControlFlowAndValidation(t *testing.T) {
	source := `package main
var calls int
func next(i int)Result[int,error]{calls++;return Result[int,error]::Ok(i)}
func loops()int{
 n:=0
 for i:=0;i<3;i++ {match next(i){Result[int,error]::Ok(v)=>{n+=v+1;break},Result[int,error]::Err(_)=>{},};n+=100}
 for i:=0;i<3;i++ {match next(i){Result[int,error]::Ok(_)=>{n++;continue},Result[int,error]::Err(_)=>{},};n+=100}
 return n
}
func recovered(){defer func(){match next(0){Result[int,error]::Ok(_)=>{if recover()!="kept"{panic("recover frame")}},Result[int,error]::Err(_)=>{},}}();panic("kept")}
func invalid(){defer func(){if recover()!="hgo: invalid enum Result"{panic("missing nested validation")}}();var inner Result[int,error];match Result[Result[int,error],error]::Ok(inner){Result[Result[int,error],error]::Ok(_)=>{panic("wrong body")},Result[Result[int,error],error]::Err(_)=>{},}}
func none(v Option[int])int{nil:=42;_=nil;return match v{Option[int]::Some(n)=>n,Option[int]::None=>-1,}}
func main(){if loops()!=4||calls!=4{panic("control flow or eval count")};recovered();invalid();if none(Option[int]::None)!=-1||none(Option[int]::Some(9))!=9{panic("Option")}}
`
	r, err := Compile("plain.hgo", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r.Go, []byte("__hgo_enum_matched_")) {
		t.Fatal("plain match still emits matched state")
	}
	again, err := Compile("plain.hgo", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.Go, again.Go) || !bytes.Equal(r.SourceMap, again.SourceMap) {
		t.Fatal("nondeterministic Go/source map")
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"main.go": r.Go, "go.mod": []byte("module readability\ngo 1.27\n")} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, r.Go)
	}
}

func TestGuardedMatchRetainsFallback(t *testing.T) {
	r, err := Compile("guard.hgo", []byte(`package p
func f(v Option[int])int{return match v{Option[int]::Some(n) if n>0=>n,Option[int]::Some(_)=>0,Option[int]::None=>-1,}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Go), "__hgo_enum_matched_") {
		t.Fatal("guarded match lost fallback state")
	}
}

func TestShortValidatorPackageIdentity(t *testing.T) {
	compile := func(typ string) Result {
		t.Helper()
		r, err := Compile("binding.hgo", []byte("package p\nvar value Result["+typ+",error]\n"))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := compile("int")
	repeat := compile("int")
	seen := map[string]string{}
	if err := packageGeneratedNames("a.hgo", &first, 0, seen); err != nil {
		t.Fatal(err)
	}
	if err := packageGeneratedNames("b.hgo", &repeat, 1, seen); err != nil {
		t.Fatal("same identity was not deduplicated", err)
	}
	other := compile("string")
	other.Enums[0].Validator = first.Enums[0].Validator
	if err := packageGeneratedNames("c.hgo", &other, 2, seen); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatal("cross-file collision accepted", err)
	}
	name := first.Enums[0].Validator
	for _, body := range []string{"var " + name + " int", "func f(){type " + name + " int}", "macro_rules! bad {()=>{func(){var " + name + " int}};}"} {
		if _, err := Compile("capture.hgo", []byte("package p\n"+body)); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatal("source capture accepted", err)
		}
	}
	if _, err := CompilePackage("example.org/p", map[string][]byte{"a.hgo": []byte("package p\nvar v Result[int,error]\n"), "native.go": []byte("package p\nvar " + name + " int\n")}, nil); err == nil {
		t.Fatal("native declaration collision accepted")
	}
}

func TestPlainMatchArmSourceOrigin(t *testing.T) {
	const source = "package p\nfunc f(v Option[int])int {\n return match v {\n Option[int]::Some(n)=>n+17,\n Option[int]::None=>23,\n }\n}\n"
	r, err := Compile("origin.hgo", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(r.Go, []byte("17"))
	if at < 0 {
		t.Fatal("missing arm literal")
	}
	seg := r.Diagnostics.Lookup(at)
	if seg == nil || seg.Origin == nil || seg.Origin.File != "origin.hgo" || seg.Origin.Line != 4 {
		t.Fatalf("wrong arm origin: %+v", seg)
	}
}
