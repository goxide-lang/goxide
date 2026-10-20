package resolve

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Parsing cannot distinguish builtin new/make from a same-named function.
// Validate template type slots only after their callee has a resolved binding.
func (h *hygiene) validateAllocations() {
	for _, pkg := range h.result.Packages {
		for _, file := range pkg.Files {
			ast.Walk(file.AST, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !h.expanded.Allocations[call] || len(call.Args) == 0 {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				symbol := file.References[id.ID()].Symbol
				if symbol == nil || symbol.Kind != Builtin || (symbol.Name.Name != "new" && symbol.Name.Name != "make") {
					return true
				}
				if diagnostic := h.allocationType(file, call.Args[0]); diagnostic != nil {
					h.result.Diagnostics = append(h.result.Diagnostics, *diagnostic)
				}
				return true
			})
		}
	}
}

func (h *hygiene) allocationType(file *File, node ast.Node) *Diagnostic {
	fail := func(message string, use expand.Origin) *Diagnostic {
		d := &Diagnostic{File: file.Name, Span: node.Span(), Code: "macro-allocation", Message: message}
		h.diagnostic(node, d)
		if use.File != nil {
			d.File, d.Span, d.Definition = h.names[use.File], use.Span, use
			d.Origin.File, d.Origin.Span = d.File, d.Span
		}
		return d
	}
	if capture, ok := h.expanded.Captures[node]; ok {
		if capture.Kind == "type" {
			return nil
		}
		return fail("allocation type slot rejects captured types from "+capture.Kind+" fragments; use a type fragment", capture.Use)
	}
	switch n := node.(type) {
	case *ast.TypeExpr:
		return h.allocationType(file, n.Type)
	case *ast.ParenExpr:
		return h.allocationType(file, n.X)
	case *ast.ParenType:
		return h.allocationType(file, n.Type)
	case *ast.StarExpr:
		return h.allocationType(file, n.X)
	case *ast.PointerType:
		return h.allocationType(file, n.Elem)
	case *ast.SliceType:
		return h.allocationType(file, n.Elem)
	case *ast.ArrayType:
		// Array lengths are expression slots, including complete captures.
		return h.allocationType(file, n.Elem)
	case *ast.MapType:
		if diagnostic := h.allocationType(file, n.Key); diagnostic != nil {
			return diagnostic
		}
		return h.allocationType(file, n.Value)
	case *ast.ChanType:
		return h.allocationType(file, n.Elem)
	case *ast.PathType:
		if len(n.Path.Segments) == 1 && n.Path.Segments[0].Args == nil {
			return h.allocationType(file, n.Path.Segments[0].Name)
		}
		return fail("allocation type slots require fixed predeclared types; imported types require direct function calls", expand.Origin{})
	case *ast.Ident:
		symbol := file.References[n.ID()].Symbol
		if symbol != nil && symbol.Kind == Type && symbol.Decl.File == "" && len(symbol.Target) == 0 {
			if _, ok := types.Universe.Lookup(symbol.GoName).(*types.TypeName); ok {
				return nil
			}
		}
		if symbol != nil && symbol.Kind != Type && symbol.Kind != TypeParam {
			if symbol.Name.Ctx != 0 {
				return fail("allocation type slot cannot use a template-local binding", expand.Origin{})
			}
			return fail("allocation type slot requires a definition-site predeclared type", expand.Origin{})
		}
		return fail("allocation type slots require fixed predeclared types", expand.Origin{})
	case *ast.IndexExpr:
		return fail("allocation type slots require fixed predeclared types, not generic instances", expand.Origin{})
	case *ast.PathExpr, *ast.SelectorExpr:
		return fail("imported allocation types require direct function calls", expand.Origin{})
	case *ast.StructType, *ast.InterfaceType, *ast.FuncType, *ast.FuncLit:
		return fail("unsupported anonymous type in a template allocation type slot", expand.Origin{})
	default:
		return fail("new(expr) is unsupported in macro templates; allocation requires a type slot", expand.Origin{})
	}
}
