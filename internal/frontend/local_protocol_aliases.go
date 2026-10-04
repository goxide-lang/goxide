package frontend

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/equality"
)

// Resolve transparent package aliases through Go type identity. Declarations
// retain their own file import scopes; a new defined type never inherits a
// registration merely because its underlying representation matches.
func bindLocalProtocolAliases(path string, sources map[string][]byte, cfg *compileConfig, known map[string]PackageResult, imp types.Importer) ([]equality.Protocol, error) {
	fs := token.NewFileSet()
	var files []*ast.File
	destination := ""
	for _, name := range sortedSources(sources) {
		if destination == "" && strings.HasSuffix(name, ".gox") {
			destination = name
		}
		context, ok := cfg.builtinTypes[name]
		if !ok {
			continue
		}
		original, err := parseSourceImports(name, sources[name])
		if err != nil {
			return nil, err
		}
		var source bytes.Buffer
		fmt.Fprintf(&source, "package %s\n", original.Name.Name)
		for _, im := range context.Imports {
			source.WriteString("import ")
			if err := format.Node(&source, token.NewFileSet(), im); err != nil {
				return nil, err
			}
			source.WriteByte('\n')
		}
		names := make([]string, 0, len(context.Types))
		for n := range context.Types {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			source.WriteString("type ")
			if err := format.Node(&source, token.NewFileSet(), context.Types[n]); err != nil {
				return nil, err
			}
			source.WriteByte('\n')
		}
		f, err := parser.ParseFile(fs, name, source.Bytes(), 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if destination == "" {
		return nil, nil
	}
	// Non-type declarations and later DSL lowering are intentionally absent.
	// Only successfully resolved alias TypeNames authorize a lookup; final package
	// checking still reports errors in the complete program.
	conf := types.Config{Importer: imp, IgnoreFuncBodies: true, Error: func(error) {}}
	pkg, _ := conf.Check(path, fs, files, nil)
	if pkg == nil {
		return nil, nil
	}
	cfg.localProtocols = map[string]map[string]string{}
	var protocols []equality.Protocol
	serial := 0
	paths := make([]string, 0, len(known))
	for p := range known {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || !obj.IsAlias() {
			continue
		}
		target, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || target.TypeArgs().Len() != 0 || target.TypeParams().Len() != 0 {
			continue
		}
		bind := func(kind, definitionPath, definitionName, helper string, eq bool) error {
			if !enumOriginImportAllowed(path, definitionPath) {
				return nil
			}
			origin := pkg
			if definitionPath != path {
				if imp == nil {
					return nil
				}
				var err error
				origin, err = imp.Import(definitionPath)
				if err != nil {
					return err
				}
			}
			if origin == nil {
				return nil
			}
			canonical, ok := origin.Scope().Lookup(definitionName).(*types.TypeName)
			if !ok || !types.Identical(target, types.Unalias(canonical.Type())) {
				return nil
			}
			if cfg.localProtocols[name] == nil {
				cfg.localProtocols[name] = map[string]string{}
			}
			if cfg.localProtocols[name][kind] != "" {
				return nil
			}
			bound := helper
			if definitionPath != path {
				serial++
				prefix := fmt.Sprintf("__goxide_enum_alias_protocol_%d", serial)
				cfg.enumGoImports[destination] += fmt.Sprintf("; import %s %q", prefix, definitionPath)
				bound = prefix + "_helper"
				cfg.preludes[destination] += fmt.Sprintf("\nvar %s = %s.%s;\n", bound, prefix, helper)
			}
			// Local helpers are generated from explicit registrations and are called
			// directly. Function-valued globals would create initialization cycles
			// when a manual implementation calls back through a derived wrapper.
			cfg.localProtocols[name][kind] = bound
			if kind == "Default" {
				cfg.localProtocols[name][kind] += "()"
			}
			if kind == "PartialEq" {
				protocols = append(protocols, equality.Protocol{PackagePath: definitionPath, TypeName: name, Helper: bound, PartialEq: true, Eq: eq})
			}
			return nil
		}
		// Local source registrations use the same identity gate as artifacts.
		// Helper spelling follows a registration; an ordinary same-name Go
		// function never creates one. Final lowering checks impl conflicts/signatures.
		for _, canonicalName := range pkg.Scope().Names() {
			model := cfg.models[canonicalName]
			for _, kind := range []string{"Default", "Clone", "Debug", "Serialize", "Deserialize"} {
				_, manualDefault := cfg.manualDefaults[canonicalName]
				if !model.attrs.derive[kind] && !(kind == "Default" && manualDefault) {
					continue
				}
				if err := bind(kind, path, canonicalName, kind+canonicalName, false); err != nil {
					return nil, err
				}
			}
		}
		for _, p := range paths {
			m := known[p].enumMetadata
			for _, x := range m.Defaults {
				if err := bind("Default", x.DefinitionPath, x.DefinitionName, x.Helper, false); err != nil {
					return nil, err
				}
			}
			for _, x := range m.FieldProtocols {
				if err := bind(x.Kind, x.DefinitionPath, x.DefinitionName, x.Helper, false); err != nil {
					return nil, err
				}
			}
			for _, x := range m.Equality {
				if err := bind("PartialEq", x.DefinitionPath, x.DefinitionName, x.Helper, x.Eq); err != nil {
					return nil, err
				}
			}
		}
	}
	return protocols, nil
}
