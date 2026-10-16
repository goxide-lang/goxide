package lower_test

import (
	"bytes"
	goast "go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/resolve"
)

func TestUseCommentsStayWithImports(t *testing.T) {
	tests := []struct {
		name, source, output string
		docs                 map[string]string
		retained             []string
	}{
		{
			name: "single",
			source: `// Package main documents its imports.
package main

// Formatting tools.
use fmt;

// main runs the fixture.
func main() { fmt::Println("single") }
`,
			output: "single\n",
			docs:   map[string]string{"fmt": "Formatting tools."},
		},
		{
			name: "merged_aliases_and_unused",
			source: `// Package main documents its imports.
package main

// Formatting tools.
use fmt;
// The second formatting spelling.
use fmt as output;
// Reserved byte helpers.
use bytes;
// String tools.
use strings;

// main runs the fixture.
func main() { fmt::Println(output::Sprintf("%s", strings::Repeat("x", 2))) }
`,
			output:   "xx\n",
			docs:     map[string]string{"fmt": "Formatting tools.", "strings": "String tools."},
			retained: []string{"// The second formatting spelling.", "// Reserved byte helpers."},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := parseDiagnosticSources(t, map[string]string{"main.gox": test.source})
			output, err := lower.Lower(files, checked(t, files, resolve.Resolve(files, nil)))
			if err != nil {
				t.Fatal(err)
			}
			source := output["main.gox"]
			formatted, err := format.Source(source)
			if err != nil || !bytes.Equal(source, formatted) {
				t.Fatalf("output is not formatted Go: %v\n%s", err, source)
			}
			file, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			if file.Doc == nil || !strings.Contains(file.Doc.Text(), "Package main documents its imports.") {
				t.Fatalf("package documentation moved:\n%s", source)
			}
			seen := map[string]string{}
			for _, declaration := range file.Decls {
				switch declaration := declaration.(type) {
				case *goast.GenDecl:
					if declaration.Tok != token.IMPORT {
						continue
					}
					for _, spec := range declaration.Specs {
						imported := spec.(*goast.ImportSpec)
						name, err := strconv.Unquote(imported.Path.Value)
						if err != nil {
							t.Fatal(err)
						}
						if _, duplicate := seen[name]; duplicate {
							t.Fatalf("duplicate import %s", name)
						}
						doc := imported.Doc
						if doc == nil && len(declaration.Specs) == 1 {
							doc = declaration.Doc
						}
						if doc == nil {
							t.Fatalf("documentation detached from import %s:\n%s", name, source)
						}
						seen[name] = strings.TrimSpace(doc.Text())
					}
				case *goast.FuncDecl:
					if declaration.Name.Name == "main" && (declaration.Doc == nil || strings.TrimSpace(declaration.Doc.Text()) != "main runs the fixture.") {
						t.Fatalf("function documentation absorbed import comments:\n%s", source)
					}
				}
			}
			if len(seen) != len(test.docs) {
				t.Fatalf("imports = %v, want %v", seen, test.docs)
			}
			for name, doc := range test.docs {
				if seen[name] != doc {
					t.Fatalf("import %s doc = %q, want %q\n%s", name, seen[name], doc, source)
				}
			}
			for _, comment := range test.retained {
				if bytes.Count(source, []byte(comment)) != 1 {
					t.Fatalf("expected exactly one retained %q:\n%s", comment, source)
				}
			}
			directory := t.TempDir()
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "main.go", source)
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "."); got != test.output {
				t.Fatalf("output = %q, want %q", got, test.output)
			}
		})
	}
}
