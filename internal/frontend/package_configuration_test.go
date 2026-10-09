package frontend

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/paths"
)

func TestPackageConfigurationSemanticFingerprint(t *testing.T) {
	sources := map[string][]byte{"main.gox": []byte("package subject\nfunc F(){}\n")}
	for _, roots := range []paths.Roots{nil, {}} {
		prepared, file, err := packageConfigurationSources(sources, roots, "", "")
		if err != nil || file != "" || len(prepared) != 1 {
			t.Fatalf("empty context generated a file: %q %v", file, err)
		}
	}
	root := paths.Namespace{PackagePath: "example.org/library", ModulePath: "example.org/library", Source: "module", Children: map[string]paths.Namespace{
		"format": {PackagePath: "example.org/library/format", ModulePath: "example.org/library", Source: "module"},
	}}
	roots := paths.Roots{"library": root, "fmt": {PackagePath: "fmt", Source: "toolchain"}}
	get := func(roots paths.Roots, configuration string) []byte {
		t.Helper()
		prepared, file, err := packageConfigurationSources(sources, roots, configuration, "")
		if err != nil || file != packageConfigurationFile || len(prepared) != 2 {
			t.Fatalf("configuration source: %q %v", file, err)
		}
		if len(sources) != 1 || string(sources["main.gox"]) != "package subject\nfunc F(){}\n" {
			t.Fatal("caller source map mutated")
		}
		return prepared[file]
	}
	base := get(roots, "features=demo")
	if !bytes.Equal(base, get(paths.Roots{"fmt": roots["fmt"], "library": root}, "features=demo")) {
		t.Fatal("root insertion order changed the semantic context")
	}
	if bytes.Equal(base, get(roots, "features=other")) {
		t.Fatal("planner configuration disappeared")
	}
	for _, field := range []string{"package", "owner", "source", "child"} {
		changed := root
		switch field {
		case "package":
			changed.PackagePath += "/other"
		case "owner":
			changed.ModulePath = "example.org"
		case "source":
			changed.Source = "local"
		case "child":
			changed.Children = map[string]paths.Namespace{"format": {PackagePath: "example.org/library/other", ModulePath: root.ModulePath, Source: root.Source}}
		}
		if bytes.Equal(base, get(paths.Roots{"library": changed, "fmt": roots["fmt"]}, "features=demo")) {
			t.Fatalf("%s change disappeared from the semantic context", field)
		}
	}
}

func TestPackageConfigurationReservedOutput(t *testing.T) {
	for _, name := range []string{"goxide_project_config.go", "goxide_project_config.gox"} {
		sources := map[string][]byte{name: []byte("package subject\n")}
		_, _, err := packageConfigurationSources(sources, paths.Roots{"fmt": {PackagePath: "fmt"}}, "", "")
		if err == nil || !strings.Contains(err.Error(), "reserved project config filename conflicts") {
			t.Fatalf("%s: got %v", name, err)
		}
	}
	// A generated native Go package can be compiled independently without
	// asking the language compiler to insert a second semantic context file.
	sources := map[string][]byte{packageConfigurationFile: []byte("package subject\n")}
	if _, file, err := packageConfigurationSources(sources, nil, "", ""); err != nil || file != "" {
		t.Fatalf("native output round trip: %q %v", file, err)
	}
}
