package refs

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
)

func referenceElement(src []byte, ts []lex, start int) (string, int) {
	var best string
	end := 0
	for j := start; j < len(ts); j++ {
		if ts[j].kind == token.SEMICOLON || ts[j].kind == token.LBRACE || ts[j].kind == token.COMMA {
			break
		}
		e, err := parser.ParseExpr(string(src[ts[start].start:ts[j].end]))
		if err != nil {
			continue
		}
		if typeSyntax(e) {
			var b bytes.Buffer
			_ = format.Node(&b, token.NewFileSet(), e)
			best = b.String()
			end = ts[j].end
		}
	}
	if s := scalar(best); s != "" {
		best = s
	}
	return best, end
}
func typeSyntax(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return typeSyntax(e.X)
	case *ast.StarExpr:
		return typeSyntax(e.X)
	case *ast.ArrayType:
		return typeSyntax(e.Elt)
	case *ast.MapType:
		return typeSyntax(e.Key) && typeSyntax(e.Value)
	}
	return false
}
func (c *checker) declarations() string {
	s := declarations()
	keys := make([]string, 0, len(c.elements))
	for e := range c.elements {
		keys = append(keys, e)
	}
	sort.Strings(keys)
	for _, e := range keys {
		if scalar(e) != "" {
			continue
		}
		for _, p := range []Permission{Read, Write} {
			origin := "__hgo_ref_ro"
			if p == Write {
				origin = "__hgo_ref_rw"
			}
			s += fmt.Sprintf("\ntype %s = %s[%s]\n", c.name(Ref{e, p}), origin, e)
		}
	}
	return s
}

// qualification is an access-path type qualifier. read protects the current
// storage slot; deep projects read access to ordinary data below that slot.
// fields/elem retain only shared capabilities after copying a value aggregate.
// Object keys are resolved lexical identities, never source variable names.
type qualification struct {
	read, deep bool
	fields     map[string]*qualification
	elem       *qualification
}

func restricted(q *qualification) bool {
	return q != nil && (q.read || q.deep || len(q.fields) > 0 || restricted(q.elem))
}
func join(a, b *qualification) *qualification {
	if !restricted(a) {
		return b
	}
	if !restricted(b) {
		return a
	}
	if a.deep || b.deep {
		return &qualification{read: a.read || b.read, deep: true}
	}
	q := &qualification{read: a.read || b.read, fields: map[string]*qualification{}, elem: join(a.elem, b.elem)}
	for k, v := range a.fields {
		q.fields[k] = v
	}
	for k, v := range b.fields {
		q.fields[k] = join(q.fields[k], v)
	}
	return q
}
func sameQ(a, b *qualification) bool {
	if !restricted(a) && !restricted(b) {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.read != b.read || a.deep != b.deep || len(a.fields) != len(b.fields) || !sameQ(a.elem, b.elem) {
		return false
	}
	for k, v := range a.fields {
		if !sameQ(v, b.fields[k]) {
			return false
		}
	}
	return true
}
func projection(q *qualification, field string) *qualification {
	if q == nil {
		return nil
	}
	if q.deep {
		return &qualification{read: true, deep: true}
	}
	if field != "" {
		return q.fields[field]
	}
	return q.elem
}
func copied(t types.Type, q *qualification) *qualification {
	if !restricted(q) || t == nil {
		return nil
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Interface, *types.Chan, *types.Signature:
		return &qualification{deep: q.deep, elem: q.elem, fields: q.fields}
	}
	switch t := t.Underlying().(type) {
	case *types.Struct:
		out := &qualification{fields: map[string]*qualification{}}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if v := copied(f.Type(), projection(q, f.Name())); restricted(v) {
				out.fields[f.Name()] = v
			}
		}
		if restricted(out) {
			return out
		}
	case *types.Array:
		if v := copied(t.Elem(), projection(q, "")); restricted(v) {
			return &qualification{elem: v}
		}
	}
	return nil
}
func (c *checker) quality(e ast.Expr) *qualification {
	if e == nil {
		return nil
	}
	e = unparen(e)
	if r, ok := c.refType(c.typ(e)); ok && r.Permission == Read {
		return &qualification{deep: true}
	}
	switch e := e.(type) {
	case *ast.Ident:
		if q, ok := c.pointQualifications[e]; ok {
			return q
		}
		return c.qualifications[c.binding(e)]
	case *ast.StarExpr:
		return projection(c.quality(e.X), "")
	case *ast.SelectorExpr:
		q := c.quality(e.X)
		t := c.typ(e.X)
		if sel := c.info.Selections[e]; sel != nil && sel.Kind() == types.FieldVal {
			for _, i := range sel.Index() {
				if p, ok := t.Underlying().(*types.Pointer); ok {
					q = projection(q, "")
					t = p.Elem()
				}
				st, ok := t.Underlying().(*types.Struct)
				if !ok {
					return nil
				}
				f := st.Field(i)
				q = projection(q, f.Name())
				t = f.Type()
			}
			return q
		}
		return nil
	case *ast.IndexExpr:
		return projection(c.quality(e.X), "")
	case *ast.SliceExpr:
		q := c.quality(e.X)
		if q != nil && q.read {
			return &qualification{deep: true}
		}
		return q
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			q := c.quality(e.X)
			if restricted(q) {
				return &qualification{elem: q}
			}
		}
	}
	return nil
}
func (c *checker) checkQualifications() error {
	c.qualifications = map[types.Object]*qualification{}
	// First compute a conservative summary for forward captures and unsupported
	// control flow. refineLocalQualifications then supplies point facts for the
	// supported local flow, including strong rebinding and loop backedges.
	changed := true
	for changed {
		changed = false
		bind := func(e ast.Expr, q *qualification) {
			id, ok := unparen(e).(*ast.Ident)
			if !ok || id.Name == "_" {
				return
			}
			obj := c.binding(id)
			if obj == nil {
				return
			}
			v := join(c.qualifications[obj], copied(c.typ(e), q))
			if !sameQ(v, c.qualifications[obj]) {
				c.qualifications[obj] = v
				changed = true
			}
		}
		ast.Inspect(c.file, func(n ast.Node) bool {
			if n == nil || c.offset(n.Pos()) >= len(c.m.origin) {
				return false
			}
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Lhs) == len(n.Rhs) {
					for i, r := range n.Rhs {
						bind(n.Lhs[i], c.quality(r))
					}
				}
			case *ast.ValueSpec:
				if len(n.Names) == len(n.Values) {
					for i, r := range n.Values {
						bind(n.Names[i], c.quality(r))
					}
				}
			case *ast.RangeStmt:
				if n.Key != nil {
					if _, ok := c.typ(n.X).Underlying().(*types.Map); ok {
						bind(n.Key, projection(c.quality(n.X), ""))
					}
				}
				if n.Value != nil {
					bind(n.Value, projection(c.quality(n.X), ""))
				}
			}
			return true
		})
	}
	c.refineLocalQualifications()
	var failure error
	ast.Inspect(c.file, func(n ast.Node) bool {
		if n == nil || failure != nil || c.offset(n.Pos()) >= len(c.m.origin) {
			return false
		}
		fail := func(e ast.Node, s string) {
			conservative := false
			ast.Inspect(e, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					obj := c.binding(id)
					if obj != nil && c.conservativeObjects[obj] && restricted(c.qualifications[obj]) {
						if _, explicit := c.refType(obj.Type()); !explicit {
							conservative = true
						}
					}
				}
				return true
			})
			if conservative {
				s += "; prototype tracking limitation: captured, address-taken or control-flow slot uses a conservative qualification; use a separate fresh local"
			}
			failure = c.fail(e, s)
		}
		boundary := func(e ast.Expr) {
			if restricted(copied(c.typ(e), c.quality(e))) {
				if _, ok := c.refType(c.typ(e)); !ok {
					fail(e, "readonly projection qualification would be erased; this boundary is unsupported")
				}
			}
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if q := c.quality(l); q != nil && q.read {
					fail(l, "cannot write through a readonly projection")
				}
			}
			for i, r := range n.Rhs {
				if len(n.Lhs) != len(n.Rhs) && restricted(c.quality(r)) {
					fail(r, "tuple assignment carrying readonly projections is unsupported")
					continue
				}
				if i >= len(n.Lhs) {
					boundary(r)
					continue
				}
				if id, ok := unparen(n.Lhs[i]).(*ast.Ident); ok {
					if id.Name == "_" {
						continue
					}
					obj := c.binding(id)
					if obj == nil {
						boundary(r)
						continue
					}
					if _, ok := obj.Type().Underlying().(*types.Interface); ok {
						boundary(r)
					}
					if c.escapingSlot(obj) {
						boundary(r)
					}
				} else {
					boundary(r)
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) != len(n.Values) {
				for _, v := range n.Values {
					if restricted(c.quality(v)) {
						fail(v, "tuple declaration carrying readonly projections is unsupported")
					}
				}
			}
			for i, v := range n.Values {
				if i >= len(n.Names) || c.escapingSlot(c.binding(n.Names[i])) || isInterface(c.typ(n.Type)) {
					if r, ok := c.refType(c.typ(n.Type)); n.Type != nil && ok && r.Permission == Read {
						continue
					}
					boundary(v)
				}
			}
		case *ast.IndexExpr:
			if t := c.typ(n.X); t != nil {
				if _, ok := t.Underlying().(*types.Map); ok && c.assignmentTarget(n) {
					boundary(n.Index)
				}
			}
		case *ast.IncDecStmt:
			if q := c.quality(n.X); q != nil && q.read {
				fail(n, "cannot write through a readonly projection")
			}
		case *ast.ReturnStmt:
			for i, v := range n.Results {
				if f := c.function(n); f != nil {
					sig := c.info.Defs[f.Name].Type().(*types.Signature)
					if i < sig.Results().Len() {
						if r, ok := c.refType(sig.Results().At(i).Type()); ok && r.Permission == Read {
							continue
						}
					}
				}
				boundary(v)
			}
		case *ast.CompositeLit:
			for _, v := range n.Elts {
				if kv, ok := v.(*ast.KeyValueExpr); ok {
					boundary(kv.Key)
					boundary(kv.Value)
				} else {
					boundary(v)
				}
			}
		case *ast.CallExpr:
			if c.isIRCast(n) {
				r, _ := c.refType(c.typ(n))
				if r.Permission == Write {
					if u, ok := unparen(n.Args[0]).(*ast.UnaryExpr); ok {
						if q := c.quality(u.X); restricted(q) {
							fail(n, "cannot obtain write access through a readonly projection")
						}
					}
				}
				break
			}
			boundary(n.Fun)
			builtin := ""
			if id, ok := n.Fun.(*ast.Ident); ok {
				if _, ok := c.info.Uses[id].(*types.Builtin); ok {
					builtin = id.Name
				}
			}
			for i, a := range n.Args {
				if sig, ok := c.typ(n.Fun).(*types.Signature); ok && i < sig.Params().Len() {
					if r, ok := c.refType(sig.Params().At(i).Type()); ok && r.Permission == Read {
						continue
					}
				}
				q := c.quality(a)
				if !restricted(q) {
					continue
				}
				switch builtin {
				case "len", "cap":
					continue
				case "copy", "append":
					if i == 1 && (builtin == "copy" || n.Ellipsis.IsValid()) {
						boundaryElement := copied(elementType(c.typ(a)), projection(q, ""))
						if !restricted(boundaryElement) {
							continue
						}
					}
				}
				boundary(a)
			}
		case *ast.SelectorExpr:
			if _, ok := c.info.Uses[n.Sel].(*types.Func); ok && restricted(c.quality(n.X)) {
				fail(n, "methods on readonly projections are unsupported")
			}
		case *ast.RangeStmt:
			if n.Tok != token.DEFINE {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil {
						id, local := unparen(e).(*ast.Ident)
						unsafe := !local
						if local {
							unsafe = id.Name != "_" && (c.escapingSlot(c.binding(id)) || isInterface(c.typ(e)))
						}
						if unsafe && restricted(copied(c.typ(e), projection(c.quality(n.X), ""))) {
							fail(e, "range destination would erase readonly projection qualification")
						}
					}
				}
			}
			if n.Tok != token.DEFINE {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil {
						if q := c.quality(e); q != nil && q.read {
							fail(e, "cannot write through a readonly projection")
						}
					}
				}
			}
		case *ast.SendStmt:
			boundary(n.Value)
		}
		if e, ok := n.(ast.Expr); ok && restricted(c.quality(e)) && c.typ(e) != nil {
			switch c.typ(e).Underlying().(type) {
			case *types.Interface, *types.Chan, *types.Signature:
				fail(e, "readonly interface/channel/function projections are unsupported")
			}
		}
		return failure == nil
	})
	return failure
}
func elementType(t types.Type) types.Type {
	switch t := t.Underlying().(type) {
	case *types.Slice:
		return t.Elem()
	case *types.Array:
		return t.Elem()
	case *types.Map:
		return t.Elem()
	}
	return nil
}

// Only map-key insertion/replacement stores the capability; lookup compares
// identity and may use a restricted pointer key without upgrading that pointer.
func (c *checker) assignmentTarget(e ast.Expr) bool {
	var n ast.Node = e
	for {
		p, ok := c.parents[n].(*ast.ParenExpr)
		if !ok {
			break
		}
		n = p
	}
	switch p := c.parents[n].(type) {
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			if lhs == n {
				return true
			}
		}
	case *ast.IncDecStmt:
		return p.X == n
	case *ast.RangeStmt:
		return p.Key == n || p.Value == n
	}
	return false
}

func isInterface(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// Named results and package slots cross the checked function's boundary even
// when a naked return or a range assignment hides the consuming expression.
func (c *checker) escapingSlot(obj types.Object) bool {
	if obj == nil || obj.Parent() == nil || obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
		return true
	}
	escaped := false
	ast.Inspect(c.file, func(n ast.Node) bool {
		f, ok := n.(*ast.FuncType)
		if !ok || f.Results == nil {
			return true
		}
		for _, field := range f.Results.List {
			for _, id := range field.Names {
				if c.info.Defs[id] == obj {
					escaped = true
				}
			}
		}
		return !escaped
	})
	return escaped
}
