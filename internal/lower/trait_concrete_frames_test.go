package lower_test

import (
	"bytes"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestConcreteTraitBodySourceLines(t *testing.T) {
	const source = `package main
use os;
trait Catch { func crash(self Self) }
type Record int
impl Catch for Record {
    func crash(self Self) {
        // Keep the concrete body failure here.
        panic("concrete source line")
    }
}
func main() {
    value := Record(1)
    if len(os::Args) > 1 { saved := value.crash; saved() }
    value.crash()
}
`
	compile := func() []byte {
		output, err := lower.CompilePackage(map[string][]byte{"concrete.gox": []byte(source)}, "concrete", nil, nil, lower.PackageOptions{LineDirectives: true})
		if err != nil {
			t.Fatal(err)
		}
		return output["concrete.gox"]
	}
	data := compile()
	if !bytes.Equal(data, compile()) {
		t.Fatal("concrete body generation is nondeterministic")
	}
	fs := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fs, "concrete.go", data, goparser.ParseComments)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	commentInBusinessBody := false
	for _, declaration := range file.Decls {
		function, ok := declaration.(*goast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		hasPanic := false
		goast.Inspect(function.Body, func(node goast.Node) bool {
			call, ok := node.(*goast.CallExpr)
			if ok {
				ident, ok := call.Fun.(*goast.Ident)
				hasPanic = hasPanic || ok && ident.Name == "panic"
			}
			return true
		})
		if !hasPanic {
			continue
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if comment.Text == "// Keep the concrete body failure here." && function.Body.Lbrace < comment.Pos() && comment.End() < function.Body.Rbrace {
					commentInBusinessBody = true
				}
			}
		}
	}
	if !commentInBusinessBody {
		t.Error("concrete body comment was lost or moved outside every business body")
	}
	found := 0
	goast.Inspect(file, func(node goast.Node) bool {
		call, ok := node.(*goast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*goast.Ident)
		if !ok || ident.Name != "panic" {
			return true
		}
		found++
		if p := fs.Position(call.Pos()); p.Line != 8 || filepath.Base(p.Filename) != "concrete.gox" {
			t.Errorf("panic location = %s, expected concrete.gox:8", p)
		}
		return true
	})
	if found == 0 {
		t.Error("generated output has no panic call")
	}
	if t.Failed() {
		t.Logf("generated:\n%s", data)
	}
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module concrete\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "concrete.go"), data, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "probe")
	runGo(t, dir, "build", "-o", binary, ".")
	for _, mode := range []string{"direct", "saved"} {
		t.Run(mode, func(t *testing.T) {
			var args []string
			if mode == "saved" {
				args = []string{mode}
			}
			command := exec.Command(binary, args...)
			command.Dir = dir
			diagnostic, err := command.CombinedOutput()
			if err == nil || !bytes.Contains(diagnostic, []byte("concrete.gox:8")) || !bytes.Contains(diagnostic, []byte("panic: concrete source line")) {
				t.Fatalf("expected original concrete panic source line: %v\n%s", err, diagnostic)
			}
		})
	}
}
