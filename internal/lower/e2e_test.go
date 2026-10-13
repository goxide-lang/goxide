package lower_test

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	syntaxparser "github.com/hxsf/goxide/internal/syntax/parser"
)

func TestPureGoEndToEnd(t *testing.T) {
	files := parsePureFixtures(t)
	first := resolve.Resolve(files, nil)
	if len(first.Diagnostics) != 0 {
		t.Fatalf("initial resolve: %v", first.Diagnostics)
	}
	res := resolve.Resolve(files, fixtureImporter{first})
	if len(res.Diagnostics) != 0 {
		t.Fatalf("resolve: %v", res.Diagnostics)
	}
	output, err := lower.Lower(files, checked(t, files, res))
	if err != nil {
		t.Fatal(err)
	}
	again, err := lower.Lower(files, checked(t, files, res))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, again) {
		t.Fatal("lowering identical inputs produced different bytes")
	}
	if len(output) != len(files) {
		t.Fatalf("output count = %d, input count = %d", len(output), len(files))
	}
	for name := range files {
		got, ok := output[name]
		if !ok {
			t.Fatalf("no output for %s", name)
		}
		formatted, err := format.Source(got)
		if err != nil {
			t.Fatalf("invalid Go for %s: %v\n%s", name, err, got)
		}
		if !bytes.Equal(got, formatted) {
			t.Fatalf("output for %s is not gofmt formatted", name)
		}
		rel := strings.TrimPrefix(name, "fixture/")
		rel = strings.TrimSuffix(rel, ".gox") + ".go"
		want, err := os.ReadFile(filepath.Join("testdata", "go", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		assertEquivalentGo(t, name, got, want)
	}
	if !bytes.HasPrefix(output["fixture/main.gox"], []byte("//go:build !goxide_excluded\n\n")) {
		t.Fatal("build constraint was not retained at the file head")
	}
	decls := output["fixture/lib/declarations.gox"]
	for _, comment := range []string{"// offset is shared by both source files.", "// widget retains its field documentation and tag.", "//goxide:lower-fixture"} {
		if bytes.Count(decls, []byte(comment)) != 1 {
			t.Fatalf("expected exactly one retained %q comment", comment)
		}
	}
	declAST, err := parser.ParseFile(token.NewFileSet(), "declarations.go", decls, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	wantDocs := map[string]string{
		"Offset": "offset is shared by both source files.",
		"Widget": "widget retains its field documentation and tag.",
	}
	for _, declaration := range declAST.Decls {
		if group, ok := declaration.(*goast.GenDecl); ok && len(group.Specs) == 1 {
			name := ""
			switch spec := group.Specs[0].(type) {
			case *goast.ValueSpec:
				name = spec.Names[0].Name
			case *goast.TypeSpec:
				name = spec.Name.Name
			}
			if want, ok := wantDocs[name]; ok {
				if group.Doc == nil || !strings.Contains(group.Doc.Text(), want) {
					t.Fatalf("documentation moved away from %s", name)
				}
				delete(wantDocs, name)
			}
		}
	}
	if len(wantDocs) != 0 {
		t.Fatalf("missing documented declarations: %v", wantDocs)
	}
	trailing := false
	for _, line := range strings.Split(string(output["fixture/lib/behavior.gox"]), "\n") {
		if strings.Contains(line, "// closure step") {
			trailing = strings.TrimSpace(strings.SplitN(line, "//", 2)[0]) == "trace += value"
		}
	}
	if !trailing {
		t.Fatalf("trailing statement comment moved away from its statement:\n%s", output["fixture/lib/behavior.gox"])
	}
	for _, name := range []string{"fixture/lib/declarations.gox", "fixture/lib/behavior.gox"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), name, output[name], 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.Imports) != 1 || parsed.Imports[0].Path.Value != `"fmt"` {
			t.Fatalf("imports for %s did not merge aliases and omit unused imports: %#v", name, parsed.Imports)
		}
	}

	generated, err := lower.PrepareModule([]string{filepath.Join("testdata", "pure")}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(generated) })
	for name := range files {
		rel := strings.TrimSuffix(strings.TrimPrefix(name, "fixture/"), ".gox") + ".go"
		prepared, err := os.ReadFile(filepath.Join(generated, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		assertEquivalentGo(t, name, prepared, output[name])
	}
	handwritten := t.TempDir()
	copyGoFixtures(t, handwritten)
	runGo(t, generated, "build", "./...")
	generatedOutput := runGo(t, generated, "run", ".")
	runGo(t, handwritten, "build", "./...")
	handwrittenOutput := runGo(t, handwritten, "run", ".")
	if generatedOutput != handwrittenOutput {
		t.Fatalf("runtime output differs\nlowered: %q\nhandwritten: %q", generatedOutput, handwrittenOutput)
	}
	const wantOutput = "7 ok 5 17/21/7/9/4/go/true/5/8\n"
	if generatedOutput != wantOutput {
		t.Fatalf("runtime output = %q, want %q", generatedOutput, wantOutput)
	}
}

func parsePureFixtures(t *testing.T) map[string]*ast.File {
	t.Helper()
	files := make(map[string]*ast.File)
	groups := make(map[string][]string)
	err := filepath.WalkDir(filepath.Join("testdata", "pure"), func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(name) != ".gox" {
			return nil
		}
		source, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Join("testdata", "pure"), name)
		if err != nil {
			return err
		}
		key := "fixture/" + filepath.ToSlash(rel)
		file, diagnostics := syntaxparser.ParseFile(key, source)
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == syntaxparser.Error {
				return fmt.Errorf("parse %s: %v", key, diagnostics)
			}
		}
		files[key] = file
		groups[path.Dir(key)] = append(groups[path.Dir(key)], key)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range groups {
		sort.Strings(names)
		input := make([]*ast.File, len(names))
		for i, name := range names {
			input[i] = files[name]
		}
		expanded, err := expand.Expand(input, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i, name := range names {
			files[name] = expanded.Files[i]
		}
	}
	return files
}

type fixtureImporter struct{ result *resolve.Result }

func (i fixtureImporter) Lookup(parts []string, name resolve.Key) []resolve.External {
	if strings.Join(parts, "/") == "fmt" {
		for _, candidate := range []string{"Sprintf", "Sprint", "Println"} {
			if name.Name == candidate {
				return []resolve.External{{Name: name, Kind: resolve.Function, GoName: candidate, Visibility: resolve.Public}}
			}
		}
	}
	for _, pkg := range i.result.Packages {
		if filepath.ToSlash(pkg.Dir) != strings.Join(parts, "/") {
			continue
		}
		if symbol := pkg.Scope.Bindings[name]; symbol != nil && symbol.Visibility == resolve.Public {
			return []resolve.External{{Name: name, Kind: symbol.Kind, GoName: symbol.GoName, Visibility: symbol.Visibility}}
		}
	}
	return nil
}

func assertEquivalentGo(t *testing.T, name string, got, want []byte) {
	t.Helper()
	gotImports, gotTokens := comparableGo(t, name, got)
	wantImports, wantTokens := comparableGo(t, name, want)
	if !reflect.DeepEqual(gotImports, wantImports) {
		t.Fatalf("%s imports differ: got %v, want %v", name, gotImports, wantImports)
	}
	if !reflect.DeepEqual(gotTokens, wantTokens) {
		for i := 0; i < len(gotTokens) && i < len(wantTokens); i++ {
			if gotTokens[i] != wantTokens[i] {
				t.Fatalf("%s token %d differs: got %q, want %q\nlowered source:\n%s", name, i, gotTokens[i], wantTokens[i], got)
			}
		}
		t.Fatalf("%s token counts differ: got %d, want %d", name, len(gotTokens), len(wantTokens))
	}
}

func comparableGo(t *testing.T, name string, source []byte) ([]string, []string) {
	t.Helper()
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, name, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := make(map[string]string)
	var imports []string
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		alias := path.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		aliases[alias] = "import_" + fmt.Sprintf("%x", importPath)
		imports = append(imports, importPath)
	}
	sort.Strings(imports)
	var declarations []goast.Decl
	for _, decl := range file.Decls {
		if group, ok := decl.(*goast.GenDecl); ok && group.Tok == token.IMPORT {
			continue
		}
		declarations = append(declarations, decl)
	}
	file.Decls = declarations
	file.Imports = nil
	goast.Inspect(file, func(node goast.Node) bool {
		if selector, ok := node.(*goast.SelectorExpr); ok {
			if name, ok := selector.X.(*goast.Ident); ok && aliases[name.Name] != "" {
				name.Name = aliases[name.Name]
			}
		}
		return true
	})
	var normalized bytes.Buffer
	if err := format.Node(&normalized, set, file); err != nil {
		t.Fatal(err)
	}
	var scan scanner.Scanner
	scan.Init(token.NewFileSet().AddFile(name, -1, normalized.Len()), normalized.Bytes(), nil, 0)
	var tokens []string
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON {
			continue
		}
		if literal == "" {
			literal = kind.String()
		}
		tokens = append(tokens, kind.String()+":"+literal)
	}
	return imports, tokens
}

func copyGoFixtures(t *testing.T, destination string) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join("testdata", "go"), func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Join("testdata", "go"), name)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		output := filepath.Join(destination, rel)
		if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			return err
		}
		return os.WriteFile(output, contents, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "go.mod"), []byte("module fixture\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func runGo(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	t.Logf("%s: go %s\n%s", directory, strings.Join(args, " "), output)
	if err != nil {
		t.Fatalf("go %s: %v", strings.Join(args, " "), err)
	}
	return string(output)
}

func checked(t *testing.T, files map[string]*ast.File, res *resolve.Result) *check.Info {
	t.Helper()
	info, diagnostics := check.Check(files, res)
	if len(diagnostics) != 0 {
		t.Fatalf("check: %v", diagnostics)
	}
	return info
}
