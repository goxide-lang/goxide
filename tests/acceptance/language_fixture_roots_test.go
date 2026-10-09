package acceptance_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/rootconfig"
)

func acceptanceLanguageRoots() paths.Roots {
	roots := acceptanceSerdeRoots()
	for _, path := range []string{"bytes", "context", "errors", "io", "os", "reflect", "runtime", "strconv", "strings", "sync", "unsafe"} {
		roots[path] = paths.Namespace{PackagePath: path}
	}
	roots["atomic"] = paths.Namespace{PackagePath: "sync/atomic"}
	roots["json"] = paths.Namespace{PackagePath: "encoding/json"}
	roots["slog"] = paths.Namespace{PackagePath: "log/slog"}
	return roots
}

func compileAcceptanceLanguageFixture(filename string, source []byte) (frontend.Result, error) {
	return frontend.CompileWithOptions(filename, source, frontend.FileOptions{Roots: acceptanceLanguageRoots()})
}

// Fixture declarations choose exact package roots and owners. This helper only
// serializes that declared context; it never reads or rewrites source syntax.
func writeAcceptanceRoots(t *testing.T, dir string, roots map[string]rootconfig.Mapping) {
	t.Helper()
	var names []string
	for name := range roots {
		names = append(names, name)
	}
	sort.Strings(names)
	var text strings.Builder
	for _, name := range names {
		root := roots[name]
		fmt.Fprintf(&text, "[roots.%s]\n", name)
		if root.Source != "" {
			fmt.Fprintf(&text, "source = %q\n", root.Source)
		}
		if root.Module != "" {
			fmt.Fprintf(&text, "module = %q\n", root.Module)
		}
		fmt.Fprintf(&text, "package = %q\n", root.Package)
		if len(root.Namespaces) != 0 {
			fmt.Fprintf(&text, "[roots.%s.namespaces]\n", name)
			var children []string
			for child := range root.Namespaces {
				children = append(children, child)
			}
			sort.Strings(children)
			for _, child := range children {
				fmt.Fprintf(&text, "%q = %q\n", child, root.Namespaces[child])
			}
		}
	}
	packagesWrite(t, dir, "goxide.roots.toml", text.String())
}

// Package fixtures state one module owner and each exact package path. Standard
// packages and serde are the shared, explicit language-test environment.
func writeAcceptanceFixtureRoots(t *testing.T, dir, module string, packages map[string]string) {
	t.Helper()
	roots := map[string]rootconfig.Mapping{
		"serde": {Module: "github.com/goxide-lang/serde", Package: "github.com/goxide-lang/serde", Namespaces: map[string]string{"json": "github.com/goxide-lang/serde/json"}},
	}
	for _, path := range []string{"bytes", "context", "errors", "fmt", "io", "os", "reflect", "runtime", "strconv", "strings", "sync", "time", "unsafe"} {
		roots[path] = rootconfig.Mapping{Source: "toolchain", Package: path}
	}
	roots["atomic"] = rootconfig.Mapping{Source: "toolchain", Package: "sync/atomic"}
	roots["json"] = rootconfig.Mapping{Source: "toolchain", Package: "encoding/json"}
	roots["slog"] = rootconfig.Mapping{Source: "toolchain", Package: "log/slog"}
	for alias, path := range packages {
		roots[alias] = rootconfig.Mapping{Module: module, Package: path}
	}
	writeAcceptanceRoots(t, dir, roots)
}
