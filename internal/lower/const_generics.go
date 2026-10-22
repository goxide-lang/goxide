package lower

import (
	goast "go/ast"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func hasConstParameters(generics *syntax.GenericParams) bool {
	if generics != nil {
		for _, group := range generics.Params {
			if group.Const {
				return true
			}
		}
	}
	return false
}

func constFunctionName(function *traitFunction, instance *check.FunctionInstance) string {
	name := templateFunctionName(function)
	if instance != nil {
		for _, argument := range instance.ConstArgs {
			if argument.Param != nil {
				return caseFunctionName(name, instance.Case)
			}
		}
	}
	if instance == nil || len(instance.ConstArgs) == 0 {
		if instance != nil {
			return caseFunctionName(name, instance.Case)
		}
		return name
	}
	name = strings.TrimSuffix(name, "Func")
	for _, argument := range instance.ConstArgs {
		name += "N" + strconv.FormatInt(argument.Value, 10)
	}
	return caseFunctionName(name+"Func", instance.Case)
}

func (e *emitter) instanceBodyFunction() *check.FunctionInstance {
	if e.instanceBody == nil {
		return nil
	}
	return e.instanceBody.Function
}

func (e *emitter) constFunctionDeclarations(function *traitFunction) []goast.Decl {
	var definitions []*check.FunctionInstance
	for _, definition := range e.info.Demands.Functions {
		if definition.Decl == function.decl && len(definition.ConstArgs) != 0 {
			definitions = append(definitions, definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool {
		return constFunctionName(function, definitions[i]) < constFunctionName(function, definitions[j])
	})
	previousInfo, previousBody := e.info, e.instanceBody
	defer func() { e.info, e.instanceBody = previousInfo, previousBody }()
	var out []goast.Decl
	if e.validation != nil {
		out = append(out, e.templateFunctionDeclaration(function)...)
	}
	for _, definition := range definitions {
		e.info, e.instanceBody = definition.Body.Info, definition.Body
		out = append(out, e.templateFunctionDeclaration(function)...)
	}
	return out
}
