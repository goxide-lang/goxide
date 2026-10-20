package lower_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestManualEnumValidatorDoesNotCaptureParameters(t *testing.T) {
	for _, test := range []struct{ name, parameter, result string }{
		{"parameter-panic", "panic", "bool"},
		{"parameter-nil", "nil", "bool"},
		{"parameter-variant", "Variant", "bool"},
		{"result-panic", "other", "(panic bool)"},
		{"result-nil", "other", "(nil bool)"},
		{"result-variant", "other", "(Variant bool)"},
		{"parameter-type-argument", "int", "bool"},
		{"result-type-argument", "other", "(int bool)"},
		{"parameter-named-argument", "User", "bool"},
		{"result-named-argument", "other", "(User bool)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `package main
use fmt;
type User struct { ID int }
pub enum Nested[T] { Carry(T) }
pub enum E { Variant, Integer(Nested[int]), Named(Nested[User]) }
var visits int
impl PartialEq for E {
    func equals(self E, PARAM E) RESULT { _ = PARAM; visits++; return true }
}
func rejects(f func()) (result bool) { defer func() { result = recover() != nil }(); f(); return false }
func main() {
    valid := EqualsE(E::Variant, E::Variant)
    var invalid E
    rejected := rejects(func() { _ = EqualsE(E::Variant, invalid) })
    fmt::Println(valid, rejected, visits)
}
`
			source = strings.NewReplacer("PARAM", test.parameter, "RESULT", test.result).Replace(source)
			input := map[string]string{"hygienefixture/main.gox": source}
			output, err := lowerDeriveSources(input)
			if err != nil {
				t.Fatal(err)
			}
			again, err := lowerDeriveSources(input)
			if err != nil || !reflect.DeepEqual(output, again) {
				t.Fatalf("repeated generation differs: %v", err)
			}
			module := t.TempDir()
			if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module hygienefixture\n\ngo 1.27\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(module, "main.go"), output["hygienefixture/main.gox"], 0600); err != nil {
				t.Fatal(err)
			}
			runGo(t, module, "build", ".")
			if got := runGo(t, module, "run", "."); got != "true true 1\n" {
				t.Fatalf("manual enum output = %q", got)
			}
		})
	}
}
