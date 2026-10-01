package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
)

// Metadata travels only from successfully compiled packages in this module.
// Import names are resolved separately for every source file; dependency paths,
// never package names, authorize an enum or builtin family.
func importedDeclarations(sources map[string][]byte, packages map[string]PackageResult) (map[string][]enums.Decl, map[string]string) {
	declarations := map[string][]enums.Decl{}
	preludes := map[string]string{}
	for fi, name := range sortedSources(sources) {
		if !strings.HasSuffix(name, ".hgo") {
			continue
		}
		f, err := parseSourceImports(name, sources[name])
		if err != nil {
			continue
		} // The normal parser reports malformed imports.
		for ii, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			p, ok := packages[path]
			if !ok {
				continue
			}
			alias := ""
			if spec.Name != nil {
				alias = spec.Name.Name
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
			seen := map[string]bool{}
			families := map[string]*enums.Decl{}
			for _, fn := range sortedResultNames(p) {
				for _, d := range p.Files[fn].Enums {
					if seen[d.Name] {
						continue
					}
					seen[d.Name] = true
					if d.Family != "" {
						family := families[d.Family]
						if family == nil {
							family = &enums.Decl{PackagePath: path, Name: alias + "." + d.Family, Family: d.Family}
							for _, v := range d.Variants {
								family.Variants = append(family.Variants, enums.Variant{Name: v.Name, Payload: v.Payload, CaseName: alias + ".Hgo" + d.Family + v.Name, Constructor: alias + ".HgoNew" + d.Family + v.Name})
							}
							families[d.Family] = family
						}
						family.Instances = append(family.Instances, enums.Decl{Validator: alias + ".HgoInst" + strings.TrimPrefix(d.Validator, "__hgo_enum_inst_")})
						continue
					}
					if !d.Public {
						continue
					}
					original := d.Name
					prefix := fmt.Sprintf("__hgo_enum_import_f%d_i%d_%s", fi, ii, original)
					d.PackagePath, d.LookupName, d.Name = path, alias+"."+original, prefix
					d.Validator = prefix + "_validate"
					var code strings.Builder
					fmt.Fprintf(&code, "\ntype %s = %s.%s; var %s = %s.Validate%s;\n", prefix, alias, original, d.Validator, alias, original)
					d.Variants = append([]enums.Variant(nil), d.Variants...)
					for vi := range d.Variants {
						v := &d.Variants[vi]
						v.CaseName, v.Constructor = fmt.Sprintf("%s_case%d", prefix, vi), fmt.Sprintf("%s_new%d", prefix, vi)
						fmt.Fprintf(&code, "type %s = %s.HgoCase%d_%s_%s; var %s = %s.New%s%s;\n", v.CaseName, alias, len(original), original, v.Name, v.Constructor, alias, original, v.Name)
					}
					preludes[name] += code.String()
					declarations[name] = append(declarations[name], d)
				}
			}
			for _, family := range []string{"Option", "Result"} {
				if d := families[family]; d != nil {
					declarations[name] = append(declarations[name], *d)
				}
			}
		}
	}
	return declarations, preludes
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
