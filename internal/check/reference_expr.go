package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type permissionLocation struct {
	root   *resolve.Symbol
	fields []string
}

const permissionArrayElement = "[]"

func samePermissionLocation(a, b *permissionLocation) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || a.root != b.root || len(a.fields) != len(b.fields) {
		return false
	}
	for i, field := range a.fields {
		if field != b.fields[i] {
			return false
		}
	}
	return true
}

// A nil alternative denotes an ordinary pointer whose target is not a local
// slot. Known targets stay live across joins, so a later slot assignment is
// visible through every pointer which may refer to it.
func joinPermissionLocations(a, b *permission) []*permissionLocation {
	var locations []*permissionLocation
	for _, q := range []*permission{a, b} {
		incoming := q.locations
		if len(incoming) == 0 {
			incoming = []*permissionLocation{q.location}
		}
		for _, candidate := range incoming {
			found := false
			for _, existing := range locations {
				found = found || samePermissionLocation(existing, candidate)
			}
			if !found {
				locations = append(locations, candidate)
			}
		}
	}
	return locations
}

func (p *permissionChecker) expr(node ast.Expr, want *permission) *permission {
	if view := p.c.out.TraitViews[node]; view != nil {
		return p.traitView(node, view)
	}
	if selected := p.c.out.Members[node]; selected != nil {
		return p.traitMember(node, selected)
	}
	return p.rawExpr(node, want)
}

func (p *permissionChecker) rawExpr(node ast.Expr, want *permission) *permission {
	if node == nil {
		return nil
	}
	q := p.fromGo(p.c.out.Types[node])
	if view := p.c.out.TraitViews[node]; view != nil {
		q = p.fromGo(view.SourceType)
	}
	switch n := node.(type) {
	case *ast.Ident, *ast.PathExpr:
		s := p.c.symbol(node)
		p.initializeGlobal(p.initializers[s])
		if value := p.env[s]; value != nil {
			return value
		}
		if f := p.functions[s]; f != nil {
			return f.typ
		}
		if con := p.c.out.Constructors[node]; con != nil {
			return p.constructorType(node, con, want)
		}
	case *ast.ParenExpr:
		return p.expr(n.X, want)
	case *ast.RefExpr:
		mode := ValueRef
		if want != nil && want.ref != ValueRef {
			mode = ReadRef
		}
		if n.Mut {
			mode = WriteRef
		}
		value := p.expr(n.X, nil)
		addressable, writable := p.place(n.X)
		operand := n.X
		for {
			paren, ok := operand.(*ast.ParenExpr)
			if !ok {
				break
			}
			operand = paren.X
		}
		if !n.Mut {
			if want == nil && !writable {
				mode = ReadRef
			}
			if dereference, ok := operand.(*ast.StarExpr); ok {
				if pointer := p.expr(dereference.X, nil); pointer != nil && pointer.ref != ValueRef {
					mode = ReadRef
				}
			}
		}
		if _, literal := operand.(*ast.CompositeLit); literal && mode == ValueRef {
			addressable = true
		}
		if !addressable {
			p.c.diag(n, ReferenceAddress, "reference requires an addressable storage location")
		}
		if !writable && mode != ReadRef {
			p.c.diag(n, ReferencePermission, "cannot take a writable or native pointer through a read-only reference")
		}
		q.ref, q.elem = mode, value
		locations := p.locations(n.X)
		if len(locations) == 1 {
			q.location = locations[0]
		} else {
			q.locations = locations
		}
	case *ast.StarExpr:
		pointer := p.expr(n.X, nil)
		q = p.deref(pointer)
		if pointer != nil && (pointer.ref == ReadRef || pointer.shared) {
			q = p.readValue(q)
		}
	case *ast.SelectorExpr:
		if n.Static {
			if signature, ok := q.typ.(*types.Signature); ok && signature.Params().Len() > 0 {
				object, _, _ := types.LookupFieldOrMethod(signature.Params().At(0).Type(), false, p.c.pkg, n.Sel.Name)
				if method, ok := object.(*types.Func); ok {
					return p.nativeMethodExpression(n, method, signature)
				}
			}
			return q
		}
		x := p.expr(n.X, nil)
		if x == nil {
			break
		}
		var fieldPath []int
		if x.typ != nil {
			if obj, index, _ := types.LookupFieldOrMethod(x.typ, true, p.c.pkg, n.Sel.Name); obj != nil {
				if method, ok := obj.(*types.Func); ok {
					return p.method(n, x, method, index)
				}
				fieldPath = index
			}
		}
		if pointerType(x) {
			x = p.deref(x)
			if r := p.expr(n.X, nil); r != nil && (r.ref == ReadRef || r.shared) {
				x = p.readValue(x)
			}
		}
		for _, index := range fieldPath {
			if pointerType(x) {
				pointer := x
				x = p.deref(pointer)
				if pointer.ref == ReadRef || pointer.shared {
					x = p.readValue(x)
				}
			}
			structure, ok := x.typ.Underlying().(*types.Struct)
			if !ok || index >= structure.NumFields() {
				break
			}
			if field := p.fields(x)[structure.Field(index).Name()]; field != nil {
				x, q = field, field
			}
		}
	case *ast.IndexExpr:
		x := p.expr(n.X, nil)
		if x != nil && (len(x.params) != 0 || signatureType(x)) {
			return p.instantiateFunction(x, n.Indices, q.typ)
		}
		for _, index := range n.Indices {
			p.expr(index, nil)
		}
		if x != nil {
			if pointerType(x) {
				value := p.deref(x)
				if x.ref == ReadRef || x.shared {
					value = p.readValue(value)
				}
				x = value
			}
			q = p.element(x)
			if x.shared {
				q = p.readValue(q)
			}
		}
	case *ast.SliceExpr:
		x := p.expr(n.X, nil)
		p.expr(n.Low, nil)
		p.expr(n.High, nil)
		p.expr(n.Max, nil)
		q.elem = p.element(x)
		if x != nil {
			q.shared = x.shared || x.ref == ReadRef
		}
		if pointerType(x) {
			q.elem = p.element(p.deref(x))
		}
		addressable, writable := p.place(n.X)
		if x != nil && x.typ != nil {
			if _, array := x.typ.Underlying().(*types.Array); array && !addressable {
				p.c.diag(n.X, ReferenceAddress, "slicing an array requires an addressable storage location")
			}
		}
		if !writable {
			q.shared = true
		}
	case *ast.CompositeLit:
		if con := p.c.out.Constructors[n]; con != nil {
			return p.enumLiteral(n, con, want)
		}
		if n.Type != nil {
			q = p.astType(n.Type, nil)
		} else if want != nil {
			q = want
		}
		r := *q
		r.fields = copyFields(q.fields)
		q = &r
		for i, elt := range n.Elts {
			name := ""
			value := elt
			if kv, ok := elt.(*ast.KeyValue); ok {
				value = kv.Value
				if id, ok := kv.Key.(*ast.Ident); ok {
					name = id.Name
				}
				if _, ok := q.typ.Underlying().(*types.Map); ok {
					key := p.expr(kv.Key, q.key)
					p.require(kv.Key, key, q.key)
				}
			}
			field, fieldName := p.literalField(q, i, name)
			got := p.expr(value, field)
			p.require(value, got, field)
			if fieldName != "" {
				q.fields[fieldName] = p.adapt(got, field)
			} else {
				q.elem = p.merge(q.elem, got)
			}
		}
	case *ast.KeyValue:
		return p.expr(n.Value, want)
	case *ast.CallExpr:
		return p.call(n, want, false)
	case *ast.TypeAssertExpr:
		x := p.expr(n.X, nil)
		if n.Type != nil {
			q = p.astType(n.Type, nil)
			p.require(n, x, q)
		}
	case *ast.UnaryExpr:
		if n.Op == token.ARROW {
			p.synchronize()
		}
		x := p.expr(n.X, nil)
		if n.Op == token.ARROW {
			q = p.element(x)
			if x != nil && x.shared {
				q = p.readValue(q)
			}
		}
	case *ast.BinaryExpr:
		p.expr(n.X, nil)
		p.expr(n.Y, nil)
	case *ast.FuncLit:
		q = p.functionType(n.Params, n.Results)
		q.typ = p.c.out.Types[n]
		q.closure = n
		if n.Result != nil {
			q.results = []*permission{p.astType(n.Result, nil)}
		}
		p.checkClosure(n, q, true)
	case *ast.MatchExpr:
		return p.match(n, want)
	case *ast.TryExpr:
		x := p.expr(n.X, nil)
		if trial := p.c.out.Tries[n]; trial != nil && trial.Input != nil {
			if v := trial.Input.Variant("Some"); v != nil {
				q = p.enumField(x, v, 0)
			} else if v := trial.Input.Variant("Ok"); v != nil {
				q = p.enumField(x, v, 0)
			}
		}
	case *ast.UnsafeExpr:
		return p.expr(n.X, want)
	case *ast.AwaitExpr:
		return p.expr(n.X, want)
	case *ast.TypeExpr:
		return p.astType(n.Type, nil)
	}
	return q
}

func pointerType(q *permission) bool {
	if q == nil || q.typ == nil {
		return false
	}
	_, ok := q.typ.Underlying().(*types.Pointer)
	return ok
}
func signatureType(q *permission) bool {
	if q == nil || q.typ == nil {
		return false
	}
	_, ok := q.typ.Underlying().(*types.Signature)
	return ok
}

func (p *permissionChecker) deref(q *permission) *permission {
	if q == nil {
		return p.fromGo(nil)
	}
	locations := q.locations
	if len(locations) == 0 && q.location != nil {
		locations = []*permissionLocation{q.location}
	}
	var result *permission
	for _, location := range locations {
		if location == nil {
			result = p.merge(result, p.element(q))
			continue
		}
		if current := p.env[location.root]; current != nil {
			for _, field := range location.fields {
				if field == permissionArrayElement {
					current = p.element(current)
				} else {
					current = p.fields(current)[field]
				}
				if current == nil {
					break
				}
			}
			if current != nil {
				result = p.merge(result, current)
			}
		}
	}
	if result != nil {
		return result
	}
	return p.element(q)
}

func (p *permissionChecker) location(node ast.Expr) *permissionLocation {
	locations := p.locations(node)
	if len(locations) == 1 {
		return locations[0]
	}
	return nil
}

func (p *permissionChecker) locations(node ast.Expr) []*permissionLocation {
	switch n := node.(type) {
	case *ast.Ident, *ast.PathExpr:
		return []*permissionLocation{{root: p.c.symbol(n)}}
	case *ast.ParenExpr:
		return p.locations(n.X)
	case *ast.StarExpr:
		if q := p.expr(n.X, nil); q != nil {
			if len(q.locations) != 0 {
				return q.locations
			}
			return []*permissionLocation{q.location}
		}
	case *ast.IndexExpr:
		value := p.expr(n.X, nil)
		if value == nil || value.typ == nil {
			return nil
		}
		locations := p.locations(n.X)
		if pointerType(value) {
			locations = value.locations
			if len(locations) == 0 {
				locations = []*permissionLocation{value.location}
			}
			value = p.deref(value)
		}
		if _, array := value.typ.Underlying().(*types.Array); !array {
			return nil
		}
		projected := make([]*permissionLocation, len(locations))
		for i, location := range locations {
			if location != nil {
				projected[i] = &permissionLocation{root: location.root, fields: append(append([]string(nil), location.fields...), permissionArrayElement)}
			}
		}
		return projected
	case *ast.SelectorExpr:
		value := p.expr(n.X, nil)
		var locations []*permissionLocation
		if pointerType(value) {
			locations = value.locations
			if len(locations) == 0 {
				locations = []*permissionLocation{value.location}
			}
		} else {
			locations = p.locations(n.X)
		}
		if value == nil || value.typ == nil {
			return nil
		}
		_, indices, _ := types.LookupFieldOrMethod(value.typ, true, p.c.pkg, n.Sel.Name)
		for _, index := range indices {
			if pointerType(value) {
				locations = value.locations
				if len(locations) == 0 {
					locations = []*permissionLocation{value.location}
				}
				value = p.deref(value)
			}
			structure, ok := value.typ.Underlying().(*types.Struct)
			if !ok || index >= structure.NumFields() {
				return nil
			}
			field := structure.Field(index).Name()
			projected := make([]*permissionLocation, len(locations))
			for i, location := range locations {
				if location != nil {
					projected[i] = &permissionLocation{root: location.root, fields: append(append([]string(nil), location.fields...), field)}
				}
			}
			locations, value = projected, p.fields(value)[field]
		}
		return locations
	}
	return nil
}

func (p *permissionChecker) place(node ast.Expr) (bool, bool) {
	switch n := node.(type) {
	case *ast.Ident, *ast.PathExpr:
		s := p.c.symbol(n)
		return s != nil && s.Kind != resolve.Constant && s.Kind != resolve.Function && s.Kind != resolve.Type && s.Kind != resolve.Variant, true
	case *ast.ParenExpr:
		return p.place(n.X)
	case *ast.StarExpr:
		q := p.expr(n.X, nil)
		return pointerType(q), q == nil || (q.ref != ReadRef && !q.shared)
	case *ast.SelectorExpr:
		q := p.expr(n.X, nil)
		addressable, writable := p.place(n.X)
		if q == nil || q.typ == nil {
			return addressable, writable
		}
		_, indices, _ := types.LookupFieldOrMethod(q.typ, true, p.c.pkg, n.Sel.Name)
		for _, index := range indices {
			if pointerType(q) {
				addressable = true
				writable = writable && q.ref != ReadRef && !q.shared
				q = p.deref(q)
			}
			structure, ok := q.typ.Underlying().(*types.Struct)
			if !ok || index >= structure.NumFields() {
				break
			}
			q = p.fields(q)[structure.Field(index).Name()]
		}
		return addressable, writable
	case *ast.IndexExpr:
		q := p.expr(n.X, nil)
		if q == nil || q.typ == nil {
			return false, true
		}
		if pointerType(q) {
			return true, q.ref != ReadRef && !q.shared
		}
		switch q.typ.Underlying().(type) {
		case *types.Slice:
			return true, !q.shared
		case *types.Map:
			return false, !q.shared
		case *types.Array:
			return p.place(n.X)
		}
	}
	return false, true
}

func (p *permissionChecker) adapt(got, want *permission) *permission {
	if got == nil {
		return want
	}
	if want == nil {
		return got
	}
	r := *got
	if want.ref != ValueRef {
		r.ref = want.ref
	}
	if want.typ != nil && (got.typ == nil || untyped(got.typ)) {
		r.typ = want.typ
	}
	return &r
}

func (p *permissionChecker) literalField(q *permission, index int, name string) (*permission, string) {
	if q == nil || q.typ == nil {
		return nil, ""
	}
	if st, ok := q.typ.Underlying().(*types.Struct); ok {
		if name == "" && index < st.NumFields() {
			name = st.Field(index).Name()
		}
		return p.fields(q)[name], name
	}
	return p.element(q), ""
}

func (p *permissionChecker) constructorType(node ast.Expr, con *Constructor, want *permission) *permission {
	q := p.fromGo(instanceType(con.Enum))
	if want != nil && p.c.out.EnumOf(want.typ) != nil {
		q = want
	}
	var path *ast.Path
	switch n := node.(type) {
	case *ast.PathExpr:
		path = n.Path
	case *ast.CompositeLit:
		if t, ok := n.Type.(*ast.PathType); ok {
			path = t.Path
		}
	case *ast.CallExpr:
		return p.constructorType(n.Fun, con, want)
	}
	if path != nil {
		for _, segment := range path.Segments {
			if segment.Args != nil {
				r := *q
				r.args = nil
				for _, a := range segment.Args.Args {
					r.args = append(r.args, p.astType(a.Type, nil))
				}
				q = &r
				break
			}
		}
	}
	return q
}

func (p *permissionChecker) enumLiteral(n *ast.CompositeLit, con *Constructor, want *permission) *permission {
	q := p.constructorType(n, con, want)
	r := *q
	r.fields = copyFields(q.fields)
	q = &r
	for i, elt := range n.Elts {
		value := elt
		index := i
		if kv, ok := elt.(*ast.KeyValue); ok {
			value = kv.Value
			if id, ok := kv.Key.(*ast.Ident); ok {
				for j, f := range con.Variant.Fields {
					if f.Name == id.Name {
						index = j
						break
					}
				}
			}
		}
		field := p.enumField(q, con.Variant, index)
		got := p.expr(value, field)
		p.require(value, got, field)
		q.fields[permissionFieldKey(con.Variant, index)] = p.adapt(got, field)
	}
	return q
}

func (p *permissionChecker) method(n *ast.SelectorExpr, receiver *permission, method *types.Func, indices []int) *permission {
	q := p.fromGo(method.Type())
	locations := p.locations(n.X)
	addressable, writable := p.place(n.X)
	for _, index := range indices[:max(0, len(indices)-1)] {
		if pointerType(receiver) {
			addressable = true
			pointer := receiver
			locations = pointer.locations
			if len(locations) == 0 {
				locations = []*permissionLocation{pointer.location}
			}
			receiver = p.deref(pointer)
			if pointer.ref == ReadRef || pointer.shared {
				receiver, writable = p.readValue(receiver), false
			}
		}
		structure, ok := receiver.typ.Underlying().(*types.Struct)
		if !ok || index >= structure.NumFields() {
			break
		}
		field := structure.Field(index).Name()
		for i, location := range locations {
			if location != nil {
				locations[i] = &permissionLocation{root: location.root, fields: append(append([]string(nil), location.fields...), field)}
			}
		}
		receiver = p.fields(receiver)[field]
	}
	expose := func(want *permission) {
		p.exposures = append(p.exposures, permissionExposure{file: p.c.file, node: n, locations: locations, want: want})
	}
	fn := p.sourceMethod(method)
	if fn != nil {
		actual := receiver
		if pointerType(fn.typ.receiver) && !pointerType(receiver) {
			if !addressable {
				p.c.diag(n.X, ReferenceAddress, "pointer method requires an addressable receiver")
			}
			mode := fn.typ.receiver.ref
			if !writable {
				mode = ReadRef
			}
			actual = &permission{typ: types.NewPointer(receiver.typ), elem: receiver, ref: mode}
			expose(fn.typ.receiver)
		} else if !pointerType(fn.typ.receiver) && pointerType(receiver) {
			actual = p.deref(receiver)
			if receiver.ref == ReadRef || receiver.shared {
				actual = p.readValue(actual)
			}
		}
		sub := map[*types.TypeParam]*permission{}
		p.inferPermission(fn.typ.receiver, actual, sub)
		q = p.substitutePermission(fn.typ, sub)
		p.require(n, actual, q.receiver)
		p.checkGenericCall(q, q.params, sub)
	} else if sig, ok := method.Type().(*types.Signature); ok && sig.Recv() != nil {
		actual := receiver
		if pointerType(p.fromGo(sig.Recv().Type())) && !pointerType(actual) {
			if !addressable {
				p.c.diag(n.X, ReferenceAddress, "pointer method requires an addressable receiver")
			}
			mode := ValueRef
			if !writable {
				mode = ReadRef
			}
			actual = &permission{typ: types.NewPointer(receiver.typ), elem: receiver, ref: mode}
			expose(p.fromGo(sig.Recv().Type()))
		} else if !pointerType(p.fromGo(sig.Recv().Type())) && pointerType(actual) {
			actual = p.deref(actual)
			if receiver.ref == ReadRef || receiver.shared {
				actual = p.readValue(actual)
			}
		}
		p.require(n, actual, p.fromGo(sig.Recv().Type()))
	}
	return q
}

func (p *permissionChecker) checkClosure(n *ast.FuncLit, q *permission, record bool) {
	if p.activeClosures[n] {
		return
	}
	p.activeClosures[n] = true
	defer delete(p.activeClosures, n)
	outer, declared, results := p.env, p.declared, p.results
	captured, effects := p.captured, p.effects
	asyncEffects := p.asyncEffects
	p.captured, p.effects = clonePermissionEnv(outer), permissionEnv{}
	p.asyncEffects = permissionEnv{}
	if record {
		p.closures = append(p.closures, permissionClosure{p.c.file, n, clonePermissionEnv(outer), q})
	}
	p.env, p.declared, p.results = clonePermissionEnv(outer), clonePermissionEnv(declared), q.results
	p.bindParams(n.Params, q.params)
	p.bindParams(n.Results, q.results)
	p.functionBlock(n.Body)
	if n.Expr != nil {
		var want *permission
		if len(q.results) == 1 {
			want = q.results[0]
		}
		got := p.expr(n.Expr, want)
		p.require(n.Expr, got, want)
		if len(q.results) == 0 {
			q.results = []*permission{got}
		}
	}
	p.env, p.declared, p.results = outer, declared, results
	if q.effects == nil {
		q.effects = permissionEnv{}
	}
	for s, value := range p.effects {
		q.effects[s] = p.merge(q.effects[s], value)
	}
	if q.asyncEffects == nil {
		q.asyncEffects = permissionEnv{}
	}
	for s, value := range p.asyncEffects {
		q.asyncEffects[s] = p.merge(q.asyncEffects[s], value)
	}
	p.captured, p.effects = captured, effects
	p.asyncEffects = asyncEffects
}
