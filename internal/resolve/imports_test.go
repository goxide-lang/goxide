package resolve_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

type testImporter map[string][]resolve.External

func (i testImporter) Lookup(path []string, name resolve.Key) []resolve.External {
	var matches []resolve.External
	for _, candidate := range i[strings.Join(path, "::")] {
		if candidate.Name == name {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func TestUseBindingsAndAliasChains(t *testing.T) {
	source := `package p
use original as renamed;
use renamed::nested as nested;
use original::Item;
use extras::*;
use effects as _;
func f() { _ = renamed::F; _ = nested::Value; var x Item; _ = x }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	f := result.Files["p.gox"]
	if len(f.Uses) != 5 {
		t.Fatalf("got %d use bindings", len(f.Uses))
	}
	for index, want := range []struct {
		name        string
		path        []string
		glob, blank bool
	}{
		{"renamed", []string{"original"}, false, false},
		{"nested", []string{"original", "nested"}, false, false},
		{"Item", []string{"original", "Item"}, false, false},
		{"", []string{"extras"}, true, false},
		{"_", []string{"effects"}, false, true},
	} {
		got := f.Uses[index]
		if !reflect.DeepEqual(got.Target, want.path) || got.Glob != want.glob || got.Blank != want.blank || (!want.glob && got.Name.Name != want.name) {
			t.Errorf("use %d: %#v, want %#v", index, got, want)
		}
	}
	if _, exists := f.Scope.Aliases[resolve.Key{Name: "_"}]; exists {
		t.Fatal("blank use created a visible binding")
	}
	ref := named(t, files["p.gox"], source, "nested::Value", "Value")
	res := f.References[ref.ID()]
	if !res.Deferred || !reflect.DeepEqual(res.Path, []string{"original", "nested", "Value"}) {
		t.Fatalf("external lookup was not preserved: %#v", res)
	}
}

func TestPrivateAndPublicUsesAcrossFiles(t *testing.T) {
	sources := map[string]string{
		"a.gox": "package p\nuse localmod as privateAlias;\npub use sharedmod as shared;\nfunc a() { _ = privateAlias::F }",
		"b.gox": "package p\nfunc b() { _ = shared::F; _ = privateAlias::F }",
	}
	files := parseFiles(t, sources)
	result := resolve.Resolve(files, nil)
	privateRef := named(t, files["b.gox"], sources["b.gox"], "privateAlias::F", "privateAlias")
	diagnosticAt(t, result, "b.gox", privateRef.Span())
	sharedRef := named(t, files["b.gox"], sources["b.gox"], "shared::F", "F")
	if !result.Files["b.gox"].References[sharedRef.ID()].Deferred {
		t.Fatal("public use was not visible from another file")
	}
	if !result.Files["a.gox"].Uses[1].Public {
		t.Fatal("public use lost visibility")
	}
	file := result.Files["a.gox"]
	for _, tc := range []struct {
		index      int
		visibility resolve.Visibility
		goName     string
	}{{0, resolve.Private, "privateAlias"}, {1, resolve.Public, "Shared"}} {
		use := file.Uses[tc.index]
		if file.Visibility[use.Decl.ID] != tc.visibility || file.GoNames[use.Decl.ID] != tc.goName {
			t.Errorf("use visibility/Go name = %v/%q", file.Visibility[use.Decl.ID], file.GoNames[use.Decl.ID])
		}
	}
}

func TestPrivateUseAliasesCanDifferAcrossFiles(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"a.gox": "package p\nuse first as local;\nfunc a() { _ = local::F }",
		"b.gox": "package p\nuse second as local;\nfunc b() { _ = local::F }",
	})
	noDiagnostics(t, resolve.Resolve(files, nil))
}

func TestInvalidUseBindings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources map[string]string
	}{
		{"cycle", map[string]string{"a.gox": "package p\nuse b as a;\nuse a as b;"}},
		{"duplicate", map[string]string{"a.gox": "package p\nuse first as name;\nuse second as name;"}},
		{"duplicate public", map[string]string{"a.gox": "package p\npub use first as name;", "b.gox": "package p\npub use second as name;"}},
		{"type conflict", map[string]string{"a.gox": "package p\nuse other as Name;", "b.gox": "package p\ntype Name int"}},
		{"local type conflict", map[string]string{"a.gox": "package p\nuse other as Name;\nfunc f() { type Name int }"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := resolve.Resolve(parseFiles(t, tc.sources), nil)
			if len(result.Diagnostics) == 0 {
				t.Fatal("invalid use binding accepted")
			}
			for _, d := range result.Diagnostics {
				source := tc.sources[d.File]
				if d.Span.End <= d.Span.Start || d.Span.End > uint32(len(source)) {
					t.Fatalf("invalid diagnostic span: %v", d)
				}
			}
		})
	}
}

func TestLocalValueDoesNotHideStaticUseAlias(t *testing.T) {
	source := `package p
use strings;
func f(v int) {
 strings := v
 _ = strings.Text
 _ = strings::Repeat("x", 2)
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	f := result.Files["p.gox"]
	decl := named(t, files["p.gox"], source, "strings :=", "strings")
	value := named(t, files["p.gox"], source, "strings.Text", "strings")
	static := named(t, files["p.gox"], source, "strings::Repeat", "strings")
	if f.References[value.ID()].Symbol != f.Definitions[decl.ID()] {
		t.Fatal("selector base did not use local value binding")
	}
	if got := f.References[static.ID()].Symbol; got == nil || got != f.Uses[0].Symbol {
		t.Fatal("static path did not use import namespace")
	}
	field := named(t, files["p.gox"], source, "strings.Text", "Text")
	if got, exists := f.References[field.ID()]; exists && !got.Deferred {
		t.Fatal("value field was resolved without type information")
	}
}

func TestStaticLocalItems(t *testing.T) {
	source := `package p
enum E { A }
trait T { type Item; func method() }
struct S {}
impl S { pub func build() {} }
macro_rules! emit { () => { unresolved_inside_tokens }; }
func f() { _ = E::A; _ = T::method; _ = S::build; emit!() }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	for _, tc := range []struct {
		marker, name string
		kind         resolve.Kind
	}{
		{"E::A", "A", resolve.Variant},
		{"T::method", "method", resolve.Function},
		{"S::build", "build", resolve.Function},
		{"emit!()", "emit", resolve.Macro},
	} {
		id := named(t, files["p.gox"], source, tc.marker, tc.name).ID()
		if sym := result.Files["p.gox"].References[id].Symbol; sym == nil || sym.Kind != tc.kind {
			t.Errorf("%s did not resolve to kind %v: %#v", tc.marker, tc.kind, sym)
		}
	}
}

func TestGlobLookupAndAmbiguity(t *testing.T) {
	importer := testImporter{
		"first":  {{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, GoName: "Item", Visibility: resolve.Public}},
		"second": {{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, GoName: "Item", Visibility: resolve.Public}},
	}
	for _, tc := range []struct {
		name, uses string
		ambiguous  bool
	}{
		{"one glob", "use first::*;", false},
		{"two glob candidates", "use first::*; use second::*;", true},
		{"explicit wins", "use first::*; use second::*; use first::Item;", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package p\n" + tc.uses + "\nfunc f() { var value Item }"
			files := parseFiles(t, map[string]string{"p.gox": source})
			result := resolve.Resolve(files, importer)
			ident := named(t, files["p.gox"], source, "value Item", "Item")
			res := result.Files["p.gox"].References[ident.ID()]
			if tc.ambiguous {
				diagnosticAt(t, result, "p.gox", ident.Span())
				if len(res.Candidates) != 2 {
					t.Fatalf("ambiguity candidates = %#v", res.Candidates)
				}
			} else {
				noDiagnostics(t, result)
				if res.Symbol == nil {
					t.Fatal("imported item has no resolved symbol")
				}
			}
		})
	}
}

func TestImporterMissingMemberHasAccurateSpan(t *testing.T) {
	source := "package p\nuse remote;\nfunc f() { _ = remote::missing }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, testImporter{})
	ident := named(t, files["p.gox"], source, "missing", "missing")
	diagnosticAt(t, result, "p.gox", ident.Span())
}

func TestGlobWithoutImporterDefersUnknownNames(t *testing.T) {
	source := "package p\nuse remote::*;\nfunc f() { _ = unknown }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	ident := named(t, files["p.gox"], source, "unknown", "unknown")
	if !result.Files["p.gox"].References[ident.ID()].Deferred {
		t.Fatal("uninspected glob was treated as fully resolved")
	}
}

func TestHygienicUseAlias(t *testing.T) {
	source := "package p\nuse first as ns;\nuse second as ns;\nfunc f() { _ = ns::A; _ = ns::B }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	f := files["p.gox"]
	second := f.Items[1].(*ast.UseDecl)
	second.Tree.Alias.Ctx = 9
	ref := named(t, f, source, "ns::B", "ns")
	ref.Ctx = 9
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	if result.Files["p.gox"].References[ref.ID()].Symbol != result.Files["p.gox"].Uses[1].Symbol {
		t.Fatal("use alias lookup ignored syntax context")
	}
}

func TestImporterCanReportAmbiguousMember(t *testing.T) {
	source := "package p\nuse remote;\nfunc f() { _ = remote::Item }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	importer := testImporter{"remote": {
		{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, Visibility: resolve.Public},
		{Name: resolve.Key{Name: "Item"}, Kind: resolve.Function, Visibility: resolve.Public},
	}}
	result := resolve.Resolve(files, importer)
	ident := named(t, files["p.gox"], source, "remote::Item", "Item")
	diagnosticAt(t, result, "p.gox", ident.Span())
	if candidates := result.Files["p.gox"].References[ident.ID()].Candidates; len(candidates) != 2 {
		t.Fatalf("lost importer ambiguity candidates: %#v", candidates)
	}
}

func TestPackageExplicitUseWinsOverFileGlob(t *testing.T) {
	sources := map[string]string{
		"a.gox": "package p\npub use original::Item;",
		"b.gox": "package p\nuse other::*;\nfunc f() { var x Item }",
	}
	files := parseFiles(t, sources)
	importer := testImporter{"other": {{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, Visibility: resolve.Public}}}
	result := resolve.Resolve(files, importer)
	noDiagnostics(t, result)
	ident := named(t, files["b.gox"], sources["b.gox"], "x Item", "Item")
	if got := result.Files["b.gox"].References[ident.ID()].Symbol; got != result.Files["a.gox"].Uses[0].Symbol {
		t.Fatalf("file glob hid explicit package binding: %#v", got)
	}
}

func TestStaticLookupIgnoresGlobValue(t *testing.T) {
	sources := map[string]string{
		"a.gox": "package p\npub use original as space;",
		"b.gox": "package p\nuse values::*;\nfunc f() { _ = space::Member }",
	}
	files := parseFiles(t, sources)
	importer := testImporter{
		"values":   {{Name: resolve.Key{Name: "space"}, Kind: resolve.Variable, Visibility: resolve.Public}},
		"original": {{Name: resolve.Key{Name: "Member"}, Kind: resolve.Constant, Visibility: resolve.Public}},
	}
	result := resolve.Resolve(files, importer)
	noDiagnostics(t, result)
	ident := named(t, files["b.gox"], sources["b.gox"], "space::Member", "space")
	if got := result.Files["b.gox"].References[ident.ID()].Symbol; got != result.Files["a.gox"].Uses[0].Symbol {
		t.Fatalf("value glob hid a static namespace: %#v", got)
	}
}

func TestPublicUseConflictsWithPredeclaredValue(t *testing.T) {
	source := "package p\npub use external as len;"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	ident := named(t, files["p.gox"], source, "as len", "len")
	diagnosticAt(t, result, "p.gox", ident.Span())
}

func TestPackageAndFileGlobsBothContributeCandidates(t *testing.T) {
	sources := map[string]string{
		"a.gox": "package p\npub use first::*;",
		"b.gox": "package p\nuse second::*;\nfunc f() { var x Item }",
	}
	files := parseFiles(t, sources)
	importer := testImporter{
		"first":  {{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, Visibility: resolve.Public}},
		"second": {{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, Visibility: resolve.Public}},
	}
	result := resolve.Resolve(files, importer)
	ident := named(t, files["b.gox"], sources["b.gox"], "x Item", "Item")
	diagnosticAt(t, result, "b.gox", ident.Span())
	if got := result.Files["b.gox"].References[ident.ID()].Candidates; len(got) != 2 {
		t.Fatalf("package/file glob candidates = %#v", got)
	}
}

func TestRepeatedGlobDoesNotInventAmbiguity(t *testing.T) {
	sources := map[string]string{
		"a.gox": "package p\npub use shared::*;",
		"b.gox": "package p\nuse shared::*;\nfunc f() { var x Item }",
	}
	files := parseFiles(t, sources)
	importer := testImporter{"shared": {{Name: resolve.Key{Name: "Item"}, Kind: resolve.Type, Visibility: resolve.Public}}}
	result := resolve.Resolve(files, importer)
	noDiagnostics(t, result)
	ident := named(t, files["b.gox"], sources["b.gox"], "x Item", "Item")
	if result.Files["b.gox"].References[ident.ID()].Symbol == nil {
		t.Fatal("repeated import of the same glob member has no unique symbol")
	}
}

func TestDeclaredExternalMemberKeepsModuleOpen(t *testing.T) {
	source := "package p\nuse remote;\ndeclare func remote::Known();\nfunc f() { remote::Known(); remote::Other() }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	other := named(t, files["p.gox"], source, "remote::Other", "Other")
	if !result.Files["p.gox"].References[other.ID()].Deferred {
		t.Fatal("declare closed the external module's member table")
	}
}

func TestTypeAliasMemberLookupDefersInheritedMembers(t *testing.T) {
	source := `package p
type Base int
func (b Base) Method() {}
type Alias = Base
func f() { _ = Alias::Method }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	ident := named(t, files["p.gox"], source, "Alias::Method", "Method")
	resolved := result.Files["p.gox"].References[ident.ID()]
	if resolved.Symbol == nil && !resolved.Deferred {
		t.Fatal("type alias lost its inherited member before type checking")
	}
}
