package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestOrdinaryConditionalImpls(t *testing.T) {
	fixture := filepath.Join("testdata", "traits", "generic")
	input := t.TempDir()
	if err := os.CopyFS(input, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module genericfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture = input
	module, err := lower.PrepareModule([]string{fixture}, "genericfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{fixture}, "genericfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	for _, name := range []string{"main.go", "protocols/protocols.go", "model/model.go"} {
		first, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("same input changed %s", name)
		}
	}
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "1 9 2\n2 6\n42 7\ntrue false\n42\n" {
		t.Fatalf("generic protocols output = %q", got)
	}
}

func TestGenericImplDirectCalls(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
trait Copy { func copy(self &Self) Self }
impl Copy for int { func copy(self &Self) Self { return *self } }
struct Box[T any] { Value T }
impl[T any] Box[T] {
    pub func new(value T) Self { return Self{Value: value} }
    pub func get(self Self) T { return self.Value }
}
impl[T any] Copy for Box[T] where T: Copy {
    func copy(self &Self) Self { return Self{Value: Copy::copy(&self.Value)} }
}
func main() {
    original := Box[int]::new(42)
    copied := Copy::copy(&original)
    original.Value = 7
    fmt::Println(copied.Value, original.get())
}
`, "42 7\n")
}
