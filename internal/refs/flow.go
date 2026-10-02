package refs

import (
	"go/ast"
	"go/token"
	"go/types"
)

// refineLocalQualifications records current values separately from the lifetime
// qualification of exposed slots. Unsupported control flow retains its summary.
// It infers neither callee effects nor a whole-program alias graph.
func (c *checker) refineLocalQualifications() {
	summary := c.qualifications
	pinned := map[types.Object]bool{}
	exposed := map[types.Object]bool{}
	addressUses := map[*ast.Ident]bool{}
	pin := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				pinned[c.binding(id)] = true
			}
			return true
		})
	}
	// Taking the address of a pointee/slice element does not expose the local
	// pointer/header slot. Struct fields and array elements do expose their root.
	var pinAddress func(ast.Expr)
	pinAddress = func(e ast.Expr) {
		switch e := unparen(e).(type) {
		case *ast.Ident:
			exposed[c.binding(e)] = true
			addressUses[e] = true
		case *ast.SelectorExpr:
			if _, pointer := c.typ(e.X).Underlying().(*types.Pointer); !pointer {
				pinAddress(e.X)
			}
		case *ast.IndexExpr:
			if _, array := c.typ(e.X).Underlying().(*types.Array); array {
				pinAddress(e.X)
			}
		}
	}
	unsupportedLoop := func(n ast.Node) bool {
		unsupported := false
		ast.Inspect(n, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				unsupported = true
			}
			return !unsupported
		})
		return unsupported
	}
	ast.Inspect(c.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			ast.Inspect(n.Body, func(x ast.Node) bool {
				if id, ok := x.(*ast.Ident); ok {
					obj := c.binding(id)
					if obj != nil && (obj.Pos() < n.Pos() || obj.Pos() > n.End()) {
						exposed[obj] = true
					}
				}
				return true
			})
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				pinAddress(n.X)
			}
		case *ast.SelectorExpr:
			if c.implicitReceiverAddress(n) {
				pinAddress(n.X)
			}
		case *ast.ForStmt:
			if unsupportedLoop(n) {
				pin(n)
			}
		case *ast.RangeStmt:
			if unsupportedLoop(n) {
				pin(n)
			}
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
	c.conservativeObjects = map[types.Object]bool{}
	for obj := range pinned {
		c.conservativeObjects[obj] = true
	}
	for obj := range exposed {
		c.conservativeObjects[obj] = true
	}
	type env map[types.Object]*qualification
	clone := func(in env) env {
		if in == nil {
			return nil
		}
		out := env{}
		for o, q := range in {
			out[o] = q
		}
		return out
	}
	merge := func(a, b env) env {
		if a == nil {
			return clone(b)
		}
		out := clone(a)
		for o, q := range b {
			out[o] = join(out[o], q)
		}
		return out
	}
	same := func(a, b env) bool {
		if (a == nil) != (b == nil) {
			return false
		}
		for o, q := range a {
			if !sameQ(q, b[o]) {
				return false
			}
		}
		for o, q := range b {
			if !sameQ(q, a[o]) {
				return false
			}
		}
		return true
	}
	type loopFlow struct {
		label             string
		breaks, continues env
	}
	var loops []*loopFlow
	labels := map[ast.Stmt]string{}
	c.pointQualifications = map[*ast.Ident]*qualification{}
	// No callee/alias effects are inferred. A possible slot mutation invalidates
	// current facts before annotating the whole expression, including its early
	// operands; Go does not order every ordinary read relative to calls.
	invalidate := func(e env) {
		for obj := range exposed {
			e[obj] = summary[obj]
		}
	}
	hasEffect := func(n ast.Node) bool {
		found := false
		ast.Inspect(n, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
				found = true
			}
			if _, ok := n.(*ast.SendStmt); ok {
				found = true
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if c.info.Types[call.Fun].IsType() {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok {
					if _, builtin := c.binding(id).(*types.Builtin); builtin {
						return true
					}
				}
				found = true
			}
			return !found
		})
		return found
	}
	var expr func(ast.Node, env)
	var stmt func(ast.Stmt, env) env
	expr = func(n ast.Node, e env) {
		if n == nil {
			return
		}
		if hasEffect(n) {
			invalidate(e)
		}
		ast.Inspect(n, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				obj := c.binding(id)
				q := e[obj]
				if pinned[obj] || addressUses[id] {
					q = summary[obj]
				}
				c.pointQualifications[id] = q
			}
			if f, ok := n.(*ast.FuncLit); ok {
				outer := loops
				loops = nil
				entry := clone(e)
				for obj := range exposed {
					if obj != nil && (obj.Pos() < f.Pos() || obj.Pos() > f.End()) {
						entry[obj] = summary[obj]
					}
				}
				stmt(f.Body, entry)
				loops = outer
				return false
			}
			return true
		})
	}
	bind := func(lhs ast.Expr, q *qualification, e env) {
		if obj, path, ok := c.localValuePath(lhs); ok && !pinned[obj] && (len(path) == 0 || !exposed[obj]) {
			e[obj] = withFieldPath(e[obj], path, copied(c.typ(lhs), q))
		}
	}
	stmt = func(s ast.Stmt, e env) env {
		if s == nil || e == nil {
			return e
		}
		switch s := s.(type) {
		case *ast.BlockStmt:
			for _, s := range s.List {
				e = stmt(s, e)
			}
		case *ast.AssignStmt:
			indirect := false
			for _, l := range s.Lhs {
				if typ := c.typ(l); typ != nil {
					if _, scalar := typ.Underlying().(*types.Basic); scalar {
						continue
					}
				}
				if _, _, local := c.localValuePath(l); !local {
					if id, ok := l.(*ast.Ident); !ok || id.Name != "_" {
						indirect = true
					}
				}
			}
			if hasEffect(s) || indirect {
				invalidate(e)
			}
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
			if indirect {
				invalidate(e)
			}
		case *ast.DeclStmt:
			if hasEffect(s) {
				invalidate(e)
			}
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
				if len(v.Values) == 0 {
					// A declaration executes afresh on each loop iteration. Its
					// zero value does not retain the previous iteration's view.
					for _, l := range v.Names {
						bind(l, nil, e)
					}
				}
				if len(v.Names) == len(v.Values) {
					for i, l := range v.Names {
						bind(l, c.quality(v.Values[i]), e)
					}
				}
			}
		case *ast.ReturnStmt:
			expr(s, e)
			return nil
		case *ast.BranchStmt:
			if s.Tok == token.BREAK || s.Tok == token.CONTINUE {
				for i := len(loops) - 1; i >= 0; i-- {
					loop := loops[i]
					if s.Label != nil && loop.label != s.Label.Name {
						continue
					}
					if s.Tok == token.BREAK {
						loop.breaks = merge(loop.breaks, e)
					} else {
						loop.continues = merge(loop.continues, e)
					}
					return nil
				}
			}
		case *ast.LabeledStmt:
			labels[s.Stmt] = s.Label.Name
			e = stmt(s.Stmt, e)
		case *ast.ForStmt:
			if unsupportedLoop(s) {
				expr(s, e)
				break
			}
			initial := stmt(s.Init, e)
			header := clone(initial)
			for {
				expr(s.Cond, header)
				loop := &loopFlow{label: labels[s]}
				loops = append(loops, loop)
				body := stmt(s.Body, clone(header))
				loops = loops[:len(loops)-1]
				back := stmt(s.Post, merge(body, loop.continues))
				next := merge(initial, back)
				if same(header, next) {
					e = loop.breaks
					if s.Cond != nil {
						e = merge(e, header)
					}
					break
				}
				header = next
			}
		case *ast.RangeStmt:
			if unsupportedLoop(s) {
				expr(s, e)
				break
			}
			expr(s.X, e)
			// Go evaluates the range source once. Preserve its qualification even
			// if a local source variable is rebound inside the loop.
			value := projection(c.quality(s.X), "")
			var key *qualification
			if _, ok := c.typ(s.X).Underlying().(*types.Map); ok {
				key = value
			}
			_, iterator := c.typ(s.X).Underlying().(*types.Signature)
			_, receiving := c.typ(s.X).Underlying().(*types.Chan)
			iterationEffect := iterator || receiving
			if iterationEffect {
				invalidate(e)
			}
			initial, header := clone(e), clone(e)
			for {
				body := clone(header)
				if iterationEffect {
					invalidate(body)
				}
				for i, target := range []ast.Expr{s.Key, s.Value} {
					if target == nil {
						continue
					}
					expr(target, body)
					q := value
					if i == 0 {
						q = key
					}
					bind(target, q, body)
				}
				loop := &loopFlow{label: labels[s]}
				loops = append(loops, loop)
				body = stmt(s.Body, body)
				loops = loops[:len(loops)-1]
				next := merge(initial, merge(body, loop.continues))
				if same(header, next) {
					e = merge(header, loop.breaks)
					break
				}
				header = next
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
