package frontend

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
)

func deferredUseRoots() paths.Roots {
	return paths.Roots{
		"fmt":     {PackagePath: "fmt", Source: "toolchain"},
		"strings": {PackagePath: "strings", Source: "toolchain"},
		"strconv": {PackagePath: "strconv", Source: "toolchain"},
		"time":    {PackagePath: "time", Source: "toolchain"},
	}
}

func TestDeferredMatchUseBindings(t *testing.T) {
	const source = `package subject
use strings;
use fmt::Sprint as Text;
use strconv::Itoa as Number;
use time::Duration as Duration;
func item() Option[int] { return Option::Some(3) }
func Namespace() string {
 result := ""
 match item() { Option::Some(n) => { result = strings::Repeat("x", n) }, Option::None => { result = "empty" }, }
 return result
}
func Items() string {
 result := ""
 match item() { Option::Some(n) => { result = Text(n) }, Option::None => { result = "empty" }, }
 match item() { Option::Some(n) => { result += Number(n + 1) }, Option::None => { result = "empty" }, }
 match item() { Option::Some(n) => { result += Text(int64(Duration(n))) }, Option::None => { result = "empty" }, }
 return result
}
func Shadow() string {
 Text := func(int) string { return "outer" }
 result := ""
 match item() { Option::Some(n) => { result = Text(n); Number := func(int) string { return "inner" }; result += Number(n) }, Option::None => { result = "empty" }, }
 return result
}
`
	for _, packageMode := range []bool{false, true} {
		name := "single"
		if packageMode {
			name = "package"
		}
		t.Run(name, func(t *testing.T) {
			imp := typecheck.NewImporter("match.gox")
			emitted := map[string]string{}
			if packageMode {
				result, err := CompilePackageWithOptions("example.org/deferred", map[string][]byte{
					"match.gox": []byte(source),
					"alias.gox": []byte("package subject\npub use time::Duration as PublicDuration;\nfunc Public()PublicDuration{return PublicDuration(7)}\n"),
				}, PackageOptions{Roots: deferredUseRoots(), Importer: imp})
				if err != nil {
					t.Fatal(err)
				}
				files, err := PackageGoFiles("example.org/deferred", result, imp)
				if err != nil {
					t.Fatal(err)
				}
				for path, data := range files {
					emitted[strings.TrimSuffix(path, filepath.Ext(path))+".go"] = string(data.Go)
				}
			} else {
				result, err := CompileWithOptions("match.gox", []byte(source), FileOptions{Roots: deferredUseRoots(), Importer: imp})
				if err != nil {
					t.Fatal(err)
				}
				emitted["match.go"] = string(result.Go)
			}
			for path, data := range emitted {
				f, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]bool{}
				for _, im := range f.Imports {
					p, _ := strconv.Unquote(im.Path.Value)
					if seen[p] {
						t.Fatalf("repeated route import %s in %s", p, path)
					}
					seen[p] = true
				}
			}
			emitted["go.mod"] = "module example.org/deferred\ngo 1.27\n"
			emitted["match_test.go"] = `package subject
import "testing"
func TestRuntime(t *testing.T) {
 if Namespace() != "xxx" || Items() != "343" || Shadow() != "outerinner" { t.Fatal(Namespace(), Items(), Shadow()) }
}
`
			dir := t.TempDir()
			useWrite(t, dir, emitted)
			useGo(t, dir, "test", "-mod=readonly", "-count=1", "./...")
		})
	}
}

func TestUseRewriteDeferredMarkerAndOrdinaryStrings(t *testing.T) {
	const file = "use.gox"
	u := &usePackage{
		metadata:   useMetadata{Path: "example.org/p"},
		useImports: map[string]map[string]bool{file: {"goxideNamespace0": true}},
	}
	for _, tc := range []struct {
		name, body string
		keep       bool
	}{
		{"reserved marker", `func F(){__goxide_enum_context_match(value,"{ Option::Some(v) => { goxideNamespace0.Print(v) }, }")}`, true},
		{"ordinary string", `func F()string{return "__goxide_enum_context_match(value, goxideNamespace0.Print)"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package p\nimport goxideNamespace0 \"fmt\"\n" + tc.body)
			mapping := sourcemap.Identity(file, source)
			output, err := u.rewrite(file, source, mapping)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, output, 0)
			if err != nil {
				t.Fatal(err)
			}
			if kept := f.Imports[0].Name.Name != "_"; kept != tc.keep {
				t.Fatalf("kept=%v, want %v: %s", kept, tc.keep, output)
			}
			at := strings.Index(string(output), tc.body)
			if at < 0 || mapping.Lookup(at) == nil || mapping.Lookup(at).Origin == nil {
				t.Fatal("unchanged function lost its explicit source origin")
			}
		})
	}
}

func TestUseRewriteReusesBlankAndNamedRoutes(t *testing.T) {
	const file = "use.gox"
	u := &usePackage{
		metadata: useMetadata{Path: "example.org/p"},
		fileBindings: map[string]map[string]useBinding{file: {
			"Text": {Name: "Text", Kind: "function", RoutePath: "fmt", RouteName: "Sprint"},
		}},
	}
	for _, alias := range []string{"_", "goxideUseRoute0"} {
		source := []byte("package p\nimport " + alias + " \"fmt\"\nfunc F()string{return Text(1)}\n")
		mapping := sourcemap.Identity(file, source)
		output, err := u.rewrite(file, source, mapping)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Base(file), output, 0)
		if err != nil || len(f.Imports) != 1 || f.Imports[0].Name.Name == "_" {
			t.Fatalf("route duplicated or blank: %v\n%s", err, output)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "Text" {
				t.Fatal("unresolved use item remains")
			}
			return true
		})
		if err := typecheck.Check(file, output, typecheck.NewImporter(file)); err != nil {
			t.Fatalf("%v\n%s", err, output)
		}
	}
}

// A source package can expose macro/use metadata and enum protocols alongside an
// ordinary function. Macro expansion must not erase its import before the enum
// prelude uses that same route, even if the original source only reexports it.
func TestUseFunctionReexportPreservesEnumImports(t *testing.T) {
	source := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")
	useWrite(t, source, map[string]string{
		"go.mod": "module example.org/enumreexport\ngo 1.27\n",
		"model/model.gox": `package model
#[derive(Default)]
pub enum E { #[default] Empty, Item(int), }
var Count = 0
func Plain() int { return 7 }
`,
		"api/api.gox": `package api
use model as p;
pub use p::Plain;
`,
		"api/init.go": `package api
import "example.org/enumreexport/model"
func init() { model.Count++ }
`,
		"aliases/api.gox": `package aliases
use model as p;
pub use p::E as Public;
`,
		"client/client.gox": `package client
use api;
use aliases;
func Read() int { return api::Plain() }
func Empty() aliases::Public { return aliases::Public::Empty }
`,
		"client/client_test.go": `package client
import "testing"
import "example.org/enumreexport/model"
func TestRun(t *testing.T) {
 if Read() != 7 || model.Count != 1 { t.Fatal(Read(), model.Count) }
 model.ValidateE(Empty())
}
`,
	})
	options := useTestOptions(map[string]string{
		"model": "example.org/enumreexport/model", "api": "example.org/enumreexport/api", "aliases": "example.org/enumreexport/aliases",
	})
	if _, err := CompileModuleWithOptions(source, out, options); err != nil {
		t.Fatal(err)
	}
	useGo(t, out, "test", "./...")
}

func TestStageErrorRespectsSourceOrigin(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		diagnostic := &sourcemap.Error{Message: "wrong selector", Generated: token.Position{Filename: "generated.gox", Line: 19, Column: 13}}
		if mapped {
			diagnostic.Origin = &sourcemap.Span{File: "original.gox", Line: 7, Column: 3, Start: 42, End: 48}
		}
		message := stageError("propagate.Lower", diagnostic).Error()
		if got := strings.Contains(message, "generated/unmapped"); got == mapped {
			t.Fatalf("mapped=%v: incorrect diagnostic provenance: %s", mapped, message)
		}
		if mapped && !strings.Contains(message, "original.gox:7:3") {
			t.Fatalf("lost original coordinates: %s", message)
		}
	}
}
