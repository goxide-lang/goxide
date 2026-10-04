package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// needsAssignmentFrame identifies promoted targets whose implicit pointer path
// cannot be spelled in the importing package. Keep these assignments intact:
// Go can stage the hidden operands without exposing any private selector.
func (l *lowering) needsAssignmentFrame(s *ast.AssignStmt) bool {
	for _, lhs := range s.Lhs {
		if l.placeNeedsFrame(lhs) {
			return true
		}
	}
	return false
}

// Follow only storage paths that place would expand. Pointer, slice and map
// operands are evaluated as ordinary rvalues, whose promoted selectors remain
// legal Go expressions. An index/key or call argument is never a destination.
func (l *lowering) placeNeedsFrame(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.ParenExpr:
		return l.placeNeedsFrame(x.X)
	case *ast.IndexExpr:
		if _, array := underlyingType(l.info.TypeOf(x.X)).(*types.Array); array {
			return l.placeNeedsFrame(x.X)
		}
	case *ast.SelectorExpr:
		sel := l.info.Selections[x]
		if sel == nil || sel.Kind() != types.FieldVal {
			return false
		}
		t := underlyingType(l.info.TypeOf(x.X))
		private := false
		path := sel.Index()
		for _, i := range path[:len(path)-1] {
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem().Underlying()
			}
			f := t.(*types.Struct).Field(i)
			private = private || (!f.Exported() && f.Pkg() != l.pkg)
			t = f.Type().Underlying()
			if _, pointer := t.(*types.Pointer); pointer && private {
				return true
			}
		}
		if _, pointer := underlyingType(l.info.TypeOf(x.X)).(*types.Pointer); !pointer {
			return l.placeNeedsFrame(x.X)
		}
	}
	return false
}

// frameType binds types at file scope so source locals cannot shadow imports
// or payload type names used by compiler helper signatures.
func (l *lowering) frameType(t types.Type) (ast.Expr, error) {
	t = types.Unalias(t)
	if name := l.frameTypes[t]; name != "" {
		return ast.NewIdent(name), nil
	}
	spelling, ok := l.typeSpelling(t)
	if !ok {
		return nil, l.fail(l.file, "assignment propagation helper type is not nameable in this file")
	}
	name := "__goxide_qframe_type_" + l.cfg.Namespace + l.temp()
	if l.frameTypes == nil {
		l.frameTypes = map[types.Type]string{}
	}
	l.frameTypes[t] = name
	l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Assign: token.Pos(1), Type: parseExpr(spelling)}}})
	return ast.NewIdent(name), nil
}

func (l *lowering) assignmentFrame(s *ast.AssignStmt, ft *ast.FuncType) ([]ast.Stmt, error) {
	if _, err := l.target(ft); err != nil {
		return nil, err
	}
	result := ft.Results.List[0]
	if len(result.Names) == 0 {
		// Naming an originally unnamed result must not make an illegal source bare
		// return legal. Nested user closures have their own return targets.
		var bare *ast.ReturnStmt
		ast.Inspect(l.functionBodies[ft], func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if r, ok := n.(*ast.ReturnStmt); ok && len(r.Results) == 0 {
				bare = r
			}
			return true
		})
		if bare != nil {
			return nil, l.fail(bare, "bare return requires a named source result")
		}
		result.Names = []*ast.Ident{ast.NewIdent(l.temp())}
	} else if result.Names[0].Name == "_" {
		result.Names[0] = ast.NewIdent(l.temp())
	}
	if l.resultPointers == nil {
		l.resultPointers = map[*ast.FuncType]string{}
		l.functionPrologues = map[*ast.FuncType][]ast.Stmt{}
	}
	pointer := l.resultPointers[ft]
	if pointer == "" {
		pointer = l.temp()
		l.resultPointers[ft] = pointer
		l.functionPrologues[ft] = parseStatements(pointer + " := &" + result.Names[0].Name)
	}
	resultType, err := l.frameType(l.info.TypeOf(result.Type))
	if err != nil {
		return nil, err
	}
	if l.frameRuntime == "" {
		l.frameRuntime = "__goxide_qframe_runtime_" + l.cfg.Namespace
		spec := &ast.ImportSpec{Name: ast.NewIdent(l.frameRuntime), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote("github.com/hxsf/goxide/runtime")}}
		l.file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, l.file.Decls...)
		l.file.Imports = append(l.file.Imports, spec)
	}
	// Bind the generic instantiation outside user local scopes too.
	guardType := "__goxide_qframe_guard_" + l.cfg.Namespace + l.temp()
	l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(guardType), Assign: token.Pos(1), Type: &ast.IndexExpr{X: &ast.SelectorExpr{X: ast.NewIdent(l.frameRuntime), Sel: ast.NewIdent("Assignment")}, Index: resultType}}}})
	guard := l.temp()
	pre := parseStatements("var " + guard + " " + guardType + "; defer " + guard + ".Catch(" + pointer + ")")
	for i, x := range s.Lhs {
		v, e := l.frameExpr(x, ft, guard)
		if e != nil {
			return nil, e
		}
		s.Lhs[i] = v
	}
	for i, x := range s.Rhs {
		v, e := l.frameExpr(x, ft, guard)
		if e != nil {
			return nil, e
		}
		s.Rhs[i] = v
	}
	return []ast.Stmt{&ast.BlockStmt{List: append(pre, s)}}, nil
}

// frameExpr replaces only question calls. User expressions remain helper
// arguments evaluated in the original frame, including direct recover(). The
// complete original assignment retains Go's contextual types and place rules.
func (l *lowering) frameExpr(x ast.Expr, ft *ast.FuncType, guard string) (ast.Expr, error) {
	if x == nil || !contains(x, l.questions) {
		return x, nil
	}
	lower := func(p *ast.Expr) error { v, e := l.frameExpr(*p, ft, guard); *p = v; return e }
	switch n := x.(type) {
	case *ast.CallExpr:
		if b := l.builtin(n); b != nil && b.Pkg() != nil && b.Pkg().Path() == "unsafe" && (b.Name() == "Sizeof" || b.Name() == "Alignof" || b.Name() == "Offsetof") {
			return nil, l.fail(n, "? in unsafe compile-time operands is unsupported; lowering must not evaluate an unevaluated operand")
		}
		if id, ok := n.Fun.(*ast.Ident); ok {
			if d, question := l.questions[id.Name]; question {
				original := n.Args[0]
				inputType, e := l.frameType(l.info.TypeOf(original))
				if e != nil {
					return nil, e
				}
				outputType, e := l.frameType(l.info.TypeOf(n))
				if e != nil {
					return nil, e
				}
				argName := l.temp()
				arg := ast.NewIdent(argName)
				l.info.Types[arg] = l.info.Types[original]
				call := *n
				call.Args = []ast.Expr{arg}
				flow, e := l.questionFlow(&call, arg, ft, d)
				if e != nil {
					return nil, e
				}
				pre := flow.emit(func(value ast.Expr) ast.Stmt {
					return &ast.ExprStmt{X: &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(guard), Sel: ast.NewIdent("Fail")}, Args: []ast.Expr{value}}}
				})
				operand, e := l.frameExpr(original, ft, guard)
				if e != nil {
					return nil, e
				}
				fn := &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{ast.NewIdent(argName)}, Type: inputType}}}, Results: &ast.FieldList{List: []*ast.Field{{Type: outputType}}}}, Body: &ast.BlockStmt{List: append(pre, &ast.ReturnStmt{Results: []ast.Expr{flow.value}})}}
				return &ast.CallExpr{Fun: fn, Args: []ast.Expr{operand}}, nil
			}
		}
		if e := lower(&n.Fun); e != nil {
			return nil, e
		}
		for i := range n.Args {
			if e := lower(&n.Args[i]); e != nil {
				return nil, e
			}
		}
	case *ast.ParenExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
	case *ast.SelectorExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
	case *ast.StarExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
	case *ast.UnaryExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
	case *ast.BinaryExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
		if e := lower(&n.Y); e != nil {
			return nil, e
		}
	case *ast.IndexExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
		if e := lower(&n.Index); e != nil {
			return nil, e
		}
	case *ast.IndexListExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
		for i := range n.Indices {
			if e := lower(&n.Indices[i]); e != nil {
				return nil, e
			}
		}
	case *ast.SliceExpr:
		for _, p := range []*ast.Expr{&n.X, &n.Low, &n.High, &n.Max} {
			if e := lower(p); e != nil {
				return nil, e
			}
		}
	case *ast.TypeAssertExpr:
		if e := lower(&n.X); e != nil {
			return nil, e
		}
	case *ast.CompositeLit:
		for i := range n.Elts {
			if e := lower(&n.Elts[i]); e != nil {
				return nil, e
			}
		}
	case *ast.KeyValueExpr:
		if e := lower(&n.Key); e != nil {
			return nil, e
		}
		if e := lower(&n.Value); e != nil {
			return nil, e
		}
	default:
		return nil, l.fail(x, "unsupported expression in assignment propagation")
	}
	return x, nil
}
