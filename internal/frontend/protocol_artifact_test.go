package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/protocols"
)

func protocolTestMetadata() protocols.Metadata {
	owner := "protocol.example/producer"
	id := protocols.ID{Owner: owner, Name: "Produce"}
	ops, marker := protocols.DefinitionNames(id, true)
	member := protocols.Member{Name: "Abc", GoName: "Abc", Params: "()", Results: "Self"}
	d := protocols.Definition{ID: id, Public: true, OpsName: ops, MarkerName: marker, Members: []protocols.Member{member}}
	target := protocols.ID{Owner: owner, Name: "User"}
	return protocols.Metadata{Version: protocols.MetadataVersion, Path: owner, Package: "producer", Definitions: []protocols.Definition{d}, Implementations: []protocols.Implementation{{Protocol: id, Target: target, CarrierName: protocols.CarrierName(target), Members: []protocols.Member{member}}}}
}

func protocolTestSource(meta protocols.Metadata) string {
	d, impl := meta.Definitions[0], meta.Implementations[0]
	return fmt.Sprintf(`package producer
type %[1]s[Self any] interface{Abc()Self;%[2]s()}
type User struct{N int}
type %[3]s struct{}
func (User) %[4]s(User)%[3]s{return %[3]s{}}
func (%[3]s) Abc()User{return User{N:7}}
func (%[3]s) %[2]s(){}
`, d.OpsName, d.MarkerName, impl.CarrierName, protocols.AnchorName)
}

func TestProtocolMetadataTypedValidation(t *testing.T) {
	m := protocolTestMetadata()
	source := protocolTestSource(m)
	pkg := enumTestPackage(t, m.Path, source, nil)
	imp := enumTestImporter{m.Path: pkg}
	if err := validateProtocolMetadata(m, pkg, imp); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"wrong-operation", "Abc()User{return User{N:7}}", "Abc()int{return 7}", "operation signature mismatch"},
		{"wrong-anchor-self", protocols.AnchorName + "(User)", protocols.AnchorName + "(any)", "exact-Self anchor"},
		{"stateful-carrier", "type " + m.Implementations[0].CarrierName + " struct{}", "type " + m.Implementations[0].CarrierName + " struct{State int}", "zero fields"},
		{"missing-marker", "func (" + m.Implementations[0].CarrierName + ") " + m.Definitions[0].MarkerName + "(){}", "", "canonical operation signatures"},
		{"wrong-marker-signature", m.Definitions[0].MarkerName + "()", m.Definitions[0].MarkerName + "(int)", "marker signature"},
		{"target-alias", "type User struct{N int}", "type Real struct{N int};type User=Real", "canonical concrete target"},
		{"carrier-alias", "type " + m.Implementations[0].CarrierName + " struct{}", "type Other struct{};type " + m.Implementations[0].CarrierName + "=Other", "carrier identity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.ReplaceAll(source, tc.old, tc.replacement)
			p := enumTestPackage(t, m.Path, changed, nil)
			err := validateProtocolMetadata(m, p, enumTestImporter{m.Path: p})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q; got %v", tc.want, err)
			}
		})
	}
	t.Run("same-shape-other-protocol-is-not-owner", func(t *testing.T) {
		other := protocolTestMetadata()
		other.Definitions[0].Name = "Other"
		if err := validateProtocolMetadata(other, pkg, imp); err == nil || !strings.Contains(err.Error(), "canonical owner") {
			t.Fatal(err)
		}
	})
	t.Run("duplicate-implementation", func(t *testing.T) {
		copy := protocolTestMetadata()
		copy.Implementations = append(copy.Implementations, copy.Implementations[0])
		if err := validateProtocolMetadata(copy, pkg, imp); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatal(err)
		}
	})
	t.Run("promoted-anchor-is-not-a-local-implementation", func(t *testing.T) {
		text := strings.Replace(source, "type User struct{N int}", "type Parent struct{};type User struct{Parent}", 1)
		text = strings.Replace(text, "func (User) "+protocols.AnchorName, "func (Parent) "+protocols.AnchorName, 1)
		text = strings.Replace(text, "return User{N:7}", "return User{}", 1)
		p := enumTestPackage(t, m.Path, text, nil)
		if err := validateProtocolMetadata(m, p, enumTestImporter{m.Path: p}); err == nil || !strings.Contains(err.Error(), "exact-Self anchor") {
			t.Fatal(err)
		}
	})
	t.Run("private-impl-is-still-checked", func(t *testing.T) {
		copy := protocolTestMetadata()
		copy.Definitions[0].Public = false
		copy.Definitions[0].OpsName, copy.Definitions[0].MarkerName = protocols.DefinitionNames(copy.Definitions[0].ID, false)
		text := protocolTestSource(copy)
		p := enumTestPackage(t, copy.Path, text, nil)
		if err := validateProtocolDeclarations(copy, p, enumTestImporter{copy.Path: p}); err != nil {
			t.Fatal(err)
		}
		text = strings.Replace(text, "Abc()User{return User{N:7}}", "Abc()int{return 7}", 1)
		p = enumTestPackage(t, copy.Path, text, nil)
		if err := validateProtocolDeclarations(copy, p, enumTestImporter{copy.Path: p}); err == nil {
			t.Fatal("private implementation escaped typed validation")
		}
	})
}

func TestProtocolReexportCanonicalIdentity(t *testing.T) {
	m := protocolTestMetadata()
	producer := enumTestPackage(t, m.Path, protocolTestSource(m), nil)
	imp := enumTestImporter{m.Path: producer}
	relay := protocols.Metadata{Version: protocols.MetadataVersion, Path: "protocol.example/relay", Package: "relay", Definitions: m.Definitions}
	d := m.Definitions[0]
	source := fmt.Sprintf("package relay; import p %q; type %s[Self any] = p.%s[Self]", m.Path, d.OpsName, d.OpsName)
	pkg := enumTestPackage(t, relay.Path, source, imp)
	if err := validateProtocolMetadata(relay, pkg, imp); err != nil {
		t.Fatal(err)
	}
	if err := validateProtocolCanonicalOwners(relay, map[string]protocols.Metadata{m.Path: m}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		fmt.Sprintf("package relay; type %s[Self any] interface{Abc()Self;%s()}", d.OpsName, d.MarkerName),
		fmt.Sprintf("package relay; import p %q; type %s[Self any] p.%s[Self]", m.Path, d.OpsName, d.OpsName),
	} {
		pkg := enumTestPackage(t, relay.Path, source, imp)
		if err := validateProtocolMetadata(relay, pkg, imp); err == nil || !strings.Contains(err.Error(), "alias identity") {
			t.Fatalf("structural copy is not a canonical protocol re-export: %v", err)
		}
	}
	if err := validateProtocolCanonicalOwners(relay, nil); err == nil {
		t.Fatal("unproven canonical producer accepted")
	}
}

type protocolArtifactTestImporter struct {
	enumTestImporter
	dir, fingerprint string
}

func (i protocolArtifactTestImporter) PackageFiles(string) (string, []string, error) {
	return i.dir, []string{"producer.go"}, nil
}
func (i protocolArtifactTestImporter) PackageFingerprint(string) (string, error) {
	return i.fingerprint, nil
}

func TestProtocolArtifactSelectedSourceBindings(t *testing.T) {
	m := protocolTestMetadata()
	source := []byte(protocolTestSource(m) + fmt.Sprintf("const %s=%q\n", protocolMetadataMarker, m.Marker()))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "producer.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	imp := protocolArtifactTestImporter{enumTestImporter{m.Path: enumTestPackage(t, m.Path, string(source), nil)}, dir, "selected-source-build-view"}
	data, err := marshalProtocolArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if err != nil {
		t.Fatal(err)
	}
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, protocolMetadataFilename), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(data)
	got, err := loadProtocolArtifact(m.Path, imp)
	if err != nil || got.Marker() != m.Marker() {
		t.Fatal(got, err)
	}
	repeat, err := marshalProtocolArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if err != nil || !bytes.Equal(data, repeat) {
		t.Fatal("nondeterministic protocol artifact", err)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(*protocolArtifact)
	}{
		{"version", "unsupported metadata version", func(a *protocolArtifact) { a.ArtifactVersion++ }},
		{"schema", "unsupported metadata version", func(a *protocolArtifact) { a.Metadata.Version++ }},
		{"owner", "package identity mismatch", func(a *protocolArtifact) { a.Metadata.Path = "wrong.example/model" }},
		{"marker", "marker digest mismatch", func(a *protocolArtifact) { a.Metadata.Definitions[0].Name = "Changed" }},
		{"files", "source set mismatch", func(a *protocolArtifact) { a.Files = nil }},
		{"build-view", "build configuration changed", func(a *protocolArtifact) { a.Content = "other" }},
		{"dependencies", "dependency source closure", func(a *protocolArtifact) { a.Dependencies = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a protocolArtifact
			if err := json.Unmarshal(data, &a); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&a)
			changed, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			write(changed)
			if _, err := loadProtocolArtifact(m.Path, imp); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q; got %v", tc.want, err)
			}
		})
	}
	for name, content := range map[string][]byte{
		"duplicate-json-key": bytes.Replace(data, []byte(`"ArtifactVersion": 1`), []byte(`"ArtifactVersion": 1,"ArtifactVersion": 1`), 1),
		"unknown-json-key":   append([]byte(`{"Unknown":true,`), data[1:]...),
		"trailing-json":      append(append([]byte(nil), data...), []byte("{}")...),
	} {
		t.Run(name, func(t *testing.T) {
			write(content)
			if _, err := loadProtocolArtifact(m.Path, imp); err == nil {
				t.Fatal("invalid artifact JSON accepted")
			}
		})
	}
	t.Run("missing-artifact", func(t *testing.T) {
		if err := os.Remove(filepath.Join(dir, protocolMetadataFilename)); err != nil {
			t.Fatal(err)
		}
		if _, err := loadProtocolArtifact(m.Path, imp); err == nil || !strings.Contains(err.Error(), "cannot read") {
			t.Fatal(err)
		}
	})
	t.Run("source-change", func(t *testing.T) {
		write(data)
		if err := os.WriteFile(filepath.Join(dir, "producer.go"), append(source, []byte("\n// changed\n")...), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadProtocolArtifact(m.Path, imp); err == nil || !strings.Contains(err.Error(), "source content mismatch") {
			t.Fatal(err)
		}
	})
	t.Run("plain-go-has-no-protocol-evidence", func(t *testing.T) {
		pkg := enumTestPackage(t, m.Path, protocolTestSource(m), nil)
		got, err := loadProtocolArtifact(m.Path, enumTestImporter{m.Path: pkg})
		if err != nil || got.Present() {
			t.Fatal(got, err)
		}
	})
	t.Run("marked-package-requires-source-provider", func(t *testing.T) {
		if _, err := loadProtocolArtifact(m.Path, imp.enumTestImporter); err == nil || !strings.Contains(err.Error(), "source importer unavailable") {
			t.Fatal(err)
		}
	})
}

type protocolTypeInfoImporter struct {
	enumTestImporter
	module   string
	standard bool
}

func (i protocolTypeInfoImporter) PackageOrigin(string) (string, bool, error) {
	return i.module, i.standard, nil
}

const protocolTypeInfoSource = `package typeinfo
const GoxideTypeInfoABI=1
type CarrierFor[T any,S ~struct{}] interface{GoxideInternalTypeCarrierForSelf(T)S}
func Of[T CarrierFor[T,S],S ~struct{}]()S{var s S;return s}
`

func TestProtocolTypeInfoSelectedABI(t *testing.T) {
	makeImporter := func(source string) protocolTypeInfoImporter {
		return protocolTypeInfoImporter{enumTestImporter{protocols.TypeInfoPackage: enumTestPackage(t, protocols.TypeInfoPackage, source, nil)}, "github.com/goxide-lang/std", false}
	}
	if err := validateTypeInfoSupport(makeImporter(protocolTypeInfoSource)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"version", "GoxideTypeInfoABI=1", "GoxideTypeInfoABI=2"},
		{"anchor-self", "GoxideInternalTypeCarrierForSelf(T)S", "GoxideInternalTypeCarrierForSelf(any)S"},
		{"carrier-shape", "S ~struct{}", "S any"},
		{"of-result", "]()S{var s S;return s}", "]()any{return nil}"},
		{"of-input", "]()S{", "](v int)S{"},
		{"of-unbound-self", "func Of[T CarrierFor[T,S]", "func Of[T any"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imp := makeImporter(strings.ReplaceAll(protocolTypeInfoSource, tc.old, tc.replacement))
			if err := validateTypeInfoSupport(imp); err == nil {
				t.Fatal("wrong selected typeinfo ABI accepted")
			}
		})
	}
	for _, tc := range []struct {
		name, module string
		standard     bool
	}{{"other-module", "example.org/typeinfo", false}, {"toolchain", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			imp := makeImporter(protocolTypeInfoSource)
			imp.module, imp.standard = tc.module, tc.standard
			if err := validateTypeInfoSupport(imp); err == nil {
				t.Fatal("wrong selected module accepted")
			}
		})
	}
}

func TestProtocolGenericEvidenceMetadata(t *testing.T) {
	m := protocolTestMetadata()
	d := m.Definitions[0]
	m.Functions = []protocols.GenericFunction{{
		Name: "Make", SourceParams: []string{"T"}, HiddenParams: []string{"S"},
		Bounds: []protocols.Bound{{Function: "Make", TypeParam: "T", CarrierParam: "S", Protocol: d}},
	}}
	typeinfo := enumTestPackage(t, protocols.TypeInfoPackage, protocolTypeInfoSource, nil)
	imp := protocolTypeInfoImporter{enumTestImporter{protocols.TypeInfoPackage: typeinfo}, "github.com/goxide-lang/std", false}
	source := strings.Replace(protocolTestSource(m), "package producer", "package producer\nimport ti \""+protocols.TypeInfoPackage+"\"", 1)
	source += fmt.Sprintf("\nfunc Make[T ti.CarrierFor[T,S],S interface{~struct{};%s[T]}]()T{panic(\"unused\")}\n", d.OpsName)
	pkg := enumTestPackage(t, m.Path, source, imp)
	imp.enumTestImporter[m.Path] = pkg
	if err := validateProtocolMetadata(m, pkg, imp); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*protocols.Metadata)
	}{
		{"source-arity-cannot-expose-evidence", func(meta *protocols.Metadata) {
			meta.Functions[0].SourceParams = []string{"T", "S"}
			meta.Functions[0].HiddenParams = nil
		}},
		{"hidden-order", func(meta *protocols.Metadata) {
			meta.Functions[0].SourceParams[0] = "S"
			meta.Functions[0].HiddenParams[0] = "T"
		}},
		{"wrong-source-binder", func(meta *protocols.Metadata) { meta.Functions[0].Bounds[0].TypeParam = "S" }},
		{"wrong-evidence-binder", func(meta *protocols.Metadata) { meta.Functions[0].Bounds[0].CarrierParam = "T" }},
		{"bound-function-owner", func(meta *protocols.Metadata) { meta.Functions[0].Bounds[0].Function = "Other" }},
		{"forged-protocol", func(meta *protocols.Metadata) { meta.Functions[0].Bounds[0].Protocol.Name = "Other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(m)
			var meta protocols.Metadata
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			tc.edit(&meta)
			if err := validateProtocolMetadata(meta, pkg, imp); err == nil {
				t.Fatal("invalid generic evidence record accepted")
			}
		})
	}
	for _, tc := range []struct{ name, old, replacement string }{
		{"wrong-Self", "T ti.CarrierFor[T,S]", "T ti.CarrierFor[User,S]"},
		{"wrong-ops-argument", d.OpsName + "[T]", d.OpsName + "[User]"},
		{"missing-operations", "~struct{};" + d.OpsName + "[T]", "~struct{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.Replace(source, tc.old, tc.replacement, 1)
			bad := enumTestPackage(t, m.Path, changed, imp)
			if err := validateProtocolMetadata(m, bad, imp); err == nil {
				t.Fatal("changed actual generic constraint accepted")
			}
		})
	}
	t.Run("private-evidence-does-not-export-source-name", func(t *testing.T) {
		data, _ := json.Marshal(m)
		var meta protocols.Metadata
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Fatal(err)
		}
		meta.Definitions[0].Public = false
		meta.Definitions[0].ExportedEvidence = true
		meta.Functions[0].Bounds[0].Protocol = meta.Definitions[0]
		if err := validateProtocolMetadata(meta, pkg, imp); err != nil {
			t.Fatal(err)
		}
		if err := validateProtocolCanonicalOwners(meta, nil); err != nil {
			t.Fatal(err)
		}
		if _, ok := lookupProtocolDefinition(meta, d.Name); ok {
			t.Fatal("transported private evidence became a source import")
		}
		if _, ok := lookupProtocolEvidence(meta, d.ID); !ok {
			t.Fatal("private generic evidence lost during publication")
		}
	})
}
