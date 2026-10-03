package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type enumTestImporter map[string]*types.Package

func (i enumTestImporter) Import(path string) (*types.Package, error) { return i[path], nil }
func enumTestPackage(t *testing.T, path, source string, imp types.Importer) *types.Package {
	t.Helper()
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, "input.go", source, 0)
	if e != nil {
		t.Fatal(e)
	}
	pkg, e := (&types.Config{Importer: imp}).Check(path, fs, []*ast.File{f}, nil)
	if e != nil {
		t.Fatal(e)
	}
	return pkg
}
func enumTestMetadata() enumMetadata {
	return enumMetadata{Version: 1, Path: "example.org/producer", Package: "producer", Enums: []enumExport{{Name: "Event", DefinitionPath: "example.org/producer", DefinitionName: "Event", Marker: "sealed", Validator: "ValidateEvent", Variants: []enumVariantExport{{Name: "Empty", Case: "EmptyCase", Constructor: "NewEmpty"}, {Name: "Item", Case: "ItemCase", Constructor: "NewItem", HasPayload: true}}}}}
}

const enumTestSource = `package producer
type Event interface{sealed()}
type private struct{ N int }
type empty struct{}
func(empty)sealed(){}
type item struct{Value private}
func(item)sealed(){}
type EmptyCase=empty
type ItemCase=item
func NewEmpty()Event{return empty{}}
func NewItem(v private)Event{return item{v}}
func ValidateEvent(v Event){}
`

func TestEnumMetadataTypedValidation(t *testing.T) {
	producer := enumTestPackage(t, "example.org/producer", enumTestSource, nil)
	imp := enumTestImporter{"example.org/producer": producer}
	if e := validateEnumMetadata(enumTestMetadata(), producer, imp); e != nil {
		t.Fatal(e)
	}
	relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/producer";type Renamed=p.Event`, imp)
	alias := enumTestMetadata()
	alias.Path = "example.org/relay"
	alias.Package = "relay"
	alias.Enums[0].Name = "Renamed"
	if e := validateEnumMetadata(alias, relay, imp); e != nil {
		t.Fatal("transparent alias/private payload", e)
	}
	for _, tc := range []struct{ name, old, new, want string }{
		{"pointer-case", "type ItemCase=item", "type ItemCase=*item", "case identity"},
		{"payload-type", "func NewItem(v private)Event", "func NewItem(v int)Event", "constructor payload"},
		{"wrong-validator", "func ValidateEvent(v Event)", "func ValidateEvent(v any)", "validator signature"},
		{"extra-case-field", "type item struct{Value private}", "type item struct{Value private; Extra int}", "payload shape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(enumTestSource, tc.old, tc.new, 1)
			src = strings.Replace(src, "return item{v}", "return item{}", 1)
			pkg := enumTestPackage(t, "example.org/producer", src, nil)
			e := validateEnumMetadata(enumTestMetadata(), pkg, enumTestImporter{"example.org/producer": pkg})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, e)
			}
		})
	}
	t.Run("new-defined-interface-is-not-alias", func(t *testing.T) {
		p := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/producer";type Renamed p.Event`, imp)
		if e := validateEnumMetadata(alias, p, imp); e == nil || !strings.Contains(e.Error(), "alias identity") {
			t.Fatal(e)
		}
	})
	t.Run("duplicate-variant", func(t *testing.T) {
		m := enumTestMetadata()
		m.Enums[0].Variants[1].Name = "Empty"
		if e := validateEnumMetadata(m, producer, imp); e == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("same-name-other-package", func(t *testing.T) {
		p := enumTestPackage(t, "example.org/other", enumTestSource, nil)
		if e := validateEnumMetadata(enumTestMetadata(), p, imp); e == nil {
			t.Fatal("wrong package accepted")
		}
	})
}

type enumArtifactTestImporter struct {
	enumTestImporter
	dir, content string
}

func (i *enumArtifactTestImporter) PackageFiles(string) (string, []string, error) {
	return i.dir, []string{"producer.go"}, nil
}
func (i *enumArtifactTestImporter) PackageFingerprint(string) (string, error) { return i.content, nil }
func TestEnumArtifactBindings(t *testing.T) {
	m := enumTestMetadata()
	source := []byte(enumTestSource + fmt.Sprintf("\nconst %s=%q\n", enumMetadataMarker, m.Marker()))
	dir := t.TempDir()
	file := filepath.Join(dir, "producer.go")
	if e := os.WriteFile(file, source, 0600); e != nil {
		t.Fatal(e)
	}
	imp := &enumArtifactTestImporter{enumTestImporter: enumTestImporter{m.Path: enumTestPackage(t, m.Path, string(source), nil)}, dir: dir, content: "selected-build-and-dependency-closure"}
	data, e := marshalEnumArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, enumMetadataFilename)
	write := func(data []byte) {
		t.Helper()
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(data)
	got, e := loadEnumArtifact(m.Path, imp)
	if e != nil || got.Marker() != m.Marker() {
		t.Fatal(got, e)
	}
	repeated, e := marshalEnumArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if e != nil || !bytes.Equal(data, repeated) {
		t.Fatal("nondeterministic artifact", e)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(*enumArtifact)
	}{
		{"version", "unsupported metadata version", func(a *enumArtifact) { a.ArtifactVersion = 2 }},
		{"path", "package path mismatch", func(a *enumArtifact) { a.Metadata.Path = "wrong.example/pkg" }},
		{"marker", "marker digest mismatch", func(a *enumArtifact) { a.Metadata.Enums[0].Variants[0].Name = "Changed" }},
		{"files", "source set mismatch", func(a *enumArtifact) { a.Files = map[string]string{} }},
		{"configuration", "build configuration changed", func(a *enumArtifact) { a.Content = "other" }},
		{"dependencies", "dependency set mismatch", func(a *enumArtifact) { a.Dependencies = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a enumArtifact
			if e := json.Unmarshal(data, &a); e != nil {
				t.Fatal(e)
			}
			tc.mutate(&a)
			changed, e := json.Marshal(a)
			if e != nil {
				t.Fatal(e)
			}
			write(changed)
			_, e = loadEnumArtifact(m.Path, imp)
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, e)
			}
		})
	}
	t.Run("duplicate-json-key", func(t *testing.T) {
		write(bytes.Replace(data, []byte(`"ArtifactVersion": 1`), []byte(`"ArtifactVersion": 1, "ArtifactVersion": 1`), 1))
		if _, e := loadEnumArtifact(m.Path, imp); e == nil || !strings.Contains(e.Error(), "duplicate object key") {
			t.Fatal(e)
		}
	})
	t.Run("unknown-json-field", func(t *testing.T) {
		write(append([]byte(`{"Unknown":true,`), data[1:]...))
		if _, e := loadEnumArtifact(m.Path, imp); e == nil || !strings.Contains(e.Error(), "unknown field") {
			t.Fatal(e)
		}
	})
	t.Run("source-drift", func(t *testing.T) {
		write(data)
		if e := os.WriteFile(file, append(source, []byte("\n// changed\n")...), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := loadEnumArtifact(m.Path, imp); e == nil || !strings.Contains(e.Error(), "source content mismatch") {
			t.Fatal(e)
		}
	})
}
