package frontend

import (
	"go/constant"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

type legacyNamingImporter struct{ marker string }

func (i legacyNamingImporter) Import(path string) (*types.Package, error) {
	p := types.NewPackage(path, "old")
	p.Scope().Insert(types.NewConst(token.NoPos, p, i.marker, types.Typ[types.UntypedString], constant.MakeString("old")))
	p.MarkComplete()
	return p, nil
}
func TestNamingRejectsOldProducer(t *testing.T) {
	for _, marker := range []string{"HgoEnumMetadata", "HgoReferenceMetadata", "HgoUseMetadata", "HgoSumABI"} {
		_, err := CompilePackage("example.org/new", map[string][]byte{"new.gox": []byte("package newer\nimport _ \"example.org/old\"\n")}, legacyNamingImporter{marker})
		if err == nil || !strings.Contains(err.Error(), "regenerate") {
			t.Fatalf("%s: %v", marker, err)
		}
	}
}
func TestNamingLegacyDirectiveIsNotComment(t *testing.T) {
	for _, src := range []string{"//hgo:cfg off\npackage p\n", "package p\n//hgo:cfg off\nfunc F(){}\n"} {
		if _, err := Compile("p.gox", []byte(src)); err == nil || !strings.Contains(err.Error(), "legacy hgo directive") {
			t.Fatal(err)
		}
	}
	// A literal containing old text is user data, not a directive.
	if _, err := Compile("p.gox", []byte("package p\nconst X = `//hgo:cfg off`\n")); err != nil {
		t.Fatal(err)
	}
}
