package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

// These roots are explicit fixture data. Source files still declare every use;
// neither this context nor the CLI helper rewrites source syntax or derive names.
func acceptanceSerdeRoots() paths.Roots {
	return paths.Roots{
		"serde": {PackagePath: "github.com/goxide-lang/serde", ModulePath: "github.com/goxide-lang/serde", Source: "module", GoPrefix: "github.com/goxide-lang/serde"},
		"fmt":   {PackagePath: "fmt", Source: "toolchain"},
		"time":  {PackagePath: "time", Source: "toolchain"},
	}
}

func compileAcceptanceSerdeFixture(filename string, source []byte) (testdeps.SourceResult, error) {
	return testdeps.Source(filename, source, acceptanceSerdeRoots())
}

func writeAcceptanceSerdeRoots(t *testing.T, dir, libraryModule string) {
	t.Helper()
	var data strings.Builder
	data.WriteString(`[roots.serde]
module = "github.com/goxide-lang/serde"
package = "github.com/goxide-lang/serde"
[roots.errors]
source = "toolchain"
package = "errors"
`)
	if libraryModule != "" {
		fmt.Fprintf(&data, "[roots.library]\nmodule = %q\npackage = %q\n", libraryModule, libraryModule)
	}
	packagesWrite(t, dir, "goxide.roots.toml", data.String())
}

// compileAcceptanceGraph preserves source-level enum and protocol declarations
// across modules without depending on generated metadata. Native consumers below
// still compile against each module's separately generated Go output.
func compileAcceptanceGraph(t *testing.T, success bool, source, destination string, dependencies ...string) string {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "module")
	inputs := []testdeps.ModuleInput{{Source: source, Destination: stage}}
	for _, dependency := range dependencies {
		if filepath.Clean(dependency) == filepath.Clean(source) {
			continue
		}
		inputs = append(inputs, testdeps.ModuleInput{Source: dependency, Destination: filepath.Join(t.TempDir(), "dependency")})
	}
	err := testdeps.ModuleGraph(inputs...)
	if success != (err == nil) {
		t.Fatalf("source graph compilation expected success=%v: %v", success, err)
	}
	if err != nil {
		return err.Error()
	}
	// CompileModules uses temporary dependency destinations for source checking.
	// Runtime checks use the fixture's separately emitted dependency modules.
	manifest, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "go.mod"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, destination); err != nil {
		t.Fatal(err)
	}
	return ""
}
