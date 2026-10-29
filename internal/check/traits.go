package check

import (
	"crypto/sha256"
	"fmt"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

const TraitInvalid = "trait-invalid"

// Trait and Impl describe source identities; Go method sets do not establish an impl.
type Trait struct {
	Decl         *ast.TraitDecl
	File         string
	Symbol       *resolve.Symbol
	Type         *types.Named
	Self         *types.TypeParam
	TypeParams   []*types.TypeParam
	Associated   []*AssociatedType
	Methods      []*Member
	Runtime      bool
	Marker       string
	Requirements []*TraitBound
}

type AssociatedType struct {
	Name  string
	Param *types.TypeParam
	Decl  *ast.TypeDecl
}

type Impl struct {
	Case                   *ImplCase
	CaseEvidence           []Evidence
	MethodCases            map[*ast.FuncDecl]*BodyCase
	MethodCaseEvidence     map[*ast.FuncDecl][]Evidence
	Definition             *Impl
	ConstParams            []*ConstParam
	ConstArgs              []ConstValue
	ConstSubstitution      map[*ConstParam]ConstValue
	info                   *Info
	Decl                   *ast.ImplDecl
	DeclID                 string
	File                   string
	Target                 types.Type
	Base                   *types.Named
	Trait                  *Trait
	Inputs                 []types.Type
	InputNodes             []ast.Type
	Associated             map[string]types.Type
	AssociatedNodes        map[string]ast.Type
	Methods                []*Member
	Capture                RefMode
	Owner                  *types.Package
	TypeParams             []*types.TypeParam
	TypeArgs               []types.Type
	Template               *Impl
	Requirements           []*TraitBound
	RequirementEvidence    []Evidence
	Substitution           map[*types.TypeParam]types.Type
	associatedState        map[string]uint8
	associatedPermissions  map[string]*permission
	targetPermission       *permission
	inputPermissions       []*permission
	permissionSubstitution map[*types.TypeParam]*permission
}

type Member struct {
	Decl         *ast.FuncDecl
	File         string
	Symbol       *resolve.Symbol
	Name, GoName string
	Signature    *types.Signature // The receiver is separate from ordinary parameters.
	Receiver     RefMode
	Static       bool
	Impl         *Impl
	Trait        *Trait
	Native       *types.Func
}

type MemberSelection struct {
	Member                     *Member
	Impl                       *Impl
	Bound                      *TraitBound
	Receiver                   ast.Expr
	Address, Deref, MethodExpr bool
	Signature                  *types.Signature
	Evidence                   Evidence
}

type TraitView struct {
	Impl           *Impl
	Type           types.Type
	SourceType     types.Type
	Receiver       ast.Expr
	Address, Deref bool
	Capture        RefMode
}

type TraitBound struct {
	info                   *Info
	constSubstitution      map[*ConstParam]ConstValue
	File                   string
	Param                  *types.TypeParam
	Target                 types.Type
	TargetNode             ast.Type
	Trait                  *Trait
	Inputs                 []types.Type
	InputNodes             []ast.Type
	Associated             map[string]types.Type
	Projected              []*types.TypeParam
	AssociatedNodes        map[string]ast.Type
	substitution           map[*types.TypeParam]types.Type
	associatedPermissions  map[string]*permission
	targetPermission       *permission
	inputPermissions       []*permission
	permissionSubstitution map[*types.TypeParam]*permission
}

// Evidence records the selected source implementation or an in-scope premise.
// It never derives source protocol membership from a Go method set.
type Evidence struct {
	Bound   *TraitBound
	Impl    *Impl
	Cases   []EvidenceCase
	outputs map[string]types.Type
}

// GoPredicate is a branch-local Go constraint on a type that remains rigid.
type GoPredicate struct {
	Target     types.Type
	Constraint *types.Interface
}

// EvidenceCase is one leaf of a proved exhaustive, disjoint compile-time choice.
// Only the application solver constructs Cases after proving coverage.
type EvidenceCase struct {
	Requirements           []*TraitBound
	GoConstraints          []GoPredicate
	Evidence               Evidence
	Substitution           map[*types.TypeParam]types.Type
	ConstSubstitution      map[*ConstParam]ConstValue
	permissionSubstitution map[*types.TypeParam]*permission
}

type FunctionInstance struct {
	Case              *BodyCase
	CaseEvidence      []Evidence
	ConstArgs         []ConstValue
	ConstSubstitution map[*ConstParam]ConstValue
	// Definition points at the length-specialized emitted declaration.
	Definition             *FunctionInstance
	Signature              *types.Signature
	TypeArgs               []types.Type
	Evidence               []Evidence
	Decl                   *ast.FuncDecl
	File                   string
	Substitution           map[*types.TypeParam]types.Type
	Body                   *BodyInstance
	permissionSubstitution map[*types.TypeParam]*permission
}

func (c *checker) collectTrait(n *ast.TraitDecl) {
	s := c.symbol(n.Name)
	if s == nil {
		return
	}
	t := types.NewNamed(types.NewTypeName(0, c.pkg, s.GoName, nil), anyType, nil)
	packagePath := c.out.CanonicalPackagePath(c.pkg)
	digest := sha256.Sum256([]byte(packagePath + "::" + s.Name.Name))
	trait := &Trait{Decl: n, File: c.file.Name, Symbol: s, Type: t, Self: types.NewTypeParam(types.NewTypeName(0, c.pkg, "Self", nil), anyType), Marker: fmt.Sprintf("GoxideTrait%s_%x", strings.TrimLeft(s.GoName, "_"), digest[:4])}
	c.out.Traits[n] = trait
	c.traits[s] = trait
	c.named[s], c.out.Symbols[s] = t, t
}

func (c *checker) traitForSymbol(s *resolve.Symbol) *Trait {
	for s != nil && s.AliasOf != nil {
		s = s.AliasOf
	}
	if s != nil && len(s.Target) > 0 {
		if local := c.localSymbol(s.Target); local != nil {
			s = local
		}
	}
	return c.traits[s]
}

func (c *checker) traitOf(t types.Type) *Trait {
	if !valid(t) {
		return nil
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}
	for _, trait := range c.out.Traits {
		if trait.Type == n.Origin() {
			return trait
		}
	}
	return nil
}

func (c *checker) traitPath(parts []*ast.PathSegment) *Trait {
	return c.traitForSymbol(c.sourcePathSymbol(parts))
}

func (c *checker) sourceName(id *ast.Ident) *resolve.Symbol {
	scope := c.file.Scope
	if contextual := c.out.Resolve.Contexts[id.Ctx]; contextual != nil {
		scope = contextual
	}
	key := resolve.Key{Name: id.Name, Ctx: id.Ctx}
	var globs []*resolve.UseBinding
	for ; scope != nil; scope = scope.Parent {
		if symbol := scope.Bindings[key]; symbol != nil {
			return symbol
		}
		if aliases := scope.Aliases[key]; len(aliases) == 1 {
			return aliases[0]
		}
		globs = append(globs, scope.Globs...)
		if scope.Kind == resolve.ContextScope {
			key.Ctx = 0
			for ctx, ancestor := range c.out.Resolve.Contexts {
				if ancestor == scope.Parent {
					key.Ctx = ctx
					break
				}
			}
		}
	}
	var found *resolve.Symbol
	for _, use := range globs {
		symbol := c.localSymbol(append(append([]string(nil), use.Target...), id.Name))
		if symbol != nil {
			if found != nil && found != symbol {
				return nil
			}
			found = symbol
		}
	}
	return found
}

func (c *checker) sourcePathSymbol(parts []*ast.PathSegment) *resolve.Symbol {
	if len(parts) == 0 {
		return nil
	}
	lastName := parts[len(parts)-1].Name
	// Reexports can change the visible name and package while retaining one
	// source declaration. Prefer the resolver's canonical route for this path.
	last := c.file.References[lastName.ID()]
	if c.namespaceSymbol(last.Symbol) {
		return c.visibleSymbol(lastName, last.Symbol)
	}
	if !last.Deferred && last.Symbol != nil && len(last.Symbol.Target) != 0 {
		if symbol := c.localSymbol(last.Symbol.Target); symbol != nil {
			return c.visibleSymbol(lastName, symbol)
		}
	}
	id := parts[0].Name
	symbol := c.symbol(id)
	// An embedded field identifier also names its type. Its declaration is the
	// field, while the reference binds the type used by this path.
	if definition := c.file.Definitions[id.ID()]; definition != nil && definition.Kind == resolve.Field {
		if reference := c.file.References[id.ID()].Symbol; reference != nil {
			symbol = reference
		}
	}
	// Deferred associated projections record the type parameter for every
	// segment. Resolve the selected trait prefix from its own lexical name.
	if symbol == nil || symbol.Name.Name != id.Name {
		symbol = c.sourceName(id)
	}
	for symbol != nil && symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	if symbol == nil {
		return nil
	}
	// A project dependency alias has an importer-only root in Symbol.Target.
	// Deferred projections resolve the trait suffix here, so follow the
	// importer's actual namespace before walking that suffix.
	if symbol.Kind == resolve.Import {
		if file := c.out.Resolve.Files[symbol.Decl.File]; file != nil {
			for _, use := range file.Uses {
				if use.Symbol == symbol {
					target := append([]string(nil), use.Target...)
					for _, part := range parts[1:] {
						target = append(target, part.Name.Name)
					}
					if resolved := c.localSymbol(target); resolved != nil {
						return c.visibleSymbol(lastName, resolved)
					}
					if use.Resolved != nil {
						symbol = use.Resolved
					}
					break
				}
			}
		}
	}
	if len(symbol.Target) > 0 {
		target := append([]string(nil), symbol.Target...)
		for _, part := range parts[1:] {
			target = append(target, part.Name.Name)
		}
		return c.visibleSymbol(lastName, c.localSymbol(target))
	}
	for _, part := range parts[1:] {
		if symbol.Members == nil {
			return nil
		}
		key := resolve.Key{Name: part.Name.Name, Ctx: part.Name.Ctx}
		symbol = symbol.Members.Bindings[key]
		if symbol == nil {
			return nil
		}
	}
	return c.visibleSymbol(lastName, symbol)
}

func (c *checker) eachTrait(f func(*Trait)) {
	for _, pkg := range c.out.Resolve.Packages {
		c.pkg = c.packages[pkg]
		for _, file := range pkg.Files {
			c.file = file
			for _, item := range file.AST.Items {
				if n, ok := item.(*ast.TraitDecl); ok {
					if t := c.out.Traits[n]; t != nil {
						f(t)
					}
				}
			}
		}
	}
}

func (c *checker) prepareTraits() {
	c.eachTrait(func(t *Trait) {
		t.TypeParams = c.genericParameters(t.Decl.Generics)
		if len(c.constParameters(t.Decl.Generics)) != 0 {
			c.diag(t.Decl, TraitInvalid, "trait inputs must be type parameters")
		}
		for _, item := range t.Decl.Items {
			if n, ok := item.(*ast.TypeDecl); ok {
				if n.Type != nil || n.Generics != nil || n.Grouped {
					c.diag(n, TraitInvalid, "associated types require a plain declaration without a default")
				}
				p := types.NewTypeParam(types.NewTypeName(0, c.pkg, n.Name.Name, nil), anyType)
				t.Associated = append(t.Associated, &AssociatedType{Name: n.Name.Name, Param: p, Decl: n})
				c.out.Symbols[c.symbol(n.Name)] = p
			}
		}
		params := append([]*types.TypeParam(nil), t.TypeParams...)
		for _, a := range t.Associated {
			params = append(params, a.Param)
		}
		t.Type.SetTypeParams(params)
	})
	c.eachTrait(func(t *Trait) { c.pendingTraits = append(c.pendingTraits, t) })
	c.completePendingTraits()
}

func (c *checker) prepareTraitMethods(t *Trait) {
	var methods []*types.Func
	t.Runtime = true
	instances := 0
	for _, item := range t.Decl.Items {
		if fn, ok := item.(*ast.FuncDecl); ok {
			if fn.Body != nil {
				c.diag(fn, TraitInvalid, "trait defaults are not supported; provide every method in the impl")
			}
			m := c.makeMember(fn, nil, t)
			t.Methods = append(t.Methods, m)
			if m.Static {
				continue
			}
			instances++
			if containsType(m.Signature, t.Self) {
				t.Runtime = false
			}
			methods = append(methods, types.NewFunc(0, c.pkg, m.Name, m.Signature))
		}
	}
	if instances == 0 && len(t.Methods) != 0 {
		t.Runtime = false
	}
	var markerArgs []*types.Var
	for i := 0; i < t.Type.TypeParams().Len(); i++ {
		markerArgs = append(markerArgs, types.NewVar(0, c.pkg, "", t.Type.TypeParams().At(i)))
	}
	methods = append(methods, types.NewFunc(0, c.pkg, t.Marker, types.NewSignatureType(nil, nil, nil, types.NewTuple(markerArgs...), nil, false)))
	t.Type.SetUnderlying(types.NewInterfaceType(methods, nil).Complete())
}

func receiverParams(fn *ast.FuncDecl) (RefMode, bool, []*ast.Param) {
	if len(fn.Params) == 0 || len(fn.Params[0].Names) != 1 || fn.Params[0].Names[0].Name != "self" {
		return ValueRef, true, fn.Params
	}
	mode := ValueRef
	if ref, ok := fn.Params[0].Type.(*ast.RefType); ok {
		mode = ReadRef
		if ref.Mut {
			mode = WriteRef
		}
	}
	return mode, false, fn.Params[1:]
}

func (c *checker) makeMember(fn *ast.FuncDecl, impl *Impl, trait *Trait) *Member {
	mode, static, params := receiverParams(fn)
	if impl != nil && impl.Trait == nil && mode == WriteRef && c.enumOf(impl.Target) != nil {
		c.diag(fn.Params[0], TraitInvalid, "enum inherent methods cannot use self &mut Self; return a new enum value")
	}
	if fn.Generics != nil || fn.Where != nil || len(fn.Recv) != 0 || fn.Path != nil {
		c.diag(fn, TraitInvalid, "trait and impl members cannot have method type parameters or a Go receiver")
	}
	if impl != nil && fn.Body == nil {
		c.diag(fn, TraitInvalid, "impl member requires a body")
	}
	if !static {
		recv := fn.Params[0].Type
		if r, ok := recv.(*ast.RefType); ok {
			recv = r.Elem
		}
		if _, ok := recv.(*ast.SelfType); !ok {
			c.diag(fn.Params[0], TraitInvalid, "impl receiver must be self Self, self &Self, or self &mut Self")
		}
		c.tuple(fn.Params[:1])
	}
	sig := types.NewSignatureType(nil, nil, nil, c.tuple(params), c.tuple(fn.Results), len(params) > 0 && params[len(params)-1].Variadic)
	sym := c.symbol(fn.Name)
	m := &Member{Decl: fn, File: c.file.Name, Symbol: sym, Name: fn.Name.Name, GoName: fn.Name.Name, Signature: sig, Receiver: mode, Static: static, Impl: impl, Trait: trait}
	if sym != nil {
		m.GoName = sym.GoName
		c.out.Symbols[sym] = sig
		c.memberSymbols[sym] = m
	}
	return m
}

func containsType(t, needle types.Type) bool {
	if t == needle {
		return true
	}
	switch t := t.(type) {
	case *types.Alias:
		return containsType(types.Unalias(t), needle)
	case *types.Pointer:
		return containsType(t.Elem(), needle)
	case *types.Slice:
		return containsType(t.Elem(), needle)
	case *types.Array:
		return containsType(t.Elem(), needle)
	case *types.Map:
		return containsType(t.Key(), needle) || containsType(t.Elem(), needle)
	case *types.Chan:
		return containsType(t.Elem(), needle)
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if containsType(t.TypeArgs().At(i), needle) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if containsType(t.At(i).Type(), needle) {
				return true
			}
		}
	case *types.Signature:
		return containsType(t.Params(), needle) || containsType(t.Results(), needle)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsType(t.Field(i).Type(), needle) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if containsType(t.ExplicitMethod(i).Type(), needle) {
				return true
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if containsType(t.EmbeddedType(i), needle) {
				return true
			}
		}
	}
	return false
}

func (c *checker) prepareImpls() {
	for _, pkg := range c.out.Resolve.Packages {
		c.pkg = c.packages[pkg]
		for _, file := range pkg.Files {
			c.file = file
			for _, item := range file.AST.Items {
				n, ok := item.(*ast.ImplDecl)
				if !ok {
					continue
				}
				c.pendingImpls = append(c.pendingImpls, pendingImpl{declaration: n, file: file, pkg: c.pkg})
			}
		}
	}
	c.completePendingImpls()
}

func (c *checker) prepareImpl(n *ast.ImplDecl) *Impl {
	params := c.genericParameters(n.Generics)
	requirements := c.sourceRequirements(n.Generics, nil, params, map[string]bool{}, n)
	target := types.Unalias(c.typ(n.Type))
	if !valid(target) {
		return nil
	}
	var trait *Trait
	if n.Trait != nil {
		if path, ok := n.Trait.(*ast.PathType); ok && len(path.Path.Segments) > 0 {
			trait = c.traitPath(path.Path.Segments)
		}
		if trait == nil {
			c.diag(n.Trait, TraitInvalid, "impl requires a declared trait")
			return nil
		}
	}
	named, _ := target.(*types.Named)
	if named != nil {
		named = named.Origin()
	}
	impl := &Impl{info: c.out, ConstParams: c.constParameters(n.Generics), Decl: n, File: c.file.Name, Target: target, Base: named, Owner: c.pkg, Trait: trait, TypeParams: params, Requirements: requirements, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}, associatedState: map[string]uint8{}}
	p := c.newPermissionChecker()
	impl.targetPermission = p.astType(n.Type, nil)
	if path, ok := n.Trait.(*ast.PathType); ok && trait != nil {
		last := path.Path.Segments[len(path.Path.Segments)-1]
		impl.Inputs, impl.InputNodes, impl.inputPermissions = c.readTraitInputArguments(last.Args, trait, n.Trait)
	}
	if trait == nil && (named == nil || named.Obj().Pkg() != c.pkg) {
		c.diag(n.Type, TraitInvalid, "inherent impl requires a local named target")
		return nil
	}
	if !c.implOwnerAllowed(impl) {
		c.diag(n.Type, TraitInvalid, "body impl requires the trait or an allowed local type in its ordered head to belong to this package")
		return nil
	}
	head := append([]types.Type{target}, impl.Inputs...)
	for _, param := range params {
		found := false
		for _, slot := range head {
			found = found || c.out.containsType(slot, param)
		}
		if !found {
			c.diag(n, TraitInvalid, "impl type parameter must occur in its head: "+param.Obj().Name())
		}
	}
	for _, parameter := range impl.ConstParams {
		found := false
		for _, slot := range head {
			found = found || c.out.containsConst(slot, parameter)
		}
		if !found {
			c.diag(n, TraitInvalid, "impl const parameter must occur in its head: "+parameter.Name.Name)
		}
	}
	c.out.Impls[n] = impl
	c.implOrder = append(c.implOrder, impl)
	return impl
}

func (c *checker) prepareImplBody(impl *Impl) {
	c.enterImpl(impl)
	for _, item := range impl.Decl.Items {
		if n, ok := item.(*ast.TypeDecl); ok {
			if impl.Trait == nil {
				c.diag(n, TraitInvalid, "associated types require a trait impl")
				continue
			}
			if !n.Alias || n.Type == nil || n.Generics != nil {
				c.diag(n, TraitInvalid, "associated type requires one concrete binding")
				continue
			}
			c.registerAssociated(impl, n.Name.Name, n.Type, n)
		}
	}
	used := map[string]bool{}
	for _, param := range impl.TypeParams {
		used[param.Obj().Name()] = true
	}
	for _, requirement := range impl.Requirements {
		for _, param := range requirement.Projected {
			used[param.Obj().Name()] = true
		}
	}
	for _, requirement := range c.sourceRequirements(nil, impl.Decl.Where, nil, used, impl.Decl) {
		c.addBound(&impl.Requirements, requirement, impl.Decl)
	}
	impl.Requirements = c.inheritedBounds(impl.Requirements, impl.Decl)
	if impl.Trait != nil {
		for _, a := range impl.Trait.Associated {
			if impl.AssociatedNodes[a.Name] == nil {
				c.diag(impl.Decl, TraitInvalid, "missing associated type "+a.Name)
			} else {
				c.resolveAssociated(impl, a.Name, impl.Decl)
			}
		}
	}
	if impl.Trait != nil {
		sub := map[*types.TypeParam]types.Type{impl.Trait.Self: impl.Target}
		head := c.implHeadPermissions(impl)
		sources := map[*types.TypeParam]*permission{impl.Trait.Self: head[0]}
		for index, parameter := range impl.Trait.TypeParams {
			if index < len(impl.Inputs) {
				sub[parameter] = impl.Inputs[index]
				sources[parameter] = head[index+1]
			}
		}
		for _, a := range impl.Trait.Associated {
			sub[a.Param] = impl.Associated[a.Name]
			sources[a.Param] = c.newPermissionChecker().implAssociated(impl, a.Name, nil)
		}
		for _, requirement := range impl.Trait.Requirements {
			c.addBound(&impl.Requirements, c.freshRequirement(requirement, sub, sources, used, c.pkg), impl.Decl)
		}
		impl.Requirements = c.inheritedBounds(impl.Requirements, impl.Decl)
		c.inheritTraitGoRequirements(c.implApplication(impl))
	}
	for _, item := range impl.Decl.Items {
		if n, ok := item.(*ast.TypeDecl); ok {
			c.out.Symbols[c.symbol(n.Name)] = impl.Associated[n.Name.Name]
		}
		if fn, ok := item.(*ast.FuncDecl); ok {
			impl.Methods = append(impl.Methods, c.makeMember(fn, impl, impl.Trait))
		}
	}
	p := c.newPermissionChecker()
	impl.associatedPermissions = map[string]*permission{}
	for name := range impl.Associated {
		impl.associatedPermissions[name] = p.implAssociated(impl, name, nil)
	}
	c.leaveImpl()
}

func (c *checker) registerAssociated(impl *Impl, name string, typ ast.Type, node ast.Node) {
	found := false
	for _, a := range impl.Trait.Associated {
		found = found || a.Name == name
	}
	if !found {
		c.diag(node, TraitInvalid, "unknown associated type "+name)
		return
	}
	if impl.AssociatedNodes[name] != nil {
		c.diag(node, TraitInvalid, "duplicate associated type "+name)
		return
	}
	impl.AssociatedNodes[name] = typ
}

func (c *checker) resolveAssociated(impl *Impl, name string, node ast.Node) types.Type {
	if impl.associatedState[name] == 1 {
		c.diag(node, TraitInvalid, "cyclic associated type binding "+name)
		return types.Typ[types.Invalid]
	}
	if t := impl.Associated[name]; t != nil {
		return t
	}
	binding := impl.AssociatedNodes[name]
	if binding == nil {
		c.diag(node, TraitInvalid, "unknown associated type "+name)
		return types.Typ[types.Invalid]
	}
	impl.associatedState[name] = 1
	t := c.typ(binding)
	impl.associatedState[name] = 2
	impl.Associated[name] = t
	return t
}

func (c *checker) enterImpl(i *Impl) {
	c.currentImpl, c.currentTrait, c.currentSelf = i, i.Trait, i.Target
	c.file = c.out.Resolve.Files[i.File]
	c.pkg = i.Owner
	// The declaration package owns foreign adapters.
	for _, p := range c.out.Resolve.Packages {
		for _, f := range p.Files {
			if f == c.file {
				c.pkg = c.packages[p]
			}
		}
	}
}
func (c *checker) leaveImpl() { c.currentImpl, c.currentTrait, c.currentSelf = nil, nil, nil }

func (c *checker) setAssociated(bindings map[string]types.Type, t *Trait, name string, typ types.Type, node ast.Node) {
	found := false
	for _, a := range t.Associated {
		found = found || a.Name == name
	}
	if !found {
		c.diag(node, TraitInvalid, "unknown associated type "+name)
		return
	}
	if bindings[name] != nil {
		c.diag(node, TraitInvalid, "duplicate associated type "+name)
		return
	}
	bindings[name] = typ
}
func (c *checker) readAssociated(args *ast.GenericArgs, t *Trait, bindings map[string]types.Type, node ast.Node) {
	if args == nil {
		return
	}
	for _, a := range args.Args {
		if a.Name == nil {
			continue
		}
		c.setAssociated(bindings, t, a.Name.Name, c.typ(a.Type), a)
	}
}

func (c *checker) readTraitInputs(args *ast.GenericArgs, trait *Trait, node ast.Node) ([]types.Type, []ast.Type, []*permission) {
	inputs, nodes, sources := c.readTraitInputArguments(args, trait, node)
	if len(inputs) == len(trait.TypeParams) {
		c.checkTraitInputConstraints(inputs, trait, node)
	}
	return inputs, nodes, sources
}

func (c *checker) readTraitInputArguments(args *ast.GenericArgs, trait *Trait, node ast.Node) ([]types.Type, []ast.Type, []*permission) {
	var inputs []types.Type
	var nodes []ast.Type
	var sources []*permission
	p := c.newPermissionChecker()
	if args != nil {
		named := false
		for _, arg := range args.Args {
			if arg.Name != nil {
				named = true
				continue
			}
			if named {
				c.diag(arg, TraitInvalid, "positional trait inputs must precede associated bindings")
			}
			if arg.Type == nil || arg.Value != nil {
				c.diag(arg, TraitInvalid, "trait inputs must be types")
				continue
			}
			inputs = append(inputs, c.typ(arg.Type))
			nodes = append(nodes, arg.Type)
			sources = append(sources, p.astType(arg.Type, nil))
		}
	}
	if len(inputs) != len(trait.TypeParams) {
		c.diag(node, TraitInvalid, fmt.Sprintf("trait %s requires %d positional inputs, got %d", trait.Symbol.Name.Name, len(trait.TypeParams), len(inputs)))
	}
	return inputs, nodes, sources
}

func (c *checker) checkTraitInputConstraints(inputs []types.Type, trait *Trait, node ast.Node) bool {
	sub := map[*types.TypeParam]types.Type{}
	for index, parameter := range trait.TypeParams {
		sub[parameter] = inputs[index]
	}
	validInputs := true
	for index, parameter := range trait.TypeParams {
		if constraint, ok := c.out.Substitute(parameter.Constraint(), sub, nil).Underlying().(*types.Interface); ok && valid(inputs[index]) && !c.satisfiesGo(inputs[index], constraint) {
			c.diag(node, GenericConstraint, "trait input does not satisfy declared constraint: "+parameter.Obj().Name())
			validInputs = false
		}
	}
	return validInputs
}

func (c *checker) finishImpls() {
	for _, impl := range c.implOrder {
		c.enterImpl(impl)
		if impl.Trait != nil {
			for _, decl := range impl.Trait.Methods {
				var actual *Member
				for _, m := range impl.Methods {
					if m.Name == decl.Name {
						actual = m
						break
					}
				}
				want := c.implSignature(decl, impl)
				if actual == nil {
					c.diag(impl.Decl, TraitInvalid, "missing trait member "+decl.Name)
					continue
				}
				if actual != nil && (actual.Static != decl.Static || actual.Receiver != decl.Receiver || !sameSignature(actual.Signature, want)) {
					c.diag(actual.Decl, TraitInvalid, "signature does not match trait member "+decl.Name)
				}
			}
			for _, m := range impl.Methods {
				known := false
				for _, decl := range impl.Trait.Methods {
					known = known || m.Name == decl.Name
				}
				if !known {
					c.diag(m.Decl, TraitInvalid, "unknown trait member "+m.Name)
				}
			}
		}
		value, borrow, write := false, false, false
		for _, m := range impl.Methods {
			if m.Static {
				continue
			}
			switch m.Receiver {
			case ValueRef:
				value = true
			case ReadRef:
				borrow = true
			case WriteRef:
				write = true
			}
		}
		if write || (value && borrow) {
			impl.Capture = WriteRef
		} else if borrow {
			impl.Capture = ReadRef
		}
		c.leaveImpl()
	}
}

func sameSignature(a, b *types.Signature) bool {
	return types.Identical(a.Params(), b.Params()) && types.Identical(a.Results(), b.Results()) && a.Variadic() == b.Variadic()
}
func (c *checker) implSignature(m *Member, impl *Impl) *types.Signature {
	sub := map[*types.TypeParam]types.Type{impl.Trait.Self: impl.Target}
	for index, parameter := range impl.Trait.TypeParams {
		if index < len(impl.Inputs) {
			sub[parameter] = impl.Inputs[index]
		}
	}
	for _, a := range impl.Trait.Associated {
		if t := impl.Associated[a.Name]; t != nil {
			sub[a.Param] = t
		}
	}
	return substituteSignature(m.Signature, sub, c.out)
}
func substituteSignature(sig *types.Signature, sub map[*types.TypeParam]types.Type, info *Info) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, substituteTuple(sig.Params(), sub, info), substituteTuple(sig.Results(), sub, info), sig.Variadic())
}
func substituteTuple(tuple *types.Tuple, sub map[*types.TypeParam]types.Type, info *Info) *types.Tuple {
	if tuple == nil {
		return nil
	}
	vars := make([]*types.Var, tuple.Len())
	for i := range vars {
		v := tuple.At(i)
		vars[i] = types.NewVar(0, v.Pkg(), v.Name(), substitute(v.Type(), sub, info))
	}
	return types.NewTuple(vars...)
}
