package expand

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/derives"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

type attributeTestResolver struct {
	macros  map[string]Macro
	builtin map[string]derives.Definition
}

func (r *attributeTestResolver) LookupMacro(path *ast.Path, _ Scope) (Macro, error) {
	if macro, ok := r.macros[macroPathName(path)]; ok {
		return macro, nil
	}
	return Macro{}, fmt.Errorf("unknown macro %s", macroPathName(path))
}

func (r *attributeTestResolver) LookupBuiltinDerive(path *ast.Path, _ Scope) (derives.Definition, bool) {
	if definition, ok := r.builtin[macroPathName(path)]; ok {
		return definition, true
	}
	if len(path.Segments) == 1 {
		return derives.Builtin(path.Segments[0].Name.Name)
	}
	return derives.Definition{}, false
}

func newAttributeResolver(t *testing.T, entries []pm.Entry, helpers map[string][]string) *attributeTestResolver {
	t.Helper()
	resolver := &attributeTestResolver{macros: map[string]Macro{}, builtin: map[string]derives.Definition{}}
	for _, entry := range entries {
		file := expandFile(t, entry.Name+"_definition.gox", "pub func "+entry.Name+"() {}")
		kind := pm.Function
		if entry.Attribute != nil {
			kind = pm.Attribute
		} else if entry.Derive != nil {
			kind = pm.Derive
		}
		definition := &procmacro.Entry{Name: entry.Name, Kind: kind, Helpers: helpers[entry.Name], Function: file.Items[0].(*ast.FuncDecl), File: file}
		resolver.macros[entry.Name] = Macro{Proc: definition, File: file, Run: func(request pm.Request) ([]byte, error) {
			output, err := pm.Dispatch(request, entries)
			if err != nil {
				return nil, err
			}
			var response bytes.Buffer
			if err := pm.WriteResponse(&response, request, output); err != nil {
				return nil, err
			}
			return response.Bytes(), nil
		}}
	}
	return resolver
}

func expandAttributes(files []*ast.File, resolver Resolver) (*Result, error) {
	configs := map[*ast.File]config.Config{}
	for _, file := range files {
		configs[file] = config.Config{Environment: &config.Environment{Target: config.Target{OS: "linux", Arch: "amd64"}}}
	}
	return Expand(files, resolver, configs)
}

func procText(ctx pm.Context, source string) pm.Output {
	return pm.FromParse(public.ParseText(source, ctx.DefSite()))
}

func TestDerivesWithoutResolverRemainForSemanticChecking(t *testing.T) {
	for _, source := range []string{
		"#[derive(owner::Serialize)] #[serde(rename_all = \"snake_case\")] type Value struct { #[serde(rename = \"wire\")] Field int }",
		"#[derive(Default)] enum Value { #[default] Empty }",
		"#[derive(Unknown)] type Value int",
		"#[derive()] type Value int",
	} {
		file := expandFile(t, "deferred_derive.gox", source)
		before := printer.Source(file)
		result, err := expandAttributes([]*ast.File{file}, nil)
		if err != nil {
			t.Fatalf("derive must remain for semantic checking: %v", err)
		}
		if got := printer.Source(result.Files[0]); got != before {
			t.Fatalf("derive or helper changed without a source resolver:\n%s\nwant:\n%s", got, before)
		}
	}
	file := expandFile(t, "generated_derive.gox", "macro_rules! model { () => { #[derive(owner::Serialize)] type Value struct { #[serde(rename = \"wire\")] Field int } }; }\nmodel!()")
	result, err := expandAttributes([]*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := printer.Source(result.Files[0])
	if strings.Contains(got, "model!") || !strings.Contains(got, "#[derive(owner::Serialize)]") || !strings.Contains(got, "#[serde(rename = \"wire\")]") {
		t.Fatalf("generated derives or helpers were not preserved: %s", got)
	}
}

func TestProceduralAttributesReplaceBeforeBodyAndReclassify(t *testing.T) {
	var order []string
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "outer", Attribute: func(ctx pm.Context, args, item public.TokenStream) pm.Output {
			order = append(order, "outer")
			text := item.Display()
			for _, want := range []string{"inner", "Unknown", "missing", "Value"} {
				if !strings.Contains(text, want) {
					t.Errorf("attribute input omitted %q: %s", want, text)
				}
			}
			if strings.Contains(text, "outer") || strings.TrimSpace(args.Display()) != "7" {
				t.Errorf("attribute input or args = %q / %q", text, args.Display())
			}
			return procText(ctx, "#[next] type Value struct{}")
		}},
		{Name: "inner", Attribute: func(ctx pm.Context, _, _ public.TokenStream) pm.Output {
			order = append(order, "inner")
			return pm.Failure("removed attribute executed", ctx.CallSpan())
		}},
		{Name: "next", Attribute: func(_ pm.Context, _, item public.TokenStream) pm.Output {
			order = append(order, "next")
			return pm.Success(item)
		}},
	}, nil)
	file := expandFile(t, "attributes.gox", "#[outer(7)] #[inner] #[derive(Unknown)] type Value struct { Field missing!() }")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"outer", "next"}) || len(result.Files[0].Items) != 1 {
		t.Fatalf("order/output = %v / %s", order, printer.Source(result.Files[0]))
	}
	if len(result.Files[0].Items[0].(*ast.TypeDecl).Attrs) != 0 || len(file.Items[0].(*ast.TypeDecl).Attrs) != 3 {
		t.Fatal("attribute consumption changed the source or left executable attributes")
	}
}

func TestProceduralDerivesShareSnapshotAndConsumeSharedHelpers(t *testing.T) {
	var inputs, order []string
	entry := func(name string) pm.Entry {
		return pm.Entry{Name: name, Derive: func(ctx pm.Context, item public.TokenStream) pm.Output {
			order = append(order, name)
			inputs = append(inputs, item.Display())
			return procText(ctx, "const "+name+"Value = 1")
		}}
	}
	resolver := newAttributeResolver(t, []pm.Entry{entry("First"), entry("Second")}, map[string][]string{"First": {"shared"}, "Second": {"shared"}})
	file := expandFile(t, "derive.gox", "#[shared(item)] #[derive(First, Default, Second)] type Value struct { #[shared(field)] Field int }")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"First", "Second"}) || len(inputs) != 2 || inputs[0] != inputs[1] {
		t.Fatalf("derive order/snapshots = %v / %#v", order, inputs)
	}
	if strings.Contains(inputs[0], "derive") || strings.Contains(inputs[0], "FirstValue") || strings.Count(inputs[0], "shared") != 2 {
		t.Fatalf("derive snapshot = %s", inputs[0])
	}
	output := printer.Source(result.Files[0])
	if strings.Contains(output, "shared") || !strings.Contains(output, "#[derive(Default)]") || !strings.Contains(output, "FirstValue") || !strings.Contains(output, "SecondValue") {
		t.Fatalf("derive output = %s", output)
	}
	if len(result.Files[0].Items) != 3 || !strings.Contains(printer.Source(file), "shared") {
		t.Fatal("derive output order/count or source immutability failed")
	}
}

func TestProceduralHelperRetainsBuiltinConsumer(t *testing.T) {
	var input string
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "Custom", Derive: func(_ pm.Context, item public.TokenStream) pm.Output {
		input = item.Display()
		return pm.Success(public.New())
	}}}, map[string][]string{"Custom": {"default"}})
	file := expandFile(t, "shared_default.gox", "#[derive(Custom, Default)] enum Value { #[default] Empty }")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	value := result.Files[0].Items[0].(*ast.EnumDecl)
	if !strings.Contains(input, "default") || len(value.Variants[0].Attrs) != 1 || attributeName(value.Variants[0].Attrs[0]) != "default" {
		t.Fatalf("builtin helper was removed: %s / %s", input, printer.Source(value))
	}
}

func TestProceduralHelperAmbiguityIsCheckedBeforeAttributes(t *testing.T) {
	called := false
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "Custom", Derive: func(_ pm.Context, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
		{Name: "shared", Attribute: func(_ pm.Context, _, item public.TokenStream) pm.Output { called = true; return pm.Success(item) }},
		{Name: "erase", Attribute: func(_ pm.Context, _, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
	}, map[string][]string{"Custom": {"shared"}})
	for _, source := range []string{
		"#[shared] #[derive(Custom)] type Value int",
		"#[erase] #[derive(Custom)] type Value struct { #[shared] Field int }",
	} {
		_, err := expandAttributes([]*ast.File{expandFile(t, "ambiguous.gox", source)}, resolver)
		if err == nil || !strings.Contains(err.Error(), "both a derive helper and an executable attribute") || called {
			t.Fatalf("helper ambiguity executed macro or was accepted: called=%v err=%v", called, err)
		}
	}
	resolver.macros["macros::shared"] = resolver.macros["shared"]
	delete(resolver.macros, "shared")
	called = false
	_, err := expandAttributes([]*ast.File{expandFile(t, "qualified.gox", "#[macros::shared] #[shared] #[derive(Custom)] type Value int")}, resolver)
	if err != nil || !called {
		t.Fatalf("qualified executable attribute = %v, called=%v", err, called)
	}
}

func TestProceduralAttributesAllowRemovalBeforeUnresolvedDerive(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "erase", Attribute: func(_ pm.Context, _, _ public.TokenStream) pm.Output {
		return pm.Success(public.New())
	}}}, nil)
	file := expandFile(t, "erase.gox", "#[derive(Unknown)] #[erase] type Value int")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil || len(result.Files[0].Items) != 0 {
		t.Fatalf("deleted unresolved derive: %v", err)
	}
	_, err = expandAttributes([]*ast.File{expandFile(t, "unknown.gox", "#[derive(Unknown)] type Value int")}, resolver)
	if err == nil || !strings.Contains(err.Error(), "unknown derive") {
		t.Fatalf("unresolved final derive accepted: %v", err)
	}
}

func TestProceduralDeriveRejectsInvalidTargetsAndRegistrations(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "Custom", Derive: func(_ pm.Context, _ public.TokenStream) pm.Output { return pm.Success(public.New()) }}}, nil)
	for _, source := range []string{
		"#[derive(Custom)] func value() {}",
		"#[derive(Custom)] type (First int; Second string)",
	} {
		_, err := expandAttributes([]*ast.File{expandFile(t, "target.gox", source)}, resolver)
		if err == nil || !strings.Contains(err.Error(), "single named type declaration") {
			t.Fatalf("invalid derive target accepted: %s: %v", source, err)
		}
	}
	for _, output := range []string{
		"#[proc_macro] pub func generated() {}",
		"macro_rules! generated { () => {1}; }",
	} {
		resolver := newAttributeResolver(t, []pm.Entry{{Name: "replace", Attribute: func(ctx pm.Context, _, _ public.TokenStream) pm.Output { return procText(ctx, output) }}}, nil)
		_, err := expandAttributes([]*ast.File{expandFile(t, "register.gox", "#[replace] type Value int")}, resolver)
		if err == nil || !strings.Contains(err.Error(), "cannot generate") {
			t.Fatalf("generated registration accepted: %s: %v", output, err)
		}
	}
}

func TestHelperScopeDoesNotIncludeNestedDeclarations(t *testing.T) {
	file := expandFile(t, "nested.gox", "#[shared] func outer() { #[shared] type Inner struct { #[shared] Field int } }")
	item := file.Items[0]
	consumeHelpers(item, helperOccurrences(item, map[string]*helperConsumers{"shared": {pending: map[deriveConsumer]bool{}}}))
	var remaining []string
	ast.Walk(item, func(node ast.Node) bool {
		if attr, ok := node.(*ast.Attr); ok {
			remaining = append(remaining, attributeName(attr))
		}
		return true
	})
	sort.Strings(remaining)
	if !reflect.DeepEqual(remaining, []string{"shared", "shared"}) {
		t.Fatalf("outer helper leaked into nested declarations: %v", remaining)
	}
}

func TestProceduralItemSlots(t *testing.T) {
	for _, test := range []struct {
		name, source, output, wantError string
		position                        pm.Position
	}{
		{"file list", "#[replace] type Value int", "type First int; type Second int", "", pm.ItemsPosition},
		{"single preserved", "declare #[replace] func value()", "func value()", "", pm.ItemPosition},
		{"single removed", "declare #[replace] func value()", "", "exactly one item", pm.ItemPosition},
		{"single expanded", "declare #[replace] func value()", "func first(); func second()", "exactly one item", pm.ItemPosition},
		{"trait associated", "trait Value { #[replace] func value(self Self) }", "type Output", "", pm.TraitMembersPosition},
		{"trait methods", "trait Value { #[replace] func value(self Self) }", "func first(self Self); func second(self Self)", "", pm.TraitMembersPosition},
		{"trait category", "trait Value { #[replace] func value(self Self) }", "var value = 1", "", pm.TraitMembersPosition},
		{"trait visibility", "trait Value { #[replace] func value(self Self) }", "pub func value(self Self)", "inherit visibility", pm.TraitMembersPosition},
		{"impl methods", "type Value int; impl Value { #[replace] func value(self Self) {} }", "func first(self Self) {}; func second(self Self) {}", "", pm.ImplMembersPosition},
		{"impl body", "type Value int; impl Value { #[replace] func value(self Self) {} }", "func value(self Self)", "requires a body", pm.ImplMembersPosition},
		{"impl type binding", "type Value int; impl Value { #[replace] func value(self Self) {} }", "type Output int", "requires a type binding", pm.ImplMembersPosition},
		{"trait impl visibility", "type Value int; impl Other for Value { #[replace] func value(self Self) {} }", "pub func value(self Self) {}", "inherit visibility", pm.ImplMembersPosition},
		{"default impl member", "type Value int; impl Default for Value { #[replace] func default() Value { return Value{} } }", "func default() Value { return Value{} }", "", pm.ImplMembersPosition},
	} {
		t.Run(test.name, func(t *testing.T) {
			var position pm.Position
			resolver := newAttributeResolver(t, []pm.Entry{{Name: "replace", Attribute: func(ctx pm.Context, _, _ public.TokenStream) pm.Output {
				return procText(ctx, test.output)
			}}}, nil)
			macro := resolver.macros["replace"]
			run := macro.Run
			macro.Run = func(request pm.Request) ([]byte, error) { position = request.Position(); return run(request) }
			resolver.macros["replace"] = macro
			_, err := expandAttributes([]*ast.File{expandFile(t, "slot.gox", test.source)}, resolver)
			if position != test.position {
				t.Errorf("request position = %q, want %q", position, test.position)
			}
			if test.name == "trait category" {
				if err == nil {
					t.Fatal("trait accepted variable output")
				}
				return
			}
			if test.wantError == "" && err != nil || test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("slot output: %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestProceduralDeriveEmptyOutputRetainsSingleDeclaration(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "Custom", Derive: func(_ pm.Context, _ public.TokenStream) pm.Output { return pm.Success(public.New()) }}}, nil)
	file := expandFile(t, "single_derive.gox", "declare #[derive(Custom)] type Value int")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil || len(result.Files[0].Items) != 1 {
		t.Fatalf("single declaration with empty derive: %v", err)
	}
}

func TestDeclarativeOutputRunsProceduralAttributesInFileList(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "replace", Attribute: func(ctx pm.Context, _, item public.TokenStream) pm.Output {
		if !strings.Contains(item.Display(), "Value") {
			t.Errorf("declarative output missing from attribute input: %s", item.Display())
		}
		return procText(ctx, "type First int; type Second int")
	}}}, nil)
	file := expandFile(t, "declarative_attribute.gox", "macro_rules! emit { () => { #[replace] type Value int }; }\nemit!()")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files[0].Items) != 2 || len(result.Expansions) != 2 || result.Expansions[2].Parent != 1 {
		t.Fatalf("declarative/procedural chain output = %s, expansions=%v", printer.Source(result.Files[0]), result.Expansions)
	}
}

func TestProceduralHelperPreclassificationContinuesAfterInvalidDerive(t *testing.T) {
	called := false
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "Custom", Derive: func(_ pm.Context, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
		{Name: "shared", Attribute: func(_ pm.Context, _, item public.TokenStream) pm.Output { called = true; return pm.Success(item) }},
	}, map[string][]string{"Custom": {"shared"}})
	_, err := expandAttributes([]*ast.File{expandFile(t, "invalid_before_helper.gox", "#[derive()] #[shared] #[derive(Custom)] type Value int")}, resolver)
	if err == nil || !strings.Contains(err.Error(), "both a derive helper") || called {
		t.Fatalf("malformed request prevented helper classification: %v, called=%v", err, called)
	}
}

func TestProceduralConfigurationSkipsAllExecution(t *testing.T) {
	called := false
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "replace", Attribute: func(_ pm.Context, _, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
		{Name: "Custom", Derive: func(_ pm.Context, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
	}, nil)
	file := expandFile(t, "cfg_proc.gox", "#[cfg(any())] #[replace] #[derive(Custom)] type Value int")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil || called || len(result.Files[0].Items) != 0 || len(result.Expansions) != 0 {
		t.Fatalf("configuration executed removed macros: called=%v error=%v", called, err)
	}
}

func TestProceduralDerivePreservesOriginalCommentsAndDeletionDropsThem(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "Custom", Derive: func(ctx pm.Context, _ public.TokenStream) pm.Output { return procText(ctx, "const Derived = 1") }},
		{Name: "erase", Attribute: func(_ pm.Context, _, _ public.TokenStream) pm.Output { return pm.Success(public.New()) }},
	}, nil)
	file := expandFile(t, "comments.gox", "// Value docs\n#[derive(Custom)] type Value struct { /* field */ Field int }\n// Gone docs\n#[erase] type Gone int\n// Kept docs\ntype Kept int")
	before := printer.Source(file)
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if printer.Source(file) != before {
		t.Fatal("attribute/derive changed original source comments")
	}
	output := printer.Source(result.Files[0])
	for _, text := range []string{"Value docs", "field", "Kept docs"} {
		if !strings.Contains(output, text) {
			t.Errorf("output lost %q: %s", text, output)
		}
	}
	if strings.Contains(output, "Gone docs") || strings.Contains(output, "type Gone") {
		t.Fatalf("deleted item's comments remain: %s", output)
	}
}

func TestProceduralAttributesDoNotExecuteInOtherPositions(t *testing.T) {
	called := false
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "execute", Attribute: func(_ pm.Context, _, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }}}, nil)
	for _, source := range []string{
		"type Value struct { #[execute] Field int }",
		"enum Value { #[execute] Empty }",
		"func main() { #[execute] value := 1 }",
	} {
		result, err := expandAttributes([]*ast.File{expandFile(t, "position.gox", source)}, resolver)
		if err != nil || called {
			t.Fatalf("unsupported position executed a macro: %s: called=%v error=%v", source, called, err)
		}
		var attrs int
		ast.Walk(result.Files[0], func(node ast.Node) bool {
			if _, ok := node.(*ast.Attr); ok {
				attrs++
			}
			return true
		})
		if attrs != 1 {
			t.Fatalf("unsupported attribute disappeared before ordinary checking: %s", source)
		}
	}
}

func TestProceduralAttributeRecursionUsesCommonBudget(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "again", Attribute: func(ctx pm.Context, _, _ public.TokenStream) pm.Output {
		return procText(ctx, "#[again] type Value int")
	}}}, nil)
	result, err := expandAttributes([]*ast.File{expandFile(t, "recursion.gox", "#[again] type Value int")}, resolver)
	if err == nil || !strings.Contains(err.Error(), "macro depth budget exceeded") || len(result.Expansions) != MaxDepth+1 {
		t.Fatalf("attribute recursion did not share depth budget: %v, expansions=%d", err, len(result.Expansions))
	}
}

func TestProceduralDeriveDoesNotConsumeHelpersGeneratedAfterSnapshot(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "Custom", Derive: func(_ pm.Context, item public.TokenStream) pm.Output {
		if strings.Contains(item.Display(), "shared") || !strings.Contains(item.Display(), "generated") {
			t.Errorf("derive did not receive the pre-body snapshot: %s", item.Display())
		}
		return pm.Success(public.New())
	}}}, map[string][]string{"Custom": {"shared"}})
	file := expandFile(t, "late_helper.gox", "macro_rules! generated { () => { struct { #[shared] Field int } }; }\n#[derive(Custom)] type Value generated!()")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	output := printer.Source(result.Files[0])
	if !strings.Contains(output, "#[shared]") {
		t.Fatalf("helper unseen by derive was silently consumed: %s", output)
	}
}
