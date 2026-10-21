package check

import (
	"crypto/sha256"
	"fmt"
	"go/types"
	"path/filepath"
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
	Decl                *ast.ImplDecl
	File                string
	Target              types.Type
	Base                *types.Named
	Trait               *Trait
	Associated          map[string]types.Type
	AssociatedNodes     map[string]ast.Type
	Methods             []*Member
	Declare             bool
	Capture             RefMode
	AliasOf             *Impl
	Owner               *types.Package
	TypeParams          []*types.TypeParam
	TypeArgs            []types.Type
	Template            *Impl
	Requirements        []*TraitBound
	RequirementEvidence []Evidence
	Substitution        map[*types.TypeParam]types.Type
	associatedState     map[string]uint8
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
	File            string
	Param           *types.TypeParam
	Target          types.Type
	Trait           *Trait
	Associated      map[string]types.Type
	Projected       []*types.TypeParam
	AssociatedNodes map[string]ast.Type
	substitution    map[*types.TypeParam]types.Type
}

// Evidence records the selected source implementation or an in-scope premise.
// It never derives source protocol membership from a Go method set.
type Evidence struct {
	Bound *TraitBound
	Impl  *Impl
}

type FunctionInstance struct {
	Signature    *types.Signature
	TypeArgs     []types.Type
	Evidence     []Evidence
	Decl         *ast.FuncDecl
	File         string
	Substitution map[*types.TypeParam]types.Type
	Body         *BodyInstance
}

func (c *checker) collectTrait(n *ast.TraitDecl) {
	s := c.symbol(n.Name)
	if s == nil {
		return
	}
	t := types.NewNamed(types.NewTypeName(0, c.pkg, s.GoName, nil), anyType, nil)
	packagePath := c.pkg.Path()
	if path := c.out.PackagePaths[packagePath]; path != "" {
		packagePath = path
	} else if filepath.IsAbs(packagePath) {
		// A source-only input has no module identity. Keep its marker stable
		// across checkout locations while distinguishing source packages.
		root := packagePath
		for _, p := range c.out.Resolve.Packages {
			for {
				rel, err := filepath.Rel(root, p.Dir)
				if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					break
				}
				root = filepath.Dir(root)
			}
		}
		rel, _ := filepath.Rel(root, packagePath)
		packagePath = filepath.ToSlash(rel) + "/" + c.pkg.Name()
	}
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
		if t.Decl.Generics != nil {
			c.diag(t.Decl, TraitInvalid, "traits must be non-generic")
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
		params := make([]*types.TypeParam, 0, len(t.Associated))
		for _, a := range t.Associated {
			params = append(params, a.Param)
		}
		t.Type.SetTypeParams(params)
	})
	c.eachTrait(func(t *Trait) {
		c.currentTrait, c.currentSelf = t, t.Self
		t.Requirements = c.sourceRequirements(nil, t.Decl.Where, nil, map[string]bool{}, t.Decl)
		for _, requirement := range t.Requirements {
			if requirement.Target != t.Self {
				c.diag(t.Decl, TraitInvalid, "trait prerequisites must constrain Self")
			}
		}
		c.currentTrait, c.currentSelf = nil, nil
	})
	c.checkTraitPrerequisiteCycles()
	c.eachTrait(func(t *Trait) {
		c.currentTrait, c.currentSelf = t, t.Self
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
		methods = append(methods, types.NewFunc(0, c.pkg, t.Marker, types.NewSignatureType(nil, nil, nil, nil, nil, false)))
		t.Type.SetUnderlying(types.NewInterfaceType(methods, nil).Complete())
		c.currentTrait, c.currentSelf = nil, nil
	})
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
	if fn.Generics != nil || fn.Where != nil || len(fn.Recv) != 0 || fn.Path != nil {
		c.diag(fn, TraitInvalid, "trait and impl members cannot have method type parameters or a Go receiver")
	}
	if impl != nil && !impl.Declare && fn.Body == nil {
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
				declare := false
				if d, ok := item.(*ast.DeclareDecl); ok {
					item = d.Decl
					declare = true
				}
				n, ok := item.(*ast.ImplDecl)
				if !ok {
					continue
				}
				builtin := false
				for _, d := range c.out.Resolve.Derives {
					if d.Impl == n {
						builtin = true
						break
					}
				}
				if builtin {
					continue
				}
				if declare && (n.Generics != nil || n.Where != nil) {
					c.diag(n, TraitInvalid, "declare impl requires a concrete non-generic target")
					continue
				}
				params := c.genericParameters(n.Generics)
				requirements := c.sourceRequirements(n.Generics, nil, params, map[string]bool{}, n)
				target := types.Unalias(c.typ(n.Type))
				if !valid(target) {
					continue
				}
				if _, bare := target.(*types.TypeParam); bare {
					c.diag(n.Type, TraitInvalid, "impl target cannot be a bare type parameter")
					continue
				}
				for _, param := range params {
					if !containsType(target, param) {
						c.diag(n, TraitInvalid, "impl type parameter must occur in its target: "+param.Obj().Name())
					}
				}
				var trait *Trait
				if n.Trait != nil {
					if path, ok := n.Trait.(*ast.PathType); ok && len(path.Path.Segments) > 0 {
						trait = c.traitPath(path.Path.Segments)
					}
					if trait == nil {
						c.diag(n.Trait, TraitInvalid, "impl requires a declared trait")
						continue
					}
				}
				named, _ := target.(*types.Named)
				localTarget := named != nil && named.Obj().Pkg() == c.pkg
				if declare {
					base := target
					if pointer, ok := base.(*types.Pointer); ok {
						base = types.Unalias(pointer.Elem())
					}
					named, _ = base.(*types.Named)
					if named == nil || named.TypeArgs().Len() != 0 || named.TypeParams().Len() != 0 {
						c.diag(n.Type, TraitInvalid, "declare impl requires a concrete named type")
						continue
					}
				} else if !localTarget && (trait == nil || trait.Type.Obj().Pkg() != c.pkg) {
					c.diag(n.Type, TraitInvalid, "body impl requires the trait or outer named target to belong to this package")
					continue
				}
				if named != nil {
					named = named.Origin()
				}
				impl := &Impl{Decl: n, File: file.Name, Target: target, Base: named, Owner: c.pkg, Trait: trait, TypeParams: params, Requirements: requirements, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}, associatedState: map[string]uint8{}, Declare: declare}
				c.out.Impls[n] = impl
				c.implOrder = append(c.implOrder, impl)
			}
		}
	}
	for _, impl := range c.implOrder {
		c.enterImpl(impl)
		if impl.Trait != nil {
			for _, old := range c.implOrder {
				if old == impl {
					break
				}
				if old.Trait == impl.Trait && implTargetsOverlap(old.Target, impl.Target, old.TypeParams, impl.TypeParams) && !old.Declare && !impl.Declare {
					c.diag(impl.Decl, TraitInvalid, "duplicate trait implementation: overlapping target patterns")
				}
			}
		}
		if impl.Declare && impl.Trait != nil {
			if path, ok := impl.Decl.Trait.(*ast.PathType); ok {
				args := path.Path.Segments[len(path.Path.Segments)-1].Args
				if args != nil {
					for _, a := range args.Args {
						if a.Name != nil {
							c.registerAssociated(impl, a.Name.Name, a.Type, a)
						} else {
							c.diag(a, TraitInvalid, "trait arguments must name an associated type")
						}
					}
				}
			}
		}
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
			for _, a := range impl.Trait.Associated {
				sub[a.Param] = impl.Associated[a.Name]
			}
			for _, requirement := range impl.Trait.Requirements {
				c.addBound(&impl.Requirements, freshRequirement(requirement, sub, used, c.pkg), impl.Decl)
			}
			impl.Requirements = c.inheritedBounds(impl.Requirements, impl.Decl)
		}
		for _, item := range impl.Decl.Items {
			if n, ok := item.(*ast.TypeDecl); ok {
				c.out.Symbols[c.symbol(n.Name)] = impl.Associated[n.Name.Name]
			}
			if fn, ok := item.(*ast.FuncDecl); ok {
				impl.Methods = append(impl.Methods, c.makeMember(fn, impl, impl.Trait))
			}
		}
		c.leaveImpl()
	}
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
			c.diag(node, TraitInvalid, "trait arguments must name an associated type")
			continue
		}
		c.setAssociated(bindings, t, a.Name.Name, c.typ(a.Type), a)
	}
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
				if impl.Declare {
					if decl.Static {
						c.diag(impl.Decl, TraitInvalid, "declare impl cannot adopt a static member")
						continue
					}
					receiver := impl.Target
					if decl.Receiver != ValueRef {
						receiver = types.NewPointer(receiver)
					}
					obj, _, _ := types.LookupFieldOrMethod(receiver, false, c.pkg, decl.GoName)
					fn, ok := obj.(*types.Func)
					if !ok || !sameSignature(fn.Type().(*types.Signature), want) {
						c.diag(impl.Decl, TraitInvalid, "native method does not match "+decl.GoName)
						continue
					}
					actual = &Member{File: decl.File, Name: decl.Name, GoName: decl.GoName, Signature: want, Receiver: decl.Receiver, Trait: impl.Trait, Impl: impl, Native: fn, Decl: decl.Decl, Symbol: decl.Symbol}
					impl.Methods = append(impl.Methods, actual)
				} else if actual == nil {
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
		for _, old := range c.implOrder {
			if old == impl {
				break
			}
			if old.Trait == impl.Trait && impl.Trait != nil && implTargetsOverlap(old.Target, impl.Target, old.TypeParams, impl.TypeParams) {
				compatible := old.Declare && impl.Declare
				for _, a := range impl.Trait.Associated {
					compatible = compatible && types.Identical(old.Associated[a.Name], impl.Associated[a.Name])
				}
				if !compatible && (old.Declare || impl.Declare) {
					c.diag(impl.Decl, TraitInvalid, "conflicting trait implementation")
				} else if compatible && c.sameSourcePackage(impl.File, old.File) {
					impl.AliasOf = old
					if old.AliasOf != nil {
						impl.AliasOf = old.AliasOf
					}
				}
			}
		}
		c.leaveImpl()
	}
}

func sameSignature(a, b *types.Signature) bool {
	return types.Identical(a.Params(), b.Params()) && types.Identical(a.Results(), b.Results()) && a.Variadic() == b.Variadic()
}
func (c *checker) implSignature(m *Member, impl *Impl) *types.Signature {
	sub := map[*types.TypeParam]types.Type{impl.Trait.Self: impl.Target}
	for _, a := range impl.Trait.Associated {
		if t := impl.Associated[a.Name]; t != nil {
			sub[a.Param] = t
		}
	}
	return substituteSignature(m.Signature, sub)
}
func substituteSignature(sig *types.Signature, sub map[*types.TypeParam]types.Type) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, substituteTuple(sig.Params(), sub), substituteTuple(sig.Results(), sub), sig.Variadic())
}
func substituteTuple(tuple *types.Tuple, sub map[*types.TypeParam]types.Type) *types.Tuple {
	if tuple == nil {
		return nil
	}
	vars := make([]*types.Var, tuple.Len())
	for i := range vars {
		v := tuple.At(i)
		vars[i] = types.NewVar(0, v.Pkg(), v.Name(), substitute(v.Type(), sub))
	}
	return types.NewTuple(vars...)
}
