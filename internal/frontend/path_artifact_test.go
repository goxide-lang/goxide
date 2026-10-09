package frontend

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/paths"
)

type pathArtifactImporter struct {
	*enumArtifactTestImporter
	module string
}

func (i *pathArtifactImporter) PackageOrigin(string) (string, bool, error) {
	return i.module, false, nil
}

func TestStaticPathNamespaceArtifactRetainsSourceProof(t *testing.T) {
	const owner = "example.org/api"
	const module = "example.org/std"
	const child = module + "/formatting"
	group := paths.Namespace{ModulePath: module, Source: "module", Children: map[string]paths.Namespace{"formatting": {PackagePath: child, ModulePath: module, Source: "module"}}}
	m := useMetadata{Version: useMetadataVersion, Path: owner, Package: "api", Bindings: []useBinding{{Name: "standard", Kind: "namespace", OriginName: "standard", RouteName: "standard", Namespace: &group, Public: true}}}
	source := []byte(fmt.Sprintf("package api\nconst %s=%q\n", useMetadataMarker, m.marker()))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "producer.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	imp := &pathArtifactImporter{enumArtifactTestImporter: &enumArtifactTestImporter{enumTestImporter: enumTestImporter{
		owner: enumTestPackage(t, owner, string(source), nil),
		child: enumTestPackage(t, child, "package formatting;func Int(v int)int{return v}", nil),
	}, dir: dir, content: "selected-source-and-dependencies"}, module: module}
	if err := writeUseArtifact(dir, owner, m, map[string][]byte{"producer.go": source}, imp); err != nil {
		t.Fatal(err)
	}
	u := &usePackage{imp: imp, metadata: useMetadata{Path: "example.org/consumer"}}
	loaded, err := u.load(owner)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Bindings[0].Namespace
	if got.Source != "module" || got.ModulePath != module || got.Children["formatting"].PackagePath != child {
		t.Fatalf("source proof lost: %+v", got)
	}
	imp.module = "example.org/wrong"
	if _, err := u.surface(got.Children["formatting"]); err == nil || !strings.Contains(err.Error(), "source mismatch") {
		t.Fatalf("wrong module accepted: %v", err)
	}
	imp.module = module
	imp.enumTestImporter[child] = types.NewPackage("example.org/fake", "formatting")
	if _, err := u.surface(got.Children["formatting"]); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("wrong package accepted: %v", err)
	}
}
