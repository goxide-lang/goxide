package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestNestedMacroLocalsKeepDistinctSlotsInSameBlock(t *testing.T) {
	runMacroBindingSource(t, `package main
use fmt;
macro_rules! inner { () => {
    value := 9
    value++
    fmt::Println(value)
}; }
macro_rules! outer { () => {
    value := 3
    inner!()
    value++
    fmt::Println(value)
}; }
func main() { outer!() }
`, "10\n4\n")
}

func TestMacroCallerKeepsBindingAcrossGeneratedLocals(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"literal-local", `func() int {
    value := 9
    fmt::Println(value)
    return $caller::value
}()`, "9\n3\n"},
		// The nested reader uses the captured local without changing inner's caller binding.
		{"captured-local", `func() int {
    $n := 9
    fmt::Println($read!())
    return $caller::value
}()`, "9\n3\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := strings.ReplaceAll(`package main
use fmt;
macro_rules! read_local { () => { $caller::value }; }
macro_rules! inner { ($n:ident, $read:ident) => { BODY }; }
macro_rules! outer { () => { func() int {
    value := 3
    return inner!(value, read_local)
}() }; }
func main() { fmt::Println(outer!()) }
`, "BODY", test.body)
			runMacroBindingSource(t, source, test.want)
		})
	}
}

func runMacroBindingSource(t *testing.T, source, want string) {
	t.Helper()
	const name = "macrobindingfixture/main.gox"
	sources := map[string]string{name: source}
	output, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output[name], again[name]) {
		t.Fatal("same source produced different Go bytes")
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module macrobindingfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), output[name], 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
