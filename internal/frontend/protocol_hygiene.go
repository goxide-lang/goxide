package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/hxsf/goxide/internal/sourcemap"
)

const protocolTypeAnchor = "GoxideInternalTypeCarrierForSelf"

func reservedProtocolName(name string) bool {
	return name == protocolTypeAnchor || name == protocolMetadataMarker || strings.HasPrefix(name, "__goxide_protocol_") || strings.HasPrefix(name, "GoxideCompilerProtocol_")
}

// checkProtocolSourceNames runs before lowering introduces protocol helpers.
// Native Go may deliberately implement the type carrier contract or consume
// generated APIs. Its actual declaration conflicts are checked separately.
func checkProtocolSourceNames(sources map[string][]byte, single ...bool) error {
	for _, file := range sortedSources(sources) {
		if strings.HasSuffix(file, ".go") && (len(single) == 0 || !single[0]) {
			continue
		}
		for _, t := range pathTokens(file, sources[file]) {
			if t.kind == token.IDENT && reservedProtocolName(t.text) {
				origin := sourcemap.Position(file, sources[file], t.start, t.end)
				return &sourcemap.Error{Message: "reserved protocol identifier " + t.text, Origin: &origin, Generated: t.pos}
			}
		}
	}
	return nil
}

// checkProtocolGeneratedNames checks only symbols this package will generate.
// An independently implemented native Go carrier is legal; it conflicts only
// when the compiler also needs to declare the anchor on that same target.
func checkProtocolGeneratedNames(sources map[string][]byte, generatedSymbols, anchorTargets []string) error {
	symbols, targets := map[string]bool{}, map[string]bool{}
	for _, name := range generatedSymbols {
		symbols[name] = true
	}
	for _, name := range anchorTargets {
		targets[name] = true
	}
	if len(symbols) == 0 && len(targets) == 0 {
		return nil
	}
	fs := token.NewFileSet()
	files := map[string]*ast.File{}
	aliases := map[string]string{}
	for _, name := range sortedSources(sources) {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fs, name, sources[name], 0)
		if err != nil {
			return err
		}
		files[name] = f
		for _, d := range f.Decls {
			if d, ok := d.(*ast.GenDecl); ok && d.Tok == token.TYPE {
				for _, s := range d.Specs {
					sp := s.(*ast.TypeSpec)
					if id, ok := ast.Unparen(sp.Type).(*ast.Ident); ok && sp.Assign.IsValid() {
						aliases[sp.Name.Name] = id.Name
					}
				}
			}
		}
	}
	canonical := func(name string) string {
		seen := map[string]bool{}
		for aliases[name] != "" && !seen[name] {
			seen[name] = true
			name = aliases[name]
		}
		return name
	}
	for target := range targets {
		targets[canonical(target)] = true
	}
	conflict := func(file string, id *ast.Ident, message string) error {
		pos := fs.Position(id.Pos())
		origin := sourcemap.Position(file, sources[file], pos.Offset, pos.Offset+len(id.Name))
		return &sourcemap.Error{Message: message, Origin: &origin, Generated: pos}
	}
	for _, file := range sortedSources(sources) {
		f := files[file]
		if f == nil {
			continue
		}
		for _, im := range f.Imports {
			if im.Name != nil && symbols[im.Name.Name] {
				return conflict(file, im.Name, "generated protocol symbol conflicts with native import "+im.Name.Name)
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					if symbols[d.Name.Name] {
						return conflict(file, d.Name, "generated protocol symbol conflicts with native declaration "+d.Name.Name)
					}
					continue
				}
				if d.Name.Name != protocolTypeAnchor || len(d.Recv.List) != 1 {
					continue
				}
				recv := ast.Unparen(d.Recv.List[0].Type)
				if p, ok := recv.(*ast.StarExpr); ok {
					recv = ast.Unparen(p.X)
				}
				if id, ok := recv.(*ast.Ident); ok && targets[canonical(id.Name)] {
					return conflict(file, d.Name, fmt.Sprintf("generated protocol anchor conflicts with native method on %s", id.Name))
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.ValueSpec:
						for _, id := range sp.Names {
							if symbols[id.Name] {
								return conflict(file, id, "generated protocol symbol conflicts with native declaration "+id.Name)
							}
						}
					case *ast.TypeSpec:
						if symbols[sp.Name.Name] {
							return conflict(file, sp.Name, "generated protocol symbol conflicts with native declaration "+sp.Name.Name)
						}
						st, ok := sp.Type.(*ast.StructType)
						if !ok || !targets[canonical(sp.Name.Name)] {
							continue
						}
						for _, field := range st.Fields.List {
							names := field.Names
							if len(names) == 0 {
								embedded := ast.Unparen(field.Type)
								if ptr, ok := embedded.(*ast.StarExpr); ok {
									embedded = ast.Unparen(ptr.X)
								}
								switch embedded := embedded.(type) {
								case *ast.Ident:
									names = []*ast.Ident{embedded}
								case *ast.SelectorExpr:
									names = []*ast.Ident{embedded.Sel}
								}
							}
							for _, id := range names {
								if id.Name == protocolTypeAnchor {
									return conflict(file, id, "generated protocol anchor conflicts with native field on "+sp.Name.Name)
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}
