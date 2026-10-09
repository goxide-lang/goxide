package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/paths"
)

// Prototype sources receive this explicit test graph. The source itself must
// declare use bindings; this helper never rewrites imports or derive names.
func compilePrototypeFixture(filename string, source []byte) (frontend.Result, error) {
	return frontend.CompileWithOptions(filename, source, frontend.FileOptions{Roots: paths.Roots{
		"serde": {PackagePath: "github.com/goxide-lang/serde", Children: map[string]paths.Namespace{
			"json": {PackagePath: "github.com/goxide-lang/serde/json"},
		}},
		"fmt": {PackagePath: "fmt"},
	}})
}
