package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type bodyInfoSelection struct {
	nodes   []ast.Node
	symbols map[*resolve.Symbol]bool
}

func bodyTypeIdentity(typ types.Type) types.Type {
	if typ == nil {
		return nil
	}
	switch typ := types.Unalias(typ).(type) {
	case *types.Named:
		return typ.Origin()
	case *types.TypeParam:
		return typ
	}
	return nil
}

// A body owns only its expression tables. Declaration types also include
// referenced globals and inferred named types: lowering can need their aliases
// or defining source without an explicit type name in this body's syntax.
func (d *demandCollector) selectBodyInfo(source *Info, body *BodyInstance) *bodyInfoSelection {
	selection := &bodyInfoSelection{symbols: map[*resolve.Symbol]bool{}}
	nodes := map[ast.Node]bool{}
	seenTypes := map[types.Type]bool{}
	seenNamed := map[*types.Named]bool{}
	var addType func(types.Type)
	var addSymbol func(*resolve.Symbol)
	var addFunction func(*ast.FuncDecl)
	addBound := func(bound *TraitBound) {
		addType(bound.Target)
		if bound.Param != nil {
			addType(bound.Param)
		}
		for _, typ := range bound.Inputs {
			addType(typ)
		}
		for _, typ := range bound.Associated {
			addType(typ)
		}
		for _, typ := range bound.Projected {
			addType(typ)
		}
	}
	addFunction = func(decl *ast.FuncDecl) {
		if decl == nil || nodes[decl] {
			return
		}
		nodes[decl] = true
		selection.nodes = append(selection.nodes, decl)
		for _, bound := range source.GenericBounds[decl] {
			addBound(bound)
		}
	}
	addSymbol = func(symbol *resolve.Symbol) {
		if symbol == nil || selection.symbols[symbol] {
			return
		}
		selection.symbols[symbol] = true
		addSymbol(symbol.AliasOf)
		typ := source.Symbols[symbol]
		addType(typ)
		if signature, ok := typ.(*types.Signature); ok {
			if function := d.functions[signature]; function != nil {
				addFunction(function.decl)
			}
		}
	}
	addType = func(typ types.Type) {
		if typ == nil || seenTypes[typ] {
			return
		}
		seenTypes[typ] = true
		for _, symbol := range d.typeSymbols[bodyTypeIdentity(typ)] {
			addSymbol(symbol)
		}
		if array, ok := source.ArrayOf(typ); ok {
			addType(array.Elem)
		}
		if named, ok := types.Unalias(typ).(*types.Named); ok && !seenNamed[named.Origin()] {
			seenNamed[named.Origin()] = true
			addType(named.Origin().Underlying())
			if instance := source.EnumOf(named.Origin()); instance != nil {
				for _, variant := range instance.Enum.Variants {
					for _, field := range variant.Fields {
						addType(field.Type)
					}
				}
			}
		}
		if parameter, ok := typ.(*types.TypeParam); ok {
			addType(parameter.Constraint())
		}
		typeChildren(typ, func(child types.Type) bool {
			addType(child)
			return false
		})
	}
	addImpl := func(impl *Impl) {
		if impl == nil {
			return
		}
		addType(impl.Target)
		for _, typ := range impl.Inputs {
			addType(typ)
		}
		for _, typ := range impl.Associated {
			addType(typ)
		}
	}
	file := source.Resolve.Files[body.File]
	addFunction(body.Decl)
	ast.Walk(body.Node, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if !nodes[node] {
			nodes[node] = true
			selection.nodes = append(selection.nodes, node)
		}
		if file != nil {
			addSymbol(file.Definitions[node.ID()])
			addSymbol(file.References[node.ID()].Symbol)
		}
		if expression, ok := node.(ast.Expr); ok {
			addType(source.Types[expression])
			if member := source.Members[expression]; member != nil {
				addType(member.Signature)
				addImpl(member.Impl)
			}
			if view := source.TraitViews[expression]; view != nil {
				addType(view.Type)
				addType(view.SourceType)
				addImpl(view.Impl)
			}
		}
		if typ, ok := node.(ast.Type); ok {
			addType(source.TypeOverrides[typ])
			addType(source.typeNodes[typ])
		}
		if instance := source.FunctionInstances[node]; instance != nil {
			addFunction(instance.Decl)
			if function := d.functions[instance.Signature]; function != nil {
				addFunction(function.decl)
			}
			for _, typ := range instance.TypeArgs {
				addType(typ)
			}
		}
		return true
	})
	for parameter, typ := range body.Substitution {
		addType(parameter)
		addType(typ)
	}
	addImpl(body.Impl)
	return selection
}
