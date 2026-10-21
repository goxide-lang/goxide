package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestTraitMethodBindingOrdinaryGoWithoutSourceCall(t *testing.T) {
	const name = "bindboundary/library/library.gox"
	sources := map[string]string{name: `package library
pub trait Catch { func catchPanic(self Self, caught *bool) }
pub struct Record {}
impl Catch for Record {
 func catchPanic(self Self, caught *bool) { *caught = recover() != nil }
}
pub func Bind[T Catch](value T) func(*bool) { return value.catchPanic }
`}
	generated, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated[name], again[name]) {
		t.Fatal("native-only binder output is nondeterministic")
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module bindboundary\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(module, "library"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "library", "library.go"), generated[name], 0600); err != nil {
		t.Fatal(err)
	}
	const consumer = `package main
import("fmt"; "bindboundary/library")
func main() {
 caught := false
 func() {
  defer library.Bind[library.Record](library.Record{})(&caught)
  panic("ordinary Go consumer")
 }()
 fmt.Println(caught)
}
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "true\n" {
		t.Fatalf("native-only binder output = %q", got)
	}
}
