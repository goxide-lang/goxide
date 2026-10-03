package frontend

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"
)

// Restored registrations have already passed exact signature and artifact checks.
func importedFieldProtocols(consumer string, sources map[string][]byte, known map[string]PackageResult) (map[string]string, map[string]string, map[string]map[string]string) {
	preludes, imports := map[string]string{}, map[string]string{}
	protocols := map[string]map[string]string{}
	for fi, name := range sortedSources(sources) {
		if !strings.HasSuffix(name, ".hgo") {
			continue
		}
		f, err := parseSourceImports(name, sources[name])
		if err != nil {
			continue
		}
		protocols[name] = map[string]string{}
		for ii, spec := range f.Imports {
			pth, _ := strconv.Unquote(spec.Path.Value)
			p, ok := known[pth]
			if !ok {
				continue
			}
			alias := p.enumMetadata.Package
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias == "" || alias == "_" || alias == "." {
				continue
			}
			for di, x := range p.enumMetadata.FieldProtocols {
				if !enumOriginImportAllowed(consumer, x.DefinitionPath) {
					continue
				}
				origin := alias
				if x.DefinitionPath != pth {
					origin = fmt.Sprintf("__hgo_enum_field_origin_f%d_i%d_d%d", fi, ii, di)
					imports[name] += fmt.Sprintf("; import %s %q", origin, x.DefinitionPath)
				}
				helper := fmt.Sprintf("__hgo_enum_field_import_f%d_i%d_d%d", fi, ii, di)
				preludes[name] += fmt.Sprintf("\nvar %s = %s.%s;\n", helper, origin, x.Helper)
				protocols[name][x.Kind+"."+pth+"."+x.Name] = helper
			}
		}
	}
	return preludes, imports, protocols
}

func (g *generator) importedField(kind string, t ast.Expr) (string, bool) {
	if id, ok := t.(*ast.Ident); ok {
		value, found := g.localProtocols[id.Name][kind]
		return value, found
	}
	x, ok := t.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	q, ok := x.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	p := g.defaultImportPaths[q.Name]
	if p == "" {
		return "", false
	}
	helper, ok := g.importedFields[kind+"."+p+"."+x.Sel.Name]
	return helper, ok
}
func (g *generator) deriveProtocol(kind string, t ast.Expr) (string, bool) {
	if kind == "Default" || kind == "Clone" || kind == "Debug" {
		if value, ok, err := g.sumProtocol(kind, t); ok || err != nil {
			if err != nil {
				g.sumError = err
			}
			return value, true
		}
	}
	if kind == "Default" {
		return g.importedDefault(t)
	}
	if kind == "Type" {
		// Container types remain in package scope so generated locals (src, item,
		// out, ...) cannot capture source import aliases or named element types.
		g.next++
		name := fmt.Sprintf("__hgo_enum_derive_type_f%d_%d", g.deriveFile, g.next)
		fmt.Fprintf(&g.helpers, "\ntype %s = %s\n", name, g.typeName(t))
		return name, true
	}
	return g.importedField(kind, t)
}
