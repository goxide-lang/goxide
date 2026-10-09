package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
)

// Metadata comes from successfully compiled packages or verified external artifacts.
// Import names are resolved separately for every source file; dependency paths,
// never package names, authorize an enum or builtin family.
func importedDeclarations(consumerPath string, sources map[string][]byte, packages map[string]PackageResult, single ...bool) (map[string][]enums.Decl, map[string]string, map[string]string, map[string]map[string]string) {
	declarations := map[string][]enums.Decl{}
	preludes := map[string]string{}
	extraImports := map[string]string{}
	unavailable := map[string]map[string]string{}
	for fi, name := range sortedSources(sources) {
		if !isGoxideInput(name, single...) {
			continue
		}
		f, err := parseSourceImports(name, sources[name])
		if err != nil {
			continue
		} // The normal parser reports malformed imports.
		visible := map[*types.Package]string{}
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			pkg := packages[path].enumTypes
			if pkg == nil {
				continue
			}
			alias := pkg.Name()
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias != "_" && alias != "." {
				visible[pkg] = alias
			}
		}
		for ii, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			p, ok := packages[path]
			if !ok {
				continue
			}
			alias := ""
			if spec.Name != nil {
				alias = spec.Name.Name
			} else if p.enumMetadata.Package != "" {
				alias = p.enumMetadata.Package
			} else {
				for _, fn := range sortedResultNames(p) {
					pf, e := parser.ParseFile(token.NewFileSet(), fn, p.Files[fn].Go, parser.PackageClauseOnly)
					if e == nil {
						alias = pf.Name.Name
						break
					}
				}
			}
			if alias == "" || alias == "_" || alias == "." {
				continue
			}
			for ei, export := range p.enumMetadata.Enums {
				if !enumOriginImportAllowed(consumerPath, export.DefinitionPath) {
					if unavailable[name] == nil {
						unavailable[name] = map[string]string{}
					}
					unavailable[name][alias+"."+export.Name] = "enum syntax through an internal-origin alias is not yet supported; use the producer's public Go API (origin " + export.DefinitionPath + ")"
					continue
				}
				originAlias := alias
				if export.DefinitionPath != path {
					originAlias = fmt.Sprintf("__goxide_enum_origin_f%d_i%d_e%d", fi, ii, ei)
					extraImports[name] += fmt.Sprintf("; import %s %q", originAlias, export.DefinitionPath)
				}
				prefix := fmt.Sprintf("__goxide_enum_import_f%d_i%d_%s", fi, ii, export.Name)
				d := enums.Decl{Name: prefix, LookupName: alias + "." + export.Name, PackagePath: export.DefinitionPath, Identity: export.DefinitionName, Tag: originAlias + "." + export.Tag, TagField: export.TagField, Validator: originAlias + "." + export.Validator, Public: true}
				var code strings.Builder
				fmt.Fprintf(&code, "\ntype %s = %s.%s;\n", prefix, alias, export.Name)
				for vi, v := range export.Variants {
					variant := enums.Variant{Name: v.Name, Tag: v.Tag, Field: v.Field, PayloadAlias: originAlias + "." + v.PayloadAlias, Constructor: fmt.Sprintf("%s_new%d", prefix, vi)}
					if v.HasPayload {
						variant.Payload = "__goxide_imported_payload"
						variant.Value, variant.Project = originAlias+"."+v.Value, originAlias+"."+v.Project
					}
					fmt.Fprintf(&code, "var %s = %s.%s;\n", variant.Constructor, originAlias, v.Constructor)
					d.Variants = append(d.Variants, variant)
				}
				preludes[name] += code.String()
				declarations[name] = append(declarations[name], d)
			}
			for ei, export := range p.enumMetadata.Families {
				export.Instances = orderedFamilyInstances(export.Instances)
				if !enumOriginImportAllowed(consumerPath, export.DefinitionPath) {
					if unavailable[name] == nil {
						unavailable[name] = map[string]string{}
					}
					unavailable[name][alias+"."+export.Name] = "sum syntax through an internal-origin alias is not yet supported; use the producer's public Go API"
					continue
				}
				originAlias := alias
				if export.DefinitionPath != path {
					originAlias = fmt.Sprintf("__goxide_enum_origin_f%d_i%d_b%d", fi, ii, ei)
					extraImports[name] += fmt.Sprintf("; import %s %q", originAlias, export.DefinitionPath)
				}
				prefix := fmt.Sprintf("__goxide_enum_family_import_f%d_i%d_b%d", fi, ii, ei)
				owners := map[string]string{path: alias, export.DefinitionPath: originAlias}
				for vi, instance := range export.Instances {
					owner := instance.ValidatorPath
					if owner == "" || owners[owner] != "" {
						continue
					}
					if !enumOriginImportAllowed(consumerPath, owner) {
						continue
					}
					oa := fmt.Sprintf("__goxide_enum_validator_owner_f%d_i%d_b%d_v%d", fi, ii, ei, vi)
					owners[owner] = oa
					extraImports[name] += fmt.Sprintf("; import %s %q", oa, owner)
				}
				d, code, reason := importedFamily(export, p.enumTypes, alias, originAlias, prefix, owners, visible)
				if reason != "" {
					if unavailable[name] == nil {
						unavailable[name] = map[string]string{}
					}
					unavailable[name][alias+"."+export.Name] = reason
					d.LookupName = ""
				}
				preludes[name] += code
				declarations[name] = append(declarations[name], d)
			}

		}
	}
	return declarations, preludes, extraImports, unavailable
}
func sortedResultNames(p PackageResult) []string {
	m := map[string][]byte{}
	for name := range p.Files {
		m[name] = nil
	}
	return sortedSources(m)
}

// Go's ImportsOnly parser still lexes the next token; a leading derive # is
// intentionally not Go syntax. Restrict parsing to the package/import prefix.
func parseSourceImports(name string, source []byte) (*ast.File, error) {
	fs := token.NewFileSet()
	file := fs.AddFile(name, -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, source, func(token.Position, string) {}, 0)
	_, tok, _ := scan.Scan()
	if tok != token.PACKAGE {
		return parser.ParseFile(fs, name, source, parser.ImportsOnly)
	}
	scan.Scan() // package name
	pos, tok, _ := scan.Scan()
	for tok == token.SEMICOLON {
		pos, tok, _ = scan.Scan()
	}
	for tok == token.IMPORT {
		pos, tok, _ = scan.Scan()
		if tok == token.LPAREN {
			for tok != token.RPAREN && tok != token.EOF {
				pos, tok, _ = scan.Scan()
			}
			if tok == token.RPAREN {
				pos, tok, _ = scan.Scan()
			}
		} else {
			if tok == token.IDENT || tok == token.PERIOD {
				pos, tok, _ = scan.Scan()
			}
			if tok == token.STRING {
				pos, tok, _ = scan.Scan()
			}
		}
		for tok == token.SEMICOLON {
			pos, tok, _ = scan.Scan()
		}
	}
	return parser.ParseFile(token.NewFileSet(), name, source[:file.Offset(pos)], parser.ImportsOnly)
}

// Validated candidates have the same registered payload contract. Select and
// bind them by owner identity, independently of artifact serialization order.
func orderedFamilyInstances(instances []enumFamilyInstanceExport) []enumFamilyInstanceExport {
	ordered := append([]enumFamilyInstanceExport(nil), instances...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ValidatorPath != ordered[j].ValidatorPath {
			return ordered[i].ValidatorPath < ordered[j].ValidatorPath
		}
		return ordered[i].Validator < ordered[j].Validator
	})
	return ordered
}
