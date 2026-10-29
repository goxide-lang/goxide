// Package check checks the language rules that are not expressed by Go syntax.
package check

import (
	"fmt"
	"go/constant"
	"go/importer"
	"go/types"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type Severity uint8

const Error Severity = 0

const (
	NonExhaustive       = "match-non-exhaustive"
	UnreachableArm      = "match-unreachable-arm"
	IncomparablePayload = "match-incomparable-payload"
	ArmTypeMismatch     = "match-arm-type"
	TryReturnType       = "try-return-type"
	GenericConstraint   = "generic-constraint"
	DeriveInvalid       = "derive-invalid"
	VariantMismatch     = "match-variant"
	EnumGoNameConflict  = "enum-go-name-conflict"
	EnumNilLiteral      = "enum-nil-literal"
	ReferencePermission = "reference-permission"
	ReferenceAddress    = "reference-address"
	ReferencePattern    = "reference-pattern"
	SourceVisibility    = "source-visibility"
)

type Diagnostic struct {
	File          string
	Span          ast.Span
	Code, Message string
	Severity      Severity
	Origin        resolve.Origin
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Span.Start, d.Span.End, d.Message)
}

type Field struct {
	Name, GoName string
	Type         types.Type
}

type Variant struct {
	Name, GoName string
	Decl         *ast.Variant
	Fields       []Field
}

type Enum struct {
	info         *Info
	Name, GoName string
	Decl         *ast.EnumDecl
	Type         *types.Named
	Variants     []*Variant
	Builtin      bool
}

type Instance struct {
	Enum *Enum
	Args []types.Type
}

func (i *Instance) Variant(name string) *Variant {
	for _, v := range i.Enum.Variants {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// Fields substitutes the enum's actual type arguments into a variant's payload.
func (i *Instance) Fields(v *Variant) []Field {
	return i.fields(v, nil)
}

func (i *Instance) fields(v *Variant, state *TypeState) []Field {
	fields := append([]Field(nil), v.Fields...)
	params := i.Enum.Type.TypeParams()
	sub := make(map[*types.TypeParam]types.Type)
	for j, arg := range i.Args {
		if j < params.Len() {
			sub[params.At(j)] = arg
		}
	}
	for j := range fields {
		if state != nil {
			fields[j].Type = state.Substitute(i.Enum.info, fields[j].Type, sub, nil)
		} else {
			fields[j].Type = substitute(fields[j].Type, sub, i.Enum.info)
		}
	}
	return fields
}

type RefMode uint8

const (
	ValueRef RefMode = iota
	ReadRef
	WriteRef
)

type Match struct {
	Enum      *Instance
	Missing   []*Variant // Variants not covered by the checked, unguarded patterns.
	Type      types.Type
	Statement bool
	Reference RefMode
}

type Constructor struct {
	Enum    *Instance
	Variant *Variant
}

type Try struct {
	Input, Output *Instance
	Value, Error  types.Type
	Conversion    *MemberSelection
}

type Info struct {
	traitApplications   map[*types.Named]*TraitBound
	BodyCases           map[*ast.FuncDecl][]*BodyCase
	ImplCases           map[*Impl][]*ImplCase
	parameterIdentities map[*types.TypeParam]string
	ArrayPatterns       map[*types.TypeParam]*ArrayPattern
	ConstParams         map[*resolve.Symbol]*ConstParam
	ConstValues         map[ast.Expr]ConstValue
	FunctionConstParams map[*types.Signature][]*ConstParam
	FunctionGenerics    map[*types.Signature]*ast.GenericParams
	EnumValid           map[*ast.CallExpr]*Instance
	EnumAssertions      map[*ast.CallExpr]types.Type
	GoResults           map[*ast.CallExpr]*GoResult
	Resolve             *resolve.Result
	// NativePackages shares complete pure Go package identities with GoImporter.
	NativePackages    map[string]*types.Package
	GoImporter        types.Importer
	GoSizes           types.Sizes
	Diagnostics       []Diagnostic
	Types             map[ast.Expr]types.Type
	Enums             map[*ast.EnumDecl]*Enum
	Matches           map[*ast.MatchExpr]*Match
	Tries             map[*ast.TryExpr]*Try
	Constructors      map[ast.Expr]*Constructor
	Symbols           map[*resolve.Symbol]types.Type
	PackagePaths      map[string]string
	Modules           map[*ast.ModDecl]*resolve.Package
	RefBindings       map[*ast.BindPat]RefMode
	Traits            map[*ast.TraitDecl]*Trait
	Impls             map[*ast.ImplDecl]*Impl
	ImplInstances     []*Impl
	Members           map[ast.Expr]*MemberSelection
	GoMembers         map[*ast.Ident]string
	TraitViews        map[ast.Expr]*TraitView
	TypeOverrides     map[ast.Type]types.Type
	GenericBounds     map[*ast.FuncDecl][]*TraitBound
	FunctionInstances map[ast.Node]*FunctionInstance
	Demands           *DemandPlan
	ConfigByFile      map[string]config.Config
	Sugar             []*SugarMember
	enums             map[*types.Named]*Enum
	typeNodes         map[ast.Type]types.Type
	goMemberNames     map[types.Object]string
}

func (r *Info) EnumOf(t types.Type) *Instance {
	if t == nil {
		return nil
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}
	e := r.enums[n.Origin()]
	if e == nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "github.com/goxide-lang/std/sum" {
		for _, builtin := range r.enums {
			if builtin.Builtin && builtin.Name == n.Obj().Name() && builtin.Type.TypeParams().Len() == n.Origin().TypeParams().Len() {
				e = builtin
				break
			}
		}
	}
	if e == nil {
		return nil
	}
	i := &Instance{Enum: e}
	for j := 0; j < n.TypeArgs().Len(); j++ {
		i.Args = append(i.Args, n.TypeArgs().At(j))
	}
	if len(i.Args) == 0 {
		for j := 0; j < n.TypeParams().Len(); j++ {
			i.Args = append(i.Args, n.TypeParams().At(j))
		}
	}
	return i
}

// Check checks isolated in-memory input, using synthetic Option/Result types.
// Module compilation supplies its real dependency graph through Options.Importer.
// The input trees and resolver tables are left unchanged.
func Check(files map[string]*ast.File, res *resolve.Result) (*Info, []Diagnostic) {
	return checkFiles(files, res, Options{})
}

// CheckWithImporter uses the source module's Go dependency graph for external types.
func CheckWithImporter(files map[string]*ast.File, res *resolve.Result, imports types.Importer, sizes types.Sizes) (*Info, []Diagnostic) {
	return CheckWithOptions(files, res, Options{Importer: imports, Sizes: sizes})
}

// Options describes the selected source graph. Configuration follows the file's
// owning distribution module, including declarations produced by macros.
type Options struct {
	Importer     types.Importer
	Sizes        types.Sizes
	ConfigByFile map[string]config.Config
}

func CheckWithOptions(files map[string]*ast.File, res *resolve.Result, options Options) (*Info, []Diagnostic) {
	return checkFiles(files, res, options)
}

func checkFiles(files map[string]*ast.File, res *resolve.Result, options Options) (*Info, []Diagnostic) {
	imports, sizes := options.Importer, options.Sizes
	if imports == nil {
		imports = importer.Default()
	}
	if sizes == nil {
		sizes = types.SizesFor("gc", runtime.GOARCH)
	}
	r := &Info{Resolve: res, Types: make(map[ast.Expr]types.Type), Enums: make(map[*ast.EnumDecl]*Enum), Matches: make(map[*ast.MatchExpr]*Match), Tries: make(map[*ast.TryExpr]*Try), Constructors: make(map[ast.Expr]*Constructor), Symbols: make(map[*resolve.Symbol]types.Type), enums: make(map[*types.Named]*Enum)}
	r.ArrayPatterns = map[*types.TypeParam]*ArrayPattern{}
	r.FunctionConstParams = map[*types.Signature][]*ConstParam{}
	r.FunctionGenerics = map[*types.Signature]*ast.GenericParams{}
	r.GoImporter = imports
	r.GoSizes = sizes
	r.ConfigByFile = options.ConfigByFile
	r.typeNodes = make(map[ast.Type]types.Type)
	r.GoResults = make(map[*ast.CallExpr]*GoResult)
	r.RefBindings = make(map[*ast.BindPat]RefMode)
	r.Traits = make(map[*ast.TraitDecl]*Trait)
	r.Impls = make(map[*ast.ImplDecl]*Impl)
	r.Members = make(map[ast.Expr]*MemberSelection)
	r.GoMembers = make(map[*ast.Ident]string)
	r.goMemberNames = make(map[types.Object]string)
	r.TraitViews = make(map[ast.Expr]*TraitView)
	r.TypeOverrides = make(map[ast.Type]types.Type)
	r.GenericBounds = make(map[*ast.FuncDecl][]*TraitBound)
	r.FunctionInstances = make(map[ast.Node]*FunctionInstance)
	r.Modules = make(map[*ast.ModDecl]*resolve.Package)
	if res == nil {
		return r, nil
	}
	c := &checker{out: r, files: files, importer: imports, named: make(map[*resolve.Symbol]*types.Named), variants: make(map[*resolve.Symbol]*Constructor), builtins: make(map[string]*Enum), packages: make(map[*resolve.Package]*types.Package), imported: make(map[string]*types.Package), constants: make(map[*resolve.Symbol]constant.Value), declarations: make(map[*resolve.Symbol]*typeDeclaration)}
	c.traits = make(map[*resolve.Symbol]*Trait)
	c.memberSymbols = make(map[*resolve.Symbol]*Member)
	c.bounds = make(map[*types.TypeParam][]*TraitBound)
	c.functionBounds = make(map[*types.Signature][]*TraitBound)
	c.sourceTypes = make(map[*resolve.Symbol]permissionDeclaration)
	c.sourceValues = make(map[*resolve.Symbol]sourceExpression)
	c.referenceModes = make(map[*resolve.Symbol]RefMode)
	sourcePackages := c.prepareNativePackages(options.Importer != nil)
	if len(r.Diagnostics) != 0 {
		return r.checkedResult()
	}
	c.standalone = options.Importer == nil
	r.PackagePaths = sourcePackagePaths(res)
	c.checkImportedVisibility()
	c.sourceModules()
	for _, p := range sourcePackages {
		c.pkg = types.NewPackage(p.Dir, p.Name)
		c.packages[p] = c.pkg
		for _, f := range p.Files {
			c.file = f
			c.collect(f.AST.Items)
		}
	}
	for _, p := range sourcePackages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.prepareGenerics(f.AST.Items)
		}
	}
	c.prepareTraits()
	c.prepareImpls()
	c.signaturePhase = 1
	for _, p := range sourcePackages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.signatures(f.AST.Items)
		}
	}
	if !c.certifyImpls() {
		// Member and body evidence require a certified declaration graph.
		// Stop here so derived missing-evidence errors cannot hide the actual
		// overlapping declarations, including conflicts in dependencies.
		return r.checkedResult()
	}
	c.checkTraitPrerequisiteCycles()
	c.finishImpls()
	c.signaturePhase = 2
	for _, p := range sourcePackages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.signatures(f.AST.Items)
		}
	}
	c.signaturePhase = 0
	c.referenceHints = c.newPermissionChecker()
	c.referenceHints.eachFile(func(items []ast.Item) { c.referenceHints.collect(items) })
	c.enumNames()
	c.checkRemainingAttributes()
	c.prepareGoSugar()
	c.validateConcreteImpls()
	for _, p := range sourcePackages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.globals(f.AST.Items)
		}
	}
	for _, implementations := range []bool{true, false} {
		for _, p := range sourcePackages {
			c.pkg = c.packages[p]
			for _, f := range p.Files {
				c.file = f
				c.checkItemPhase(f.AST.Items, implementations)
			}
		}
		if implementations {
			c.prepareImplCases()
			c.out.Sugar = nil
			c.prepareGoSugar()
		}
	}
	for _, instance := range c.instances {
		c.validateInstance(instance)
	}
	c.checkEqualityComparisons()
	c.checkReferences()
	if len(r.Diagnostics) == 0 {
		c.collectDemands()
	}
	return r.checkedResult()
}

func (r *Info) checkedResult() (*Info, []Diagnostic) {
	sort.SliceStable(r.Diagnostics, func(i, j int) bool {
		a, b := r.Diagnostics[i], r.Diagnostics[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Span.Start < b.Span.Start
	})
	return r, r.Diagnostics
}

type checker struct {
	sourceMembers          map[string][]memberLookup
	nativeMembers          map[string][]memberLookup
	pendingDeclarations    []pendingDeclaration
	pendingImpls           []pendingImpl
	pendingTraits          []*Trait
	traitRequirementsReady map[*Trait]bool
	expectedSources        map[ast.Expr]*permission
	returnSources          []*permission
	caseFunctions          map[*types.Signature]caseFunction
	signatureCases         map[*ast.FuncDecl]*bodyCaseState
	checkedFunctions       map[*ast.FuncDecl]uint8
	caseState              *bodyCaseState
	checkingBodyCase       bool
	signaturePhase         uint8
	constantStack          map[*resolve.Symbol]bool
	out                    *Info
	files                  map[string]*ast.File
	file                   *resolve.File
	pkg                    *types.Package
	packages               map[*resolve.Package]*types.Package
	named                  map[*resolve.Symbol]*types.Named
	variants               map[*resolve.Symbol]*Constructor
	builtins               map[string]*Enum
	builtinErrors          map[string]error
	standalone             bool
	imported               map[string]*types.Package
	importer               types.Importer
	returns                []types.Type
	constants              map[*resolve.Symbol]constant.Value
	instances              []genericInstance
	declarations           map[*resolve.Symbol]*typeDeclaration
	traits                 map[*resolve.Symbol]*Trait
	implOrder              []*Impl
	memberSymbols          map[*resolve.Symbol]*Member
	bounds                 map[*types.TypeParam][]*TraitBound
	activeBounds           []*TraitBound
	activeGoConstraints    []GoPredicate
	coherence              *coherenceState
	proofStack             []proofObligation
	proofFailure           string
	functionBounds         map[*types.Signature][]*TraitBound
	currentImpl            *Impl
	currentTrait           *Trait
	currentSelf            types.Type
	sourceTypes            map[*resolve.Symbol]permissionDeclaration
	sourceValues           map[*resolve.Symbol]sourceExpression
	referenceHints         *permissionChecker
	referenceModes         map[*resolve.Symbol]RefMode
	currentReceiver        *resolve.Symbol
	goInterop              *goInterop
}

func (c *checker) diag(n ast.Node, code, message string) {
	span := n.Span()
	for _, d := range c.out.Diagnostics {
		if d.File == c.file.Name && d.Span == span && d.Code == code && d.Message == message {
			return
		}
	}
	origin, ok := c.out.Resolve.Origins[resolve.Ref{File: c.file.Name, ID: n.ID()}]
	if !ok {
		origin = resolve.Origin{File: c.file.Name, Span: span}
	}
	c.out.Diagnostics = append(c.out.Diagnostics, Diagnostic{File: c.file.Name, Span: span, Code: code, Message: message, Origin: origin})
}

func (c *checker) symbol(n ast.Node) *resolve.Symbol {
	if n == nil {
		return nil
	}
	s := c.file.Definitions[n.ID()]
	if s == nil {
		s = c.file.References[n.ID()].Symbol
	}
	for s != nil && s.AliasOf != nil {
		s = s.AliasOf
	}
	// A namespace route can share its path with a private module declaration
	// or a value. Neither replaces the namespace selected by the importer.
	if s != nil && !c.namespaceSymbol(s) && len(s.Target) > 0 {
		if local := c.localSymbol(s.Target); local != nil {
			s = local
		}
	}
	if s == nil {
		s = c.localSymbol(c.file.References[n.ID()].Path)
	}
	return c.visibleSymbol(n, s)
}

func (c *checker) namespaceSymbol(symbol *resolve.Symbol) bool {
	if symbol == nil {
		return false
	}
	if symbol.Kind == resolve.Module {
		return true
	}
	if symbol.Kind == resolve.Import {
		if file := c.out.Resolve.Files[symbol.Decl.File]; file != nil {
			for _, use := range file.Uses {
				if use.Symbol == symbol && use.Resolved != nil && use.Resolved.Kind == resolve.Module {
					return true
				}
			}
		}
	}
	return false
}

func (c *checker) visibleSymbol(n ast.Node, s *resolve.Symbol) *resolve.Symbol {
	if s == nil || s.Visibility != resolve.Private || s.Decl.File == "" || c.sameSourcePackage(s.Decl.File, c.file.Name) {
		return s
	}
	// A macro may use its definition file's private import binding. The bound
	// runtime declaration is checked after following its canonical target.
	if file := c.out.Resolve.Files[s.Decl.File]; file != nil {
		for _, use := range file.Uses {
			if use.Symbol == s {
				return s
			}
		}
	}
	// Direct definition-site helpers retain their definition/call diagnostic in
	// lowering; imported paths in macro output still obey source visibility.
	origin := c.out.Resolve.Origins[resolve.Ref{File: c.file.Name, ID: n.ID()}]
	raw := c.file.References[n.ID()].Symbol
	if origin.Expn != 0 && raw != nil && raw.Decl.File != "" && len(raw.Target) == 0 {
		for raw.AliasOf != nil {
			raw = raw.AliasOf
		}
		if raw == s {
			return s
		}
	}
	c.diag(n, SourceVisibility, fmt.Sprintf("private source item %q is not visible from this package", s.Name.Name))
	return nil
}

func (c *checker) localSymbol(target []string) *resolve.Symbol {
	if len(target) < 2 {
		return nil
	}
	joined := strings.Join(target, "/")
	var found *resolve.Symbol
	for _, p := range c.out.Resolve.Packages {
		if p.IsExternalTest() {
			continue
		}
		dir := filepath.ToSlash(p.Dir)
		if !strings.HasPrefix(joined, dir+"/") {
			dir = c.out.PackagePaths[dir]
		}
		if dir == "" || !strings.HasPrefix(joined, dir+"/") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(joined, dir+"/"), "/")
		scope := p.Scope
		var symbol *resolve.Symbol
		for _, part := range parts {
			if scope == nil {
				symbol = nil
				break
			}
			symbol = scope.Bindings[resolve.Key{Name: part}]
			if symbol == nil {
				break
			}
			scope = symbol.Members
		}
		if symbol != nil {
			if found != nil && found != symbol {
				return nil
			}
			found = symbol
		}
	}
	return found
}

func valid(t types.Type) bool { return t != nil && t != types.Typ[types.Invalid] }
func defaultType(t types.Type) types.Type {
	if t == nil {
		return types.Typ[types.Invalid]
	}
	return types.Default(t)
}
