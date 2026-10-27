package analysis

import (
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

func documentSymbols(file string, items []syntax.Item) []DocumentSymbol {
	var result []DocumentSymbol
	add := func(node syntax.Node, name *syntax.Ident, kind string, children []DocumentSymbol) {
		if name == nil || name.Missing {
			return
		}
		result = append(result, DocumentSymbol{Name: name.Name, Kind: kind, Range: Location{File: file, Span: node.Span()}, Selection: Location{File: file, Span: name.Span()}, Children: children})
	}
	fields := func(typ syntax.Type) []DocumentSymbol {
		var children []DocumentSymbol
		if structure, ok := typ.(*syntax.StructType); ok {
			for _, field := range structure.Fields {
				for _, name := range field.Names {
					if !name.Missing {
						children = append(children, DocumentSymbol{Name: name.Name, Kind: "field", Range: Location{File: file, Span: field.Span()}, Selection: Location{File: file, Span: name.Span()}})
					}
				}
			}
		}
		return children
	}
	values := func(specs []*syntax.ValueSpec, kind string) {
		for _, spec := range specs {
			for _, name := range spec.Names {
				add(spec, name, kind, nil)
			}
		}
	}
	for _, item := range items {
		switch item := item.(type) {
		case *syntax.FuncDecl:
			add(item, item.Name, "function", nil)
		case *syntax.TypeDecl:
			if item.Grouped {
				for _, spec := range item.Specs {
					add(spec, spec.Name, "type", fields(spec.Type))
				}
			} else {
				add(item, item.Name, "type", fields(item.Type))
			}
		case *syntax.EnumDecl:
			var children []DocumentSymbol
			for _, variant := range item.Variants {
				if variant.Name != nil && !variant.Name.Missing {
					children = append(children, DocumentSymbol{Name: variant.Name.Name, Kind: "variant", Range: Location{File: file, Span: variant.Span()}, Selection: Location{File: file, Span: variant.Name.Span()}})
				}
			}
			add(item, item.Name, "enum", children)
		case *syntax.TraitDecl:
			var children []syntax.Item
			for _, child := range item.Items {
				children = append(children, child)
			}
			add(item, item.Name, "trait", documentSymbols(file, children))
		case *syntax.ImplDecl:
			if item.Type == nil || hasIncompleteSyntax(item.Type) {
				continue
			}
			name := "impl " + printer.Source(item.Type)
			if item.Trait != nil {
				name = "impl " + printer.Source(item.Trait) + " for " + printer.Source(item.Type)
			}
			result = append(result, DocumentSymbol{Name: name, Kind: "type", Range: Location{File: file, Span: item.Span()}, Selection: Location{File: file, Span: item.Type.Span()}, Children: documentSymbols(file, item.Items)})
		case *syntax.ModDecl:
			add(item, item.Name, "module", documentSymbols(file, item.Items))
		case *syntax.MacroRulesDecl:
			add(item, item.Name, "macro", nil)
		case *syntax.VarDecl:
			values(item.Specs, "variable")
		case *syntax.ConstDecl:
			values(item.Specs, "constant")
		case *syntax.DeclareDecl:
			result = append(result, documentSymbols(file, []syntax.Item{item.Decl})...)
		}
	}
	return result
}
