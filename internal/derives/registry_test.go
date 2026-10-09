package derives

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

type testImporter map[string]*types.Package

func (i testImporter) Import(path string) (*types.Package, error) {
	p, ok := i[path]
	if !ok {
		return nil, fmt.Errorf("package %s unavailable", path)
	}
	return p, nil
}

func (i testImporter) PackageOrigin(path string) (string, bool, error) {
	if path != SerdePackage {
		return "", false, fmt.Errorf("no fixture origin for %s", path)
	}
	return SerdePackage, false, nil
}

type originImporter struct {
	types.Importer
	module   string
	standard bool
	err      error
}

func (i originImporter) PackageOrigin(string) (string, bool, error) {
	return i.module, i.standard, i.err
}

type importerWithoutOrigin struct{ types.Importer }

func protocolPackage(t *testing.T, path, source string, imp types.Importer) *types.Package {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "protocol.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{Importer: imp}).Check(path, fs, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestRegistryRequiresCanonicalOwner(t *testing.T) {
	for _, owner := range []string{"", "serde", SerdePackage + "/json", "example.org/serde"} {
		for _, name := range []string{"Serialize", "Deserialize"} {
			if _, ok := Lookup(owner, name); ok {
				t.Errorf("%q acquired %s", owner, name)
			}
		}
	}
	for _, name := range []string{"Serialize", "Deserialize"} {
		if _, ok := Lookup(SerdePackage, name); !ok {
			t.Error(name)
		}
	}
	if _, ok := Lookup(SerdePackage, "Clone"); ok {
		t.Fatal("serde acquired a compiler builtin")
	}
}

func TestOwnerProtocolDirectionsAreIndependent(t *testing.T) {
	for _, name := range []string{"Serialize", "Deserialize"} {
		t.Run(name, func(t *testing.T) {
			def, _ := Lookup(SerdePackage, name)
			pkg := protocolPackage(t, SerdePackage, "package wire; type "+def.Protocol+" interface{}", nil)
			if err := Validate(def, testImporter{SerdePackage: pkg}); err != nil {
				t.Fatal(err)
			}
			other := "Serialize"
			if name == other {
				other = "Deserialize"
			}
			missing, _ := Lookup(SerdePackage, other)
			if err := Validate(missing, testImporter{SerdePackage: pkg}); err == nil || !strings.Contains(err.Error(), "missing protocol") {
				t.Fatalf("unselected direction was assumed: %v", err)
			}
		})
	}
}

func TestOwnerProtocolRequiresResolverOrigin(t *testing.T) {
	pkg := protocolPackage(t, SerdePackage, "package serde; type Serializer interface{}", nil)
	base := testImporter{SerdePackage: pkg}
	def, _ := Lookup(SerdePackage, "Serialize")
	for _, tc := range []struct {
		name string
		imp  types.Importer
		want string
	}{
		{"unproved", importerWithoutOrigin{base}, "requires package module origin proof"},
		{"parent module", originImporter{Importer: base, module: "github.com/goxide-lang"}, "must belong to module"},
		{"subpackage module", originImporter{Importer: base, module: SerdePackage + "/json"}, "must belong to module"},
		{"missing module", originImporter{Importer: base}, "must belong to module"},
		{"standard spoof", originImporter{Importer: base, module: SerdePackage, standard: true}, "must belong to module"},
		{"lookup failure", originImporter{Importer: base, err: fmt.Errorf("resolver unavailable")}, "resolver unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(def, tc.imp); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("owner origin was not verified: %v; want %s", err, tc.want)
			}
		})
	}
}

func TestOwnerProtocolRejectsImpostors(t *testing.T) {
	foreign := protocolPackage(t, "example.org/fake", "package serde; type Serializer interface{}", nil)
	for _, tc := range []struct{ name, source, want string }{
		{"missing", "package serde", "missing protocol Serializer"},
		{"function", "package serde; func Serializer(){}", "missing protocol Serializer"},
		{"not interface", "package serde; type Serializer int", "must be a value interface"},
		{"constraint", "package serde; type Serializer interface{~int}", "must be a value interface"},
		{"generic", "package serde; type Serializer[T any] interface{}", "must be non-generic"},
		{"foreign alias", `package serde; import fake "example.org/fake"; type Serializer = fake.Serializer`, "must be defined by"},
		{"other named alias", "package serde; type Other interface{}; type Serializer = Other", "must be defined by"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := protocolPackage(t, SerdePackage, tc.source, testImporter{"example.org/fake": foreign})
			def, _ := Lookup(SerdePackage, "Serialize")
			if err := Validate(def, testImporter{SerdePackage: pkg}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
	def, _ := Lookup(SerdePackage, "Serialize")
	for _, imp := range []types.Importer{nil, testImporter{}, testImporter{SerdePackage: nil}, testImporter{SerdePackage: foreign}} {
		if err := Validate(def, imp); err == nil {
			t.Fatal("missing or wrong owner accepted")
		}
	}
	def.Protocol = "Other"
	if err := Validate(def, testImporter{SerdePackage: foreign}); err == nil || !strings.Contains(err.Error(), "unregistered derive") {
		t.Fatal("constructed registration acquired protocol", err)
	}
}
