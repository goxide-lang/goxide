package lower

import "github.com/goxide-lang/goxide/internal/analysis"

func CompileProcModules(modules []analysis.Module, packagePath string) (*analysis.ProcLibrary, error) {
	selected, library, err := analysis.PrepareProcModules(modules, packagePath)
	if err != nil {
		return nil, err
	}
	if err := CompileModules(selected); err != nil {
		return nil, err
	}
	return library, nil
}
