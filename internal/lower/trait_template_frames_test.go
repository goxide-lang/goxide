package lower_test

import (
	"bytes"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/typecheck"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestTraitTemplatePanicSourceLine(t *testing.T) {
	const source = `package main
trait Copy { func copy(self &Self) Self }
impl Copy for int { func copy(self &Self) Self { return *self } }
impl[T Copy] Copy for []T {
    func copy(self &Self) Self {
        out := make([]T, len(*self))
        for i := range *self { out[i] = Copy::copy(&(*self)[i]) }
        return out
    }
}
func Crash[T Copy](value &T) {
    _ = Copy::copy(value)
    // Keep the failure next to the business body.
    panic("review source line")
}
func Other() {}
func main() { values := []int{1}; Crash[[]int](&values) }
`
	configuration := testdeps.Configuration()
	imports := typecheck.NewImporter("review.gox", typecheck.ImporterOptions{Config: configuration})
	output, err := lower.CompilePackage(map[string][]byte{"review.gox": []byte(source)}, "review", imports, nil, lower.PackageOptions{LineDirectives: true, Config: configuration})
	if err != nil {
		t.Fatal(err)
	}
	data := output["review.gox"]
	if !bytes.Contains(data, []byte("// Keep the failure next to the business body.")) {
		t.Fatalf("generated output lost the business comment:\n%s", data)
	}
	fs := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fs, "review.go", data, goparser.ParseComments)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
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
		if p := fs.Position(call.Pos()); p.Line != 14 {
			t.Errorf("panic location = %s, expected review.gox:14", p)
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
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module review\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.go"), data, 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, dir, "build", ".")
	command := exec.Command("go", "run", ".")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	diagnostic, err := command.CombinedOutput()
	if err == nil || !bytes.Contains(diagnostic, []byte("review.gox:14")) || !bytes.Contains(diagnostic, []byte("panic: review source line")) {
		t.Fatalf("expected original panic source line: %v\n%s", err, diagnostic)
	}
}

func TestTraitTemplateForeignTypeRecoverFrame(t *testing.T) {
	sources := map[string]string{
		"review/lib/lib.gox": `package lib
pub trait Copy { func copy(self &Self) Self }
impl[T Copy] Copy for []T {
    func copy(self &Self) Self {
        out := make([]T, len(*self))
        for i := range *self { out[i] = Copy::copy(&(*self)[i]) }
        return out
    }
}
pub func catchPanic[T Copy](value &T, caught *bool) {
    *caught = recover() != nil
    _ = Copy::copy(value)
}
`,
		"review/main.gox": `package main
use review::lib;
struct Secret { value int }
impl lib::Copy for Secret { func copy(self &Self) Self { return Self{value: self.value} } }
func main() {
    values := []Secret{{value: 7}}
    caught := false
    func() { defer lib::catchPanic[[]Secret](&values, &caught); panic("caught") }()
    println(caught, values[0].value)
}
`,
	}
	input := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module review\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, source := range sources {
		path := filepath.Join(input, strings.TrimPrefix(name, "review/"))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := lower.PrepareModule([]string{input}, "review", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	again, err := lower.PrepareModule([]string{input}, "review", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(again)
	for _, name := range []string{"main.go", "lib/lib.go"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, second) {
			t.Fatalf("nondeterministic %s", name)
		}
	}
	runGo(t, dir, "build", "./...")
	if got := runGo(t, dir, "run", "."); got != "true 7\n" {
		t.Fatalf("output = %q", got)
	}
}
