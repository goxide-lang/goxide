package lower

import (
	"fmt"
	goast "go/ast"
	"go/parser"
	gotoken "go/token"
	"go/types"
	"testing"
)

func TestMemberNamesFollowObjectsAndPromotion(t *testing.T) {
	fset := gotoken.NewFileSet()
	file := memberTestFile(t, fset, "p/p.go", `package p
type A[T any] struct { value T }
type B struct { value int }
type Outer struct { A[int] }
func (a A[T]) next() T { return a.value }
func f() int {
	a := A[int]{value: 1}
	b := B{value: 2}
	o := Outer{A: a}
	return a.value + b.value + o.value + a.next()
}`)
	names := memberTestNames(file, "A", map[string]string{"value": "Value", "next": "Next"})
	var renamer memberRenamer
	renamer.rename("p", fset, []*goast.File{file}, names, nil)
	memberTestCheck(t, "p", fset, file, nil)
	selectors := map[string]int{}
	keys := map[string]int{}
	goast.Inspect(file, func(node goast.Node) bool {
		switch n := node.(type) {
		case *goast.SelectorExpr:
			selectors[n.Sel.Name]++
		case *goast.KeyValueExpr:
			if key, ok := n.Key.(*goast.Ident); ok {
				keys[key.Name]++
			}
		}
		return true
	})
	if selectors["Value"] != 3 || selectors["value"] != 1 || selectors["Next"] != 1 {
		t.Fatalf("member selections = %v", selectors)
	}
	if keys["Value"] != 1 || keys["value"] != 1 {
		t.Fatalf("struct keys = %v", keys)
	}
}

func TestMemberNamesAcrossPackagesAndChainedSelectors(t *testing.T) {
	fset := gotoken.NewFileSet()
	dependency := memberTestFile(t, fset, "p/p.go", `package p
type A struct { value int; child *A }
func New() *A { return &A{value: 1, child: &A{value: 2}} }
func (a *A) next() *A { return a }
`)
	var renamer memberRenamer
	pkg := renamer.rename("p", fset, []*goast.File{dependency}, memberTestNames(dependency, "A", map[string]string{"value": "Value", "child": "Child", "next": "Next"}), nil)
	imports := memberTestImporter{"p": pkg}
	caller := memberTestFile(t, fset, "q/q.go", `package q
import "p"
func f() int { return p.New().next().child.value + p.A{value: 3}.value }
`)
	renamer.rename("q", fset, []*goast.File{caller}, nil, imports)
	memberTestCheck(t, "p", fset, dependency, nil)
	memberTestCheck(t, "q", fset, caller, imports)
	goast.Inspect(caller, func(node goast.Node) bool {
		if id, ok := node.(*goast.Ident); ok && (id.Name == "value" || id.Name == "child" || id.Name == "next") {
			t.Errorf("unmapped imported member %s", id.Name)
		}
		return true
	})
}

func TestMemberNamesForRenamedEmbeddedType(t *testing.T) {
	fset := gotoken.NewFileSet()
	file := memberTestFile(t, fset, "p/p.go", `package p
type _Private struct { N int }
type Outer struct { Private }
func f(o Outer) int { return o.Private.N }
`)
	names := map[*goast.Ident]string{}
	goast.Inspect(file, func(node goast.Node) bool {
		if field, ok := node.(*goast.Field); ok && len(field.Names) == 0 {
			if id := embeddedIdent(field.Type); id != nil && id.Name == "Private" {
				names[id] = "_Private"
			}
		}
		return true
	})
	var renamer memberRenamer
	renamer.rename("p", fset, []*goast.File{file}, names, nil)
	memberTestCheck(t, "p", fset, file, nil)
	goast.Inspect(file, func(node goast.Node) bool {
		if id, ok := node.(*goast.Ident); ok && id.Name == "Private" {
			t.Error("embedded field kept its source name")
		}
		return true
	})
}

func memberTestFile(t *testing.T, fset *gotoken.FileSet, name, source string) *goast.File {
	t.Helper()
	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func memberTestNames(file *goast.File, typeName string, mapped map[string]string) map[*goast.Ident]string {
	names := map[*goast.Ident]string{}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *goast.GenDecl:
			for _, spec := range decl.Specs {
				typ, ok := spec.(*goast.TypeSpec)
				if !ok || typ.Name.Name != typeName {
					continue
				}
				structure := typ.Type.(*goast.StructType)
				for _, field := range structure.Fields.List {
					for _, id := range field.Names {
						if name := mapped[id.Name]; name != "" {
							names[id] = name
						}
					}
				}
			}
		case *goast.FuncDecl:
			if decl.Recv != nil && embeddedIdent(decl.Recv.List[0].Type).Name == typeName {
				if name := mapped[decl.Name.Name]; name != "" {
					names[decl.Name] = name
				}
			}
		}
	}
	return names
}

func memberTestCheck(t *testing.T, name string, fset *gotoken.FileSet, file *goast.File, importer types.Importer) {
	t.Helper()
	config := types.Config{Importer: importer}
	if _, err := config.Check(name, fset, []*goast.File{file}, nil); err != nil {
		t.Fatal(err)
	}
}

type memberTestImporter map[string]*types.Package

func (i memberTestImporter) Import(path string) (*types.Package, error) {
	if pkg := i[path]; pkg != nil {
		return pkg, nil
	}
	return nil, fmt.Errorf("missing test package %s", path)
}
