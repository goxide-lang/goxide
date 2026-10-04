package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSerdeProtocolMetadataSignatures(t *testing.T) {
	core := enumTestPackage(t, "github.com/goxide-lang/serde", `package serde;type Serializer interface{String(string)error};type Deserializer interface{Skip()error}`, nil)
	imp := enumTestImporter{core.Path(): core}
	for _, kind := range []string{"Serialize", "Deserialize"} {
		t.Run(kind, func(t *testing.T) {
			helper := "func SerializeValue(s serde.Serializer,v *Value)error{return nil}"
			if kind == "Deserialize" {
				helper = "func DeserializeValue(d serde.Deserializer)(Value,error){return Value{},nil}"
			}
			source := `package fields;import "github.com/goxide-lang/serde";type Value struct{N int};` + helper
			p := enumTestPackage(t, "example.org/fields", source, imp)
			imp[p.Path()] = p
			m := fieldTestMetadata(kind)
			if err := validateEnumMetadata(m, p, imp); err != nil {
				t.Fatal(err)
			}
			relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/fields";type Alias=p.Value`, imp)
			alias := fieldTestMetadata(kind)
			alias.Path = relay.Path()
			alias.Package = relay.Name()
			alias.FieldProtocols[0].Name = "Alias"
			if err := validateEnumMetadata(alias, relay, imp); err != nil {
				t.Fatal(err)
			}
			badHelpers := []string{
				strings.Replace(helper, "error", "Wrong", 1) + `;type Wrong interface{Error()string}`,
				strings.Replace(helper, kind+"Value(", kind+"Value[T any](", 1),
				strings.Replace(helper, "func "+kind+"Value(", "var "+kind+"Value=func(", 1),
			}
			if kind == "Serialize" {
				badHelpers = append(badHelpers,
					"func SerializeValue(s any,v *Value)error{return nil}",
					"func SerializeValue(s serde.Serializer,v Value)error{return nil}",
					"func SerializeValue(s serde.Serializer,v ...*Value)error{return nil}",
					"func SerializeValue(s serde.Deserializer,v *Value)error{return nil}",
					"type Wrong serde.Serializer;func SerializeValue(s Wrong,v *Value)error{return nil}",
				)
			} else {
				badHelpers = append(badHelpers,
					"func DeserializeValue(d any)(Value,error){return Value{},nil}",
					"func DeserializeValue(d serde.Deserializer)(*Value,error){return nil,nil}",
					"func DeserializeValue(d ...serde.Deserializer)(Value,error){return Value{},nil}",
					"func DeserializeValue(d serde.Serializer)(Value,error){return Value{},nil}",
					"type Wrong Value;func DeserializeValue(d serde.Deserializer)(Wrong,error){return Wrong{},nil}",
				)
			}
			for i, bad := range badHelpers {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					src := `package fields;import "github.com/goxide-lang/serde";var _ serde.Serializer;type Value struct{N int};` + bad
					p := enumTestPackage(t, "example.org/fields", src, imp)
					if err := validateEnumMetadata(m, p, imp); err == nil {
						t.Fatal("accepted", bad)
					}
				})
			}
			for _, source := range []string{"", "manual", "native"} {
				bad := fieldTestMetadata(kind)
				bad.FieldProtocols[0].Source = source
				if err := validateEnumMetadata(bad, p, imp); err == nil {
					t.Fatal("source", source)
				}
			}
		})
	}
}

func TestSerdeProtocolArtifactOriginAuthorization(t *testing.T) {
	core := enumTestPackage(t, "github.com/goxide-lang/serde", `package serde;type Serializer interface{String(string)error};type Deserializer interface{Skip()error}`, nil)
	imp := equalityArtifactImporter{enumTestImporter: enumTestImporter{core.Path(): core}, dirs: map[string]string{}}
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
	const serdeSource = `package fields;import "github.com/goxide-lang/serde";type Value struct{N int};func SerializeValue(s serde.Serializer,v *Value)error{return nil};func DeserializeValue(d serde.Deserializer)(Value,error){return Value{},nil}`
	origin := fieldTestMetadata("Serialize")
	write(origin, serdeSource)
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
	relay.FieldProtocols[0].Kind = "Deserialize"
	relay.FieldProtocols[0].Helper = "DeserializeValue"
	write(relay, src)
	if _, err := loadEnumArtifact(relay.Path, imp); err == nil || !strings.Contains(err.Error(), "field protocol alias does not match") {
		t.Fatal("relay invented Deserialize", err)
	}
	relay.FieldProtocols[0].Kind = "Serialize"
	relay.FieldProtocols[0].Helper = "SerializeValue"
	write(relay, src)
	write(origin, serdeSource+";const NewProducer=1")
	if _, err := loadEnumArtifact(relay.Path, imp); err == nil {
		t.Fatal("stale producer accepted")
	}
}
