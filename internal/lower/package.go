package lower

import (
	"context"
	"go/types"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

// PackageOptions controls generation for a package in an existing Go project.
type PackageOptions struct {
	// StableGo keeps dependency entry points independent of consumer instantiations.
	StableGo            bool
	LineDirectives      bool
	Config              config.Config
	RejectConfiguration bool // Partial in-place generation cannot switch package configuration.
	ProcLibraries       []*analysis.ProcLibrary
	RuntimeRoots        []analysis.RuntimeRoot
	SourcePackages      []analysis.SourcePackage
	// RequireRoot runs only when source resolution selects a dependency root.
	RequireRoot func(alias string, namespace paths.Namespace) error
	// Expanded, when supplied, receives resolved source for the original inputs.
	Expanded map[string][]byte
	// OnProcDiagnostic receives every procedural diagnostic in expansion order,
	// including warnings, notes, and errors when compilation fails.
	// Filename is the original input key, or a canonical source key for a
	// projected procedural macro definition.
	OnProcDiagnostic func(filename string, diagnostic expand.ProcDiagnostic)
	procLibrary      *analysis.ProcLibrary
}

// CompilePackage analyzes once and emits the selected in-memory package.
func CompilePackage(sources map[string][]byte, packagePath string, goImports types.Importer, roots paths.Roots, options PackageOptions) (output map[string][]byte, err error) {
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: sources, PackagePath: packagePath, GoImporter: goImports, Roots: roots, Options: analysis.Options{Config: options.Config, RejectConfiguration: options.RejectConfiguration, ProcLibraries: options.ProcLibraries, RuntimeRoots: options.RuntimeRoots, SourcePackages: options.SourcePackages, RequireRoot: options.RequireRoot, OnProcDiagnostic: options.OnProcDiagnostic, Producer: options.procLibrary}})
	if result != nil {
		defer func() { err = analysis.FormatSourceDiagnostic(err, result.Files, result.DisplayNames, result.Expanded) }()
	}
	if err != nil {
		return nil, err
	}
	generated, err := Generate(context.Background(), result, GenerationOptions{LineDirectives: options.LineDirectives, StableGo: options.StableGo})
	if err != nil {
		return nil, err
	}
	output = map[string][]byte{}
	for name, data := range generated {
		display := result.DisplayNames[name]
		if display == "" {
			continue
		}
		output[display] = data
		if options.Expanded != nil {
			options.Expanded[display] = []byte(printer.Source(result.Resolved.Files[name].AST))
		}
	}
	return output, nil
}
