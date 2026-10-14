package lower_test

import (
	"bytes"
	"errors"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
)

func TestCompileModulesMappedBlankImports(t *testing.T) {
	const native = "package effects\nimport \"fmt\"\nfunc init() { fmt.Println(\"initialized\") }\n"
	sources := map[string]string{
		"go.mod":                "module fixture\n\ngo 1.27\n",
		"main.gox":              "package main\nuse api::effects as _;\nuse api::effects as effects;\nuse console;\nfunc main() { console::Println(effects::Value()) }\n",
		"effects/source.gox":    "package effects\npub func Value() int { return 9 }\n",
		"effects/native.go":     native,
		"effects/value_test.go": "package effects_test\nimport (\"testing\"; \"fixture/effects\")\nfunc TestValue(t *testing.T) { if effects.Value() != 9 { t.Fatal(\"wrong value\") } }\n",
	}
	roots := paths.Roots{
		"console": {PackagePath: "fmt", Source: "toolchain"},
		"api": {Children: map[string]paths.Namespace{
			"effects": {PackagePath: "fixture/effects", ModulePath: "fixture"},
			"unused":  {PackagePath: "unavailable.invalid/dependency", ModulePath: "unavailable.invalid/dependency"},
		}},
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := t.TempDir()
		for name, source := range sources {
			writeDiagnosticModuleFile(t, directory, name, []byte(source))
		}
		if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Roots: roots}}); err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(directory, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt != 0 && !bytes.Equal(previous, output) {
			t.Fatalf("same input generated different Go:\n%s\n%s", previous, output)
		}
		previous = output
		file, err := goparser.ParseFile(token.NewFileSet(), "main.go", output, goparser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, imported := range file.Imports {
			if imported.Path.Value == `"fixture/effects"` {
				count++
				if imported.Name == nil || imported.Name.Name == "_" {
					t.Fatalf("ordinary and blank uses did not merge:\n%s", output)
				}
			}
		}
		if count != 1 || strings.Contains(string(output), "$goxide$roots") {
			t.Fatalf("namespace leaked or imports were duplicated:\n%s", output)
		}
		unchanged, err := os.ReadFile(filepath.Join(directory, "effects/native.go"))
		if err != nil || string(unchanged) != native {
			t.Fatalf("native Go changed: %q, %v", unchanged, err)
		}
		if attempt == 0 {
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "."); got != "initialized\n9\n" {
				t.Fatalf("side effect output = %q", got)
			}
			runGo(t, directory, "test", "./...")
		}
	}
}

func TestCompileModulesRejectInvalidRootsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name, use, want string
		roots           paths.Roots
	}{
		{"unknown-root", "use fmt;", "unknown dependency root", paths.Roots{}},
		{"blank-group", "use api as _;", "concrete package", paths.Roots{"api": {Children: map[string]paths.Namespace{"fmt": {PackagePath: "fmt", Source: "toolchain"}}}}},
		{"blank-wrong-owner", "use api as _;", "belongs to module", paths.Roots{"api": {PackagePath: "fixture/child", ModulePath: "another"}}},
		{"blank-false-toolchain", "use api as _;", "not supplied by the Go toolchain", paths.Roots{"api": {PackagePath: "fixture/child", Source: "toolchain"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := "package main\n" + test.use + "\nfunc main() {}\n"
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "main.gox", []byte(source))
			writeDiagnosticModuleFile(t, directory, "child/child.gox", []byte("package child\npub func Value() int { return 1 }\n"))
			err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Roots: test.roots}})
			var diagnostic *lower.Diagnostic
			if !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message, test.want) || diagnostic.File != "fixture/main.gox" || diagnostic.Span.Start == 0 || diagnostic.Span.End > uint32(len(source)) {
				t.Fatalf("missing located %q diagnostic: %v", test.want, err)
			}
			for _, name := range []string{"main", "child/child"} {
				if _, err := os.Stat(filepath.Join(directory, name+".gox")); err != nil {
					t.Fatalf("failure removed source: %v", err)
				}
				if _, err := os.Stat(filepath.Join(directory, name+".go")); !os.IsNotExist(err) {
					t.Fatalf("failure wrote generated Go: %v", err)
				}
			}
		})
	}
}

func TestCompileModulesPublicLocalTypeAlias(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":      "module fixture\n\ngo 1.27\n",
		"lib/api.gox": "package lib\ntype Value int\ntype Box[T any] struct { Value T }\npub use Value as Renamed;\npub use Box as RenamedBox;\n",
		"main.go":     "package main\nimport (\"fmt\"; \"fixture/lib\")\nfunc main() { var value lib.Renamed = 7; boxed := lib.RenamedBox[int]{Value: 8}; fmt.Println(value, boxed.Value) }\n",
	} {
		writeDiagnosticModuleFile(t, directory, name, []byte(source))
	}
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Roots: paths.Roots{}}}); err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "."); got != "7 8\n" {
		t.Fatalf("native Go alias output = %q", got)
	}
}

func TestCompileModulesChecksEmittedImportCycles(t *testing.T) {
	for _, test := range []struct {
		name, a, b    string
		cycle, native bool
	}{
		{"unused", "package a\nuse fixture::b;\npub func Value() int { return 1 }\n", "package b\nuse fixture::a;\npub func Value() int { return 2 }\n", false, false},
		{"used", "package a\nuse fixture::b;\npub func Value() int { return b::Value() }\n", "package b\nuse fixture::a;\npub func Value() int { return a::Value() }\n", true, false},
		{"blank", "package a\nuse fixture::b as _;\npub func Value() int { return 1 }\n", "package b\nuse fixture::a as _;\npub func Value() int { return 2 }\n", true, false},
		{"native", "package a\nuse fixture::b as _;\npub func Value() int { return 1 }\n", "package b\nimport _ \"fixture/a\"\n", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "a/a.gox", []byte(test.a))
			bName := "b/b.gox"
			spanStart := strings.Index(test.b, "use")
			if test.native {
				bName, spanStart = "b/b.go", strings.Index(test.b, `_ "fixture/a"`)
			}
			writeDiagnosticModuleFile(t, directory, bName, []byte(test.b))
			err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory}})
			if !test.cycle {
				if err != nil {
					t.Fatal(err)
				}
				runGo(t, directory, "build", "./...")
				return
			}
			var diagnostic *lower.Diagnostic
			if !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message, "Go import cycle: fixture/a -> fixture/b -> fixture/a") || diagnostic.File != "fixture/"+bName || diagnostic.Span.Start != uint32(spanStart) {
				t.Fatalf("missing source import cycle diagnostic: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "a/a.go")); !os.IsNotExist(err) {
				t.Fatalf("cycle wrote partial output: %v", err)
			}
		})
	}
}
