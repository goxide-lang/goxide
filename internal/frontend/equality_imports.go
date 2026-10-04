package frontend

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/equality"
)

// Bind actual verified protocol exports at package scope; source import aliases
// stay file-scoped and caller local identifiers cannot capture helper selection.
func importedEquality(consumer string, sources map[string][]byte, known map[string]PackageResult) (map[string]string, map[string]string, []equality.Protocol) {
	prelude, imports := map[string]string{}, map[string]string{}
	var protocols []equality.Protocol
	for fi, name := range sortedSources(sources) {
		if !strings.HasSuffix(name, ".gox") {
			continue
		}
		f, err := parseSourceImports(name, sources[name])
		if err != nil {
			continue
		}
		for ii, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			p, ok := known[path]
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
			for ei, x := range p.enumMetadata.Equality {
				if !enumOriginImportAllowed(consumer, x.DefinitionPath) {
					continue
				}
				origin := alias
				if x.DefinitionPath != path {
					origin = fmt.Sprintf("__goxide_enum_eq_origin_f%d_i%d_e%d", fi, ii, ei)
					imports[name] += fmt.Sprintf("; import %s %q", origin, x.DefinitionPath)
				}
				prefix := fmt.Sprintf("__goxide_enum_eq_import_f%d_i%d_e%d", fi, ii, ei)
				prelude[name] += fmt.Sprintf("\ntype %s = %s.%s; var %s_helper = %s.%s;\n", prefix, alias, x.Name, prefix, origin, x.Helper)
				protocols = append(protocols, equality.Protocol{PackagePath: x.DefinitionPath, TypeName: prefix, Helper: prefix + "_helper", PartialEq: true, Eq: x.Eq})
			}
		}
	}
	return prelude, imports, protocols
}
