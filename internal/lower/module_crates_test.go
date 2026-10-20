package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

func TestModuleExpandedBindingsPrecedeStandardRoots(t *testing.T) {
	for _, tc := range []struct {
		name, helpers, source, native string
	}{
		{"generated-enum", "", `macro_rules! gen {()=>{enum fmt {Unit}};}
gen!();
use fmt::Unit;
func main(){_=Unit}
`, ""},
		{"generated-alias", "", `macro_rules! gen {($name:ident)=>{use strings as $name;};}
gen!(fmt);
use fmt::ToUpper;
func main(){if ToUpper("ok") != "OK" { panic("wrong root") }}
`, ""},
		{"definition-file-alias", `use fmt as output;
macro_rules! generated { ($name:ident)=>{ use output::Println as $name; }; }
`, `use strings as output;
generated!(print);
func main(){print(42);_=output::ToUpper("ok")}
`, ""},
		{"native-type", "", "use fmt as Printer;\nfunc main(){_=Printer{}}\n", "package main\ntype fmt struct{}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\n"+tc.source))
			if tc.helpers != "" {
				writeDiagnosticModuleFile(t, directory, "helpers.gox", []byte("package main\n"+tc.helpers))
			}
			if tc.native != "" {
				writeDiagnosticModuleFile(t, directory, "native.go", []byte(tc.native))
			}
			module := lower.Module{Path: "fixture", Dir: directory, Roots: paths.Roots{}, Config: testConfig(t)}
			if err := lower.CompileModules([]lower.Module{module}); err != nil {
				t.Fatal(err)
			}
			runGo(t, directory, "run", ".")
		})
	}
}

func TestNativeModuleSelectsExplicitPureGoChild(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod": "module fixture\n\ngo 1.27\n",
		"main.gox": `package main
mod helper;
use fmt;
use helper::Value;
func main() { fmt::Println(Value()) }
`,
		"helper/value.go": "package helper\nfunc Value() int { return 42 }\n",
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Roots: paths.Roots{}, Config: testConfig(t)}}); err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "."); got != "42\n" {
		t.Fatalf("native module child output = %q", got)
	}
}

func TestCrateMixedGraphAndDefinitionSiteMacro(t *testing.T) {
	sources := map[string]string{
		"go.mod": "module fixture\n\ngo 1.27\n",
		"src/lib.gox": `package library
pub mod model;
pub mod native;
pub macro_rules! answer { () => { crate::model::Value() }; }
`,
		"src/model/value.gox": "package model\npub func Value() int { return 20 }\n",
		"src/native/value.go": `package native
import "fixture/src/native/helper"
func Value() int { return helper.Value() }
`,
		"src/native/helper/value.go": "package helper\nfunc Value() int { return 22 }\n",
		"cmd/app/main.gox": `package main
pub mod local;
mod sibling;
use appcore;
use fmt;
func main() { fmt::Println(appcore::answer!(), appcore::native::Value(), crate::sibling::Value()) }
`,
		"cmd/app/local/value.gox":   "package local\npub func Value() int { return 7 }\n",
		"cmd/app/sibling/value.gox": "package sibling\npub func Value() int { return crate::local::Value() }\n",
		"cmd/other/main.gox":        "this unselected target is deliberately invalid\n",
		"orphan/bad.go":             "this unselected Go package is deliberately invalid\n",
		"src/orphan/bad.gox":        "this undeclared directory is deliberately invalid\n",
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := t.TempDir()
		for name, source := range sources {
			writeDiagnosticModuleFile(t, directory, name, []byte(source))
		}
		module := lower.Module{Path: "fixture", Dir: directory, Roots: paths.Roots{}, Config: testConfig(t),
			RootPackage: "fixture/src",
			Crates: []lower.Crate{
				{Name: "appcore", Root: "fixture/src", SourceDir: "fixture/src", Library: true},
				{Name: "app", Root: "fixture/cmd/app", SourceDir: "fixture/cmd/app"},
			},
			Targets: []lower.TargetBoundary{{Path: "fixture/src"}, {Path: "fixture/cmd/app"}, {Path: "fixture/cmd/other"}},
		}
		if err := lower.CompileModules([]lower.Module{module}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(directory, "cmd/app/main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt != 0 && !bytes.Equal(previous, data) {
			t.Fatal("same crate graph generated different Go")
		}
		previous = data
		if strings.Contains(string(data), "$goxide$roots") || strings.Contains(string(data), "fixture/cmd/app\"") {
			t.Fatalf("generated Go retained a routing or main-package import:\n%s", data)
		}
		if attempt == 0 {
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "./cmd/app"); got != "20 22 7\n" {
				t.Fatalf("crate output = %q", got)
			}
			runGo(t, directory, "test", "./...")
		}
		for _, name := range []string{"cmd/other/main.gox", "orphan/bad.go", "src/orphan/bad.gox"} {
			if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				t.Fatalf("unselected staged source remains: %s", name)
			}
		}
	}
}

func TestCrateRejectsPrivateAndIndependentTrees(t *testing.T) {
	for _, test := range []struct {
		name, source, native, lib, want string
	}{
		{"private-source", "use appcore::hidden;\nfunc main(){ _ = hidden::Value() }", "", "mod hidden;", "unresolved"},
		{"private-native", "func main(){}", "package main\nimport \"fixture/src/hidden\"\nvar _ = hidden.Value\n", "mod hidden;", "not publicly reachable"},
		{"go-import-unselected-bin", "func main(){}", "package main\nimport _ \"fixture/cmd/other/helper\"\n", "", "outside crate"},
		{"go-import-outside-tree", "func main(){}", "package main\nimport _ \"fixture/shared\"\n", "", "outside crate"},
		{"nested-target", "func main(){}", "", "pub mod nested;", "independent target"},
		{"private-sibling", "mod local; mod sibling;\nfunc main(){ _ = sibling::Value() }", "", "", "unresolved"},
		{"reserved-static-root", "mod crate;\nfunc main(){}", "", "", "reserved static path root"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			sources := map[string]string{
				"go.mod":                    "module fixture\n\ngo 1.27\n",
				"src/lib.gox":               "package library\n" + test.lib + "\n",
				"src/hidden/value.gox":      "package hidden\npub func Value() int { return 1 }\n",
				"src/nested/main.gox":       "package main\nfunc main(){}\n",
				"cmd/app/main.gox":          "package main\n" + test.source + "\n",
				"cmd/app/local/value.gox":   "package local\npub func Value() int { return 1 }\n",
				"cmd/app/sibling/value.gox": "package sibling\npub func Value() int { return crate::local::Value() }\n",
				"cmd/app/crate/value.gox":   "package value\n",
				"cmd/other/helper/value.go": "package helper\n",
				"shared/value.go":           "package shared\n",
			}
			if test.native != "" {
				sources["cmd/app/native.go"] = test.native
			}
			for name, source := range sources {
				writeDiagnosticModuleFile(t, directory, name, []byte(source))
			}
			err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}, RootPackage: "fixture/src",
				Crates: []lower.Crate{
					{Name: "appcore", Root: "fixture/src", SourceDir: "fixture/src", Library: true},
					{Name: "app", Root: "fixture/cmd/app", SourceDir: "fixture/cmd/app"},
				},
				Targets: []lower.TargetBoundary{{Path: "fixture/src"}, {Path: "fixture/src/nested"}, {Path: "fixture/cmd/app"}, {Path: "fixture/cmd/other"}},
			}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
			if _, err := os.Stat(filepath.Join(directory, "cmd/app/main.go")); !os.IsNotExist(err) {
				t.Fatal("failure wrote generated Go")
			}
		})
	}
}

func TestCrateMacroGeneratedModule(t *testing.T) {
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte(`package main
macro_rules! modules { () => { pub mod child; }; }
modules!()
use fmt;
func main(){ fmt::Println(crate::child::Value(), crate::child::value!()) }
`))
	writeDiagnosticModuleFile(t, directory, "child/value.gox", []byte("package child\npub func Value() int { return 42 }\npub macro_rules! value { () => { 42 }; }\n"))
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}, Crates: []lower.Crate{{Root: "fixture", SourceDir: "fixture"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "."); got != "42 42\n" {
		t.Fatalf("macro module output = %q", got)
	}
}

func TestCrateMacroCannotMountAnotherCratePackage(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":               "module fixture\n\ngo 1.27\n",
		"lib.gox":              "package library\nmod hidden;\n",
		"src/hidden/value.gox": "package hidden\npub func Secret() int { return 42 }\n",
		"goxide_targets/bin/app/main.gox": `package main
macro_rules! mount { () => { mod hidden; }; }
mount!()
func main() { _ = hidden::Secret() }
`,
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}, RootPackage: "fixture",
		Crates: []lower.Crate{
			{Name: "library", Root: "fixture", SourceDir: "fixture/src", Library: true},
			{Name: "app", Root: "fixture/goxide_targets/bin/app", SourceDir: "fixture/src"},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "fixture/src/hidden belongs to two crates") {
		t.Fatalf("want conflicting crate ownership, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "goxide_targets/bin/app/main.go")); !os.IsNotExist(err) {
		t.Fatal("failure wrote generated Go")
	}
}

func TestCrateMacroModuleAlreadyLoadedByGoImport(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":          "module fixture\n\ngo 1.27\n",
		"native.go":       "package main\nimport _ \"fixture/child\"\n",
		"child/value.gox": "package child\npub macro_rules! answer { () => { 42 }; }\n",
		"main.gox": `package main
macro_rules! mount { () => { mod child; }; }
mount!()
use fmt;
func main() { fmt::Println(child::answer!()) }
`,
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{},
		Crates: []lower.Crate{{Root: "fixture", SourceDir: "fixture"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "."); got != "42\n" {
		t.Fatalf("macro output = %q", got)
	}
}

func TestCratePublicItemReexport(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":               "module fixture\n\ngo 1.27\n",
		"src/lib.gox":          "package library\nmod hidden; pub use hidden::Answer;\n",
		"src/hidden/value.gox": "package hidden\npub func Answer() int { return 42 }\n",
		"cmd/app/main.gox":     "package main\nuse library::Answer; use fmt;\nfunc main(){fmt::Println(Answer())}\n",
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}, RootPackage: "fixture/src",
		Crates: []lower.Crate{
			{Name: "library", Root: "fixture/src", SourceDir: "fixture/src", Library: true},
			{Name: "app", Root: "fixture/cmd/app", SourceDir: "fixture/cmd/app"},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "./cmd/app"); got != "42\n" {
		t.Fatalf("public item reexport output = %q", got)
	}
}

func TestSourceUseBindingPrecedesGoRoot(t *testing.T) {
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte(`package main
use encoding::json as encoding;
use encoding::Marshal;
use fmt;
func main(){ data, err := Marshal(42); if err != nil { panic(err) }; fmt::Println(string(data)) }
`))
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}}}); err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "."); got != "42\n" {
		t.Fatalf("source alias output = %q", got)
	}
}

func TestSourceTypeBindingPrecedesGoRoot(t *testing.T) {
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\nenum fmt { Unit }\nuse fmt::Unit;\nfunc main(){ _ = Unit }\n"))
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}}}); err != nil {
		t.Fatal(err)
	}
	runGo(t, directory, "build", ".")
}
