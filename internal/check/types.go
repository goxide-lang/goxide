package check

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

var anyType = types.NewInterfaceType(nil, nil).Complete()

func (c *checker) enumNames() {
	for _, pkg := range c.out.Resolve.Packages {
		used := map[string]bool{}
		for _, symbol := range pkg.Scope.Bindings {
			if symbol.Kind != resolve.Import && symbol.Kind != resolve.Module && symbol.Kind != resolve.Macro {
				used[symbol.GoName] = true
			}
		}
		for _, file := range pkg.Files {
			c.file = file
			ast.Walk(file.AST, func(n ast.Node) bool {
				if decl, ok := n.(*ast.EnumDecl); ok {
					if e := c.out.Enums[decl]; e != nil {
						for _, v := range e.Variants {
							if used[v.GoName] {
								c.diag(v.Decl.Name, EnumGoNameConflict, fmt.Sprintf("enum variant Go name %q conflicts with another package declaration", v.GoName))
							}
							used[v.GoName] = true
						}
					}
					return false
				}
				return true
			})
		}
	}
}

func (c *checker) builtinEnums(standalone bool) {
	const path = "github.com/goxide-lang/std/sum"
	var p *types.Package
	var importErr error
	if standalone {
		// Check without an importer is the isolated, in-memory checking mode.
		p = types.NewPackage(path, "sum")
	} else {
		p, importErr = c.importer.Import(path)
	}
	c.builtinErrors = make(map[string]error)
	for _, name := range []string{"Option", "Result"} {
		if importErr != nil {
			c.builtinErrors[name] = fmt.Errorf("import %q: %w", path, importErr)
			continue
		}
		count := 1
		if name == "Result" {
			count = 2
		}
		var n *types.Named
		if standalone {
			n = types.NewNamed(types.NewTypeName(0, p, name, nil), types.NewStruct(nil, nil), nil)
			params := []*types.TypeParam{types.NewTypeParam(types.NewTypeName(0, p, "T", nil), anyType)}
			if count == 2 {
				params = append(params, types.NewTypeParam(types.NewTypeName(0, p, "E", nil), anyType))
			}
			n.SetTypeParams(params)
		} else if object, ok := p.Scope().Lookup(name).(*types.TypeName); ok {
			n, _ = types.Unalias(object.Type()).(*types.Named)
		}
		if n == nil || n.TypeParams().Len() != count {
			c.builtinErrors[name] = fmt.Errorf("%s.%s must be a named type with %d type parameters", path, name, count)
			continue
		}
		params := n.TypeParams()
		e := &Enum{info: c.out, Name: name, GoName: name, Type: n, Builtin: true}
		if name == "Option" {
			e.Variants = []*Variant{{Name: "None", GoName: "None"}, {Name: "Some", GoName: "Some", Fields: []Field{{Name: "Value", GoName: "Value", Type: params.At(0)}}}}
		} else {
			e.Variants = []*Variant{{Name: "Ok", GoName: "Ok", Fields: []Field{{Name: "Value", GoName: "Value", Type: params.At(0)}}}, {Name: "Err", GoName: "Err", Fields: []Field{{Name: "Value", GoName: "Value", Type: params.At(1)}}}}
		}
		c.builtins[name], c.out.enums[n] = e, e
	}
}

func (c *checker) builtinEnum(name string) *Enum {
	if name != "Option" && name != "Result" {
		return nil
	}
	if c.builtinErrors == nil {
		c.builtinEnums(c.standalone)
	}
	if err := c.builtinErrors[name]; err != nil {
		c.diag(c.file.AST, "go-import", err.Error())
	}
	return c.builtins[name]
}

// Native signatures may introduce Option/Result without a source type name.
func (c *checker) enumOf(t types.Type) *Instance {
	if t != nil {
		if named, ok := types.Unalias(t).(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "github.com/goxide-lang/std/sum" {
			c.builtinEnum(named.Obj().Name())
		}
	}
	return c.out.EnumOf(t)
}

func (c *checker) collect(items []ast.Item) {
	for _, item := range items {
		switch n := item.(type) {
		case *ast.EnumDecl:
			s := c.symbol(n.Name)
			if s == nil {
				continue
			}
			marker := types.NewFunc(0, c.pkg, "is"+n.Name.Name, types.NewSignatureType(nil, nil, nil, nil, nil, false))
			t := types.NewNamed(types.NewTypeName(0, c.pkg, s.GoName, nil), types.NewInterfaceType([]*types.Func{marker}, nil).Complete(), nil)
			c.named[s], c.out.Symbols[s] = t, t
			e := &Enum{info: c.out, Name: n.Name.Name, GoName: s.GoName, Decl: n, Type: t}
			c.out.Enums[n], c.out.enums[t] = e, e
		case *ast.TypeDecl:
			if n.Grouped {
				for _, s := range n.Specs {
					c.collectType(s.Name, s.Type, s.Generics, s.Alias)
				}
			} else {
				c.collectType(n.Name, n.Type, n.Generics, n.Alias)
			}
		case *ast.ConstDecl:
			for _, spec := range n.Specs {
				for index, name := range spec.Names {
					if symbol := c.symbol(name); symbol != nil && index < len(spec.Values) {
						c.sourceValues[symbol] = sourceExpression{file: c.file, node: spec.Values[index]}
						if spec.Type != nil {
							c.sourceTypes[symbol] = permissionDeclaration{c.file, spec.Type}
						}
					}
				}
			}
		case *ast.TraitDecl:
			c.collectTrait(n)
		case *ast.ModDecl:
			c.collect(n.Items)
		case *ast.DeclareDecl:
			c.collect([]ast.Item{n.Decl})
		}
	}
}

type typeDeclaration struct {
	file                     *resolve.File
	node                     ast.Type
	generics                 *ast.GenericParams
	alias, defining, defined bool
}

func (c *checker) collectType(id *ast.Ident, t ast.Type, generics *ast.GenericParams, alias bool) {
	s := c.symbol(id)
	if s == nil {
		return
	}
	n := types.NewNamed(types.NewTypeName(0, c.pkg, s.GoName, nil), types.Typ[types.Invalid], nil)
	c.named[s], c.out.Symbols[s] = n, n
	c.declarations[s] = &typeDeclaration{file: c.file, node: t, generics: generics, alias: alias}
}

func (c *checker) params(g *ast.GenericParams) []*types.TypeParam {
	if g == nil {
		return nil
	}
	var out []*types.TypeParam
	for _, p := range g.Params {
		if p.Const {
			c.diag(p, GenericConstraint, "const parameters are only supported on functions and impls")
			continue
		}
		for _, name := range p.Names {
			t := types.NewTypeParam(types.NewTypeName(0, c.pkg, name.Name, nil), anyType)
			if s := c.symbol(name); s != nil {
				c.out.Symbols[s] = t
			}
			out = append(out, t)
		}
	}
	i := 0
	for _, p := range g.Params {
		if p.Const {
			continue
		}
		for range p.Names {
			if p.Constraint != nil && c.declarationEvidenceReady(p.Constraint) {
				out[i].SetConstraint(c.typ(p.Constraint))
			}
			i++
		}
	}
	return out
}

func (c *checker) prepareGenerics(items []ast.Item) {
	for _, item := range items {
		switch n := item.(type) {
		case *ast.EnumDecl:
			if e := c.out.Enums[n]; e != nil {
				params := c.params(n.Generics)
				e.Type.SetTypeParams(params)
				var args []*types.Var
				for _, param := range params {
					args = append(args, types.NewVar(0, c.pkg, "", param))
				}
				marker := types.NewFunc(0, c.pkg, "is"+n.Name.Name, types.NewSignatureType(nil, nil, nil, types.NewTuple(args...), nil, false))
				e.Type.SetUnderlying(types.NewInterfaceType([]*types.Func{marker}, nil).Complete())
			}
		case *ast.TypeDecl:
			if n.Grouped {
				for _, s := range n.Specs {
					if t := c.named[c.symbol(s.Name)]; t != nil {
						t.SetTypeParams(c.params(s.Generics))
					}
				}
			} else if t := c.named[c.symbol(n.Name)]; t != nil {
				t.SetTypeParams(c.params(n.Generics))
			}
		case *ast.ModDecl:
			c.prepareGenerics(n.Items)
		case *ast.DeclareDecl:
			c.prepareGenerics([]ast.Item{n.Decl})
		}
	}
}

func (c *checker) signatures(items []ast.Item) {
	for _, item := range items {
		if function, ok := item.(*ast.FuncDecl); ok {
			if c.signaturePhase == 1 && len(function.Recv) == 0 || c.signaturePhase == 2 && len(function.Recv) != 0 {
				continue
			}
		}
		if c.signaturePhase == 2 {
			if _, enum := item.(*ast.EnumDecl); enum {
				continue
			}
		}
		if c.signaturePhase == 1 {
			switch item.(type) {
			case *ast.TypeDecl, *ast.EnumDecl, *ast.FuncDecl:
				if c.signatureNeedsEvidence(item, map[*resolve.Symbol]bool{}) {
					c.pendingDeclarations = append(c.pendingDeclarations, pendingDeclaration{item, c.file, c.pkg})
					continue
				}
			}
		}
		switch n := item.(type) {
		case *ast.EnumDecl:
			c.completeGenericConstraints(n.Generics)
			e := c.out.Enums[n]
			if e == nil {
				continue
			}
			for _, v := range n.Variants {
				name := v.Name.Name
				if s := c.symbol(v.Name); s != nil {
					name = s.GoName
				}
				variant := &Variant{Name: v.Name.Name, GoName: name, Decl: v}
				index := 0
				for _, f := range v.Fields {
					t := c.typ(f.Type)
					if v.Kind == ast.Tuple {
						field := "Value"
						if len(v.Fields) > 1 {
							field = fmt.Sprintf("V%d", index)
						}
						variant.Fields = append(variant.Fields, Field{Name: field, GoName: field, Type: t})
						index++
					} else {
						for _, id := range f.Names {
							name := id.Name
							if s := c.symbol(id); s != nil {
								name = s.GoName
							}
							variant.Fields = append(variant.Fields, Field{Name: id.Name, GoName: name, Type: t})
						}
					}
				}
				e.Variants = append(e.Variants, variant)
				if s := c.symbol(v.Name); s != nil {
					c.variants[s] = &Constructor{Enum: c.enumOf(e.Type), Variant: variant}
				}
			}
		case *ast.TypeDecl:
			if n.Grouped {
				for _, s := range n.Specs {
					c.typeSignature(s.Name, s.Type, s.Generics, s.Alias)
				}
			} else {
				c.typeSignature(n.Name, n.Type, n.Generics, n.Alias)
			}
		case *ast.FuncDecl:
			previousState := c.caseState
			state := &bodyCaseState{}
			c.caseState = state
			c.receiverParams(n.Recv)
			params := c.functionParams(n)
			oldBounds := c.activeBounds
			c.activeBounds = c.out.GenericBounds[n]
			sig := types.NewSignatureType(nil, nil, params, c.tuple(n.Params), c.tuple(n.Results), len(n.Params) > 0 && n.Params[len(n.Params)-1].Variadic)
			c.functionBounds[sig] = c.out.GenericBounds[n]
			if c.caseFunctions == nil {
				c.caseFunctions = map[*types.Signature]caseFunction{}
			}
			c.caseFunctions[sig] = caseFunction{declaration: n, file: c.file, pkg: c.pkg}
			c.out.FunctionConstParams[sig] = c.constParameters(n.Generics)
			c.out.FunctionGenerics[sig] = n.Generics
			if s := c.symbol(n.Name); s != nil {
				c.out.Symbols[s] = sig
			}
			if len(n.Recv) > 0 {
				recv := c.typ(n.Recv[0].Type)
				base := recv
				if p, ok := base.(*types.Pointer); ok {
					base = p.Elem()
				}
				if named, ok := base.(*types.Named); ok {
					method := types.NewSignatureType(types.NewVar(0, c.pkg, "", recv), nil, nil, sig.Params(), sig.Results(), sig.Variadic())
					named.Origin().AddMethod(types.NewFunc(0, c.pkg, n.Name.Name, method))
				}
			}
			c.activeBounds = oldBounds
			c.caseState = previousState
			if len(state.choices) != 0 {
				if c.signatureCases == nil {
					c.signatureCases = map[*ast.FuncDecl]*bodyCaseState{}
				}
				c.signatureCases[n] = state
			}
		case *ast.ModDecl:
			c.signatures(n.Items)
		case *ast.DeclareDecl:
			c.signatures([]ast.Item{n.Decl})
		}
	}
}

func (c *checker) receiverParams(params []*ast.Param) {
	if len(params) == 0 {
		return
	}
	t := params[0].Type
	if p, ok := t.(*ast.PointerType); ok {
		t = p.Elem
	}
	path, ok := t.(*ast.PathType)
	if !ok || path.Path == nil || len(path.Path.Segments) == 0 {
		return
	}
	seg := path.Path.Segments[len(path.Path.Segments)-1]
	if seg.Args == nil {
		return
	}
	named, ok := c.symbolType(c.symbol(seg.Name)).(*types.Named)
	if !ok {
		return
	}
	for i, arg := range seg.Args.Args {
		if i >= named.TypeParams().Len() {
			break
		}
		if typ, ok := arg.Type.(*ast.PathType); ok && len(typ.Path.Segments) == 1 {
			if s := c.symbol(typ.Path.Segments[0].Name); s != nil {
				c.out.Symbols[s] = named.TypeParams().At(i)
			}
		}
	}
}

func (c *checker) typeSignature(name *ast.Ident, t ast.Type, g *ast.GenericParams, alias bool) {
	c.completeGenericConstraints(g)
	s := c.symbol(name)
	c.defineType(s)
}

func (c *checker) defineType(s *resolve.Symbol) {
	decl := c.declarations[s]
	if decl == nil || decl.defined || decl.defining {
		return
	}
	oldFile, oldPkg := c.file, c.pkg
	c.file = decl.file
	if !c.declarationEvidenceReady(decl.node) || decl.generics != nil && !c.declarationEvidenceReady(decl.generics) {
		c.file = oldFile
		return
	}
	decl.defining = true
	n := c.named[s]
	if n == nil {
		return
	}
	c.pkg = n.Obj().Pkg()
	underlying := c.typ(decl.node)
	if decl.alias {
		var params []*types.TypeParam
		sub := make(map[*types.TypeParam]types.Type)
		for i := 0; i < n.TypeParams().Len(); i++ {
			old := n.TypeParams().At(i)
			param := types.NewTypeParam(types.NewTypeName(0, c.pkg, old.Obj().Name(), nil), anyType)
			params = append(params, param)
			sub[old] = param
		}
		for i, param := range params {
			param.SetConstraint(substitute(n.TypeParams().At(i).Constraint(), sub, c.out))
		}
		alias := types.NewAlias(types.NewTypeName(0, c.pkg, s.GoName, nil), substitute(underlying, sub, c.out))
		alias.SetTypeParams(params)
		c.out.Symbols[s] = alias
	} else if valid(underlying) {
		n.SetUnderlying(underlying.Underlying())
	}
	decl.defined, decl.defining = true, false
	c.file, c.pkg = oldFile, oldPkg
}

func (c *checker) tuple(params []*ast.Param) *types.Tuple {
	var vars []*types.Var
	for _, p := range params {
		t := c.typ(p.Type)
		if p.Variadic {
			t = types.NewSlice(t)
		}
		if len(p.Names) == 0 {
			vars = append(vars, types.NewVar(0, c.pkg, "", t))
		}
		for _, name := range p.Names {
			vars = append(vars, types.NewVar(0, c.pkg, name.Name, t))
			if s := c.symbol(name); s != nil {
				c.out.Symbols[s] = t
				c.sourceTypes[s] = permissionDeclaration{c.file, p.Type}
			}
		}
	}
	return types.NewTuple(vars...)
}

func (c *checker) typ(node ast.Type) (result types.Type) {
	if node == nil {
		return types.Typ[types.Invalid]
	}
	defer func() {
		if c.currentImpl == nil || c.currentImpl.Template == nil {
			c.out.typeNodes[node] = result
		}
	}()
	if t, ok := c.traitType(node); ok {
		if c.currentImpl == nil || c.currentImpl.Template == nil {
			c.out.TypeOverrides[node] = t
		}
		return t
	}
	switch n := node.(type) {
	case *ast.PathType:
		return c.pathType(n.Path)
	case *ast.PointerType:
		return types.NewPointer(c.typ(n.Elem))
	case *ast.RefType:
		return types.NewPointer(c.typ(n.Elem))
	case *ast.SliceType:
		return types.NewSlice(c.typ(n.Elem))
	case *ast.ArrayType:
		if n.Ellipsis {
			if known := c.out.typeNodes[n]; valid(known) {
				return known
			}
			c.diag(n, GenericConstraint, "an inferred array length requires a composite literal")
			return types.Typ[types.Invalid]
		}
		length, ok := c.constantLength(n.Len, true)
		if !ok {
			return types.Typ[types.Invalid]
		}
		return c.out.ArrayType(c.typ(n.Elem), length)
	case *ast.MapType:
		return types.NewMap(c.typ(n.Key), c.typ(n.Value))
	case *ast.ChanType:
		return types.NewChan(types.ChanDir(n.Dir), c.typ(n.Elem))
	case *ast.ParenType:
		return c.typ(n.Type)
	case *ast.FuncType:
		return types.NewSignatureType(nil, nil, nil, c.tuple(n.Params), c.tuple(n.Results), len(n.Params) > 0 && n.Params[len(n.Params)-1].Variadic)
	case *ast.StructType:
		var fields []*types.Var
		for _, f := range n.Fields {
			t := c.typ(f.Type)
			if len(f.Names) == 0 {
				base := types.Unalias(t)
				if pointer, ok := base.(*types.Pointer); ok {
					base = types.Unalias(pointer.Elem())
				}
				if named, ok := base.(*types.Named); ok {
					fields = append(fields, types.NewField(0, c.pkg, named.Obj().Name(), t, true))
				}
			}
			for _, id := range f.Names {
				fields = append(fields, types.NewField(0, c.pkg, id.Name, t, false))
			}
		}
		return types.NewStruct(fields, nil)
	case *ast.InterfaceType:
		var methods []*types.Func
		var embeds []types.Type
		for _, m := range n.Methods {
			if m.Name != nil {
				methods = append(methods, types.NewFunc(0, c.pkg, m.Name.Name, types.NewSignatureType(nil, nil, nil, c.tuple(m.Params), c.tuple(m.Results), false)))
			} else if m.Type != nil {
				embeds = append(embeds, c.typ(m.Type))
			}
		}
		c.validateSymbolicInterface(n, methods, embeds)
		return types.NewInterfaceType(methods, embeds).Complete()
	case *ast.UnionType:
		var terms []*types.Term
		for _, t := range n.Terms {
			if a, ok := t.(*ast.ApproxType); ok {
				terms = append(terms, types.NewTerm(true, c.typ(a.Type)))
			} else {
				terms = append(terms, types.NewTerm(false, c.typ(t)))
			}
		}
		return types.NewInterfaceType(nil, []types.Type{types.NewUnion(terms)}).Complete()
	case *ast.ApproxType:
		return types.NewInterfaceType(nil, []types.Type{types.NewUnion([]*types.Term{types.NewTerm(true, c.typ(n.Type))})}).Complete()
	case *ast.ProjectionType:
		if s := c.symbol(n.Item); s != nil {
			return c.symbolType(s)
		}
	}
	return types.Typ[types.Invalid]
}

func (c *checker) symbolType(s *resolve.Symbol) types.Type {
	if s == nil {
		return types.Typ[types.Invalid]
	}
	for s.AliasOf != nil {
		s = s.AliasOf
	}
	c.defineType(s)
	if t := c.out.Symbols[s]; t != nil {
		return t
	}
	if s.Decl.File == "" && len(s.Target) == 0 {
		if e := c.builtinEnum(s.Name.Name); e != nil {
			return e.Type
		}
		if obj := types.Universe.Lookup(s.GoName); obj != nil {
			return obj.Type()
		}
	}
	if len(s.Target) >= 2 && s.Kind != resolve.Import && s.Kind != resolve.Module {
		return c.importedType(s.Target, s.GoName, true)
	}
	return types.Typ[types.Invalid]
}

func (c *checker) pathType(path *ast.Path) types.Type {
	if path == nil || len(path.Segments) == 0 {
		return types.Typ[types.Invalid]
	}
	last := path.Segments[len(path.Segments)-1]
	t := c.symbolType(c.sourcePathSymbol(path.Segments))
	if !valid(t) {
		t = c.importType(path)
	}
	if !valid(t) && len(path.Segments) > 1 {
		res := c.file.References[path.ID()]
		if res.Deferred && res.Symbol != nil {
			switch res.Symbol.Kind {
			case resolve.Type, resolve.TypeParam, resolve.Enum, resolve.Trait:
				c.diag(path, TraitInvalid, "unknown associated type or member path")
			}
		}
	}
	if last.Args != nil {
		t = c.instantiate(t, last.Args)
	}
	return t
}

type genericInstance struct {
	file   *resolve.File
	typ    types.Type
	args   *ast.GenericArgs
	actual []types.Type
}

func (c *checker) validateInstance(i genericInstance) {
	_, err := c.instantiateGo(i.typ, i.actual)
	if err == nil {
		return
	}
	old := c.file
	c.file = i.file
	defer func() { c.file = old }()
	n := ast.Node(i.args)
	if argument, ok := err.(*types.ArgumentError); ok && argument.Index < len(i.args.Args) {
		n = i.args.Args[argument.Index].Type
	}
	c.diag(n, GenericConstraint, err.Error())
}

func (c *checker) instantiate(t types.Type, args *ast.GenericArgs) types.Type {
	if sig, ok := t.(*types.Signature); ok {
		return c.explicitFunctionArguments(sig, args)
	}
	actual := make([]types.Type, 0, len(args.Args))
	for _, a := range args.Args {
		actual = append(actual, c.genericArgType(a))
	}
	if !valid(t) {
		return t
	}
	count := 0
	switch t := t.(type) {
	case *types.Named:
		count = t.TypeParams().Len()
	case *types.Alias:
		count = t.TypeParams().Len()
	case *types.Signature:
		count = t.TypeParams().Len()
	default:
		return t
	}
	if len(actual) != count {
		c.diag(args, GenericConstraint, fmt.Sprintf("got %d type arguments but want %d", len(actual), count))
		return types.Typ[types.Invalid]
	}
	c.instances = append(c.instances, genericInstance{file: c.file, typ: t, args: args, actual: actual})
	inst, _ := types.Instantiate(nil, t, actual, false)
	if inst == nil {
		return types.Typ[types.Invalid]
	}
	return inst
}

func (c *checker) importType(path *ast.Path) types.Type {
	if path == nil || len(path.Segments) == 0 {
		return types.Typ[types.Invalid]
	}
	res := c.file.References[path.ID()]
	parts := res.Path
	if !res.Deferred && res.Symbol != nil && len(res.Symbol.Target) > 0 {
		parts = res.Symbol.Target
	}
	if len(parts) < 2 {
		for _, s := range path.Segments {
			parts = append(parts, s.Name.Name)
		}
	}
	if len(parts) < 2 {
		return types.Typ[types.Invalid]
	}
	name := ""
	if res.Symbol != nil && res.Symbol.Kind != resolve.Import && res.Symbol.Kind != resolve.Module {
		name = res.Symbol.GoName
	}
	required := !res.Deferred && res.Symbol != nil && len(res.Symbol.Target) >= 2 && res.Symbol.Kind != resolve.Import && res.Symbol.Kind != resolve.Module
	return c.importedType(parts, name, required)
}

func (c *checker) importedType(parts []string, name string, required bool) types.Type {
	if len(parts) < 2 {
		return types.Typ[types.Invalid]
	}
	if name == "" {
		name = parts[len(parts)-1]
	}
	prefix := strings.Join(parts[:len(parts)-1], "/")
	if prefix == "std/sum" || prefix == "github.com/goxide-lang/std/sum" {
		if e := c.builtinEnum(name); e != nil {
			return e.Type
		}
	}
	if obj := c.importedObject(parts, name, required); obj != nil {
		return obj.Type()
	}
	return types.Typ[types.Invalid]
}

func (c *checker) importedObject(parts []string, name string, required bool) types.Object {
	if len(parts) < 2 {
		return nil
	}
	if name == "" {
		name = parts[len(parts)-1]
	}
	prefix := strings.Join(parts[:len(parts)-1], "/")
	p := c.imported[prefix]
	if p == nil {
		var err error
		p, err = c.importer.Import(prefix)
		if err != nil {
			if required {
				c.diag(c.file.AST, "go-import", fmt.Sprintf("import %q: %v", prefix, err))
			}
			return nil
		}
		c.imported[prefix] = p
	}
	return p.Scope().Lookup(name)
}

func substitute(t types.Type, sub map[*types.TypeParam]types.Type, info *Info) types.Type {
	return info.Substitute(t, sub, nil)
}
