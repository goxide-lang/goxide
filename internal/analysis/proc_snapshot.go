package analysis

import (
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

const (
	maxProcSnapshotFiles   = 256
	maxProcSnapshotBytes   = 8 * 1024 * 1024
	maxProcSnapshotEntries = 1024
)

// ProcLibrarySnapshot accompanies an already checked and built macro executable
// in a fixed compiler distribution. It contains logical source names, never a
// host filesystem path or captured process environment. It is not an untrusted
// macro package format: the distribution must bind it to that exact executable.
type ProcLibrarySnapshot struct {
	PackagePath  string               `json:"package"`
	ModulePath   string               `json:"module"`
	CompiledPath string               `json:"compiled_package"`
	Sources      map[string][]byte    `json:"sources"`
	Roots        paths.Roots          `json:"roots"`
	Features     config.Features      `json:"features"`
	GoSugar      *bool                `json:"go_sugar,omitempty"`
	Target       config.Target        `json:"target"`
	Entries      []ProcSnapshotEntry  `json:"entries"`
	GoTrees      []ProcSnapshotGoTree `json:"go_trees,omitempty"`
}

// ProcSnapshotGoTree retains namespace discovery from the checked producer's
// selected dependency graph. It carries no type data or runtime import grant.
type ProcSnapshotGoTree struct {
	Source   string   `json:"source"`
	Module   string   `json:"module"`
	Packages []string `json:"packages"`
}

// ProcSnapshotEntry records the checked source registration and its Go export.
type ProcSnapshotEntry struct {
	Name     string   `json:"name"`
	Kind     pm.Kind  `json:"kind"`
	Helpers  []string `json:"helpers,omitempty"`
	File     string   `json:"file"`
	Function string   `json:"function"`
	GoName   string   `json:"go_name"`
}

// SnapshotProcLibrary exports a producer after signature checking. Source
// configuration must be reproducible from its target and feature set alone;
// environment-dependent source selection is deliberately not shipped. Call it
// while the producer's selected import graph is available; project builds expose
// that lifetime through their OnPreparedSources callback.
func SnapshotProcLibrary(library *ProcLibrary) (ProcLibrarySnapshot, error) {
	if library == nil || library.sources == nil || library.config.Environment == nil || library.GoNames == nil {
		return ProcLibrarySnapshot{}, fmt.Errorf("procedural snapshot requires a checked library with an explicit target")
	}
	goSugar := library.config.GoSugarEnabled()
	snapshot := ProcLibrarySnapshot{
		PackagePath: library.PackagePath, ModulePath: library.modulePath, CompiledPath: library.compiledPath,
		Sources: map[string][]byte{}, Roots: maps.Clone(library.roots), Features: cloneProcFeatures(library.config.Features),
		Target: library.config.Environment.Target, GoSugar: &goSugar,
	}
	for source, contents := range library.sources {
		if filepath.Ext(source) != ".gox" {
			continue
		}
		name := filepath.Base(source)
		if _, exists := snapshot.Sources[name]; exists {
			return ProcLibrarySnapshot{}, fmt.Errorf("duplicate procedural snapshot source %s", name)
		}
		snapshot.Sources[name] = slices.Clone(contents)
	}
	for _, entry := range library.Entries {
		if entry == nil || entry.Function == nil || library.entryFiles[entry] == "" || library.GoNames[entry.Name] == "" {
			return ProcLibrarySnapshot{}, fmt.Errorf("procedural snapshot requires checked source registrations")
		}
		snapshot.Entries = append(snapshot.Entries, ProcSnapshotEntry{
			Name: entry.Name, Kind: entry.Kind, Helpers: slices.Clone(entry.Helpers),
			File: path.Base(library.entryFiles[entry]), Function: entry.Function.Name.Name, GoName: library.GoNames[entry.Name],
		})
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool { return snapshot.Entries[i].Name < snapshot.Entries[j].Name })
	if index, ok := library.importer.(interface {
		PackageIndex(string, string) ([]string, error)
	}); ok {
		trees := map[string]ProcSnapshotGoTree{"toolchain\x00": {Source: "toolchain"}}
		for _, root := range snapshot.Roots {
			if root.Unavailable == "" && root.GoPrefix != "" && root.ModulePath != "" {
				trees[root.Source+"\x00"+root.ModulePath] = ProcSnapshotGoTree{Source: root.Source, Module: root.ModulePath}
			}
		}
		var keys []string
		for key := range trees {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			tree := trees[key]
			packages, err := index.PackageIndex(tree.Source, tree.Module)
			if err != nil {
				return ProcLibrarySnapshot{}, fmt.Errorf("procedural snapshot package index: %w", err)
			}
			tree.Packages = slices.Clone(packages)
			sort.Strings(tree.Packages)
			tree.Packages = slices.Compact(tree.Packages)
			snapshot.GoTrees = append(snapshot.GoTrees, tree)
		}
	}
	if err := snapshot.validate(); err != nil {
		return ProcLibrarySnapshot{}, err
	}
	// Recollect with the original configuration to catch any source-selection
	// dependency on the build process without serializing its private values.
	configuration := library.config
	configuration.Reads = &config.Reads{}
	if _, _, err := snapshot.parse(configuration); err != nil {
		return ProcLibrarySnapshot{}, err
	}
	if len(configuration.Reads.Inputs()) != 0 {
		return ProcLibrarySnapshot{}, fmt.Errorf("procedural snapshot source selection cannot depend on environment variables")
	}
	if _, err := LoadProcLibrarySnapshot(snapshot, library.importer, nil); err != nil {
		return ProcLibrarySnapshot{}, err
	}
	return snapshot, nil
}

// LoadProcLibrarySnapshot reconstructs only source registration and definition
// scope. It performs no filesystem lookup, macro build, or SDK type checking.
// importer provides the fixed distribution's runtime packages; run executes the
// matching prebuilt macro. Neither is serialized in the snapshot.
func LoadProcLibrarySnapshot(snapshot ProcLibrarySnapshot, importer types.Importer, run func(pm.Request) ([]byte, error)) (*ProcLibrary, error) {
	if importer == nil {
		return nil, fmt.Errorf("procedural snapshot requires a fixed dependency importer")
	}
	if err := snapshot.validate(); err != nil {
		return nil, err
	}
	environment, err := config.NewEnvironment(snapshot.Target, nil)
	if err != nil {
		return nil, fmt.Errorf("procedural snapshot target: %w", err)
	}
	if environment.Target != snapshot.Target {
		return nil, fmt.Errorf("procedural snapshot target is inconsistent")
	}
	goSugar := (config.Config{GoSugar: snapshot.GoSugar}).GoSugarEnabled()
	configuration := config.Config{Features: cloneProcFeatures(snapshot.Features), GoSugar: &goSugar, Environment: environment, Reads: &config.Reads{}}
	entries, entryFiles, err := snapshot.parse(configuration)
	if err != nil {
		return nil, err
	}
	if len(configuration.Reads.Inputs()) != 0 {
		return nil, fmt.Errorf("procedural snapshot source selection cannot depend on environment variables")
	}
	if len(entries) != len(snapshot.Entries) {
		return nil, fmt.Errorf("procedural snapshot registrations do not match source")
	}
	wanted := make(map[string]ProcSnapshotEntry, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if _, duplicate := wanted[entry.Name]; duplicate {
			return nil, fmt.Errorf("duplicate procedural snapshot registration %s", entry.Name)
		}
		wanted[entry.Name] = entry
	}
	library := &ProcLibrary{
		PackagePath: snapshot.PackagePath, modulePath: snapshot.ModulePath, compiledPath: snapshot.CompiledPath,
		Entries: entries, entryFiles: entryFiles, GoNames: map[string]string{}, Run: run,
		sources: map[string][]byte{}, roots: maps.Clone(snapshot.Roots), config: configuration, importer: importer,
	}
	if len(snapshot.GoTrees) != 0 {
		sourceImports := &procSnapshotImporter{Importer: importer, trees: map[string]ProcSnapshotGoTree{}, origins: map[string]ProcSnapshotGoTree{}}
		for _, tree := range snapshot.GoTrees {
			tree.Packages = slices.Clone(tree.Packages)
			sourceImports.trees[tree.Source+"\x00"+tree.Module] = tree
			for _, name := range tree.Packages {
				sourceImports.origins[name] = tree
			}
		}
		library.importer = sourceImports
	}
	for _, entry := range entries {
		stored, exists := wanted[entry.Name]
		if !exists || entry.Kind != stored.Kind || entry.Function.Name.Name != stored.Function ||
			path.Base(entryFiles[entry]) != stored.File || !slices.Equal(entry.Helpers, stored.Helpers) {
			return nil, fmt.Errorf("procedural snapshot registration %s does not match source", entry.Name)
		}
		library.GoNames[entry.Name] = stored.GoName
	}
	for name, contents := range snapshot.Sources {
		library.sources[name] = slices.Clone(contents)
	}
	return library, nil
}

func cloneProcFeatures(features config.Features) config.Features {
	return config.Features{Enabled: maps.Clone(features.Enabled), Declared: maps.Clone(features.Declared)}
}

func (snapshot ProcLibrarySnapshot) validate() error {
	for _, name := range []string{snapshot.PackagePath, snapshot.ModulePath, snapshot.CompiledPath} {
		if name == "" || len(name) > 1024 || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "." ||
			strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n\t :") {
			return fmt.Errorf("invalid procedural snapshot package path %q", name)
		}
	}
	if len(snapshot.Sources) == 0 || len(snapshot.Sources) > maxProcSnapshotFiles || len(snapshot.Entries) > maxProcSnapshotEntries ||
		len(snapshot.Roots) > maxProcSnapshotEntries || len(snapshot.Features.Enabled) > maxProcSnapshotEntries || len(snapshot.Features.Declared) > maxProcSnapshotEntries || len(snapshot.GoTrees) > maxProcSnapshotEntries {
		return fmt.Errorf("procedural snapshot exceeds source or registration limits")
	}
	size := 0
	for name, contents := range snapshot.Sources {
		if path.Base(name) != name || name == "" || path.Ext(name) != ".gox" || strings.ContainsAny(name, "\\\x00\r\n") {
			return fmt.Errorf("invalid procedural snapshot source name %q", name)
		}
		size += len(contents)
		if size > maxProcSnapshotBytes {
			return fmt.Errorf("procedural snapshot sources exceed 8 MiB")
		}
	}
	for _, entry := range snapshot.Entries {
		if !token.IsIdentifier(entry.GoName) || !token.IsExported(entry.GoName) || len(entry.Helpers) > maxProcSnapshotEntries {
			return fmt.Errorf("invalid procedural snapshot export %q", entry.GoName)
		}
	}
	trees, origins, packages := map[string]bool{}, map[string]string{}, 0
	for _, tree := range snapshot.GoTrees {
		key := tree.Source + "\x00" + tree.Module
		if trees[key] || tree.Source == "toolchain" && tree.Module != "" {
			return fmt.Errorf("duplicate or invalid procedural snapshot package tree")
		}
		trees[key] = true
		for _, name := range tree.Packages {
			packages++
			if packages > 65536 || name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n\t :") ||
				(tree.Source != "toolchain" && (tree.Module == "" || name != tree.Module && !strings.HasPrefix(name, tree.Module+"/"))) {
				return fmt.Errorf("invalid procedural snapshot indexed package %q", name)
			}
			owner := tree.Module
			if tree.Source == "toolchain" {
				owner = "toolchain"
			}
			if previous, exists := origins[name]; exists && previous != owner {
				return fmt.Errorf("conflicting procedural snapshot package ownership for %s", name)
			}
			origins[name] = owner
		}
	}
	return nil
}

// Source normalization can discover producer-only package names without
// loading their exports. All actual type imports still use the fixed consumer
// importer, so this metadata cannot add a runtime package to generated output.
type procSnapshotImporter struct {
	types.Importer
	trees   map[string]ProcSnapshotGoTree
	origins map[string]ProcSnapshotGoTree
}

func (imports *procSnapshotImporter) PackageIndex(source, module string) ([]string, error) {
	if tree, ok := imports.trees[source+"\x00"+module]; ok {
		return slices.Clone(tree.Packages), nil
	}
	if underlying, ok := imports.Importer.(interface {
		PackageIndex(string, string) ([]string, error)
	}); ok {
		return underlying.PackageIndex(source, module)
	}
	return nil, fmt.Errorf("procedural snapshot has no package tree for %s", module)
}

func (imports *procSnapshotImporter) PackageOrigin(name string) (string, bool, error) {
	if tree, ok := imports.origins[name]; ok {
		return tree.Module, tree.Source == "toolchain", nil
	}
	if underlying, ok := imports.Importer.(interface {
		PackageOrigin(string) (string, bool, error)
	}); ok {
		return underlying.PackageOrigin(name)
	}
	return "", false, fmt.Errorf("procedural snapshot has no package owner for %s", name)
}

func (snapshot ProcLibrarySnapshot) parse(configuration config.Config) ([]*procmacro.Entry, map[*procmacro.Entry]string, error) {
	files := map[string]*syntax.File{}
	names := make([]string, 0, len(snapshot.Sources))
	for name := range snapshot.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := path.Join(snapshot.PackagePath, name)
		file, diagnostics := parser.ParseFile(key, snapshot.Sources[name])
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == parser.Error {
				return nil, nil, &Diagnostic{File: key, Span: diagnostic.Span, Message: diagnostic.Message}
			}
		}
		if _, err := expand.Configure(file, configuration); err != nil {
			return nil, nil, err
		}
		files[key] = file
	}
	entries, err := procmacro.Collect(files, snapshot.PackagePath, true)
	if err != nil {
		return nil, nil, err
	}
	entryFiles := map[*procmacro.Entry]string{}
	for _, entry := range entries {
		for name, file := range files {
			if entry.File == file {
				entryFiles[entry] = name
			}
		}
	}
	return entries, entryFiles, nil
}
