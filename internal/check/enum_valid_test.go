package check_test

import (
	"go/types"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestEnumValidStaticInputs(t *testing.T) {
	for _, test := range []struct{ name, source, code string }{
		{"enum", `enum E { A }; func f(value E) bool { return enum_valid(value) }`, ""},
		{"alias", `enum E { A }; type Alias = E; func f(value Alias) bool { return enum_valid(value) }`, ""},
		{"generic-enum", `enum E[T any] { A(T) }; func f[T any](value E[T]) bool { return enum_valid(value) }`, ""},
		{"option", `func f[T any](value Option[T]) bool { return enum_valid(value) }`, ""},
		{"read-reference", `enum E { A([]int) }; func f(value &E) bool { return (enum_valid)(*value) }`, ""},
		{"parenthesized", `enum E { A }; func f(value E) bool { return (enum_valid)(value) }`, ""},
		{"shadowed", `func f() int { enum_valid := func(value int) int { return value + 1 }; return enum_valid(4) }`, ""},
		{"package-function", `func enum_valid(value int) int { return value + 1 }; func f() int { return enum_valid(4) }`, ""},
		{"ordinary-interface", `func f(value any) bool { return enum_valid(value) }`, "enum-valid-type"},
		{"pointer", `enum E { A }; func f(value *E) bool { return enum_valid(value) }`, "enum-valid-type"},
		{"unbounded", `func f[T any](value T) bool { return enum_valid(value) }`, "enum-valid-type"},
		{"literal-nil", `func f() bool { return enum_valid(nil) }`, "enum-valid-type"},
		{"no-argument", `func f() bool { return enum_valid() }`, "enum-valid-call"},
		{"builtin-value", `func f() { check := enum_valid; _ = check }`, "enum-valid-call"},
		{"extra-argument", `enum E { A }; func f(value E) bool { return enum_valid(value, value) }`, "enum-valid-call"},
		{"ellipsis", `enum E { A }; func f(value []E) bool { return enum_valid(value...) }`, "enum-valid-call"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package enumvalid\n"+test.source)
			info, diagnostics := check.Check(files, resolved)
			if test.code == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("unexpected diagnostics: %v", diagnostics)
				}
				for call := range info.EnumValid {
					if info.Types[call] != types.Typ[types.Bool] {
						t.Fatalf("enum_valid result = %v", info.Types[call])
					}
				}
				if (test.name == "shadowed" || test.name == "package-function") && len(info.EnumValid) != 0 {
					t.Fatal("ordinary function was treated as the enum_valid builtin")
				}
				return
			}
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("missing %s: %v", test.code, diagnostics)
		})
	}
}
