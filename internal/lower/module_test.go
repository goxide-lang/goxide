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

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
)

func TestCompileModulesMappedBlankImports(t *testing.T) {
	const native = "package effects\nimport \"fmt\"\nfunc init() { fmt.Println(\"initialized\") }\n"
	sources := map[string]string{
		"go.mod":                "module fixture\n\ngo 1.27\n",
		"main.gox":              "package main\nuse api as _;\nuse api as effects;\nuse console;\nfunc main() { console::Println(effects::Value()) }\n",
		"effects/source.gox":    "package effects\npub func Value() int { return 9 }\n",
		"effects/native.go":     native,
		"effects/value_test.go": "package effects_test\nimport (\"testing\"; \"fixture/effects\")\nfunc TestValue(t *testing.T) { if effects.Value() != 9 { t.Fatal(\"wrong value\") } }\n",
	}
	roots := paths.Roots{
		"console": {PackagePath: "fmt", Source: "toolchain"},
		"api":     {PackagePath: "fixture/effects", ModulePath: "fixture"},
		"unused":  {GoPrefix: "unavailable.invalid/dependency", ModulePath: "unavailable.invalid/dependency", Source: "module"},
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := t.TempDir()
		for name, source := range sources {
			writeDiagnosticModuleFile(t, directory, name, []byte(source))
		}
		if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: roots}}); err != nil {
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
		{"unknown-root", "use nonexistent_root;", "unknown dependency root", paths.Roots{}},
		{"blank-group", "use api as _;", "concrete package", paths.Roots{"api": {GoPrefix: "archive", Source: "toolchain"}}},
		{"blank-wrong-owner", "use api as _;", "belongs to module", paths.Roots{"api": {PackagePath: "fixture/child", ModulePath: "another"}}},
		{"blank-false-toolchain", "use api as _;", "not supplied by the Go toolchain", paths.Roots{"api": {PackagePath: "fixture/child", Source: "toolchain"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := "package main\n" + test.use + "\nfunc main() {}\n"
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "main.gox", []byte(source))
			writeDiagnosticModuleFile(t, directory, "child/child.gox", []byte("package child\npub func Value() int { return 1 }\n"))
			err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: test.roots}})
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

func TestCompileModulesMappedNamespacesRequireStaticPaths(t *testing.T) {
	for _, test := range []struct{ name, use, body, selector, receiver string }{
		{"function", "use fmt;", `func main() { fmt.Println("wrong") }`, "fmt.Println", "fmt"},
		{"type", "use bytes;", "var buffer bytes.Buffer\nfunc main() {}", "bytes.Buffer", "bytes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := "package main\n" + test.use + "\n" + test.body + "\n"
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "main.gox", []byte(source))
			writeDiagnosticModuleFile(t, directory, "other.gox", []byte("package main\nfunc Other() {}\n"))
			roots := paths.Roots{
				"fmt":   {PackagePath: "fmt", Source: "toolchain"},
				"bytes": {PackagePath: "bytes", Source: "toolchain"},
			}
			err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: roots}})
			var diagnostic resolve.Diagnostic
			start := uint32(strings.Index(source, test.selector))
			if !errors.As(err, &diagnostic) || diagnostic.Code != "namespace-as-value" || diagnostic.File != "fixture/main.gox" || diagnostic.Span.Start != start || diagnostic.Span.End != start+uint32(len(test.receiver)) {
				t.Fatalf("missing source namespace diagnostic: %v", err)
			}
			if !strings.Contains(err.Error(), "static :: path") || strings.Contains(err.Error(), "$goxide$roots") {
				t.Fatalf("namespace diagnostic exposed routing or omitted the correction: %v", err)
			}
			for _, name := range []string{"main", "other"} {
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

func TestCompileModulesMappedNamespacesKeepValueBindings(t *testing.T) {
	const source = `package main
use fmt;
use bytes;
use fmt::Println as print;
use api as output;
func main() {
    var buffer bytes::Buffer
    buffer.WriteString("buffer")
    fmt := struct { Text string }{Text: "local"}
    fmt::Println("static", buffer.String())
    print(fmt.Text)
    output::Println("nested")
}
`
	roots := paths.Roots{
		"fmt":   {PackagePath: "fmt", Source: "toolchain"},
		"bytes": {PackagePath: "bytes", Source: "toolchain"},
		"api":   {PackagePath: "fmt", Source: "toolchain"},
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := t.TempDir()
		writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
		writeDiagnosticModuleFile(t, directory, "main.gox", []byte(source))
		if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: roots}}); err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(directory, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output), "$goxide$roots") {
			t.Fatalf("namespace routing leaked into Go:\n%s", output)
		}
		if attempt != 0 && !bytes.Equal(previous, output) {
			t.Fatalf("same input generated different Go:\n%s\n%s", previous, output)
		}
		previous = output
		if attempt == 0 {
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "."); got != "static buffer\nlocal\nnested\n" {
				t.Fatalf("namespace and value output = %q", got)
			}
		}
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
	if err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t), Roots: paths.Roots{}}}); err != nil {
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
			err := lower.CompileModules([]lower.Module{{Path: "fixture", Dir: directory, Config: testConfig(t)}})
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
