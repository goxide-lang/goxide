package frontend

import (
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/typecheck"
)

func staticTestRoots() paths.Roots {
	return paths.Roots{
		"strings": {PackagePath: "strings", Source: "toolchain"},
		"time":    {PackagePath: "time", Source: "toolchain"},
		"std":     {Children: map[string]paths.Namespace{"text": {PackagePath: "strings", Source: "toolchain"}}},
	}
}

func TestStaticPathSelectors(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"package call", "use strings; func F()string{return strings::Repeat(\"x\",2)}", ""},
		{"private function", "use strings::Repeat as Twice; func F()string{return Twice(\"x\",2)}", ""},
		{"private type", "use time::Duration as D; func F(x D)D{return x}", ""},
		{"private const", "use time::Second; func F()int64{return int64(Second)}", ""},
		{"explicit namespace child", "use std; func F()string{return std::text::Repeat(\"x\",2)}", ""},
		{"namespace alias selects item", "use strings as text;use text::Repeat;func F()string{return Repeat(\"x\",2)}", ""},
		{"namespace alias declared later", "use text::Repeat;use strings as text;func F()string{return Repeat(\"x\",2)}", ""},
		{"alias cycle", "use left as right;use right as left;", "cyclic use alias"},
		{"local value field", "func F()int{x:=struct{V int}{V:2};return x.V}", ""},
		{"local receiver shadows namespace", "use time; func F()int{time:=struct{V int}{V:2};return time.V}", ""},
		{"namespace and local value coexist", "use strings;func F()string{strings:=struct{Value string}{\"x\"};return strings::Repeat(strings.Value,2)}", ""},
		{"namespace and local method coexist", "use strings;type T struct{};func(T)Repeat()string{return \"x\"};func F()string{strings:=T{};return strings::Repeat(strings.Repeat(),2)}", ""},
		{"namespace and local type conflict", "use time;type T struct{};func(T)M()int{return 42};func F()int{type time=T;return time::M(T{})}", "local type time conflicts with a visible use namespace"},
		{"namespace and nested local type conflict", "use time;func F(){if true{type time struct{};_=time{}}}", "local type time conflicts with a visible use namespace"},
		{"namespace and predeclared type conflict", "use strings as int;func F()int{return 1}", "namespace int conflicts with a predeclared type"},
		{"namespace and builtin function field coexist", "use strings as len;type Record struct{len int};func F()int{return len::Count(\"x\",\"x\")+Record{len:3}.len+len([]int{1})}", ""},
		{"type method expression", "type T struct{};func(T)M()int{return 1};func F()int{return T::M(T{})}", ""},
		{"external type method expression", "use time;func F(t time::Time)int64{return time::Time::Unix(t)}", ""},
		{"pointer type method expression", "type T struct{};func(*T)M()int{return 1};func F()int{return (*T)::M(&T{})}", ""},
		{"package variable method", "use time;func F()string{return time::Local.String()}", ""},
		{"package variable static method rejected", "use time;func F()string{return time::Local::String()}", "value members use '.'"},
		{"method value", "type T struct{};func(T)M()int{return 1};func F()int{t:=T{};m:=t.M;return m()}", ""},
		{"package dot rejected", "use strings; func F()string{return strings.Repeat(\"x\",2)}", "static package and type paths use '::'"},
		{"type dot rejected", "type T struct{};func(T)M()int{return 1};func F()int{return T.M(T{})}", "static package and type paths use '::'"},
		{"value static rejected", "func F()int{x:=struct{V int}{V:2};return x::V}", "value members use '.'"},
		{"value method static rejected", "type T struct{};func(T)M()int{return 1};func F()int{t:=T{};return t::M()}", "value members use '.'"},
		{"old import rejected", "import \"strings\"; func F()string{return strings::Repeat(\"x\",2)}", "replace import with use"},
		{"unused namespace preserves init", "use strings; func F()int{return 1}", ""},
		{"side effect", "use strings as _; func F()int{return 1}", ""},
		{"single local module boundary", "mod child;func F()int{return child::Value()}", "local mod requires directory package compilation"},
		{"group has no initialization", "use std as _;", "side-effect use requires a concrete package"},
		{"item has no initialization", "use strings::Repeat as _;", "side-effect use requires a concrete package"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imp := typecheck.NewImporter("paths.gox")
			r, err := CompileWithOptions("paths.gox", []byte("package p\n"+tc.source+"\n"), FileOptions{Roots: staticTestRoots(), Importer: imp})
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %q; Go:\n%s", err, tc.want, r.Go)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := typecheck.Check("paths.go", r.Go, imp); err != nil {
				t.Fatalf("%v\n%s", err, r.Go)
			}
		})
	}
}

func TestStaticPathDependencyPreparation(t *testing.T) {
	roots := staticTestRoots()
	p, err := PrepareSourcePaths("example.org/app", "main.gox", []byte("package p\nmod child;\nuse std::text::Repeat;\nuse time as _;\n"), roots, map[string]bool{"example.org/app/child": true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Imports, []string{"example.org/app/child", "strings", "time"}) {
		t.Fatal(p.Imports)
	}
	p, err = PrepareSourcePaths("example.org/app", "main.gox", []byte("package p\nuse std;func F()string{return std::text::Repeat(\"x\",2)}"), roots, nil)
	if err != nil || !reflect.DeepEqual(p.Imports, []string{"strings"}) {
		t.Fatalf("selected namespace leaf missing: %v %v", p.Imports, err)
	}
	for _, tc := range []struct{ source, want string }{
		{"mod missing;", "not selected"},
		{"mod time;", "conflicts with dependency"},
		{"mod child {};", "inline modules"},
		{"mod child = go import \"strings\";", "replace import with use"},
		{"use std::unknown::Foo;", "unknown namespace member"},
	} {
		_, err := PrepareSourcePaths("example.org/app", "main.gox", []byte("package p\n"+tc.source), roots, map[string]bool{"example.org/app/child": true})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.source, err)
		}
	}
}

func TestStaticPathUsesAreFileScoped(t *testing.T) {
	for _, tc := range []struct{ name, decl, expr string }{
		{"function", "use strings::Repeat as Imported;", "Imported(\"x\",2)"},
		{"type", "use time::Duration as Imported;", "Imported(1)"},
		{"constant", "use time::Second as Imported;", "Imported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imp := typecheck.NewImporter("one.gox")
			sources := map[string][]byte{"one.gox": []byte("package p\n" + tc.decl + "\nfunc One(){_=" + tc.expr + "}\n"), "two.gox": []byte("package p\nfunc Two(){_=" + tc.expr + "}\n")}
			_, err := CompilePackageWithOptions("example.org/p", sources, PackageOptions{Roots: staticTestRoots(), Importer: imp})
			if err == nil || !strings.Contains(err.Error(), "undefined: Imported") {
				t.Fatalf("private binding leaked: %v", err)
			}
			sources["two.gox"] = []byte("package p\n" + tc.decl + "\nfunc Two(){_=" + tc.expr + "}\n")
			if _, err := CompilePackageWithOptions("example.org/p", sources, PackageOptions{Roots: staticTestRoots(), Importer: imp}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStaticPathPublicNamespaceAndDerive(t *testing.T) {
	imp := typecheck.NewImporter("api.gox")
	roots := serdeFixtureRoots()
	sources := map[string][]byte{"api.gox": []byte("package api\npub use serde as wire;\npub use serde::Serialize as Wire;\n"), "model.gox": []byte("package api\n#[derive(wire::Deserialize, Wire)]\ntype Item struct{Value int}\n")}
	r, err := CompilePackageWithOptions("example.org/api", sources, PackageOptions{Roots: roots, Importer: imp})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.uses.Bindings) != 2 {
		t.Fatalf("public metadata: %+v", r.uses)
	}
	for _, b := range r.uses.Bindings {
		if !b.Public || b.OriginPath != "github.com/goxide-lang/serde" {
			t.Fatalf("lost canonical owner: %+v", b)
		}
	}
}

func TestStaticPathPackageAliasesAndHygiene(t *testing.T) {
	for _, other := range []string{"sibling.go", "sibling.gox"} {
		for _, decl := range []string{"use strings;func F()string{return strings::Repeat(\"x\",2)}", "use strings::Repeat as Twice;func F()string{return Twice(\"x\",2)}"} {
			sources := map[string][]byte{"main.gox": []byte("package p\n" + decl), other: []byte("package p\nconst goxideNamespace0=42\nconst goxideUseRoute0=42\n")}
			if _, err := CompilePackageWithOptions("example.org/p", sources, PackageOptions{Roots: staticTestRoots(), Importer: typecheck.NewImporter("main.gox")}); err != nil {
				t.Fatal(err)
			}
		}
	}
	sources := map[string][]byte{"a.gox": []byte("package p\nuse strings as private;pub use private as text;\n"), "b.gox": []byte("package p\nuse text::Repeat as Twice;func F()string{return Twice(\"x\",2)}\n")}
	plans, err := PreparePackageSourcePaths("example.org/p", sources, staticTestRoots(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plans["b.gox"].Imports, []string{"strings"}) {
		t.Fatal(plans["b.gox"].Imports)
	}
	if _, err := CompilePackageWithOptions("example.org/p", sources, PackageOptions{Roots: staticTestRoots(), Importer: typecheck.NewImporter("a.gox")}); err != nil {
		t.Fatal(err)
	}
	delete(sources, "a.gox")
	if _, err := PreparePackageSourcePaths("example.org/p", sources, staticTestRoots(), nil); err == nil {
		t.Fatal("sibling public use root remained after its declaration was removed")
	}
}

func TestStaticPathReexportedNamespaceSideEffect(t *testing.T) {
	const api = "example.org/api"
	const driver = "example.org/driver"
	node := paths.Namespace{PackagePath: driver}
	metadata := useMetadata{Version: useMetadataVersion, Path: api, Package: "api", Bindings: []useBinding{{Name: "Driver", Kind: "namespace", OriginPath: driver, OriginName: "Driver", RoutePath: driver, RouteName: "Driver", Namespace: &node, Public: true}}}
	imp := enumTestImporter{api: types.NewPackage(api, "api"), driver: types.NewPackage(driver, "driver")}
	sources := map[string][]byte{"consumer.gox": []byte("package consumer\nuse api::Driver as _;\n")}
	u, err := prepareUses("example.org/consumer", sources, map[string]PackageResult{api: {uses: metadata}}, imp, paths.Roots{"api": {PackagePath: api}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(u.plans["consumer.gox"].Imports, []string{api, driver}) {
		t.Fatalf("lost exact side-effect package: %v", u.plans["consumer.gox"].Imports)
	}
	out, err := u.rewrite("consumer.gox", u.prepared["consumer.gox"], u.sourceMap("consumer.gox"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "import _ \""+api+"\"") || !strings.Contains(string(out), "import _ \""+driver+"\"") {
		t.Fatalf("initialization dependencies missing:\n%s", out)
	}
}
