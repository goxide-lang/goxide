package check_test

import (
	"fmt"
	goast "go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

type checkedImport func(string) (*types.Package, error)

func (f checkedImport) Import(path string) (*types.Package, error) { return f(path) }

func TestBuiltinEnumsRequireSelectedSumTypes(t *testing.T) {
	const sumPath = "github.com/goxide-lang/std/sum"
	for _, test := range []struct{ name, sum, source, want string }{
		{"unused", "", `func f() int { return 1 }`, ""},
		{"import failure", "", `func f(x Option[int]) Option[int] { return x }`, "sum export data unavailable"},
		{"bare some", "", `func f() { _ = Some(1) }`, "sum export data unavailable"},
		{"bare none", "", `func f() { _ = None }`, "sum export data unavailable"},
		{"bare ok", "", `func f() { _ = Ok(1) }`, "sum export data unavailable"},
		{"bare err", "", `func f() { _ = Err(nil) }`, "sum export data unavailable"},
		{"missing type", `type Result[T, E any] struct{}`, `func f(x Option[int]) {}`, "Option must be a named type with 1 type parameters"},
		{"not a type", `const Option = 1`, `func f(x Option[int]) {}`, "Option must be a named type with 1 type parameters"},
		{"wrong arity", `type Option[T, E any] struct{}`, `func f(x Option[int]) {}`, "Option must be a named type with 1 type parameters"},
		{"missing result", `type Option[T any] struct{}`, `func f(x Result[int, error]) {}`, "Result must be a named type with 2 type parameters"},
		{"selected type", `type Option[T any] struct{ Value T }`, `func f(x Option[int]) Option[int] { return x }`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sum *types.Package
			if test.sum != "" {
				set := token.NewFileSet()
				file, err := goparser.ParseFile(set, "sum.go", "package sum\n"+test.sum, 0)
				if err != nil {
					t.Fatal(err)
				}
				sum, err = new(types.Config).Check(sumPath, set, []*goast.File{file}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			sumImports := 0
			imports := checkedImport(func(path string) (*types.Package, error) {
				if path != sumPath {
					return importer.Default().Import(path)
				}
				sumImports++
				if sum == nil {
					return nil, fmt.Errorf("sum export data unavailable")
				}
				return sum, nil
			})
			files, resolved := parse(t, "package fixture\n"+test.source)
			info, diagnostics := check.CheckWithImporter(files, resolved, imports, nil)
			if test.want != "" {
				for _, diagnostic := range diagnostics {
					if diagnostic.Code == "go-import" && strings.Contains(diagnostic.Message, test.want) {
						return
					}
				}
				t.Fatalf("missing dependency diagnostic %q: %v", test.want, diagnostics)
			}
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			if test.name == "unused" && sumImports != 0 {
				t.Fatal("checking a program without sum types imported sum")
			}
			if sum != nil {
				for _, typ := range info.Symbols {
					if signature, ok := typ.(*types.Signature); ok && signature.Params().Len() == 1 {
						actual := signature.Params().At(0).Type().(*types.Named)
						if actual.Origin() != sum.Scope().Lookup("Option").Type() {
							t.Fatal("checker replaced the selected sum type")
						}
						return
					}
				}
				t.Fatal("missing checked function signature")
			}
		})
	}
}

func TestNativeSumUsePreservesDependencyError(t *testing.T) {
	sum := types.NewPackage("github.com/goxide-lang/std/sum", "sum")
	option := types.NewNamed(types.NewTypeName(0, sum, "Option", nil), types.NewStruct(nil, nil), nil)
	option.SetTypeParams([]*types.TypeParam{types.NewTypeParam(types.NewTypeName(0, sum, "T", nil), types.NewInterfaceType(nil, nil).Complete())})
	value, err := types.Instantiate(nil, option, []types.Type{types.Typ[types.Int]}, true)
	if err != nil {
		t.Fatal(err)
	}
	native := types.NewPackage("native", "native")
	native.Scope().Insert(types.NewFunc(0, native, "Read", types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, native, "", value)), false)))
	for _, expression := range []string{
		`match Read() { _ => 1 }`,
		`match Read() { Some(v) => v, None => 0 }`,
		`enum_valid(Read())`,
		`Read()?`,
	} {
		t.Run(expression, func(t *testing.T) {
			file, diagnostics := parser.ParseFile("main.gox", []byte("package main; use native::Read; func f() { _ = "+expression+" }"))
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			files := map[string]*ast.File{"main.gox": file}
			resolved := resolve.Resolve(files, canonicalImports{"native": {{Name: resolve.Key{Name: "Read"}, Kind: resolve.Function, GoName: "Read", Visibility: resolve.Public}}})
			if len(resolved.Diagnostics) != 0 {
				t.Fatal(resolved.Diagnostics)
			}
			_, checked := check.CheckWithImporter(files, resolved, checkedImport(func(path string) (*types.Package, error) {
				if path == "native" {
					return native, nil
				}
				return nil, fmt.Errorf("sum export data unavailable")
			}), nil)
			for _, diagnostic := range checked {
				if diagnostic.Code == "go-import" && strings.Contains(diagnostic.Message, "sum export data unavailable") {
					return
				}
			}
			t.Fatalf("native sum operation lost dependency error: %v", checked)
		})
	}
}

func TestGoResultReportsMissingSumDependency(t *testing.T) {
	file, diagnostics := parser.ParseFile("main.gox", []byte(`package main
func pair() (int, error) { return 1, nil }
func f() { _ = go_result!(pair()) }
`))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	expanded, err := expand.Expand([]*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolve.ResolveExpanded(map[string]*ast.File{"main.gox": file}, expanded, nil)
	if len(resolved.Diagnostics) != 0 {
		t.Fatal(resolved.Diagnostics)
	}
	info, checked := check.CheckWithImporter(map[string]*ast.File{"main.gox": expanded.Files[0]}, resolved, checkedImport(func(path string) (*types.Package, error) {
		return nil, fmt.Errorf("missing selected dependency %s", path)
	}), nil)
	for _, diagnostic := range checked {
		if diagnostic.Code == "go-import" && strings.Contains(diagnostic.Message, "missing selected dependency github.com/goxide-lang/std/sum") {
			if len(info.GoResults) != 0 {
				t.Fatal("failed sum import created a Go result adapter")
			}
			return
		}
	}
	t.Fatalf("missing sum import diagnostic: %v", checked)
}

func TestCheckPreservesResolvedPackageImportError(t *testing.T) {
	file, diagnostics := parser.ParseFile("main.gox", []byte(`package main; use broken::F; func f() { F() }`))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	files := map[string]*ast.File{"main.gox": file}
	resolved := resolve.Resolve(files, canonicalImports{"broken": {{Name: resolve.Key{Name: "F"}, Kind: resolve.Function, GoName: "F", Visibility: resolve.Public}}})
	if len(resolved.Diagnostics) != 0 {
		t.Fatal(resolved.Diagnostics)
	}
	_, checked := check.CheckWithImporter(files, resolved, checkedImport(func(path string) (*types.Package, error) {
		return nil, fmt.Errorf("corrupt export data for %s", path)
	}), nil)
	for _, diagnostic := range checked {
		if diagnostic.Code == "go-import" && strings.Contains(diagnostic.Message, "corrupt export data for broken") {
			return
		}
	}
	t.Fatalf("resolved package import error was lost: %v", checked)
}
