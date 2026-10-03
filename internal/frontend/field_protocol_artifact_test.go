package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fieldTestMetadata(kind string) enumMetadata {
	return enumMetadata{Version: enumMetadataVersion, Path: "example.org/fields", Package: "fields", FieldProtocols: []fieldProtocolExport{{Name: "Value", DefinitionPath: "example.org/fields", DefinitionName: "Value", Helper: kind + "Value", Kind: kind, Source: "derive"}}}
}

const fieldTestSource = `package fields;type Value struct{N int};func CloneValue(v *Value)Value{return *v};func DebugValue(v *Value)string{return "shown"}`

func TestFieldProtocolMetadataValidation(t *testing.T) {
	p := enumTestPackage(t, "example.org/fields", fieldTestSource, nil)
	imp := enumTestImporter{p.Path(): p}
	for _, kind := range []string{"Clone", "Debug"} {
		t.Run(kind, func(t *testing.T) {
			m := fieldTestMetadata(kind)
			if !m.present() {
				t.Fatal("field-only metadata absent")
			}
			if err := validateEnumMetadata(m, p, imp); err != nil {
				t.Fatal(err)
			}
			relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/fields";type Alias=p.Value`, imp)
			m.Path = relay.Path()
			m.Package = relay.Name()
			m.FieldProtocols[0].Name = "Alias"
			if err := validateEnumMetadata(m, relay, imp); err != nil {
				t.Fatal(err)
			}
			defined := enumTestPackage(t, relay.Path(), `package relay;import p "example.org/fields";type Alias p.Value`, imp)
			if err := validateEnumMetadata(m, defined, imp); err == nil || !strings.Contains(err.Error(), "alias identity") {
				t.Fatal(err)
			}
			result, ret := "Value", "Value{}"
			if kind == "Debug" {
				result, ret = "string", `"x"`
			}
			for _, helper := range []string{
				fmt.Sprintf("func %sValue(v Value)%s{return %s}", kind, result, ret),
				fmt.Sprintf("func %sValue(v any)%s{return %s}", kind, result, ret),
				fmt.Sprintf("func %sValue(v ...*Value)%s{return %s}", kind, result, ret),
				fmt.Sprintf("func %sValue[T any](v *Value)%s{return %s}", kind, result, ret),
				fmt.Sprintf("var %sValue=func(v *Value)%s{return %s}", kind, result, ret),
				fmt.Sprintf("type Wrong %s;func %sValue(v *Value)Wrong{return Wrong(%s)}", result, kind, ret),
				fmt.Sprintf("func %sValue(v *Value)(%s,error){return %s,nil}", kind, result, ret),
			} {
				bad := enumTestPackage(t, p.Path(), "package fields;type Value struct{N int};"+helper, nil)
				if err := validateEnumMetadata(fieldTestMetadata(kind), bad, enumTestImporter{bad.Path(): bad}); err == nil {
					t.Fatal("bad signature", helper)
				}
			}
			for _, source := range []string{"", "manual", "native"} {
				bad := fieldTestMetadata(kind)
				bad.FieldProtocols[0].Source = source
				if err := validateEnumMetadata(bad, p, imp); err == nil {
					t.Fatal(source)
				}
			}
			bad := fieldTestMetadata(kind)
			bad.FieldProtocols = append(bad.FieldProtocols, bad.FieldProtocols[0])
			if err := validateEnumMetadata(bad, p, imp); err == nil {
				t.Fatal("duplicate accepted")
			}
		})
	}
	m, err := collectEnumExports(p.Path(), PackageResult{}, p)
	if err != nil || m.present() {
		t.Fatal("helper spelling inferred registration", m, err)
	}
	result := PackageResult{Files: map[string]Result{"a.hgo": {fieldProtocols: map[string][]string{"Value": {"Clone", "Debug"}}}}}
	m, err = collectEnumExports(p.Path(), result, p)
	if err != nil || len(m.FieldProtocols) != 2 {
		t.Fatal(m, err)
	}
}
func TestFieldProtocolArtifactOriginAuthorization(t *testing.T) {
	imp := equalityArtifactImporter{enumTestImporter: enumTestImporter{}, dirs: map[string]string{}}
	write := func(m enumMetadata, body string) {
		t.Helper()
		src := []byte(body + fmt.Sprintf("\nconst %s=%q\n", enumMetadataMarker, m.Marker()))
		imp.enumTestImporter[m.Path] = enumTestPackage(t, m.Path, string(src), imp)
		dir := t.TempDir()
		imp.dirs[m.Path] = dir
		if err := os.WriteFile(filepath.Join(dir, "source.go"), src, 0600); err != nil {
			t.Fatal(err)
		}
		data, err := marshalEnumArtifact(m, map[string][]byte{"source.go": src}, imp)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, enumMetadataFilename), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	origin := fieldTestMetadata("Clone")
	write(origin, fieldTestSource)
	if _, err := loadEnumArtifact(origin.Path, imp); err != nil {
		t.Fatal(err)
	}
	relay := origin
	relay.FieldProtocols = append([]fieldProtocolExport(nil), origin.FieldProtocols...)
	relay.Path = "example.org/relay"
	relay.Package = "relay"
	relay.FieldProtocols[0].Name = "Alias"
	src := `package relay;import p "example.org/fields";type Alias=p.Value`
	write(relay, src)
	if _, err := loadEnumArtifact(relay.Path, imp); err != nil {
		t.Fatal(err)
	}
	// Correct Go signature alone cannot create an unregistered protocol at a relay.
	relay.FieldProtocols[0].Kind = "Debug"
	relay.FieldProtocols[0].Helper = "DebugValue"
	write(relay, src)
	if _, err := loadEnumArtifact(relay.Path, imp); err == nil || !strings.Contains(err.Error(), "field protocol alias does not match") {
		t.Fatal("relay invented Debug", err)
	}
	relay.FieldProtocols[0].Kind = "Clone"
	relay.FieldProtocols[0].Helper = "CloneValue"
	write(relay, src)
	write(origin, fieldTestSource+";const NewProducer=1")
	if _, err := loadEnumArtifact(relay.Path, imp); err == nil {
		t.Fatal("stale producer accepted")
	}
}
