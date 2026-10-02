package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

const matchInfer = "__hgo_match_expr_infer"
const matchContext = "__hgo_match_expr_context"
const matchOperand = "__hgo_match_expr_operand_context"
const matchZero = "__hgo_match_expr_zero"
const matchReturn = "__hgo_match_expr_return"
const matchPending = "__hgo_match_expr_pending"

// pendingMatchType distinguishes unresolved inference from a user interface.
// No provisional type facts are used for emission: resolving a slot restarts
// whole-package typing before propagation captures contextual expression facts.
func pendingMatchType(t types.Type) bool {
	return t != nil && strings.Contains(types.TypeString(t, nil), matchPending)
}

type matchResultError struct{ origin, message string }

func (e *matchResultError) Error() string {
	return e.origin + ": expression match: " + e.message + "; use an explicit typed local initializer"
}
func (e *matchResultError) SourceDiagnostic() bool {
	return !strings.HasPrefix(e.origin, "[generated/unmapped]")
}
func matchResultFailure(call *ast.CallExpr, message string) error {
	origin := "expression match"
	if len(call.Args) > 0 {
		if s, ok := call.Args[len(call.Args)-1].(*ast.BasicLit); ok {
			if v, e := strconv.Unquote(s.Value); e == nil {
				origin = v
			}
		}
	}
	return &matchResultError{origin, message}
}

// resolveMatchResult resolves one slot at a time. Questions and later matches
// may depend on it, so the caller alternates this pass with ? signature typing.
func (l *lowering) resolveMatchResult() (bool, error) {
	var slots []*ast.ValueSpec
	ast.Inspect(l.file, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if !ok || len(v.Names) != 1 || len(v.Values) != 1 {
			return true
		}
		call, ok := v.Values[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if ok && (id.Name == matchInfer || id.Name == matchContext || id.Name == matchOperand) {
			slots = append(slots, v)
		}
		return true
	})

	// Resolve concrete arms before defaulting constants. This lets a sibling
	// match supply the operator identity without inventing a common type.
	preferred := map[*ast.ValueSpec]bool{}
	for _, slot := range slots {
		obj := l.info.Defs[slot.Names[0]]
		ast.Inspect(l.file, func(n ast.Node) bool {
			a, ok := n.(*ast.AssignStmt)
			if !ok || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
				return true
			}
			id, ok := a.Lhs[0].(*ast.Ident)
			if !ok || l.info.ObjectOf(id) != obj {
				return true
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
			if types.CheckExpr(l.fs, l.pkg, a.Rhs[0].Pos(), a.Rhs[0], info) == nil {
				t := info.Types[a.Rhs[0]].Type
				if t != nil && !pendingMatchType(t) {
					b, ok := t.(*types.Basic)
					if !ok || b.Info()&types.IsUntyped == 0 {
						preferred[slot] = true
					}
				}
			}
			return true
		})
	}
	sort.SliceStable(slots, func(i, j int) bool { return preferred[slots[i]] && !preferred[slots[j]] })
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(l.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	var pending error
	for _, slot := range slots {
		call := slot.Values[0].(*ast.CallExpr)
		fail := func(s string) error { return matchResultFailure(call, s) }
		obj := l.info.Defs[slot.Names[0]]
		var arms []ast.Expr
		ast.Inspect(l.file, func(n ast.Node) bool {
			a, ok := n.(*ast.AssignStmt)
			if !ok || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
				return true
			}
			id, ok := a.Lhs[0].(*ast.Ident)
			if ok && l.info.ObjectOf(id) == obj {
				arms = append(arms, a.Rhs[0])
			}
			return true
		})
		if len(arms) == 0 {
			return false, fail("no result arms available for inference")
		}
		var target types.Type
		if call.Fun.(*ast.Ident).Name == matchContext {
			id, ok := call.Args[0].(*ast.Ident)
			if !ok {
				return false, fail("assignment target must be a local variable")
			}
			v, ok := l.info.ObjectOf(id).(*types.Var)
			if !ok || v.Parent() == l.pkg.Scope() {
				return false, fail("assignment target must be an existing local variable")
			}
			target = v.Type()
		}

		if call.Fun.(*ast.Ident).Name == matchOperand {
			target = l.info.TypeOf(call.Args[0])
			if rc, ok := call.Args[0].(*ast.CallExpr); ok {
				if id, ok := rc.Fun.(*ast.Ident); ok && id.Name == matchReturn {
					// Resolve the enclosing user signature, not a copied spelling
					// that a local declaration may shadow or a package cannot name.
					var nearest *types.Signature
					end := token.Pos(1 << 30)
					ast.Inspect(l.file, func(n ast.Node) bool {
						if n == nil {
							return false
						}
						var ft *types.Signature
						switch f := n.(type) {
						case *ast.FuncDecl:
							if obj := l.info.Defs[f.Name]; obj != nil {
								ft, _ = obj.Type().(*types.Signature)
							}
						case *ast.FuncLit:
							if matchAtom(f) == nil {
								ft, _ = l.info.TypeOf(f).(*types.Signature)
							}
						}
						if ft != nil && n.Pos() < slot.Pos() && slot.End() < n.End() && n.End() <= end {
							nearest = ft
							end = n.End()
						}
						return true
					})
					if nearest != nil {
						if nearest.Results().Len() == 1 {
							target = nearest.Results().At(0).Type()
						}
					}
				}
			}
			kind, _ := strconv.Unquote(call.Args[1].(*ast.BasicLit).Value)

			if kind != "direct" && target != nil {
				basic, ok := target.Underlying().(*types.Basic)
				if !ok || (kind == "numeric" && basic.Info()&(types.IsNumeric|types.IsString) == 0) || (kind == "boolean" && basic.Info()&types.IsBoolean == 0) {
					target = nil
				}
			}
			if pendingMatchType(target) {
				target = nil
			}
		}
		if peer := l.matchPeerType(slot, parents); peer != nil {
			target = peer
		}
		var values []types.TypeAndValue
		blocked := false
		for _, arm := range arms {
			question := false
			ast.Inspect(arm, func(n ast.Node) bool {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				if id, ok := n.(*ast.Ident); ok && id.Name == marker {
					question = true
				}
				return true
			})
			if question {
				blocked = true
				break
			}
			// Check without an assignment context. In particular, constants, nil and
			// runtime shifts must not inherit the provisional pending-interface default.
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
			if err := types.CheckExpr(l.fs, l.pkg, arm.Pos(), arm, info); err != nil {
				blocked = true
				break
			}
			tv := info.Types[arm]
			if tv.Type == nil || pendingMatchType(tv.Type) {
				blocked = true
				break
			}
			values = append(values, tv)
		}
		if blocked || pendingMatchType(target) {
			pending = fail("result types are unresolved or an arm needs a type context")
			continue
		}
		if target == nil {
			for _, tv := range values {
				if b, ok := types.Unalias(tv.Type).(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
					continue
				}
				if target == nil {
					target = tv.Type
				} else if !types.Identical(target, tv.Type) {
					return false, fail("result arms have different concrete types; no implicit common type is inferred")
				}
			}
			if target == nil {
				for _, tv := range values {
					b, ok := types.Unalias(tv.Type).(*types.Basic)
					if !ok || b.Kind() == types.UntypedNil {
						return false, fail("nil-only or mixed untyped results need an explicit result type")
					}
					d := types.Default(tv.Type)
					if target == nil {
						target = d
					} else if !types.Identical(target, d) {
						return false, fail("untyped result arms have different default types")
					}
				}
			}
		}
		spelling, visible := l.typeSpelling(target)
		var typeExpr ast.Expr
		// Reuse the reference stage's concrete aliases instead of exposing its
		// generic nominal origin through a new public-looking type alias. Finish
		// must still restore &T/mut& T before permission checking.
		var referenceAliases []string
		for id, obj := range l.info.Defs {
			if tn, ok := obj.(*types.TypeName); ok && tn.IsAlias() && (strings.HasPrefix(id.Name, "__hgo_ref_read_") || strings.HasPrefix(id.Name, "__hgo_ref_write_")) && types.Identical(tn.Type(), target) {
				referenceAliases = append(referenceAliases, id.Name)
			}
		}
		sort.Strings(referenceAliases)
		if len(referenceAliases) > 0 {
			typeExpr = ast.NewIdent(referenceAliases[0])
			if l.cfg.MatchReferenceTemps != nil {
				l.cfg.MatchReferenceTemps[slot.Names[0].Name] = true
			}
			spelling = referenceAliases[0]
		} else if visible {
			// File-scope binding protects both package names and builtin types from
			// local shadowing at the match's use site.
			var err error
			typeExpr, err = l.frameType(target)
			if err != nil {
				return false, fail("result type cannot be bound in this file")
			}
		} else {
			spelling = types.TypeString(target, func(p *types.Package) string {
				if p == l.pkg {
					return ""
				}
				return p.Name()
			})
			tv, err := types.Eval(l.fs, l.pkg, slot.Pos(), spelling)
			if err != nil || !tv.IsType() || !types.Identical(target, tv.Type) {
				return false, fail("result type is private, shadowed, or not nameable at this location")
			}
			typeExpr = parseExpr(spelling)
		}
		// Exact contextual assignment checking also enforces constant range and
		// contextual shifts. AssignableTo alone lacks the constant's value.
		for _, arm := range arms {
			// Newly appended aliases enter scope on the next check; use the proven
			// spelling here and verify its identity at the arm's lexical position.
			tv, err := types.Eval(l.fs, l.pkg, arm.Pos(), spelling)
			if err != nil || !tv.IsType() || !types.Identical(target, tv.Type) {
				// Binding protects shadowed uses. Validate after restarting type checking
				// with the concrete declaration instead of inventing a captured spelling.
				continue
			}
			probe := &ast.CompositeLit{Type: &ast.ArrayType{Elt: parseExpr(spelling)}, Elts: []ast.Expr{arm}}
			if err := types.CheckExpr(l.fs, l.pkg, arm.Pos(), probe, nil); err != nil {
				message := err.Error()
				if te, ok := err.(types.Error); ok {
					message = te.Msg
				}
				return false, fail("arm is not assignable to the result type: " + message)
			}
		}
		slot.Type = typeExpr
		slot.Values = nil
		ast.Inspect(l.file, func(n ast.Node) bool {
			fn := matchAtom(n)
			if fn != nil {
				spec := fn.Body.List[0].(*ast.DeclStmt).Decl.(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
				if spec == slot {
					fn.Type.Results.List[0].Type = typeExpr
					return false
				}
			}
			return true
		})
		return true, nil
	}
	return false, pending
}

func matchSeedDecl(d ast.Decl) bool {
	if f, ok := d.(*ast.FuncDecl); ok {
		return f.Name.Name == matchInfer || f.Name.Name == matchContext || f.Name.Name == matchOperand || f.Name.Name == matchZero || f.Name.Name == matchReturn
	}
	if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE && len(g.Specs) == 1 {
		return g.Specs[0].(*ast.TypeSpec).Name.Name == matchPending
	}
	return false
}
