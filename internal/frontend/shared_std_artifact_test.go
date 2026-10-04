package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestSharedStdValidatorOwnerAuthorization(t *testing.T) {
	std, err := typecheck.NewImporter("artifact.gox").Import(enums.SumPackage)
	if err != nil {
		t.Fatal(err)
	}
	imp := equalityArtifactImporter{enumTestImporter: enumTestImporter{enums.SumPackage: std}, dirs: map[string]string{}}
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
	family := sharedSumFamily("Option")
	family.Instances = []enumFamilyInstanceExport{{ValidatorPath: "example.org/owner", Validator: "Check"}, {ValidatorPath: "example.org/owner", Validator: "Also"}}
	owner := enumMetadata{Version: enumMetadataVersion, Path: "example.org/owner", Package: "owner", Families: []enumFamilyExport{family}}
	write(owner, `package owner;import s "github.com/goxide-lang/std/sum";type Option[T any]=s.Option[T];func Check(Option[int]){};func Also(Option[int]){};func Unregistered(Option[int]){}`)
	other := enumMetadata{Version: enumMetadataVersion, Path: "example.org/other", Package: "other", Families: []enumFamilyExport{sharedSumFamily("Option")}}
	write(other, `package other;import s "github.com/goxide-lang/std/sum";type Option[T any]=s.Option[T];func Check(Option[int]){}`)
	relay := enumMetadata{Version: enumMetadataVersion, Path: "example.org/relay", Package: "relay", Families: []enumFamilyExport{family}}
	body := `package relay;import o "example.org/owner";import x "example.org/other";type Option[T any]=o.Option[T];var _=x.Check`
	write(relay, body)
	if _, err := loadEnumArtifact(relay.Path, imp); err != nil {
		t.Fatal("registered owner", err)
	}
	for _, tc := range []struct{ name, owner, helper string }{{"owner-path", "example.org/other", "Check"}, {"same-signature-unregistered", "example.org/owner", "Unregistered"}} {
		t.Run(tc.name, func(t *testing.T) {
			changed := relay
			changed.Families = []enumFamilyExport{sharedSumFamily("Option")}
			changed.Families[0].Instances = []enumFamilyInstanceExport{{ValidatorPath: tc.owner, Validator: tc.helper}}
			// Rebind both source marker and artifact digest, so this tests ownership,
			// not merely rejection of an inconsistent JSON edit.
			write(changed, body)
			if _, err := loadEnumArtifact(changed.Path, imp); err == nil || !strings.Contains(err.Error(), "not registered by owner") {
				t.Fatalf("owner provenance bypass: %v", err)
			}
		})
	}
	// Candidate order must not change generated bindings or owner aliases.
	forward := family
	backward := family
	backward.Instances = []enumFamilyInstanceExport{family.Instances[1], family.Instances[0]}
	var outputs [][]byte
	for _, f := range []enumFamilyExport{forward, backward} {
		metadata := relay
		metadata.Families = []enumFamilyExport{f}
		write(metadata, body)
		loaded, err := loadEnumArtifact(relay.Path, imp)
		if err != nil {
			t.Fatal(err)
		}
		decls, prelude, imports, _ := importedDeclarations("example.org/consumer", map[string][]byte{"input.gox": []byte("package consumer\nimport r \"example.org/relay\"\nvar V r.Option[int]\n")}, map[string]PackageResult{relay.Path: {enumMetadata: loaded, enumTypes: imp.enumTestImporter[relay.Path]}})
		data, err := json.Marshal(decls)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, append(data, []byte(prelude["input.gox"]+imports["input.gox"])...))
	}
	if !bytes.Equal(outputs[0], outputs[1]) {
		t.Fatal("candidate order changed generated imports/prelude")
	}
}
