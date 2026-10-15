package check

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

type Derive struct {
	File        string
	Node        ast.Node
	Type        *types.Named
	Requests    map[string]bool
	Manual      map[string]*ast.FuncDecl
	ManualFiles map[string]string
	Helpers     map[string]*resolve.Symbol
}

func (c *checker) collectDerives() {
	nodes := map[*resolve.Symbol]ast.Node{}
	for _, p := range c.out.Resolve.Packages {
		for _, f := range p.Files {
			for _, item := range f.AST.Items {
				switch n := item.(type) {
				case *ast.TypeDecl:
					if n.Grouped {
						for _, s := range n.Specs {
							nodes[f.Definitions[s.Name.ID()]] = s
						}
					} else {
						nodes[f.Definitions[n.Name.ID()]] = n
					}
				case *ast.EnumDecl:
					nodes[f.Definitions[n.Name.ID()]] = n
				}
			}
		}
	}
	for _, entry := range c.out.Resolve.Derives {
		c.file = c.out.Resolve.Files[entry.File]
		t, ok := c.symbolType(entry.Target).(*types.Named)
		if !ok || nodes[entry.Target] == nil {
			c.diag(entry.Node, DeriveInvalid, "protocol implementation requires a local defined type, not an alias")
			continue
		}
		c.pkg = t.Obj().Pkg()
		if entry.Owner != "" {
			definition, _ := derives.Lookup(entry.Owner, entry.Name)
			if err := derives.Validate(definition, c.importer); err != nil {
				c.diag(entry.Attr, DeriveInvalid, err.Error())
				continue
			}
		}
		if t.TypeParams().Len() != 0 {
			c.diag(entry.Node, DeriveInvalid, "generic protocol derivation is not defined")
			continue
		}
		d := c.out.Derives[t]
		if d == nil {
			d = &Derive{File: entry.Target.Decl.File, Node: nodes[entry.Target], Type: t, Requests: map[string]bool{}, Manual: map[string]*ast.FuncDecl{}, ManualFiles: map[string]string{}, Helpers: map[string]*resolve.Symbol{}}
			c.out.Derives[t] = d
		}
		_, manual := d.Manual[entry.Name]
		if d.Requests[entry.Name] || manual {
			c.diag(entry.Node, DeriveInvalid, fmt.Sprintf("duplicate %s implementation for %s", entry.Name, entry.Target.Name.Name))
			continue
		}
		if entry.Impl == nil {
			d.Requests[entry.Name] = true
		} else {
			function, valid := c.manualDerive(entry, t)
			if !valid {
				continue
			}
			d.Manual[entry.Name] = function
			d.ManualFiles[entry.Name] = entry.File
		}
		if entry.Helper != nil {
			d.Helpers[entry.Name] = entry.Helper
			if sig := c.protocolSignature(entry.Name, t); sig != nil {
				c.out.Symbols[entry.Helper] = sig
			}
		}
		if entry.Name == "Error" {
			c.errorProtocol(d)
			if entry.Helper != nil {
				c.conversions = append(c.conversions, entry.Helper)
			}
		}
	}
	for _, d := range c.out.Derives {
		c.file = c.out.Resolve.Files[d.File]
		_, manualEq := d.Manual["Eq"]
		_, manualPartial := d.Manual["PartialEq"]
		if (d.Requests["Eq"] || manualEq) && !d.Requests["PartialEq"] && !manualPartial {
			c.diag(d.Node, DeriveInvalid, "Eq requires an explicit PartialEq implementation")
		}
		if d.Requests["Eq"] && manualPartial {
			c.diag(d.Node, DeriveInvalid, "derive Eq cannot prove manual PartialEq; implement Eq explicitly")
		}
	}
	c.deriveAttributes()
}

// Default retains its conservative static recursion rule. Function references
// count as dependencies even when passed through a local function value.
func (c *checker) checkDefaultCycles() {
	graph := map[*resolve.Symbol][]*resolve.Symbol{}
	var roots []*Derive
	for _, file := range c.out.Resolve.Files {
		c.file = file
		ast.Walk(file.AST, func(node ast.Node) bool {
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			owner := c.symbol(fn.Name)
			ast.Walk(fn.Body, func(node ast.Node) bool {
				if symbol := c.symbol(node); symbol != nil && symbol.Kind == resolve.Function {
					graph[owner] = append(graph[owner], symbol)
				}
				return true
			})
			return false
		})
	}
	for _, derive := range c.out.Derives {
		helper := derive.Helpers["Default"]
		if helper == nil {
			continue
		}
		roots = append(roots, derive)
		if fn := derive.Manual["Default"]; fn != nil {
			c.file = c.out.Resolve.Files[derive.ManualFiles["Default"]]
			graph[helper] = append(graph[helper], c.symbol(fn.Name))
			continue
		}
		var add func(types.Type)
		add = func(typ types.Type) {
			typ = types.Unalias(typ)
			if named, ok := typ.(*types.Named); ok {
				if child := c.out.Derives[named]; child != nil && child.Helpers["Default"] != nil {
					graph[helper] = append(graph[helper], child.Helpers["Default"])
				}
				return
			}
			if record, ok := typ.(*types.Struct); ok {
				for index := 0; index < record.NumFields(); index++ {
					add(record.Field(index).Type())
				}
			}
		}
		if instance := c.out.EnumOf(derive.Type); instance != nil {
			for _, variant := range instance.Enum.Variants {
				for _, attr := range variant.Decl.Attrs {
					if resolve.AttributeName(attr) == "default" {
						for _, field := range instance.Fields(variant) {
							add(field.Type)
						}
					}
				}
			}
		} else {
			add(derive.Type.Underlying())
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].File != roots[j].File {
			return roots[i].File < roots[j].File
		}
		return roots[i].Node.Span().Start < roots[j].Node.Span().Start
	})
	for _, root := range roots {
		active, done := map[*resolve.Symbol]bool{}, map[*resolve.Symbol]bool{}
		var stack []string
		var visit func(*resolve.Symbol) bool
		visit = func(symbol *resolve.Symbol) bool {
			if symbol == nil || done[symbol] {
				return false
			}
			if active[symbol] {
				stack = append(stack, symbol.GoName)
				return true
			}
			active[symbol] = true
			stack = append(stack, symbol.GoName)
			for _, next := range graph[symbol] {
				if visit(next) {
					return true
				}
			}
			stack = stack[:len(stack)-1]
			delete(active, symbol)
			done[symbol] = true
			return false
		}
		if visit(root.Helpers["Default"]) {
			c.file = c.out.Resolve.Files[root.File]
			c.diag(root.Node, DeriveInvalid, "recursive Default protocol dependency: "+strings.Join(stack, " -> ")+" (static recursion is unsupported)")
		}
	}
}

func (c *checker) protocolSignature(kind string, t types.Type) *types.Signature {
	var params, results []types.Type
	switch kind {
	case "Default":
		results = []types.Type{t}
	case "Clone":
		params, results = []types.Type{types.NewPointer(t)}, []types.Type{t}
	case "Debug":
		params, results = []types.Type{types.NewPointer(t)}, []types.Type{types.Typ[types.String]}
	case "PartialEq":
		params, results = []types.Type{t, t}, []types.Type{types.Typ[types.Bool]}
	case "Error":
		params, results = []types.Type{types.Universe.Lookup("error").Type()}, []types.Type{t}
	case "Serialize", "Deserialize":
		pkg, err := c.importer.Import(derives.SerdePackage)
		if err != nil {
			return nil
		}
		if kind == "Serialize" {
			params = []types.Type{pkg.Scope().Lookup("Serializer").Type(), types.NewPointer(t)}
			results = []types.Type{types.Universe.Lookup("error").Type()}
		} else {
			params = []types.Type{pkg.Scope().Lookup("Deserializer").Type()}
			results = []types.Type{t, types.Universe.Lookup("error").Type()}
		}
	default:
		return nil
	}
	tuple := func(ts []types.Type) *types.Tuple {
		var vars []*types.Var
		for _, t := range ts {
			vars = append(vars, types.NewVar(0, nil, "", t))
		}
		return types.NewTuple(vars...)
	}
	return types.NewSignatureType(nil, nil, nil, tuple(params), tuple(results), false)
}

func (c *checker) manualDerive(entry resolve.Derive, t *types.Named) (*ast.FuncDecl, bool) {
	n := entry.Impl
	fail := func(message string) (*ast.FuncDecl, bool) {
		c.diag(n, DeriveInvalid, "manual "+entry.Name+": "+message)
		return nil, false
	}
	if n.Generics != nil || n.Where != nil {
		return fail("requires a concrete non-generic impl")
	}
	if entry.Name == "Eq" {
		if len(n.Items) != 0 {
			return fail("Eq impl must be empty")
		}
		return nil, true
	}
	if len(n.Items) != 1 {
		return fail("requires exactly one protocol function")
	}
	f, ok := n.Items[0].(*ast.FuncDecl)
	if !ok || f.Name == nil || f.Body == nil || f.Path != nil || f.Generics != nil || f.Where != nil || len(f.Recv) != 0 || f.Mods.Unsafe || f.Mods.Async || f.Mods.Const {
		return fail("requires an ordinary function body with the protocol signature")
	}
	if entry.Name == "Default" {
		if _, ok := t.Underlying().(*types.Struct); !ok {
			return fail("requires a local struct")
		}
		if f.Name.Name != "default" || len(f.Params) != 0 {
			return fail("expected func default() Self")
		}
	} else {
		params := c.tuple(f.Params)
		if f.Name.Name != "equals" || params.Len() != 2 || params.At(0).Name() != "self" || !types.Identical(params.At(0).Type(), t) || !types.Identical(params.At(1).Type(), t) {
			return fail("expected func equals(self Self, other Self) bool")
		}
		for _, p := range f.Params {
			if p.Variadic {
				return fail("equals parameters must be values, not variadic")
			}
		}
	}
	results := c.tuple(f.Results)
	want := types.Type(t)
	if entry.Name == "PartialEq" {
		want = types.Typ[types.Bool]
	}
	if results.Len() != 1 || !types.Identical(results.At(0).Type(), want) {
		return fail("wrong result type")
	}
	return f, true
}

func (c *checker) errorProtocol(d *Derive) {
	errType := types.Universe.Lookup("error").Type()
	method := func(name string, result types.Type) *types.Func {
		sig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", result)), false)
		return types.NewFunc(0, d.Type.Obj().Pkg(), name, sig)
	}
	if iface, ok := d.Type.Underlying().(*types.Interface); ok && c.out.EnumOf(d.Type) != nil {
		methods := make([]*types.Func, 0, iface.NumExplicitMethods()+2)
		for i := 0; i < iface.NumExplicitMethods(); i++ {
			methods = append(methods, iface.ExplicitMethod(i))
		}
		methods = append(methods, method("Error", types.Typ[types.String]), method("Unwrap", errType))
		d.Type.SetUnderlying(types.NewInterfaceType(methods, nil).Complete())
	} else {
		methods := []*types.Func{method("Error", types.Typ[types.String])}
		shape := deriveStructType(d.Node)
		if shape != nil {
			for _, field := range shape.Fields {
				for _, attr := range field.Attrs {
					if name := resolve.AttributeName(attr); name == "source" || name == "from" {
						methods = append(methods, method("Unwrap", errType))
						break
					}
				}
				if len(methods) == 2 {
					break
				}
			}
		}
		for _, m := range methods {
			sig := m.Type().(*types.Signature)
			if previous, _, _ := types.LookupFieldOrMethod(d.Type, true, c.pkg, m.Name()); previous != nil {
				c.diag(d.Node, DeriveInvalid, "derived "+m.Name()+" conflicts with an existing member")
				continue
			}
			d.Type.AddMethod(types.NewFunc(0, d.Type.Obj().Pkg(), m.Name(), types.NewSignatureType(types.NewVar(0, d.Type.Obj().Pkg(), "", d.Type), nil, nil, sig.Params(), sig.Results(), false)))
		}
	}
}

func (c *checker) deriveAttributes() {
	allowed := map[*ast.Attr]bool{}
	for _, entry := range c.out.Resolve.Derives {
		if entry.Attr != nil {
			allowed[entry.Attr] = true
		}
	}
	for _, d := range c.out.Derives {
		c.file = c.out.Resolve.Files[d.File]
		ast.Walk(d.Node, func(n ast.Node) bool {
			var attrs []*ast.Attr
			switch n := n.(type) {
			case *ast.TypeDecl:
				attrs = n.Attrs
			case *ast.TypeSpec:
				attrs = n.Attrs
			case *ast.EnumDecl:
				attrs = n.Attrs
			case *ast.Variant:
				attrs = n.Attrs
				for _, a := range attrs {
					if resolve.AttributeName(a) == "default" && d.Requests["Default"] && (a.Args == nil || len(a.Args.Tokens) == 0) {
						allowed[a] = true
					}
				}
			case *ast.Field:
				attrs = n.Attrs
			}
			for _, a := range attrs {
				switch resolve.AttributeName(a) {
				case "serde":
					allowed[a] = (d.Requests["Serialize"] || d.Requests["Deserialize"]) && (n == d.Node || errorAttributePosition(d, n, "source"))
				case "error", "source", "from":
					allowed[a] = d.Requests["Error"] && errorAttributePosition(d, n, resolve.AttributeName(a))
				}
			}
			return true
		})
	}
	for _, p := range c.out.Resolve.Packages {
		for _, f := range p.Files {
			c.file = f
			ast.Walk(f.AST, func(n ast.Node) bool {
				if a, ok := n.(*ast.Attr); ok && !allowed[a] {
					c.diag(a, DeriveInvalid, "unsupported attribute or invalid attribute position: "+resolve.AttributeName(a))
					return false
				}
				return true
			})
		}
	}
}

func errorAttributePosition(d *Derive, node ast.Node, name string) bool {
	if enum, ok := d.Node.(*ast.EnumDecl); ok {
		for _, variant := range enum.Variants {
			if node == variant {
				return true
			}
		}
		return false
	}
	shape := deriveStructType(d.Node)
	if shape == nil {
		return false
	}
	if name == "error" {
		return node == d.Node
	}
	for _, field := range shape.Fields {
		if node == field {
			return true
		}
	}
	return false
}

func deriveStructType(node ast.Node) *ast.StructType {
	var typ ast.Type
	switch node := node.(type) {
	case *ast.TypeDecl:
		typ = node.Type
	case *ast.TypeSpec:
		typ = node.Type
	}
	shape, _ := typ.(*ast.StructType)
	return shape
}
