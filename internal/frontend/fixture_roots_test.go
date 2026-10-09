package frontend

import "github.com/hxsf/goxide/internal/paths"

// serdeFixtureRoots is the explicit dependency context for these compiler
// fixtures. It is test data, not a production prelude or import fallback.
func serdeFixtureRoots() paths.Roots {
	return paths.Roots{
		"serde": {PackagePath: "github.com/goxide-lang/serde", Children: map[string]paths.Namespace{
			"json": {PackagePath: "github.com/goxide-lang/serde/json"},
		}},
		"fmt": {PackagePath: "fmt"},
	}
}

func compileSerdeFixture(filename string, source []byte) (Result, error) {
	return CompileWithOptions(filename, source, FileOptions{Roots: serdeFixtureRoots()})
}

// frontendFixtureRoots supplies the common toolchain/std context, with each
// test spelling out its additional package mappings instead of inferring them
// from import paths or scanning a fixture's sources.
func frontendFixtureRoots(packages map[string]string) paths.Roots {
	roots := paths.Roots{
		"std": {Children: map[string]paths.Namespace{
			"sum": {PackagePath: "github.com/goxide-lang/std/sum"},
		}},
		"fmt":     {PackagePath: "fmt"},
		"strings": {PackagePath: "strings"},
		"time":    {PackagePath: "time"},
	}
	for alias, path := range packages {
		roots[alias] = paths.Namespace{PackagePath: path}
	}
	return roots
}

func compileFrontendFixture(filename string, source []byte) (Result, error) {
	return CompileWithOptions(filename, source, FileOptions{Roots: frontendFixtureRoots(nil)})
}
