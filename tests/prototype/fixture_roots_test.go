package prototype_test

import (
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/tests/testutil"
)

// Prototype sources receive this explicit test graph. The source itself must
// declare use bindings; this helper never rewrites imports or derive names.
func compilePrototypeFixture(filename string, source []byte) (testutil.SourceResult, error) {
	return testutil.Source(filename, source, paths.Roots{
		"serde": {ModulePath: "github.com/goxide-lang/serde", Source: "module", PackagePath: "github.com/goxide-lang/serde", Children: map[string]paths.Namespace{
			"json": {PackagePath: "github.com/goxide-lang/serde/json"},
		}},
		"fmt": {PackagePath: "fmt", Source: "toolchain"},
	})
}
