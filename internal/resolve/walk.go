package resolve

import (
	"fmt"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (r *resolver) walkFile() {
	for _, item := range r.file.AST.Items {
		r.walkItem(r.file.Scope, item)
	}
}

func (r *resolver) walkItem(scope *Scope, item ast.Item) {
	r.enterExpansion(scope, item)
	switch n := item.(type) {
	case *ast.FuncDecl:
		if n.Path != nil {
			r.path(scope, n.Path)
		}
		fn := r.lexicalScope(FunctionScope, scope, n)
		r.file.Scopes[n.ID()] = fn
		r.receiverParams(fn, n.Recv)
		r.generics(fn, n.Generics, n.Where)
		r.paramTypes(fn, n.Recv)
		r.paramTypes(fn, n.Params)
		r.paramTypes(fn, n.Results)
		r.bindParams(fn, n.Recv)
		r.bindParams(fn, n.Params)
		r.bindParams(fn, n.Results)
		if self := r.lookup(scope, Key{Name: "Self"}, true).Symbol; self != nil && len(n.Recv) == 0 && len(n.Params) > 0 {
			names := n.Params[0].Names
			if len(names) == 1 && names[0].Name == "self" {
				r.contextAlias(fn, n, "self", r.file.Definitions[names[0].ID()])
			}
		}
		r.functionBody(fn, n.Body)
	case *ast.TypeDecl:
		if n.Grouped {
			for _, spec := range n.Specs {
				r.typeBody(scope, spec, spec.Generics, spec.Where, spec.Type)
			}
		} else {
			r.typeBody(scope, n, n.Generics, n.Where, n.Type)
		}
	case *ast.EnumDecl:
		inner := r.lexicalScope(BlockScope, scope, n)
		r.generics(inner, n.Generics, n.Where)
		for _, variant := range n.Variants {
			for _, field := range variant.Fields {
				r.typ(inner, field.Type)
			}
			r.expr(inner, variant.Disc)
		}
	case *ast.TraitDecl:
		inner := r.lexicalScope(BlockScope, scope, n)
		r.selfType(inner, n, r.file.Scopes[n.ID()])
		r.generics(inner, n.Generics, n.Where)
		for _, member := range n.Items {
			r.walkItem(inner, member)
		}
	case *ast.ImplDecl:
		inner := r.lexicalScope(BlockScope, scope, n)
		r.selfType(inner, n, r.file.Scopes[n.ID()])
		r.generics(inner, n.Generics, n.Where)
		r.typ(inner, n.Trait)
		r.typ(inner, n.Type)
		for _, member := range n.Items {
			r.walkItem(inner, member)
		}
	case *ast.DeclareDecl:
		r.walkItem(scope, n.Decl)
	case *ast.ModDecl:
		inner := scope
		if symbol := r.file.Definitions[n.ID()]; symbol != nil && symbol.Members != nil {
			inner = symbol.Members
		}
		for _, item := range n.Items {
			r.walkItem(inner, item)
		}
	case *ast.ConstDecl:
		for _, spec := range n.Specs {
			r.valueSpec(scope, spec)
		}
	case *ast.VarDecl:
		for _, spec := range n.Specs {
			r.valueSpec(scope, spec)
		}
	case *ast.MacroItem:
		r.macroPath(scope, n.Path)
	case *ast.UseDecl, *ast.MacroRulesDecl, *ast.BadItem:
	}
}

func (r *resolver) selfType(scope *Scope, owner ast.Node, members *Scope) {
	symbol := &Symbol{Name: Key{Name: "Self"}, Kind: Type, Decl: r.ref(owner), Members: members}
	scope.Bindings[symbol.Name] = symbol
	r.contextAlias(scope, owner, "Self", symbol)
}

func (r *resolver) typeBody(scope *Scope, node ast.Node, params *ast.GenericParams, where *ast.WhereClause, typ ast.Type) {
	inner := r.lexicalScope(BlockScope, scope, node)
	if params != nil {
		r.file.Scopes[params.ID()] = inner
	}
	r.generics(inner, params, where)
	r.typ(inner, typ)
}

func (r *resolver) generics(scope *Scope, params *ast.GenericParams, where *ast.WhereClause) {
	if params != nil {
		r.file.Scopes[params.ID()] = scope
		for _, param := range params.Params {
			r.file.Visibility[param.ID()] = Private
			for _, name := range param.Names {
				sym := r.define(scope, param, name, TypeParam)
				if len(param.Names) == 1 {
					r.recordDeclaration(param, sym)
				}
			}
		}
		for _, param := range params.Params {
			r.typ(scope, param.Constraint)
		}
	}
	if where != nil {
		for _, predicate := range where.Predicates {
			r.typ(scope, predicate.Type)
			for _, bound := range predicate.Bounds {
				r.typ(scope, bound)
			}
		}
	}
}

func (r *resolver) receiverParams(scope *Scope, params []*ast.Param) {
	for _, param := range params {
		typ := param.Type
		if ptr, ok := typ.(*ast.PointerType); ok {
			typ = ptr.Elem
		}
		path, ok := typ.(*ast.PathType)
		if !ok || path.Path == nil || len(path.Path.Segments) != 1 {
			continue
		}
		args := path.Path.Segments[0].Args
		if args == nil {
			continue
		}
		for _, arg := range args.Args {
			if name := singleTypeName(arg.Type); name != nil {
				r.define(scope, arg, name, TypeParam)
			}
		}
	}
}

func singleTypeName(typ ast.Type) *ast.Ident {
	if path, ok := typ.(*ast.PathType); ok && path.Path != nil && len(path.Path.Segments) == 1 && path.Path.Segments[0].Args == nil {
		return path.Path.Segments[0].Name
	}
	return nil
}

func (r *resolver) paramTypes(scope *Scope, params []*ast.Param) {
	for _, param := range params {
		r.typ(scope, param.Type)
	}
}

func (r *resolver) bindParams(scope *Scope, params []*ast.Param) {
	for _, param := range params {
		for _, name := range param.Names {
			sym := r.define(scope, param, name, Variable)
			if len(param.Names) == 1 {
				r.recordDeclaration(param, sym)
			}
		}
		r.file.Visibility[param.ID()] = Private
	}
}

func (r *resolver) valueSpec(scope *Scope, spec *ast.ValueSpec) {
	r.typ(scope, spec.Type)
	for _, expr := range spec.Values {
		r.expr(scope, expr)
	}
}

func (r *resolver) typ(scope *Scope, typ ast.Type) {
	r.enterExpansion(scope, typ)
	switch n := typ.(type) {
	case *ast.PathType:
		r.file.References[n.ID()] = r.path(scope, n.Path)
	case *ast.RefType:
		r.typ(scope, n.Elem)
	case *ast.PointerType:
		r.typ(scope, n.Elem)
	case *ast.SliceType:
		r.typ(scope, n.Elem)
	case *ast.ArrayType:
		r.expr(scope, n.Len)
		r.typ(scope, n.Elem)
	case *ast.MapType:
		r.typ(scope, n.Key)
		r.typ(scope, n.Value)
	case *ast.ChanType:
		r.typ(scope, n.Elem)
	case *ast.FuncType:
		fn := r.lexicalScope(FunctionScope, scope, n)
		r.file.Scopes[n.ID()] = fn
		r.paramTypes(scope, n.Params)
		r.paramTypes(scope, n.Results)
		r.bindParams(fn, n.Params)
		r.bindParams(fn, n.Results)
	case *ast.StructType:
		if r.file.Scopes[n.ID()] == nil {
			r.declareTypeMembers(n, n, nil, true)
		}
		for _, field := range n.Fields {
			r.typ(scope, field.Type)
		}
	case *ast.InterfaceType:
		if r.file.Scopes[n.ID()] == nil {
			r.declareTypeMembers(n, n, nil, true)
		}
		for _, method := range n.Methods {
			r.typ(scope, method.Type)
			fn := r.lexicalScope(FunctionScope, scope, method)
			r.file.Scopes[method.ID()] = fn
			r.paramTypes(scope, method.Params)
			r.paramTypes(scope, method.Results)
			r.bindParams(fn, method.Params)
			r.bindParams(fn, method.Results)
		}
	case *ast.UnionType:
		for _, term := range n.Terms {
			r.typ(scope, term)
		}
	case *ast.ApproxType:
		r.typ(scope, n.Type)
	case *ast.SelfType:
		res := r.lookup(scope, Key{Name: "Self"}, true)
		r.file.References[n.ID()] = res
		if res.Symbol == nil {
			r.diag(n, "unresolved", "Self is only available in trait and impl declarations")
		}
	case *ast.ProjectionType:
		r.typ(scope, n.X)
		res := r.file.References[n.X.ID()]
		if n.Dot && isModule(res.Symbol) {
			r.diag(n.X, "namespace-as-value", "module members require a static :: path")
		}
		if res.Symbol != nil || res.Deferred {
			res = r.member(res, n.Item)
			r.record(n.Item, res)
		}
		r.file.References[n.ID()] = res
		if n.Item != nil {
			r.file.References[n.Item.ID()] = res
		}
		r.genericArgs(scope, n.Args)
	case *ast.ParenType:
		r.typ(scope, n.Type)
		r.file.References[n.ID()] = r.file.References[n.Type.ID()]
	case *ast.MacroType:
		r.file.References[n.ID()] = r.macroPath(scope, n.Path)
	case *ast.BadType:
	}
}

func (r *resolver) genericArgs(scope *Scope, args *ast.GenericArgs) {
	if args != nil {
		for _, arg := range args.Args {
			r.typ(scope, arg.Type)
			if arg.Name != nil {
				r.file.References[arg.Name.ID()] = Resolution{Deferred: true}
			}
		}
	}
}

func (r *resolver) expr(scope *Scope, expr ast.Expr) {
	r.enterExpansion(scope, expr)
	switch n := expr.(type) {
	case *ast.Ident:
		r.ident(scope, n, false)
	case *ast.CompositeLit:
		r.typ(scope, n.Type)
		for _, elem := range n.Elts {
			if kv, ok := elem.(*ast.KeyValue); ok && !r.indexKeys(r.file, n.Type, make(map[*Symbol]bool)) {
				if name, ok := kv.Key.(*ast.Ident); ok {
					candidate := r.lookup(r.identScope(scope, name), key(name), false)
					candidate.Deferred = true
					r.file.References[name.ID()] = candidate
				} else {
					r.expr(scope, kv.Key)
				}
				r.expr(scope, kv.Value)
			} else {
				r.expr(scope, elem)
			}
		}
	case *ast.KeyValue:
		r.expr(scope, n.Key)
		r.expr(scope, n.Value)
	case *ast.ParenExpr:
		r.expr(scope, n.X)
		r.file.References[n.ID()] = r.file.References[n.X.ID()]
	case *ast.SelectorExpr:
		r.expr(scope, n.X)
		if isModule(r.file.References[n.X.ID()].Symbol) {
			r.diag(n.X, "namespace-as-value", "module members require a static :: path")
		}
		r.file.References[n.Sel.ID()] = Resolution{Deferred: true}
		r.file.References[n.ID()] = Resolution{Deferred: true}
	case *ast.IndexExpr:
		r.expr(scope, n.X)
		for _, index := range n.Indices {
			r.expr(scope, index)
		}
	case *ast.SliceExpr:
		r.expr(scope, n.X)
		r.expr(scope, n.Low)
		r.expr(scope, n.High)
		r.expr(scope, n.Max)
	case *ast.TypeAssertExpr:
		r.expr(scope, n.X)
		r.typ(scope, n.Type)
	case *ast.CallExpr:
		_, print := r.result.Prints[n]
		_, adapter := r.result.GoResults[n]
		if !print && !adapter {
			r.expr(scope, n.Fun)
		}
		for _, arg := range n.Args {
			r.expr(scope, arg)
		}
	case *ast.StarExpr:
		r.expr(scope, n.X)
	case *ast.UnaryExpr:
		r.expr(scope, n.X)
	case *ast.BinaryExpr:
		r.expr(scope, n.X)
		r.expr(scope, n.Y)
	case *ast.FuncLit:
		fn := r.lexicalScope(FunctionScope, scope, n)
		r.file.Scopes[n.ID()] = fn
		r.paramTypes(scope, n.Params)
		r.paramTypes(scope, n.Results)
		r.typ(scope, n.Result)
		r.bindParams(fn, n.Params)
		r.bindParams(fn, n.Results)
		r.functionBody(fn, n.Body)
		r.expr(fn, n.Expr)
	case *ast.PathExpr:
		r.file.References[n.ID()] = r.path(scope, n.Path)
	case *ast.MatchExpr:
		r.expr(scope, n.X)
		for _, arm := range n.Arms {
			inner := r.lexicalScope(BlockScope, scope, arm)
			r.file.Scopes[arm.ID()] = inner
			r.pattern(inner, arm.Pat)
			r.expr(inner, arm.Guard)
			r.expr(inner, arm.Body)
			r.block(inner, arm.Block, false)
		}
	case *ast.TryExpr:
		r.expr(scope, n.X)
	case *ast.RefExpr:
		r.expr(scope, n.X)
	case *ast.UnsafeExpr:
		r.expr(scope, n.X)
	case *ast.MacroExpr:
		r.file.References[n.ID()] = r.macroPath(scope, n.Path)
	case *ast.QualifiedSelectorExpr:
		r.expr(scope, n.X)
		r.typ(scope, n.Trait)
		r.file.References[n.Sel.ID()] = Resolution{Deferred: true}
		r.file.References[n.ID()] = Resolution{Deferred: true}
	case *ast.AwaitExpr:
		r.expr(scope, n.X)
	case *ast.TypeExpr:
		r.typ(scope, n.Type)
	case *ast.BasicLit, *ast.BoolLit, *ast.BadExpr:
	}
}

func isModule(symbol *Symbol) bool {
	if symbol != nil && symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	return symbol != nil && symbol.Kind == Module
}

func (r *resolver) indexKeys(file *File, typ ast.Type, seen map[*Symbol]bool) bool {
	switch n := typ.(type) {
	case *ast.MapType, *ast.ArrayType, *ast.SliceType:
		return true
	case *ast.ParenType:
		return r.indexKeys(file, n.Type, seen)
	case *ast.PathType:
		if n.Path != nil && len(n.Path.Segments) == 1 {
			// Reuse declaration-time bindings; aliases can refer to shadowed locals.
			symbol := file.References[n.ID()].Symbol
			if symbol == nil || seen[symbol] {
				return false
			}
			seen[symbol] = true
			file := r.result.Files[symbol.Decl.File]
			if file == nil {
				return false
			}
			var target ast.Type
			ast.Walk(file.AST, func(node ast.Node) bool {
				switch decl := node.(type) {
				case *ast.TypeDecl:
					if decl.Name != nil && decl.Name.ID() == symbol.Decl.ID {
						target = decl.Type
					}
				case *ast.TypeSpec:
					if decl.Name != nil && decl.Name.ID() == symbol.Decl.ID {
						target = decl.Type
					}
				}
				return target == nil
			})
			return r.indexKeys(file, target, seen)
		}
	}
	return false
}

func (r *resolver) functionBody(scope *Scope, body *ast.Block) {
	if body == nil {
		return
	}
	// Labels belong to a whole function, including forward references.
	ast.Walk(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return false
		case *ast.LabeledStmt:
			if n.Label == nil || n.Label.Name == "_" {
				return true
			}
			k := key(n.Label)
			if scope.Labels[k] != nil {
				r.diag(n.Label, "duplicate", fmt.Sprintf("duplicate label %q", k.Name))
			} else {
				symbol := &Symbol{Name: k, Kind: Label, Decl: r.ref(n.Label), GoName: k.Name}
				scope.Labels[k] = symbol
				r.recordDeclaration(n.Label, symbol)
				r.recordDeclaration(n, symbol)
			}
		}
		return true
	})
	r.block(scope, body, false)
}

func (r *resolver) block(scope *Scope, block *ast.Block, child bool) {
	if block == nil {
		return
	}
	if child {
		scope = r.lexicalScope(BlockScope, scope, block)
	}
	r.file.Scopes[block.ID()] = scope
	for _, stmt := range block.List {
		r.stmt(scope, stmt)
	}
}

func (r *resolver) stmt(scope *Scope, stmt ast.Stmt) {
	r.enterExpansion(scope, stmt)
	switch n := stmt.(type) {
	case *ast.AssignStmt:
		for _, expr := range n.Rhs {
			r.expr(scope, expr)
		}
		if n.Tok == token.DEFINE {
			r.shortDecl(scope, n, n.Lhs)
		} else {
			for _, expr := range n.Lhs {
				r.assignTarget(scope, expr)
			}
		}
	case *ast.IncDecStmt:
		r.expr(scope, n.X)
	case *ast.ExprStmt:
		r.expr(scope, n.X)
	case *ast.SendStmt:
		r.expr(scope, n.Chan)
		r.expr(scope, n.Value)
	case *ast.GoStmt:
		r.expr(scope, n.Call)
	case *ast.DeferStmt:
		r.expr(scope, n.Call)
	case *ast.ReturnStmt:
		for _, expr := range n.Results {
			r.expr(scope, expr)
		}
	case *ast.BranchStmt:
		if n.Label != nil {
			for fn := scope; fn != nil; fn = fn.Parent {
				if fn.Kind == FunctionScope {
					symbol := fn.Labels[key(n.Label)]
					r.file.References[n.Label.ID()] = Resolution{Symbol: symbol}
					if symbol == nil {
						r.diag(n.Label, "unresolved", fmt.Sprintf("unresolved label %q", n.Label.Name))
					}
					break
				}
			}
		}
	case *ast.Block:
		r.block(scope, n, true)
	case *ast.IfStmt:
		inner := r.statementScope(scope, n)
		r.stmt(inner, n.Init)
		r.expr(inner, n.Cond)
		r.block(inner, n.Body, true)
		r.stmt(inner, n.Else)
	case *ast.SwitchStmt:
		inner := r.statementScope(scope, n)
		r.stmt(inner, n.Init)
		r.expr(inner, n.Tag)
		for _, clause := range n.Cases {
			for _, expr := range clause.List {
				r.expr(inner, expr)
			}
			r.caseBody(inner, clause, nil)
		}
	case *ast.TypeSwitchStmt:
		inner := r.statementScope(scope, n)
		r.stmt(inner, n.Init)
		var name *ast.Ident
		if assign, ok := n.Assign.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
			for _, rhs := range assign.Rhs {
				r.expr(inner, rhs)
			}
			if len(assign.Lhs) == 1 {
				name, _ = assign.Lhs[0].(*ast.Ident)
			}
		} else {
			r.stmt(inner, n.Assign)
		}
		for _, clause := range n.Cases {
			for _, typ := range clause.Types {
				r.typ(inner, typ)
			}
			r.caseBody(inner, clause, name)
		}
	case *ast.SelectStmt:
		for _, clause := range n.Cases {
			inner := r.statementScope(scope, clause)
			r.stmt(inner, clause.Comm)
			for _, stmt := range clause.Body {
				r.stmt(inner, stmt)
			}
		}
	case *ast.ForStmt:
		inner := r.statementScope(scope, n)
		r.stmt(inner, n.Init)
		r.expr(inner, n.Cond)
		r.stmt(inner, n.Post)
		r.block(inner, n.Body, true)
	case *ast.RangeStmt:
		r.expr(scope, n.X)
		inner := r.statementScope(scope, n)
		var lhs []ast.Expr
		if n.Key != nil {
			lhs = append(lhs, n.Key)
		}
		if n.Value != nil {
			lhs = append(lhs, n.Value)
		}
		if n.Tok == token.DEFINE {
			r.shortDecl(inner, n, lhs)
		} else {
			for _, expr := range lhs {
				r.assignTarget(inner, expr)
			}
		}
		r.block(inner, n.Body, true)
	case *ast.LabeledStmt:
		r.stmt(scope, n.Stmt)
	case *ast.DeclStmt:
		r.localItem(scope, n.Decl)
	case *ast.LetStmt:
		r.typ(scope, n.Type)
		r.expr(scope, n.Value)
		r.pattern(scope, n.Pat)
	case *ast.LoopStmt:
		r.block(scope, n.Body, true)
	case *ast.WhileStmt:
		r.expr(scope, n.Cond)
		r.block(scope, n.Body, true)
	case *ast.EmptyStmt, *ast.BadStmt:
	}
}

func (r *resolver) statementScope(parent *Scope, node ast.Node) *Scope {
	scope := r.lexicalScope(BlockScope, parent, node)
	r.file.Scopes[node.ID()] = scope
	return scope
}

func (r *resolver) caseBody(scope *Scope, clause *ast.CaseClause, name *ast.Ident) {
	inner := r.statementScope(scope, clause)
	if name != nil {
		first := r.file.Definitions[name.ID()]
		symbol := r.define(inner, clause, name, Variable)
		r.recordDeclaration(clause, symbol)
		if first != nil {
			r.file.Definitions[name.ID()] = first
		}
	}
	for _, stmt := range clause.Body {
		r.stmt(inner, stmt)
	}
}

func (r *resolver) assignTarget(scope *Scope, expr ast.Expr) {
	if name, ok := expr.(*ast.Ident); ok && name.Name == "_" {
		return
	}
	r.expr(scope, expr)
}

func (r *resolver) shortDecl(scope *Scope, node ast.Node, lhs []ast.Expr) {
	seen := make(map[Key]bool)
	newNames := 0
	for _, expr := range lhs {
		name, ok := expr.(*ast.Ident)
		if !ok {
			r.diag(expr, "invalid-short-declaration", "short declaration requires identifier names")
			r.expr(scope, expr)
			continue
		}
		if name.Name == "_" {
			continue
		}
		k := key(name)
		if seen[k] {
			r.diag(name, "duplicate", fmt.Sprintf("duplicate name %q in short declaration", name.Name))
			continue
		}
		seen[k] = true
		if symbol := scope.Bindings[k]; symbol != nil {
			r.file.References[name.ID()] = Resolution{Symbol: symbol}
			if symbol.Kind != Variable {
				r.diag(name, "duplicate", fmt.Sprintf("%q is already declared in this scope", name.Name))
			}
		} else {
			r.define(scope, name, name, Variable)
			newNames++
		}
	}
	if newNames == 0 {
		r.diag(node, "no-new-variables", "short declaration has no new variables")
	}
}

func (r *resolver) localItem(scope *Scope, item ast.Item) {
	var specs []*ast.ValueSpec
	kind := Variable
	switch n := item.(type) {
	case *ast.VarDecl:
		specs = n.Specs
	case *ast.ConstDecl:
		specs = n.Specs
		kind = Constant
	case *ast.TypeDecl:
		if n.Grouped {
			r.file.Visibility[n.ID()] = Private
			for _, spec := range n.Specs {
				r.declareTypeSpec(scope, spec, n.Vis)
				r.typeBody(scope, spec, spec.Generics, spec.Where, spec.Type)
			}
			return
		}
		r.declareItem(scope, item)
		r.walkItem(scope, item)
		return
	default:
		r.declareItem(scope, item)
		r.walkItem(scope, item)
		return
	}
	for _, spec := range specs {
		r.valueSpec(scope, spec)
		r.declareValues(scope, item, []*ast.ValueSpec{spec}, kind, ast.Visibility{}, nil)
	}
}

func (r *resolver) pattern(scope *Scope, pattern ast.Pat) {
	switch n := pattern.(type) {
	case *ast.BindPat:
		res := r.lookup(r.identScope(scope.Parent, n.Name), key(n.Name), false)
		if !n.Ref && !n.Mut && n.Sub == nil && len(res.Candidates) > 1 {
			r.record(n.Name, res)
			r.file.References[n.ID()] = res
		} else if !n.Ref && !n.Mut && n.Sub == nil && patternConstant(res.Symbol) {
			r.file.References[n.ID()] = res
			r.file.References[n.Name.ID()] = res
		} else {
			symbol := r.define(scope, n, n.Name, Variable)
			r.recordDeclaration(n, symbol)
		}
		r.pattern(scope, n.Sub)
	case *ast.LitPat:
		r.expr(scope, n.Value)
	case *ast.PathPat:
		r.file.References[n.ID()] = r.path(scope, n.Path)
	case *ast.TuplePat:
		r.file.References[n.ID()] = r.path(scope, n.Path)
		for _, elem := range n.Elts {
			r.pattern(scope, elem)
		}
	case *ast.StructPat:
		r.file.References[n.ID()] = r.path(scope, n.Path)
		for _, field := range n.Fields {
			if field.Name != nil {
				r.file.References[field.Name.ID()] = Resolution{Deferred: true}
			}
			r.pattern(scope, field.Pat)
		}
	case *ast.OrPat:
		r.orPattern(scope, n)
	case *ast.RangePat:
		r.expr(scope, n.Low)
		r.expr(scope, n.High)
	case *ast.WildPat, *ast.BadPat:
	}
}

func patternConstant(symbol *Symbol) bool {
	if symbol != nil && symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	return symbol != nil && (symbol.Kind == Constant || symbol.Kind == Variant || symbol.Kind == Builtin && symbol.Name.Name == "None")
}

func (r *resolver) orPattern(scope *Scope, pattern *ast.OrPat) {
	var first map[Key]*Symbol
	for _, alternative := range pattern.Alts {
		inner := r.lexicalScope(BlockScope, scope.Parent, alternative)
		r.pattern(inner, alternative)
		if first == nil {
			first = inner.Bindings
			for k, symbol := range first {
				if scope.Bindings[k] != nil {
					r.diag(alternative, "duplicate", fmt.Sprintf("duplicate pattern binding %q", k.Name))
				} else {
					scope.Bindings[k] = symbol
				}
			}
			continue
		}
		matched := len(first) == len(inner.Bindings)
		for k := range first {
			if inner.Bindings[k] == nil {
				matched = false
			}
		}
		if !matched {
			r.diag(alternative, "pattern-bindings", "all alternatives must bind the same names")
		} else {
			ast.Walk(alternative, func(node ast.Node) bool {
				if symbol := r.file.Definitions[node.ID()]; symbol != nil && inner.Bindings[symbol.Name] == symbol {
					r.file.Definitions[node.ID()] = first[symbol.Name]
				}
				return true
			})
		}
	}
}
