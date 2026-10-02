package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
)

// expressionFact is captured before lowering mutates syntax. go/types records
// contextual types for untyped shifts/comparisons, so TypeOf alone cannot tell
// whether a temporary would default or erase a private named scalar identity.
// This records provenance for our bounded expression planner, not a second
// complete Go type checker. Unsupported syntax never uses an inferred fact.
type expressionFact struct {
	typ         types.Type
	untyped     bool
	boolean     bool
	constant    bool
	addressable bool
}

func (l *lowering) captureExpressionFacts() {
	l.expressionFacts = map[ast.Expr]expressionFact{}
	ast.Inspect(l.file, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok {
			l.expressionFact(x)
		}
		return true
	})
}
func (l *lowering) expressionFact(x ast.Expr) expressionFact {
	if f, ok := l.expressionFacts[x]; ok {
		return f
	}
	tv := l.info.Types[x]
	f := expressionFact{typ: tv.Type, constant: tv.Value != nil, addressable: tv.Addressable()}
	if tv.Type != nil {
		if b, ok := tv.Type.Underlying().(*types.Basic); ok {
			f.boolean = b.Info()&types.IsBoolean != 0
		}
	}
	untypedObject := func(id *ast.Ident) bool {
		if c, ok := l.info.Uses[id].(*types.Const); ok {
			if b, ok := c.Type().(*types.Basic); ok {
				return b.Info()&types.IsUntyped != 0
			}
		}
		if _, ok := l.info.Uses[id].(*types.Nil); ok {
			return true
		}
		return false
	}
	switch n := x.(type) {
	case *ast.BasicLit:
		f.untyped = true
	case *ast.Ident:
		f.untyped = untypedObject(n)
	case *ast.SelectorExpr:
		if l.info.Selections[n] == nil {
			f.untyped = untypedObject(n.Sel)
		}
	case *ast.ParenExpr:
		f.untyped = l.expressionFact(n.X).untyped
	case *ast.UnaryExpr:
		switch n.Op {
		case token.ADD, token.SUB, token.XOR, token.NOT:
			f.untyped = l.expressionFact(n.X).untyped
		}
	case *ast.BinaryExpr:
		switch n.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			f.untyped = true
			f.boolean = true
		case token.SHL, token.SHR:
			f.untyped = l.expressionFact(n.X).untyped
		default:
			f.untyped = l.expressionFact(n.X).untyped && l.expressionFact(n.Y).untyped
		}
	}
	l.expressionFacts[x] = f
	return f
}
