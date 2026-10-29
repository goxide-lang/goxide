package lower

import (
	goast "go/ast"
	gotoken "go/token"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type stableConstAlias struct {
	value syntax.Expr
	typ   syntax.Type
	iota  int
}

// Symbolic constants are supplied to a published Go body as ordinary int
// parameters. Inline local aliases at their uses, keeping a blank constant at
// each original spec so later iota values retain their source group positions.
func (e *emitter) stableConstDecl(node *syntax.ConstDecl) *goast.GenDecl {
	decl := &goast.GenDecl{TokPos: e.pos(node), Tok: gotoken.CONST}
	if node.Grouped {
		decl.Lparen, decl.Rparen = e.pos(node)+gotoken.Pos(len("const")), e.end(node)-1
	}
	var values []syntax.Expr
	var typ syntax.Type
	for index, spec := range node.Specs {
		if len(spec.Values) != 0 {
			values, typ = spec.Values, spec.Type
		}
		symbolic := false
		for _, value := range values {
			symbolic = symbolic || e.stableConstDependent(value)
		}
		if !symbolic {
			decl.Specs = append(decl.Specs, e.valueDecl(spec, gotoken.CONST, false, []*syntax.ValueSpec{spec}).Specs...)
			continue
		}
		for _, value := range values {
			e.retainStableArrayBindings(value)
		}
		for position, name := range spec.Names {
			if position < len(values) {
				symbol := e.file.Definitions[name.ID()]
				if symbol != nil {
					e.constTemplate.aliases[symbol] = stableConstAlias{value: values[position], typ: typ, iota: index}
				}
			}
		}
		decl.Specs = append(decl.Specs, &goast.ValueSpec{
			Names:  []*goast.Ident{goast.NewIdent("_")},
			Values: []goast.Expr{&goast.BasicLit{Kind: gotoken.INT, Value: "0"}},
		})
	}
	return decl
}

func (e *emitter) stableConstDependent(node syntax.Expr) bool {
	found := false
	syntax.Walk(node, func(node syntax.Node) bool {
		if found {
			return false
		}
		switch node := node.(type) {
		case *syntax.Ident:
			symbol := e.file.References[node.ID()].Symbol
			_, alias := e.constTemplate.aliases[symbol]
			found = alias || e.info.ConstParams[symbol] != nil
		case *syntax.CallExpr:
			symbol := e.valueSymbol(node.Fun)
			if len(node.Args) == 1 && symbol != nil && symbol.Kind == resolve.Builtin && (symbol.Name.Name == "len" || symbol.Name.Name == "cap") {
				array, _ := e.stableArrayShape(node.Args[0])
				found = array != nil && array.Length.Param != nil
			}
		}
		return !found
	})
	return found
}
