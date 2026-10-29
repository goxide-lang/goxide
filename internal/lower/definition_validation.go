package lower

import (
	"fmt"
	goast "go/ast"
	"go/constant"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"regexp"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// ValidateDefinitions checks every generic definition and every evidence
// leaf, even when no call demands it. This AST is never printed or executed.
// Unknown Go array lengths only preserve array operations; check is responsible
// for exact source length identity at assignment and other type boundaries.
func ValidateDefinitions(files map[string]*syntax.File, info *check.Info) error {
	validation := &definitionValidation{parameters: map[*check.ConstParam]validationParameter{}, reserved: map[string]bool{}}
	inputs := make(map[string]*syntax.File, len(files))
	for name, file := range files {
		inputs[name] = file
	}
	if info != nil && info.Resolve != nil {
		for name, file := range info.Resolve.Files {
			syntax.Walk(file.AST, func(node syntax.Node) bool {
				if ident, ok := node.(*syntax.Ident); ok {
					validation.reserved[ident.Name] = true
				}
				return true
			})
			for _, symbol := range file.Definitions {
				validation.reserved[symbol.GoName] = true
			}
			for _, name := range file.GoNames {
				validation.reserved[name] = true
			}
			if file.AST.NativeGo && info.NativePackages[filepath.ToSlash(filepath.Dir(name))] == nil {
				inputs[name] = file.AST
			}
		}
	}
	_, err := lower(inputs, info, emissionOptions{validation: validation})
	if _, generatedError := err.(types.Error); generatedError {
		// Successful compilation does not need source tracking. On failure,
		// replay the same private lowering state with exact diagnostic tokens.
		validation.err, validation.provenance = nil, true
		_, err = lower(inputs, info, emissionOptions{validation: validation})
	}
	return err
}

func hasGenericDefinitions(files map[string]*syntax.File) bool {
	found := false
	for _, file := range files {
		syntax.Walk(file, func(node syntax.Node) bool {
			if found {
				return false
			}
			switch declaration := node.(type) {
			case *syntax.FuncDecl:
				found = declaration.Generics != nil && declaration.Body != nil
			case *syntax.ImplDecl:
				if declaration.Generics != nil {
					for _, item := range declaration.Items {
						if member, ok := item.(*syntax.FuncDecl); ok && member.Body != nil {
							found = true
						}
					}
				}
			}
			return !found
		})
	}
	return found
}

type definitionValidation struct {
	provenance bool
	parameters map[*check.ConstParam]validationParameter
	reserved   map[string]bool
	err        error
}

type validationParameter struct{ length, array string }

func (v *definitionValidation) parameterNames(parameter *check.ConstParam) validationParameter {
	names, exists := v.parameters[parameter]
	if !exists {
		fresh := func(prefix string) string {
			for index := 1; ; index++ {
				name := fmt.Sprintf("goxideValidation%s%d", prefix, index)
				if !v.reserved[name] {
					v.reserved[name] = true
					return name
				}
			}
		}
		names = validationParameter{length: fresh("Length"), array: fresh("Array")}
		v.parameters[parameter] = names
	}
	return names
}

func (v *definitionValidation) constantName(parameter *check.ConstParam) string {
	return v.parameterNames(parameter).length
}

func (v *definitionValidation) arrayType(parameter *check.ConstParam, element goast.Expr) goast.Expr {
	return &goast.IndexExpr{X: goast.NewIdent(v.parameterNames(parameter).array), Index: element}
}

func (e *emitter) validationConstParameter(node syntax.Expr) *check.ConstParam {
	if e.validation == nil || node == nil {
		return nil
	}
	if value, ok := e.info.ConstValues[node]; ok {
		return value.Param
	}
	return e.info.ConstParams[e.valueSymbol(node)]
}

func (v *definitionValidation) preparePackage(pkg *types.Package) {
	for _, names := range v.parameters {
		length := types.NewConst(gotoken.NoPos, pkg, names.length, types.Typ[types.Int], constant.MakeUnknown())
		pkg.Scope().Insert(length)
		parameter := types.NewTypeParam(types.NewTypeName(gotoken.NoPos, pkg, "Element", nil), types.Universe.Lookup("any").Type())
		name := types.NewTypeName(gotoken.NoPos, pkg, names.array, nil)
		alias := types.NewAlias(name, types.NewArray(parameter, -1))
		alias.SetTypeParams([]*types.TypeParam{parameter})
		pkg.Scope().Insert(name)
	}
}

// go/types fills an unknown array's length from a composite literal. Use a
// typed local and a slice literal so validating one literal cannot mutate the
// symbolic type shared with later operations or other definitions. The slice
// still checks element assignments, constant keys, and duplicate indices.
func (e *emitter) validationArrayLiteral(node *syntax.CompositeLit, array *check.ArrayPattern) goast.Expr {
	typ := e.validation.arrayType(array.Length.Param, e.goType(array.Elem))
	value := e.fresh("array")
	body := []goast.Stmt{varStmt(value, typ, nil)}
	elements := &goast.CompositeLit{Type: &goast.ArrayType{Elt: e.goType(array.Elem)}, Elts: e.exprs(node.Elts)}
	body = append(body, &goast.AssignStmt{Lhs: []goast.Expr{goast.NewIdent("_")}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{elements}})
	body = append(body, &goast.ReturnStmt{Results: []goast.Expr{value}})
	result := &goast.CallExpr{Fun: &goast.FuncLit{Type: &goast.FuncType{Params: &goast.FieldList{}, Results: &goast.FieldList{List: []*goast.Field{{Type: typ}}}}, Body: &goast.BlockStmt{List: body}}}
	e.anchor(result, node)
	return result
}

var validationIdentifier = regexp.MustCompile(`[\pL_][\pL\p{Nd}_]*`)

func (v *definitionValidation) report(err error, emitters []*emitter) {
	if v.err != nil {
		return
	}
	if !v.provenance {
		v.err = err
		return
	}
	diagnostic, ok := err.(types.Error)
	if !ok {
		v.err = err
		return
	}
	for _, emitter := range emitters {
		if diagnostic.Pos < emitter.base || diagnostic.Pos >= emitter.base+gotoken.Pos(emitter.tokenFile.Size()) {
			continue
		}
		original := emitter.diagnosticNodes[diagnostic.Pos]
		if original == nil {
			v.err = fmt.Errorf("泛型定义检查失败（生成的 Go 无源码映射）: %w", diagnostic)
			return
		}
		names := map[string]string{}
		for parameter, generated := range v.parameters {
			names[generated.length] = parameter.Name.Name
			names[generated.array] = "[" + parameter.Name.Name + "]"
		}
		message := validationIdentifier.ReplaceAllStringFunc(diagnostic.Msg, func(identifier string) string {
			if source := names[identifier]; source != "" {
				return source
			}
			return identifier
		})
		emitter.fail(original, "泛型定义检查失败: "+message)
		v.err = emitter.err
		return
	}
	v.err = fmt.Errorf("泛型定义检查失败（生成的 Go 无源码映射）: %w", diagnostic)
}
