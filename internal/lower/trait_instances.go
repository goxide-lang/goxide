package lower

import (
	goast "go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type traitInstance struct {
	body *check.BodyInstance
	name string
}

// Concrete and ordinary generic impl bodies use their own rebound semantic
// view. Other instantiations of the same declaration must stay independent.
func (e *emitter) memberBodyInfo(member *check.Member) func() {
	previousInfo, previousBody := e.info, e.instanceBody
	if e.info.Demands != nil {
		for _, body := range e.info.Demands.Bodies[member.Decl] {
			if body.Impl != nil && implTemplate(body.Impl) == implTemplate(member.Impl) && types.Identical(body.Impl.Target, member.Impl.Target) {
				e.info, e.instanceBody = body.Info, body
				break
			}
		}
	}
	return func() { e.info, e.instanceBody = previousInfo, previousBody }
}

func instanceImplKey(info *check.Info, impl *check.Impl, member string) string {
	return implIdentity(info, impl) + ":" + types.TypeString(impl.Target, func(pkg *types.Package) string { return canonicalPackage(info, pkg) }) + ":" + member
}

func instanceFunctionKey(info *check.Info, function *traitFunction, instance *check.FunctionInstance) string {
	var key strings.Builder
	key.WriteString(function.symbol.Decl.File)
	key.WriteString(":" + strconv.Itoa(int(function.decl.Span().Start)))
	for _, typ := range instance.TypeArgs {
		key.WriteByte(':')
		key.WriteString(types.TypeString(typ, func(pkg *types.Package) string { return canonicalPackage(info, pkg) }))
	}
	return key.String()
}

// A specialization must not make the definition package depend on consumers.
// Foreign arguments use an ordinary generic operation helper instead.
func localInstanceType(typ types.Type, file string, seen map[types.Type]bool) bool {
	if typ == nil || seen[typ] {
		return true
	}
	seen[typ] = true
	switch typ := types.Unalias(typ).(type) {
	case *types.TypeParam:
		return false
	case *types.Named:
		if pkg := typ.Obj().Pkg(); pkg != nil && pkg.Path() != filepath.ToSlash(filepath.Dir(file)) {
			return false
		}
		for index := 0; index < typ.TypeArgs().Len(); index++ {
			if !localInstanceType(typ.TypeArgs().At(index), file, seen) {
				return false
			}
		}
		return true
	case *types.Pointer:
		return localInstanceType(typ.Elem(), file, seen)
	case *types.Slice:
		return localInstanceType(typ.Elem(), file, seen)
	case *types.Array:
		return localInstanceType(typ.Elem(), file, seen)
	case *types.Map:
		return localInstanceType(typ.Key(), file, seen) && localInstanceType(typ.Elem(), file, seen)
	case *types.Chan:
		return localInstanceType(typ.Elem(), file, seen)
	case *types.Struct:
		for index := 0; index < typ.NumFields(); index++ {
			field := typ.Field(index)
			if !field.Exported() && field.Pkg() != nil && field.Pkg().Path() != filepath.ToSlash(filepath.Dir(file)) {
				return false
			}
			if !localInstanceType(field.Type(), file, seen) {
				return false
			}
		}
	case *types.Interface:
		for index := 0; index < typ.NumMethods(); index++ {
			method := typ.Method(index)
			if !method.Exported() && method.Pkg() != nil && method.Pkg().Path() != filepath.ToSlash(filepath.Dir(file)) {
				return false
			}
			if !localInstanceType(method.Type(), file, seen) {
				return false
			}
		}
		for index := 0; index < typ.NumEmbeddeds(); index++ {
			if !localInstanceType(typ.EmbeddedType(index), file, seen) {
				return false
			}
		}
	case *types.Signature:
		return localInstanceType(typ.Params(), file, seen) && localInstanceType(typ.Results(), file, seen)
	case *types.Tuple:
		for index := 0; index < typ.Len(); index++ {
			if !localInstanceType(typ.At(index).Type(), file, seen) {
				return false
			}
		}
	}
	return true
}

func (p *traitProgram) prepareInstances(info *check.Info) {
	p.instances = map[string]*traitInstance{}
	p.templateImpls = map[*check.Impl]bool{}
	p.templateFunctions = map[*syntax.FuncDecl]bool{}
	if info.Demands == nil {
		return
	}
	for _, impl := range info.Demands.Impls {
		if impl.Template != nil {
			p.templateImpls[impl.Template] = true
		}
	}
	for _, function := range info.Demands.Functions {
		if declared := p.functions[function.Signature]; declared != nil && info.Demands.FunctionTemplates[declared.decl] {
			p.templateFunctions[declared.decl] = true
		}
	}
	var keys []string
	for _, bodies := range info.Demands.Bodies {
		for _, body := range bodies {
			if body.Decl == nil || len(body.Substitution) == 0 {
				continue
			}
			local := true
			for _, typ := range body.Substitution {
				local = local && localInstanceType(typ, body.File, map[types.Type]bool{})
			}
			if !local {
				continue
			}
			var key, name string
			if body.Impl != nil && body.Impl.Template != nil && info.Demands.ImplTemplates[body.Impl.Template] {
				key = instanceImplKey(info, body.Impl, body.Decl.Name.Name)
				name = "Goxide" + typeTargetName(body.Impl.Target)
				if body.Impl.Trait != nil {
					name += traitName(body.Impl.Trait)
				}
				name += exportedName(body.Decl.Name.Name)
			} else if body.Function != nil {
				function := p.functions[body.Function.Signature]
				if function == nil || !info.Demands.FunctionTemplates[function.decl] {
					continue
				}
				key = instanceFunctionKey(info, function, body.Function)
				name = "Goxide" + exportedName(function.symbol.GoName)
				for _, typ := range body.Function.TypeArgs {
					name += typeTargetName(typ)
				}
			}
			if key != "" && p.instances[key] == nil {
				p.instances[key] = &traitInstance{body: body, name: name}
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	used, reserved := map[string]bool{}, map[string]bool{}
	for _, key := range keys {
		instance := p.instances[key]
		reserved[filepath.Dir(instance.body.File)+":"+instance.name] = true
	}
	for _, key := range keys {
		instance := p.instances[key]
		prefix := filepath.Dir(instance.body.File) + ":"
		name := instance.name
		if used[prefix+name] {
			for suffix := 2; ; suffix++ {
				candidate := name + strconv.Itoa(suffix)
				if !used[prefix+candidate] && !reserved[prefix+candidate] {
					instance.name = candidate
					break
				}
			}
		}
		used[prefix+instance.name] = true
	}
}

func (e *emitter) instanceDeclarations(node *syntax.FuncDecl) []goast.Decl {
	var instances []*traitInstance
	for _, instance := range e.traits.instances {
		if instance.body.File == e.name && instance.body.Decl == node {
			instances = append(instances, instance)
		}
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].name < instances[j].name })
	var out []goast.Decl
	for _, instance := range instances {
		body := instance.body
		previousInfo, previousBody, previousCallbacks := e.info, e.instanceBody, e.boundCallbacks
		e.info, e.instanceBody, e.boundCallbacks = body.Info, body, nil
		ft := &goast.FuncType{Params: e.params(node.Params), Results: e.params(node.Results)}
		declaration := &goast.FuncDecl{Name: goast.NewIdent(instance.name), Type: ft, Body: e.functionBody(ft, node.Body)}
		e.generatedName(instance.name, node)
		e.anchor(declaration, node)
		out = append(out, declaration)
		e.info, e.instanceBody, e.boundCallbacks = previousInfo, previousBody, previousCallbacks
	}
	return out
}
