package check

import (
	"fmt"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// An application is the ordered source head (Self, positional inputs). Outputs
// and prerequisites identify evidence, but never identify the query itself.
func (i *Impl) ApplicationKey() string {
	if i == nil {
		return ""
	}
	return applicationKey(i.info, i.Trait, i.Target, i.Inputs, i.targetPermission, i.inputPermissions)
}

func (b *TraitBound) ApplicationKey() string {
	if b == nil {
		return ""
	}
	return applicationKey(b.info, b.Trait, b.Target, b.Inputs, b.targetPermission, b.inputPermissions)
}

func applicationKey(info *Info, trait *Trait, target types.Type, inputs []types.Type, self *permission, sources []*permission) string {
	var out strings.Builder
	if trait != nil {
		out.WriteString(trait.Marker)
	}
	out.WriteString("(")
	out.WriteString(sourceTypeKey(info, target, self))
	for i, input := range inputs {
		var source *permission
		if i < len(sources) {
			source = sources[i]
		}
		out.WriteString(",")
		out.WriteString(sourceTypeKey(info, input, source))
	}
	out.WriteString(")")
	return out.String()
}

func (b *TraitBound) EvidenceKey() string {
	if b == nil {
		return ""
	}
	return b.ApplicationKey() + associatedKey(b.info, b.Associated, b.associatedPermissions)
}

func (e Evidence) Key() string {
	if e.Bound != nil {
		return "bound:" + e.Bound.EvidenceKey()
	}
	if e.Impl != nil {
		i := e.Impl
		key := "impl:" + i.DeclID + ":" + i.ApplicationKey() + associatedKey(i.info, i.Associated, i.associatedPermissions)
		for _, premise := range i.RequirementEvidence {
			key += "{" + premise.Key() + "}"
		}
		return key
	}
	var keys []string
	for _, branch := range e.Cases {
		key := branch.Evidence.Key()
		for _, b := range branch.Requirements {
			key += "{" + b.EvidenceKey() + "}"
		}
		for _, b := range branch.GoConstraints {
			key += "{" + sourceTypeKey(nil, b.Target, nil) + ":" + sourceTypeKey(nil, b.Constraint, nil) + "}"
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "cases:" + strings.Join(keys, "|")
}

func associatedKey(info *Info, outputs map[string]types.Type, sources map[string]*permission) string {
	var names []string
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, name := range names {
		out.WriteString(";")
		out.WriteString(name)
		out.WriteString("=")
		out.WriteString(sourceTypeKey(info, outputs[name], sources[name]))
	}
	return out.String()
}

// Keys serialize nominal types and source constructors, with no transient flow
// capabilities or filesystem offsets. Equality is checked structurally below.
func sourceTypeKey(info *Info, typ types.Type, source *permission) string {
	if source != nil {
		typ = source.typ
	}
	if typ == nil {
		return "nil"
	}
	typ = types.Unalias(typ)
	child := func(t types.Type, q *permission) string { return sourceTypeKey(info, t, q) }
	elem, key := (*permission)(nil), (*permission)(nil)
	var args, params, results []*permission
	if source != nil {
		elem, key, args, params, results = source.elem, source.key, source.args, source.params, source.results
	}
	at := func(list []*permission, index int) *permission {
		if index < len(list) {
			return list[index]
		}
		return nil
	}
	qualify := func(pkg *types.Package) string {
		return info.CanonicalPackagePath(pkg)
	}
	memberID := func(object types.Object) string {
		if object.Exported() {
			return object.Name()
		}
		return qualify(object.Pkg()) + "." + object.Name()
	}
	_, nominal := typ.(*types.Named)
	if source != nil && source.ref != ValueRef && !nominal {
		return fmt.Sprintf("ref%d(%s)", source.ref, child(pointerElement(typ), elem))
	}
	if info != nil {
		if a, ok := info.arrayShape(typ); ok {
			length := strconv.FormatInt(a.Length.Value, 10)
			if a.Length.Param != nil {
				length = "const:" + a.Length.Param.Name.Name
			}
			return "[" + length + "]" + child(a.Elem, elem)
		}
	}
	switch t := typ.(type) {
	case *types.Basic:
		// byte and rune are alternate spellings of uint8 and int32. A
		// prerequisite key must agree with types.Identical for these aliases.
		return types.Typ[t.Kind()].Name()
	case *types.TypeParam:
		if info != nil {
			for _, impl := range info.Impls {
				for index, parameter := range impl.TypeParams {
					if t == parameter && impl.DeclID != "" {
						return "parameter:" + impl.DeclID + ":" + strconv.Itoa(index)
					}
				}
			}
			if identity := info.parameterIdentities[t]; identity != "" {
				return "parameter:" + identity
			}
		}
		return "parameter:" + qualify(t.Obj().Pkg()) + ":" + t.Obj().Name()
	case *types.Named:
		name := qualify(t.Obj().Pkg()) + "::" + t.Obj().Name()
		if t.TypeArgs().Len() == 0 {
			return name
		}
		var values []string
		for i := 0; i < t.TypeArgs().Len(); i++ {
			values = append(values, child(t.TypeArgs().At(i), at(args, i)))
		}
		return name + "[" + strings.Join(values, ",") + "]"
	case *types.Pointer:
		return "*" + child(t.Elem(), elem)
	case *types.Slice:
		return "[]" + child(t.Elem(), elem)
	case *types.Array:
		return fmt.Sprintf("[%d]", t.Len()) + child(t.Elem(), elem)
	case *types.Map:
		return "map[" + child(t.Key(), key) + "]" + child(t.Elem(), elem)
	case *types.Chan:
		return fmt.Sprintf("chan%d(", t.Dir()) + child(t.Elem(), elem) + ")"
	case *types.Signature:
		var in, out []string
		for i := 0; i < t.Params().Len(); i++ {
			in = append(in, child(t.Params().At(i).Type(), at(params, i)))
		}
		for i := 0; i < t.Results().Len(); i++ {
			out = append(out, child(t.Results().At(i).Type(), at(results, i)))
		}
		return fmt.Sprintf("func%t(%s)(%s)", t.Variadic(), strings.Join(in, ","), strings.Join(out, ","))
	case *types.Struct:
		var fields []string
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			var q *permission
			if source != nil {
				q = source.fields[f.Name()]
			}
			fields = append(fields, fmt.Sprintf("%s:%t:%q:%s", memberID(f), f.Embedded(), t.Tag(i), child(f.Type(), q)))
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	case *types.Interface:
		var members []string
		for index := 0; index < t.NumMethods(); index++ {
			method := t.Method(index)
			var q *permission
			if source != nil {
				q = source.fields[method.Name()]
			}
			members = append(members, memberID(method)+":"+child(method.Type(), q))
		}
		for index := 0; index < t.NumEmbeddeds(); index++ {
			members = append(members, child(t.EmbeddedType(index), nil))
		}
		sort.Strings(members)
		return "interface{" + strings.Join(members, ";") + "}"
	}
	return types.TypeString(typ, qualify)
}

func pointerElement(typ types.Type) types.Type {
	if pointer, ok := types.Unalias(typ).Underlying().(*types.Pointer); ok {
		return pointer.Elem()
	}
	return typ
}

func (c *checker) implHeadPermissions(impl *Impl) []*permission {
	return c.applicationPermissions(impl.Target, impl.Inputs, impl.targetPermission, impl.inputPermissions)
}

func (c *checker) boundHeadPermissions(bound *TraitBound) []*permission {
	return c.applicationPermissions(bound.Target, bound.Inputs, bound.targetPermission, bound.inputPermissions)
}

func (c *checker) applicationPermissions(target types.Type, inputs []types.Type, self *permission, sources []*permission) []*permission {
	p := c.newPermissionChecker()
	if self == nil {
		self = p.fromGo(target)
	}
	result := []*permission{self}
	for i, input := range inputs {
		q := (*permission)(nil)
		if i < len(sources) {
			q = sources[i]
		}
		if q == nil {
			q = p.fromGo(input)
		}
		result = append(result, q)
	}
	return result
}

func (c *checker) sameSourceType(a, b *permission) bool {
	if a == nil || b == nil {
		return a == b
	}
	return types.Identical(a.typ, b.typ) && sourceTypeKey(c.out, a.typ, a) == sourceTypeKey(c.out, b.typ, b)
}

func (c *checker) sameApplication(a, b *TraitBound) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Trait != b.Trait || len(a.Inputs) != len(b.Inputs) {
		return false
	}
	x, y := c.boundHeadPermissions(a), c.boundHeadPermissions(b)
	for i := range x {
		if !c.sameSourceType(x[i], y[i]) {
			return false
		}
	}
	return true
}

func (c *checker) implApplication(impl *Impl) *TraitBound {
	return &TraitBound{info: c.out, File: impl.File, Trait: impl.Trait, Target: impl.Target, TargetNode: impl.Decl.Type, Inputs: impl.Inputs, InputNodes: impl.InputNodes, targetPermission: impl.targetPermission, inputPermissions: impl.inputPermissions}
}

// Matching instantiates only declaration variables. Query parameters remain
// rigid even when a declaration's where clauses might later constrain them.
func (c *checker) matchApplicationHead(template *Impl, goal *TraitBound) (map[*types.TypeParam]types.Type, map[*ConstParam]ConstValue, map[*types.TypeParam]*permission, bool) {
	sub := map[*types.TypeParam]types.Type{}
	permissions := map[*types.TypeParam]*permission{}
	variables := map[*types.TypeParam]bool{}
	for _, parameter := range template.TypeParams {
		variables[parameter] = true
	}
	constants := newImplPatternContext(c.out, template.ConstParams)
	if template.Trait != goal.Trait || len(template.Inputs) != len(goal.Inputs) {
		return nil, nil, nil, false
	}
	p := c.newPermissionChecker()
	child := func(q *permission, typ types.Type) *permission {
		if q != nil {
			return q
		}
		return p.fromGo(typ)
	}
	var match func(*permission, *permission) bool
	match = func(left, right *permission) bool {
		if left == nil || right == nil {
			return left == right
		}
		a, b := types.Unalias(left.typ), types.Unalias(right.typ)
		if parameter, ok := a.(*types.TypeParam); ok && variables[parameter] && !c.out.isArray(a) && left.ref == ValueRef {
			if old := permissions[parameter]; old != nil {
				return c.sameSourceType(old, right)
			}
			sub[parameter], permissions[parameter] = b, right
			return true
		}
		if left.ref != right.ref {
			return false
		}
		if array, ok := c.out.arrayShape(a); ok {
			other, ok := c.out.arrayShape(b)
			return ok && constants.bind(array.Length, other.Length) && match(child(left.elem, array.Elem), child(right.elem, other.Elem))
		}
		switch a := a.(type) {
		case *types.Named:
			b, ok := b.(*types.Named)
			if !ok || a.Origin() != b.Origin() || a.TypeArgs().Len() != b.TypeArgs().Len() {
				return false
			}
			for index := 0; index < a.TypeArgs().Len(); index++ {
				var x, y *permission
				if index < len(left.args) {
					x = left.args[index]
				}
				if index < len(right.args) {
					y = right.args[index]
				}
				if !match(child(x, a.TypeArgs().At(index)), child(y, b.TypeArgs().At(index))) {
					return false
				}
			}
			return true
		case *types.Pointer:
			b, ok := b.(*types.Pointer)
			return ok && match(child(left.elem, a.Elem()), child(right.elem, b.Elem()))
		case *types.Slice:
			b, ok := b.(*types.Slice)
			return ok && match(child(left.elem, a.Elem()), child(right.elem, b.Elem()))
		case *types.Map:
			b, ok := b.(*types.Map)
			return ok && match(child(left.key, a.Key()), child(right.key, b.Key())) && match(child(left.elem, a.Elem()), child(right.elem, b.Elem()))
		case *types.Chan:
			b, ok := b.(*types.Chan)
			return ok && a.Dir() == b.Dir() && match(child(left.elem, a.Elem()), child(right.elem, b.Elem()))
		case *types.Struct:
			b, ok := b.(*types.Struct)
			if !ok || a.NumFields() != b.NumFields() {
				return false
			}
			for index := 0; index < a.NumFields(); index++ {
				x, y := a.Field(index), b.Field(index)
				if x.Id() != y.Id() || x.Embedded() != y.Embedded() || a.Tag(index) != b.Tag(index) || !match(child(left.fields[x.Name()], x.Type()), child(right.fields[y.Name()], y.Type())) {
					return false
				}
			}
			return true
		case *types.Signature:
			b, ok := b.(*types.Signature)
			if !ok || a.Variadic() != b.Variadic() || a.Params().Len() != b.Params().Len() || a.Results().Len() != b.Results().Len() {
				return false
			}
			for _, pair := range []struct {
				x, y   *types.Tuple
				xp, yp []*permission
			}{{a.Params(), b.Params(), left.params, right.params}, {a.Results(), b.Results(), left.results, right.results}} {
				for index := 0; index < pair.x.Len(); index++ {
					var x, y *permission
					if index < len(pair.xp) {
						x = pair.xp[index]
					}
					if index < len(pair.yp) {
						y = pair.yp[index]
					}
					if !match(child(x, pair.x.At(index).Type()), child(y, pair.y.At(index).Type())) {
						return false
					}
				}
			}
			return true
		case *types.Interface:
			b, ok := b.(*types.Interface)
			if !ok || !a.IsMethodSet() || !b.IsMethodSet() || a.NumMethods() != b.NumMethods() {
				return c.sameSourceType(left, right)
			}
			for index := 0; index < a.NumMethods(); index++ {
				x, y := a.Method(index), b.Method(index)
				if x.Id() != y.Id() || !match(child(left.fields[x.Name()], x.Type()), child(right.fields[y.Name()], y.Type())) {
					return false
				}
			}
			return true
		}
		return c.sameSourceType(left, right)
	}
	x, y := c.implHeadPermissions(template), c.boundHeadPermissions(goal)
	for index := range x {
		if !match(x[index], y[index]) {
			return nil, nil, nil, false
		}
	}
	if len(sub) != len(variables) || len(constants.bindings) != len(constants.parameters) {
		return nil, nil, nil, false
	}
	return sub, constants.bindings, permissions, true
}

// Source substitution composes chains before rebuilding structural nodes. A
// bare type parameter can bind an entire reference tree, including nested refs.
func (c *checker) substituteSourcePermission(q *permission, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, permissions map[*types.TypeParam]*permission) *permission {
	p := c.newPermissionChecker()
	active := map[*types.TypeParam]bool{}
	var replace func(*permission) *permission
	replace = func(q *permission) *permission {
		if q == nil {
			return nil
		}
		if parameter, ok := types.Unalias(q.typ).(*types.TypeParam); ok && !c.out.isArray(q.typ) && q.ref == ValueRef && !active[parameter] {
			next := permissions[parameter]
			if next == nil && sub[parameter] != nil && sub[parameter] != parameter {
				next = p.fromGo(sub[parameter])
			}
			if next != nil && next != q && !(next.typ == parameter && next.ref == ValueRef) {
				active[parameter] = true
				result := replace(next)
				delete(active, parameter)
				return result
			}
		}
		r := &permission{typ: c.out.Substitute(q.typ, sub, constants), ref: q.ref}
		r.elem, r.key = replace(q.elem), replace(q.key)
		for _, child := range q.args {
			r.args = append(r.args, replace(child))
		}
		for _, child := range q.params {
			r.params = append(r.params, replace(child))
		}
		for _, child := range q.results {
			r.results = append(r.results, replace(child))
		}
		if q.fields != nil {
			r.fields = map[string]*permission{}
			for name, child := range q.fields {
				r.fields[name] = replace(child)
			}
		}
		return r
	}
	return replace(q)
}
