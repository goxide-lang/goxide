package refs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScalarEndToEnd(t *testing.T) {
	src := `package main
func create() &mut int { n:=10; return &mut n }
func read(r &int) int { return *r }
func bump(w &mut int) { *w += 2; (*w)++ }
func main() { w:=create(); copied:=w; bump(copied); r:=&(*w); x:=*r; x++; var rr &int = &x; if read(rr)!=14 {panic("value copy")}; if read(r)!=13 {panic("shared identity")}; { r:=3; r++; if r!=4 {panic("shadow")}}; *w=21; if read(r)!=21 {panic("read path is not global immutability")} }
`
	r, err := Lower("scalar.gox", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.References) == 0 {
		t.Fatal("missing source type records")
	}
	if strings.Contains(string(r.Source), prefix) {
		t.Fatal("IR leaked")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module refs.test\ngo 1.27\n"), 0600)
	os.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600)
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, r.Source)
	}
}
func TestReject(t *testing.T) {
	cases := map[string]string{
		"write": "func f(r &int){*r=1}", "inc": "func f(r &int){(*r)++}", "compound": "func f(r &int){*r+=1}",
		"raw-var": "func f(r &int){var p *int=r; _=p}", "raw-return": "func f(r &int)*int{return r}", "raw-call": "func g(p *int){};func f(r &int){g(r)}",
		"any": "func f(r &int){var x any=r;_=x}", "cast": "func f(r &int){p:=(*int)(r);_=p}", "any-cast": "func f(r &int){_=any(r)}",
		"reborrow-write": "func f(r &int){w:=&mut (*r);*w=1}", "readonly-reborrow": "func f(r &int){p:=&(*r);*p=1}",
		"function-value": "func f(r &int)int{return *r};func g(){_=f}", "slice": "func f(r &int){_= []any{r}}", "nil": "func f(){var r &int=nil;_=r}", "zero": "func f(){var r &int;_=r}",
		"named-return": "func f()(r &int){return}", "struct": "type S struct {r &int}", "callback": "func f(g func(&int)){}",
		"multi-return": "func f(r &int)(&int,int){return r,1}", "vararg": "func sink(...any){};func f(r &int){sink(r)}", "pointer-slot": "func f(r &int){_= &r}",
		"raw-adopt": "func f(p *int)&int{return p}", "write-adopt": "func f(){x:=1;var p &mut int=&x;_=p}",
	}
	for n, b := range cases {
		t.Run(n, func(t *testing.T) {
			_, err := Lower(n+".gox", []byte("package p\n"+b))
			if err == nil {
				t.Fatalf("accepted %s", b)
			}
		})
	}
}
func TestPlainGoUnchanged(t *testing.T) {
	s := []byte("package p\nfunc f(p *int)*int {x:=2; r:=&x; *p=*r;return r}\n")
	r, e := Lower("raw.gox", s)
	if e != nil {
		t.Fatal(e)
	}
	if string(r.Source) != string(s) {
		t.Fatal("plain Go changed")
	}
}
func TestReadAddress(t *testing.T) {
	s := []byte("package p\nfunc read(r &int)int{return *r};func f()int{x:=2;return read(&x)}\n")
	r, e := Lower("read.gox", s)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(r.Source), "*int") {
		t.Fatal(string(r.Source))
	}
}

func TestStandardImport(t *testing.T) {
	_, err := Lower("import.gox", []byte("package p\nimport \"fmt\"\nfunc f(r &int)string{return fmt.Sprint(*r)}\n"))
	if err != nil {
		t.Fatal(err)
	}
}
func TestExtraReject(t *testing.T) {
	for _, b := range []string{
		`type R = &int;func f(r R){*r=1}`,
		`type R &int;func f(r R){*r=1}`,
		`func f(){w:=(&mut int)(nil);_=w}`,
		`func f(r &int){for *r = range 3{}}`,
		`func erase[T ~*int](v T)*int{return v};func f(r &int){p:=erase(r);*p=2}`,
	} {
		if _, err := Lower("unsupported.gox", []byte("package p\n"+b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestAttributesAndRawStrings(t *testing.T) {
	src := "package p\n#[derive(Debug)]\ntype S struct { N int }\nfunc f(r &int)string { return `hello\n#[not_an_attribute]` }\n"
	r, err := Lower("attrs.gox", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Source), "#[derive(Debug)]") || !strings.Contains(string(r.Source), "`hello\n#[not_an_attribute]`") {
		t.Fatal(string(r.Source))
	}
}

func TestNestedReborrowCannotUpgrade(t *testing.T) {
	for _, b := range []string{`func f(r &int){w:=&mut *(&(*r));*w=2}`, `func f(r &int){p:=&(*(&(*r)));*p=2}`, `func f(r &int){w:=&mut *(&mut *r);*w=2}`} {
		if _, err := Lower("nested.gox", []byte("package p\n"+b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestHostMutIdentifier(t *testing.T) {
	for _, s := range []string{`package p;func f(mut int,n int)int{return mut & n}`, `package p;type S struct{mut int};func f(s S,n int)int{return s.mut & n}`} {
		r, e := Lower("host.go", []byte(s))
		if e != nil || string(r.Source) != s {
			t.Fatalf("host changed: %v %s", e, r.Source)
		}
	}
}

func TestPhantomGenericReferenceRejected(t *testing.T) {
	for _, src := range []string{
		`type Phantom[T any] struct{};func f(){var p Phantom[&int];_=p}`,
		`func phantom[T any](){};func f(r &int){phantom[&int]()}`,
	} {
		if _, err := Lower("phantom.gox", []byte("package p\n"+src)); err == nil {
			t.Fatalf("accepted phantom reference argument: %s", src)
		}
	}
}
