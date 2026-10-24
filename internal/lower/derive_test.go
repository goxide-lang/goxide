package lower_test

import (
	"fmt"
	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/typecheck"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
	"go/importer"
	"sort"
)

// Business protocol acceptance lives in internal/compile/formal_std_test.go.
func lowerDeriveSources(sources map[string]string) (map[string][]byte, error) {
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	originals := map[string]*ast.File{}
	var input []*ast.File
	for _, name := range names {
		f, ds := parser.ParseFile(name, []byte(sources[name]))
		if len(ds) > 0 {
			return nil, fmt.Errorf("parse: %v", ds)
		}
		originals[name] = f
		input = append(input, f)
	}
	expanded, err := expand.Expand(input, nil)
	if err != nil {
		return nil, err
	}
	resolved := resolve.ResolveExpanded(originals, expanded, coreFixtureImporter{importer.Default()})
	if len(resolved.Diagnostics) > 0 {
		return nil, fmt.Errorf("resolve: %v", resolved.Diagnostics)
	}
	files := map[string]*ast.File{}
	for i, name := range names {
		files[name] = expanded.Files[i]
	}
	configuration := testdeps.Configuration()
	imports := typecheck.NewImporter("fixture.gox", typecheck.ImporterOptions{Config: configuration})
	info, ds := check.CheckWithImporter(files, resolved, imports, configuration.Sizes())
	if len(ds) > 0 {
		return nil, fmt.Errorf("check: %v", ds)
	}
	return lower.Lower(files, info)
}
