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
		_, err := CompilePackageWithOptions("example.org/new", map[string][]byte{"new.gox": []byte("package newer\nuse old as _;\n")}, PackageOptions{Importer: legacyNamingImporter{marker}, Roots: frontendFixtureRoots(map[string]string{"old": "example.org/old"})})
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

func TestNamingModuleInputs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		generated map[string][]byte
		source    string
	}{
		{name: "directory-directive", source: "//hgo:cfg off\npackage p\n"},
		{name: "generated-source", source: "package p\n", generated: map[string][]byte{"old.hgo": []byte("package p\n")}},
		{name: "generated-directive", source: "package p\n", generated: map[string][]byte{"new.gox": []byte("//hgo:cfg off\npackage p\n")}},
		{name: "generated-metadata", source: "package p\n", generated: map[string][]byte{"hgo_project_config.go": []byte("package p\n")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := sharedStdModule(t, "example.org/naming")
			sharedStdWrite(t, root, "main.gox", tc.source)
			_, err := CompileModuleWithOptions(root, t.TempDir()+"/out", ModuleOptions{GeneratedFiles: tc.generated})
			if err == nil || !strings.Contains(err.Error(), "legacy") {
				t.Fatal(err)
			}
		})
	}
}
