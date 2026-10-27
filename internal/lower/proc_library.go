package lower

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/paths"
)

func CompileProcLibrary(sources map[string][]byte, packagePath string, goImports types.Importer, roots paths.Roots, options PackageOptions) (map[string][]byte, *analysis.ProcLibrary, error) {
	library := analysis.NewProcLibrary(sources, packagePath, roots, options.Config)
	options.procLibrary = library
	output, err := CompilePackage(sources, packagePath, goImports, roots, options)
	if err != nil {
		return nil, nil, err
	}
	return output, library, nil
}
