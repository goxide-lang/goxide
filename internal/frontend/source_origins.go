package frontend

import (
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// attachDeriveOrigins records causal provenance, not a claim that generated
// helper syntax appeared in the input. Field selector tokens take precedence.
func attachDeriveOrigins(dst *sourcemap.Map, src []byte, models []model, input *sourcemap.Map, inputFS *token.FileSet, generatedStart int) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "generated.go", src, 0)
	if err != nil {
		return
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fs.Position(fn.Pos()).Offset < generatedStart {
			continue
		}
		for _, m := range models {
			match := false
			for _, kind := range []string{"Serialize", "Deserialize", "Debug", "Clone", "Default", "From"} {
				if fn.Name.Name == kind+m.name && (m.attrs.derive[kind] || kind == "From" && m.from != "") {
					match = true
				}
			}
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				var b strings.Builder
				switch r := fn.Recv.List[0].Type.(type) {
				case *ast.Ident:
					b.WriteString(r.Name)
				case *ast.StarExpr:
					if id, ok := r.X.(*ast.Ident); ok {
						b.WriteString(id.Name)
					}
				}
				match = m.attrs.derive["Error"] && b.String() == m.name && (fn.Name.Name == "Error" || fn.Name.Name == "Unwrap")
			}
			if !match {
				continue
			}
			origin := input.Lookup(inputFS.Position(m.spec.Name.Pos()).Offset)
			if origin != nil {
				dst.Attach(fs.Position(fn.Name.Pos()).Offset, fs.Position(fn.Name.End()).Offset, origin.Origin, origin.Stack, "derive helper name")
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if !selectorOfModel(sel, m.name) {
					return true
				}
				for _, f := range m.fields {
					if sel.Sel.Name == f.name {
						p := inputFS.Position(f.typ.Pos())
						o := input.Lookup(p.Offset)
						if o != nil {
							span := input.SpanFor(p.Offset, inputFS.Position(f.typ.End()).Offset)
							dst.Attach(fs.Position(sel.Pos()).Offset, fs.Position(sel.End()).Offset, span, o.Stack, "derive field")
						}
					}
				}
				return true
			})
		}
	}
}

// Verify the selector receiver's lexical declaration, including shadowing in
// nested helper closures. A coincidentally equal serializer API name is not a
// field of the user's model.
func selectorOfModel(sel *ast.SelectorExpr, model string) bool {
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Obj == nil {
		return false
	}
	var typ ast.Expr
	switch d := id.Obj.Decl.(type) {
	case *ast.Field:
		typ = d.Type
	case *ast.ValueSpec:
		typ = d.Type
	}
	if p, ok := typ.(*ast.StarExpr); ok {
		typ = p.X
	}
	t, ok := typ.(*ast.Ident)
	return ok && t.Name == model
}
