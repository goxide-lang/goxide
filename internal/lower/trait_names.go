package lower

import (
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

const typeCarrierAnchor = "GoxideInternalTypeCarrierForSelf"

// Generated names and their owning files are fixed before emitting any file.
type traitProgram struct {
	impls             []*check.Impl
	carriers          map[*types.Named]*traitCarrier
	facades           map[string]*traitFacade
	claims            map[string]map[string]syntax.Node
	members           map[*types.Named]map[string]syntax.Node
	functions         map[*types.Signature]*traitFunction
	instances         map[string]*traitInstance
	templateImpls     map[*check.Impl]bool
	templateFunctions map[*syntax.FuncDecl]bool
	templateNames     map[*check.Impl]string
	legacyFunctions   map[*syntax.FuncDecl]bool
	helperFunctions   map[*syntax.FuncDecl]bool
	helperImpls       map[*check.Impl]bool
	inferredCalls     map[*syntax.FuncDecl]bool
}

type traitCarrier struct {
	typ   *types.Named
	name  string
	file  string
	impls []*check.Impl
}

type traitFacade struct {
	typ   *types.Named
	file  string
	impls []*check.Impl
}

func prepareTraits(info *check.Info) *traitProgram {
	p := &traitProgram{carriers: map[*types.Named]*traitCarrier{}, facades: map[string]*traitFacade{}, claims: map[string]map[string]syntax.Node{}, members: map[*types.Named]map[string]syntax.Node{}}
	if info == nil {
		return p
	}
	p.functions = prepareTraitFunctions(info)
	p.prepareInstances(info)
	for _, impl := range info.Impls {
		if len(impl.ConstParams) != 0 {
			if info.Demands != nil {
				for _, definition := range info.Demands.GenericImpls {
					if definition.Template == impl {
						p.impls = append(p.impls, definition)
					}
				}
			}
			continue
		}
		if info.Demands != nil && !parameterizedImpl(impl) {
			for _, selected := range info.Demands.Impls {
				if implTemplate(selected) == impl {
					impl = selected
					break
				}
			}
		}
		if impl.AliasOf == nil && (info.Demands == nil || !info.Demands.ImplTemplates[impl] || p.templateImpls[impl]) {
			p.impls = append(p.impls, impl)
		}
	}
	sort.Slice(p.impls, func(i, j int) bool {
		a, b := p.impls[i], p.impls[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Decl.Span().Start != b.Decl.Span().Start {
			return a.Decl.Span().Start < b.Decl.Span().Start
		}
		return implIdentity(info, a) < implIdentity(info, b)
	})
	p.prepareTemplateNames(info)
	seen := map[string]bool{}
	unique := p.impls[:0]
	for _, impl := range p.impls {
		if impl.Trait == nil {
			unique = append(unique, impl)
			continue
		}
		key := implIdentity(info, impl)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, impl)
		if carrierTarget(info, impl) {
			carrier := p.carriers[impl.Base]
			if carrier == nil {
				carrier = &traitCarrier{typ: impl.Base, name: "goxide" + exportedName(impl.Base.Obj().Name()) + "Ops", file: impl.File}
				p.carriers[impl.Base] = carrier
			}
			carrier.impls = append(carrier.impls, impl)
		}
		if impl.Base == nil || parameterizedImpl(impl) {
			continue
		}
		local := impl.Base.Obj().Pkg() != nil && impl.Base.Obj().Pkg().Path() == filepath.ToSlash(filepath.Dir(impl.File))
		if impl.Trait.Runtime && (!local || !goMethodReceiver(impl.Base)) {
			// Value enum views already have an As method on every variant.
			if local && info.EnumOf(impl.Base) != nil && impl.Capture == check.ValueRef {
				continue
			}
			key := filepath.ToSlash(filepath.Dir(impl.File)) + ":" + types.TypeString(impl.Base, nil)
			facade := p.facades[key]
			if facade == nil {
				facade = &traitFacade{typ: impl.Base, file: impl.File}
				p.facades[key] = facade
			}
		}
	}
	p.impls = unique
	for _, facade := range p.facades {
		for _, impl := range p.impls {
			if impl.Base == facade.typ && impl.Trait != nil && impl.Trait.Runtime && filepath.Dir(impl.File) == filepath.Dir(facade.file) {
				facade.impls = append(facade.impls, impl)
			}
		}
	}
	if info.Demands != nil {
		p.prepareFunctionEntries(info)
		p.prepareTemplateHelpers(info)
		p.prepareInferenceCalls(info)
	}
	return p
}

func carrierTarget(info *check.Info, impl *check.Impl) bool {
	return !parameterizedImpl(impl) && impl.Base != nil && impl.Base.TypeParams().Len() == 0 && types.Identical(types.Unalias(impl.Target), impl.Base) &&
		impl.Base.Obj().Pkg() != nil && impl.Base.Obj().Pkg().Path() == filepath.ToSlash(filepath.Dir(impl.File)) &&
		(goMethodReceiver(impl.Base) || info.EnumOf(impl.Base) != nil)
}

func goMethodReceiver(t *types.Named) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return false
	default:
		return true
	}
}

func exportedName(name string) string {
	name = strings.TrimLeft(name, "_")
	if name == "" {
		return "Value"
	}
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}

func privateName(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(first)) + name[size:]
}

func canonicalPackage(info *check.Info, pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	if path := info.PackagePaths[pkg.Path()]; path != "" {
		return path
	}
	return pkg.Path()
}

func implIdentity(info *check.Info, impl *check.Impl) string {
	impl = implTemplate(impl)
	target := types.TypeString(impl.Target, func(pkg *types.Package) string { return canonicalPackage(info, pkg) })
	owner := filepath.ToSlash(filepath.Dir(impl.File))
	if mapped := info.PackagePaths[owner]; mapped != "" {
		owner = mapped
	}
	trait := "inherent"
	if impl.Trait != nil {
		trait = impl.Trait.Marker
	}
	return owner + ":" + target + ":" + trait
}

func implSuffix(info *check.Info, impl *check.Impl) string {
	digest := sha256.Sum256([]byte(implIdentity(info, impl)))
	return fmt.Sprintf("%x", digest[:4])
}

func targetName(impl *check.Impl) string {
	if impl.Base == nil {
		return typeTargetName(impl.Target)
	}
	name := exportedName(impl.Base.Obj().Name())
	if pkg := impl.Base.Obj().Pkg(); pkg != nil && pkg.Path() != filepath.ToSlash(filepath.Dir(impl.File)) {
		name = exportedName(pkg.Name()) + name
	}
	return name
}

func typeTargetName(target types.Type) string {
	switch target := types.Unalias(target).(type) {
	case *types.Named:
		name := exportedName(target.Obj().Name())
		for index := 0; index < target.TypeArgs().Len(); index++ {
			name += typeTargetName(target.TypeArgs().At(index))
		}
		return name
	case *types.TypeParam:
		return exportedName(target.Obj().Name())
	case *types.Pointer:
		return typeTargetName(target.Elem()) + "Pointer"
	case *types.Slice:
		return typeTargetName(target.Elem()) + "Slice"
	case *types.Array:
		return fmt.Sprintf("%sArray%d", typeTargetName(target.Elem()), target.Len())
	case *types.Map:
		return "Map" + typeTargetName(target.Key()) + typeTargetName(target.Elem())
	case *types.Chan:
		direction := "Chan"
		if target.Dir() == types.RecvOnly {
			direction = "ReceiveChan"
		}
		if target.Dir() == types.SendOnly {
			direction = "SendChan"
		}
		return direction + typeTargetName(target.Elem())
	case *types.Signature:
		name := "Func"
		for index := 0; index < target.Params().Len(); index++ {
			name += typeTargetName(target.Params().At(index).Type())
		}
		name += "To"
		for index := 0; index < target.Results().Len(); index++ {
			name += typeTargetName(target.Results().At(index).Type())
		}
		return name
	case *types.Struct:
		return "Record"
	case *types.Interface:
		return "Interface"
	case *types.Basic:
		return exportedName(target.Name())
	default:
		return "Value"
	}
}

func traitName(trait *check.Trait) string { return exportedName(trait.Symbol.GoName) }
func traitFactory(impl *check.Impl) string {
	impl = implTemplate(impl)
	return "As" + targetName(impl) + traitName(impl.Trait)
}
func (e *emitter) traitFactoryName(impl *check.Impl) string {
	impl = implTemplate(impl)
	if parameterizedImpl(impl) {
		return "As" + e.traits.templateNames[impl] + "Func"
	}
	name := traitFactory(impl)
	if parameterizedImpl(impl) || impl.Base == nil || impl.Base.TypeParams().Len() != 0 || impl.Base.TypeArgs().Len() != 0 {
		name += "_" + implSuffix(e.info, impl)
	}
	return name
}
func traitAs(impl *check.Impl) string { return "As" + traitName(impl.Trait) }
func traitOperation(member *check.Member) string {
	return member.Trait.Marker + "_" + exportedName(member.GoName)
}
func traitWitness(trait *check.Trait, name string) string {
	return trait.Marker + "_Type" + exportedName(name)
}

func (e *emitter) traitWrapper(impl *check.Impl) string {
	impl = implTemplate(impl)
	if parameterizedImpl(impl) {
		return "goxide" + e.traits.templateNames[impl] + "View"
	}
	return "goxide" + targetName(impl) + traitName(impl.Trait) + "_" + implSuffix(e.info, impl)
}

func (e *emitter) traitFreeOperation(member *check.Member) string {
	impl := implTemplate(member.Impl)
	return "Goxide" + targetName(impl) + "_" + traitOperation(member) + "_" + implSuffix(e.info, impl)
}

func (e *emitter) inherentName(member *check.Member) string {
	impl := implTemplate(member.Impl)
	name := targetName(impl) + exportedName(member.GoName)
	if member.Name == "new" {
		name = "New" + targetName(impl)
	}
	if !nativeInherentImpl(impl) {
		name += "_" + implSuffix(e.info, impl)
	}
	if member.Symbol != nil && member.Symbol.Visibility != resolve.Public {
		name = privateName(name)
	}
	return name
}

func nativeInherentImpl(impl *check.Impl) bool {
	if impl == nil {
		return false
	}
	return check.CanUseGoReceiver(implTemplate(impl))
}

func carrierExportedName(carrier *traitCarrier) string { return exportedName(carrier.name) }

func (e *emitter) generatedName(name string, node syntax.Node) bool {
	pkg := filepath.ToSlash(filepath.Dir(e.name))
	identity := pkg + "\x00" + e.tree.Package.Name
	claims := e.traits.claims[identity]
	if claims == nil {
		claims = map[string]syntax.Node{}
		e.traits.claims[identity] = claims
	}
	if previous := claims[name]; previous != nil && previous != node {
		e.fail(node, "生成的 Go 名字 "+name+" 与另一生成入口冲突")
		return false
	}
	for _, source := range e.result.Packages {
		if filepath.ToSlash(source.Dir) != pkg || source.Name != e.tree.Package.Name {
			continue
		}
		for _, symbol := range source.Scope.Bindings {
			if symbol.GoName == name {
				e.fail(node, "生成的 Go 名字 "+name+" 与现有声明冲突")
				return false
			}
		}
	}
	for _, enum := range e.info.Enums {
		if !e.localTypePackage(enum.Type.Obj().Pkg()) {
			continue
		}
		for _, variant := range enum.Variants {
			if variant.GoName == name {
				e.fail(node, "生成的 Go 名字 "+name+" 与 enum 变体冲突")
				return false
			}
		}
	}
	claims[name] = node
	e.reserved[name] = true
	return true
}

func (e *emitter) generatedMember(target *types.Named, name string, impl *check.Impl) {
	node := impl.Decl
	fail := func(message string) {
		file := e.name
		e.name = impl.File
		e.fail(node, message)
		e.name = file
	}
	claims := e.traits.members[target]
	if claims == nil {
		claims = map[string]syntax.Node{}
		e.traits.members[target] = claims
	}
	if previous := claims[name]; previous != nil && previous != node {
		fail("生成的 Go 方法 " + name + " 与另一入口冲突")
		return
	}
	if member, _, _ := types.LookupFieldOrMethod(target, true, target.Obj().Pkg(), name); member != nil {
		fail("生成的 Go 方法 " + name + " 与现有成员冲突")
		return
	}
	if instance := e.info.EnumOf(target); instance != nil {
		for _, variant := range instance.Enum.Variants {
			for _, field := range variant.Fields {
				if field.GoName == name {
					fail("生成的 Go 方法 " + name + " 与 enum 变体 " + variant.Name + " 的载荷字段冲突")
					return
				}
			}
		}
	}
	claims[name] = node
}

// Import aliases are chosen while emitting source bodies, before these helpers.
func (e *emitter) reserveTraitNames() {
	for _, impl := range e.traits.impls {
		if !e.samePackage(impl.File) {
			continue
		}
		if impl.Trait == nil {
			for _, m := range impl.Methods {
				if m.Static || !nativeInherentImpl(impl) {
					e.reserved[e.inherentName(m)] = true
				}
			}
			continue
		}
		e.reserved[e.traitWrapper(impl)] = true
		if impl.Trait.Runtime {
			e.reserved[e.traitFactoryName(impl)] = true
		}
		if !carrierTarget(e.info, impl) {
			e.reserved[e.implOpsName(impl)] = true
			for _, m := range impl.Methods {
				e.reserved[e.traitFreeOperation(m)] = true
			}
		}
	}
	for _, carrier := range e.traits.carriers {
		if e.samePackage(carrier.file) {
			e.reserved[carrier.name] = true
			e.reserved[carrierExportedName(carrier)] = true
		}
	}
	for _, facade := range e.traits.facades {
		if e.samePackage(facade.file) {
			name := targetName(facade.impls[0])
			e.reserved["goxideFor"+name], e.reserved["For"+name] = true, true
		}
	}
	for _, item := range e.tree.Items {
		if fn, ok := item.(*syntax.FuncDecl); ok {
			for _, bound := range e.info.GenericBounds[fn] {
				for _, parameter := range bound.Projected {
					e.reserved[parameter.Obj().Name()] = true
				}
			}
		}
	}
}

func (e *emitter) sourceGlobal(file, name string) goast.Expr {
	if e.samePackage(file) {
		return goast.NewIdent(name)
	}
	pkg := filepath.ToSlash(filepath.Dir(file))
	if mapped := e.info.PackagePaths[pkg]; mapped != "" {
		pkg = mapped
	}
	return &goast.SelectorExpr{X: e.importName(strings.Split(pkg, "/"), "", 0), Sel: goast.NewIdent(name)}
}

func (p *traitProgram) prepareTemplateNames(info *check.Info) {
	p.templateNames = map[*check.Impl]string{}
	impls := append([]*check.Impl(nil), p.impls...)
	sort.Slice(impls, func(i, j int) bool { return implIdentity(info, impls[i]) < implIdentity(info, impls[j]) })
	reserved, used := map[string]bool{}, map[string]bool{}
	base := func(impl *check.Impl) string {
		name := typeTargetName(impl.Target)
		if impl.Trait != nil {
			name += traitName(impl.Trait)
		}
		return name
	}
	for _, impl := range impls {
		if parameterizedImpl(impl) {
			reserved[filepath.Dir(impl.File)+":"+base(impl)] = true
		}
	}
	for _, impl := range impls {
		if !parameterizedImpl(impl) {
			continue
		}
		name := base(impl)
		prefix := filepath.Dir(impl.File) + ":"
		if used[prefix+name] {
			for suffix := 2; ; suffix++ {
				candidate := fmt.Sprintf("%s%d", name, suffix)
				if !used[prefix+candidate] && !reserved[prefix+candidate] {
					name = candidate
					break
				}
			}
		}
		used[prefix+name] = true
		p.templateNames[impl] = name
	}
}
