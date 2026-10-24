package lower

import (
	goast "go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
)

// Inherent operations have no source receiver. Their carrier is always empty;
// it never stores a value of Self or an implementation dictionary.
type inherentCarrier struct {
	name string
	impl *check.Impl
}

func plainInherentCarrier(info *check.Info, impl *check.Impl) bool {
	if len(impl.Requirements) != 0 || len(impl.ConstParams) != 0 || len(impl.ConstArgs) != 0 || implHasBodyCases(info, impl) || info.Demands != nil && info.Demands.ImplTemplates[impl] {
		return false
	}
	target, ok := types.Unalias(impl.Target).(*types.Named)
	if !ok || len(impl.TypeParams) != target.Origin().TypeParams().Len() {
		return false
	}
	if len(impl.TypeParams) == 0 {
		return target.TypeArgs().Len() == 0
	}
	if target.TypeArgs().Len() != len(impl.TypeParams) {
		return false
	}
	sub := map[*types.TypeParam]types.Type{}
	for index, parameter := range impl.TypeParams {
		if target.TypeArgs().At(index) != parameter {
			return false
		}
		sub[parameter] = target.Origin().TypeParams().At(index)
	}
	for _, parameter := range impl.TypeParams {
		constraint, ok := info.Substitute(parameter.Constraint(), sub, nil).Underlying().(*types.Interface)
		if !ok || !types.Satisfies(sub[parameter], constraint) {
			return false
		}
	}
	return true
}

func (p *traitProgram) prepareInherentCarriers(info *check.Info) {
	p.inherentCarriers = map[*check.Impl]*inherentCarrier{}
	shared := map[*types.Named]*inherentCarrier{}
	for _, impl := range p.impls {
		if impl.Trait != nil {
			continue
		}
		for _, member := range impl.Methods {
			if !member.Static && !p.enumReadExpressions[member.Decl] {
				continue
			}
			name := targetName(impl) + "Ops_" + implSuffix(info, impl)
			var carrier *inherentCarrier
			if plainInherentCarrier(info, impl) {
				name = impl.Base.Obj().Name() + "Ops"
				carrier = shared[impl.Base]
			}
			if carrier == nil {
				carrier = &inherentCarrier{name: name, impl: impl}
				if plainInherentCarrier(info, impl) {
					shared[impl.Base] = carrier
				}
			}
			p.inherentCarriers[impl] = carrier
			p.inherentCarriers[implTemplate(impl)] = carrier
			break
		}
	}
}

func (e *emitter) inherentCarrier(impl *check.Impl) *inherentCarrier {
	return e.traits.inherentCarriers[implTemplate(impl)]
}

func carrierMethod(typ goast.Expr, name string) goast.Expr {
	return &goast.SelectorExpr{X: &goast.ParenExpr{X: &goast.CompositeLit{Type: typ}}, Sel: goast.NewIdent(name)}
}

func (e *emitter) inherentCarrierType(impl *check.Impl) goast.Expr {
	carrier := e.inherentCarrier(impl)
	return enumIndices(e.sourceGlobal(carrier.impl.File, carrier.name), e.implTypeArgs(impl))
}

func (e *emitter) inherentCarrierDeclarations() []goast.Decl {
	seen := map[*inherentCarrier]bool{}
	var carriers []*inherentCarrier
	for _, carrier := range e.traits.inherentCarriers {
		impl := carrier.impl
		if seen[carrier] || impl.File != e.name || parameterizedImpl(impl) && len(impl.Requirements) != 0 || e.info.Demands != nil && e.info.Demands.ImplTemplates[impl] {
			continue
		}
		seen[carrier] = true
		carriers = append(carriers, carrier)
	}
	sort.Slice(carriers, func(i, j int) bool { return carriers[i].name < carriers[j].name })
	var declarations []goast.Decl
	for _, carrier := range carriers {
		e.generatedName(carrier.name, carrier.impl.Decl)
		declarations = append(declarations, genericTypeDeclaration(carrier.name, e.implGenerics(carrier.impl), &goast.StructType{Fields: &goast.FieldList{}}))
	}
	return declarations
}

func (e *emitter) instanceOperation(instance *traitInstance) goast.Expr {
	if instance.body.Impl != nil {
		for _, member := range instance.body.Impl.Methods {
			if member.Decl == instance.body.Decl && member.Trait == nil && member.Static {
				return carrierMethod(e.sourceGlobal(instance.body.File, instance.name+"Ops"), member.GoName)
			}
		}
	}
	return e.sourceGlobal(instance.body.File, instance.name)
}

func (e *emitter) inherentTemplateCarrierName(member *check.Member) string {
	return strings.TrimSuffix(e.templateOperationName(member), "Func") + "Ops"
}

func (e *emitter) inherentTemplateArguments(member *check.Member) []goast.Expr {
	arguments := e.operationTemplateArguments(member)
	if memberCase(member) != nil {
		return arguments
	}
	impl := member.Impl
	for index, bound := range implTemplate(impl).Requirements {
		for _, projected := range bound.Projected {
			actual := types.Type(projected)
			if index < len(impl.Requirements) {
				for _, associated := range bound.Trait.Associated {
					if types.Identical(bound.Associated[associated.Name], projected) {
						actual = impl.Requirements[index].Associated[associated.Name]
						break
					}
				}
			}
			arguments = append(arguments, e.goType(actual))
		}
	}
	return arguments
}

func typeParameterArguments(parameters *goast.FieldList) []goast.Expr {
	var arguments []goast.Expr
	if parameters != nil {
		for _, parameter := range parameters.List {
			for _, name := range parameter.Names {
				arguments = append(arguments, goast.NewIdent(name.Name))
			}
		}
	}
	return arguments
}

// A Go generic function can infer an otherwise unnameable private type when
// passed as an argument. The bridge only binds a carrier operation; the body
// stays in the returned business function and keeps its defer/recover frame.
func (e *emitter) inherentTemplateDeclaration(member *check.Member, declaration *goast.FuncDecl, method string) (*goast.FuncDecl, *goast.FuncDecl) {
	name, parameters := declaration.Name.Name, declaration.Type.TypeParams
	carrier := enumIndices(goast.NewIdent(e.inherentTemplateCarrierName(member)), typeParameterArguments(parameters))
	var arguments []goast.Expr
	fields := &goast.FieldList{}
	for _, parameter := range declaration.Type.Params.List {
		field := *parameter
		field.Names = nil
		for _, original := range parameter.Names {
			identifier := goast.NewIdent(original.Name)
			if identifier.Name == "_" {
				identifier = e.fresh("Witness")
			}
			field.Names = append(field.Names, identifier)
			arguments = append(arguments, identifier)
		}
		fields.List = append(fields.List, &field)
	}
	call := &goast.CallExpr{Fun: carrierMethod(carrier, method), Args: arguments}
	if len(fields.List) != 0 {
		if _, variadic := fields.List[len(fields.List)-1].Type.(*goast.Ellipsis); variadic {
			call.Ellipsis = 1
		}
	}
	bridge := &goast.FuncDecl{Name: goast.NewIdent(name), Type: &goast.FuncType{TypeParams: parameters, Params: fields, Results: declaration.Type.Results}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{call}}}}}
	declaration.Name = goast.NewIdent(method)
	declaration.Recv = receiverField("", carrier)
	declaration.Type.TypeParams = nil
	return declaration, bridge
}
