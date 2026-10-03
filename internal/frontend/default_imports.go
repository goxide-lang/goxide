package frontend

import (
	"fmt"
	"go/ast"
	"go/types"
	"path"
	"strconv"
	"strings"
)

// Source/artifact registrations, not helper spelling, authorize this binding.
// Each source file keeps its own imports; helpers are anchored at package scope.
func importedDefaults(consumer string, sources map[string][]byte, known map[string]PackageResult) (map[string]string, map[string]string, map[string]map[string]string) {
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
			for di, x := range p.enumMetadata.Defaults {
				if !enumOriginImportAllowed(consumer, x.DefinitionPath) {
					continue
				}
				origin := alias
				if x.DefinitionPath != pth {
					origin = fmt.Sprintf("__hgo_enum_default_origin_f%d_i%d_d%d", fi, ii, di)
					imports[name] += fmt.Sprintf("; import %s %q", origin, x.DefinitionPath)
				}
				helper := fmt.Sprintf("__hgo_enum_default_import_f%d_i%d_d%d", fi, ii, di)
				preludes[name] += fmt.Sprintf("\nvar %s = %s.%s;\n", helper, origin, x.Helper)
				protocols[name][pth+"."+x.Name] = helper + "()"
			}
		}
	}
	return preludes, imports, protocols
}
func defaultImportPaths(f *ast.File, imp types.Importer) map[string]string {
	out := map[string]string{}
	if f == nil {
		return out
	}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		alias := path.Base(p)
		if im.Name != nil {
			alias = im.Name.Name
		} else if imp != nil {
			if pkg, err := imp.Import(p); err == nil && pkg != nil {
				alias = pkg.Name()
			}
		}
		if alias != "_" && alias != "." {
			out[alias] = p
		}
	}
	return out
}
func (g *generator) importedDefault(t ast.Expr) (string, bool) {
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
	value, ok := g.importedDefaults[p+"."+x.Sel.Name]
	return value, ok
}
