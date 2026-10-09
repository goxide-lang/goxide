package acceptance_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/paths"
)

// These roots are explicit fixture data. Source files still declare every use;
// neither this context nor the CLI helper rewrites source syntax or derive names.
func acceptanceSerdeRoots() paths.Roots {
	return paths.Roots{
		"serde": {PackagePath: "github.com/goxide-lang/serde", Children: map[string]paths.Namespace{
			"json": {PackagePath: "github.com/goxide-lang/serde/json"},
		}},
		"fmt":  {PackagePath: "fmt"},
		"time": {PackagePath: "time"},
	}
}

func compileAcceptanceSerdeFixture(filename string, source []byte) (frontend.Result, error) {
	return frontend.CompileWithOptions(filename, source, frontend.FileOptions{Roots: acceptanceSerdeRoots()})
}

func writeAcceptanceSerdeRoots(t *testing.T, dir, libraryModule string, namespaces ...string) {
	t.Helper()
	var data strings.Builder
	data.WriteString(`[roots.serde]
module = "github.com/goxide-lang/serde"
package = "github.com/goxide-lang/serde"
[roots.serde.namespaces]
json = "github.com/goxide-lang/serde/json"
[roots.errors]
source = "toolchain"
package = "errors"
`)
	if libraryModule != "" {
		fmt.Fprintf(&data, "[roots.library]\nmodule = %q\n[roots.library.namespaces]\n", libraryModule)
		for _, namespace := range namespaces {
			fmt.Fprintf(&data, "%q = %q\n", namespace, libraryModule+"/"+namespace)
		}
	}
	packagesWrite(t, dir, "goxide.roots.toml", data.String())
}
