package check

import (
	"fmt"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (c *checker) expr(node ast.Expr, expected types.Type) types.Type {
	if node == nil {
		return types.Typ[types.Invalid]
	}
	var t types.Type = types.Typ[types.Invalid]
	switch n := node.(type) {
	case *ast.Ident:
		if c.enumValidBuiltin(n) {
			c.diag(n, "enum-valid-call", "enum_valid must be called with an enum value")
		}
		c.recordConstExpression(n)
		t = c.symbolType(c.symbol(n))
		if !c.file.AST.NativeGo && n.Name == "nil" && t == types.Typ[types.UntypedNil] {
			if enum := c.out.EnumOf(expected); enum != nil {
				c.diag(n, EnumNilLiteral, fmt.Sprintf("nil is not a valid value of enum %s", enum.Enum.Name))
			}
		}
		if con := c.constructor(nil, c.symbol(n), expected); con != nil {
			c.out.Constructors[n] = con
			t = instanceType(con.Enum)
		}
	case *ast.PathExpr:
		if c.enumValidBuiltin(n) {
			c.diag(n, "enum-valid-call", "enum_valid must be called with an enum value")
		}
		c.recordConstExpression(n)
		if selected, handled := c.parameterizedMember(n, nil, expected, false, false); handled {
			if selected != nil {
				c.out.Members[n] = selected
				t = selected.Signature
			}
		} else if selected := c.out.Members[n]; selected != nil && selected.MethodExpr {
			t = selected.Signature
		} else if selected := c.staticMember(n); selected != nil {
			c.out.Members[n] = selected
			t = selected.Signature
		} else if con := c.constructor(n.Path, c.symbol(n), expected); con != nil {
			c.out.Constructors[n] = con
			t = instanceType(con.Enum)
		} else {
			t = c.pathType(n.Path)
		}
	case *ast.BoolLit:
		t = types.Typ[types.UntypedBool]
	case *ast.BasicLit:
		switch n.Kind {
		case token.INT:
			t = types.Typ[types.UntypedInt]
		case token.FLOAT:
			t = types.Typ[types.UntypedFloat]
		case token.IMAG:
			t = types.Typ[types.UntypedComplex]
		case token.CHAR:
			t = types.Typ[types.UntypedRune]
		case token.STRING:
			t = types.Typ[types.UntypedString]
		}
	case *ast.ParenExpr:
		t = c.exprSource(n.X, expected, c.expectedSources[n])
	case *ast.CallExpr:
		if result, ok := c.enumValid(n); ok {
			t = result
			break
		}
		if result, ok := c.unsafeCall(n); ok {
			t = result
			break
		}
		if keep, ok := c.out.Resolve.GoResults[n]; ok {
			t = c.goResult(n, keep)
			break
		}
		c.qualifiedCall(n)
		if c.equalityCall(n) {
			t = types.Typ[types.Bool]
			break
		}
		if _, print := c.out.Resolve.Prints[n]; print {
			for _, arg := range n.Args {
				c.expr(arg, nil)
			}
			t = types.NewTuple()
			break
		}
		funExpected := expected
		if c.traitOf(expected) != nil {
			funExpected = nil
		}
		var fun types.Type
		if c.expectedSources == nil {
			c.expectedSources = map[ast.Expr]*permission{}
		}
		c.expectedSources[n.Fun] = c.expectedSources[n]
		partial, explicit := c.partialFunctionApplication(n)
		if selected, handled := c.parameterizedMember(n.Fun, n.Args, expected, true, n.Ellipsis); handled {
			fun = types.Typ[types.Invalid]
			if selected != nil {
				c.out.Members[n.Fun] = selected
				c.out.Types[n.Fun] = selected.Signature
				fun = selected.Signature
			}
		} else if partial != nil {
			fun = partial
			c.out.Types[n.Fun] = partial
		} else {
			fun = c.expr(n.Fun, funExpected)
		}
		if con := c.out.Constructors[n.Fun]; con != nil {
			fields := con.Enum.Fields(con.Variant)
			for i, a := range n.Args {
				var want types.Type
				if i < len(fields) {
					want = fields[i].Type
				}
				c.expr(a, want)
			}
			c.out.Constructors[n] = con
			t = instanceType(con.Enum)
		} else if c.isTypeExpr(n.Fun) {
			for _, a := range n.Args {
				got := c.expr(a, fun)
				if valid(got) && valid(fun) && !types.ConvertibleTo(got, fun) {
					c.diag(a, "conversion", fmt.Sprintf("cannot convert %s to %s", got, fun))
				}
			}
			t = fun
		} else if sig, ok := fun.(*types.Signature); ok {
			sub := map[*types.TypeParam]types.Type{}
			for parameter, actual := range explicit {
				sub[parameter] = actual
			}
			constants := map[*ConstParam]ConstValue{}
			inferenceOK := true
			var formal *permission
			if c.referenceHints != nil {
				formal = c.referenceHints.sourceShape(n.Fun, map[*resolve.Symbol]bool{})
			}
			parameter := func(index int) types.Type {
				var want types.Type
				if index < sig.Params().Len() {
					want = sig.Params().At(index).Type()
				} else if sig.Variadic() && sig.Params().Len() > 0 {
					want = sig.Params().At(sig.Params().Len() - 1).Type()
				}
				if sig.Variadic() && index >= sig.Params().Len()-1 && !n.Ellipsis {
					if slice, ok := want.(*types.Slice); ok {
						want = slice.Elem()
					}
				}
				return c.out.Substitute(want, explicit, nil)
			}
			for i, a := range n.Args {
				want := parameter(i)
				var source *permission
				if formal != nil {
					source = c.referenceHints.callParam(formal, i, n.Ellipsis)
				}
				got := c.exprSource(a, want, source)
				_, call := referenceScrutinee(a).(*ast.CallExpr)
				if tuple, ok := got.(*types.Tuple); ok && tuple.Len() > 1 && call && len(n.Args) == 1 && !n.Ellipsis {
					for j := 0; j < tuple.Len(); j++ {
						inferenceOK = c.inferArrayArguments(parameter(j), tuple.At(j).Type(), sub, constants) && inferenceOK
					}
				} else if got != types.Typ[types.UntypedNil] {
					inferenceOK = c.inferArrayArguments(want, defaultType(got), sub, constants) && inferenceOK
				}
			}
			if c.functionGeneric(sig) {
				args := make([]types.Type, c.sourceArity(sig))
				complete := inferenceOK
				constArgs := make([]ConstValue, len(c.out.FunctionConstParams[sig]))
				for i, param := range c.out.FunctionConstParams[sig] {
					var found bool
					constArgs[i], found = constants[param]
					complete = complete && found
				}
				for i := range args {
					args[i] = sub[sig.TypeParams().At(i)]
					complete = complete && valid(args[i])
				}
				if !complete && len(c.out.FunctionConstParams[sig]) != 0 {
					c.diag(n, GenericConstraint, "cannot infer consistent const array arguments")
				} else if !complete && partial != nil {
					c.diag(n, GenericConstraint, "cannot infer complete trailing type arguments")
				}
				if complete {
					if inst, ok := c.instantiateFunctionType(sig, args, constArgs, n).(*types.Signature); ok {
						sig = inst
						if partial != nil {
							c.out.Types[n.Fun] = inst
						}
						for i, arg := range n.Args {
							index := i
							if sig.Variadic() && index >= sig.Params().Len() {
								index = sig.Params().Len() - 1
							}
							if index >= 0 && index < sig.Params().Len() && c.nilLiteral(arg) {
								want := sig.Params().At(index).Type()
								if sig.Variadic() && index == sig.Params().Len()-1 && !n.Ellipsis {
									want = want.(*types.Slice).Elem()
								}
								c.expr(arg, want)
							}
						}
					}
				}
			}
			t = tupleResult(sig.Results())
		} else {
			for _, a := range n.Args {
				c.expr(a, nil)
			}
			if symbol := c.symbol(referenceScrutinee(n.Fun)); symbol != nil && symbol.Kind == resolve.Builtin {
				switch symbol.Name.Name {
				case "append":
					if len(n.Args) == 0 {
						c.diag(n, "builtin-call", "not enough arguments in call to append")
					} else if slice, ok := c.out.Types[n.Args[0]].Underlying().(*types.Slice); ok && !n.Ellipsis {
						for _, arg := range n.Args[1:] {
							c.expr(arg, slice.Elem())
						}
					}
				case "copy":
					if n.Ellipsis {
						c.diag(n, "builtin-call", "invalid use of ... with built-in copy")
					} else if len(n.Args) < 2 {
						c.diag(n, "builtin-call", "not enough arguments in call to copy")
					} else if len(n.Args) > 2 {
						c.diag(n, "builtin-call", "too many arguments in call to copy")
					}
				}
			}
			if name := calleeName(n.Fun); name != "" {
				switch name {
				case "len", "cap", "copy":
					t = types.Typ[types.Int]
				case "append":
					if len(n.Args) > 0 {
						t = c.out.Types[n.Args[0]]
					}
				case "new":
					if len(n.Args) > 0 {
						t = types.NewPointer(c.exprType(n.Args[0]))
					}
				case "make":
					if len(n.Args) > 0 {
						t = c.exprType(n.Args[0])
					}
				case "recover":
					t = anyType
				default:
					if c.isTypeExpr(n.Fun) {
						t = fun
					}
				}
			}
		}
	case *ast.CompositeLit:
		if pt, ok := n.Type.(*ast.PathType); ok {
			if con := c.constructor(pt.Path, c.symbol(pt), expected); con != nil {
				c.out.Constructors[n] = con
				t = instanceType(con.Enum)
			}
		}
		if !valid(t) {
			if array, ok := n.Type.(*ast.ArrayType); ok && array.Ellipsis {
				t = c.inferredArrayLiteral(array, n.Elts)
			} else {
				t = c.typ(n.Type)
			}
			if n.Type == nil && valid(expected) {
				t = expected
			}
		}
		for i, elt := range n.Elts {
			want, keyType := c.literalType(t, i, "")
			if kv, ok := elt.(*ast.KeyValue); ok {
				if id, ok := kv.Key.(*ast.Ident); ok {
					want, _ = c.literalType(t, i, id.Name)
				}
				if con := c.out.Constructors[n]; con != nil {
					if id, ok := kv.Key.(*ast.Ident); ok {
						for _, field := range con.Enum.Fields(con.Variant) {
							if field.Name == id.Name {
								want = field.Type
							}
						}
					}
				}
				if keyType != nil {
					c.expr(kv.Key, keyType)
				}
				c.expr(kv.Value, want)
			} else {
				if con := c.out.Constructors[n]; con != nil {
					fields := con.Enum.Fields(con.Variant)
					if i < len(fields) {
						want = fields[i].Type
					}
				}
				c.expr(elt, want)
			}
		}
	case *ast.KeyValue:
		t = c.expr(n.Value, expected)
	case *ast.SelectorExpr:
		x := c.expr(n.X, nil)
		if c.isTypeExpr(n.X) {
			if !n.Static {
				c.diag(n, "method-expression", "method expressions require a static :: selector")
				break
			}
			if object := c.lookupGoMember(x, false, n.Sel.Name); object != nil {
				if function, ok := object.(*types.Func); ok {
					signature := function.Type().(*types.Signature)
					params := []*types.Var{types.NewVar(0, c.pkg, "", x)}
					for i := 0; i < signature.Params().Len(); i++ {
						params = append(params, types.NewVar(0, c.pkg, "", signature.Params().At(i).Type()))
					}
					t = types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), signature.Results(), signature.Variadic())
				}
			}
			if !valid(t) {
				c.diag(n, "method-expression", "type does not have the selected method")
			}
			break
		}
		if selected, handled := c.parameterizedMember(n, nil, expected, false, false); handled {
			if selected != nil {
				c.out.Members[n] = selected
				t = selected.Signature
			}
			break
		}
		if selected := c.instanceMember(n, n.X, x, n.Sel.Name, nil); selected != nil {
			if selected.Member.Native == nil {
				c.out.Members[n] = selected
			}
			t = selected.Signature
			break
		}
		if valid(x) {
			if obj := c.lookupGoMember(x, true, n.Sel.Name); obj != nil {
				t = obj.Type()
			}
		}
	case *ast.IndexExpr:
		x := c.expr(n.X, nil)
		if sig, ok := x.(*types.Signature); ok && c.functionGeneric(sig) {
			if len(c.out.FunctionConstParams[sig]) != 0 {
				t = c.explicitIndexArguments(sig, n)
				break
			}
			args := make([]types.Type, len(n.Indices))
			for i, a := range n.Indices {
				args[i] = c.genericExprType(a)
			}
			t = c.instantiateFunctionType(sig, args, nil, n)
		} else if named, ok := x.(*types.Named); ok && named.TypeParams().Len() > 0 && c.isTypeExpr(n.X) {
			args := make([]types.Type, len(n.Indices))
			for i, a := range n.Indices {
				args[i] = c.genericExprType(a)
			}
			if inst, err := c.instantiateGo(named, args); err == nil {
				t = inst
			} else {
				c.diag(n, GenericConstraint, err.Error())
			}
		} else {
			for _, i := range n.Indices {
				c.expr(i, nil)
			}
			if valid(x) {
				if array, ok := c.out.ArrayOf(x); ok {
					t = array.Elem
					break
				}
				switch v := x.Underlying().(type) {
				case *types.Array:
					t = v.Elem()
				case *types.Slice:
					t = v.Elem()
				case *types.Map:
					t = v.Elem()
				case *types.Pointer:
					if a, ok := c.out.ArrayOf(v.Elem()); ok {
						t = a.Elem
					}
				case *types.Basic:
					if v.Info()&types.IsString != 0 {
						t = types.Typ[types.Byte]
					}
				}
			}
		}
	case *ast.SliceExpr:
		x := c.expr(n.X, nil)
		c.expr(n.Low, nil)
		c.expr(n.High, nil)
		c.expr(n.Max, nil)
		t = x
		if valid(x) {
			if array, ok := c.out.ArrayOf(x); ok {
				t = types.NewSlice(array.Elem)
				break
			}
			switch v := x.Underlying().(type) {
			case *types.Array:
				t = types.NewSlice(v.Elem())
			case *types.Pointer:
				if a, ok := c.out.ArrayOf(v.Elem()); ok {
					t = types.NewSlice(a.Elem)
				}
			}
		}
	case *ast.TypeAssertExpr:
		c.expr(n.X, nil)
		t = c.typ(n.Type)
	case *ast.StarExpr:
		if c.isTypeExpr(n.X) {
			t = types.NewPointer(c.exprType(n.X))
		} else if p, ok := c.expr(n.X, nil).Underlying().(*types.Pointer); ok {
			t = p.Elem()
		}
	case *ast.UnaryExpr:
		x := c.expr(n.X, expected)
		t = x
		switch n.Op {
		case token.AND:
			t = types.NewPointer(x)
		case token.ARROW:
			if ch, ok := x.Underlying().(*types.Chan); ok {
				t = ch.Elem()
			}
		}
	case *ast.BinaryExpr:
		leftWant, rightWant := expected, expected
		comparison := false
		if !valid(expected) || types.IsInterface(expected) {
			leftWant, rightWant = nil, nil
		}
		switch n.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			leftWant, rightWant = nil, nil
			comparison = true
		case token.SHL, token.SHR:
			rightWant = nil
		}
		x := c.expr(n.X, leftWant)
		if rightWant == nil && n.Op != token.SHL && n.Op != token.SHR && valid(x) && !untyped(x) && (!comparison || !c.nilLiteral(n.Y)) {
			rightWant = x
		}
		y := c.expr(n.Y, rightWant)
		if untyped(x) && valid(y) && !untyped(y) && n.Op != token.SHL && n.Op != token.SHR && (!comparison || !c.nilLiteral(n.X)) {
			x = c.expr(n.X, y)
		}
		t = x
		if !valid(t) {
			t = y
		}
		switch n.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			t = types.Typ[types.UntypedBool]
		}
	case *ast.FuncLit:
		params, results := c.tuple(n.Params), c.tuple(n.Results)
		oldSources := c.returnSources
		c.returnSources = c.newPermissionChecker().functionType(nil, n.Results).results
		if n.Result != nil {
			results = types.NewTuple(types.NewVar(0, c.pkg, "", c.typ(n.Result)))
			c.returnSources = []*permission{c.newPermissionChecker().astType(n.Result, nil)}
		}
		old := c.returns
		c.returns = tupleTypes(results)
		c.block(n.Body)
		if n.Expr != nil {
			var want types.Type
			if len(c.returns) == 1 {
				want = c.returns[0]
			}
			var source *permission
			if len(c.returnSources) == 1 {
				source = c.returnSources[0]
			}
			ret := c.exprSource(n.Expr, want, source)
			if results.Len() == 0 {
				results = types.NewTuple(types.NewVar(0, c.pkg, "", defaultType(ret)))
			}
		}
		c.returns = old
		c.returnSources = oldSources
		t = types.NewSignatureType(nil, nil, nil, params, results, len(n.Params) > 0 && n.Params[len(n.Params)-1].Variadic)
	case *ast.MatchExpr:
		t = c.match(n, expected, false)
	case *ast.TryExpr:
		t = c.try(n)
	case *ast.RefExpr:
		t = types.NewPointer(c.expr(n.X, nil))
	case *ast.UnsafeExpr:
		t = c.expr(n.X, expected)
	case *ast.AwaitExpr:
		t = c.expr(n.X, expected)
	case *ast.TypeExpr:
		t = c.typ(n.Type)
	case *ast.QualifiedSelectorExpr:
		if selected, handled := c.parameterizedMember(n, nil, expected, false, false); handled {
			if selected != nil {
				c.out.Members[n] = selected
				t = selected.Signature
			}
			break
		}
		x := c.expr(n.X, nil)
		var trait *Trait
		if path, ok := n.Trait.(*ast.PathType); ok && path.Path != nil && len(path.Path.Segments) > 0 {
			trait = c.traitPath(path.Path.Segments)
		}
		if trait == nil {
			c.diag(n, TraitInvalid, "qualified call requires a trait")
		} else if selected := c.instanceMember(n, n.X, x, n.Sel.Name, trait); selected != nil {
			c.out.Members[n] = selected
			t = selected.Signature
		}
	}
	if t == nil {
		t = types.Typ[types.Invalid]
	}
	if untyped(t) && valid(expected) && !types.IsInterface(expected) && types.AssignableTo(t, expected) {
		t = expected
	}
	t = c.adaptTrait(node, t, expected)
	c.out.Types[node] = t
	return t
}

func (c *checker) nilLiteral(e ast.Expr) bool {
	id, ok := referenceScrutinee(e).(*ast.Ident)
	return ok && id.Name == "nil" && c.symbolType(c.symbol(id)) == types.Typ[types.UntypedNil]
}

func (c *checker) literalType(t types.Type, index int, name string) (types.Type, types.Type) {
	if !valid(t) {
		return nil, nil
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	if array, ok := c.out.ArrayOf(t); ok {
		return array.Elem, types.Typ[types.Int]
	}
	switch t := t.Underlying().(type) {
	case *types.Array:
		return t.Elem(), types.Typ[types.Int]
	case *types.Slice:
		return t.Elem(), types.Typ[types.Int]
	case *types.Map:
		return t.Elem(), t.Key()
	case *types.Struct:
		if name != "" {
			for i := 0; i < t.NumFields(); i++ {
				if t.Field(i).Name() == name {
					return t.Field(i).Type(), nil
				}
			}
		} else if index < t.NumFields() {
			return t.Field(index).Type(), nil
		}
	}
	return nil, nil
}

func (c *checker) constructor(path *ast.Path, s *resolve.Symbol, expected types.Type) *Constructor {
	var base *Constructor
	var sourceType types.Type
	if path != nil && len(path.Segments) == 2 && path.Segments[0].Name.Name == "Self" {
		target := c.currentSelf
		if target == nil && s != nil {
			for decl, impl := range c.out.Impls {
				if s.Decl.File == impl.File && s.Decl.ID == decl.ID() {
					target = impl.Target
					break
				}
			}
		}
		if instance := c.out.EnumOf(target); instance != nil {
			if variant := instance.Variant(path.Segments[len(path.Segments)-1].Name.Name); variant != nil {
				base = &Constructor{Enum: instance, Variant: variant}
			}
		}
	}
	if s != nil {
		if variant := c.variants[s]; variant != nil {
			base = variant
		}
	}
	if path != nil && len(path.Segments) > 1 {
		prefix := path.Segments[:len(path.Segments)-1]
		sourceType = c.symbolType(c.sourcePathSymbol(prefix))
		if !valid(sourceType) {
			resolved := c.file.References[prefix[len(prefix)-1].Name.ID()]
			if !resolved.Deferred && resolved.Symbol != nil && (resolved.Symbol.Kind == resolve.Type || resolved.Symbol.Kind == resolve.Enum) {
				sourceType = c.symbolType(resolved.Symbol)
			}
		}
		if instance := c.out.EnumOf(sourceType); base == nil && instance != nil {
			if variant := instance.Variant(path.Segments[len(path.Segments)-1].Name.Name); variant != nil {
				base = &Constructor{Enum: instance, Variant: variant}
			}
		}
	}
	if base == nil && s != nil && s.Kind == resolve.Variant && s.Decl.File == "" && len(s.Target) == 0 {
		for _, e := range c.builtins {
			i := c.out.EnumOf(e.Type)
			if v := i.Variant(s.Name.Name); v != nil {
				base = &Constructor{Enum: i, Variant: v}
				break
			}
		}
	}
	if base == nil {
		return nil
	}
	if !c.enumAccessible(base.Enum) {
		c.diag(path, VariantMismatch, "internal-origin alias cannot expose enum variants outside its internal package boundary")
		return nil
	}
	instance := base.Enum
	if !valid(sourceType) {
		sourceType = base.Enum.Enum.Type
	}
	inferArguments := false
	switch source := sourceType.(type) {
	case *types.Named:
		inferArguments = source.TypeParams().Len() > 0 && source.TypeArgs().Len() == 0
	case *types.Alias:
		inferArguments = source.TypeParams().Len() > 0 && source.TypeArgs().Len() == 0
	}
	if want := c.out.EnumOf(expected); inferArguments && want != nil && want.Enum == instance.Enum {
		instance = want
	}
	if path != nil {
		for _, seg := range path.Segments {
			if seg.Args != nil {
				instance = c.out.EnumOf(c.instantiate(sourceType, seg.Args))
				break
			}
		}
	}
	if instance == nil {
		return nil
	}
	return &Constructor{Enum: instance, Variant: base.Variant}
}

func instanceType(i *Instance) types.Type {
	if len(i.Args) == 0 {
		return i.Enum.Type
	}
	t, err := types.Instantiate(nil, i.Enum.Type, i.Args, false)
	if err != nil {
		return i.Enum.Type
	}
	return t
}

func tupleResult(t *types.Tuple) types.Type {
	if t.Len() == 1 {
		return t.At(0).Type()
	}
	return t
}
func tupleTypes(t *types.Tuple) []types.Type {
	var out []types.Type
	for i := 0; i < t.Len(); i++ {
		out = append(out, t.At(i).Type())
	}
	return out
}

func (c *checker) exprType(e ast.Expr) types.Type {
	if t, ok := e.(*ast.TypeExpr); ok {
		return c.typ(t.Type)
	}
	return c.expr(e, nil)
}

func (c *checker) isTypeExpr(e ast.Expr) bool {
	if c.out.Members[e] != nil {
		return false
	}
	switch n := e.(type) {
	case *ast.TypeExpr:
		return true
	case *ast.Ident:
		s := c.symbol(n)
		return s != nil && (s.Kind == resolve.Type || s.Kind == resolve.Enum || s.Kind == resolve.TypeParam)
	case *ast.PathExpr:
		s := c.symbol(n)
		return s != nil && (s.Kind == resolve.Type || s.Kind == resolve.Enum || s.Kind == resolve.TypeParam)
	case *ast.IndexExpr:
		return c.isTypeExpr(n.X)
	case *ast.ParenExpr:
		return c.isTypeExpr(n.X)
	case *ast.StarExpr:
		return c.isTypeExpr(n.X)
	}
	return false
}

func calleeName(e ast.Expr) string {
	switch n := e.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.PathExpr:
		if len(n.Path.Segments) > 0 {
			return n.Path.Segments[len(n.Path.Segments)-1].Name.Name
		}
	}
	return ""
}
