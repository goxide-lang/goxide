package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// A published const template keeps an actual array as a Go type parameter.
// Length and a typed projection are supplied separately; the body stays in its
// defining package and neither reflection nor a consumer-specific entry is used.
type constArray struct {
	typ, elem           types.Type
	length              *check.ConstParam
	typeName, sliceName string
}
type constTemplate struct {
	arrays     []*constArray
	parameters []*check.ConstParam
	lengths    map[*check.ConstParam]string
	aliases    map[*resolve.Symbol]stableConstAlias
	iota       *int
}

func originalImpl(impl *check.Impl) *check.Impl {
	for impl.Template != nil {
		impl = impl.Template
	}
	return impl
}

func (e *emitter) stableConstPlan(node syntax.Node, constants []*check.ConstParam) *constTemplate {
	if !e.traits.stableGo || e.validation != nil || len(constants) == 0 {
		return nil
	}
	info := e.traits.sourceInfo
	plan := &constTemplate{parameters: constants, lengths: map[*check.ConstParam]string{}}
	seen := map[types.Type]bool{}
	var collect func(types.Type)
	collect = func(typ types.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if array, ok := e.typeState.ArrayOf(info, typ); ok {
			collect(array.Elem)
			if array.Length.Param != nil {
				for _, old := range plan.arrays {
					if old.length == array.Length.Param && types.Identical(old.elem, array.Elem) {
						return
					}
				}
				plan.arrays = append(plan.arrays, &constArray{typ: typ, elem: array.Elem, length: array.Length.Param})
			}
			return
		}
		switch t := types.Unalias(typ).(type) {
		case *types.Pointer:
			collect(t.Elem())
		case *types.Slice:
			collect(t.Elem())
		case *types.Map:
			collect(t.Key())
			collect(t.Elem())
		case *types.Chan:
			collect(t.Elem())
		case *types.Named:
			for i := 0; i < t.TypeArgs().Len(); i++ {
				collect(t.TypeArgs().At(i))
			}
		case *types.Signature:
			collect(t.Params())
			collect(t.Results())
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				collect(t.At(i).Type())
			}
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				collect(t.Field(i).Type())
			}
		case *types.Interface:
			for i := 0; i < t.NumExplicitMethods(); i++ {
				collect(t.ExplicitMethod(i).Type())
			}
			for i := 0; i < t.NumEmbeddeds(); i++ {
				collect(t.EmbeddedType(i))
			}
		case *types.Union:
			for i := 0; i < t.Len(); i++ {
				collect(t.Term(i).Type())
			}
		}
	}
	if impl, ok := node.(*syntax.ImplDecl); ok {
		if definition := info.Impls[impl]; definition != nil {
			collect(definition.Target)
			for _, member := range definition.Methods {
				collect(member.Signature)
			}
		}
	}
	var sourceFile string
	switch declaration := node.(type) {
	case *syntax.ImplDecl:
		sourceFile = info.Impls[declaration].File
	case *syntax.FuncDecl:
		for _, function := range e.traits.functions {
			if function.decl == declaration {
				sourceFile = function.symbol.Decl.File
				break
			}
		}
	}
	source := info.Resolve.Files[sourceFile]
	syntax.Walk(node, func(n syntax.Node) bool {
		if expression, ok := n.(syntax.Expr); ok {
			collect(info.Types[expression])
		}
		if typ, ok := n.(syntax.Type); ok {
			collect(info.SourceType(typ))
		}
		if id, ok := n.(*syntax.Ident); ok && source != nil {
			collect(info.Symbols[source.Definitions[id.ID()]])
		}
		return true
	})
	return plan
}

func (e *emitter) stableImplPlan(impl *check.Impl) *constTemplate {
	if !e.traits.stableGo || e.validation != nil {
		return nil
	}
	impl = originalImpl(impl)
	return e.stableConstPlan(impl.Decl, impl.ConstParams)
}
func (e *emitter) stableFunctionPlan(function *traitFunction) *constTemplate {
	if !e.traits.stableGo || e.validation != nil {
		return nil
	}
	info := e.traits.sourceInfo
	signature, _ := info.Symbols[function.symbol].(*types.Signature)
	return e.stableConstPlan(function.decl, info.FunctionConstParams[signature])
}
func (e *emitter) enterConstTemplate(plan *constTemplate) func() {
	if plan == nil {
		return func() {}
	}
	previous, previousInfo := e.constTemplate, e.info
	contextInfo := e.traits.sourceInfo
	if e.instanceBody != nil && e.instanceBody.Case != nil {
		contextInfo = e.info
		for _, array := range plan.arrays {
			array.typ = e.typeState.Substitute(contextInfo, array.typ, e.instanceBody.Substitution, nil)
			if pattern, ok := e.typeState.ArrayOf(contextInfo, array.typ); ok {
				array.elem = pattern.Elem
				array.length = pattern.Length.Param
			}
		}
	} else if e.caseViewImpl != nil && e.caseViewImpl.Case != nil {
		contextInfo = e.info
		for _, array := range plan.arrays {
			array.typ = e.typeState.Substitute(contextInfo, array.typ, e.caseViewImpl.Substitution, nil)
			if pattern, ok := e.typeState.ArrayOf(contextInfo, array.typ); ok {
				array.elem = pattern.Elem
				array.length = pattern.Length.Param
			}
		}
	}
	for _, parameter := range plan.parameters {
		plan.lengths[parameter] = e.fresh("Const" + parameter.Name.Name).Name
	}
	for _, array := range plan.arrays {
		array.typeName = e.fresh("Array").Name
		array.sliceName = e.fresh("ArraySlice").Name
	}
	e.constTemplate, e.info = plan, contextInfo
	plan.aliases = make(map[*resolve.Symbol]stableConstAlias)
	return func() { e.constTemplate, e.info = previous, previousInfo }
}
func (e *emitter) stableArray(typ types.Type) *constArray {
	if e.constTemplate == nil {
		return nil
	}
	array, ok := e.typeState.ArrayOf(e.info, typ)
	if !ok || array.Length.Param == nil {
		return nil
	}
	for _, candidate := range e.constTemplate.arrays {
		if candidate.length == array.Length.Param && types.Identical(candidate.elem, array.Elem) {
			return candidate
		}
	}
	return nil
}
func (e *emitter) stableArrayType(typ types.Type) goast.Expr {
	if array := e.stableArray(typ); array != nil {
		return goast.NewIdent(array.typeName)
	}
	return nil
}
func (e *emitter) stableConstant(node syntax.Expr) goast.Expr {
	if e.constTemplate == nil {
		return nil
	}
	id, ok := node.(*syntax.Ident)
	if !ok {
		return nil
	}
	symbol := e.file.References[id.ID()].Symbol
	if alias, ok := e.constTemplate.aliases[symbol]; ok {
		previous := e.constTemplate.iota
		e.constTemplate.iota = &alias.iota
		value := e.expr(alias.value)
		e.constTemplate.iota = previous
		if alias.typ != nil {
			value = &goast.CallExpr{Fun: e.typ(alias.typ), Args: []goast.Expr{value}}
		}
		return value
	}
	if e.constTemplate.iota != nil && symbol != nil && symbol.Kind == resolve.Constant && symbol.Name.Name == "iota" && symbol.Decl.File == "" {
		return &goast.BasicLit{Kind: gotoken.INT, Value: strconv.Itoa(*e.constTemplate.iota)}
	}
	parameter := e.info.ConstParams[symbol]
	if name := e.constTemplate.lengths[parameter]; name != "" {
		return e.callbackReference(goast.NewIdent(name))
	}
	return nil
}
func (e *emitter) stableConstTypeParameters(fields *goast.FieldList) *goast.FieldList {
	if e.constTemplate == nil {
		return fields
	}
	if len(e.constTemplate.arrays) == 0 {
		return fields
	}
	if fields == nil {
		fields = &goast.FieldList{}
	}
	for _, array := range e.constTemplate.arrays {
		constraint := "any"
		if e.typeState.Comparable(e.info, array.typ) {
			constraint = "comparable"
		}
		fields.List = append(fields.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(array.typeName)}, Type: goast.NewIdent(constraint)})
	}
	return fields
}
func (e *emitter) stableConstParameters() []*goast.Field {
	if e.constTemplate == nil {
		return nil
	}
	var fields []*goast.Field
	for _, parameter := range e.constTemplate.parameters {
		fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(e.constTemplate.lengths[parameter])}, Type: goast.NewIdent("int")})
	}
	for _, array := range e.constTemplate.arrays {
		signature := &goast.FuncType{Params: &goast.FieldList{List: []*goast.Field{{Type: &goast.StarExpr{X: goast.NewIdent(array.typeName)}}}}, Results: &goast.FieldList{List: []*goast.Field{{Type: &goast.ArrayType{Elt: e.goType(array.elem)}}}}}
		fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(array.sliceName)}, Type: signature})
	}
	return fields
}
func (e *emitter) stableConstTypes(plan *constTemplate, sub map[*types.TypeParam]types.Type, constants map[*check.ConstParam]check.ConstValue) []types.Type {
	if plan == nil {
		return nil
	}
	var result []types.Type
	for _, array := range plan.arrays {
		result = append(result, e.typeState.Substitute(e.traits.sourceInfo, array.typ, sub, constants))
	}
	return result
}
func (e *emitter) stableConstArguments(plan *constTemplate, sub map[*types.TypeParam]types.Type, constants map[*check.ConstParam]check.ConstValue) ([]goast.Expr, []types.Type) {
	if plan == nil {
		return nil, nil
	}
	var args []goast.Expr
	var signatures []types.Type
	for _, parameter := range plan.parameters {
		value, ok := constants[parameter]
		if !ok {
			value = check.ConstValue{Param: parameter}
		}
		expression := goast.Expr(&goast.BasicLit{Kind: gotoken.INT, Value: strconv.FormatInt(value.Value, 10)})
		if value.Param != nil {
			name := ""
			if e.constTemplate != nil {
				name = e.constTemplate.lengths[value.Param]
			}
			if name == "" {
				e.fail(parameter.Name, "stable Go const argument is not available")
			}
			expression = e.callbackReference(goast.NewIdent(name))
		}
		args = append(args, expression)
		signatures = append(signatures, types.Typ[types.Int])
	}
	for _, typ := range e.stableConstTypes(plan, sub, constants) {
		array, ok := e.typeState.ArrayOf(e.info, typ)
		if !ok {
			e.fail(e.tree, "stable Go array shape is missing")
			continue
		}
		args = append(args, e.stableProjection(typ))
		signatures = append(signatures, signatureOf([]types.Type{types.NewPointer(typ)}, types.NewSlice(array.Elem)))
	}
	return args, signatures
}
func (e *emitter) stableProjection(typ types.Type) goast.Expr {
	if array := e.stableArray(typ); array != nil {
		return e.callbackReference(goast.NewIdent(array.sliceName))
	}
	array, ok := e.typeState.ArrayOf(e.info, typ)
	if !ok || array.Length.Param != nil {
		e.fail(e.tree, "stable Go array projection needs a checked length")
		return goast.NewIdent("goxideMissingArray")
	}
	name := e.fresh("ArraySlice")
	element := e.fresh("Element")
	target := e.fresh("Array")
	value := e.fresh("Value")
	signature := &goast.FuncType{TypeParams: &goast.FieldList{List: []*goast.Field{
		{Names: []*goast.Ident{element}, Type: goast.NewIdent("any")},
		{Names: []*goast.Ident{target}, Type: &goast.UnaryExpr{Op: gotoken.TILDE, X: &goast.ArrayType{Len: &goast.BasicLit{Kind: gotoken.INT, Value: strconv.FormatInt(array.Length.Value, 10)}, Elt: element}}},
	}}, Params: &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{value}, Type: &goast.StarExpr{X: target}}}}, Results: &goast.FieldList{List: []*goast.Field{{Type: &goast.ArrayType{Elt: element}}}}}
	body := &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.SliceExpr{X: &goast.ParenExpr{X: &goast.StarExpr{X: value}}}}}}}
	e.flowHelpers = append(e.flowHelpers, &goast.FuncDecl{Name: name, Type: signature, Body: body})
	if e.nameableType(typ) {
		return enumIndices(name, []goast.Expr{e.goType(array.Elem), e.goType(typ)})
	}
	return name
}

func (p *traitInference) stableConstants(plan *constTemplate, sub map[*types.TypeParam]types.Type, constants map[*check.ConstParam]check.ConstValue) ([]goast.Expr, []types.Type) {
	values, signatures := p.e.stableConstArguments(plan, sub, constants)
	for i, value := range values {
		values[i] = p.parameter(value, signatures[i])
	}
	return values, signatures
}

func stableTypeTargetName(state *check.TypeState, info *check.Info, typ types.Type) string {
	if array, ok := state.ArrayOf(info, typ); ok {
		length := strconv.FormatInt(array.Length.Value, 10)
		if array.Length.Param != nil {
			length = exportedName(array.Length.Param.Name.Name)
		}
		return stableTypeTargetName(state, info, array.Elem) + "Array" + length
	}
	switch t := types.Unalias(typ).(type) {
	case *types.Named:
		name := exportedName(t.Obj().Name())
		for i := 0; i < t.TypeArgs().Len(); i++ {
			name += stableTypeTargetName(state, info, t.TypeArgs().At(i))
		}
		return name
	case *types.Pointer:
		return stableTypeTargetName(state, info, t.Elem()) + "Pointer"
	case *types.Slice:
		return stableTypeTargetName(state, info, t.Elem()) + "Slice"
	case *types.Map:
		return "Map" + stableTypeTargetName(state, info, t.Key()) + stableTypeTargetName(state, info, t.Elem())
	case *types.Chan:
		direction := "Chan"
		if t.Dir() == types.RecvOnly {
			direction = "ReceiveChan"
		} else if t.Dir() == types.SendOnly {
			direction = "SendChan"
		}
		return direction + stableTypeTargetName(state, info, t.Elem())
	case *types.Signature:
		name := "Func"
		for i := 0; i < t.Params().Len(); i++ {
			name += stableTypeTargetName(state, info, t.Params().At(i).Type())
		}
		name += "To"
		for i := 0; i < t.Results().Len(); i++ {
			name += stableTypeTargetName(state, info, t.Results().At(i).Type())
		}
		return name
	}
	return typeTargetName(typ)
}

func generatedBodyUses(body *goast.BlockStmt, name string) bool {
	used := false
	goast.Inspect(body, func(node goast.Node) bool {
		if id, ok := node.(*goast.Ident); ok && id.Name == name {
			used = true
		}
		return !used
	})
	return used
}
