package matchexpr

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// compositeFunctionTypes identifies func tokens used as types, by parsing the
// literal's complete type rather than guessing from the token before func.
// Bodies are deliberately absent from each parse: enum/match syntax inside a
// literal cannot change which tokens belong to its type.
func compositeFunctionTypes(source []byte, ts []tok, pairs map[int]int) map[int]bool {
	types := map[int]bool{}
	reverse := map[int]int{}
	for start, end := range pairs {
		reverse[end] = start
	}
	funcs := make([]int, len(ts)+1)
	for i, t := range ts {
		funcs[i+1] = funcs[i]
		if t.k == token.FUNC {
			funcs[i+1]++
		}
	}
	for open, t := range ts {
		if t.k != token.LBRACE {
			continue
		}
		for start := open - 1; start >= 0; start-- {
			group := false
			if paired, ok := reverse[start]; ok {
				start, group = paired, true
			}
			if !group && (ts[start].k == token.SEMICOLON || ts[start].k == token.LBRACE || ts[start].k == token.RBRACE) {
				break
			}
			if funcs[open] == funcs[start] {
				continue
			}
			switch ts[start].k {
			case token.IDENT, token.LBRACK, token.MAP, token.STRUCT:
			default:
				continue
			}
			expr, err := parser.ParseExpr(string(source[ts[start].a:t.a]) + "{}")
			literal, ok := expr.(*ast.CompositeLit)
			if err == nil && ok {
				markFunctionTypes(literal.Type, nil, ts[start].a, types)
			}
		}
	}
	return types
}

// markFunctionTypes maps parser positions back to source byte offsets. The
// outer signature of a real function is excluded. A FuncLit inside a type's
// expression (for example an array length) is also a real function boundary.
func markFunctionTypes(root ast.Node, outer *ast.FuncType, offset int, found map[int]bool) {
	ast.Inspect(root, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if typ, ok := n.(*ast.FuncType); ok && typ != outer {
			found[offset+int(typ.Func)-1] = true
		}
		return true
	})
}

// functionSignature parses a declaration or literal header and returns the
// adjustment from parser positions to offsets within that original header.
func functionSignature(header string) (*ast.FuncType, int) {
	if expr, err := parser.ParseExpr(header + "{}"); err == nil {
		if literal, ok := expr.(*ast.FuncLit); ok {
			return literal.Type, 0
		}
	}
	const prefix = "package p\n"
	if file, err := parser.ParseFile(token.NewFileSet(), "", prefix+header+"{}", 0); err == nil && len(file.Decls) == 1 {
		if declaration, ok := file.Decls[0].(*ast.FuncDecl); ok {
			return declaration.Type, -len(prefix)
		}
	}
	return nil, 0
}
