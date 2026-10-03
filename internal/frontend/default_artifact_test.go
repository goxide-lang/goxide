package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func defaultTestMetadata() enumMetadata {
	return enumMetadata{Version: enumMetadataVersion, Path: "example.org/defaults", Package: "defaults", Defaults: []defaultExport{{Name: "Value", DefinitionPath: "example.org/defaults", DefinitionName: "Value", Helper: "DefaultValue", Source: "manual"}}}
}

const defaultTestSource = `package defaults;type Value struct{private int};func DefaultValue()Value{return Value{private:7}}`

func TestDefaultMetadataValidation(t *testing.T) {
	p := enumTestPackage(t, "example.org/defaults", defaultTestSource, nil)
	imp := enumTestImporter{p.Path(): p}
	m := defaultTestMetadata()
	if !m.present() {
		t.Fatal("Default-only metadata absent")
	}
	if err := validateEnumMetadata(m, p, imp); err != nil {
		t.Fatal(err)
	}
	relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/defaults";type Alias=p.Value`, imp)
	m.Path = relay.Path()
	m.Package = relay.Name()
	m.Defaults[0].Name = "Alias"
	if err := validateEnumMetadata(m, relay, imp); err != nil {
		t.Fatal(err)
	}
	defined := enumTestPackage(t, relay.Path(), `package relay;import p "example.org/defaults";type Alias p.Value`, imp)
	if err := validateEnumMetadata(m, defined, imp); err == nil || !strings.Contains(err.Error(), "alias identity") {
		t.Fatal(err)
	}
	for _, helper := range []string{
		`func DefaultValue(v int)Value{return Value{}}`,
		`func DefaultValue()*Value{return nil}`,
		`type Other Value;func DefaultValue()Other{return Other{}}`,
		`func DefaultValue[T any]()Value{return Value{}}`,
		`func DefaultValue(...int)Value{return Value{}}`,
		`func DefaultValue()(Value,error){return Value{},nil}`,
		`var DefaultValue=func()Value{return Value{}}`,
	} {
		p := enumTestPackage(t, "example.org/defaults", "package defaults;type Value struct{private int};"+helper, nil)
		if err := validateEnumMetadata(defaultTestMetadata(), p, enumTestImporter{p.Path(): p}); err == nil {
			t.Fatal("invalid helper accepted", helper)
		}
	}
	for _, source := range []string{"", "native", "guessed"} {
		m := defaultTestMetadata()
		m.Defaults[0].Source = source
		if err := validateEnumMetadata(m, p, imp); err == nil {
			t.Fatal(source)
		}
	}
	m = defaultTestMetadata()
	m.Defaults = append(m.Defaults, m.Defaults[0])
	if err := validateEnumMetadata(m, p, imp); err == nil {
		t.Fatal("duplicate accepted")
	}
	m, err := collectEnumExports(p.Path(), PackageResult{}, p)
	if err != nil || m.present() {
		t.Fatal("helper spelling inferred registration", m, err)
	}
}
func TestDefaultArtifactOriginAuthorization(t *testing.T) {
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
	origin := defaultTestMetadata()
	write(origin, defaultTestSource)
	if _, err := loadEnumArtifact(origin.Path, imp); err != nil {
		t.Fatal(err)
	}
	relay := origin
	relay.Defaults = append([]defaultExport(nil), origin.Defaults...)
	relay.Path = "example.org/relay"
	relay.Package = "relay"
	relay.Defaults[0].Name = "Alias"
	src := `package relay;import p "example.org/defaults";type Alias=p.Value`
	write(relay, src)
	if _, err := loadEnumArtifact(relay.Path, imp); err != nil {
		t.Fatal(err)
	}
	relay.Defaults[0].Source = "derive"
	write(relay, src)
	if _, err := loadEnumArtifact(relay.Path, imp); err == nil || !strings.Contains(err.Error(), "Default alias does not match") {
		t.Fatal("relay changed provenance", err)
	}
	// Restoring the relay then changing its producer must invalidate the dependency.
	relay.Defaults[0].Source = "manual"
	write(relay, src)
	origin.Defaults[0].Source = "derive"
	write(origin, defaultTestSource)
	if _, err := loadEnumArtifact(relay.Path, imp); err == nil {
		t.Fatal("stale relay accepted")
	}
}
func TestCollectDefaultRegistrations(t *testing.T) {
	p := enumTestPackage(t, "example.org/defaults", defaultTestSource+";type Alias=Value;type Fake Value", nil)
	result := PackageResult{Files: map[string]Result{"a.hgo": {defaultRegistrations: map[string]string{"Value": "manual"}}}}
	m, err := collectEnumExports(p.Path(), result, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Defaults) != 2 || m.Defaults[0].Name != "Alias" || m.Defaults[1].Name != "Value" || m.Defaults[0].Source != "manual" {
		t.Fatal(m.Defaults)
	}
	if err = validateEnumMetadata(m, p, enumTestImporter{p.Path(): p}); err != nil {
		t.Fatal(err)
	}
}
