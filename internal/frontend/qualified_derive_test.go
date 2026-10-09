package frontend

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/typecheck"
)

type deriveProtocolImporter struct{ enumTestImporter }

func (i deriveProtocolImporter) PackageOrigin(path string) (string, bool, error) {
	if path != derives.SerdePackage {
		return "", false, fmt.Errorf("no fixture module origin for %s", path)
	}
	return derives.SerdePackage, false, nil
}

func TestStaticSerdeDeriveRejectsParentModuleOwner(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "serde"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"go.mod":            "module github.com/goxide-lang\ngo 1.27\n",
		"serde/protocol.go": "package serde; type Serializer interface{}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "model.gox")
	for _, claimedModule := range []string{"", derives.SerdePackage} {
		t.Run("claimed="+claimedModule, func(t *testing.T) {
			_, err := CompileWithOptions(file, []byte("package p\nuse serde;\n#[derive(serde::Serialize)]\ntype Item struct{}\n"), FileOptions{
				Importer: typecheck.NewImporter(file),
				Roots:    paths.Roots{"serde": {PackagePath: derives.SerdePackage, ModulePath: claimedModule}},
			})
			want := "must belong to module " + derives.SerdePackage
			if claimedModule != "" {
				want = "static path package source mismatch for " + derives.SerdePackage
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("canonical package spelling acquired derive ownership from its parent module: %v", err)
			}
		})
	}
}

func TestDeriveBindingUsesCanonicalIdentity(t *testing.T) {
	pkg := enumTestPackage(t, derives.SerdePackage, "package wire; type Serializer interface{}; type Deserializer interface{}", nil)
	imp := deriveProtocolImporter{enumTestImporter{derives.SerdePackage: pkg}}
	for _, tc := range []struct{ text, kind string }{
		{"serde::Serialize", "Serialize"},
		{"wire::Deserialize", "Deserialize"},
		{"WireSerialize", "Serialize"},
		{"Clone", "Serialize"},
		{"facade::api::Wire", "Serialize"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			lookup := func(names []string) (paths.Binding, error) {
				if strings.Join(names, "::") != tc.text {
					t.Fatalf("path segments changed: %v", names)
				}
				return paths.Binding{Kind: "derive", OwnerPath: derives.SerdePackage, Name: tc.kind, RoutePath: "example.org/facade", RouteName: "Wire"}, nil
			}
			got, err := resolveDerive(tc.text, lookup, imp)
			if err != nil || got != tc.kind {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestDeriveBindingRejectsNamesWithoutAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		binding    paths.Binding
		want       string
	}{
		{"ordinary type shadows builtin", "Clone", paths.Binding{Kind: "type", OwnerPath: "example.org/local", Name: "Clone"}, "not a registered derive"},
		{"ordinary type", "Serialize", paths.Binding{Kind: "type", OwnerPath: derives.SerdePackage, Name: "Serialize"}, "not a registered derive"},
		{"ordinary function", "wire::Serialize", paths.Binding{Kind: "func", OwnerPath: derives.SerdePackage, Name: "Serialize"}, "not a registered derive"},
		{"namespace", "serde", paths.Binding{Kind: "namespace", OwnerPath: derives.SerdePackage}, "not a registered derive"},
		{"JSON owner", "json::Serialize", paths.Binding{Kind: "derive", OwnerPath: derives.SerdePackage + "/json", Name: "Serialize"}, "does not provide Serialize"},
		{"fake owner", "serde::Serialize", paths.Binding{Kind: "derive", OwnerPath: "example.org/serde", Name: "Serialize"}, "does not provide Serialize"},
		{"fake member", "serde::Unknown", paths.Binding{Kind: "derive", OwnerPath: derives.SerdePackage, Name: "Unknown"}, "does not provide Unknown"},
		{"unowned registry entry", "Wire", paths.Binding{Kind: "derive", Name: "Clone"}, "does not provide Clone"},
		{"legacy dot", "serde.Serialize", paths.Binding{Kind: "derive", OwnerPath: derives.SerdePackage, Name: "Serialize"}, "static paths use '::'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func([]string) (paths.Binding, error) { return tc.binding, nil }
			if _, err := resolveDerive(tc.text, lookup, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
	for _, text := range []string{"Serialize", "Deserialize", "serde::Serialize"} {
		if _, err := resolveDerive(text, nil, nil); err == nil || !strings.Contains(err.Error(), "unbound derive") {
			t.Fatalf("%s: %v", text, err)
		}
	}
	_, err := resolveDerive("wire::Serialize", func([]string) (paths.Binding, error) {
		return paths.Binding{}, fmt.Errorf("file scope has no wire")
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "file scope has no wire") {
		t.Fatal("binding error was hidden", err)
	}
}

func TestDeriveBindingValidatesSelectedOwnerProtocol(t *testing.T) {
	for _, tc := range []struct{ kind, source, want string }{
		{"Serialize", "package serde; type Serializer interface{}", ""},
		{"Deserialize", "package serde; type Deserializer interface{}", ""},
		{"Deserialize", "package serde; type Serializer interface{}", "missing protocol Deserializer"},
		{"Serialize", "package serde; type Serializer int", "must be a value interface"},
	} {
		pkg := enumTestPackage(t, derives.SerdePackage, tc.source, nil)
		lookup := func([]string) (paths.Binding, error) {
			return paths.Binding{Kind: "derive", OwnerPath: derives.SerdePackage, Name: tc.kind}, nil
		}
		_, err := resolveDerive("Bound", lookup, deriveProtocolImporter{enumTestImporter{derives.SerdePackage: pkg}})
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Fatalf("%s: %v; want %q", tc.kind, err, tc.want)
		}
	}
	lookup := func([]string) (paths.Binding, error) {
		return paths.Binding{Kind: "derive", OwnerPath: derives.SerdePackage, Name: "Serialize"}, nil
	}
	if _, err := resolveDerive("Bound", lookup, enumTestImporter{derives.SerdePackage: (*types.Package)(nil)}); err == nil {
		t.Fatal("missing owner accepted")
	}
}

func TestDerivePreludeOnlyWhenUnbound(t *testing.T) {
	for _, name := range []string{"Default", "Clone", "Debug", "Error", "PartialEq", "Eq"} {
		for _, lookup := range []deriveBindingLookup{nil, func([]string) (paths.Binding, error) {
			return paths.Binding{}, &paths.UnboundError{Name: name}
		}} {
			if got, err := resolveDerive(name, lookup, nil); err != nil || got != name {
				t.Fatalf("%s: %q, %v", name, got, err)
			}
		}
	}
	_, err := resolveDerive("Clone", func([]string) (paths.Binding, error) {
		return paths.Binding{}, fmt.Errorf("invalid explicit binding")
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid explicit binding") {
		t.Fatal("prelude swallowed an invalid explicit binding", err)
	}
}

func TestStaticSerdeDeriveSource(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"owner namespace", "use serde;\n#[derive(serde::Serialize, serde::Deserialize)]\ntype Item struct{Value int}"},
		{"namespace alias", "use serde as wire;\n#[derive(wire::Serialize, wire::Deserialize, Debug)]\ntype Item struct{Value int}"},
		{"symbol alias", "use serde::Serialize as Wire;\n#[derive(Wire)]\ntype Item struct{Value int}"},
		{"alias shadows builtin", "use serde::Serialize as Clone;\n#[derive(Clone)]\ntype Item struct{Value int}"},
		{"decode only", "use serde::Deserialize;\n#[derive(Deserialize)]\ntype Item struct{Value int}"},
		{"enum", "use serde;\n#[derive(serde::Serialize, serde::Deserialize)]\nenum Item{Empty, Value(int),}"},
		{"local value shadow", "use serde as wire;\n#[derive(wire::Serialize)]\ntype Item struct{Value int}\nfunc Local()int{wire:=struct{Value int}{Value:7};return wire.Value}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imp := typecheck.NewImporter("model.gox")
			r, err := CompileWithOptions("model.gox", []byte("package p\n"+tc.source+"\n"), FileOptions{Roots: serdeFixtureRoots(), Importer: imp})
			if err != nil {
				t.Fatal(err)
			}
			if err := typecheck.Check("model.go", r.Go, imp); err != nil {
				t.Fatalf("generated Go failed: %v\n%s", err, r.Go)
			}
			if tc.name == "alias shadows builtin" && (!strings.Contains(string(r.Go), "func SerializeItem(") || strings.Contains(string(r.Go), "func CloneItem(")) {
				t.Fatal("explicit binding fell back to builtin Clone")
			}
			if tc.name == "symbol alias" && strings.Contains(string(r.Go), "func DeserializeItem(") || tc.name == "decode only" && strings.Contains(string(r.Go), "func SerializeItem(") {
				t.Fatal("unrequested serde direction generated")
			}
		})
	}
}

func TestStaticSerdeDeriveSourceRejections(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"unbound bare", "#[derive(Serialize)]\ntype Item struct{Value int}", "unbound derive Serialize"},
		{"available root not used", "#[derive(serde::Serialize)]\ntype Item struct{Value int}", "unbound derive serde::Serialize"},
		{"ordinary declaration", "type Serialize struct{}\n#[derive(Serialize)]\ntype Item struct{Value int}", "not a registered derive"},
		{"ordinary declaration shadows builtin", "type Clone struct{}\n#[derive(Clone)]\ntype Item struct{Value int}", "not a registered derive"},
		{"same-line declaration shadows builtin", "#[derive(Clone)] type Clone struct{Value int}", "not a registered derive"},
		{"json owner", "use serde::json as serde;\n#[derive(serde::Serialize)]\ntype Item struct{Value int}", "Serialize"},
		{"duplicate canonical", "use serde as a;\nuse serde as b;\n#[derive(a::Serialize,b::Serialize)]\ntype Item struct{Value int}", "duplicate derive Serialize"},
		{"old dot path", "use serde;\n#[derive(serde.Serialize)]\ntype Item struct{Value int}", "static paths use '::'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions("bad.gox", []byte("package p\n"+tc.source+"\n"), FileOptions{Roots: serdeFixtureRoots()})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "bad.gox:") {
				t.Fatalf("got %v; want located %q", err, tc.want)
			}
		})
	}
}

func TestStaticSerdeDeriveFileScopes(t *testing.T) {
	imp := typecheck.NewImporter("types.gox")
	files := map[string][]byte{
		"types.gox": []byte("package p\nuse serde as wire;\n#[derive(wire::Serialize, wire::Deserialize)]\ntype Item struct{Value int}\n"),
		"other.gox": []byte("package p\nuse serde::json as wire;\nvar _ = wire::MaxDepth\n"),
	}
	options := PackageOptions{Importer: imp, Roots: serdeFixtureRoots()}
	if _, err := CompilePackageWithOptions("example.org/derived", files, options); err != nil {
		t.Fatal(err)
	}
	files["types.gox"] = []byte("package p\n#[derive(wire::Serialize)]\ntype Item struct{Value int}\n")
	if _, err := CompilePackageWithOptions("example.org/derived", files, options); err == nil || !strings.Contains(err.Error(), "unbound derive") {
		t.Fatalf("sibling binding authorized derive: %v", err)
	}
}

func TestStaticSerdeDeriveGeneratedGo(t *testing.T) {
	imp := typecheck.NewImporter("model.gox")
	result, err := CompileWithOptions("model.gox", []byte(`package derived
use serde as wire;
#[derive(wire::Serialize, wire::Deserialize)]
type Item struct{Value int}
`), FileOptions{Roots: serdeFixtureRoots(), Importer: imp})
	if err != nil {
		t.Fatal(err)
	}
	dir := sharedStdModule(t, "example.org/derived")
	sharedStdWrite(t, dir, "model.go", string(result.Go))
	sharedStdWrite(t, dir, "model_test.go", `package derived
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestRoundTrip(t *testing.T){
 value:=Item{Value:42}
 data,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeItem(s,&value)})
 if err!=nil{t.Fatal(err)}
 got,err:=serdejson.Unmarshal(data,DeserializeItem)
 if err!=nil||got!=value{t.Fatalf("%s %#v %v",data,got,err)}
}
`)
	sharedStdGo(t, dir, "test", "-race", "-mod=readonly", "-count=1", ".")
}
