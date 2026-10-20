package lower_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestCompileModulesRejectMappedPrivatePackages(t *testing.T) {
	for _, test := range []struct {
		name, declaration, target, use string
	}{
		{"private-module", "mod hidden;", "hidden", "use api; func main(){_=api::Value()}"},
		{"undeclared-directory", "", "hidden", "use api; func main(){_=api::Value()}"},
		{"private-parent-public-leaf", "mod hidden;", "hidden/leaf", "use api; func main(){_=api::Value()}"},
		{"member-reexport", "mod hidden; pub use hidden::Value;", "hidden", "use api; func main(){_=api::Value()}"},
		{"glob-does-not-export-package", "mod hidden; pub use hidden::*;", "hidden", "use api; func main(){_=api::Value()}"},
		{"unused-namespace", "", "hidden", "use api; func main(){}"},
		{"blank-namespace", "", "hidden", "use api as _; func main(){}"},
		{"glob-namespace", "", "hidden", "use api::*; func main(){_=Value()}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			source := "package main\n" + test.use + "\n"
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte(source))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\n"+test.declaration+"\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/value.gox", []byte("package hidden\npub mod leaf;\npub func Value() int { return 9 }\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.gox", []byte("package leaf\npub func Value() int { return 11 }\n"))
			roots := paths.Roots{"api": {PackagePath: "zlib/" + test.target, ModulePath: "zlib"}}
			err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: roots, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			})
			var diagnostic *lower.Diagnostic
			if !errors.As(err, &diagnostic) || diagnostic.File != "app/main.gox" || !strings.Contains(diagnostic.Message, "not publicly reachable") || !strings.Contains(diagnostic.Message, `"zlib/`+test.target+`"`) {
				t.Fatalf("missing public-entry diagnostic: %v", err)
			}
			if diagnostic.Span.Start == 0 || diagnostic.Span.End > uint32(len(source)) || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
				t.Fatalf("wrong source location: %+v", diagnostic)
			}
			for _, name := range []string{"app/main", "lib/lib", "lib/hidden/value", "lib/hidden/leaf/value"} {
				if _, err := os.Stat(filepath.Join(root, name+".go")); !os.IsNotExist(err) {
					t.Fatalf("failure wrote generated Go: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, name+".gox")); err != nil {
					t.Fatalf("failure removed source: %v", err)
				}
			}
		})
	}
}

func TestCompileModulesPublicPackageRoutes(t *testing.T) {
	for _, test := range []struct{ name, declaration, target, call, output string }{
		{"public-module", "pub mod hidden;", "zlib/entry/hidden", "Value()", "9\n"},
		{"namespace-reexport", "mod hidden; pub use hidden as API;", "zlib/entry/hidden", "Value()", "9\n"},
		{"leaf-reexport", "mod hidden; pub use hidden::leaf as API;", "zlib/entry/hidden/leaf", "Value()", "11\n"},
		{"member-reexport", "mod hidden; pub use hidden::Value as PublicValue;", "zlib/entry", "PublicValue()", "9\n"},
		{"glob-module-reexport", "mod hidden; pub use hidden::*;", "zlib/entry/hidden/leaf", "Value()", "11\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var previous []byte
			for attempt := 0; attempt < 2; attempt++ {
				root := t.TempDir()
				app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
				writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
				writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse api; use console; func main(){console::Println(api::"+test.call+")}\n"))
				writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, lib, "entry/lib.gox", []byte("package entry\n"+test.declaration+"\n"))
				writeDiagnosticModuleFile(t, lib, "entry/hidden/value.gox", []byte("package hidden\npub mod leaf;\npub func Value() int { return 9 }\n"))
				writeDiagnosticModuleFile(t, lib, "entry/hidden/leaf/value.gox", []byte("package leaf\npub func Value() int { return 11 }\n"))
				roots := paths.Roots{"api": {PackagePath: test.target, ModulePath: "zlib"}, "console": {PackagePath: "fmt", Source: "toolchain"}}
				if err := lower.CompileModules([]lower.Module{
					{Path: "app", Dir: app, Roots: roots, Config: testConfig(t)},
					{Path: "zlib", Dir: lib, RootPackage: "zlib/entry", Roots: paths.Roots{}, Config: testConfig(t)},
				}); err != nil {
					t.Fatal(err)
				}
				generated, err := os.ReadFile(filepath.Join(app, "main.go"))
				if err != nil {
					t.Fatal(err)
				}
				if attempt != 0 && !bytes.Equal(previous, generated) {
					t.Fatalf("same input generated different Go:\n%s\n%s", previous, generated)
				}
				previous = generated
				if attempt == 0 {
					runGo(t, app, "build", "./...")
					if got := runGo(t, app, "run", "."); got != test.output {
						t.Fatalf("output = %q, want %q", got, test.output)
					}
				}
			}
		})
	}
}

func TestCompileModulesPublicReachabilityKeepsUnusedRootsLazy(t *testing.T) {
	root := t.TempDir()
	app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
	writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
	writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse api;func main(){_=api::Value()}\n"))
	writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\npub mod visible;\n"))
	writeDiagnosticModuleFile(t, lib, "visible/value.gox", []byte("package visible\npub func Value() int{return 1}\n"))
	if err := lower.CompileModules([]lower.Module{
		{Path: "app", Dir: app, Roots: paths.Roots{"api": {PackagePath: "zlib/visible", ModulePath: "zlib"}}, Config: testConfig(t)},
		{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{"unused": {GoPrefix: "unavailable.invalid/missing", ModulePath: "unavailable.invalid/missing", Source: "module"}}, Config: testConfig(t)},
	}); err != nil {
		t.Fatal(err)
	}
	runGo(t, app, "build", "./...")
}

func TestCompileModulesRejectMappedGoLeafBelowPrivateSourceModule(t *testing.T) {
	root := t.TempDir()
	app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
	writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
	writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse api;func main(){_=api::Value()}\n"))
	writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;\n"))
	writeDiagnosticModuleFile(t, lib, "hidden/mod.gox", []byte("package hidden\npub use leaf;\n"))
	writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.go", []byte("package leaf\nimport _ \"zlib\"\nfunc Value() int{return 1}\n"))
	err := lower.CompileModules([]lower.Module{
		{Path: "app", Dir: app, Roots: paths.Roots{"api": {PackagePath: "zlib/hidden/leaf", ModulePath: "zlib"}}, Config: testConfig(t)},
		{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{"leaf": {PackagePath: "zlib/hidden/leaf", ModulePath: "zlib"}}, Config: testConfig(t)},
	})
	var diagnostic *lower.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.File != "app/main.gox" || !strings.Contains(diagnostic.Message, "not publicly reachable") {
		t.Fatalf("Go leaf bypassed private source ancestor: %v", err)
	}
}

func TestCompileModulesMemberReexportCannotExposeSameNamedPackage(t *testing.T) {
	for _, test := range []struct{ name, export, target string }{
		{"member", "pub use hidden::leaf;", "hidden/leaf"},
		{"member-glob", "pub use hidden::leaf::*;", "hidden/leaf/child"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse api;func main(){_=api::Value()}\n"))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;"+test.export+"\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/member.gox", []byte("package hidden\npub func leaf() int{return 1}\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.gox", []byte("package leaf\npub mod child;pub func Value() int{return 2}\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/leaf/child/value.gox", []byte("package child\npub func Value() int{return 3}\n"))
			err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"api": {PackagePath: "zlib/" + test.target, ModulePath: "zlib"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			})
			var diagnostic *lower.Diagnostic
			wantFile, wantMessage := "app/main.gox", "not publicly reachable"
			if test.name == "member-glob" {
				wantFile, wantMessage = "zlib/lib.gox", `glob use target "leaf" is not a namespace`
			}
			if !errors.As(err, &diagnostic) || diagnostic.File != wantFile || !strings.Contains(diagnostic.Message, wantMessage) {
				t.Fatalf("member reexport exposed same-named package: %v", err)
			}
		})
	}
}

func TestCompileModulesMemberReexportWithSameNamedPackage(t *testing.T) {
	for _, test := range []struct{ name, use, call, member string }{
		{"qualified", "use lib;", "lib::leaf()", "pub func leaf()int{return 23}"},
		{"imported", "use lib::leaf;", "leaf()", "pub func leaf()int{return 23}"},
		{"type-static", "use lib;", "lib::leaf::Value()", "pub type leaf struct{}\nimpl leaf{pub func Value()int{return 23}}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\n"+test.use+"use console;func main(){console::Println("+test.call+")}\n"))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;pub use hidden::leaf;\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/member.gox", []byte("package hidden\n"+test.member+"\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.gox", []byte("package leaf\npub func Value() int{return 2}\n"))
			if err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: "zlib", ModulePath: "zlib"}, "console": {PackagePath: "fmt", Source: "toolchain"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			}); err != nil {
				t.Fatal(err)
			}
			runGo(t, app, "build", "./...")
			if got := runGo(t, app, "run", "."); got != "23\n" {
				t.Fatalf("output = %q", got)
			}
		})
	}
}

func TestCompileModulesValueCannotSelectSameNamedPackageMembers(t *testing.T) {
	for _, declaration := range []string{
		"pub func leaf()int{return 1}",
		"pub var leaf int=1",
		"pub const leaf=1",
	} {
		t.Run(declaration, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse lib;func main(){_=lib::leaf::Value()}\n"))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;pub use hidden::leaf;\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/member.gox", []byte("package hidden\n"+declaration+"\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.gox", []byte("package leaf\npub func Value()int{return 2}\n"))
			err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: "zlib", ModulePath: "zlib"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			})
			var diagnostic resolve.Diagnostic
			if !errors.As(err, &diagnostic) || diagnostic.File != "app/main.gox" || diagnostic.Code != "unresolved" {
				t.Fatalf("value became a namespace: %v", err)
			}
			if _, err := os.Stat(filepath.Join(app, "main.go")); !os.IsNotExist(err) {
				t.Fatalf("failure wrote generated Go: %v", err)
			}
		})
	}
}

func TestCompileModulesTypeCannotSelectSameNamedPackageMembers(t *testing.T) {
	for _, test := range []struct{ name, export, call, message string }{
		{"path", "pub use hidden::leaf;", "lib::leaf::Value()", "unknown associated type or member path"},
		{"glob", "pub use hidden::leaf::*;", "lib::Value()", "unresolved name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse lib;func main(){_="+test.call+"}\n"))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;"+test.export+"\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/member.gox", []byte("package hidden\npub type leaf struct{}\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.gox", []byte("package leaf\npub func Value()int{return 2}\n"))
			err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: "zlib", ModulePath: "zlib"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			})
			if err == nil || !strings.Contains(err.Error(), "app/main.gox:") || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("type selected package members: %v", err)
			}
			if _, err := os.Stat(filepath.Join(app, "main.go")); !os.IsNotExist(err) {
				t.Fatalf("failure wrote generated Go: %v", err)
			}
		})
	}
}

func TestCompileModulesPublicNamespaceWithSameNamedPrivateItem(t *testing.T) {
	root := t.TempDir()
	app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
	writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
	writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse lib::API;use console;func main(){console::Println(API::Value())}\n"))
	writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\npub use physical as API;\n"))
	writeDiagnosticModuleFile(t, lib, "hidden/member.gox", []byte("package hidden\nfunc leaf()int{return 99}\n"))
	writeDiagnosticModuleFile(t, lib, "hidden/leaf/value.gox", []byte("package leaf\npub func Value()int{return 31}\n"))
	if err := lower.CompileModules([]lower.Module{
		{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: "zlib", ModulePath: "zlib"}, "console": {PackagePath: "fmt", Source: "toolchain"}}, Config: testConfig(t)},
		{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{"physical": {PackagePath: "zlib/hidden/leaf", ModulePath: "zlib"}}, Config: testConfig(t)},
	}); err != nil {
		t.Fatal(err)
	}
	runGo(t, app, "build", "./...")
	if got := runGo(t, app, "run", "."); got != "31\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestCompileModulesRejectPrivateMemberUse(t *testing.T) {
	for _, consumer := range []string{
		"use lib::API::secret;func main(){_=secret()}",
		"use lib::API::secret as renamed;func main(){}",
		"use lib::API::hidden;func main(){var value hidden;_=value}",
	} {
		t.Run(consumer, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			source := "package main\n" + consumer + "\n"
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte(source))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;pub use hidden as API;\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/member.gox", []byte("package hidden\nfunc secret()int{return 1}\ntype hidden struct{}\n"))
			err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: "zlib", ModulePath: "zlib"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			})
			var diagnostic *lower.Diagnostic
			if !errors.As(err, &diagnostic) || diagnostic.File != "app/main.gox" || !strings.Contains(diagnostic.Message, "not a public item or namespace") || strings.Contains(diagnostic.Message, "$goxide$roots") {
				t.Fatalf("missing private member use diagnostic: %v", err)
			}
			if diagnostic.Span.Start == 0 || diagnostic.Span.End > uint32(len(source)) || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
				t.Fatalf("wrong source location: %+v", diagnostic)
			}
			if _, err := os.Stat(filepath.Join(app, "main.go")); !os.IsNotExist(err) {
				t.Fatalf("failure wrote generated Go: %v", err)
			}
			if _, err := os.Stat(filepath.Join(app, "main.gox")); err != nil {
				t.Fatalf("failure removed source: %v", err)
			}
		})
	}
}

func TestCompileModulesPublicNamespaceAliasAcrossFiles(t *testing.T) {
	root := t.TempDir()
	app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
	writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
	writeDiagnosticModuleFile(t, app, "imports.gox", []byte("package main\nuse lib;pub use lib::API as Selected;\n"))
	writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse Selected as api;use direct;use console;func main(){console::Println(api::Value()+direct::Value())}\n"))
	writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\npub use facade::API;\n"))
	writeDiagnosticModuleFile(t, lib, "facade/api.gox", []byte("package facade\nmod hidden;pub use hidden as API;\n"))
	writeDiagnosticModuleFile(t, lib, "facade/hidden/value.gox", []byte("package hidden\npub func Value() int{return 13}\n"))
	if err := lower.CompileModules([]lower.Module{
		{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: "zlib", ModulePath: "zlib"}, "direct": {PackagePath: "zlib/facade/hidden", ModulePath: "zlib"}, "console": {PackagePath: "fmt", Source: "toolchain"}}, Config: testConfig(t)},
		{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{"facade": {PackagePath: "zlib/facade", ModulePath: "zlib"}}, Config: testConfig(t)},
	}); err != nil {
		t.Fatal(err)
	}
	runGo(t, app, "build", "./...")
	if got := runGo(t, app, "run", "."); got != "26\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestCompileModulesNativePackagePublicRoutes(t *testing.T) {
	for _, sourceRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "native-module", true: "source-namespace-reexport"}[sourceRoot], func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse api;use console;func main(){console::Println(api::Value())}\n"))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			if sourceRoot {
				writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\npub use child as API;\n"))
			} else {
				writeDiagnosticModuleFile(t, lib, "lib.go", []byte("package zlib\n"))
			}
			writeDiagnosticModuleFile(t, lib, "child/value.go", []byte("package child\nfunc Value() int{return 17}\n"))
			if err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"api": {PackagePath: "zlib/child", ModulePath: "zlib"}, "console": {PackagePath: "fmt", Source: "toolchain"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{"child": {PackagePath: "zlib/child", ModulePath: "zlib"}}, Config: testConfig(t)},
			}); err != nil {
				t.Fatal(err)
			}
			runGo(t, app, "build", "./...")
			if got := runGo(t, app, "run", "."); got != "17\n" {
				t.Fatalf("output = %q", got)
			}
		})
	}
}

func TestCompileModulesDeriveCannotSelectPrivateMappedNamespace(t *testing.T) {
	root := t.TempDir()
	app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
	writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse hidden;\n#[derive(hidden::Serialize)]\ntype Record struct{Value int}\nfunc main(){}\n"))
	writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\nmod hidden;\n"))
	writeDiagnosticModuleFile(t, lib, "hidden/codecs.gox", []byte("package hidden\npub use wire::Serialize;\n"))
	for directory, module := range map[string]string{app: "module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n", lib: "module zlib\n\ngo 1.27\n"} {
		if err := testdeps.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0644); err != nil {
			t.Fatal(err)
		}
	}
	err := lower.CompileModules([]lower.Module{
		{Path: "app", Dir: app, Roots: paths.Roots{"hidden": {PackagePath: "zlib/hidden", ModulePath: "zlib"}}, Config: testConfig(t)},
		{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{"wire": {PackagePath: "github.com/goxide-lang/serde", ModulePath: "github.com/goxide-lang/serde"}}, Config: testConfig(t)},
	})
	var diagnostic *lower.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.File != "app/main.gox" || !strings.Contains(diagnostic.Message, "not publicly reachable") {
		t.Fatalf("derive bypassed private source namespace: %v", err)
	}
}

func TestCompileModulesMacroUsesAfterExpansion(t *testing.T) {
	const publicMacro = "pub macro_rules! add_two { ($value:expr) => { $value + 2 }; }"
	const reexport = "mod hidden; pub use hidden::add_two;"
	for _, test := range []struct{ name, declaration, export, mapping, use, value, wantError string }{
		{"called-reexport", publicMacro, reexport, "zlib", "use lib::add_two as plus;", "plus!(40)", ""},
		{"unused-reexport", publicMacro, reexport, "zlib", "use lib::add_two as unused;", "42", ""},
		{"private-unused", "macro_rules! add_two { ($value:expr) => { $value + 2 }; }", "pub mod hidden;", "zlib", "use lib::hidden::add_two as unused;", "42", "not a public item or namespace"},
		{"disabled-unused", "#[cfg(any())]\n" + publicMacro, "pub mod hidden;", "zlib", "use lib::hidden::add_two as unused;", "42", "not a public item or namespace"},
		{"missing-unused", publicMacro, "pub mod hidden;", "zlib", "use lib::hidden::missing as unused;", "42", "not a public item or namespace"},
		{"private-mapped", publicMacro, "mod hidden;", "zlib/hidden", "use lib::add_two as unused;", "42", "not publicly reachable"},
		{"macro-glob", publicMacro, reexport, "zlib", "use lib::add_two::*;", "42", "not a namespace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
			writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../lib\n"))
			writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\n"+test.use+"use console;func main(){console::Println("+test.value+")}\n"))
			writeDiagnosticModuleFile(t, lib, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, lib, "lib.gox", []byte("package zlib\n"+test.export+"\n"))
			writeDiagnosticModuleFile(t, lib, "hidden/macros.gox", []byte("package hidden\n"+test.declaration+"\n"))
			err := lower.CompileModules([]lower.Module{
				{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: test.mapping, ModulePath: "zlib"}, "console": {PackagePath: "fmt", Source: "toolchain"}}, Config: testConfig(t)},
				{Path: "zlib", Dir: lib, RootPackage: "zlib", Roots: paths.Roots{}, Config: testConfig(t)},
			})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), "app/main.gox:") || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("invalid macro import error = %v, want %q", err, test.wantError)
				}
				if _, err := os.Stat(filepath.Join(app, "main.go")); !os.IsNotExist(err) {
					t.Fatalf("failure wrote generated Go: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			runGo(t, app, "build", "./...")
			if got := runGo(t, app, "run", "."); got != "42\n" {
				t.Fatalf("output = %q", got)
			}
		})
	}
}
