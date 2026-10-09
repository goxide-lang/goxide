package frontend

import (
	"bytes"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/testdeps"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runMappedGo(t *testing.T, src string, mode string) (Result, []byte, error) {
	t.Helper()
	r, e := compileFrontendFixture("provenance.gox", []byte(src))
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
	src := "package main\nuse fmt;\n#[derive(Clone)]\ntype Item struct{Value int}\nfunc main(){fmt::Print(`before\n//line imaginary.go:1\nafter`)}\n"
	_, out, e := runMappedGo(t, src, "run")
	if e != nil || string(out) != "before\n//line imaginary.go:1\nafter" {
		t.Fatalf("literal changed: %v %q", e, out)
	}
}
func TestSourceOriginsSerializerMethodIsNotField(t *testing.T) {
	src := []byte("package fixture; use serde;\n#[derive(serde::Serialize)]\ntype Item struct{Struct int}\n")
	r, e := compileSerdeFixture("serializer.gox", src)
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

func TestReferenceErrorPreservesCopiedMacroArgument(t *testing.T) {
	src := `package p
macro_rules! bad {($r:ident)=>{$r.N++};}
type S struct{N int}
func f(r &S){


 bad!(r)
}
`
	_, err := Compile("phase.gox", []byte(src))
	if err == nil {
		t.Fatal("readonly write accepted")
	}
	for _, want := range []string{"phase.gox:7:7: refs: cannot write", "source span [94,95)", "macro bad defined at phase.gox:2:1; invoked at phase.gox:7:2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing copied argument evidence %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "generated/unmapped") {
		t.Fatalf("verified source argument was marked generated: %v", err)
	}
}

func TestGeneratedReferenceErrorDoesNotAcquireOrigin(t *testing.T) {
	source := []byte("package p\nvar x int\n")
	position := token.Position{Filename: "phase.gox", Offset: bytes.Index(source, []byte("x int")), Line: 2, Column: 5}
	err := &refs.Error{Position: position, Message: "synthetic checker failure", Generated: true}
	mapped := mapReferenceError(err, map[string]*sourcemap.Map{"phase.gox": sourcemap.Identity("phase.gox", source)})
	if mapped != err || !strings.Contains(stageError("refs.Finish", mapped).Error(), "generated/unmapped") {
		t.Fatalf("generated coordinates acquired an unproved source origin: %v", mapped)
	}
}
