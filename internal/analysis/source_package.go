package analysis

import (
	"fmt"
	"go/types"
	"maps"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// SourcePackage preserves an ordinary dependency's source-language namespace,
// including macro reexports. File selection and authored item configuration
// are frozen; macro definitions remain source. Environment-dependent snapshots
// require live compilation and cannot be loaded into a fixed distribution.
type SourcePackage struct {
	PackagePath          string            `json:"package"`
	ModulePath           string            `json:"module"`
	RootPackage          string            `json:"root_package,omitempty"`
	SourceDir            string            `json:"source_dir,omitempty"`
	Sources              map[string][]byte `json:"sources"`
	Roots                paths.Roots       `json:"roots"`
	Features             config.Features   `json:"features"`
	GoSugar              *bool             `json:"go_sugar,omitempty"`
	Target               config.Target     `json:"target"`
	RuntimeRoots         []RuntimeRoot     `json:"runtime_roots,omitempty"`
	EnvironmentDependent bool              `json:"environment_dependent,omitempty"`
}

func snapshotSourcePackages(modules []Module, crates *crateGraph, originals map[string]*syntax.File, configurations map[*syntax.File]moduleRootConfig, sources *sourceSet) ([]SourcePackage, error) {
	packages := map[string]*SourcePackage{}
	for _, module := range modules {
		if module.Config.Environment == nil {
			return nil, fmt.Errorf("source snapshots require an explicit target")
		}
		moduleRoots := sources.roots[module.Path]
		for _, extension := range []string{".gox", ".go"} {
			names := sources.moduleFiles(module, extension)
			for _, name := range names {
				if strings.HasSuffix(name, "_test"+extension) {
					continue
				}
				matches, err := sources.matches(name, module.Config.Environment.Context(module.Tags, module.Race))
				if err != nil {
					return nil, err
				}
				if !matches {
					continue
				}
				relative, err := filepath.Rel(module.Dir, name)
				if err != nil {
					return nil, err
				}
				key := path.Join(module.Path, filepath.ToSlash(relative))
				packagePath := path.Dir(key)
				if !crates.selected(module, packagePath) {
					continue
				}
				if extension == ".go" && originals[key] == nil {
					continue
				}
				data := sources.read(name)
				if extension == ".gox" {
					configured, ok := sources.configured[key]
					if !ok {
						return nil, fmt.Errorf("source %s was not analyzed", key)
					}
					data = configured.text
				}
				pkg := packages[packagePath]
				if pkg == nil {
					roots, _ := crates.roots(module, packagePath, moduleRoots)
					if configuration, ok := configurations[originals[key]]; ok {
						roots = configuration.roots
					}
					goSugar := module.Config.GoSugarEnabled()
					pkg = &SourcePackage{PackagePath: packagePath, ModulePath: module.Path, RootPackage: module.RootPackage, SourceDir: crates.bases[packagePath], Sources: map[string][]byte{}, Roots: maps.Clone(roots), Features: cloneProcFeatures(module.Config.Features), GoSugar: &goSugar, Target: module.Config.Environment.Target, RuntimeRoots: append([]RuntimeRoot(nil), module.RuntimeRoots...)}
					pkg.EnvironmentDependent = module.Config.Reads != nil && len(module.Config.Reads.Inputs()) != 0
					packages[packagePath] = pkg
				}
				pkg.Sources[path.Base(key)] = data
			}
		}
	}
	var names []string
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]SourcePackage, 0, len(names))
	for _, name := range names {
		result = append(result, *packages[name])
	}
	return result, nil
}

func prepareSourcePackages(packages []SourcePackage, files map[string]*syntax.File, configs map[*syntax.File]config.Config, roots map[*syntax.File]moduleRootConfig, imports *moduleImporter, importer types.Importer, sources *sourceSet) error {
	packageNames := map[string]string{}
	seenModules := map[string]bool{}
	if len(packages) != 0 {
		imports.publicRoots = map[string]string{}
		imports.sourcePackages = map[string]bool{}
		imports.crates = &crateGraph{bases: map[string]string{}, owners: map[string]*Crate{}}
	}
	for _, pkg := range packages {
		if pkg.EnvironmentDependent {
			return fmt.Errorf("source snapshot %s depends on environment variables; compile its live sources instead", pkg.PackagePath)
		}
		if pkg.PackagePath == "" || pkg.ModulePath == "" || imports.owners[pkg.PackagePath] != "" {
			return fmt.Errorf("duplicate or invalid source package %s", pkg.PackagePath)
		}
		imports.owners[pkg.PackagePath] = pkg.ModulePath
		imports.publicRoots[pkg.ModulePath], imports.sourcePackages[pkg.PackagePath] = pkg.RootPackage, true
		imports.crates.bases[pkg.PackagePath] = pkg.SourceDir
		if !seenModules[pkg.ModulePath] {
			module := Module{Path: pkg.ModulePath}
			if pkg.RootPackage != "" {
				module.Crates = []Crate{{Root: pkg.RootPackage, Library: true}}
			}
			imports.crates.modules = append(imports.crates.modules, module)
			seenModules[pkg.ModulePath] = true
		}
		if root := pkg.Roots["crate"].PackagePath; root != "" {
			imports.crates.owners[pkg.PackagePath] = &Crate{Root: root, Library: true}
		}
		for name, data := range pkg.Sources {
			key := path.Join(pkg.PackagePath, name)
			sources.add(key, data)
			source := sources.parse(key)
			if source.GoSyntax != nil {
				packageNames[pkg.PackagePath] = source.GoSyntax.Name.Name
			} else if source.Syntax != nil {
				packageNames[pkg.PackagePath] = source.Syntax.Package.Name
			}
		}

	}
	for _, pkg := range packages {
		environment, err := config.NewEnvironment(pkg.Target, nil)
		if err != nil {
			return err
		}
		goSugar := (config.Config{GoSugar: pkg.GoSugar}).GoSugarEnabled()
		configuration := config.Config{Features: cloneProcFeatures(pkg.Features), GoSugar: &goSugar, Environment: environment, Reads: &config.Reads{}}
		var names []string
		for name := range pkg.Sources {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if path.Base(name) != name {
				return fmt.Errorf("invalid source snapshot filename %s", name)
			}
			key := path.Join(pkg.PackagePath, name)
			data := sources.files[key].Content
			if files[key] != nil {
				return fmt.Errorf("duplicate source file %s", key)
			}
			var file *syntax.File
			switch path.Ext(name) {
			case ".gox":
				var err error
				file, _, err = sources.working(key, configuration)
				if err != nil {
					return err
				}
				if file == nil {
					continue
				}
				if pkg.Roots["crate"].PackagePath != "" {
					addCrateUse(file)
				}
			case ".go":
				native, err := sources.native(key)
				if err != nil {
					return err
				}
				if native == nil {
					continue
				}
				set := sources.files[key].GoTokens
				file, err = nativeDeclarations(key, data, native, set, packageNames, importer)
				if err != nil {
					return err
				}
				for _, imported := range native.Imports {
					target, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						return err
					}
					imports.crates.edges = append(imports.crates.edges, nativeCrateImport{pkg.PackagePath, target, key})
				}
			default:
				return fmt.Errorf("invalid source snapshot file %s", name)
			}
			files[key], configs[file] = file, configuration
			roots[file] = moduleRootConfig{module: pkg.PackagePath, name: key, roots: pkg.Roots, runtimeRoots: pkg.RuntimeRoots}
		}
	}
	return nil
}
