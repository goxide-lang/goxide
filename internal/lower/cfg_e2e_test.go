package lower_test

import (
	"bytes"
	"errors"
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

func TestCFGPackageSelectionAndDeterminism(t *testing.T) {
	const source = `package main
#[cfg(any())]
use unavailable;
#[cfg(any())]
func Hidden() { unknown!(); absent() }
#[cfg(feature("json"))]
func Mode() string { return "json" }
#[cfg(not(feature("json")))]
func Mode() string { return "plain" }
#[cfg_attr(not(feature("json")), cfg(any()))]
type Counter struct { Value int }
#[cfg(feature("json"))]
func Count() int { return Counter{}.Value }
#[cfg(not(feature("json")))]
func Count() int { return -1 }
const HasJSON = cfg!(feature("json"))
`
	const optional = "package main\n#[cfg(feature(\"json\"))]\nfunc Extra() int { return 7 }\n"
	const native = "package main\nimport \"fmt\"\nfunc main() { fmt.Println(Mode(), HasJSON, Count()) }\n"
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			features := config.Features{Enabled: map[string]bool{"json": enabled}, Declared: map[string]bool{"json": true}}
			var previous map[string][]byte
			for attempt := 0; attempt < 2; attempt++ {
				sources := map[string][]byte{"main.gox": []byte(source), "optional.gox": []byte(optional), "native.go": []byte(native)}
				output, err := lower.CompilePackage(sources, "cfgfixture", nil, paths.Roots{}, lower.PackageOptions{Config: testConfig(t, features)})
				if err != nil {
					t.Fatal(err)
				}
				if len(output) != 2 {
					t.Fatalf("empty source file was omitted: %v", output)
				}
				if !enabled {
					file, err := goparser.ParseFile(token.NewFileSet(), "optional.go", output["optional.gox"], 0)
					if err != nil || file.Name.Name != "main" || len(file.Decls) != 0 {
						t.Fatalf("disabled file must have an empty package shell: %v\n%s", err, output["optional.gox"])
					}
				}
				if attempt != 0 {
					for name, data := range output {
						if !bytes.Equal(data, previous[name]) {
							t.Fatalf("same configuration changed %s", name)
						}
					}
					continue
				}
				previous = output
				directory := t.TempDir()
				writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module cfgfixture\n\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, directory, "native.go", []byte(native))
				for name, data := range output {
					writeDiagnosticModuleFile(t, directory, strings.TrimSuffix(name, ".gox")+"_gox.go", data)
				}
				want, count := "plain false -1\n", -1
				if enabled {
					want, count = "json true 0\n", 0
				}
				writeDiagnosticModuleFile(t, directory, "main_test.go", []byte(fmt.Sprintf("package main\nimport \"testing\"\nfunc TestConfiguration(t *testing.T) { if HasJSON != %t || Count() != %d { t.Fatal(HasJSON, Count()) } }\n", enabled, count)))
				runGo(t, directory, "build", "./...")
				if got := runGo(t, directory, "run", "."); got != want {
					t.Fatalf("configuration output = %q, want %q", got, want)
				}
				runGo(t, directory, "test", "./...")
			}
		})
	}
}

func TestCFGOrdinaryIfChecksBothBranches(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"macro", "missing!()", "macro missing"},
		{"name", "absent()", "unresolved name"},
		{"reference", "view := &mut *ro; _ = view", "read-only"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package fixture\nfunc Check(ro &int) { if cfg!(any()) { " + test.body + " } }\n"
			_, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte(source)}, "cfgfixture", nil, nil, lower.PackageOptions{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("false if skipped %s checking: %v", test.name, err)
			}
		})
	}
}

func TestCFGMacroModuleOrigins(t *testing.T) {
	const definitions = `package cfgkit
pub macro_rules! flag { () => { cfg!(feature("side")) }; }
pub macro_rules! named { ($name:tt) => { cfg!(feature($name)) }; }
pub macro_rules! pass { ($value:expr) => { $value }; }
pub macro_rules! relay { ($value:expr) => { pass!($value) }; }
pub macro_rules! item { ($item:item) => { $item }; }
pub macro_rules! selected { ($name:ident) => { #[cfg(feature("side"))] type $name struct { Value int } }; }
pub macro_rules! conditional { ($name:ident) => { #[cfg_attr(feature("side"), cfg(any()))] type $name struct { Value int } }; }
`
	const source = `package main
use cfgkit;
const Caller = cfg!(feature("side"))
const Template = cfgkit::flag!()
const Parameter = cfgkit::named!("side")
const Captured = cfgkit::pass!(cfg!(feature("side")))
const Forwarded = cfgkit::relay!(cfg!(feature("side")))
cfgkit::selected!(TemplateType)
cfgkit::conditional!(TemplateConditional)
cfgkit::item!(#[cfg(feature("side"))] type CapturedType struct { Value int })
cfgkit::item!(#[cfg_attr(feature("side"), cfg(any()))] type CapturedConditional struct { Value int })
cfgkit::item!(#[cfg_attr(feature("side"), cfg(any()))] type CapturedHidden struct {})
`
	const native = "package main\nimport \"fmt\"\nfunc main() { fmt.Println(Caller, Template, Parameter, Captured, Forwarded) }\n"
	for _, callerEnabled := range []bool{false, true} {
		t.Run(fmt.Sprint(callerEnabled), func(t *testing.T) {
			var previous map[string][]byte
			for attempt := 0; attempt < 2; attempt++ {
				root := t.TempDir()
				app, kit := filepath.Join(root, "app"), filepath.Join(root, "kit")
				writeDiagnosticModuleFile(t, app, "go.mod", []byte("module cfgapp\n\ngo 1.27\nrequire cfgkit v0.0.0\nreplace cfgkit => ../kit\n"))
				writeDiagnosticModuleFile(t, kit, "go.mod", []byte("module cfgkit\n\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, app, "main.gox", []byte(source))
				writeDiagnosticModuleFile(t, app, "native.go", []byte(native))
				writeDiagnosticModuleFile(t, kit, "macros.gox", []byte(definitions))
				modules := []lower.Module{
					{Path: "cfgapp", Dir: app, Config: testConfig(t, config.Features{Enabled: map[string]bool{"side": callerEnabled}, Declared: map[string]bool{"side": true}})},
					{Path: "cfgkit", Dir: kit, Config: testConfig(t, config.Features{Enabled: map[string]bool{"side": !callerEnabled}, Declared: map[string]bool{"side": true}})},
				}
				if err := lower.CompileModules(modules); err != nil {
					t.Fatal(err)
				}
				output := map[string][]byte{}
				for _, name := range []string{"app/main.go", "kit/macros.go"} {
					data, err := os.ReadFile(filepath.Join(root, name))
					if err != nil {
						t.Fatal(err)
					}
					output[name] = data
					if attempt != 0 && !bytes.Equal(data, previous[name]) {
						t.Fatalf("same module configuration changed %s", name)
					}
				}
				file, err := goparser.ParseFile(token.NewFileSet(), "main.go", output["app/main.go"], 0)
				if err != nil {
					t.Fatal(err)
				}
				declared := map[string]bool{}
				goast.Inspect(file, func(node goast.Node) bool {
					switch n := node.(type) {
					case *goast.TypeSpec:
						declared[n.Name.Name] = true
					case *goast.FuncDecl:
						declared[n.Name.Name] = true
					}
					return true
				})
				for name, want := range map[string]bool{
					"TemplateType": !callerEnabled, "TemplateConditional": callerEnabled,
					"CapturedType": callerEnabled, "CapturedConditional": !callerEnabled, "CapturedHidden": !callerEnabled,
				} {
					if declared[name] != want {
						t.Fatalf("%s presence = %t, want %t\n%s", name, declared[name], want, output["app/main.go"])
					}
				}
				previous = output
				if attempt == 0 {
					runGo(t, app, "build", "./...")
					want := fmt.Sprintf("%t %t %t %t %t\n", callerEnabled, !callerEnabled, !callerEnabled, callerEnabled, callerEnabled)
					if got := runGo(t, app, "run", "."); got != want {
						t.Fatalf("macro feature origins = %q, want %q", got, want)
					}
				}
			}
		})
	}
}

func TestCFGMacroSourceCycles(t *testing.T) {
	for _, test := range []struct {
		name, a, b string
		cycle      bool
	}{
		{"unused", `pub macro_rules! value { () => { b::value!() }; }`, `pub macro_rules! value { () => { a::value!() }; }`, false},
		{"deleted-output", `pub macro_rules! value { () => { 7 }; }
pub macro_rules! removed { () => { #[cfg(any())] func Hidden() int { return b::value!() } }; }
removed!()`, `pub macro_rules! value { () => { a::value!() }; }`, false},
		{"ordinary-false-if", `pub macro_rules! value { () => { 7 }; }
func Check() { if cfg!(any()) { _ = b::value!() } }`, `pub macro_rules! value { () => { a::value!() }; }`, true},
		{"outer-call-retained", `pub macro_rules! removed { () => { #[cfg(any())] func Hidden() {} }; }
func Check() { _ = b::value!() }`, `pub macro_rules! value { () => { 7 }; }
a::removed!()`, true},
		{"recursive", `pub macro_rules! value { () => { b::value!() }; }
var Value = value!()`, `pub macro_rules! value { () => { a::value!() }; }`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			for name, source := range map[string]string{
				"go.mod": "module cfgcycle\n\ngo 1.27\n", "main.gox": "package main\nfunc main() {}\n",
				"a/a.gox": "package a\nuse cfgcycle::b;\n" + test.a + "\n",
				"b/b.gox": "package b\nuse cfgcycle::a;\n" + test.b + "\n",
			} {
				writeDiagnosticModuleFile(t, directory, name, []byte(source))
			}
			err := lower.CompileModules([]lower.Module{{Path: "cfgcycle", Dir: directory, Config: testConfig(t)}})
			if test.cycle {
				if err == nil || !strings.Contains(err.Error(), "macro source dependency cycle: cfgcycle/a -> cfgcycle/b -> cfgcycle/a") {
					t.Fatalf("active macro cycle was not diagnosed: %v", err)
				}
				if _, err := os.Stat(filepath.Join(directory, "main.go")); !os.IsNotExist(err) {
					t.Fatalf("cycle published partial output: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			runGo(t, directory, "build", "./...")
			runGo(t, directory, "run", ".")
		})
	}
}

func TestCFGMacroImportSelection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			directory := t.TempDir()
			body := "func main() {}\n"
			if enabled {
				body = "func main() { Console::Println(\"mapped\") }\n"
			}
			for name, source := range map[string]string{
				"go.mod":   "module cfgimports\n\ngo 1.27\n",
				"main.gox": "package main\nuse cfgimports::kit;\nkit::console!(Console)\nkit::hidden_use!()\n" + body,
				"kit/macros.gox": `package kit
pub macro_rules! console { ($name:ident) => { #[cfg(feature("console"))] use console as $name; }; }
pub macro_rules! hidden_use { () => { #[cfg(any())] use unavailable; }; }
`,
			} {
				writeDiagnosticModuleFile(t, directory, name, []byte(source))
			}
			roots := paths.Roots{"console": {PackagePath: "fmt", Source: "toolchain"}}
			if err := lower.CompileModules([]lower.Module{{Path: "cfgimports", Dir: directory, Roots: roots, Config: testConfig(t, config.Features{Enabled: map[string]bool{"console": enabled}})}}); err != nil {
				t.Fatal(err)
			}
			runGo(t, directory, "build", "./...")
			want := ""
			if enabled {
				want = "mapped\n"
			}
			if got := runGo(t, directory, "run", "."); got != want {
				t.Fatalf("macro import output = %q, want %q", got, want)
			}
		})
	}
}

func TestCFGRootMatchesModuleName(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":          "module app\n\ngo 1.27\n",
		"main.gox":        "package main\n#[cfg(all())]\nuse app;\nfunc main() { if app::Value() != 42 { panic(\"wrong root\") } }\n",
		"local/value.gox": "package local\npub func Value() int { return 42 }\n",
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	roots := paths.Roots{"app": {PackagePath: "app/local", ModulePath: "app"}}
	if err := lower.CompileModules([]lower.Module{{Path: "app", Dir: directory, Config: testConfig(t), Roots: roots}}); err != nil {
		t.Fatal(err)
	}
	runGo(t, directory, "build", "./...")
	runGo(t, directory, "run", ".")
}

func TestCFGMacroImportModuleOrigins(t *testing.T) {
	const source = `package main
use cfgkit;
cfgkit::imported!(Template)
cfgkit::item!(#[cfg(all())] use api as Captured;)
cfgkit::explicit!(Explicit)
cfgkit::captured_root!(api, Path)
func Values() (int, int, int, int) { return Template::Value(), Captured::Value(), Explicit::Value(), Path::Value() }
`
	const definitions = `package cfgkit
pub macro_rules! imported { ($name:ident) => { #[cfg(all())] use api as $name; }; }
pub macro_rules! item { ($item:item) => { $item }; }
pub macro_rules! explicit { ($name:ident) => { #[cfg(all())] use $caller::api as $name; }; }
pub macro_rules! captured_root { ($target:ident, $name:ident) => { #[cfg(all())] use $target as $name; }; }
`
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		root := t.TempDir()
		app, kit := filepath.Join(root, "app"), filepath.Join(root, "kit")
		writeDiagnosticModuleFile(t, app, "go.mod", []byte("module cfgapp\n\ngo 1.27\nrequire cfgkit v0.0.0\nreplace cfgkit => ../kit\n"))
		writeDiagnosticModuleFile(t, kit, "go.mod", []byte("module cfgkit\n\ngo 1.27\n"))
		writeDiagnosticModuleFile(t, app, "main.gox", []byte(source))
		writeDiagnosticModuleFile(t, app, "native.go", []byte("package main\nimport \"fmt\"\nfunc main() { fmt.Println(Values()) }\n"))
		writeDiagnosticModuleFile(t, app, "local/value.gox", []byte("package local\npub func Value() int { return 11 }\n"))
		writeDiagnosticModuleFile(t, kit, "local/value.gox", []byte("package local\npub func Value() int { return 22 }\n"))
		writeDiagnosticModuleFile(t, kit, "macros.gox", []byte(definitions))
		modules := []lower.Module{
			{Path: "cfgapp", Dir: app, Config: testConfig(t), Roots: paths.Roots{
				"api":    {PackagePath: "cfgapp/local", ModulePath: "cfgapp"},
				"cfgkit": {PackagePath: "cfgkit", ModulePath: "cfgkit"},
			}},
			{Path: "cfgkit", Dir: kit, Config: testConfig(t), Roots: paths.Roots{"api": {PackagePath: "cfgkit/local", ModulePath: "cfgkit"}}},
		}
		if err := lower.CompileModules(modules); err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(app, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt != 0 && !bytes.Equal(previous, output) {
			t.Fatal("same macro import configuration changed output")
		}
		previous = output
		if attempt == 0 {
			runGo(t, app, "build", "./...")
			if got := runGo(t, app, "run", "."); got != "22 11 11 11\n" {
				t.Fatalf("macro import roots = %q", got)
			}
		}
	}
}

func TestCFGMacroImportDiagnosticOrigin(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":         "module cfgorigin\n\ngo 1.27\n",
		"main.gox":       "package main\nuse cfgorigin::kit;\nkit::bad!()\nfunc main(){}\n",
		"kit/macros.gox": "package kit\npub macro_rules! bad { () => { #[cfg(all())] use api as _; }; }\n",
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	roots := paths.Roots{"api": {PackagePath: "fmt", ModulePath: "wrong"}}
	err := lower.CompileModules([]lower.Module{{Path: "cfgorigin", Dir: directory, Config: testConfig(t), Roots: roots}})
	var diagnostic *lower.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Origin.File != "cfgorigin/kit/macros.gox" || diagnostic.Origin.Expn == 0 || len(diagnostic.Origin.Stack) == 0 || !strings.Contains(err.Error(), "called at cfgorigin/main.gox:3:1") {
		t.Fatalf("macro import error lost definition and call origins: %v", err)
	}
}

func TestCFGUnsupportedPositionsBeforeResolution(t *testing.T) {
	for _, declaration := range []string{
		"type Record struct { #[cfg(any())] Value Missing }",
		"trait Protocol { #[cfg_attr(any(), derive(Default))] func value() Missing }",
	} {
		_, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package fixture\n" + declaration + "\n")}, "cfgfixture", nil, nil, lower.PackageOptions{})
		if err == nil || !strings.Contains(err.Error(), "configuration attributes require a complete top-level item") {
			t.Fatalf("internal configuration position was ignored or resolved: %v", err)
		}
	}
}
