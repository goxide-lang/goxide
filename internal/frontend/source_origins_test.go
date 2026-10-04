package frontend

import (
	"bytes"
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runMappedGo(t *testing.T, src string, mode string) (Result, []byte, error) {
	t.Helper()
	r, e := Compile("provenance.gox", []byte(src))
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	for n, b := range map[string][]byte{"go.mod": []byte("module fixture\ngo 1.27\n"), "main.go": r.Go} {
		if e := testdeps.WriteFile(filepath.Join(dir, n), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), mode, "-buildvcs=false", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	out, e := cmd.CombinedOutput()
	return r, out, e
}
func TestSourceOriginsNestedRealGoError(t *testing.T) {
	src := `package main
macro_rules! inner {($x:ident)=>{$x+1};}
macro_rules! outer {($x:ident)=>{inner!($x)};}
func main(){_=outer!(missing)}
`
	r, raw, e := runMappedGo(t, src, "build")
	if e == nil {
		t.Fatal("invalid nested macro accepted")
	}
	mapped := r.Diagnostics.Diagnostics(r.Go, string(raw))
	for _, want := range []string{"provenance.gox:4:", "macro outer defined at provenance.gox:3:", "macro inner defined at provenance.gox:2:"} {
		if !strings.Contains(mapped, want) {
			t.Fatalf("missing %q\nraw: %s\nmapped: %s", want, raw, mapped)
		}
	}
	t.Logf("raw: %s\nmapped: %s", raw, mapped)
}
func TestSourceOriginsRawLineDirectivePreserved(t *testing.T) {
	src := "package main\nimport \"fmt\"\n#[derive(Clone)]\ntype Item struct{Value int}\nfunc main(){fmt.Print(`before\n//line imaginary.go:1\nafter`)}\n"
	_, out, e := runMappedGo(t, src, "run")
	if e != nil || string(out) != "before\n//line imaginary.go:1\nafter" {
		t.Fatalf("literal changed: %v %q", e, out)
	}
}
func TestSourceOriginsSerializerMethodIsNotField(t *testing.T) {
	src := []byte("package fixture\n#[derive(Serialize)]\ntype Item struct{Struct int}\n")
	r, e := Compile("serializer.gox", src)
	if e != nil {
		t.Fatal(e)
	}
	at := bytes.Index(r.Go, []byte("s.Struct"))
	if at < 0 {
		t.Fatal(string(r.Go))
	}
	if s := r.Diagnostics.Lookup(at + 2); s != nil && s.Origin != nil {
		t.Fatalf("serializer API attributed to user field: %+v", s)
	}
	at = bytes.Index(r.Go, []byte("v.Struct"))
	if at < 0 {
		t.Fatal(string(r.Go))
	}
	s := r.Diagnostics.Lookup(at + 2)
	if s == nil || s.Origin == nil || s.Kind != "derive field" {
		t.Fatalf("actual model field lost provenance: %+v", s)
	}
}

func TestTransformedPhaseErrorIsExplicitlyUnmapped(t *testing.T) {
	src := `package p
macro_rules! bad {($r:ident)=>{$r.N++};}
type S struct{N int}
func f(r &S){


 bad!(r)
}
`
	_, err := Compile("phase.gox", []byte(src))
	if err == nil || !strings.Contains(err.Error(), "refs.Finish [generated/unmapped; intermediate coordinates]") {
		t.Fatalf("transformed position must not claim original source precision: %v", err)
	}
}
