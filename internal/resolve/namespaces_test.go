package resolve_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestOrdinaryAndMacroDeclarationsHaveIndependentBindings(t *testing.T) {
	source := `package p
pub trait Debug { func debug() }
pub macro_rules! Debug { () => { 1 }; }
func f() { var value Debug; _ = value; Debug!() }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	for _, test := range []struct {
		marker    string
		namespace resolve.Namespace
		kind      resolve.Kind
	}{{"value Debug", resolve.OrdinaryNamespace, resolve.Trait}, {"Debug!()", resolve.MacroNamespace, resolve.Macro}} {
		name := named(t, files["p.gox"], source, test.marker, "Debug")
		symbol := file.References[name.ID()].Symbol
		if symbol == nil || symbol.Kind != test.kind || symbol.Name.Namespace != test.namespace {
			t.Fatalf("%s resolved to %#v", test.marker, symbol)
		}
		if binding := result.Packages[0].Scope.Bindings[resolve.Key{Name: "Debug", Namespace: test.namespace}]; binding != symbol {
			t.Fatalf("%s did not select its namespace binding", test.marker)
		}
	}
}

func TestSingleUseKeepsIndependentNamespaceTargets(t *testing.T) {
	ordinary := resolve.External{Name: resolve.Key{Name: "Debug"}, Kind: resolve.Trait, GoName: "Debug", Visibility: resolve.Public, Target: []string{"protocols", "Debug"}}
	macro := resolve.External{Name: resolve.Key{Name: "Debug", Namespace: resolve.MacroNamespace}, Kind: resolve.Macro, Visibility: resolve.Public, Target: []string{"derives", "Debug"}}
	importer := testImporter{"prelude": {ordinary, macro}, "protocols": {ordinary}, "derives": {macro}}
	source := `package p
use prelude::Debug;
use Debug as Renamed;
func f() { var value Renamed; _ = value; Renamed!() }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, importer)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	if len(file.Uses) != 2 {
		t.Fatalf("one syntax use must retain one entry: %d", len(file.Uses))
	}
	for _, use := range file.Uses {
		if use.Symbol == nil || use.Resolved == nil || use.Macro == nil || use.Macro.Symbol == nil || use.Macro.Resolved == nil {
			t.Fatalf("use lost a namespace: %#v", use)
		}
		if !reflect.DeepEqual(use.Target, ordinary.Target) || !reflect.DeepEqual(use.Macro.Target, macro.Target) {
			t.Fatalf("ordinary/macro targets overwritten: %v / %v", use.Target, use.Macro.Target)
		}
	}
	for _, test := range []struct {
		marker string
		kind   resolve.Kind
	}{{"value Renamed", resolve.Trait}, {"Renamed!()", resolve.Macro}} {
		name := named(t, files["p.gox"], source, test.marker, "Renamed")
		if symbol := file.References[name.ID()].Symbol; symbol == nil || symbol.Kind != test.kind {
			t.Fatalf("%s selected wrong namespace: %#v", test.marker, symbol)
		}
	}
}

func TestNamedUsesOnlyConflictWithinTheirNamespace(t *testing.T) {
	importer := testImporter{
		"values": {{Name: resolve.Key{Name: "Shared"}, Kind: resolve.Function, GoName: "Shared", Visibility: resolve.Public}},
		"macros": {{Name: resolve.Key{Name: "Shared", Namespace: resolve.MacroNamespace}, Kind: resolve.Macro, Visibility: resolve.Public}},
	}
	for _, uses := range []string{
		"use values::Shared; use macros::Shared;",
		"use macros::Shared; use values::Shared;",
		"use values::Shared as Selected; use macros::Shared as Selected;",
	} {
		result := resolve.Resolve(parseFiles(t, map[string]string{"p.gox": "package p\n" + uses}), importer)
		noDiagnostics(t, result)
		var ordinary, macros int
		for _, use := range result.Files["p.gox"].Uses {
			if use.Symbol != nil {
				ordinary++
			}
			if use.Macro != nil && use.Macro.Symbol != nil {
				macros++
			}
		}
		if ordinary != 1 || macros != 1 {
			t.Fatalf("unexpected namespace binding counts for %q: %d / %d", uses, ordinary, macros)
		}
	}
	for _, source := range []string{
		"use values::Shared; use values::Shared;",
		"use macros::Shared; use macros::Shared;",
		"use values::Shared; func Shared() {}",
		"use macros::Shared; macro_rules! Shared { () => { 1 }; }",
	} {
		result := resolve.Resolve(parseFiles(t, map[string]string{"p.gox": "package p\n" + source}), importer)
		found := false
		for _, diagnostic := range result.Diagnostics {
			found = found || diagnostic.Code == "duplicate-use" || diagnostic.Code == "use-conflict"
		}
		if !found {
			t.Fatalf("same-namespace conflict was accepted for %q: %v", source, result.Diagnostics)
		}
	}
}

func TestAliasChainFiltersUnavailableNamespaceBranches(t *testing.T) {
	importer := testImporter{
		"values": {{Name: resolve.Key{Name: "Shared"}, Kind: resolve.Function, GoName: "Shared", Visibility: resolve.Public}},
		"macros": {{Name: resolve.Key{Name: "Shared", Namespace: resolve.MacroNamespace}, Kind: resolve.Macro, Visibility: resolve.Public}},
	}
	for _, imports := range []string{
		"use Shared as Selected; use macros::Shared; use values::Shared;",
		"use Shared as Selected; use values::Shared; use macros::Shared;",
	} {
		source := "package p\n" + imports + "\nfunc f() { Selected(); Selected!() }"
		files := parseFiles(t, map[string]string{"p.gox": source})
		result := resolve.Resolve(files, importer)
		noDiagnostics(t, result)
		use := result.Files["p.gox"].Uses[0]
		if use.Resolved == nil || use.Resolved.Kind != resolve.Function || use.Macro == nil || use.Macro.Resolved == nil || use.Macro.Resolved.Kind != resolve.Macro {
			t.Fatalf("alias chain lost a namespace: %#v", use)
		}
	}
}

func TestMacroUseCandidatesSurviveDeferredImporter(t *testing.T) {
	for _, importer := range []resolve.Importer{nil, deferredNamespaceImporter{}} {
		for _, source := range []string{
			"package p\nuse unknown::Debug;",
			"package p\npub mod custom;use custom::Debug;",
		} {
			result := resolve.Resolve(parseFiles(t, map[string]string{"p.gox": source}), importer)
			noDiagnostics(t, result)
			use := result.Files["p.gox"].Uses[0]
			if use.Symbol == nil || use.Macro == nil || use.Macro.Symbol == nil {
				t.Fatalf("deferred importer lost unresolved namespace: %#v", use)
			}
		}
	}
}

type deferredNamespaceImporter struct{}

func (deferredNamespaceImporter) Lookup([]string, resolve.Key) []resolve.External { return nil }
func (deferredNamespaceImporter) DeferredImports() bool                           { return true }

func TestExplicitNamesShadowGlobsOnlyInTheirNamespace(t *testing.T) {
	importer := testImporter{
		"exports": {
			{Name: resolve.Key{Name: "Shared"}, Kind: resolve.Function, GoName: "Shared", Visibility: resolve.Public},
			{Name: resolve.Key{Name: "Shared", Namespace: resolve.MacroNamespace}, Kind: resolve.Macro, Visibility: resolve.Public},
		},
	}
	for _, declaration := range []string{"func Shared() {}", "macro_rules! Shared { () => { 1 }; }"} {
		source := "package p\nuse exports::*;\n" + declaration + "\nfunc f() { Shared(); Shared!() }"
		files := parseFiles(t, map[string]string{"p.gox": source})
		result := resolve.Resolve(files, importer)
		noDiagnostics(t, result)
		file := result.Files["p.gox"]
		for _, marker := range []string{"Shared();", "Shared!()"} {
			name := named(t, files["p.gox"], source, marker, "Shared")
			symbol := file.References[name.ID()].Symbol
			local := strings.HasPrefix(declaration, "func") == (marker == "Shared();")
			if symbol == nil || (symbol.Decl.File == "p.gox") != local {
				t.Fatalf("%s / %s crossed namespace shadowing: %#v", declaration, marker, symbol)
			}
		}
	}
}

func TestUseRootFallbackIsSelectedPerNamespace(t *testing.T) {
	importer := testImporter{
		"macros":     {{Name: resolve.Key{Name: "util", Namespace: resolve.MacroNamespace}, Kind: resolve.Macro, Visibility: resolve.Public}},
		"dependency": {{Name: resolve.Key{Name: "Runtime"}, Kind: resolve.Function, GoName: "Runtime", Visibility: resolve.Public}},
		"actual":     {{Name: resolve.Key{Name: "Runtime"}, Kind: resolve.Function, GoName: "Runtime", Visibility: resolve.Public}},
	}
	for _, test := range []struct {
		prefix string
		want   []string
		bound  bool
	}{
		{"use macros::util;", []string{"dependency", "Runtime"}, false},
		{"use actual as util;", []string{"actual", "Runtime"}, true},
	} {
		source := "package p\n" + test.prefix + "\nuse util::Runtime as run;func f(){run()}"
		files := parseFiles(t, map[string]string{"p.gox": source})
		use := files["p.gox"].Items[1].(*ast.UseDecl)
		use.RootFallback = []string{"dependency", "Runtime"}
		result := resolve.Resolve(files, importer)
		noDiagnostics(t, result)
		binding := result.Files["p.gox"].Uses[1]
		if !reflect.DeepEqual(binding.Target, test.want) || binding.BoundRoot != test.bound {
			t.Fatalf("%s selected target=%v, bound=%v", test.prefix, binding.Target, binding.BoundRoot)
		}
		if use.Tree.Path.Segments[0].Name.Name != "util" {
			t.Fatal("resolver rewrote the source root")
		}
	}
}
