package frontend

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/typecheck"
)

// FileOptions binds a single requested source to a caller-owned module graph.
// PackagePath is the proven consumer identity, not the package clause spelling.
// A configured owner must provide the importer for that same graph.
type FileOptions struct {
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
	_, uses, err := scanUses(filename, source)
	if err != nil {
		return Result{}, err
	}
	if len(uses) > 0 {
		return Result{}, fmt.Errorf("%s: use/pub use requires directory or package compilation", filename)
	}
	if options.PackagePath != "" && options.Importer == nil {
		return Result{}, fmt.Errorf("%s: configured single-file package requires its module importer", filename)
	}
	if options.Importer == nil {
		options.Importer = typecheck.NewImporter(filename)
	}
	if err := checkSingleFileImports(filename, source, options); err != nil {
		return Result{}, err
	}
	if options.PackagePath == "" {
		f, err := parseSourceImports(filename, source)
		if err != nil {
			return Result{}, err
		}
		hasSharedSum := false
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			hasSharedSum = hasSharedSum || path == enums.SumPackage
		}
		if !hasSharedSum {
			return compile(filename, source, nil, options)
		}
	}
	result, err := prepareCompilationContext(options.PackagePath, map[string][]byte{filename: source}, nil, options.Importer, true)
	if err != nil {
		return Result{}, err
	}
	return result.Files[filename], nil
}

func checkSingleFileImports(filename string, source []byte, options FileOptions) error {
	f, err := parseSourceImports(filename, source)
	if err != nil {
		return err
	}
	for _, spec := range f.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path == "C" {
			continue
		}
		if options.PackagePath != "" && !enumOriginImportAllowed(options.PackagePath, path) {
			return enumImportError(filename, source, int(spec.Pos())-1, "internal package is not accessible from "+options.PackagePath+": "+path)
		}
		pkg, err := options.Importer.Import(path)
		if err != nil {
			if options.PackagePath != "" {
				return enumImportError(filename, source, int(spec.Pos())-1, err.Error())
			}
			// Legacy standalone transpilation remains conditionally type checked.
			continue
		}
		if pkg != nil && pkg.Scope().Lookup(useMetadataMarker) != nil {
			return enumImportError(filename, source, int(spec.Pos())-1, "imports with use metadata require directory/package compilation: "+path)
		}
	}
	return nil
}

// Directory inputs distinguish native Go by suffix. A requested single source
// always uses language lowering, independent of its display filename.
func isGoxideInput(name string, single ...bool) bool {
	return len(single) > 0 && single[0] || strings.HasSuffix(name, ".gox")
}
