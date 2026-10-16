package buildscript

import (
	"testing"

	"github.com/hxsf/goxide/internal/paths"
)

func TestHostImportDiagnosticUsesSourceLine(t *testing.T) {
	const source = "package helper\n// Padding makes the byte offset differ from the line number.\n\n    @\n"
	_, err := hostImports("example.test/host", map[string][]byte{"helper.gox": []byte(source)}, paths.Roots{})
	const want = "helper.gox:4:5: unexpected character '@'"
	if err == nil || err.Error() != want {
		t.Fatalf("diagnostic = %v, want %q", err, want)
	}
}
