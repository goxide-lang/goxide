package resolve_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type sourceMacroResolver map[string]*ast.File

func (sources sourceMacroResolver) LookupMacro(path *ast.Path, _ expand.Scope) (expand.Macro, error) {
	var names []string
	for _, segment := range path.Segments {
		names = append(names, segment.Name.Name)
	}
	file := sources[strings.Join(names, "::")]
	if file != nil {
		for _, item := range file.Items {
			if macro, ok := item.(*ast.MacroRulesDecl); ok && macro.Name.Name == names[len(names)-1] {
				return expand.Macro{Rules: macro, File: file}, nil
			}
		}
	}
	return expand.Macro{}, fmt.Errorf("missing source macro %s", strings.Join(names, "::"))
}

func expandForResolution(t *testing.T, files map[string]*ast.File, names []string, lookup expand.Resolver) *expand.Result {
	t.Helper()
	var input []*ast.File
	for _, name := range names {
		input = append(input, files[name])
	}
	result, err := expand.Expand(input, lookup)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func expansionIdents(file *ast.File, name string) []*ast.Ident {
	var ids []*ast.Ident
	ast.Walk(file, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Name == name {
			ids = append(ids, id)
		}
		return true
	})
	return ids
}

func TestResolveExpandedTemplateBindingsAndCaptures(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! add { ($x:expr) => { func() int { saved := 1; return saved + $x }() }; }
func run() { saved := 10; _ = add!(saved); _ = add!(saved) }
`})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	var caller *resolve.Symbol
	locals := make(map[ast.SyntaxCtx]*resolve.Symbol)
	for _, id := range expansionIdents(file.AST, "saved") {
		if symbol := file.Definitions[id.ID()]; symbol != nil {
			if id.Ctx == 0 {
				caller = symbol
			} else {
				locals[id.Ctx] = symbol
			}
		}
	}
	if caller == nil || len(locals) != 2 || len(result.Contexts) != 2 {
		t.Fatalf("caller/local/context counts = %v/%d/%d", caller, len(locals), len(result.Contexts))
	}
	var captures, templates int
	for _, id := range expansionIdents(file.AST, "saved") {
		if reference, ok := file.References[id.ID()]; ok {
			want := caller
			if id.Ctx != 0 {
				want = locals[id.Ctx]
				templates++
			} else {
				captures++
			}
			if reference.Symbol == nil || reference.Symbol != want {
				t.Fatalf("saved context %d resolved to %v, want %v", id.Ctx, reference.Symbol, want)
			}
		}
	}
	if captures != 2 || templates != 2 {
		t.Fatalf("capture/template references = %d/%d", captures, templates)
	}
	for ctx := range locals {
		scope := result.Contexts[ctx]
		if scope == nil || scope.Parent != file.Scope || scope.Caller == nil || scope.Parent == scope.Caller {
			t.Fatalf("context %d has no distinct definition/caller scopes: %#v", ctx, scope)
		}
		var binding *resolve.Symbol
		for callerScope := scope.Caller; callerScope != nil; callerScope = callerScope.Parent {
			if binding = callerScope.Bindings[resolve.Key{Name: "saved"}]; binding != nil {
				break
			}
		}
		if binding != caller {
			t.Fatalf("context %d caller chain does not reach the call-site binding", ctx)
		}
	}
}

func TestResolveExpandedRepeatedBlocksKeepLexicalBindings(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! blocks { ($($x:expr),*) => { $({ temporary := $x; _ = temporary; };)* }; }
func run() { blocks!(1, 2); blocks!(3) }
`})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	seen := make(map[*resolve.Symbol]bool)
	var current *resolve.Symbol
	var refs int
	for _, id := range expansionIdents(file.AST, "temporary") {
		if symbol := file.Definitions[id.ID()]; symbol != nil {
			if seen[symbol] {
				t.Fatal("separate blocks shared a declaration symbol")
			}
			seen[symbol], current = true, symbol
		} else if reference, ok := file.References[id.ID()]; ok {
			refs++
			if current == nil || reference.Symbol != current {
				t.Fatal("repeated block reference escaped its lexical block")
			}
		}
	}
	if len(seen) != 3 || refs != 3 {
		t.Fatalf("declarations/references = %d/%d", len(seen), refs)
	}
}

func TestResolveExpandedDefinitionAndCallerScopesAcrossPackages(t *testing.T) {
	sources := map[string]string{
		"defs/macros.gox": `package definitions
use definition_tools as helpers;
var value = 1
macro_rules! scoped { () => { value + $caller::value + helpers::run() + $caller::helpers::run() }; }
`,
		"app/main.gox": `package app
use caller_tools as helpers;
func run() { value := 2; _ = defs::scoped!() }
`,
	}
	files := parseFiles(t, sources)
	expanded := expandForResolution(t, files, []string{"app/main.gox"}, sourceMacroResolver{"defs::scoped": files["defs/macros.gox"]})
	importer := testImporter{
		"definition_tools": {{Name: resolve.Key{Name: "run"}, Kind: resolve.Function, GoName: "DefinitionRun", Visibility: resolve.Public}},
		"caller_tools":     {{Name: resolve.Key{Name: "run"}, Kind: resolve.Function, GoName: "CallerRun", Visibility: resolve.Public}},
	}
	result := resolve.ResolveExpanded(files, expanded, importer)
	noDiagnostics(t, result)
	definition := result.Files["defs/macros.gox"]
	definitionID := named(t, files["defs/macros.gox"], sources["defs/macros.gox"], "value = 1", "value")
	definitionValue := definition.Definitions[definitionID.ID()]
	caller := result.Files["app/main.gox"]
	var callerValue *resolve.Symbol
	for _, id := range expansionIdents(caller.AST, "value") {
		if symbol := caller.Definitions[id.ID()]; symbol != nil {
			callerValue = symbol
		}
	}
	if definitionValue == nil || callerValue == nil || definitionValue == callerValue {
		t.Fatal("definition and caller values were not distinct")
	}
	for _, id := range expansionIdents(caller.AST, "value") {
		if reference, ok := caller.References[id.ID()]; ok {
			want := definitionValue
			if id.Ctx == 0 {
				want = callerValue
			}
			if reference.Symbol != want {
				t.Fatalf("value context %d resolved to %v, want %v", id.Ctx, reference.Symbol, want)
			}
		}
	}
	var paths []string
	for _, id := range expansionIdents(caller.AST, "run") {
		if reference, ok := caller.References[id.ID()]; ok {
			if reference.Symbol == nil {
				t.Fatalf("qualified function unresolved: %#v", reference)
			}
			paths = append(paths, reference.Symbol.GoName)
		}
	}
	if !reflect.DeepEqual(paths, []string{"DefinitionRun", "CallerRun"}) {
		t.Fatalf("definition/caller use aliases resolved to %v", paths)
	}
}

func TestResolveExpandedNestedCallerFindsOuterTemplateLocal(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! caller_value { () => { $caller::value }; }
macro_rules! outer { () => { func() int { value := 3; return caller_value!() }() }; }
func run() { value := 1; _ = outer!(); _ = value }
`})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	bindings := make(map[ast.SyntaxCtx]*resolve.Symbol)
	for _, id := range expansionIdents(file.AST, "value") {
		if symbol := file.Definitions[id.ID()]; symbol != nil {
			bindings[id.Ctx] = symbol
		}
	}
	if len(bindings) != 2 || bindings[0] == nil {
		t.Fatalf("outer template and source value bindings = %#v", bindings)
	}
	var templateRefs, sourceRefs int
	for _, id := range expansionIdents(file.AST, "value") {
		if reference, ok := file.References[id.ID()]; ok {
			if reference.Symbol == nil || reference.Symbol != bindings[id.Ctx] {
				t.Fatalf("nested caller escaped outer template context %d: %#v", id.Ctx, reference)
			}
			if id.Ctx == 0 {
				sourceRefs++
			} else {
				templateRefs++
			}
		}
	}
	if templateRefs != 1 || sourceRefs != 1 {
		t.Fatalf("nested caller/source references = %d/%d", templateRefs, sourceRefs)
	}
}

func TestResolveExpandedIntroductionsJoinCallerPackage(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"defs/macros.gox": `package definitions
macro_rules! emit_decl { () => { pub func generated() int { return generated() } }; }
`,
		"app/main.gox": "package app\ndefs::emit_decl!()\n",
		"app/use.gox":  "package app\nfunc useGenerated() { _ = generated() }\n",
	})
	expanded := expandForResolution(t, files, []string{"app/main.gox", "app/use.gox"}, sourceMacroResolver{"defs::emit_decl": files["defs/macros.gox"]})
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	var symbol *resolve.Symbol
	for id, introduced := range expanded.Introductions {
		if id.Name == "generated" {
			symbol = result.Files["app/main.gox"].Definitions[id.ID()]
			if symbol == nil || symbol.Name != (resolve.Key{Name: "generated"}) || introduced.Ctx == 0 || symbol.Visibility != resolve.Public || symbol.GoName != "Generated" {
				t.Fatalf("generated public declaration = %#v, introduction = %#v", symbol, introduced)
			}
		}
	}
	if symbol == nil {
		t.Fatal("missing generated declaration introduction")
	}
	for _, name := range []string{"app/main.gox", "app/use.gox"} {
		file := result.Files[name]
		var refs int
		for _, id := range expansionIdents(file.AST, "generated") {
			if reference, ok := file.References[id.ID()]; ok {
				refs++
				if reference.Symbol != symbol {
					t.Fatalf("%s generated reference = %#v, want %#v", name, reference.Symbol, symbol)
				}
			}
		}
		if refs != 1 {
			t.Fatalf("%s generated references = %d", name, refs)
		}
	}
	for _, pkg := range result.Packages {
		got := pkg.Scope.Bindings[resolve.Key{Name: "generated"}]
		if pkg.Name == "app" && got != symbol || pkg.Name == "definitions" && got != nil {
			t.Fatalf("generated declaration belongs to wrong package %s", pkg.Name)
		}
	}
}

func TestResolveExpandedIntroductionsUseOrdinaryConflictRules(t *testing.T) {
	for _, tc := range []struct {
		name, generated, written string
	}{
		{"same name", "func same() {}", "func same() {}"},
		{"mapped Go name", "pub func lower() {}", "func Lower() {}"},
		{"unexportable public name", "pub func _hidden() {}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package p\nmacro_rules! emit_decl { () => { " + tc.generated + " }; }\nemit_decl!()\n" + tc.written
			files := parseFiles(t, map[string]string{"p.gox": source})
			expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
			result := resolve.ResolveExpanded(files, expanded, nil)
			ordinary := resolve.Resolve(parseFiles(t, map[string]string{"p.gox": "package p\n" + tc.generated + "\n" + tc.written}), nil)
			if len(result.Diagnostics) == 0 || len(result.Diagnostics) != len(ordinary.Diagnostics) {
				t.Fatalf("expanded/ordinary conflict diagnostics = %v / %v", result.Diagnostics, ordinary.Diagnostics)
			}
			for i, diagnostic := range result.Diagnostics {
				if diagnostic.Code != ordinary.Diagnostics[i].Code || diagnostic.Message != ordinary.Diagnostics[i].Message {
					t.Fatalf("expanded conflict differs from handwritten rule: %v / %v", diagnostic, ordinary.Diagnostics[i])
				}
			}
		})
	}
}

func TestResolveExpandedDuplicateIntroductionKeepsCallerReference(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"defs/macros.gox": "package definitions\nmacro_rules! emit_decl { () => { func same() { same() } }; }\n",
		"app/a.gox":       "package app\nfunc same() {}\n",
		"app/b.gox":       "package app\ndefs::emit_decl!()\n",
	})
	expanded := expandForResolution(t, files, []string{"app/a.gox", "app/b.gox"}, sourceMacroResolver{"defs::emit_decl": files["defs/macros.gox"]})
	result := resolve.ResolveExpanded(files, expanded, nil)
	ordinary := resolve.Resolve(parseFiles(t, map[string]string{
		"app/a.gox": "package app\nfunc same() {}\n",
		"app/b.gox": "package app\nfunc same() { same() }\n",
	}), nil)
	if len(result.Diagnostics) != 1 || len(ordinary.Diagnostics) != 1 || result.Diagnostics[0].Code != ordinary.Diagnostics[0].Code || result.Diagnostics[0].Message != ordinary.Diagnostics[0].Message {
		t.Fatalf("duplicate introduction must report only the ordinary duplicate: %v / %v", result.Diagnostics, ordinary.Diagnostics)
	}
	written := result.Files["app/a.gox"]
	declarations := expansionIdents(written.AST, "same")
	if len(declarations) != 1 || written.Definitions[declarations[0].ID()] == nil {
		t.Fatal("missing handwritten declaration")
	}
	want := written.Definitions[declarations[0].ID()]
	generated := result.Files["app/b.gox"]
	var references int
	for _, id := range expansionIdents(generated.AST, "same") {
		if reference, ok := generated.References[id.ID()]; ok {
			references++
			if reference.Symbol != want {
				t.Fatalf("duplicate template reference = %#v, want handwritten declaration %#v", reference.Symbol, want)
			}
		}
	}
	if references != 1 {
		t.Fatalf("generated body references = %d", references)
	}
}

func TestResolveExpandedMissingDefinitionNameKeepsOriginAndStack(t *testing.T) {
	sources := map[string]string{
		"defs/macros.gox": `package definitions
macro_rules! inner { () => { missing }; }
macro_rules! outer { () => { inner!() }; }
`,
		"app/main.gox": "package app\nvar missing = 1\nvar result = defs::outer!()\n",
	}
	files := parseFiles(t, sources)
	expanded := expandForResolution(t, files, []string{"app/main.gox"}, sourceMacroResolver{
		"defs::outer": files["defs/macros.gox"], "inner": files["defs/macros.gox"],
	})
	result := resolve.ResolveExpanded(files, expanded, nil)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one unresolved definition name, got %v", result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	start := uint32(strings.Index(sources["defs/macros.gox"], "missing"))
	span := ast.Span{Start: start, End: start + uint32(len("missing"))}
	if diagnostic.Code != "unresolved" || diagnostic.Severity != resolve.Error || diagnostic.File != "defs/macros.gox" || diagnostic.Span != span {
		t.Fatalf("unresolved diagnostic does not point at definition identifier: %#v", diagnostic)
	}
	if diagnostic.Origin.File != "defs/macros.gox" || diagnostic.Origin.Span != span || diagnostic.Origin.Expn == 0 || !reflect.DeepEqual(diagnostic.Origin.Stack, []ast.ExpnID{1, 2}) {
		t.Fatalf("definition provenance or nested stack lost: %#v", diagnostic.Origin)
	}
	if diagnostic.Definition.File != files["defs/macros.gox"] || diagnostic.Definition.Span != span {
		t.Fatalf("definition source location = %#v", diagnostic.Definition)
	}
	invocation := expanded.Expansions[diagnostic.Origin.Expn]
	if diagnostic.Call != invocation.Call || expanded.Expansions[diagnostic.Origin.Stack[0]].Call.File != files["app/main.gox"] {
		t.Fatalf("nested call or outer caller location lost: %#v", diagnostic)
	}
}

func TestResolveExpandedUnresolvedLocationsFollowDefinitionOrCapture(t *testing.T) {
	for _, tc := range []struct {
		name, template, argument, originFile string
	}{
		{"definition", "missing", "1", "defs/macros.gox"},
		{"capture", "$x", "missing", "app/main.gox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources := map[string]string{
				"defs/macros.gox": "package definitions\nmacro_rules! expression { ($x:expr) => { " + tc.template + " }; }\n",
				"app/main.gox":    "package app\nvar result = defs::expression!(" + tc.argument + ")\n",
			}
			files := parseFiles(t, sources)
			expanded := expandForResolution(t, files, []string{"app/main.gox"}, sourceMacroResolver{"defs::expression": files["defs/macros.gox"]})
			result := resolve.ResolveExpanded(files, expanded, nil)
			if len(result.Diagnostics) != 1 {
				t.Fatalf("unresolved diagnostics = %v", result.Diagnostics)
			}
			start := uint32(strings.Index(sources[tc.originFile], "missing"))
			span := ast.Span{Start: start, End: start + uint32(len("missing"))}
			diagnostic := result.Diagnostics[0]
			if diagnostic.File != tc.originFile || diagnostic.Span != span || diagnostic.Origin.File != tc.originFile || diagnostic.Origin.Span != span {
				t.Fatalf("unresolved %s points to wrong source: %#v", tc.name, diagnostic)
			}
			if diagnostic.Call != expanded.Expansions[1].Call || diagnostic.Call.File != files["app/main.gox"] || !reflect.DeepEqual(diagnostic.Origin.Stack, []ast.ExpnID{1}) {
				t.Fatalf("unresolved %s lost invocation provenance: %#v", tc.name, diagnostic)
			}
		})
	}
}

func TestResolveExpandedZeroContextsMatchesResolve(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"p/a.gox": `package p
use tools as tool;
pub func visible(x int) int { if x > 0 { y := x; return y }; return unknown }
struct Record { pub value int }
impl Record { pub func build() {} }
`,
		"p/b.gox": "package p\nfunc useIt() { _ = visible(1); _ = Record::build; missing!() }\n",
	})
	if got, want := resolve.ResolveExpanded(files, nil, nil), resolve.Resolve(files, nil); !reflect.DeepEqual(got, want) {
		t.Fatal("nil expansion changed the complete resolution result")
	}
	expanded := expandForResolution(t, map[string]*ast.File{"p/a.gox": files["p/a.gox"]}, []string{"p/a.gox"}, nil)
	plain := make(map[string]*ast.File)
	for name, file := range files {
		plain[name] = file
	}
	plain["p/a.gox"] = expanded.Files[0]
	if len(expanded.Contexts) != 0 {
		t.Fatal("zero-context fixture unexpectedly contains expansion contexts")
	}
	got, want := resolve.ResolveExpanded(files, expanded, nil), resolve.Resolve(plain, nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("zero contexts changed packages, definitions, references, visibility, Go names, scopes, uses, or diagnostics")
	}
}

func TestResolveUnexpandedMacroExpressionsUsePathsWithoutUndefined(t *testing.T) {
	source := `package p
use tools as imported;
macro_rules! known { () => { absent_inside_tokens }; }
var value = 1
func run() { known!(); value!(); absent!(); absent::nested!(); imported::unknown!() }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	for _, expanded := range []bool{false, true} {
		t.Run(fmt.Sprintf("expanded=%v", expanded), func(t *testing.T) {
			result := resolve.Resolve(files, nil)
			if expanded {
				result = resolve.ResolveExpanded(files, nil, nil)
			}
			noDiagnostics(t, result)
			file := result.Files["p.gox"]
			id := named(t, files["p.gox"], source, "known!()", "known")
			if symbol := file.References[id.ID()].Symbol; symbol == nil || symbol.Kind != resolve.Macro {
				t.Fatalf("unexpanded macro did not use macro path resolution: %#v", symbol)
			}
			id = named(t, files["p.gox"], source, "value!()", "value")
			if reference := file.References[id.ID()]; reference.Symbol != nil || !reference.Deferred {
				t.Fatalf("ordinary value entered macro namespace: %#v", reference)
			}
			ast.Walk(file.AST, func(node ast.Node) bool {
				if macro, ok := node.(*ast.MacroExpr); ok {
					if _, present := file.References[macro.ID()]; !present {
						t.Errorf("macro %s has no path result", macro.Path.Segments[0].Name.Name)
					}
				}
				return true
			})
		})
	}
}

func TestResolveExpandedKeepsPhaseDiagnosticsSeparate(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! bad { ($T:expr) => { func(value $T) {} }; }
var generated = bad!(int)
var unrelated = absent
`})
	expanded, err := expand.Expand([]*ast.File{files["p.gox"]}, nil)
	var expansionDiagnostic *expand.Diagnostic
	if !errors.As(err, &expansionDiagnostic) || expansionDiagnostic.Code != "category" {
		t.Fatalf("expected expand category diagnostic, got %v", err)
	}
	before := *expansionDiagnostic
	result := resolve.ResolveExpanded(files, expanded, nil)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unresolved" {
		t.Fatalf("resolver mixed phase diagnostics or lost its own error: %v", result.Diagnostics)
	}
	if !reflect.DeepEqual(before, *expansionDiagnostic) {
		t.Fatal("resolution changed the expansion diagnostic")
	}
}
