package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
)

func TestRenameNameUsesDeclarationVisibilityAndGoMapping(t *testing.T) {
	for _, test := range []struct {
		name, source, old, new, generated string
		visibility                        resolve.Visibility
	}{
		{"Go public to private", "func Upper()int{return 1};func F()int{return Upper()}", "Upper", "lower", "lower", resolve.Private},
		{"Go private to public", "func lower()int{return 1};func F()int{return lower()}", "lower", "Upper", "Upper", resolve.Public},
		{"explicit public lower name", "pub func lower()int{return 1};func F()int{return lower()}", "lower", "answer", "Answer", resolve.Public},
		{"private enum", "enum Old{Zero,};func F()Old{return Old::Zero}", "Old", "New", "_New", resolve.Private},
		{"private trait", "trait Old{func value(self Self)int};type A struct{};impl Old for A{func value(self Self)int{return 1}}", "Old", "New", "_New", resolve.Private},
		{"private struct sugar", "struct Old{N int};func F()Old{return Old{N:1}}", "Old", "New", "_New", resolve.Private},
		{"public trait member", "pub trait P{func Value(self Self)int};type A struct{};impl P for A{func Value(self Self)int{return 1}};func F(a A)int{return a.Value()}", "Value", "value", "Value", resolve.Public},
		{"private trait member", "trait P{func value(self Self)int};type A struct{};impl P for A{func value(self Self)int{return 1}};func F(a A)int{return a.value()}", "value", "Value", "_Value", resolve.Private},
		{"public enum variant", "pub enum E{One,};func F()E{return E::One}", "One", "two", "Two", resolve.Public},
		{"private enum variant", "enum E{one,};func F()E{return E::one}", "one", "Two", "_Two", resolve.Private},
		{"local uppercase", "func F(old int)int{return old}", "old", "Upper", "Upper", resolve.Private},
		{"inherent member", "type A struct{};impl A{func value(self Self)int{return 1}};func F(a A)int{return a.value()}", "value", "Value", "_Value", resolve.Private},
		{"Go receiver method", "type A struct{};func(a A)Value()int{return 1};func F(a A)int{return a.Value()}", "Value", "value", "value", resolve.Private},
		{"public grouped constant", "pub const(old=1);func F()int{return old}", "old", "answer", "Answer", resolve.Public},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := "package fixture\n" + test.source + "\n"
			before, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
			if before.Err != nil {
				t.Fatal(before.Err)
			}
			edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(text, test.old)), test.new)
			if err != nil {
				t.Fatalf("legal rename rejected: %v", err)
			}
			sources := applyQueryEdits(t, before, edits)
			after, err := analysis.Analyze(context.Background(), analysis.Input{PackagePath: "fixture", Sources: sources, Options: analysis.Options{PreserveSyntax: true}})
			if err != nil || after.Err != nil {
				t.Fatalf("renamed program: %v / %v\n%s", err, after.Err, sources["main.gox"])
			}
			found := false
			for _, symbol := range after.Resolved.Files["fixture/main.gox"].Definitions {
				if symbol.Name.Name == test.new {
					found = true
					if symbol.GoName != test.generated || symbol.Visibility != test.visibility {
						t.Fatalf("mapped rename = %s/%v, want %s/%v", symbol.GoName, symbol.Visibility, test.generated, test.visibility)
					}
				}
			}
			if !found {
				t.Fatal("rename did not change a declaration")
			}
		})
	}
}

func TestRenameNameRejectsGoMappingCollisions(t *testing.T) {
	for _, test := range []struct{ name, source, old, new string }{
		{"public spelling", "pub func alpha()int{return 1};func Beta()int{return 2};func F()int{return alpha()+Beta()}", "alpha", "beta"},
		{"private prefix", "struct Old{};type _New int;func F()Old{return Old{}}", "Old", "New"},
		{"inherited public member", "pub trait P{func alpha(self Self)int;func Beta(self Self)int};type A struct{};impl P for A{func alpha(self Self)int{return 1};func Beta(self Self)int{return 2}}", "alpha", "beta"},
		{"inherited private variant", "enum E{old,_New,};func F()E{return E::old}", "old", "New"},
		{"public unexportable", "pub func old()int{return 1}", "old", "_hidden"},
		{"package declaration versus import", "use fmt as Beta;pub func alpha()int{return 1};func F(){Beta::Println(alpha())}", "alpha", "beta"},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := "package fixture\n" + test.source + "\n"
			before, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
			if before.Err != nil {
				t.Fatalf("invalid collision fixture: %v", before.Err)
			}
			if edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(text, test.old)), test.new); err == nil {
				t.Fatalf("rename accepted a generated name collision: %+v", edits)
			}
		})
	}
}

func TestRenameNamePreservesExistingCrossPackageVisibility(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		declaration := "package dep\nfunc Upper()int{return 42}\n"
		if explicit {
			declaration = strings.Replace(declaration, "func Upper", "pub func Upper", 1)
		}
		consumer := "package fixture\nuse api::Upper;func F()int{return Upper()}\n"
		input := analysis.Input{PackagePath: "fixture", Sources: map[string][]byte{"main.gox": []byte(consumer)}, Roots: paths.Roots{"api": {PackagePath: "dep", ModulePath: "dep"}}, Options: analysis.Options{Editor: true, SourcePackages: []analysis.SourcePackage{{PackagePath: "dep", ModulePath: "dep", RootPackage: "dep", Target: config.HostTarget(), Sources: map[string][]byte{"lib.gox": []byte(declaration)}}}}}
		before, err := analysis.Analyze(t.Context(), input)
		if err != nil || before.Err != nil {
			t.Fatalf("initial analysis: %v / %v", err, before.Err)
		}
		edits, err := analysis.NewQueries(before).Rename("dep/lib.gox", uint32(strings.Index(declaration, "Upper")), "lower")
		if !explicit {
			if err == nil {
				t.Fatalf("implicit public rename accepted inaccessible cross-package references: %+v", edits)
			}
			continue
		}
		if err != nil {
			t.Fatalf("explicit pub must preserve public lower names: %v", err)
		}
		changed := applyQueryEdits(t, before, edits)
		input.Sources["main.gox"] = changed["main.gox"]
		input.Options.SourcePackages[0].Sources["lib.gox"] = changed["dep/lib.gox"]
		if after, err := analysis.Analyze(t.Context(), input); err != nil || after.Err != nil {
			t.Fatalf("explicit pub rename broke cross-package program: %v / %v\nedits=%+v\nconsumer=%s\ndependency=%s", err, after.Err, edits, input.Sources["main.gox"], input.Options.SourcePackages[0].Sources["lib.gox"])
		}
	}
}

func TestRenameNamePreservesPackageEntryPoints(t *testing.T) {
	for _, test := range []struct{ name, source, selected, newName string }{
		{"business to init", "package fixture\nfunc business(){}\nfunc F(){business()}\n", "business", "init"},
		{"public business to init", "package fixture\npub func business(){}\nfunc F(){business()}\n", "business", "init"},
		{"existing init", "package fixture\nvar count int\nfunc init(){count++}\n", "init", "initialize"},
		{"main entry removed", "package main\nfunc main(){println(42)}\n", "main(){", "start"},
		{"business to main entry", "package main\nfunc business(){println(42)}\n", "business", "main"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(test.source)})
			if before.Err != nil {
				t.Fatal(before.Err)
			}
			edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(test.source, test.selected)), test.newName)
			if err == nil || edits != nil || !strings.Contains(err.Error(), "structural") {
				t.Fatalf("automatic execution entry changed: %+v, %v", edits, err)
			}
		})
	}
}

func TestRenameNameAllowsOrdinaryInitAndMainIdentifiers(t *testing.T) {
	for _, test := range []struct{ name, source, selected, newName string }{
		{"local variable to init", "package fixture\nfunc F()int{value:=42;return value}\n", "value", "init"},
		{"local init variable", "package fixture\nfunc F()int{init:=42;return init}\n", "init", "value"},
		{"method to init", "package fixture\ntype S struct{}\nfunc(s S)value()int{return 42}\nfunc F(s S)int{return s.value()}\n", "value", "init"},
		{"method named init", "package fixture\ntype S struct{}\nfunc(s S)init()int{return 42}\nfunc F(s S)int{return s.init()}\n", "init", "value"},
		{"inherent method to main", "package fixture\ntype S struct{}\nimpl S{func value(self Self)int{return 42}}\nfunc F(s S)int{return s.value()}\n", "value", "main"},
		{"library function named main", "package fixture\nfunc main()int{return 42}\nfunc F()int{return main()}\n", "main()", "value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(test.source)})
			if before.Err != nil {
				t.Fatal(before.Err)
			}
			edits, err := queries.Rename("fixture/main.gox", uint32(strings.Index(test.source, test.selected)), test.newName)
			if err != nil || len(edits) == 0 {
				t.Fatalf("ordinary identifier was mistaken for an entry point: %+v, %v", edits, err)
			}
			if after, err := analysis.Analyze(t.Context(), analysis.Input{Sources: applyQueryEdits(t, before, edits), PackagePath: "fixture", Options: analysis.Options{PreserveSyntax: true}}); err != nil || after.Err != nil {
				t.Fatalf("ordinary rename broke source: %v / %v", err, after.Err)
			}
		})
	}
}
