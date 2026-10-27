package analysis_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func editorActions(t *testing.T, input analysis.Input, needle string) (*analysis.Result, []analysis.CodeAction) {
	t.Helper()
	input.Options.Editor = true
	result, err := analysis.Analyze(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	file := input.PackagePath + "/main.gox"
	text := string(input.Sources["main.gox"])
	offset := strings.Index(text, needle)
	if offset < 0 {
		t.Fatal("missing selection", needle)
	}
	actions, err := analysis.NewQueries(result).CodeActions(file, syntax.Span{Start: uint32(offset), End: uint32(offset)})
	if err != nil {
		t.Fatal(err)
	}
	return result, actions
}
func applyAction(t *testing.T, text string, actions []analysis.CodeAction, title string) string {
	t.Helper()
	for _, action := range actions {
		if action.Title != title {
			continue
		}
		edits := append([]analysis.SourceEdit(nil), action.Edits...)
		sort.Slice(edits, func(i, j int) bool { return edits[i].Span.Start > edits[j].Span.Start })
		for _, edit := range edits {
			text = text[:edit.Span.Start] + edit.NewText + text[edit.Span.End:]
		}
		return text
	}
	t.Fatalf("missing action %s: %#v", title, actions)
	return ""
}
func checkActionSource(t *testing.T, input analysis.Input, text string) {
	t.Helper()
	input.Sources["main.gox"] = []byte(text)
	input.Options.Editor = false
	result, err := analysis.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("edited source rejected: %v\n%s\n%v", err, text, result.Diagnostics)
	}
	if _, err := lower.Generate(t.Context(), result, lower.GenerationOptions{StableGo: true}); err != nil {
		t.Fatalf("edited source generation: %v\n%s", err, text)
	}
}
func TestImplActionsPreserveSignaturesAndExistingMethods(t *testing.T) {
	text := "package fixture\r\ntrait Read[T any] { type Item; func get(self &Self, value T) Self::Item; func put(self &mut Self, values ...T); func make() Self }\r\ntype Value struct{}\r\nimpl Read[int] for Value {\r\n type Item = string\r\n func make() Self { return Value{} }\r\n}\r\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
	result, actions := editorActions(t, input, "impl Read")
	if len(actions) != 3 {
		t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
	}
	edited := applyAction(t, text, actions, "Implement all missing members")
	for _, want := range []string{"self &Self, value int", "Self::Item", "self &mut Self, values ...int"} {
		if !strings.Contains(edited, want) {
			t.Fatalf("missing %s:\n%s", want, edited)
		}
	}
	if strings.Count(edited, "func make()") != 2 {
		t.Fatal("existing method duplicated", edited)
	}
	if strings.Contains(strings.ReplaceAll(edited, "\r\n", ""), "\n") {
		t.Fatal("mixed line endings", edited)
	}
	checkActionSource(t, input, edited)
}
func TestImplActionImportsSourceTypesAcrossPackages(t *testing.T) {
	text := "package fixture\nuse api::Read;\ntype Value struct{}\nimpl Read for Value {}\n"
	dependency := "package api\npub type data struct{}\npub trait Read { func read(self &Self, value data) data }\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Roots: paths.Roots{"api": {PackagePath: "dep", ModulePath: "dep"}}, Options: analysis.Options{SourcePackages: []analysis.SourcePackage{{PackagePath: "dep", ModulePath: "dep", RootPackage: "dep", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte(dependency)}}}}}
	result, actions := editorActions(t, input, "impl Read")
	if len(actions) != 1 {
		t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
	}
	edited := applyAction(t, text, actions, "Implement read")
	if !strings.Contains(edited, "use api::data;") || !strings.Contains(edited, "value data) data") {
		t.Fatalf("source names/import lost:\n%s", edited)
	}
	checkActionSource(t, input, edited)
}
func TestMatchActionsUseGuardedCoverageAndPayloadShapes(t *testing.T) {
	text := "package fixture\nenum Event { Idle, Pair(int, string), Data { number int } }\nfunc f(value Event) int { return match value { Event::Idle if true => 1, Event::Pair(_, _) => 2 } }\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
	result, actions := editorActions(t, input, "match value")
	if len(actions) != 1 {
		t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
	}
	edited := applyAction(t, text, actions, "Add missing match arms")
	if strings.Count(edited, "Event::Idle") != 2 || strings.Count(edited, "Event::Pair") != 1 || !strings.Contains(edited, "Event::Data { number: _ }") {
		t.Fatal(edited)
	}
	checkActionSource(t, input, edited)
	input.Sources["main.gox"] = []byte(strings.Replace(text, "Event::Idle if true => 1, Event::Pair(_, _) => 2", "_ => 1", 1))
	_, actions = editorActions(t, input, "match value")
	if len(actions) != 0 {
		t.Fatalf("wildcard actions=%#v", actions)
	}
}
func TestImplMissingAssociatedTypeIsExplicitPlaceholder(t *testing.T) {
	text := "package fixture\ntrait Read { type Item; func read(self Self) Self::Item }\ntype Value struct{}\nimpl Read for Value {}\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
	_, actions := editorActions(t, input, "impl Read")
	edited := applyAction(t, text, actions, "Implement all missing members")
	if !strings.Contains(edited, "type Item = __GoxideTODO_Item") {
		t.Fatal(edited)
	}
	edited = strings.Replace(edited, "__GoxideTODO_Item", "int", 1)
	checkActionSource(t, input, edited)
}
func TestImportActionsOfferOnlyReachablePublicSourceBindings(t *testing.T) {
	text := "package fixture\nfunc f() int { return answer() }\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Roots: paths.Roots{"api": {PackagePath: "dep", ModulePath: "dep"}}, Options: analysis.Options{SourcePackages: []analysis.SourcePackage{{PackagePath: "dep", ModulePath: "dep", RootPackage: "dep", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte("package api\npub func answer() int { return 42 }\n")}}}}}
	result, actions := editorActions(t, input, "answer()")
	if len(actions) != 1 {
		t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
	}
	edited := applyAction(t, text, actions, "Import api::answer")
	checkActionSource(t, input, edited)
}

func TestImplActionsQualifyNestedSourceTypesAndAvoidAliasCollisions(t *testing.T) {
	text := "package fixture\nuse api::Read;\ntype data struct{}\ntype Value struct{}\nimpl Read for Value {}\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Roots: paths.Roots{"api": {PackagePath: "dep", ModulePath: "dep"}, "records": {PackagePath: "model", ModulePath: "model"}}, Options: analysis.Options{SourcePackages: []analysis.SourcePackage{
		{PackagePath: "dep", ModulePath: "dep", RootPackage: "dep", Target: config.HostTarget(), Roots: paths.Roots{"model": {PackagePath: "model", ModulePath: "model"}}, Sources: map[string][]byte{"lib.gox": []byte("package api\nuse model;\npub trait Read { func read(self &Self, value model::data) model::data }\n")}},
		{PackagePath: "model", ModulePath: "model", RootPackage: "model", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte("package model\npub type data struct{}\n")}},
	}}}

	result, actions := editorActions(t, input, "impl Read")
	if len(actions) != 1 {
		t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
	}
	edited := applyAction(t, text, actions, "Implement read")
	if !strings.Contains(edited, "use records::data as data1;") || !strings.Contains(edited, "value data1) data1") {
		t.Fatalf("source alias collision:\n%s", edited)
	}
	checkActionSource(t, input, edited)
}

func TestMatchActionsSupportGenericEnumsAndCompositeResults(t *testing.T) {
	for _, text := range []string{
		"package fixture\nenum Event[T any] { Item(T), Empty }\nfunc f(value Event[int]) []int { return match value { Event[int]::Item(n) => []int{n} } }\n",
		"package fixture\nenum Event { Item, Empty }\nfunc f[T any](value Event, values []T) []T { return match value { Event::Item => values } }\n",
	} {
		input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
		result, actions := editorActions(t, input, "match value")
		if len(actions) != 1 {
			t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
		}
		edited := applyAction(t, text, actions, "Add missing match arms")
		checkActionSource(t, input, edited)
	}
}

func TestCodingActionsDoNotGuessUnavailableOrExpandedDeclarations(t *testing.T) {
	for _, text := range []string{
		"package fixture\ntrait Read{func read(self Self) int}\ntype Value struct{}\nmacro_rules! make {()=>{impl Read for Value{}};}\nmake!()\n",
		"package fixture\ntrait Read{func read(self Self) int}\ntype Value struct{}\nimpl Read for Value {\n",
	} {
		needle := "impl Read"
		if strings.Contains(text, "make!()") {
			needle = "make!()"
		}
		_, actions := editorActions(t, analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}, needle)
		if len(actions) != 0 {
			t.Fatalf("unsafe action on incomplete/generated declaration: %#v", actions)
		}
	}
}
func TestImportActionsRespectPrivateInternalAndShadowedRoots(t *testing.T) {
	for _, test := range []struct{ name, caller, pkg, decl string }{
		{"private", "package fixture\nfunc f()int{return answer()}\n", "dep", "func answer()int{return 1}"},
		{"internal", "package fixture\nfunc f()int{return answer()}\n", "other/internal/private", "pub func answer()int{return 1}"},
		{"shadowed-root", "package fixture\ntype api struct{}\nfunc f()int{return answer()}\n", "dep", "pub func answer()int{return 1}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(test.caller)}, PackagePath: "fixture", Roots: paths.Roots{"api": {PackagePath: test.pkg, ModulePath: "dep"}}, Options: analysis.Options{SourcePackages: []analysis.SourcePackage{{PackagePath: test.pkg, ModulePath: "dep", RootPackage: test.pkg, Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte("package api\n" + test.decl + "\n")}}}}}
			_, actions := editorActions(t, input, "answer()")
			if len(actions) != 0 {
				t.Fatalf("unreachable import action=%#v", actions)
			}
		})
	}
}

func TestMatchActionsKeepTrailingArmCommentsAndSeparators(t *testing.T) {
	for _, last := range []string{"Event::Idle => 1, // keep\n", "Event::Idle => 1 // keep\n", "Event::Idle => 1 /* keep */", "Event::Idle => 1, /* keep */"} {
		text := "package fixture\nenum Event {Idle, Empty}\nfunc f(value Event) int {return match value {" + last + "}}\n"
		input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
		_, actions := editorActions(t, input, "match value")
		edited := applyAction(t, text, actions, "Add missing match arms")
		if strings.Contains(last, "// keep") && !strings.Contains(edited, "1, // keep") || strings.Contains(last, "/* keep */") && !strings.Contains(edited, "1, /* keep */") {
			t.Fatalf("comment moved or invalid comma:\n%s", edited)
		}
		checkActionSource(t, input, edited)
	}
}

func TestMatchActionsForBuiltinOptionAndResult(t *testing.T) {
	for _, text := range []string{
		"package fixture\nfunc f(value Option[int]) int {return match value {Some(n)=>n}}\n",
		"package fixture\nfunc f(value Result[int,string]) int {return match value {Ok(n)=>n}}\n",
	} {
		input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
		result, actions := editorActions(t, input, "match value")
		if len(actions) != 1 {
			t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
		}
		edited := applyAction(t, text, actions, "Add missing match arms")
		checkActionSource(t, input, edited)
	}
}

func TestMatchActionsUseAuthoredReturnTypesForEmptyAndReferenceMatches(t *testing.T) {
	for _, text := range []string{
		"package fixture\nenum Event {Idle, Empty}\nfunc f(value Event) int {return match value {}}\n",
		"package fixture\nenum Event {Idle, Empty}\nfunc f(value Event, number &int) &int {return match value {Event::Idle => number}}\n",
	} {
		input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
		result, actions := editorActions(t, input, "match value")
		if len(actions) != 1 {
			t.Fatalf("actions=%#v diagnostics=%v", actions, result.Diagnostics)
		}
		edited := applyAction(t, text, actions, "Add missing match arms")
		if strings.Contains(text, "number &int") && !strings.Contains(edited, "func() &int") {
			t.Fatalf("reference return permission erased:\n%s", edited)
		}
		checkActionSource(t, input, edited)
	}
}
