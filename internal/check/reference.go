package check

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

// permission keeps source capabilities alongside, rather than inside, Go's
// representation types. A shared restriction concerns reachable storage; it
// does not make a copied struct's own scalar fields immutable.
type permission struct {
	typ                   types.Type
	ref                   RefMode
	shared                bool
	elem, key             *permission
	fields                map[string]*permission
	args, params, results []*permission
	receiver              *permission
	location              *permissionLocation
	locations             []*permissionLocation
	function              *permissionFunction
	closure               *ast.FuncLit
	effects               permissionEnv
}

type permissionEnv map[*resolve.Symbol]*permission

type permissionDeclaration struct {
	file *resolve.File
	node ast.Type
}

type permissionFunction struct {
	file *resolve.File
	decl *ast.FuncDecl
	typ  *permission
}

type permissionInitializer struct {
	file         *resolve.File
	pkg          *types.Package
	spec         *ast.ValueSpec
	active, done bool
}

type permissionChecker struct {
	c              *checker
	env            permissionEnv
	declared       permissionEnv
	results        []*permission
	functions      map[*resolve.Symbol]*permissionFunction
	named          map[*types.Named]permissionDeclaration
	enumFiles      map[*Enum]*resolve.File
	building       map[*resolve.Symbol]bool
	observed       permissionEnv
	closures       []permissionClosure
	flow           []*permissionFlow
	labels         map[*resolve.Symbol]permissionEnv
	labelsChanged  bool
	globals        permissionEnv
	activeCalls    map[*ast.FuncDecl]bool
	activeClosures map[*ast.FuncLit]bool
	captured       permissionEnv
	effects        permissionEnv
	instance       map[*types.TypeParam]*permission
	initializers   map[*resolve.Symbol]*permissionInitializer
}

type permissionClosure struct {
	file *resolve.File
	lit  *ast.FuncLit
	env  permissionEnv
	typ  *permission
}

func (c *checker) checkReferences() {
	p := &permissionChecker{c: c, env: permissionEnv{}, declared: permissionEnv{}, functions: map[*resolve.Symbol]*permissionFunction{}, named: map[*types.Named]permissionDeclaration{}, enumFiles: map[*Enum]*resolve.File{}, building: map[*resolve.Symbol]bool{}, observed: permissionEnv{}, initializers: map[*resolve.Symbol]*permissionInitializer{}}
	for s, d := range c.declarations {
		if n := c.named[s]; n != nil {
			p.named[n.Origin()] = permissionDeclaration{d.file, d.node}
		}
	}
	p.eachFile(func(items []ast.Item) { p.collect(items) })
	p.activeCalls = map[*ast.FuncDecl]bool{}
	p.activeClosures = map[*ast.FuncLit]bool{}
	p.globals = p.env
	p.eachFile(func(items []ast.Item) { p.globalValues(items) })
	globals := p.env
	p.globals = globals
	p.eachFile(func(items []ast.Item) { p.checkItems(items, globals) })
}

func (p *permissionChecker) eachFile(f func([]ast.Item)) {
	for _, pkg := range p.c.out.Resolve.Packages {
		p.c.pkg = p.c.packages[pkg]
		for _, file := range pkg.Files {
			p.c.file = file
			f(file.AST.Items)
		}
	}
}

func (p *permissionChecker) collect(items []ast.Item) {
	for _, item := range items {
		switch n := item.(type) {
		case *ast.FuncDecl:
			q := p.functionType(n.Params, n.Results)
			q.typ = p.c.out.Symbols[p.c.symbol(n.Name)]
			if len(n.Recv) != 0 {
				q.receiver = p.astType(n.Recv[0].Type, nil)
			}
			f := &permissionFunction{p.c.file, n, q}
			p.functions[p.c.symbol(n.Name)] = f
			q.function = f
		case *ast.EnumDecl:
			p.enumFiles[p.c.out.Enums[n]] = p.c.file
		case *ast.VarDecl:
			p.collectInitializers(n.Specs)
		case *ast.ConstDecl:
			p.collectInitializers(n.Specs)
		case *ast.ModDecl:
			p.collect(n.Items)
		case *ast.DeclareDecl:
			p.collect([]ast.Item{n.Decl})
		case *ast.ImplDecl:
			p.collect(n.Items)
		}
	}
}

func (p *permissionChecker) collectInitializers(specs []*ast.ValueSpec) {
	for _, spec := range specs {
		initializer := &permissionInitializer{file: p.c.file, pkg: p.c.pkg, spec: spec}
		for _, name := range spec.Names {
			p.initializers[p.c.symbol(name)] = initializer
		}
	}
}

func (p *permissionChecker) initializeGlobal(initializer *permissionInitializer) {
	if initializer == nil || initializer.done || initializer.active {
		return
	}
	initializer.active = true
	oldFile, oldPkg := p.c.file, p.c.pkg
	p.c.file, p.c.pkg = initializer.file, initializer.pkg
	p.values([]*ast.ValueSpec{initializer.spec})
	p.c.file, p.c.pkg = oldFile, oldPkg
	initializer.done, initializer.active = true, false
}

func (p *permissionChecker) globalValues(items []ast.Item) {
	for _, item := range items {
		switch n := item.(type) {
		case *ast.VarDecl:
			for _, spec := range n.Specs {
				for _, name := range spec.Names {
					p.initializeGlobal(p.initializers[p.c.symbol(name)])
				}
			}
		case *ast.ConstDecl:
			for _, spec := range n.Specs {
				for _, name := range spec.Names {
					p.initializeGlobal(p.initializers[p.c.symbol(name)])
				}
			}
		case *ast.ModDecl:
			p.globalValues(n.Items)
		case *ast.DeclareDecl:
			p.globalValues([]ast.Item{n.Decl})
		}
	}
}

func (p *permissionChecker) checkItems(items []ast.Item, globals permissionEnv) {
	for _, item := range items {
		switch n := item.(type) {
		case *ast.FuncDecl:
			if n.Body == nil {
				continue
			}
			p.env, p.declared, p.observed = clonePermissionEnv(globals), permissionEnv{}, permissionEnv{}
			p.closures = nil
			f := p.functions[p.c.symbol(n.Name)]
			p.results = f.typ.results
			p.bindParams(n.Params, f.typ.params)
			p.bindParams(n.Results, f.typ.results)
			if f.typ.receiver != nil {
				p.bindParams(n.Recv, []*permission{f.typ.receiver})
			}
			p.functionBlock(n.Body)
			// Captured variables are slots, not snapshots. Recheck captures with
			// every capability that can be assigned while the closure exists.
			for i := 0; i < len(p.closures); i++ {
				closure := p.closures[i]
				p.c.file = closure.file
				p.env = clonePermissionEnv(closure.env)
				for s, q := range p.env {
					p.env[s] = p.merge(q, p.observed[s])
				}
				p.checkClosure(closure.lit, closure.typ, false)
			}
		case *ast.ModDecl:
			p.checkItems(n.Items, globals)
		case *ast.DeclareDecl:
			p.checkItems([]ast.Item{n.Decl}, globals)
		case *ast.ImplDecl:
			p.checkItems(n.Items, globals)
		}
	}
}

func (p *permissionChecker) functionType(params, results []*ast.Param) *permission {
	return &permission{params: p.paramTypes(params), results: p.paramTypes(results)}
}

func (p *permissionChecker) paramTypes(params []*ast.Param) []*permission {
	var result []*permission
	for _, param := range params {
		q := p.astType(param.Type, nil)
		if param.Variadic {
			q = &permission{typ: types.NewSlice(q.typ), elem: q}
		}
		count := len(param.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			result = append(result, q)
		}
	}
	return result
}

func (p *permissionChecker) bindParams(params []*ast.Param, values []*permission) {
	index := 0
	for _, param := range params {
		count := len(param.Names)
		if count == 0 {
			index++
			continue
		}
		for _, name := range param.Names {
			if index < len(values) {
				s := p.c.symbol(name)
				p.env[s], p.declared[s] = values[index], values[index]
			}
			index++
		}
	}
}

func (p *permissionChecker) fromGo(t types.Type) *permission {
	q := &permission{typ: t}
	if t == nil {
		return q
	}
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			q.args = append(q.args, p.fromGo(t.TypeArgs().At(i)))
		}
	case *types.Pointer:
		q.elem = p.fromGo(t.Elem())
	case *types.Slice:
		q.elem = p.fromGo(t.Elem())
	case *types.Array:
		q.elem = p.fromGo(t.Elem())
	case *types.Map:
		q.key, q.elem = p.fromGo(t.Key()), p.fromGo(t.Elem())
	case *types.Chan:
		q.elem = p.fromGo(t.Elem())
	case *types.Signature:
		for i := 0; i < t.Params().Len(); i++ {
			q.params = append(q.params, p.fromGo(t.Params().At(i).Type()))
		}
		for i := 0; i < t.Results().Len(); i++ {
			q.results = append(q.results, p.fromGo(t.Results().At(i).Type()))
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			q.results = append(q.results, p.fromGo(t.At(i).Type()))
		}
	}
	return q
}

func (p *permissionChecker) astType(node ast.Type, sub map[*types.TypeParam]*permission) *permission {
	if node == nil {
		return nil
	}
	if sub == nil {
		sub = p.instance
	}
	t := p.c.typ(node)
	q := p.fromGo(t)
	switch n := node.(type) {
	case *ast.PathType:
		if tp, ok := t.(*types.TypeParam); ok && sub[tp] != nil {
			return sub[tp]
		}
		if n.Path != nil && len(n.Path.Segments) != 0 {
			seg := n.Path.Segments[len(n.Path.Segments)-1]
			if seg.Args != nil {
				q.args = nil
				for _, a := range seg.Args.Args {
					q.args = append(q.args, p.astType(a.Type, sub))
				}
			}
			s := p.c.symbol(seg.Name)
			if decl := p.c.declarations[s]; decl != nil && !p.building[s] {
				p.building[s] = true
				old := p.c.file
				p.c.file = decl.file
				args := p.substitution(p.c.named[s], q.args)
				// Named reference and container types retain their source
				// capabilities even though go/types exposes only their layout.
				if _, structure := decl.node.(*ast.StructType); !structure || decl.alias {
					under := p.astType(decl.node, args)
					if decl.alias {
						q = under
					} else if under != nil {
						q.ref, q.elem, q.key = under.ref, under.elem, under.key
						q.params, q.results = under.params, under.results
					}
				}
				p.c.file = old
				delete(p.building, s)
			}
		}
	case *ast.RefType:
		q.ref = ReadRef
		if n.Mut {
			q.ref = WriteRef
		}
		q.elem = p.astType(n.Elem, sub)
	case *ast.PointerType:
		q.elem = p.astType(n.Elem, sub)
	case *ast.SliceType:
		q.elem = p.astType(n.Elem, sub)
	case *ast.ArrayType:
		q.elem = p.astType(n.Elem, sub)
	case *ast.MapType:
		q.key, q.elem = p.astType(n.Key, sub), p.astType(n.Value, sub)
	case *ast.ChanType:
		q.elem = p.astType(n.Elem, sub)
	case *ast.ParenType:
		return p.astType(n.Type, sub)
	case *ast.StructType:
		q.fields = map[string]*permission{}
		for _, field := range n.Fields {
			value := p.astType(field.Type, sub)
			for _, name := range field.Names {
				q.fields[name.Name] = value
			}
		}
	case *ast.FuncType:
		q.params, q.results = p.paramTypes(n.Params), p.paramTypes(n.Results)
		for i, parameter := range q.params {
			q.params[i] = p.substitutePermission(parameter, sub)
		}
		for i, result := range q.results {
			q.results[i] = p.substitutePermission(result, sub)
		}
	}
	return q
}

func (p *permissionChecker) substitution(n *types.Named, args []*permission) map[*types.TypeParam]*permission {
	sub := map[*types.TypeParam]*permission{}
	if n != nil {
		for i := 0; i < n.TypeParams().Len() && i < len(args); i++ {
			sub[n.TypeParams().At(i)] = args[i]
		}
	}
	return sub
}

func (p *permissionChecker) fields(q *permission) map[string]*permission {
	fields := map[string]*permission{}
	if q == nil || q.typ == nil {
		return fields
	}
	if n, ok := types.Unalias(q.typ).(*types.Named); ok {
		if decl, exists := p.named[n.Origin()]; exists {
			old := p.c.file
			p.c.file = decl.file
			under := p.astType(decl.node, p.substitution(n.Origin(), q.args))
			p.c.file = old
			for name, value := range p.fields(under) {
				fields[name] = value
			}
		}
	}
	if st, ok := q.typ.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			if fields[f.Name()] == nil {
				fields[f.Name()] = p.fromGo(f.Type())
			}
		}
	}
	for name, value := range q.fields {
		fields[name] = value
	}
	return fields
}

func (p *permissionChecker) enumField(q *permission, variant *Variant, index int) *permission {
	if q == nil || index >= len(variant.Fields) {
		return p.fromGo(nil)
	}
	key := variant.Name + "." + strconv.Itoa(index)
	if field := q.fields[key]; field != nil {
		if q.shared {
			return p.readValue(field)
		}
		return field
	}
	instance := p.c.out.EnumOf(q.typ)
	if instance == nil {
		return p.fromGo(variant.Fields[index].Type)
	}
	var field *permission
	if instance.Enum.Builtin {
		arg := 0
		if variant.Name == "Err" {
			arg = 1
		}
		if arg < len(q.args) {
			field = q.args[arg]
		}
	} else if variant.Decl != nil {
		old := p.c.file
		p.c.file = p.enumFiles[instance.Enum]
		cursor := 0
		for _, decl := range variant.Decl.Fields {
			count := len(decl.Names)
			if count == 0 {
				count = 1
			}
			if index >= cursor && index < cursor+count {
				field = p.astType(decl.Type, p.substitution(instance.Enum.Type, q.args))
				break
			}
			cursor += count
		}
		p.c.file = old
	}
	if field == nil {
		field = p.fromGo(instance.Fields(variant)[index].Type)
	}
	if q.shared {
		field = p.readValue(field)
	}
	return field
}

func (p *permissionChecker) element(q *permission) *permission {
	if q == nil {
		return p.fromGo(nil)
	}
	if q.elem != nil {
		return q.elem
	}
	if n, ok := types.Unalias(q.typ).(*types.Named); ok {
		if decl, exists := p.named[n.Origin()]; exists {
			old := p.c.file
			p.c.file = decl.file
			under := p.astType(decl.node, p.substitution(n.Origin(), q.args))
			p.c.file = old
			if under.elem != nil {
				return under.elem
			}
		}
	}
	if q.typ != nil {
		switch t := q.typ.Underlying().(type) {
		case *types.Pointer:
			return p.fromGo(t.Elem())
		case *types.Slice:
			return p.fromGo(t.Elem())
		case *types.Array:
			return p.fromGo(t.Elem())
		case *types.Map:
			return p.fromGo(t.Elem())
		case *types.Chan:
			return p.fromGo(t.Elem())
		}
	}
	return p.fromGo(nil)
}

func (p *permissionChecker) readValue(q *permission) *permission {
	return p.readValueSeen(q, map[types.Type]bool{})
}

func (p *permissionChecker) readValueSeen(q *permission, seen map[types.Type]bool) *permission {
	if q == nil || q.typ == nil {
		return q
	}
	r := *q
	if e := p.c.out.EnumOf(q.typ); e != nil {
		if !e.Enum.Builtin {
			r.shared = true
			return &r
		}
		r.fields = copyFields(q.fields)
		for _, v := range e.Enum.Variants {
			for i := range v.Fields {
				r.fields[v.Name+"."+strconv.Itoa(i)] = p.readValueSeen(p.enumField(q, v, i), seen)
			}
		}
		return &r
	}
	switch q.typ.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface, *types.Signature:
		r.shared = true
	case *types.Array:
		r.elem = p.readValueSeen(p.element(q), seen)
	case *types.Struct:
		if permissionTypeSeen(seen, q.typ) {
			return &r
		}
		seen[q.typ] = true
		r.fields = map[string]*permission{}
		for name, value := range p.fields(q) {
			r.fields[name] = p.readValueSeen(value, seen)
		}
		delete(seen, q.typ)
	}
	return &r
}

func copyFields(in map[string]*permission) map[string]*permission {
	out := map[string]*permission{}
	for name, q := range in {
		out[name] = q
	}
	return out
}

func (p *permissionChecker) merge(a, b *permission) *permission {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a == b {
		return a
	}
	r := *a
	if a.ref == ReadRef || b.ref == ReadRef {
		r.ref = ReadRef
	} else if a.ref == WriteRef || b.ref == WriteRef {
		r.ref = WriteRef
	}
	r.shared = a.shared || b.shared
	r.elem, r.key = p.merge(a.elem, b.elem), p.merge(a.key, b.key)
	r.receiver = p.merge(a.receiver, b.receiver)
	r.effects = clonePermissionEnv(a.effects)
	if r.effects == nil && len(b.effects) > 0 {
		r.effects = permissionEnv{}
	}
	for s, q := range b.effects {
		r.effects[s] = p.merge(r.effects[s], q)
	}
	if a.function != b.function {
		r.function = nil
	}
	if a.closure != b.closure {
		r.closure = nil
	}
	if !samePermissionLocation(a.location, b.location) {
		r.location = nil
	}
	if pointerType(a) && pointerType(b) && (r.location == nil || len(a.locations) > 0 || len(b.locations) > 0) {
		r.location = nil
		r.locations = joinPermissionLocations(a, b)
	}
	r.fields = copyFields(a.fields)
	for name, q := range b.fields {
		r.fields[name] = p.merge(r.fields[name], q)
	}
	mergeList := func(a, b []*permission) []*permission {
		out := append([]*permission(nil), a...)
		for i, q := range b {
			if i < len(out) {
				out[i] = p.merge(out[i], q)
			} else {
				out = append(out, q)
			}
		}
		return out
	}
	r.args, r.params, r.results = mergeList(a.args, b.args), mergeList(a.params, b.params), mergeList(a.results, b.results)
	return &r
}

func (p *permissionChecker) equal(a, b *permission) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.ref != b.ref || a.shared != b.shared || len(a.fields) != len(b.fields) || len(a.args) != len(b.args) || len(a.params) != len(b.params) || len(a.results) != len(b.results) {
		return false
	}
	if a.function != b.function || a.closure != b.closure || len(a.effects) != len(b.effects) {
		return false
	}
	for s, q := range a.effects {
		if !p.equal(q, b.effects[s]) {
			return false
		}
	}
	if !p.equal(a.elem, b.elem) || !p.equal(a.key, b.key) || !p.equal(a.receiver, b.receiver) || !samePermissionLocation(a.location, b.location) {
		return false
	}
	if len(a.locations) != len(b.locations) {
		return false
	}
	for i, location := range a.locations {
		if !samePermissionLocation(location, b.locations[i]) {
			return false
		}
	}
	for k, q := range a.fields {
		if !p.equal(q, b.fields[k]) {
			return false
		}
	}
	for i, q := range a.args {
		if !p.equal(q, b.args[i]) {
			return false
		}
	}
	for i, q := range a.params {
		if !p.equal(q, b.params[i]) {
			return false
		}
	}
	for i, q := range a.results {
		if !p.equal(q, b.results[i]) {
			return false
		}
	}
	return true
}

func (p *permissionChecker) require(node ast.Node, got, want *permission) {
	if !p.compatible(got, want, map[[2]types.Type]bool{}) {
		p.c.diag(node, ReferencePermission, "cannot erase or increase read-only reference permissions")
	}
}

func (p *permissionChecker) compatible(got, want *permission, seen map[[2]types.Type]bool) bool {
	if got == nil || want == nil {
		return true
	}
	if (got.ref == ReadRef || got.shared) && want.ref != ReadRef && !want.shared {
		if got.ref == ReadRef || p.indirect(got) {
			return false
		}
	}
	if got.typ != nil && want.typ != nil {
		if _, ok := want.typ.Underlying().(*types.Interface); ok && p.c.out.EnumOf(want.typ) == nil {
			return !p.restricted(got, map[types.Type]bool{})
		}
	}
	if want.ref == ReadRef {
		return true
	}
	if !p.compatible(got.elem, want.elem, seen) || !p.compatible(got.key, want.key, seen) {
		return false
	}
	for i, q := range got.args {
		if i < len(want.args) && !p.compatible(q, want.args[i], seen) {
			return false
		}
	}
	for i, q := range got.params {
		if i < len(want.params) && !p.compatible(want.params[i], q, seen) {
			return false
		}
	}
	for i, q := range got.results {
		if i < len(want.results) && !p.compatible(q, want.results[i], seen) {
			return false
		}
	}
	pair := [2]types.Type{got.typ, want.typ}
	if permissionPairSeen(seen, pair) {
		return true
	}
	seen[pair] = true
	defer delete(seen, pair)
	if e := p.c.out.EnumOf(got.typ); e != nil {
		for _, v := range e.Enum.Variants {
			for i := range v.Fields {
				if !p.compatible(p.enumField(got, v, i), p.enumField(want, v, i), seen) {
					return false
				}
			}
		}
	} else if got.typ != nil {
		if _, ok := got.typ.Underlying().(*types.Struct); ok {
			wfields := p.fields(want)
			for name, q := range p.fields(got) {
				if !p.compatible(q, wfields[name], seen) {
					return false
				}
			}
		}
	}
	return true
}

func (p *permissionChecker) indirect(q *permission) bool {
	if q == nil || q.typ == nil {
		return false
	}
	if e := p.c.out.EnumOf(q.typ); e != nil {
		return !e.Enum.Builtin
	}
	switch q.typ.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface, *types.Signature:
		return true
	}
	return false
}

func (p *permissionChecker) restricted(q *permission, seen map[types.Type]bool) bool {
	if q == nil {
		return false
	}
	if q.ref == ReadRef || q.shared {
		return true
	}
	if permissionTypeSeen(seen, q.typ) {
		return false
	}
	seen[q.typ] = true
	defer delete(seen, q.typ)
	if p.restricted(q.elem, seen) || p.restricted(q.key, seen) {
		return true
	}
	for _, v := range q.args {
		if p.restricted(v, seen) {
			return true
		}
	}
	for _, v := range q.params {
		if p.restricted(v, seen) {
			return true
		}
	}
	for _, v := range q.results {
		if p.restricted(v, seen) {
			return true
		}
	}
	for _, v := range p.fields(q) {
		if p.restricted(v, seen) {
			return true
		}
	}
	if e := p.c.out.EnumOf(q.typ); e != nil {
		for _, v := range e.Enum.Variants {
			for i := range v.Fields {
				if p.restricted(p.enumField(q, v, i), seen) {
					return true
				}
			}
		}
	}
	return false
}

func (p *permissionChecker) reportWrite(node ast.Node) {
	p.c.diag(node, ReferencePermission, "cannot write through a read-only reference")
}

func permissionTypeSeen(seen map[types.Type]bool, t types.Type) bool {
	if seen[t] {
		return true
	}
	for previous := range seen {
		if t != nil && previous != nil && types.Identical(t, previous) {
			return true
		}
	}
	return false
}

func permissionPairSeen(seen map[[2]types.Type]bool, pair [2]types.Type) bool {
	if seen[pair] {
		return true
	}
	for previous := range seen {
		if pair[0] != nil && pair[1] != nil && previous[0] != nil && previous[1] != nil && types.Identical(pair[0], previous[0]) && types.Identical(pair[1], previous[1]) {
			return true
		}
	}
	return false
}

func (p *permissionChecker) setSymbol(s *resolve.Symbol, q *permission) {
	if s == nil {
		return
	}
	p.env[s] = q
	p.observed[s] = p.merge(p.observed[s], q)
	if p.captured[s] != nil {
		p.effects[s] = p.merge(p.effects[s], q)
	}
}

func permissionFieldKey(v *Variant, i int) string { return fmt.Sprintf("%s.%d", v.Name, i) }
