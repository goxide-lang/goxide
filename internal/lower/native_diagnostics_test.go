package lower_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

func TestNativeDeclarationDiagnosticUsesSourceLine(t *testing.T) {
	const source = `package fixture
// deliberately long comment to separate offset from line
type where int

func F[T any]() where { return 0 }
`
	_, err := lower.CompilePackage(map[string][]byte{"native.go": []byte(source)}, "fixture", nil, nil, lower.PackageOptions{})
	const want = "fixture/native.go:5:35: native Go declaration: expected predicate after where"
	if err == nil || err.Error() != want {
		t.Fatalf("diagnostic = %v, want %q", err, want)
	}
}
