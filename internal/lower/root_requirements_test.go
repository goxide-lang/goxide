package lower_test

import (
	"errors"
	"fmt"
	"go/importer"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

type rootDemand struct {
	alias     string
	namespace paths.Namespace
}

func (e *rootDemand) Error() string { return fmt.Sprintf("activate root %s", e.alias) }

func TestRequireRootPreservesTypedDemand(t *testing.T) {
	root := paths.Namespace{PackagePath: "example.org/pending", ModulePath: "example.org/pending", GoPrefix: "example.org/pending"}
	for _, test := range []struct{ name, source string }{
		{"member", "use dep;\nfunc Value() int { return dep::Value() }"},
		{"macro", "use dep;\nfunc Value() int { return dep::value!() }"},
		{"attribute", "use dep;\n#[dep::keep]\ntype Value struct {}"},
		{"derive", "use dep;\n#[derive(dep::Describe)]\ntype Value struct {}"},
		{"generated-use", "macro_rules! imports { ($name:ident) => { use dep as $name; }; }\nimports!(Selected)\nfunc Value() int { return Selected::Value() }"},
		{"generated-macro-use", "macro_rules! imports { ($name:ident) => { use dep as $name; }; }\nimports!(Selected)\nfunc Value() int { return Selected::value!() }"},
	} {
		t.Run(test.name, func(t *testing.T) {
			demand := &rootDemand{alias: "dep", namespace: root}
			output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package fixture\n" + test.source + "\n")}, "fixture", importer.Default(), paths.Roots{"dep": root}, lower.PackageOptions{
				Config: testConfig(t),
				RequireRoot: func(alias string, namespace paths.Namespace) error {
					if alias != demand.alias || namespace != demand.namespace {
						t.Fatalf("selected root = %s %+v, want %s %+v", alias, namespace, demand.alias, demand.namespace)
					}
					return demand
				},
			})
			var found *rootDemand
			if output != nil || !errors.As(err, &found) || found != demand {
				t.Fatalf("typed demand was lost before package lookup: output=%v error=%v", output, err)
			}
		})
	}
}

func TestRequireRootSkipsUnselectedBindings(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"unused", "// use dep; dep::value!()\nfunc Value() string { return \"dep::value!()\" }"},
		{"cfg", "#[cfg(any())] use dep;\n#[cfg(any())] func Hidden() { dep::value!() }\nfunc Value() {}"},
		{"unused-macro", "macro_rules! hidden { () => { use dep; }; }\nfunc Value() {}"},
		{"cfg-macro", "macro_rules! hidden { () => { #[cfg(any())] use dep; }; }\nhidden!()\nfunc Value() {}"},
		{"local-type", "enum dep { Ready }\nuse dep::Ready;\nfunc Value() { _ = Ready }"},
		{"use-binding", "enum Local { Ready }\nuse Local as dep;\nuse dep::Ready;\nfunc Value() { _ = Ready }"},
		{"generated-type", "macro_rules! bind { ($name:ident) => { enum $name { Ready } }; }\nbind!(dep)\nuse dep::Ready;\nfunc Value() { _ = Ready }"},
		{"generated-use-binding", "enum Local { Ready }\nmacro_rules! bind { ($name:ident) => { use Local as $name; }; }\nbind!(dep)\nuse dep::Ready;\nfunc Value() { _ = Ready }"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package fixture\n" + test.source + "\n")}, "fixture", nil,
				paths.Roots{"dep": {Unavailable: "unused dependency must remain unavailable"}}, lower.PackageOptions{
					Config: testConfig(t),
					RequireRoot: func(alias string, namespace paths.Namespace) error {
						calls++
						return &rootDemand{alias: alias, namespace: namespace}
					},
				})
			if err != nil || calls != 0 || output["main.gox"] == nil {
				t.Fatalf("unselected dependency was requested: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestRequireRootFollowsMacroScope(t *testing.T) {
	cfg := testConfig(t)
	kit := paths.Namespace{PackagePath: "example.org/kit", ModulePath: "example.org/kit"}
	data := paths.Namespace{PackagePath: "example.org/data", ModulePath: "example.org/data"}
	pending := paths.Namespace{PackagePath: "example.org/pending", ModulePath: "example.org/pending", GoPrefix: "example.org/pending"}
	packages := []lower.SourcePackage{
		{PackagePath: kit.PackagePath, ModulePath: kit.ModulePath, RootPackage: kit.PackagePath, Target: cfg.Environment.Target, Roots: paths.Roots{"api": data}, Sources: map[string][]byte{"macros.gox": []byte(`package kit
pub macro_rules! definition { ($name:ident) => { use api as $name; }; }
pub macro_rules! caller { ($name:ident) => { use $caller::api as $name; }; }
pub macro_rules! captured { ($root:ident, $name:ident) => { use $root as $name; }; }
`)}},
		{PackagePath: data.PackagePath, ModulePath: data.ModulePath, RootPackage: data.PackagePath, Target: cfg.Environment.Target, Sources: map[string][]byte{"lib.gox": []byte("package data\npub func Value() int { return 42 }\npub macro_rules! value { () => { 42 }; }\n")}},
	}
	for _, test := range []struct {
		name, call, member string
		demand             bool
	}{
		{"definition", "kit::definition!(Selected)", "Value()", false},
		{"caller", "kit::caller!(Selected)", "Value()", true},
		{"capture", "kit::captured!(api, Selected)", "Value()", true},
		{"definition-macro", "kit::definition!(Selected)", "value!()", false},
		{"caller-macro", "kit::caller!(Selected)", "value!()", true},
		{"capture-macro", "kit::captured!(api, Selected)", "value!()", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			demand := &rootDemand{alias: "api", namespace: pending}
			output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package fixture\nuse kit;\n" + test.call + "\nfunc Value() int { return Selected::" + test.member + " }\n")}, "fixture", importer.Default(),
				paths.Roots{"kit": kit, "api": pending}, lower.PackageOptions{Config: cfg, SourcePackages: packages, RequireRoot: func(alias string, namespace paths.Namespace) error {
					if alias == "kit" && namespace == kit {
						return nil
					}
					if alias != demand.alias || namespace != demand.namespace {
						t.Fatalf("consumer hook received dependency-owned root %s %+v", alias, namespace)
					}
					return demand
				}})
			if test.demand {
				var found *rootDemand
				if output != nil || !errors.As(err, &found) || found != demand {
					t.Fatalf("caller root did not preserve typed demand: %v", err)
				}
			} else if err != nil || output["main.gox"] == nil {
				t.Fatalf("definition root used consumer hook: %v", err)
			}
		})
	}
}

func TestGeneratedBindingsPrecedeRootsDuringMacroLookup(t *testing.T) {
	cfg := testConfig(t)
	kit := paths.Namespace{PackagePath: "example.org/kit", ModulePath: "example.org/kit"}
	for _, test := range []struct{ name, source string }{
		{"enum", "macro_rules! bind { ($name:ident) => { enum $name { Ready } }; }\nbind!(dep)"},
		{"alias", "enum Local { Ready }\nmacro_rules! bind { ($name:ident) => { use Local as $name; }; }\nbind!(dep)"},
		{"later-enum", "macro_rules! alias { ($name:ident) => { use kit as $name; }; }\nalias!(Selected)\nfunc Before() int { return Selected::value!() }\nmacro_rules! bind { ($name:ident) => { enum $name { Ready } }; }\nbind!(dep)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package fixture\nuse kit;\n" + test.source + "\nuse dep::Ready;\nfunc Value() int { _ = Ready; return kit::value!() }\n")}, "fixture", importer.Default(),
				paths.Roots{"kit": kit, "dep": {Unavailable: "shadowed dependency must remain unavailable"}}, lower.PackageOptions{
					Config: cfg,
					SourcePackages: []lower.SourcePackage{{PackagePath: kit.PackagePath, ModulePath: kit.ModulePath, RootPackage: kit.PackagePath, Target: cfg.Environment.Target,
						Sources: map[string][]byte{"lib.gox": []byte("package kit\npub macro_rules! value { () => { 42 }; }\n")}}},
					RequireRoot: func(alias string, namespace paths.Namespace) error {
						if alias == "kit" && namespace == kit {
							return nil
						}
						calls++
						return &rootDemand{alias: alias, namespace: namespace}
					},
				})
			if err != nil || calls != 0 || output["main.gox"] == nil {
				t.Fatalf("generated binding lost priority during macro lookup: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestNativePackageRequireRoot(t *testing.T) {
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\nuse pending;\nfunc main() { pending::value!() }\n"))
	root := paths.Namespace{PackagePath: "example.org/pending", ModulePath: "example.org/pending", GoPrefix: "example.org/pending"}
	demand := &rootDemand{alias: "pending", namespace: root}
	err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Roots: paths.Roots{"pending": root}, Config: testConfig(t), NativePackage: &lower.NativePackage{
		Importer:     importer.Default(),
		DisplayNames: map[string]string{"fixture/main.gox": filepath.Join(directory, "main.gox")},
		RequireRoot: func(alias string, namespace paths.Namespace) error {
			if alias != demand.alias || namespace != demand.namespace {
				t.Fatalf("native selected root = %s %+v", alias, namespace)
			}
			return demand
		},
	}}})
	var found *rootDemand
	if !errors.As(err, &found) || found != demand {
		t.Fatalf("native typed demand was lost: %v", err)
	}
}

func TestRequireRootRetainsToolchainFallback(t *testing.T) {
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\nuse fmt;\nfunc main() { fmt::Println(42) }\n"))
	calls := 0
	err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Roots: paths.Roots{}, Config: testConfig(t), NativePackage: &lower.NativePackage{RequireRoot: func(alias string, namespace paths.Namespace) error {
		calls++
		if alias != "fmt" || namespace.Source != "toolchain" || namespace.PackagePath != "fmt" {
			t.Fatalf("toolchain fallback changed: %s %+v", alias, namespace)
		}
		return nil
	}}}})
	if err != nil || calls == 0 {
		t.Fatalf("toolchain root was not resolved: calls=%d error=%v", calls, err)
	}
	if got := runGo(t, directory, "run", "."); got != "42\n" {
		t.Fatalf("toolchain output = %q", got)
	}
}
