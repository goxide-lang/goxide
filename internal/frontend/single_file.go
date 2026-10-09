package frontend

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/typecheck"
)

// FileOptions binds a single requested source to a caller-owned module graph.
// PackagePath is the proven consumer identity, not the package clause spelling.
// A configured owner must provide the importer for that same graph.
type FileOptions struct {
	Roots       paths.Roots
	PackagePath string
	Importer    types.Importer
}

// CompileWithOptions restores existing registered imports for an explicitly
// known source owner. An empty owner retains the bounded standalone/WASM mode.
// Only source is compiled; no sibling files or publication artifacts are added.
func CompileWithOptions(filename string, source []byte, options FileOptions) (Result, error) {
	if err := naming.Source(filename, source); err != nil {
		return Result{}, err
	}
	_, declarations, err := scanUses(filename, source)
	if err != nil {
		return Result{}, err
	}
	for _, declaration := range declarations {
		if declaration.public {
			return Result{}, fmt.Errorf("%s: pub use requires package compilation to publish its metadata", filename)
		}
	}
	if options.PackagePath != "" && options.Importer == nil {
		return Result{}, fmt.Errorf("%s: configured single-file package requires its module importer", filename)
	}
	if options.Importer == nil {
		options.Importer = typecheck.NewImporter(filename)
	}
	result, err := prepareCompilationContext(options.PackagePath, map[string][]byte{filename: source}, nil, options.Importer, true, options.Roots)
	if err != nil {
		return Result{}, err
	}
	return result.Files[filename], nil
}

// Directory inputs distinguish native Go by suffix. A requested single source
// always uses language lowering, independent of its display filename.
func isGoxideInput(name string, single ...bool) bool {
	return len(single) > 0 && single[0] || strings.HasSuffix(name, ".gox")
}
