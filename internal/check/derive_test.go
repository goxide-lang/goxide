package check_test

import (
	"github.com/goxide-lang/goxide/internal/check"
	"testing"
)

func TestOrdinaryProtocolSignatureDiagnostics(t *testing.T) {
	for _, impl := range []string{
		"impl Default for T { func r#default() int { return 0 } }",
		"impl PartialEq for T { func equals(value T, other T) bool { return true } }",
		"impl Eq for T { func unwanted() {} }",
	} {
		source := `package p
trait Default { func r#default() Self }
trait PartialEq { func equals(self Self, other Self) bool }
trait Eq {}
type T struct{}
` + impl
		files, resolved := parse(t, source)
		_, diagnostics := check.Check(files, resolved)
		if len(diagnostics) == 0 {
			t.Fatalf("invalid ordinary impl accepted: %s", impl)
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Code != check.TraitInvalid {
				t.Fatalf("expected ordinary trait validation for %s, got %v", impl, diagnostics)
			}
		}
	}
}
