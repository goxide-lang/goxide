package check

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

type pendingDeclaration struct {
	item ast.Item
	file *resolve.File
	pkg  *types.Package
}

type pendingImpl struct {
	declaration *ast.ImplDecl
	file        *resolve.File
	pkg         *types.Package
}

// Scan before evaluating a declaration, so unsuccessful evidence never enters
// the permanent type, associated-output, or signature caches. Method bodies are
// checked later in their definition environment and are not dependencies here.
func (c *checker) declarationEvidence(node ast.Node, seen map[*resolve.Symbol]bool, missingOnly bool) bool {
	needs := false
	var where *ast.WhereClause
	var ownTrait *Trait
	var ownTarget ast.Type
	var ownInputs *ast.GenericArgs
	switch declaration := node.(type) {
	case *ast.ImplDecl:
		where, ownTarget = declaration.Where, declaration.Type
		if path, ok := declaration.Trait.(*ast.PathType); ok {
			ownTrait = c.traitPath(path.Path.Segments)
			ownInputs = path.Path.Segments[len(path.Path.Segments)-1].Args
		}
	case *ast.TraitDecl:
		where, ownTrait = declaration.Where, c.out.Traits[declaration]
	case *ast.FuncDecl:
		where = declaration.Where
	}
	localApplication := func(parts []*ast.PathSegment, split int, trait *Trait) bool {
		target := &ast.PathType{Path: &ast.Path{Segments: parts[:split]}}
		inputs := c.declarationArgumentsKey(parts[len(parts)-2].Args)
		self := split == 1 && parts[0].Name.Name == "Self"
		if ownTrait == trait && (self || ownTarget != nil && c.declarationTypeKey(target) == c.declarationTypeKey(ownTarget)) {
			if ownTarget != nil && inputs == c.declarationArgumentsKey(ownInputs) {
				return true
			}
			if ownTarget == nil {
				var parameters []string
				for _, parameter := range ownTrait.TypeParams {
					parameters = append(parameters, fmt.Sprintf("%p[]", c.symbolForTypeParameter(parameter)))
				}
				if inputs == strings.Join(parameters, ",") {
					return true
				}
			}
		}
		if where != nil {
			for _, predicate := range where.Predicates {
				if c.declarationTypeKey(target) != c.declarationTypeKey(predicate.Type) {
					continue
				}
				for _, bound := range predicate.Bounds {
					if path, ok := bound.(*ast.PathType); ok && c.traitPath(path.Path.Segments) == trait && inputs == c.declarationArgumentsKey(path.Path.Segments[len(path.Path.Segments)-1].Args) {
						return true
					}
				}
			}
		}
		return false
	}
	var visit func(ast.Node)
	visit = func(root ast.Node) {
		if root == nil || needs {
			return
		}
		ast.Walk(root, func(node ast.Node) bool {
			if needs {
				return false
			}
			if fn, ok := node.(*ast.FuncDecl); ok {
				if fn.Generics != nil {
					visit(fn.Generics)
				}
				if fn.Where != nil {
					visit(fn.Where)
				}
				for _, group := range [][]*ast.Param{fn.Recv, fn.Params, fn.Results} {
					for _, parameter := range group {
						visit(parameter.Type)
					}
				}
				return false
			}
			typ, ok := node.(ast.Type)
			if !ok {
				return true
			}
			parts := projectionTypePath(typ)
			if len(parts) == 0 {
				return true
			}
			for split := 1; split < len(parts)-1; split++ {
				if trait := c.traitPath(parts[split : len(parts)-1]); trait != nil {
					if localApplication(parts, split, trait) {
						continue
					}
					// A parameter projection is supplied by the declaration's
					// explicit bounds, without consulting the implementation set.
					if symbol := c.sourcePathSymbol(parts[:split]); symbol != nil && symbol.Kind == resolve.TypeParam {
						continue
					}
					if !missingOnly || c.coherence == nil || !c.coherence.certified[trait] {
						needs = true
						return false
					}
				}
			}
			symbol := c.sourcePathSymbol(parts)
			if declaration := c.declarations[symbol]; declaration != nil && declaration.alias && !seen[symbol] {
				seen[symbol] = true
				file := c.file
				c.file = declaration.file
				if declaration.generics != nil {
					visit(declaration.generics)
				}
				visit(declaration.node)
				c.file = file
			}
			return !needs
		})
	}
	visit(node)
	return needs
}

func (c *checker) symbolForTypeParameter(parameter *types.TypeParam) *resolve.Symbol {
	for symbol, typ := range c.out.Symbols {
		if typ == parameter {
			return symbol
		}
	}
	return nil
}

func (c *checker) declarationArgumentsKey(arguments *ast.GenericArgs) string {
	return c.declarationArgumentsKeySub(arguments, nil, map[*resolve.Symbol]bool{})
}

func (c *checker) declarationArgumentsKeySub(arguments *ast.GenericArgs, sub map[*resolve.Symbol]string, seen map[*resolve.Symbol]bool) string {
	var parts []string
	if arguments != nil {
		for _, argument := range arguments.Args {
			if argument.Name != nil {
				continue
			}
			if argument.Type != nil {
				parts = append(parts, c.declarationTypeKeySub(argument.Type, sub, seen))
			} else {
				parts = append(parts, printer.Source(argument.Value))
			}
		}
	}
	return strings.Join(parts, ",")
}

func (c *checker) declarationTypeKey(node ast.Type) string {
	return c.declarationTypeKeySub(node, nil, map[*resolve.Symbol]bool{})
}

func (c *checker) declarationTypeKeySub(node ast.Type, sub map[*resolve.Symbol]string, seen map[*resolve.Symbol]bool) string {
	switch typ := node.(type) {
	case *ast.PathType:
		if typ.Path == nil || len(typ.Path.Segments) == 0 {
			return ""
		}
		parts := typ.Path.Segments
		if symbol := c.sourcePathSymbol(parts); symbol != nil {
			if key, exists := sub[symbol]; exists {
				return key
			}
			arguments := parts[len(parts)-1].Args
			if declaration := c.declarations[symbol]; declaration != nil && declaration.alias && !seen[symbol] {
				var actual []string
				if arguments != nil {
					for _, argument := range arguments.Args {
						if argument.Type != nil {
							actual = append(actual, c.declarationTypeKeySub(argument.Type, sub, seen))
						}
					}
				}
				seen[symbol] = true
				defer delete(seen, symbol)
				file := c.file
				c.file = declaration.file
				bound := cloneMap(sub)
				if bound == nil {
					bound = map[*resolve.Symbol]string{}
				}
				index := 0
				if declaration.generics != nil {
					for _, group := range declaration.generics.Params {
						for _, name := range group.Names {
							if index < len(actual) {
								bound[c.symbol(name)] = actual[index]
							}
							index++
						}
					}
				}
				key := c.declarationTypeKeySub(declaration.node, bound, seen)
				c.file = file
				return key
			}
			return fmt.Sprintf("%p", symbol) + "[" + c.declarationArgumentsKeySub(arguments, sub, seen) + "]"
		}
	case *ast.RefType:
		return fmt.Sprintf("&%t(%s)", typ.Mut, c.declarationTypeKeySub(typ.Elem, sub, seen))
	case *ast.PointerType:
		return "*(" + c.declarationTypeKeySub(typ.Elem, sub, seen) + ")"
	case *ast.SliceType:
		return "[](" + c.declarationTypeKeySub(typ.Elem, sub, seen) + ")"
	case *ast.ArrayType:
		return "[" + printer.Source(typ.Len) + "](" + c.declarationTypeKeySub(typ.Elem, sub, seen) + ")"
	case *ast.MapType:
		return "map[" + c.declarationTypeKeySub(typ.Key, sub, seen) + "]" + c.declarationTypeKeySub(typ.Value, sub, seen)
	case *ast.ChanType:
		return fmt.Sprintf("chan%d(%s)", typ.Dir, c.declarationTypeKeySub(typ.Elem, sub, seen))
	case *ast.ParenType:
		return c.declarationTypeKeySub(typ.Type, sub, seen)
	}
	return printer.Source(node)
}

func (c *checker) signatureNeedsEvidence(node ast.Node, seen map[*resolve.Symbol]bool) bool {
	return c.declarationEvidence(node, seen, false)
}

func (c *checker) declarationEvidenceReady(node ast.Node) bool {
	return !c.declarationEvidence(node, map[*resolve.Symbol]bool{}, true)
}

func (c *checker) completePendingTraits() bool {
	file, pkg := c.file, c.pkg
	defer func() { c.file, c.pkg = file, pkg }()
	if c.traitRequirementsReady == nil {
		c.traitRequirementsReady = map[*Trait]bool{}
	}
	progress := false
	for _, trait := range c.pendingTraits {
		c.file, c.pkg = c.out.Resolve.Files[trait.File], trait.Type.Obj().Pkg()
		if c.traitRequirementsReady[trait] || trait.Decl.Generics != nil && !c.declarationEvidenceReady(trait.Decl.Generics) || trait.Decl.Where != nil && !c.declarationEvidenceReady(trait.Decl.Where) {
			continue
		}
		c.currentTrait, c.currentSelf = trait, trait.Self
		trait.Requirements = c.sourceRequirements(trait.Decl.Generics, trait.Decl.Where, trait.TypeParams, map[string]bool{}, trait.Decl)
		c.currentTrait, c.currentSelf = nil, nil
		c.traitRequirementsReady[trait], progress = true, true
	}
	pending := c.pendingTraits
	c.pendingTraits = nil
	for _, trait := range pending {
		c.file, c.pkg = c.out.Resolve.Files[trait.File], trait.Type.Obj().Pkg()
		if !c.traitRequirementsReady[trait] || !c.declarationEvidenceReady(trait.Decl) {
			c.pendingTraits = append(c.pendingTraits, trait)
			continue
		}
		c.currentTrait, c.currentSelf = trait, trait.Self
		trait.Requirements = c.inheritedBounds(trait.Requirements, trait.Decl)
		bounds := c.activeBounds
		c.activeBounds = trait.Requirements
		c.prepareTraitMethods(trait)
		c.activeBounds = bounds
		c.currentTrait, c.currentSelf = nil, nil
		progress = true
	}
	return progress
}

func (c *checker) pendingTrait(trait *Trait) bool {
	for _, pending := range c.pendingTraits {
		if pending == trait {
			return true
		}
	}
	file := c.file
	defer func() { c.file = file }()
	for _, pending := range c.pendingImpls {
		c.file = pending.file
		if path, ok := pending.declaration.Trait.(*ast.PathType); ok && c.traitPath(path.Path.Segments) == trait {
			return true
		}
	}
	return false
}

func (c *checker) completePendingImpls() bool {
	file, pkg := c.file, c.pkg
	defer func() { c.file, c.pkg = file, pkg }()
	pending := c.pendingImpls
	c.pendingImpls = nil
	var ready []*Impl
	progress := false
	for _, entry := range pending {
		c.file, c.pkg = entry.file, entry.pkg
		available := c.declarationEvidenceReady(entry.declaration)
		if path, ok := entry.declaration.Trait.(*ast.PathType); ok {
			trait := c.traitPath(path.Path.Segments)
			for _, incomplete := range c.pendingTraits {
				available = available && incomplete != trait
			}
		}
		if !available {
			c.pendingImpls = append(c.pendingImpls, entry)
			continue
		}
		if impl := c.prepareImpl(entry.declaration); impl != nil {
			ready = append(ready, impl)
		}
		progress = true
	}
	for _, impl := range ready {
		c.prepareImplBody(impl)
	}
	return progress
}

// Coherence calls this after independently certifying traits. Its monotone
// fixed point permits dependency chains but never bootstraps a projection from
// the very coherence certificate which still depends on that projection.
func (c *checker) completeCertifiedSignatures() bool {
	file, pkg, phase := c.file, c.pkg, c.signaturePhase
	defer func() { c.file, c.pkg, c.signaturePhase = file, pkg, phase }()
	progress := c.completePendingTraits()
	if c.completePendingImpls() {
		progress = true
	}
	pending := c.pendingDeclarations
	c.pendingDeclarations = nil
	c.signaturePhase = 0
	for _, entry := range pending {
		c.file, c.pkg = entry.file, entry.pkg
		if !c.declarationEvidenceReady(entry.item) {
			c.pendingDeclarations = append(c.pendingDeclarations, entry)
			continue
		}
		c.signatures([]ast.Item{entry.item})
		progress = true
	}
	return progress
}

func (c *checker) pendingNativeMethod(typ types.Type, name string) bool {
	file := c.file
	defer func() { c.file = file }()
	seen := map[types.Type]bool{}
	var visit func(types.Type) bool
	visit = func(typ types.Type) bool {
		if typ == nil || seen[typ] {
			return false
		}
		seen[typ] = true
		if c.pendingUnderlying(typ) {
			return true
		}
		if pointer, ok := types.Unalias(typ).(*types.Pointer); ok {
			return visit(pointer.Elem())
		}
		if named, ok := types.Unalias(typ).(*types.Named); ok {
			for _, entry := range c.pendingDeclarations {
				fn, ok := entry.item.(*ast.FuncDecl)
				if !ok || len(fn.Recv) == 0 || fn.Name.Name != name {
					continue
				}
				receiver := fn.Recv[0].Type
				if pointer, ok := receiver.(*ast.PointerType); ok {
					receiver = pointer.Elem
				}
				path, ok := receiver.(*ast.PathType)
				if !ok {
					continue
				}
				c.file = entry.file
				if base := c.named[c.sourcePathSymbol(path.Path.Segments)]; base != nil && base.Origin() == named.Origin() {
					return true
				}
			}
		}
		if structure, ok := typ.Underlying().(*types.Struct); ok {
			for i := 0; i < structure.NumFields(); i++ {
				field := structure.Field(i)
				if field.Embedded() && visit(field.Type()) {
					return true
				}
			}
		}
		return false
	}
	return visit(typ)
}

// Nominal identity exists before fields and embedded methods are complete.
// Consumers which need the underlying representation must preserve Unknown.
func (c *checker) pendingUnderlying(typ types.Type) bool {
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok {
		return false
	}
	for symbol, declaration := range c.declarations {
		if !declaration.defined && c.named[symbol] != nil && c.named[symbol].Origin() == named.Origin() {
			return true
		}
	}
	return false
}

func (c *checker) diagnosePendingDeclarations() bool {
	file, pkg := c.file, c.pkg
	defer func() { c.file, c.pkg = file, pkg }()
	const message = "cyclic declaration evidence dependency: associated projection requires independently certified trait implementations"
	for _, entry := range c.pendingImpls {
		c.file, c.pkg = entry.file, entry.pkg
		c.diag(entry.declaration, TraitInvalid, message)
	}
	for _, entry := range c.pendingDeclarations {
		c.file, c.pkg = entry.file, entry.pkg
		c.diag(entry.item, TraitInvalid, message)
	}
	for _, trait := range c.pendingTraits {
		c.file, c.pkg = c.out.Resolve.Files[trait.File], trait.Type.Obj().Pkg()
		c.diag(trait.Decl, TraitInvalid, message)
	}
	return len(c.pendingImpls)+len(c.pendingDeclarations)+len(c.pendingTraits) == 0
}

func (c *checker) completeGenericConstraints(generic *ast.GenericParams) {
	if generic == nil {
		return
	}
	for _, group := range generic.Params {
		if group.Const || group.Constraint == nil {
			continue
		}
		for _, name := range group.Names {
			if parameter, ok := c.out.Symbols[c.symbol(name)].(*types.TypeParam); ok {
				parameter.SetConstraint(c.typ(group.Constraint))
			}
		}
	}
}
