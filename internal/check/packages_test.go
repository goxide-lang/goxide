package check_test

import (
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

type canonicalImports map[string][]resolve.External

func (i canonicalImports) Lookup(path []string, name resolve.Key) []resolve.External {
	var found []resolve.External
	for _, item := range i[strings.Join(path, "/")] {
		if item.Name == name {
			found = append(found, item)
		}
	}
	return found
}

func checkPackageSources(t *testing.T, sources map[string]string, imports resolve.Importer) (*check.Info, []check.Diagnostic) {
	t.Helper()
	files := make(map[string]*ast.File)
	for name, source := range sources {
		file, diagnostics := parser.ParseFile(name, []byte(source))
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		files[name] = file
	}
	resolved := resolve.Resolve(files, imports)
	if len(resolved.Diagnostics) != 0 {
		t.Fatal(resolved.Diagnostics)
	}
	return check.Check(files, resolved)
}

func TestSourceEnumPackagePathUsesSlashes(t *testing.T) {
	const filename = "example.org/orders/order/order.gox"
	for _, test := range []struct{ name, filename string }{
		{"logical", filename},
		{"native", filepath.FromSlash(filename)},
	} {
		t.Run(test.name, func(t *testing.T) {
			info, diagnostics := checkPackageSources(t, map[string]string{
				test.filename: "package order\npub enum Order { Pending, Paid(int) }\n",
			}, nil)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			if len(info.Enums) != 1 {
				t.Fatalf("checked enums = %d, want 1", len(info.Enums))
			}
			for _, enum := range info.Enums {
				if got := enum.Type.Obj().Pkg().Path(); got != "example.org/orders/order" {
					t.Fatalf("source type Go package path = %q; want slash-separated import path", got)
				}
			}
		})
	}
}

func TestNativeSingleItemImportsKeepTypes(t *testing.T) {
	imports := canonicalImports{
		"bytes": {{Name: resolve.Key{Name: "Buffer"}, Kind: resolve.Type, GoName: "Buffer", Visibility: resolve.Public}},
		"fmt":   {{Name: resolve.Key{Name: "Sprintf"}, Kind: resolve.Function, GoName: "Sprintf", Visibility: resolve.Public}},
	}
	info, diagnostics := checkPackageSources(t, map[string]string{"fixture/main.gox": `package main;use bytes::Buffer as Storage;use fmt::Sprintf as render;func f(value Storage)string{return render("%v",value)}`}, imports)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	found := false
	for symbol, typ := range info.Symbols {
		if symbol.Name.Name == "value" {
			found = true
			named, ok := typ.(*types.Named)
			if !ok || named.Obj().Pkg().Path() != "bytes" || named.Obj().Name() != "Buffer" {
				t.Fatalf("imported native type = %s", typ)
			}
		}
	}
	if !found {
		t.Fatal("missing source parameter")
	}
	for expression, typ := range info.Types {
		if _, ok := expression.(*ast.CallExpr); ok && !types.Identical(typ, types.Typ[types.String]) {
			t.Fatalf("imported native call result = %s", typ)
		}
	}
}

func TestCanonicalSourcePathsIgnoreNamespaceAliases(t *testing.T) {
	imports := canonicalImports{
		"routing/lib": {{Name: resolve.Key{Name: "Value"}, Kind: resolve.Function, GoName: "Value", Visibility: resolve.Public, Target: []string{"fixture", "lib", "Value"}}},
	}
	info, diagnostics := checkPackageSources(t, map[string]string{
		"fixture/lib/api.gox": `package lib;func Value()int{return 42}`,
		"fixture/main.gox":    `package main;use routing::lib as lib;func f()int{return lib::Value()}`,
	}, imports)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if got := info.PackagePaths["fixture/lib"]; got != "fixture/lib" {
		t.Fatalf("namespace replaced canonical source package: %q", got)
	}
}

func TestSourceReexportsKeepEnumTraitAndReferenceFacts(t *testing.T) {
	item := func(name string, kind resolve.Kind, canonical ...string) resolve.External {
		return resolve.External{Name: resolve.Key{Name: name}, Kind: kind, GoName: canonical[len(canonical)-1], Visibility: resolve.Public, Target: append([]string{"fixture", "model"}, canonical...)}
	}
	imports := canonicalImports{
		"fixture/facade":     {item("Choice", resolve.Enum, "Data"), item("Readable", resolve.Trait, "Source"), item("Counter", resolve.Variable, "Count"), item("ReadOnly", resolve.Function, "ReadOnly")},
		"fixture/model":      {item("Data", resolve.Enum, "Data"), item("Source", resolve.Trait, "Source"), item("Count", resolve.Variable, "Count"), item("ReadOnly", resolve.Function, "ReadOnly")},
		"fixture/model/Data": {item("Value", resolve.Variant, "Data", "Value")},
	}
	sources := map[string]string{
		"fixture/model/model.gox": `package model
pub trait Source{type Item;func Get(self Self)Self::Item}
pub enum Data{Value(int)}
impl Source for Data{type Item=int;func Get(self Self)Self::Item{return match self{Self::Value(n)=>n}}}
var Count=4
func ReadOnly()&int{return &Count}
`,
		"fixture/facade/api.gox": `package facade;pub use fixture::model::Data as Choice;pub use fixture::model::Source as Readable;pub use fixture::model::Count as Counter;pub use fixture::model::ReadOnly;`,
		"fixture/main.gox": `package main
use fixture::facade as api;
use fixture::facade::Choice as Item;
use fixture::facade::Readable;
use fixture::facade::Counter as count;
func f()int{var x Item=api::Choice::Value(1);var view Readable[Item=int]=x;_= &count == &api::Counter;return view.Get()+match x{api::Choice::Value(n)=>n}}
func g[T Readable](value T)T::Readable::Item{return value.Get()}
`,
	}
	info, diagnostics := checkPackageSources(t, sources, imports)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(info.TraitViews) != 1 || len(info.GenericBounds) != 1 || len(info.Matches) != 2 {
		t.Fatalf("reexport facts: views=%d bounds=%d matches=%d", len(info.TraitViews), len(info.GenericBounds), len(info.Matches))
	}
	sources["fixture/main.gox"] += `func bad(){value:=api::ReadOnly();*value=8}`
	_, diagnostics = checkPackageSources(t, sources, imports)
	found := false
	for _, diagnostic := range diagnostics {
		found = found || diagnostic.File == "fixture/main.gox" && diagnostic.Code == check.ReferencePermission && strings.Contains(diagnostic.Message, "read-only")
	}
	if !found {
		t.Fatalf("reexport lost read-only result: %v", diagnostics)
	}
}

func TestPrivateSourceImportsRejectedBeforeLowering(t *testing.T) {
	for _, test := range []struct{ name, declaration, use string }{
		{"function", `func hidden()int{return 1}`, `use fixture::lib::hidden;func f(){_=hidden()}`},
		{"unused", `func hidden()int{return 1}`, `pub use fixture::lib::hidden;`},
		{"type", `type hidden int`, `use fixture::lib::hidden;func f(){var x hidden;_=x}`},
		{"qualified-type", `type hidden int`, `use fixture::lib;func f(){var x lib::hidden;_=x}`},
		{"enum", `enum hidden{Item}`, `use fixture::lib::hidden;func f(){var x hidden;_=x}`},
		{"trait", `trait hidden{func get(self Self)int}`, `use fixture::lib::hidden;`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := map[string]*ast.File{}
			for name, source := range map[string]string{
				"fixture/lib/lib.gox": "package lib;" + test.declaration,
				"fixture/main.gox":    "package main;" + test.use,
			} {
				file, diagnostics := parser.ParseFile(name, []byte(source))
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				files[name] = file
			}
			_, diagnostics := check.Check(files, resolve.Resolve(files, nil))
			found := false
			for _, diagnostic := range diagnostics {
				found = found || diagnostic.Code == check.SourceVisibility && diagnostic.File == "fixture/main.gox" && strings.Contains(diagnostic.Message, `"hidden"`) && diagnostic.Origin.File == diagnostic.File && diagnostic.Origin.Span == diagnostic.Span
			}
			if !found {
				t.Fatalf("missing consumer source visibility diagnostic: %v", diagnostics)
			}
		})
	}
}

func TestMacroExpandedImportStillChecksPrivateSourceItems(t *testing.T) {
	sources := map[string]string{
		"fixture/lib/lib.gox": `package lib;func hidden()int{return 1}`,
		"fixture/main.gox":    `package main;use fixture::lib;macro_rules! leak{()=>{lib::hidden()};};func main(){_=leak!()}`,
	}
	files := map[string]*ast.File{}
	var trees []*ast.File
	for name, source := range sources {
		file, diagnostics := parser.ParseFile(name, []byte(source))
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		files[name] = file
		trees = append(trees, file)
	}
	expanded, err := expand.Expand(trees, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolve.ResolveExpanded(files, expanded, nil)
	_, diagnostics := check.Check(files, resolved)
	found := false
	for _, diagnostic := range diagnostics {
		found = found || diagnostic.Code == check.SourceVisibility && diagnostic.File == "fixture/main.gox" && diagnostic.Origin.Expn != 0
	}
	if !found {
		t.Fatalf("macro expansion bypassed imported private member: %v", diagnostics)
	}
}

func TestSourceModuleRecordsOnlySelectedChildPackage(t *testing.T) {
	sources := map[string]string{
		"fixture/main.gox":           `package main;mod child;mod absent;`,
		"fixture/child/child.gox":    `package child;pub func Answer()int{return 42}`,
		"fixture/child/api_test.gox": `package child_test;func TestAnswer(){}`,
	}
	info, diagnostics := checkPackageSources(t, sources, nil)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(info.Modules) != 1 {
		t.Fatalf("selected source modules = %d, want 1", len(info.Modules))
	}
	for decl, pkg := range info.Modules {
		if decl.Name.Name != "child" || pkg.Dir != "fixture/child" || pkg.Name != "child" {
			t.Fatalf("module %s selected package %s %s", decl.Name.Name, pkg.Dir, pkg.Name)
		}
	}
}

func TestExternalTestPackageKeepsSourceTypeIdentity(t *testing.T) {
	info, diagnostics := checkPackageSources(t, map[string]string{
		"fixture/lib/api.gox":      `package lib;type Value int`,
		"fixture/lib/api_test.gox": `package lib_test;use fixture::lib;type Value string;func f(input lib::Value)Value{return "ok"}`,
		"fixture/main.gox":         `package main;use fixture::lib;func f(input lib::Value)int{return int(input)}`,
	}, nil)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	var imported types.Type
	count := 0
	for symbol, typ := range info.Symbols {
		if symbol.Name.Name != "input" {
			continue
		}
		count++
		named, ok := typ.(*types.Named)
		if !ok || named.Obj().Pkg().Name() != "lib" || named.Obj().Pkg().Path() != "fixture/lib" {
			t.Fatalf("import selected external test type: %s", typ)
		}
		if imported != nil && !types.Identical(imported, typ) {
			t.Fatalf("package consumers received distinct type identities: %s, %s", imported, typ)
		}
		imported = typ
	}
	if count != 2 {
		t.Fatalf("checked imported parameters = %d, want 2", count)
	}
}

func TestExternalTestPackageCannotAccessPrivateSourceItems(t *testing.T) {
	for _, use := range []string{
		`use fixture::lib::hidden;func f(){_=hidden()}`,
		`pub use fixture::lib::hidden;`,
		`use fixture::lib;func f(){var value lib::secret;_=value}`,
	} {
		files := make(map[string]*ast.File)
		for name, source := range map[string]string{
			"fixture/lib/api.gox":      `package lib;func hidden()int{return 42};type secret int`,
			"fixture/lib/api_test.gox": "package lib_test;" + use,
		} {
			file, diagnostics := parser.ParseFile(name, []byte(source))
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			files[name] = file
		}
		_, diagnostics := check.Check(files, resolve.Resolve(files, nil))
		found := false
		for _, diagnostic := range diagnostics {
			found = found || diagnostic.Code == check.SourceVisibility && diagnostic.File == "fixture/lib/api_test.gox"
		}
		if !found {
			t.Fatalf("external test package bypassed source visibility with %s: %v", use, diagnostics)
		}
	}
}
