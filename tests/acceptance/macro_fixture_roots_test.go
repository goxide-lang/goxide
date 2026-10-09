package acceptance_test

import (
	"github.com/hxsf/goxide/internal/rootconfig"
	"testing"
)

// Every entry is an explicit fixture choice. No source import, package directory,
// or Go requirement is inspected to manufacture a language namespace.
func writeMacroFixtureRoots(t *testing.T, dir string, declared map[string]rootconfig.Mapping) {
	t.Helper()
	roots := map[string]rootconfig.Mapping{}
	for name, pkg := range map[string]string{
		"fmt": "fmt", "io": "io", "errors": "errors", "strconv": "strconv", "strings": "strings",
		"reflect": "reflect", "context": "context", "slog": "log/slog", "sync": "sync", "time": "time",
	} {
		roots[name] = rootconfig.Mapping{Source: "toolchain", Package: pkg}
	}
	for name, mapping := range declared {
		roots[name] = mapping
	}
	writeAcceptanceRoots(t, dir, roots)
}
