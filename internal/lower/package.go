package lower

import (
	"fmt"
	"go/importer"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"path"
	"path/filepath"
	"sort"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

// PackageOptions controls generation for a package in an existing Go project.
type PackageOptions struct {
	LineDirectives      bool
	Config              config.Config
	RejectConfiguration bool // Partial in-place generation cannot switch package configuration.
	ProcLibraries       []*ProcLibrary
	// OnProcDiagnostic receives every procedural diagnostic in expansion order,
	// including warnings, notes, and errors when compilation fails.
	// Filename is the original input key, or a canonical source key for a
	// projected procedural macro definition.
	OnProcDiagnostic func(filename string, diagnostic expand.ProcDiagnostic)
	procLibrary      *ProcLibrary
}

// CompilePackage compiles caller-selected .gox and native .go files from one
// package. Native declarations participate in the source graph but are never
// emitted. Output keys are the original .gox input keys; this function performs
// no writes. The caller supplies an importer for the project's Go module graph.
func CompilePackage(sources map[string][]byte, packagePath string, goImports types.Importer, roots paths.Roots, options PackageOptions) (output map[string][]byte, err error) {
	if packagePath == "" {
		return nil, fmt.Errorf("package path is required")
	}
	if goImports == nil {
		goImports = importer.Default()
	}
	if options.procLibrary != nil {
		options.procLibrary.importer = goImports
	}
	if options.Config.Reads == nil {
		options.Config.Reads = new(config.Reads)
	}
	files := map[string]*syntax.File{}
	configs := map[*syntax.File]config.Config{}
	rootConfigs := map[*syntax.File]moduleRootConfig{}
	displayNames := map[string]string{}
	var expanded *expand.Result
	defer func() { err = FormatSourceDiagnostic(err, files, displayNames, expanded) }()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	imports := &moduleImporter{external: goImports, cache: map[string]*types.Package{}, namespaces: map[string]rootRequirement{}, owners: map[string]string{packagePath: packagePath}}
	strict := map[string]string{}
	for _, name := range names {
		key := path.Join(packagePath, filepath.Base(name))
		if displayNames[key] != "" {
			return nil, fmt.Errorf("duplicate source file %s", key)
		}
		displayNames[key] = name
		data := sources[name]
		switch filepath.Ext(name) {
		case ".gox":
			file, diagnostics := parser.ParseFile(key, data)
			files[key] = file
			for _, diagnostic := range diagnostics {
				if diagnostic.Severity == parser.Error {
					return nil, &Diagnostic{File: key, Span: diagnostic.Span, Message: diagnostic.Message}
				}
			}
			used, err := expand.Configure(file, options.Config)
			if err != nil {
				return nil, err
			}
			if used && options.RejectConfiguration {
				return nil, fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
			}
			configs[file] = options.Config
			rootConfigs[file] = moduleRootConfig{module: packagePath, name: key, roots: roots}
			if roots != nil {
				strict[key] = packagePath
			}
		case ".go":
			set := gotoken.NewFileSet()
			file, parseErr := goparser.ParseFile(set, name, data, goparser.SkipObjectResolution)
			if parseErr != nil {
				return nil, parseErr
			}
			tree, nativeErr := nativeDeclarations(key, data, file, set, map[string]string{packagePath: file.Name.Name}, goImports)
			if nativeErr != nil {
				return nil, nativeErr
			}
			files[key] = tree
		default:
			return nil, fmt.Errorf("%s: expected .gox or .go source", name)
		}
	}
	entries, err := procmacro.Collect(files, packagePath, options.procLibrary != nil)
	if err != nil {
		return nil, err
	}
	if library := options.procLibrary; library != nil {
		library.Entries = entries
		library.entryFiles = map[*procmacro.Entry]string{}
		for _, entry := range entries {
			for name, file := range files {
				if file == entry.File {
					library.entryFiles[entry] = name
				}
			}
		}
		procmacro.Strip(entries)
	}
	imports.procs, err = prepareProcLibraries(options.ProcLibraries, files, configs, rootConfigs, imports.owners)
	if err != nil {
		return nil, err
	}
	for _, requirement := range prepareSourceRoots(files, rootConfigs, goImports) {
		if _, exists := imports.namespaces[requirement.target]; !exists {
			imports.namespaces[requirement.target] = requirement
		}
	}
	packages := resolve.Resolve(files, nil)
	imports.SetSourcePackages(packages.Packages)
	var groups [][]*syntax.File
	for _, pkg := range packages.Packages {
		var group []*syntax.File
		for _, file := range pkg.Files {
			group = append(group, file.AST)
		}
		groups = append(groups, group)
	}
	macros := &moduleMacros{files: files, resolved: packages, imports: imports, producer: options.procLibrary}
	expanded, err = expand.ExpandPackages(groups, macros, configs)
	reportProcDiagnostics(options.OnProcDiagnostic, files, displayNames, expanded)
	if cycle := macros.checkCycles(expanded); cycle != nil {
		return nil, cycle
	}
	if imports.err != nil {
		return nil, imports.err
	}
	if err != nil {
		return nil, err
	}
	if expanded.UsesConfiguration && options.RejectConfiguration {
		return nil, fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
	}
	for _, requirement := range applyExpandedRoots(expanded, rootConfigs, files) {
		if _, exists := imports.namespaces[requirement.target]; !exists {
			imports.namespaces[requirement.target] = requirement
		}
	}
	resolved := resolve.ResolveExpanded(files, expanded, imports)
	if imports.err != nil {
		return nil, imports.err
	}
	if err := imports.finishUses(resolved, strict); err != nil {
		return nil, err
	}
	if len(resolved.Diagnostics) != 0 {
		return nil, resolved.Diagnostics[0]
	}
	expandedFiles := make(map[string]*syntax.File, len(files))
	lowerFiles := map[string]*syntax.File{}
	for name, file := range resolved.Files {
		expandedFiles[name] = file.AST
		if !file.AST.NativeGo && displayNames[name] != "" {
			lowerFiles[name] = file.AST
		}
	}
	checked, diagnostics := check.CheckWithImporter(expandedFiles, resolved, goImports, options.Config.Sizes())
	if len(diagnostics) != 0 {
		return nil, diagnostics[0]
	}
	if options.procLibrary != nil {
		if err := options.procLibrary.validate(resolved, checked, goImports); err != nil {
			return nil, err
		}
	}
	generated, err := lower(lowerFiles, checked, emissionOptions{lineDirectives: options.LineDirectives, originals: files})
	if err != nil {
		return nil, err
	}
	if err := ApplyTargetConstraints(generated, files, options.Config); err != nil {
		return nil, err
	}
	output = make(map[string][]byte, len(generated))
	for name, data := range generated {
		output[displayNames[name]] = data
	}
	return output, nil
}
