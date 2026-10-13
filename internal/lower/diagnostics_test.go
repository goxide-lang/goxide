package lower_test

import (
	"errors"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func TestUnsupportedFeaturesReturnLocatedDiagnosticsWithoutPartialOutput(t *testing.T) {
	tests := []struct {
		name, source, feature string
	}{
		{"enum", "enum Value { Empty, }", "enum"},
		{"match", "func f(value int) int { return match value { _ => 1 } }", "match"},
		{"propagation", "func f() int { return missing()? }", "?"},
		{"macro_expression", "var value = missing!()", "宏"},
		{"macro_item", "missing!()", "宏"},
		{"macro_type", "type Value = missing!()", "宏"},
		{"macro_definition", "macro_rules! value { () => { 1 }; }", "宏"},
		{"reference_type", "var value &int", "引用"},
		{"mutable_reference", "func f() { value := 1; _ = &mut value }", "引用"},
		{"trait", "trait Value { func Read() int }", "trait"},
		{"impl", "type Value int; impl Value { func Read() int { return 1 } }", "impl"},
		{"derive", "#[derive(Debug)] type Value int", "属性"},
		{"bar_closure", "var f = |x| x + 1", "闭包"},
		{"unsafe_function", "unsafe func f() {}", "修饰符"},
		{"unsafe_expression", "func f() int { return 1 }; var value = unsafe f()", "unsafe"},
		{"where", "func f[T any](value T) T where T: any { return value }", "where"},
		{"module", "mod child;", "mod"},
		{"declare", "declare func f() int", "declare"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "package p\n" + test.source + "\n"
			files := parseDiagnosticSources(t, map[string]string{
				"a-good.gox": "package p\nvar ordinary = 1\n",
				"z-bad.gox":  source,
			})
			output, err := lower.Lower(files, resolve.Resolve(files, nil))
			if err == nil {
				t.Fatalf("unsupported %s produced Go: %s", test.name, output["z-bad.gox"])
			}
			if output != nil {
				t.Fatalf("failure leaked partial output: %v", output)
			}
			var diagnostic *lower.Diagnostic
			if !errors.As(err, &diagnostic) {
				t.Fatalf("error has no structured lower diagnostic: %T %v", err, err)
			}
			if diagnostic.File != "z-bad.gox" || diagnostic.Span.Start == 0 || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > uint32(len(source)) {
				t.Fatalf("diagnostic has no valid source location: %#v", diagnostic)
			}
			if diagnostic.Severity != lower.Error || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
				t.Fatalf("diagnostic lost severity or origin: %#v", diagnostic)
			}
			if !strings.Contains(diagnostic.Message, "尚未降低") || !strings.Contains(diagnostic.Message, test.feature) {
				t.Fatalf("diagnostic does not identify unsupported feature %q: %v", test.feature, diagnostic)
			}
			if !strings.Contains(err.Error(), diagnostic.File) || !strings.Contains(err.Error(), diagnostic.Message) {
				t.Fatalf("error text omits location or message: %v", err)
			}
		})
	}
}

func TestRawGoKeywordHasLocatedDiagnostic(t *testing.T) {
	const source = "package p\nfunc f() { r#type := 1; _ = r#type }\n"
	files := parseDiagnosticSources(t, map[string]string{"keyword.gox": source})
	output, err := lower.Lower(files, resolve.Resolve(files, nil))
	var diagnostic *lower.Diagnostic
	if output != nil || !errors.As(err, &diagnostic) {
		t.Fatalf("raw Go keyword was not rejected atomically: output=%v, error=%v", output, err)
	}
	start := uint32(strings.Index(source, "r#type"))
	if diagnostic.File != "keyword.gox" || diagnostic.Span != (ast.Span{Start: start, End: start + 6}) {
		t.Fatalf("keyword diagnostic points to wrong source location: %#v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "type") || !strings.Contains(diagnostic.Message, "Go") {
		t.Fatalf("keyword diagnostic is unclear: %v", diagnostic)
	}
}

func TestBlankImportsRetainSideEffectsAndUnusedImportsDisappear(t *testing.T) {
	const source = `package main
use fixture::side as _;
use strings;
func main() {}
`
	files := parseDiagnosticSources(t, map[string]string{"main.gox": source})
	output, err := lower.Lower(files, resolve.Resolve(files, nil))
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(token.NewFileSet(), "main.go", output["main.gox"], 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Imports) != 1 || file.Imports[0].Name == nil || file.Imports[0].Name.Name != "_" || file.Imports[0].Path.Value != `"fixture/side"` {
		t.Fatalf("blank import or unused import handling is incorrect:\n%s", output["main.gox"])
	}
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.go", output["main.gox"])
	writeDiagnosticModuleFile(t, directory, "side/side.go", []byte("package side\nimport \"fmt\"\nfunc init() { fmt.Println(\"side initialized\") }\n"))
	if got := runGo(t, directory, "run", "."); got != "side initialized\n" {
		t.Fatalf("blank import initialization output = %q", got)
	}
}

func TestImportAliasAvoidsLocalNameCollision(t *testing.T) {
	const source = `package main
use fmt;
func main() {
	fmt := 42
	fmt::Println(fmt)
}
`
	files := parseDiagnosticSources(t, map[string]string{"main.gox": source})
	res := resolve.Resolve(files, nil)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("resolve: %v", res.Diagnostics)
	}
	output, err := lower.Lower(files, res)
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(token.NewFileSet(), "main.go", output["main.gox"], 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Imports) != 1 || file.Imports[0].Name == nil || file.Imports[0].Name.Name == "fmt" || file.Imports[0].Path.Value != `"fmt"` {
		t.Fatalf("import alias does not avoid the local fmt binding:\n%s", output["main.gox"])
	}
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.go", output["main.gox"])
	if got := runGo(t, directory, "run", "."); got != "42\n" {
		t.Fatalf("renamed import output = %q", got)
	}
}

func parseDiagnosticSources(t *testing.T, sources map[string]string) map[string]*ast.File {
	t.Helper()
	files := make(map[string]*ast.File, len(sources))
	for name, source := range sources {
		file, diagnostics := parser.ParseFile(name, []byte(source))
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == parser.Error {
				t.Fatalf("parse %s: %v", name, diagnostics)
			}
		}
		files[name] = file
	}
	return files
}

func writeDiagnosticModuleFile(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	filename := filepath.Join(directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatal(err)
	}
}
