package refs

import (
	"go/ast"
	"go/token"
	"go/types"
)

// refineLocalQualifications retains a conservative summary for captured,
// address-taken and loop-carried slots, and records lexical point facts for
// ordinary local slots. It is deliberately intraprocedural: no call effects or
// whole-program alias graph is inferred.
func (c *checker) refineLocalQualifications() {
	summary := c.qualifications
	pinned := map[types.Object]bool{}
	pin := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				pinned[c.binding(id)] = true
			}
			return true
		})
	}
	ast.Inspect(c.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			ast.Inspect(n.Body, func(x ast.Node) bool {
				if id, ok := x.(*ast.Ident); ok {
					obj := c.binding(id)
					if obj != nil && (obj.Pos() < n.Pos() || obj.Pos() > n.End()) {
						pinned[obj] = true
					}
				}
				return true
			})
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				pin(n.X)
			}
		case *ast.ForStmt:
			pin(n)
		case *ast.RangeStmt:
			pin(n)
		case *ast.SwitchStmt:
			pin(n)
		case *ast.TypeSwitchStmt:
			pin(n)
		case *ast.SelectStmt:
			pin(n)
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				pin(c.file)
			}
		}
		return true
	})
	c.conservativeObjects = pinned
	type env map[types.Object]*qualification
	clone := func(in env) env {
		out := env{}
		for o, q := range in {
			out[o] = q
		}
		return out
	}
	merge := func(a, b env) env {
		out := clone(a)
		for o, q := range b {
			out[o] = join(out[o], q)
		}
		return out
	}
	c.pointQualifications = map[*ast.Ident]*qualification{}
	var expr func(ast.Node, env)
	var stmt func(ast.Stmt, env) env
	expr = func(n ast.Node, e env) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				obj := c.binding(id)
				q := e[obj]
				if pinned[obj] {
					q = summary[obj]
				}
				c.pointQualifications[id] = q
			}
			if f, ok := n.(*ast.FuncLit); ok {
				stmt(f.Body, clone(e))
				return false
			}
			return true
		})
	}
	bind := func(lhs ast.Expr, q *qualification, e env) {
		if id, ok := unparen(lhs).(*ast.Ident); ok {
			obj := c.binding(id)
			if !pinned[obj] {
				e[obj] = copied(c.typ(lhs), q)
			}
		}
	}
	stmt = func(s ast.Stmt, e env) env {
		if s == nil {
			return e
		}
		switch s := s.(type) {
		case *ast.BlockStmt:
			for _, s := range s.List {
				e = stmt(s, e)
			}
		case *ast.AssignStmt:
			for _, r := range s.Rhs {
				expr(r, e)
			}
			for _, l := range s.Lhs {
				expr(l, e)
			}
			if len(s.Lhs) == len(s.Rhs) {
				qs := make([]*qualification, len(s.Rhs))
				for i, r := range s.Rhs {
					qs[i] = c.quality(r)
				}
				for i, l := range s.Lhs {
					bind(l, qs[i], e)
				}
			}
		case *ast.DeclStmt:
			d, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				break
			}
			for _, sp := range d.Specs {
				v, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, r := range v.Values {
					expr(r, e)
				}
				if len(v.Names) == len(v.Values) {
					for i, l := range v.Names {
						bind(l, c.quality(v.Values[i]), e)
					}
				}
			}
		case *ast.IfStmt:
			e = stmt(s.Init, e)
			expr(s.Cond, e)
			a := stmt(s.Body, clone(e))
			b := stmt(s.Else, clone(e))
			e = merge(a, b)
		default:
			expr(s, e)
		}
		return e
	}
	for _, d := range c.file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Body != nil {
			stmt(f.Body, env{})
		}
	}
}
