package prototype_test

import (
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/tests/testutil"
)

// Prototype sources receive this explicit test graph. The source itself must
// declare use bindings; this helper never rewrites imports or derive names.
func compilePrototypeFixture(filename string, source []byte) (testutil.SourceResult, error) {
	return testutil.Source(filename, source, paths.Roots{
		"serde": {ModulePath: "github.com/goxide-lang/serde", Source: "module", GoPrefix: "github.com/goxide-lang/serde"},
		"fmt":   {PackagePath: "fmt", Source: "toolchain"},
	})
}
