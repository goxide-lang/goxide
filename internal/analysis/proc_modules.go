package analysis

import (
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"

	"github.com/goxide-lang/goxide/internal/config"
)

// CompileProcModules compiles one macro program, including its ordinary source
// dependencies. Only the first module's lib root can register macro entries.
func PrepareProcModules(modules []Module, packagePath string) ([]Module, *ProcLibrary, error) {
	if len(modules) == 0 || packagePath == "" {
		return nil, nil, fmt.Errorf("procedural macro compilation requires staging modules and a lib root")
	}
	module := modules[0]
	library := &ProcLibrary{PackagePath: packagePath, modulePath: module.Path, compiledPath: packagePath, sources: map[string][]byte{}, roots: maps.Clone(module.Roots), config: module.Config}
	library.config.Features.Enabled = maps.Clone(module.Config.Features.Enabled)
	library.config.Features.Declared = maps.Clone(module.Config.Features.Declared)
	if environment := module.Config.Environment; environment != nil {
		copy := *environment
		copy.SupportedTargets = append([]config.Target(nil), environment.SupportedTargets...)
		library.config.Environment = &copy
	}
	names, err := moduleSources(module.Dir)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		relative, err := filepath.Rel(module.Dir, name)
		if err != nil {
			return nil, nil, err
		}
		if path.Dir(path.Join(module.Path, filepath.ToSlash(relative))) != packagePath {
			continue
		}
		selected, err := matchesModuleSource(name, module.Config.Environment.Context(module.Tags, module.Race))
		if err != nil {
			return nil, nil, err
		}
		if !selected {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, nil, err
		}
		library.sources[filepath.Base(name)] = data
	}
	modules = append([]Module(nil), modules...)
	modules[0].Producer = library
	return modules, library, nil
}
