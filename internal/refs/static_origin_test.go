package refs

import (
	"fmt"
	"strings"
	"testing"
)

func TestImportedFunctionValueReportsMemberToken(t *testing.T) {
	meta := Metadata{Version: MetadataVersion, Path: "example/api", Functions: map[string]Signature{
		"View": {Params: []Slot{{Type: "*int", Permission: Read}}, Results: []Slot{{Type: "*int", Permission: Read}}},
	}}
	dep := checkedPathPackage(t, meta.Path, fmt.Sprintf("package api\nconst %s=%q\nfunc View(p *int)*int{return p}\n", MetadataMarker, meta.Marker()))
	for _, expression := range []string{"goxideNamespace0.View", "(goxideNamespace0.View)"} {
		source := "package p\nimport goxideNamespace0 \"example/api\"\nfunc f(){_=" + expression + "}\n"
		_, _, err := BindImportsWithPath("example/p", map[string][]byte{"use.go": []byte(source)}, metadataImporter{meta.Path: dep}, map[string]Metadata{meta.Path: meta})
		diagnostic, ok := err.(*ImportError)
		if !ok || diagnostic.Position.Offset != strings.Index(source, ".View")+1 || !strings.Contains(diagnostic.Message, "function values/callbacks") {
			t.Fatalf("diagnostic must identify the verified member, not its synthetic namespace: %v", err)
		}
	}
}
