package typecheck

import (
	"errors"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
)

type namespaceLookupImporter struct {
	pkg *types.Package
	err error
}

func (i *namespaceLookupImporter) Import(name string) (*types.Package, error) {
	if name != i.pkg.Path() {
		return nil, fmt.Errorf("not a package: %s", name)
	}
	return i.pkg, i.err
}

func (i *namespaceLookupImporter) PackageIndex(string, string) ([]string, error) {
	return []string{i.pkg.Path()}, nil
}

func (i *namespaceLookupImporter) PackageOrigin(string) (string, bool, error) {
	return i.pkg.Path(), false, nil
}

func TestNamespaceLookupPreservesSelectedPackageError(t *testing.T) {
	pkg := types.NewPackage("example.test/dependency", "dependency")
	pkg.Scope().Insert(types.NewVar(0, pkg, "Value", types.Typ[types.Int]))
	goImports := &namespaceLookupImporter{pkg: pkg}
	imports := NewNamespaceImporter(goImports, paths.Roots{"dependency": {
		Source: "module", ModulePath: pkg.Path(), GoPrefix: pkg.Path(),
	}})
	root := []string{namespaceRootPrefix, "dependency"}
	if got := imports.Lookup(root, resolve.Key{Name: "Missing"}); len(got) != 0 || imports.Err() != nil {
		t.Fatalf("missing member became an import failure: %v, %v", got, imports.Err())
	}
	if got := imports.Lookup(append(root, "Missing"), resolve.Key{Name: "Value"}); len(got) != 0 || imports.Err() != nil {
		t.Fatalf("member path probe became an import failure: %v, %v", got, imports.Err())
	}
	want := errors.New("corrupt export data")
	goImports.err = want
	if got := imports.Lookup(root, resolve.Key{Name: "Value"}); len(got) != 0 || !errors.Is(imports.Err(), want) {
		t.Fatalf("selected package lost its import error: %v, %v", got, imports.Err())
	}
	goImports.err = errors.New("later import failure")
	imports.Lookup(root, resolve.Key{Name: "Value"})
	if !errors.Is(imports.Err(), want) {
		t.Fatalf("later lookup replaced the first error: %v", imports.Err())
	}
}

func TestToolchainPackageTreesAndMembers(t *testing.T) {
	importer := NewImporter("input.gox", ImporterOptions{Config: testConfig(t, nil)})
	names := paths.NewGoNamespaces(importer)
	archive, ok, err := names.Root("archive")
	if err != nil || !ok || archive.PackagePath != "" || archive.GoPrefix != "archive" {
		t.Fatalf("archive grouping namespace = %#v, %t, %v", archive, ok, err)
	}
	tar, ok, err := names.Resolve(archive, []string{"tar"})
	if err != nil || !ok || tar.PackagePath != "archive/tar" || names.Validate(tar) != nil {
		t.Fatalf("archive::tar = %#v, %t, %v", tar, ok, err)
	}
	if _, ok, err := names.Root("json"); err != nil || ok {
		t.Fatalf("bare json guessed a package: %t, %v", ok, err)
	}
	goRoot, ok, err := names.Root("go")
	if err != nil || !ok {
		t.Fatalf("raw keyword grouping namespace: %t, %v", ok, err)
	}
	if ast, ok, err := names.Resolve(goRoot, []string{"ast"}); err != nil || !ok || ast.PackagePath != "go/ast" {
		t.Fatalf("r#go::ast = %#v, %t, %v", ast, ok, err)
	}
	imports := NewNamespaceImporter(importer, nil)
	for _, tc := range []struct {
		namespace []string
		member    string
		kind      resolve.Kind
	}{
		{[]string{"encoding"}, "json", resolve.Module},
		{[]string{"encoding"}, "BinaryMarshaler", resolve.Type},
		{[]string{"encoding", "json"}, "Marshal", resolve.Function},
	} {
		members := imports.Lookup(append([]string{namespaceRootPrefix}, tc.namespace...), resolve.Key{Name: tc.member})
		if len(members) != 1 || members[0].Kind != tc.kind {
			t.Errorf("%v::%s = %#v (%v)", tc.namespace, tc.member, members, imports.Err())
		}
	}
	shadowed := NewNamespaceImporter(importer, paths.Roots{"fmt": {Unavailable: "optional dependency fmt is inactive"}})
	if got := shadowed.Lookup([]string{namespaceRootPrefix}, resolve.Key{Name: "fmt"}); len(got) != 0 || shadowed.Err() == nil || !strings.Contains(shadowed.Err().Error(), "inactive") {
		t.Fatalf("inactive declaration fell back to standard fmt: %#v, %v", got, shadowed.Err())
	}
}

func TestSelectedModulePackageTreeAndAmbiguity(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module tree.example/lib\n\ngo 1.27\n",
		"core/core.go":      "package core\nconst Json = 1\n",
		"core/Json/json.go": "package json\nconst Value = 2\n",
		"json/json.go":      "package json\nconst Value = 3\n",
		"hidden/go.mod":     "module tree.example/lib/hidden\n\ngo 1.27\n",
		"hidden/hidden.go":  "package hidden\nconst Value = 4\n",
	}
	for name, source := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	importer := NewImporter(filepath.Join(root, "input.gox"), ImporterOptions{Config: testConfig(t, nil)})
	names := paths.NewGoNamespaces(importer)
	tree := paths.Namespace{ModulePath: "tree.example/lib", Source: "module", GoPrefix: "tree.example/lib"}
	group, ok, err := names.Resolve(tree, nil)
	if err != nil || !ok || group.PackagePath != "" {
		t.Fatalf("module without a root package: %#v, %t, %v", group, ok, err)
	}
	json, ok, err := names.Resolve(tree, []string{"json"})
	if err != nil || !ok || json.PackagePath != "tree.example/lib/json" || names.Validate(json) != nil {
		t.Fatalf("module child: %#v, %t, %v", json, ok, err)
	}
	if _, ok, err := names.Resolve(tree, []string{"hidden"}); err != nil || ok {
		t.Fatalf("nested module leaked into selected owner: %t, %v", ok, err)
	}
	if _, _, err := names.Resolve(tree, []string{"core", "Json"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("package/member ambiguity was hidden: %v", err)
	}
	authored := paths.Namespace{ModulePath: "tree.example/lib", PackagePath: "tree.example/lib/core", Source: "module"}
	if _, ok, err := names.Resolve(authored, []string{"Json"}); err != nil || ok {
		t.Fatalf("source crate exposed physical child: %t, %v", ok, err)
	}
	wrong := json
	wrong.ModulePath = "tree.example"
	if err := names.Validate(wrong); err == nil || !strings.Contains(err.Error(), "belongs to module") {
		t.Fatalf("path-prefix ownership was accepted: %v", err)
	}
}
