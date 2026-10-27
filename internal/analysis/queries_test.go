package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
)

func analyzeQueries(t *testing.T, sources map[string][]byte) (*analysis.Result, *analysis.Queries) {
	t.Helper()
	r, err := analysis.Analyze(context.Background(), analysis.Input{Sources: sources, PackagePath: "fixture", Options: analysis.Options{PreserveSyntax: true}})
	if err != nil {
		t.Fatal(err)
	}
	return r, analysis.NewQueries(r)
}

func TestSourceQueriesUseCurrentBindingsAndScopes(t *testing.T) {
	text := "package fixture\nfunc twice(value int)int{return value*2}\nfunc F(input int)int{first:=twice(input); {first:=\"nested\"; _=first}; later:=first; return later}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	key := "fixture/main.gox"
	offset := uint32(strings.Index(text, "twice(input)"))
	hover, err := queries.Hover(key, offset)
	if err != nil || hover == nil || hover.Text != "func twice(value int) int" {
		t.Fatalf("hover = %#v, %v", hover, err)
	}
	definitions, err := queries.Definition(key, offset)
	if err != nil || len(definitions) != 1 || int(definitions[0].Span.Start) != strings.Index(text, "twice(value") {
		t.Fatalf("definition = %#v, %v", definitions, err)
	}
	completions, err := queries.Complete(key, uint32(strings.Index(text, "_=first")+2))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, completion := range completions {
		found[completion.Name] = completion.Detail
	}
	if found["first"] != "variable first string" || found["input"] != "variable input int" || found["later"] != "" {
		t.Fatalf("scope completion = %#v", found)
	}
	symbols, err := queries.Symbols(key)
	if err != nil || len(symbols) != 2 || symbols[0].Name != "twice" || symbols[1].Name != "F" {
		t.Fatalf("symbols = %#v, %v", symbols, err)
	}
}

func TestDefinitionSelectsBusinessImplAndNativeDeclaration(t *testing.T) {
	text := "package fixture\ntrait P{func get(self Self)int}\ntype S struct{}\nimpl P for S{func get(self Self)int{return 1}}\nfunc F(s S)int{return s.get()+Native()}\n"
	native := "package fixture\nimport \"time\"\nvar when time.Time\nfunc Native()int{return 1}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text), "native.go": []byte(native)})
	definitions, err := queries.Definition("fixture/main.gox", uint32(strings.Index(text, "s.get()")+2))
	if err != nil || len(definitions) != 1 || int(definitions[0].Span.Start) != strings.Index(text, "get(self Self)int{return") {
		t.Fatalf("impl definition = %#v, %v", definitions, err)
	}
	definitions, err = queries.Definition("fixture/main.gox", uint32(strings.Index(text, "Native()")))
	if err != nil || len(definitions) != 1 || definitions[0].File != "fixture/native.go" || int(definitions[0].Span.Start) != strings.Index(native, "Native()") {
		t.Fatalf("native definition = %#v, %v; expected %d", definitions, err, strings.Index(native, "Native()"))
	}
}

func TestMacroDefinitionRetainsTemplateAndCallOrigins(t *testing.T) {
	text := "package fixture\nmacro_rules! make {()=>{func Generated()int{return 1}};}\nmake!()\nfunc F()int{return Generated()}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	definitions, err := queries.Definition("fixture/main.gox", uint32(strings.LastIndex(text, "Generated()")))
	if err != nil || len(definitions) != 1 {
		t.Fatalf("definition = %#v, %v", definitions, err)
	}
	location := definitions[0]
	if !location.Generated || location.MacroDefinition == nil || location.MacroCall == nil || int(location.Span.Start) != strings.Index(text, "Generated()") {
		t.Fatalf("macro origin = %#v", location)
	}
	definitions, err = queries.Definition("fixture/main.gox", uint32(strings.Index(text, "make!()")))
	if err != nil || len(definitions) != 1 || int(definitions[0].Span.Start) != strings.Index(text, "make {") {
		t.Fatalf("macro definition = %#v, %v", definitions, err)
	}
}

func TestMemberQueriesUseSourceFieldsAndNativeMethods(t *testing.T) {
	text := "package fixture\ntype S struct{value int}\nimpl S{func get(self Self)int{return self.value}}\nfunc F(s S,n Native)int{return s.value+n.Get()}\n"
	native := "package fixture\ntype Native struct{}\nfunc(Native)Get()int{return 1}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text), "native.go": []byte(native)})
	definitions, err := queries.Definition("fixture/main.gox", uint32(strings.LastIndex(text, "s.value")+2))
	if err != nil || len(definitions) != 1 || int(definitions[0].Span.Start) != strings.Index(text, "value int") {
		t.Fatalf("field definition = %#v, %v", definitions, err)
	}
	definitions, err = queries.Definition("fixture/main.gox", uint32(strings.Index(text, "n.Get()")+2))
	if err != nil || len(definitions) != 1 || definitions[0].File != "fixture/native.go" || int(definitions[0].Span.Start) != strings.Index(native, "Get()") {
		t.Fatalf("native method definition = %#v, %v", definitions, err)
	}
	completions, err := queries.Complete("fixture/main.gox", uint32(strings.LastIndex(text, "s.value")+2))
	found := map[string]bool{}
	for _, completion := range completions {
		found[completion.Name] = true
	}
	if err != nil || !found["value"] || !found["get"] || found["F"] {
		t.Fatalf("member completion = %#v, %v", completions, err)
	}
	symbols, err := queries.Symbols("fixture/main.gox")
	if err != nil || len(symbols) != 3 || len(symbols[0].Children) != 1 || symbols[0].Children[0].Name != "value" || len(symbols[1].Children) != 1 || symbols[1].Children[0].Name != "get" {
		t.Fatalf("symbol hierarchy = %#v, %v", symbols, err)
	}
}

func TestIncompleteSelectorCompletesKnownReceiver(t *testing.T) {
	text := "package fixture\ntype S struct{value int}\nfunc F(s S){s.}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}})
	if err != nil {
		t.Fatal(err)
	}
	completions, err := analysis.NewQueries(result).Complete("fixture/main.gox", uint32(strings.Index(text, "s.")+2))
	if err != nil || len(completions) != 1 || completions[0].Name != "value" {
		t.Fatalf("completion = %#v, %v; %v", completions, err, result.Diagnostics)
	}
}

func TestIncompleteStaticPathCompletesCurrentEnum(t *testing.T) {
	text := "package fixture\nenum Color{Red,Blue}\nfunc F(){Color::}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}})
	if err != nil {
		t.Fatal(err)
	}
	completions, err := analysis.NewQueries(result).Complete("fixture/main.gox", uint32(strings.Index(text, "Color::")+len("Color::")))
	if err != nil || len(completions) != 2 || completions[0].Name != "Blue" || completions[1].Name != "Red" {
		t.Fatalf("static completion = %#v, %v; %v", completions, err, result.Diagnostics)
	}
}

func TestSourcePackageDefinitionUsesAuthoredPublicName(t *testing.T) {
	text := "package fixture\nuse api::add;\nfunc F()int{return add()}\n"
	dependency := "package dep\npub func add()int{return 1}\n"
	result, err := analysis.Analyze(context.Background(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Roots: paths.Roots{"api": {PackagePath: "dep", ModulePath: "dep"}}, Options: analysis.Options{Editor: true, SourcePackages: []analysis.SourcePackage{{PackagePath: "dep", ModulePath: "dep", RootPackage: "dep", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte(dependency)}}}}})
	if err != nil || result.Err != nil {
		t.Fatalf("analysis %v / %v", err, result.Err)
	}
	definitions, err := analysis.NewQueries(result).Definition("fixture/main.gox", uint32(strings.LastIndex(text, "add()")))
	if err != nil || len(definitions) != 1 || definitions[0].File != "dep/lib.gox" || int(definitions[0].Span.Start) != strings.Index(dependency, "add()") {
		t.Fatalf("import definition = %#v, %v", definitions, err)
	}
}
