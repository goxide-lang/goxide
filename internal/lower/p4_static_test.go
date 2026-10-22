package lower_test

import (
	"bytes"
	goast "go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestP4StaticProtocolExample(t *testing.T) {
	module := prepareP2Example(t, "static_protocol", "staticprotocol.example/app")
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "Ada Ada\n" {
		t.Fatalf("static_protocol output = %q", got)
	}
}

func TestP4CarrierAssociatedTypesAndNativeEnumViews(t *testing.T) {
	fixture := filepath.Join("testdata", "traits", "static")
	input := t.TempDir()
	if err := os.CopyFS(input, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module traitfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture = input
	module, err := lower.PrepareModule([]string{fixture}, "traitfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{fixture}, "traitfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	first, err := os.ReadFile(filepath.Join(module, "model", "model.go"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(again, "model", "model.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical trait inputs produced different Go bytes")
	}
	pinP4Dependencies(t, module)
	consumer, err := os.ReadFile(filepath.Join(fixture, "consumer.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), consumer, 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	const want = "*model.Ready 37 Ada 37 37 Ada\nint string 37\n37 99 Ada\n7 12\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("native carrier/view output = %q, want %q", got, want)
	}
	runGo(t, module, "vet", "./...")
	poisonP4TypeWitnesses(t, filepath.Join(module, "model", "model.go"))
	if got := runGo(t, module, "run", "-race", "."); got != want {
		t.Fatalf("type witnesses changed runtime behavior: %q", got)
	}
}

func poisonP4TypeWitnesses(t *testing.T, filename string) {
	t.Helper()
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, filename, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	anchors, associated := 0, 0
	for _, declaration := range file.Decls {
		method, ok := declaration.(*goast.FuncDecl)
		if !ok || method.Recv == nil || method.Body == nil {
			continue
		}
		if method.Name.Name == "GoxideInternalTypeCarrierForSelf" {
			anchors++
		} else if strings.HasPrefix(method.Name.Name, "GoxideTrait") && strings.Contains(method.Name.Name, "_Type") && len(method.Type.Params.List) == 0 {
			associated++
		} else {
			continue
		}
		method.Body = &goast.BlockStmt{List: []goast.Stmt{&goast.ExprStmt{X: &goast.CallExpr{
			Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: token.STRING, Value: `"compile-time type witness executed"`}},
		}}}}
	}
	if anchors < 3 || associated < 2 {
		t.Fatalf("missing struct/enum type witnesses: anchors=%d associated=%d", anchors, associated)
	}
	var output bytes.Buffer
	if err := format.Node(&output, positions, file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func pinP4Dependencies(t *testing.T, directory string) {
	t.Helper()
	name := filepath.Join(directory, "go.mod")
	module, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(name, module, 0600); err != nil {
		t.Fatal(err)
	}
}
