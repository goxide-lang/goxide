package propagate

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestPackageFromBindingsHaveFileNamespaces(t *testing.T) {
	src := []byte(`package sample
type Failure struct { Cause error }
func (f Failure) Error() string { return "failure" }
func FromFailure(e error) Failure { return Failure{e} }
func load() Result[int,error] { return Result[int,error]::Err(nil) }
func first() Result[int,Failure] { n:=load()?; return Result[int,Failure]::Ok(n) }
func second() Result[int,Failure] { n:=load()?; return Result[int,Failure]::Ok(n) }
`)
	p, err := Prepare("all.hgo", src)
	if err != nil {
		t.Fatal(err)
	}
	en, err := enums.Lower("all.hgo", p)
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "lowered.go", en.Source, 0)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]*bytes.Buffer{}
	for _, name := range []string{"first.go", "second.go", "shared.go"} {
		parts[name] = bytes.NewBufferString("package sample\n")
	}
	for _, decl := range f.Decls {
		name := "shared.go"
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == "first" || fn.Name.Name == "second") {
			name = fn.Name.Name + ".go"
		}
		if err := format.Node(parts[name], fs, decl); err != nil {
			t.Fatal(err)
		}
		parts[name].WriteByte('\n')
	}
	files := map[string][]byte{}
	for name, buf := range parts {
		files[name] = buf.Bytes()
	}
	for _, name := range []string{"first.go", "second.go"} {
		out, err := Lower(name, files[name], Config{Families: en.Enums, From: []Conversion{{Source: "error", Target: "Failure", Helper: "FromFailure"}}, Files: files, PackagePath: "example.org/sample", Namespace: strings.TrimSuffix(name, ".go")})
		if err != nil {
			t.Fatal(err)
		}
		files[name] = out
	}
	if err := typecheck.CheckPackage("example.org/sample", files, importer.Default()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["first.go"], []byte("__hgo_qfrom_first_")) || !bytes.Contains(files["second.go"], []byte("__hgo_qfrom_second_")) {
		t.Fatal("file binding namespaces missing")
	}
}

func TestCompanionDeclarationsKeepFileScopes(t *testing.T) {
	source := []byte(`package sample
import native "fmt"
func seed() Result[int,string] { return Result[int,string]::Ok(7) }
func outer() Result[int,string] { n:=load()?; native.Sprint(n); return Result[int,string]::Ok(n) }
`)
	prepared, err := Prepare("active.hgo", source)
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := enums.Lower("active.hgo", prepared)
	if err != nil {
		t.Fatal(err)
	}
	var resultName string
	for _, d := range lowered.Enums {
		if d.Family == "Result" {
			resultName = d.Name
		}
	}
	if resultName == "" {
		t.Fatal("missing Result metadata")
	}
	companion := []byte("package sample\nimport native \"strconv\"\nfunc load() " + resultName + " { native.Itoa(1); return seed() }")
	config := Config{Families: lowered.Enums, PackagePath: "example.org/app/sample", Files: map[string][]byte{"helper.go": companion, "active.hgo": []byte("ignored active copy")}}
	out, err := Lower("active.hgo", lowered.Source, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := typecheck.CheckPackage(config.PackagePath, map[string][]byte{"active.go": out, "helper.go": companion}, importer.Default()); err != nil {
		t.Fatal(err)
	}
	// An import in the active file must not become available in its companion.
	bad := []byte("package sample\nfunc load() " + resultName + " { native.Sprint(1); return seed() }")
	if err := typecheck.CheckPackage(config.PackagePath, map[string][]byte{"active.go": out, "helper.go": bad}, importer.Default()); err == nil || !strings.Contains(err.Error(), "undefined: native") {
		t.Fatalf("import scope leaked: %v", err)
	}
	config.Files = map[string][]byte{"broken.go": []byte("package sample\nfunc (")}
	if _, err := Lower("active.hgo", lowered.Source, config); err == nil || !strings.Contains(err.Error(), "broken.go") {
		t.Fatalf("companion parse failure lost: %v", err)
	}
	config.Files = map[string][]byte{"other.go": []byte("package other")}
	if _, err := Lower("active.hgo", lowered.Source, config); err == nil || !strings.Contains(err.Error(), "expected sample") {
		t.Fatalf("mixed package accepted: %v", err)
	}
}
