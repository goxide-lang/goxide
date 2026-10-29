package analysis

import (
	"context"
	"errors"
	"fmt"
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
)

type lookupImporter struct {
	pkg *types.Package
	err error
}

func (i lookupImporter) Import(name string) (*types.Package, error) {
	if name != i.pkg.Path() {
		return nil, fmt.Errorf("not a package: %s", name)
	}
	return i.pkg, i.err
}

func (i lookupImporter) PackageIndex(source, module string) ([]string, error) {
	if source == "module" && module == i.pkg.Path() {
		return []string{module}, nil
	}
	return nil, nil
}

func (i lookupImporter) PackageOrigin(string) (string, bool, error) {
	return i.pkg.Path(), false, nil
}

func TestAnalysisPreservesSelectedGoPackageError(t *testing.T) {
	want := errors.New("corrupt dependency export data")
	goImports := lookupImporter{pkg: types.NewPackage("example.test/dependency", "dependency"), err: want}
	for filename, source := range map[string]string{
		"main.gox": "package fixture\nuse {}; use dependency::Value;\nfunc F() int { return Value }\n",
		"main.go":  "package fixture\nimport dep \"example.test/dependency\"\nvar Value dep.Type\n",
	} {
		t.Run(filename, func(t *testing.T) {
			result, err := Analyze(context.Background(), Input{
				PackagePath: "fixture",
				Sources:     map[string][]byte{filename: []byte(source)},
				GoImporter:  goImports,
				Roots:       paths.Roots{"dependency": {Source: "module", ModulePath: goImports.pkg.Path(), GoPrefix: goImports.pkg.Path()}},
			})
			if !errors.Is(err, want) || result == nil || !errors.Is(result.Err, want) {
				t.Fatalf("analysis replaced the selected import failure: result=%v, error=%v", result, err)
			}
		})
	}
}

func TestAnalysisCanonicalizesNativeNamespaceImports(t *testing.T) {
	pkg := types.NewPackage("dependency", "dependency")
	pkg.Scope().Insert(types.NewTypeName(0, pkg, "Type", types.Typ[types.Int]))
	for name, source := range map[string]string{
		"alias": "package library\nimport dep \"dependency\"\nvar Value dep.Type\n",
		"blank": "package library\nimport _ \"dependency\"\n",
	} {
		for _, importErr := range []error{nil, errors.New("corrupt routed dependency export data")} {
			t.Run(name+"/"+fmt.Sprint(importErr), func(t *testing.T) {
				result, err := Analyze(context.Background(), Input{
					PackagePath: "fixture",
					Sources:     map[string][]byte{"main.gox": []byte("package fixture\n")},
					GoImporter:  lookupImporter{pkg: pkg, err: importErr},
					Options: Options{SourcePackages: []SourcePackage{{
						PackagePath: "library", ModulePath: "library", Target: config.HostTarget(),
						Sources: map[string][]byte{
							"lib.gox":   []byte("package library\n"),
							"native.go": []byte(source),
						},
						Roots: paths.Roots{"dependency": {Source: "module", ModulePath: pkg.Path(), GoPrefix: pkg.Path()}},
					}}},
				})
				if !errors.Is(err, importErr) || result == nil || !errors.Is(result.Err, importErr) {
					t.Fatalf("native namespace import lost its canonical package: result=%v, error=%v", result, err)
				}
				if err == nil {
					uses := result.Resolved.Files["library/native.go"].Uses
					if len(uses) != 1 || strings.Join(uses[0].Target, "/") != pkg.Path() {
						t.Fatalf("native import target was not canonicalized: %v", uses)
					}
				}
			})
		}
	}
}

func TestPackageProbeDoesNotHideSelectedImportFailure(t *testing.T) {
	want := errors.New("broken export data")
	goImports := lookupImporter{pkg: types.NewPackage("example.test/dependency", "dependency"), err: want}
	imports := &moduleImporter{
		external: goImports,
		cache:    map[string]goImportResult{},
		namespaces: map[string]rootRequirement{"dependency": {
			namespace: paths.Namespace{Source: "module", ModulePath: goImports.pkg.Path(), GoPrefix: goImports.pkg.Path()},
		}},
	}
	implicit := imports.namespaces["dependency"]
	implicit.implicit = true
	implicit.requireRoot = func(string, paths.Namespace) error {
		t.Fatal("failed implicit member lookup activated its dependency root")
		return nil
	}
	imports.namespaces["prelude"] = implicit
	if got := imports.Lookup([]string{"prelude"}, resolve.Key{Name: "Value"}); len(got) != 0 || imports.err != nil {
		t.Fatalf("implicit member probe became an import failure: %v, %v", got, imports.err)
	}
	if got := imports.Lookup([]string{"example.test", "dependency"}, resolve.Key{Name: "Value"}); len(got) != 0 || imports.err != nil {
		t.Fatalf("candidate path became an import failure: %v, %v", got, imports.err)
	}
	if got := imports.Lookup([]string{"dependency"}, resolve.Key{Name: "Value"}); len(got) != 0 || !errors.Is(imports.err, want) {
		t.Fatalf("cached probe lost the selected import failure: %v, %v", got, imports.err)
	}
}

func TestPackageProbeAcceptsPackageWithoutParent(t *testing.T) {
	imports := &moduleImporter{
		external: lookupImporter{pkg: types.NewPackage("archive/tar", "tar")},
		cache:    map[string]goImportResult{},
	}
	use := &resolve.UseBinding{Target: []string{"archive", "tar"}, Name: resolve.Key{Name: "tar"}}
	if !imports.finishUseTarget(use) || imports.err != nil {
		t.Fatalf("parent package probe rejected archive/tar: %v", imports.err)
	}
}
