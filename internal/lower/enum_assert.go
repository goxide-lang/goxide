package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type enumAssertionHelper struct {
	typ         *types.Named
	seen        []types.Type
	file        string
	name        string
	body        *goast.BlockStmt
	value       *goast.Ident
	calls       []*enumAssertionCall
	callbacks   []*enumAssertionHelper
	names       map[*enumAssertionHelper]*goast.Ident
	signature   *types.Signature
	opaque      []types.Type
	typeNames   map[types.Type]*goast.Ident
	constraints map[types.Type][]types.Type
}

type enumAssertionCall struct {
	helper *enumAssertionHelper
	caller *emitter
	within *enumAssertionHelper
	call   *goast.CallExpr
}

type enumAssertionPlan struct {
	helpers []*enumAssertionHelper
	calls   []*enumAssertionCall
	values  []*enumAssertionValue
}

type enumAssertionValue struct {
	helper *enumAssertionHelper
	caller *emitter
	value  *goast.ParenExpr
}

func (e *emitter) enumAssertionFunction(typ types.Type, node syntax.Node) goast.Expr {
	if callback := e.enumCheckCallback(typ); callback != nil {
		return callback
	}
	if named, ok := types.Unalias(typ).(*types.Named); ok && e.enumAssertionNeedsHelper(named, nil) {
		helper := e.enumAssertionHelper(named, nil, node)
		value := &goast.ParenExpr{X: goast.NewIdent("invalidEnumAssertion")}
		if helper != nil {
			e.enumAssertions.values = append(e.enumAssertions.values, &enumAssertionValue{helper, e, value})
		}
		return value
	}
	value := e.fresh("enum")
	return &goast.FuncLit{Type: &goast.FuncType{Params: &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{value}, Type: e.goType(typ)}}}}, Body: &goast.BlockStmt{List: e.enumAssertionBody(typ, value, nil, node, nil, false)}}
}

func (e *emitter) enumAssert(call *syntax.CallExpr, typ types.Type) goast.Expr {
	argument := e.expr(call.Args[0])
	if callback := e.enumCheckCallback(typ); callback != nil {
		return &goast.CallExpr{Fun: callback, Args: []goast.Expr{argument}}
	}
	if named, ok := types.Unalias(typ).(*types.Named); ok && e.enumAssertionNeedsHelper(named, nil) {
		return e.enumAssertionInvoke(named, argument, nil, call, nil)
	}
	value := e.fresh("enum")
	function := &goast.FuncLit{
		Type: &goast.FuncType{Params: &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{value}, Type: e.goType(typ)}}}},
		Body: &goast.BlockStmt{List: e.enumAssertionBody(typ, value, nil, call, nil, false)},
	}
	// Evaluate in the caller before entering the validation frame.
	return &goast.CallExpr{Fun: function, Args: []goast.Expr{argument}}
}

func assertionSeen(seen []types.Type, typ types.Type) bool {
	for _, previous := range seen {
		if types.Identical(previous, typ) {
			return true
		}
	}
	return false
}

func (e *emitter) enumAssertionContains(typ types.Type, seen []types.Type) bool {
	typ = types.Unalias(typ)
	if assertionSeen(seen, typ) {
		return false
	}
	if e.info.EnumOf(typ) != nil {
		return true
	}
	seen = append(append([]types.Type(nil), seen...), typ)
	if array, ok := e.typeState.ArrayOf(e.info, typ); ok {
		return e.enumAssertionContains(array.Elem, seen)
	}
	if named, ok := typ.(*types.Named); ok {
		typ = named.Underlying()
	}
	switch typ := typ.(type) {
	case *types.TypeParam:
		return true
	case *types.Struct:
		for index := 0; index < typ.NumFields(); index++ {
			if e.enumAssertionContains(typ.Field(index).Type(), seen) {
				return true
			}
		}
	case *types.Slice:
		return e.enumAssertionContains(typ.Elem(), seen)
	case *types.Map:
		return e.enumAssertionContains(typ.Elem(), seen)
	}
	return false
}

func (e *emitter) enumAssertionNeedsHelper(typ *types.Named, seen []types.Type) bool {
	return !e.nameableType(typ) || e.enumAssertionPrivate(typ, seen)
}

// Only fields actually traversed require access to their defining package.
func (e *emitter) enumAssertionPrivate(typ *types.Named, seen []types.Type) bool {
	if e.localTypePackage(typ.Obj().Pkg()) {
		return false
	}
	if !typ.Obj().Exported() && e.exportedTypeAlias(typ) == nil {
		return true
	}
	if instance := e.info.EnumOf(typ); instance != nil {
		if instance.Enum.Builtin {
			return false
		}
		for _, variant := range instance.Enum.Variants {
			if !gotoken.IsExported(variant.GoName) {
				return true
			}
			for _, field := range e.typeState.Fields(instance, variant) {
				if !gotoken.IsExported(field.GoName) && e.enumAssertionContains(field.Type, append(seen, typ)) {
					return true
				}
			}
		}
	}
	if record, ok := typ.Underlying().(*types.Struct); ok {
		for index := 0; index < record.NumFields(); index++ {
			field := record.Field(index)
			if !field.Exported() && e.enumAssertionContains(field.Type(), append(seen, typ)) {
				return true
			}
		}
	}
	return false
}

func (e *emitter) enumAssertionBody(typ types.Type, value goast.Expr, seen []types.Type, node syntax.Node, within *enumAssertionHelper, root bool) []goast.Stmt {
	typ = types.Unalias(typ)
	if !e.enumAssertionContains(typ, seen) {
		return nil
	}
	if named, ok := typ.(*types.Named); ok && !root && e.enumAssertionNeedsHelper(named, seen) {
		return []goast.Stmt{&goast.ExprStmt{X: e.enumAssertionInvoke(named, value, seen, node, within)}}
	}
	seen = append(append([]types.Type(nil), seen...), typ)
	if instance := e.info.EnumOf(typ); instance != nil {
		invalid := func() goast.Stmt {
			return &goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote("goxide: invalid enum " + instance.Enum.Name)}}}}
		}
		if len(instance.Enum.Variants) == 0 {
			return []goast.Stmt{invalid()}
		}
		active := e.fresh("variant")
		cases := &goast.BlockStmt{}
		for _, variant := range instance.Enum.Variants {
			clause := &goast.CaseClause{}
			if instance.Enum.Builtin {
				clause.List = []goast.Expr{integer(variantTag(variant.Name))}
			} else {
				variantType := &goast.StarExpr{X: e.variantType(instance, variant)}
				zero := e.fresh("nilVariant")
				clause.List = []goast.Expr{variantType}
				clause.Body = []goast.Stmt{varStmt(zero, variantType, nil), &goast.IfStmt{Cond: &goast.BinaryExpr{X: active, Op: gotoken.EQL, Y: zero}, Body: &goast.BlockStmt{List: []goast.Stmt{invalid()}}}}
			}
			for _, field := range e.typeState.Fields(instance, variant) {
				var member goast.Expr = &goast.SelectorExpr{X: active, Sel: goast.NewIdent(field.GoName)}
				if instance.Enum.Builtin {
					member = &goast.CallExpr{Fun: e.sumMember("Value" + variant.Name), Args: []goast.Expr{value}}
				}
				clause.Body = append(clause.Body, e.enumAssertionBody(field.Type, member, seen, node, within, false)...)
			}
			cases.List = append(cases.List, clause)
		}
		cases.List = append(cases.List, &goast.CaseClause{Body: []goast.Stmt{invalid()}})
		if instance.Enum.Builtin {
			return []goast.Stmt{&goast.SwitchStmt{Tag: &goast.CallExpr{Fun: e.sumMember("Tag" + instance.Enum.Name), Args: []goast.Expr{value}}, Body: cases}}
		}
		return []goast.Stmt{&goast.TypeSwitchStmt{Assign: &goast.AssignStmt{Lhs: []goast.Expr{active}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.TypeAssertExpr{X: value}}}, Body: cases}}
	}
	var element types.Type
	if array, ok := e.typeState.ArrayOf(e.info, typ); ok {
		element = array.Elem
	} else {
		if named, ok := typ.(*types.Named); ok {
			typ = named.Underlying()
		}
		switch typ := typ.(type) {
		case *types.TypeParam:
			if e.validation == nil {
				e.fail(node, "enum_assert requires statically determined payload types")
			}
		case *types.Struct:
			var body []goast.Stmt
			for index := 0; index < typ.NumFields(); index++ {
				field := typ.Field(index)
				member := &goast.SelectorExpr{X: value, Sel: goast.NewIdent(e.typeState.GoMemberName(e.info, field))}
				body = append(body, e.enumAssertionBody(field.Type(), member, seen, node, within, false)...)
			}
			return body
		case *types.Slice:
			element = typ.Elem()
		case *types.Map:
			element = typ.Elem()
		}
	}
	if element != nil {
		item := e.fresh("item")
		if body := e.enumAssertionBody(element, item, seen, node, within, false); len(body) != 0 {
			return []goast.Stmt{&goast.RangeStmt{Key: goast.NewIdent("_"), Value: item, Tok: gotoken.DEFINE, X: value, Body: &goast.BlockStmt{List: body}}}
		}
	}
	return nil
}

func (e *emitter) enumAssertionInvoke(typ *types.Named, value goast.Expr, seen []types.Type, node syntax.Node, within *enumAssertionHelper) goast.Expr {
	helper := e.enumAssertionHelper(typ, seen, node)
	call := &goast.CallExpr{Args: []goast.Expr{value}}
	if helper == nil {
		call.Fun = goast.NewIdent("invalidEnumAssertion")
		return call
	}
	call.Fun = goast.NewIdent(helper.name)
	invocation := &enumAssertionCall{helper: helper, caller: e, within: within, call: call}
	e.enumAssertions.calls = append(e.enumAssertions.calls, invocation)
	if within != nil {
		within.calls = append(within.calls, invocation)
	}
	return call
}

func (e *emitter) enumAssertionHelper(typ *types.Named, seen []types.Type, node syntax.Node) *enumAssertionHelper {
	var files []string
	for name := range e.result.Files {
		files = append(files, name)
	}
	sort.Strings(files)
	file := ""
	for _, name := range files {
		for _, symbol := range e.result.Files[name].Definitions {
			if symbol.Kind != resolve.Type && symbol.Kind != resolve.Enum {
				continue
			}
			if candidate, ok := e.info.Symbols[symbol].(*types.Named); ok && types.Identical(candidate.Origin(), typ.Origin()) {
				file = name
				break
			}
		}
		if file != "" {
			break
		}
	}
	if file == "" {
		if e.enumAssertionPrivate(typ, seen) {
			e.fail(node, "enum_assert needs the defining source to inspect private fields of "+typ.Obj().Name())
			return nil
		}
		// Public representations such as Option/Result need only generic
		// inference for their inaccessible type arguments, not owner source.
		file = e.name
	}
	for _, candidate := range e.enumAssertions.helpers {
		if candidate.file != file || !types.Identical(candidate.typ, typ) || len(candidate.seen) != len(seen) {
			continue
		}
		same := true
		for _, previous := range seen {
			same = same && assertionSeen(candidate.seen, previous)
		}
		if same {
			return candidate
		}
	}
	reserved := map[string]bool{}
	for _, source := range e.result.Files {
		if filepath.Dir(source.Name) != filepath.Dir(file) || source.AST.Package.Name != e.result.Files[file].AST.Package.Name {
			continue
		}
		for _, symbol := range source.Definitions {
			reserved[symbol.GoName] = true
		}
		for _, name := range source.GoNames {
			reserved[name] = true
		}
	}
	for _, helper := range e.enumAssertions.helpers {
		if filepath.Dir(helper.file) == filepath.Dir(file) {
			reserved[helper.name] = true
		}
	}
	base := "GoxideInternalEnumAssert_" + typ.Obj().Name()
	name := base
	for index := 1; reserved[name]; index++ {
		name = base + "_" + strconv.Itoa(index)
	}
	helper := &enumAssertionHelper{typ: typ, seen: append([]types.Type(nil), seen...), file: file, name: name}
	e.enumAssertions.helpers = append(e.enumAssertions.helpers, helper)
	return helper
}

// Private types in a helper signature become Go type parameters. The value
// and callback arguments infer them; the helper never spells a foreign private
// name or imports its caller. Comparable is an ordinary Go formation bound.
func (e *emitter) enumAssertionTypes(helper *enumAssertionHelper, typ types.Type) {
	typ = types.Unalias(typ)
	if helper.typeNames == nil {
		helper.typeNames = map[types.Type]*goast.Ident{}
	}
	if assertionSeen(helper.opaque, typ) {
		return
	}
	add := func() {
		helper.opaque = append(helper.opaque, typ)
		helper.typeNames[typ] = e.fresh("enumType")
	}
	switch typ := typ.(type) {
	case *types.Named:
		if !typ.Obj().Exported() && !e.localTypePackage(typ.Obj().Pkg()) && e.exportedTypeAlias(typ) == nil {
			add()
			return
		}
		substitution := map[*types.TypeParam]types.Type{}
		for index := 0; index < typ.TypeArgs().Len(); index++ {
			substitution[typ.TypeParams().At(index)] = typ.TypeArgs().At(index)
		}
		for index := 0; index < typ.TypeArgs().Len(); index++ {
			argument := typ.TypeArgs().At(index)
			e.enumAssertionTypes(helper, argument)
			for _, opaque := range helper.opaque {
				if !types.Identical(argument, opaque) {
					continue
				}
				constraint := e.typeState.Substitute(e.info, typ.TypeParams().At(index).Constraint(), substitution, nil)
				if types.Identical(constraint, types.Universe.Lookup("any").Type()) {
					continue
				}
				if helper.constraints == nil {
					helper.constraints = map[types.Type][]types.Type{}
				}
				if !assertionSeen(helper.constraints[opaque], constraint) {
					helper.constraints[opaque] = append(helper.constraints[opaque], constraint)
				}
			}
		}
	case *types.Pointer:
		e.enumAssertionTypes(helper, typ.Elem())
	case *types.Array:
		e.enumAssertionTypes(helper, typ.Elem())
	case *types.Slice:
		e.enumAssertionTypes(helper, typ.Elem())
	case *types.Map:
		e.enumAssertionTypes(helper, typ.Key())
		e.enumAssertionTypes(helper, typ.Elem())
	case *types.Chan:
		e.enumAssertionTypes(helper, typ.Elem())
	case *types.Signature:
		for index := 0; index < typ.Params().Len(); index++ {
			e.enumAssertionTypes(helper, typ.Params().At(index).Type())
		}
	case *types.Struct, *types.Interface:
		if !e.nameableType(typ) {
			add()
		}
	}
}

func enumAssertionSignature(helper *enumAssertionHelper) {
	if helper.signature != nil {
		return
	}
	seen := map[*enumAssertionHelper]bool{}
	for _, call := range helper.calls {
		child := call.helper
		enumAssertionSignature(child)
		for _, dependency := range append([]*enumAssertionHelper{child}, child.callbacks...) {
			if !seen[dependency] {
				seen[dependency] = true
				helper.callbacks = append(helper.callbacks, dependency)
			}
		}
	}
	parameters := []*types.Var{types.NewParam(gotoken.NoPos, nil, "", helper.typ)}
	for _, callback := range helper.callbacks {
		parameters = append(parameters, types.NewParam(gotoken.NoPos, nil, "", callback.signature))
	}
	helper.signature = types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), nil, false)
}

func emitEnumAssertions(emitters []*emitter, plan *enumAssertionPlan) error {
	byFile := map[string]*emitter{}
	for _, emitter := range emitters {
		byFile[emitter.name] = emitter
	}
	// First discover the finite callback graph. Concrete seen paths remain
	// attached to every edge, including when an owner helper crosses packages.
	for index := 0; index < len(plan.helpers); index++ {
		helper := plan.helpers[index]
		e := byFile[helper.file]
		if e == nil {
			return fmt.Errorf("enum_assert defining source %s is not emitted", helper.file)
		}
		e.enumAssertionTypes(helper, helper.typ)
		previous := e.inferenceTypes
		e.inferenceTypes = helper.typeNames
		helper.value = e.fresh("enum")
		helper.body = &goast.BlockStmt{List: e.enumAssertionBody(helper.typ, helper.value, helper.seen, e.tree, helper, true)}
		e.inferenceTypes = previous
		if e.err != nil {
			return e.err
		}
	}
	for _, helper := range plan.helpers {
		enumAssertionSignature(helper)
		e := byFile[helper.file]
		e.enumAssertionTypes(helper, helper.signature)
		previous := e.inferenceTypes
		e.inferenceTypes = helper.typeNames
		function := e.goType(helper.signature).(*goast.FuncType)
		function.Params.List[0].Names = []*goast.Ident{helper.value}
		helper.names = map[*enumAssertionHelper]*goast.Ident{}
		for index, callback := range helper.callbacks {
			name := e.fresh("enumCheck")
			helper.names[callback] = name
			function.Params.List[index+1].Names = []*goast.Ident{name}
		}
		if len(helper.opaque) != 0 {
			function.TypeParams = &goast.FieldList{}
			for _, typ := range helper.opaque {
				constraint := &goast.InterfaceType{Methods: &goast.FieldList{}}
				if types.Comparable(typ) {
					constraint.Methods.List = append(constraint.Methods.List, &goast.Field{Type: e.goType(types.Universe.Lookup("comparable").Type())})
				}
				for _, required := range helper.constraints[typ] {
					constraint.Methods.List = append(constraint.Methods.List, &goast.Field{Type: e.goType(required)})
				}
				function.TypeParams.List = append(function.TypeParams.List, &goast.Field{Names: []*goast.Ident{helper.typeNames[typ]}, Type: constraint})
			}
		}
		e.inferenceTypes = previous
		if !e.generatedName(helper.name, e.tree) {
			return e.err
		}
		e.goFile.Decls = append(e.goFile.Decls, &goast.FuncDecl{Name: goast.NewIdent(helper.name), Type: function, Body: helper.body})
		if e.err != nil {
			return e.err
		}
	}
	for _, invocation := range plan.calls {
		reference := func(helper *enumAssertionHelper) goast.Expr {
			if invocation.within != nil {
				return invocation.within.names[helper]
			}
			return invocation.caller.sourceGlobal(helper.file, helper.name)
		}
		invocation.call.Fun = reference(invocation.helper)
		for _, callback := range invocation.helper.callbacks {
			invocation.call.Args = append(invocation.call.Args, reference(callback))
		}
	}
	for _, request := range plan.values {
		e, helper := request.caller, request.helper
		reference := e.sourceGlobal(helper.file, helper.name)
		if len(helper.callbacks) == 0 {
			request.value.X = reference
			continue
		}
		p := newTraitInference(e)
		function := p.parameter(reference, helper.signature)
		var callbacks []goast.Expr
		for _, dependency := range helper.callbacks {
			callbacks = append(callbacks, p.parameter(e.sourceGlobal(dependency.file, dependency.name), dependency.signature))
		}
		signature := signatureOf([]types.Type{helper.typ}, nil)
		p.collect(signature)
		previous := e.inferenceTypes
		e.inferenceTypes = p.bodyTypes
		parameter := e.fresh("enum")
		closure := &goast.FuncLit{Type: e.goType(signature).(*goast.FuncType), Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ExprStmt{X: &goast.CallExpr{Fun: function, Args: append([]goast.Expr{parameter}, callbacks...)}}}}}
		closure.Type.Params.List[0].Names = []*goast.Ident{parameter}
		e.inferenceTypes = previous
		request.value.X = p.finish(closure, signature)
		if e.err != nil {
			return e.err
		}
	}

	return nil
}
