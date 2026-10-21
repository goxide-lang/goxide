package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (p *permissionChecker) values(specs []*ast.ValueSpec) {
	for _, spec := range specs {
		var want *permission
		if spec.Type != nil {
			want = p.astType(spec.Type, nil)
			if len(spec.Values) == 0 && want.ref != ValueRef {
				p.c.diag(spec.Type, ReferencePermission, "reference variable requires an initializer")
			}
		}
		var values []*permission
		for _, value := range spec.Values {
			got := p.expr(value, want)
			if got != nil {
				if _, tuple := got.typ.(*types.Tuple); tuple {
					values = append(values, got.results...)
					continue
				}
			}
			if want != nil {
				p.requireLocal(value, got, want)
			}
			values = append(values, p.adapt(got, want))
		}
		for i, name := range spec.Names {
			s := p.c.symbol(name)
			q := want
			if i < len(values) {
				q = values[i]
			}
			if q == nil {
				q = p.fromGo(p.c.out.Symbols[s])
			}
			p.setSymbol(s, q)
			if want != nil {
				p.declared[s] = want
			}
		}
	}
}

func (p *permissionChecker) requireLocal(node ast.Node, got, want *permission) {
	// Shared storage qualifiers travel with local copies even when their Go
	// spelling has no capability annotation. Explicit reference-to-pointer
	// conversion and interface erasure remain capability boundaries.
	if got == nil || want == nil {
		return
	}
	if want.typ != nil {
		if _, iface := want.typ.Underlying().(*types.Interface); iface && !p.c.out.isArray(want.typ) && p.c.out.EnumOf(want.typ) == nil {
			p.require(node, got, want)
			return
		}
	}
	p.require(node, p.withoutShared(got), want)
}

func (p *permissionChecker) withoutShared(q *permission) *permission {
	if q == nil {
		return nil
	}
	r := *q
	r.shared = false
	r.elem, r.key = p.withoutShared(q.elem), p.withoutShared(q.key)
	r.fields = map[string]*permission{}
	for name, field := range q.fields {
		r.fields[name] = p.withoutShared(field)
	}
	return &r
}

func (p *permissionChecker) simpleStmt(node ast.Stmt) bool {
	switch n := node.(type) {
	case *ast.DeclStmt:
		switch decl := n.Decl.(type) {
		case *ast.VarDecl:
			p.values(decl.Specs)
		case *ast.ConstDecl:
			p.values(decl.Specs)
		}
	case *ast.LetStmt:
		var want *permission
		if n.Type != nil {
			want = p.astType(n.Type, nil)
		}
		got := p.expr(n.Value, want)
		p.requireLocal(n.Value, got, want)
		p.bindPattern(n.Pat, p.adapt(got, want), ValueRef, n.Value, 0)
	case *ast.AssignStmt:
		var values []*permission
		for i, rhs := range n.Rhs {
			var want *permission
			if i < len(n.Lhs) {
				want = p.assignmentType(n.Lhs[i])
			}
			if n.Tok == token.DEFINE {
				want = nil
			}
			got := p.expr(rhs, want)
			if got != nil {
				if _, tuple := got.typ.(*types.Tuple); tuple {
					values = append(values, got.results...)
					continue
				}
			}
			values = append(values, got)
		}
		for i, lhs := range n.Lhs {
			var got *permission
			if i < len(values) {
				got = values[i]
			}
			if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
				continue
			}
			if !p.assignable(lhs) {
				p.c.diag(lhs, ReferenceAddress, "assignment requires an addressable storage location or map index")
			}
			_, writable := p.place(lhs)
			if !writable {
				p.reportWrite(lhs)
			}
			want := p.assignmentType(lhs)
			if n.Tok == token.DEFINE {
				want = nil
			}
			if len(n.Rhs) > 0 {
				rhs := n.Rhs[0]
				if i < len(n.Rhs) {
					rhs = n.Rhs[i]
				}
				if location := p.location(lhs); location != nil && p.initializers[location.root] == nil {
					p.requireLocal(rhs, got, want)
				} else {
					p.require(rhs, got, want)
				}
			}
			if n.Tok == token.ASSIGN || n.Tok == token.DEFINE {
				p.assign(lhs, p.adapt(got, want))
			}
		}
	case *ast.IncDecStmt:
		p.expr(n.X, nil)
		if !p.assignable(n.X) {
			p.c.diag(n.X, ReferenceAddress, "increment or decrement requires an addressable storage location or map index")
		}
		_, writable := p.place(n.X)
		if !writable {
			p.reportWrite(n.X)
		}
	case *ast.ExprStmt:
		p.expr(n.X, nil)
	case *ast.ReturnStmt:
		var values []*permission
		for i, value := range n.Results {
			var want *permission
			if i < len(p.results) {
				want = p.results[i]
			}
			got := p.expr(value, want)
			if got != nil {
				if _, tuple := got.typ.(*types.Tuple); tuple {
					values = append(values, got.results...)
					continue
				}
			}
			p.require(value, got, want)
			values = append(values, got)
		}
		if len(n.Results) == 1 && len(values) > 1 {
			for i, got := range values {
				if i < len(p.results) {
					p.require(n.Results[0], got, p.results[i])
				}
			}
		}
		return false
	case *ast.SendStmt:
		p.synchronize()
		channel := p.expr(n.Chan, nil)
		got := p.expr(n.Value, p.element(channel))
		p.require(n.Value, got, p.element(channel))
		if channel != nil && channel.shared {
			p.reportWrite(n.Chan)
		}
	case *ast.GoStmt:
		if call, ok := n.Call.(*ast.CallExpr); ok {
			p.call(call, nil, true)
		} else {
			p.expr(n.Call, nil)
		}
	case *ast.DeferStmt:
		p.expr(n.Call, nil)
	}
	return true
}

func (p *permissionChecker) assignmentType(node ast.Expr) *permission {
	if id, ok := node.(*ast.Ident); ok {
		s := p.c.symbol(id)
		if p.initializers[s] != nil {
			return p.globals[s]
		}
		return p.declared[s]
	}
	if paren, ok := node.(*ast.ParenExpr); ok {
		return p.assignmentType(paren.X)
	}
	if location := p.location(node); location != nil {
		q := p.declared[location.root]
		if p.initializers[location.root] != nil {
			q = p.globals[location.root]
		}
		if q == nil {
			q = p.fromGo(p.c.out.Symbols[location.root])
		}
		for _, field := range location.fields {
			if field == permissionArrayElement {
				q = p.element(q)
			} else {
				q = p.fields(q)[field]
			}
		}
		return q
	}
	return p.expr(node, nil)
}

func (p *permissionChecker) assign(node ast.Expr, value *permission) {
	if location := p.location(node); location != nil && location.root != nil {
		if len(location.fields) == 0 {
			p.setSymbol(location.root, value)
			return
		}
		if !p.directValuePath(node) {
			value = p.merge(p.expr(node, nil), value)
		}
		var update func(*permission, []string) *permission
		update = func(q *permission, path []string) *permission {
			if len(path) == 0 {
				return value
			}
			if q == nil {
				return q
			}
			r := *q
			if path[0] == permissionArrayElement {
				r.elem = update(p.element(q), path[1:])
			} else {
				r.fields = copyFields(q.fields)
				r.fields[path[0]] = update(p.fields(q)[path[0]], path[1:])
			}
			return &r
		}
		p.setSymbol(location.root, update(p.env[location.root], location.fields))
	}
}

func (p *permissionChecker) directValuePath(node ast.Expr) bool {
	switch node := node.(type) {
	case *ast.Ident, *ast.PathExpr:
		return true
	case *ast.ParenExpr:
		return p.directValuePath(node.X)
	case *ast.SelectorExpr:
		value := p.expr(node.X, nil)
		if value == nil || value.typ == nil || !p.directValuePath(node.X) {
			return false
		}
		_, indices, _ := types.LookupFieldOrMethod(value.typ, true, p.c.pkg, node.Sel.Name)
		for _, index := range indices {
			if pointerType(value) {
				return false
			}
			structure, ok := value.typ.Underlying().(*types.Struct)
			if !ok || index >= structure.NumFields() {
				return false
			}
			value = p.fields(value)[structure.Field(index).Name()]
		}
		return true
	}
	return false
}

func (p *permissionChecker) synchronize() {
	for symbol, value := range p.asyncEffects {
		p.setSymbol(symbol, p.merge(p.env[symbol], value))
	}
}

func (p *permissionChecker) assignable(node ast.Expr) bool {
	if addressable, _ := p.place(node); addressable {
		return true
	}
	if paren, ok := node.(*ast.ParenExpr); ok {
		return p.assignable(paren.X)
	}
	if index, ok := node.(*ast.IndexExpr); ok {
		if value := p.expr(index.X, nil); value != nil && value.typ != nil {
			_, ok := value.typ.Underlying().(*types.Map)
			return ok
		}
	}
	return false
}

func (p *permissionChecker) rangeBind(n *ast.RangeStmt, source *permission) {
	key := p.fromGo(p.c.out.Types[n.Key])
	value := p.element(source)
	if source != nil {
		if source.key != nil {
			key = source.key
		}
		if source.shared {
			key = p.readValue(key)
			value = p.readValue(value)
		}
	}
	for i, node := range []ast.Expr{n.Key, n.Value} {
		if node == nil {
			continue
		}
		q := key
		if i == 1 {
			q = value
		}
		if n.Tok != token.DEFINE {
			_, writable := p.place(node)
			if !writable {
				p.reportWrite(node)
			}
			if location := p.location(node); location != nil && p.initializers[location.root] == nil {
				p.requireLocal(node, q, p.assignmentType(node))
			} else {
				p.require(node, q, p.assignmentType(node))
			}
		}
		p.assign(node, q)
	}
}

func (p *permissionChecker) match(n *ast.MatchExpr, want *permission) *permission {
	info := p.c.out.Matches[n]
	if info == nil {
		return p.fromGo(p.c.out.Types[n])
	}
	owner := n.X
	mode := info.Reference
	if ref, ok := referenceScrutinee(n.X).(*ast.RefExpr); ok {
		owner = ref.X
		q := p.fromGo(p.c.out.Types[n.X])
		q.ref = mode
		p.expr(n.X, q)
	}
	value := p.expr(owner, nil)
	if mode == ReadRef {
		value = p.readValue(value)
	}
	if pointerType(value) && value.ref != ValueRef {
		p.c.diag(n.X, ReferencePattern, "matching a reference requires an explicit dereference of the owner")
	}
	entry := clonePermissionEnv(p.env)
	// Failed guards keep their effects for the next arm of the same variant.
	// Executed arm bodies only flow to the match's continuation.
	pending := map[string]permissionEnv{}
	var variants []string
	if info.Enum != nil {
		for _, variant := range info.Enum.Enum.Variants {
			variants = append(variants, variant.Name)
		}
	} else {
		variants = []string{""}
	}
	for _, variant := range variants {
		pending[variant] = entry
	}
	var result *permission
	var exits []permissionEnv
	for _, arm := range n.Arms {
		name := p.c.patternName(arm.Pat)
		if info.Enum == nil {
			name = ""
		}
		var names []string
		var inputs []permissionEnv
		if p.c.wildcard(arm.Pat) {
			for _, variant := range variants {
				names = append(names, variant)
				inputs = append(inputs, pending[variant])
			}
		} else {
			names = []string{name}
			inputs = []permissionEnv{pending[name]}
		}
		p.env = clonePermissionEnv(p.joinEnvs(inputs...))
		if p.env == nil {
			continue
		}
		p.bindPattern(arm.Pat, value, mode, owner, 0)
		p.expr(arm.Guard, nil)
		for _, variant := range names {
			if arm.Guard != nil {
				if p.c.irrefutable(arm.Pat, p.c.out.Types[owner]) {
					pending[variant] = clonePermissionEnv(p.env)
				} else {
					pending[variant] = p.joinEnvs(pending[variant], p.env)
				}
			} else if p.c.irrefutable(arm.Pat, p.c.out.Types[owner]) {
				pending[variant] = nil
			}
		}
		continues := true
		if arm.Body != nil {
			got := p.expr(arm.Body, want)
			p.require(arm.Body, got, want)
			result = p.merge(result, got)
		} else {
			continues = p.block(arm.Block)
		}
		if continues {
			exits = append(exits, p.env)
		}
	}
	p.env = p.joinEnvs(exits...)
	if result == nil {
		result = p.fromGo(p.c.out.Types[n])
	}
	return result
}

func (p *permissionChecker) bindPattern(pattern ast.Pat, value *permission, mode RefMode, owner ast.Expr, depth int) {
	if pattern == nil {
		return
	}
	switch n := pattern.(type) {
	case *ast.BindPat:
		s := p.c.symbol(n.Name)
		if s == nil {
			return
		}
		if s.Kind == resolve.Variant || s.Kind == resolve.Constant || s.Kind == resolve.ConstParam {
			return
		}
		binding := mode
		if n.Ref {
			binding = ReadRef
			if n.Mut {
				binding = WriteRef
			}
		}
		if binding != ValueRef {
			if depth > 1 {
				p.c.diag(n, ReferencePattern, "nested payload reference patterns are not supported")
				return
			}
			addressable, writable := p.place(owner)
			if ownerValue := p.expr(owner, nil); ownerValue != nil && ownerValue.shared {
				if enum := p.c.out.EnumOf(ownerValue.typ); enum != nil && !enum.Enum.Builtin {
					writable = false
				}
			}
			if !addressable {
				p.c.diag(n, ReferenceAddress, "payload reference requires an addressable enum owner")
			}
			if binding == WriteRef && (!writable || mode == ReadRef) {
				p.c.diag(n, ReferencePermission, "cannot take a writable payload reference through a read-only owner")
			}
			q := &permission{typ: p.c.out.Symbols[s], ref: binding, elem: value}
			p.setSymbol(s, q)
		} else {
			p.setSymbol(s, value)
		}
		if n.Sub != nil {
			p.bindPattern(n.Sub, value, mode, owner, depth)
		}
	case *ast.TuplePat:
		if depth > 0 && patternReferences(n) {
			p.c.diag(n, ReferencePattern, "nested payload reference patterns are not supported")
			return
		}
		if value == nil {
			return
		}
		instance := p.c.out.EnumOf(value.typ)
		if instance == nil {
			return
		}
		variant := instance.Variant(p.c.patternName(n))
		if variant == nil {
			return
		}
		for i, child := range n.Elts {
			p.bindPattern(child, p.enumField(value, variant, i), mode, owner, depth+1)
		}
	case *ast.StructPat:
		if depth > 0 && patternReferences(n) {
			p.c.diag(n, ReferencePattern, "nested payload reference patterns are not supported")
			return
		}
		if value == nil {
			return
		}
		instance := p.c.out.EnumOf(value.typ)
		if instance == nil {
			return
		}
		variant := instance.Variant(p.c.patternName(n))
		if variant == nil {
			return
		}
		for _, child := range n.Fields {
			for i, field := range variant.Fields {
				if field.Name == child.Name.Name {
					p.bindPattern(child.Pat, p.enumField(value, variant, i), mode, owner, depth+1)
				}
			}
		}
	case *ast.OrPat:
		for _, alt := range n.Alts {
			p.bindPattern(alt, value, mode, owner, depth)
		}
	}
}

func patternReferences(pattern ast.Pat) bool {
	switch n := pattern.(type) {
	case *ast.BindPat:
		return n.Ref || patternReferences(n.Sub)
	case *ast.TuplePat:
		for _, child := range n.Elts {
			if patternReferences(child) {
				return true
			}
		}
	case *ast.StructPat:
		for _, child := range n.Fields {
			if patternReferences(child.Pat) {
				return true
			}
		}
	case *ast.OrPat:
		for _, child := range n.Alts {
			if patternReferences(child) {
				return true
			}
		}
	}
	return false
}
