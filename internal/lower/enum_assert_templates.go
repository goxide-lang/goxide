package lower

import (
	goast "go/ast"
	"go/types"
	"sort"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// A conditional body can inspect a concrete enum whose payload belongs to its
// caller. Pass the entire finite assertion as a typed operation, so recursive
// paths have exactly the same stopping point as a concrete inline assertion.
type enumAssertionBodyKey struct {
	declaration *syntax.FuncDecl
	branch      *check.BodyCase
}

func (p *traitProgram) prepareEnumAssertions(info *check.Info) {
	p.enumChecks = map[enumAssertionBodyKey][]types.Type{}
	type body struct {
		key  enumAssertionBodyKey
		info *check.Info
	}
	var bodies []body
	files := make([]string, 0, len(info.Resolve.Files))
	for name := range info.Resolve.Files {
		files = append(files, name)
	}
	sort.Strings(files)
	for _, name := range files {
		syntax.Walk(info.Resolve.Files[name].AST, func(node syntax.Node) bool {
			if declaration, ok := node.(*syntax.FuncDecl); ok && declaration.Body != nil {
				bodies = append(bodies, body{enumAssertionBodyKey{declaration, nil}, info})
				branches := append([]*check.BodyCase(nil), info.BodyCases[declaration]...)
				sort.Slice(branches, func(i, j int) bool { return branches[i].Key < branches[j].Key })
				for _, branch := range branches {
					bodies = append(bodies, body{enumAssertionBodyKey{declaration, branch}, branch.Info})
				}
			}
			return true
		})
	}
	// Keep substitutions on edges: Box[T] in a callee may be Box[[]U] in its
	// caller. A concrete request needs no callback in the enclosing declaration.
	type edge struct {
		target     enumAssertionBodyKey
		substitute func(types.Type) types.Type
	}
	edges := map[enumAssertionBodyKey][]edge{}
	for _, body := range bodies {
		current := body.info
		syntax.Walk(body.key.declaration.Body, func(node syntax.Node) bool {
			if call, ok := node.(*syntax.CallExpr); ok {
				if typ := current.EnumAssertions[call]; typ != nil && inferenceHasParameter(typ) {
					p.enumChecks[body.key] = appendAssertionType(p.enumChecks[body.key], typ)
				}
			}
			if expression, ok := node.(syntax.Expr); ok {
				if selection := current.Members[expression]; selection != nil && selection.Impl != nil && parameterizedImpl(implTemplate(selection.Impl)) {
					member := selectedMember(selection)
					impl := member.Impl
					edges[body.key] = append(edges[body.key], edge{enumAssertionBodyKey{member.Decl, memberCase(member)}, func(typ types.Type) types.Type {
						return substituteEnumCheck(p.typeState, current, typ, impl.Substitution, impl.ConstSubstitution)
					}})
				}
			}
			if instance := current.FunctionInstances[node]; instance != nil {
				if function := p.functions[instance.Signature]; function != nil {
					edges[body.key] = append(edges[body.key], edge{enumAssertionBodyKey{function.decl, instance.Case}, func(typ types.Type) types.Type {
						return substituteEnumCheck(p.typeState, current, typ, instance.Substitution, instance.ConstSubstitution)
					}})
				}
			}
			return true
		})
	}
	// Expand only finite substitution states. An expanding recursion is left to
	// the existing Go generic-definition validator; building an unbounded list of
	// validator parameters first would prevent its instantiation-cycle diagnostic.
	parameters := map[enumAssertionBodyKey][]*types.TypeParam{}
	for signature, function := range p.functions {
		key := enumAssertionBodyKey{declaration: function.decl}
		for index := 0; index < signature.TypeParams().Len(); index++ {
			parameters[key] = append(parameters[key], signature.TypeParams().At(index))
		}
	}
	for _, impl := range info.Impls {
		for _, member := range impl.Methods {
			parameters[enumAssertionBodyKey{declaration: member.Decl}] = impl.TypeParams
		}
	}
	for _, body := range bodies {
		if body.key.branch != nil {
			parameters[body.key] = body.key.branch.TypeParams
		}
	}
	direct := p.enumChecks
	p.enumChecks = map[enumAssertionBodyKey][]types.Type{}
	type state struct {
		key       enumAssertionBodyKey
		arguments []types.Type
	}
	for _, root := range bodies {
		var walk func(enumAssertionBodyKey, func(types.Type) types.Type, []state)
		walk = func(key enumAssertionBodyKey, substitute func(types.Type) types.Type, path []state) {
			for _, required := range direct[key] {
				typ := substitute(required)
				if inferenceHasParameter(typ) {
					p.enumChecks[root.key] = appendAssertionType(p.enumChecks[root.key], typ)
				}
			}
			var arguments []types.Type
			for _, parameter := range parameters[key] {
				arguments = append(arguments, substitute(parameter))
			}
			for _, previous := range path {
				if previous.key != key || len(previous.arguments) != len(arguments) {
					continue
				}
				same := true
				for index, argument := range arguments {
					same = same && types.Identical(argument, previous.arguments[index])
					if assertionTypeContains(argument, previous.arguments[index]) {
						return
					}
				}
				if same {
					return
				}
			}
			path = append(append([]state(nil), path...), state{key, arguments})
			for _, edge := range edges[key] {
				walk(edge.target, func(typ types.Type) types.Type { return substitute(edge.substitute(typ)) }, path)
			}
		}
		walk(root.key, func(typ types.Type) types.Type { return typ }, nil)
	}

	for key, checks := range p.enumChecks {
		if len(checks) != 0 && p.templateFunctions[key.declaration] {
			p.legacyFunctions[key.declaration] = false
			p.helperFunctions[key.declaration] = true
		}
	}
}

func appendAssertionType(list []types.Type, typ types.Type) []types.Type {
	if !assertionSeen(list, typ) {
		return append(list, typ)
	}
	return list
}

func (e *emitter) memberEnumChecks(member *check.Member) []types.Type {
	key := enumAssertionBodyKey{member.Decl, memberCase(member)}
	var out []types.Type
	for _, typ := range e.traits.enumChecks[key] {
		out = append(out, substituteEnumCheck(e.typeState, e.info, typ, member.Impl.Substitution, member.Impl.ConstSubstitution))
	}
	return out
}

func (e *emitter) viewEnumChecks(impl *check.Impl) []types.Type {
	var requirements []types.Type
	for _, member := range impl.Methods {
		copy := *member
		copy.Impl = impl
		for _, typ := range e.traits.enumChecks[enumAssertionBodyKey{member.Decl, memberCase(&copy)}] {
			requirements = appendAssertionType(requirements, typ)
		}
	}
	var out []types.Type
	for _, typ := range requirements {
		out = append(out, substituteEnumCheck(e.typeState, e.info, typ, impl.Substitution, impl.ConstSubstitution))
	}
	return out
}

func (e *emitter) functionEnumChecks(function *traitFunction, instance *check.FunctionInstance) []types.Type {
	if len(e.info.GenericBounds[function.decl]) == 0 && (instance == nil || instance.Case == nil || len(instance.Case.Requirements) == 0) {
		return nil
	}
	key := enumAssertionBodyKey{declaration: function.decl}
	if instance != nil {
		key.branch = instance.Case
	}
	var out []types.Type
	for _, typ := range e.traits.enumChecks[key] {
		if instance != nil {
			typ = substituteEnumCheck(e.typeState, e.info, typ, instance.Substitution, instance.ConstSubstitution)
		}
		out = append(out, typ)
	}
	return out
}

func (e *emitter) templateEnumChecks(required []types.Type, parameters *goast.FieldList) func() {
	previous := e.enumCheckCallbacks
	e.enumCheckCallbacks = map[types.Type]goast.Expr{}
	for _, typ := range required {
		name := e.fresh("enumCheck")
		e.enumCheckCallbacks[typ] = name
		parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{name}, Type: e.goType(signatureOf([]types.Type{typ}, nil))})
	}
	return func() { e.enumCheckCallbacks = previous }
}

func (e *emitter) enumCheckCallback(typ types.Type) goast.Expr {
	for candidate, callback := range e.enumCheckCallbacks {
		if types.Identical(candidate, typ) {
			return e.callbackReference(callback)
		}
	}
	return nil
}

func (e *emitter) enumCheckArguments(required []types.Type) []goast.Expr {
	var out []goast.Expr
	for _, typ := range required {
		out = append(out, e.enumAssertionFunction(typ, e.tree))
	}
	return out
}

func (p *traitInference) enumChecks(required []types.Type) ([]goast.Expr, []types.Type) {
	var values []goast.Expr
	var signatures []types.Type
	for _, typ := range required {
		signature := signatureOf([]types.Type{typ}, nil)
		values = append(values, p.parameter(p.e.enumAssertionFunction(typ, p.e.tree), signature))
		signatures = append(signatures, signature)
	}
	return values, signatures
}

// A strictly containing argument on the same recursive parameter position is
// the growing type dependency rejected by Go's instantiation-cycle check.
func assertionTypeContains(container, want types.Type) bool {
	container = types.Unalias(container)
	var children []types.Type
	switch typ := container.(type) {
	case *types.Named:
		for index := 0; index < typ.TypeArgs().Len(); index++ {
			children = append(children, typ.TypeArgs().At(index))
		}
	case *types.Pointer:
		children = append(children, typ.Elem())
	case *types.Slice:
		children = append(children, typ.Elem())
	case *types.Array:
		children = append(children, typ.Elem())
	case *types.Map:
		children = append(children, typ.Key(), typ.Elem())
	case *types.Chan:
		children = append(children, typ.Elem())
	case *types.Struct:
		for index := 0; index < typ.NumFields(); index++ {
			children = append(children, typ.Field(index).Type())
		}
	case *types.Signature:
		children = append(children, typ.Params(), typ.Results())
	case *types.Tuple:
		if typ != nil {
			for index := 0; index < typ.Len(); index++ {
				children = append(children, typ.At(index).Type())
			}
		}
	}
	for _, child := range children {
		if types.Identical(child, want) || assertionTypeContains(child, want) {
			return true
		}
	}
	return false
}

// Recursive calls substitute simultaneously: A->B, B->A swaps parameters;
// it must not chase replacements back into the caller's binders. Isolating the
// callee binders also retains arrays and associated-type substitution support.
func substituteEnumCheck(state *check.TypeState, info *check.Info, typ types.Type, substitution map[*types.TypeParam]types.Type, constants map[*check.ConstParam]check.ConstValue) types.Type {
	if len(substitution) == 0 {
		return state.Substitute(info, typ, nil, constants)
	}
	isolated := map[*types.TypeParam]types.Type{}
	arguments := map[*types.TypeParam]types.Type{}
	for parameter, argument := range substitution {
		fresh := types.NewTypeParam(types.NewTypeName(parameter.Obj().Pos(), parameter.Obj().Pkg(), parameter.Obj().Name(), nil), parameter.Constraint())
		isolated[parameter] = fresh
		arguments[fresh] = argument
	}
	return state.Substitute(info, state.Substitute(info, typ, isolated, constants), arguments, nil)
}
