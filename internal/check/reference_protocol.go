package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (p *permissionChecker) protocolCall(n *ast.CallExpr, _ *permission) (*permission, bool) {
	result := p.fromGo(p.c.out.Types[n])
	if p.c.out.Equalities[n] != nil {
		selector := n.Fun.(*ast.SelectorExpr)
		for _, arg := range append([]ast.Expr{selector.X}, n.Args...) {
			p.protocolRead(arg, "PartialEq", p.expr(arg, nil), map[types.Type]bool{})
		}
		return result, true
	}
	symbol := p.protocolSymbol(n.Fun)
	if symbol == nil {
		return nil, false
	}
	for _, d := range p.c.out.Derives {
		for _, kind := range []string{"Clone", "Debug", "PartialEq", "Serialize"} {
			if !d.Requests[kind] || d.Helpers[kind] != symbol {
				continue
			}
			sig, _ := p.c.out.Symbols[symbol].(*types.Signature)
			if sig == nil {
				return nil, false
			}
			for i, arg := range n.Args {
				if i >= sig.Params().Len() {
					p.expr(arg, nil)
					continue
				}
				formal := p.fromGo(sig.Params().At(i).Type())
				payload := kind != "Serialize" || i == 1
				if payload && kind != "PartialEq" {
					formal.ref = ReadRef
				}
				actual := p.expr(arg, formal)
				if !payload {
					p.require(arg, actual, formal)
					continue
				}
				value := actual
				if kind != "PartialEq" {
					p.require(arg, actual, formal)
					value = p.deref(actual)
					if actual != nil && (actual.ref == ReadRef || actual.shared) {
						value = p.readValue(value)
					}
				}
				p.protocolRead(arg, kind, value, map[types.Type]bool{})
				if kind == "Clone" {
					result = p.clonedValue(value, map[types.Type]bool{})
				}
			}
			return result, true
		}
	}
	return nil, false
}

func (p *permissionChecker) protocolSymbol(node ast.Expr) *resolve.Symbol {
	if paren, ok := node.(*ast.ParenExpr); ok {
		return p.protocolSymbol(paren.X)
	}
	return p.c.symbol(node)
}

// Generated protocols read fields, but a nested manual implementation retains
// its declared contract. In particular, manual equality can mutate shared data.
func (p *permissionChecker) protocolRead(node ast.Node, kind string, value *permission, seen map[types.Type]bool) {
	if value == nil || value.typ == nil || permissionTypeSeen(seen, value.typ) {
		return
	}
	seen[value.typ] = true
	defer delete(seen, value.typ)
	if named, ok := types.Unalias(value.typ).(*types.Named); ok {
		if d := p.c.out.Derives[named]; d != nil {
			if manual := d.Manual[kind]; manual != nil {
				old := p.c.file
				p.c.file = p.c.out.Resolve.Files[d.ManualFiles[kind]]
				params := p.paramTypes(manual.Params)
				p.c.file = old
				if len(params) != 0 {
					p.require(node, value, params[0])
				}
				return
			}
		}
	}
	if instance := p.c.out.EnumOf(value.typ); instance != nil {
		for _, variant := range instance.Enum.Variants {
			for i := range variant.Fields {
				p.protocolRead(node, kind, p.enumField(value, variant, i), seen)
			}
		}
		return
	}
	switch value.typ.Underlying().(type) {
	case *types.Struct:
		for _, field := range p.fields(value) {
			p.protocolRead(node, kind, field, seen)
		}
	case *types.Array, *types.Slice, *types.Map:
		element := p.element(value)
		if value.shared {
			element = p.readValue(element)
		}
		p.protocolRead(node, kind, element, seen)
	}
}

// Generated Clone allocates new enum variants, slices and maps, and clones
// their contents. Leaves it merely copies retain their existing capabilities.
func (p *permissionChecker) clonedValue(value *permission, seen map[types.Type]bool) *permission {
	if value == nil || value.typ == nil || permissionTypeSeen(seen, value.typ) {
		return value
	}
	seen[value.typ] = true
	defer delete(seen, value.typ)
	r := *value
	r.location = nil
	r.locations = nil
	if named, ok := types.Unalias(value.typ).(*types.Named); ok {
		if d := p.c.out.Derives[named]; d != nil && d.Manual["Clone"] != nil {
			return value
		}
	}
	if instance := p.c.out.EnumOf(value.typ); instance != nil {
		r.shared = false
		r.fields = map[string]*permission{}
		for _, variant := range instance.Enum.Variants {
			for i := range variant.Fields {
				r.fields[permissionFieldKey(variant, i)] = p.clonedValue(p.enumField(value, variant, i), seen)
			}
		}
		r.args = nil
		for _, arg := range value.args {
			if value.shared {
				arg = p.readValue(arg)
			}
			r.args = append(r.args, p.clonedValue(arg, seen))
		}
		return &r
	}
	switch value.typ.Underlying().(type) {
	case *types.Struct:
		r.fields = map[string]*permission{}
		for name, field := range p.fields(value) {
			r.fields[name] = p.clonedValue(field, seen)
		}
	case *types.Array, *types.Slice, *types.Map:
		r.shared = false
		element := p.element(value)
		if value.shared {
			element = p.readValue(element)
		}
		r.elem = p.clonedValue(element, seen)
	}
	return &r
}
