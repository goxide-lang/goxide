package project

import (
	"strings"
	"testing"
)

func TestLibraryNamespaceValidation(t *testing.T) {
	for _, name := range []string{"app", "app_core", "_library", "工具"} {
		manifest := mappingHeader + "[lib]\nname='" + name + "'\npath='src'\n[[bin]]\nname='app-cli'\npath='cmd/app'\n"
		if _, err := Parse("library.toml", []byte(manifest)); err != nil {
			t.Fatalf("valid library %q: %v", name, err)
		}
	}
	for _, name := range []string{"crate", "app-core", "1app", "_", "use", "type"} {
		_, err := Parse("library.toml", []byte(mappingHeader+"[lib]\nname='"+name+"'\npath='src'\n"))
		if err == nil || !strings.Contains(err.Error(), "library namespace") {
			t.Fatalf("invalid library %q: %v", name, err)
		}
	}
	for _, optional := range []string{"", "optional=true\n"} {
		_, err := Parse("library.toml", []byte(mappingHeader+"[lib]\nname='fmt'\npath='src'\n[dependencies.fmt]\ngo='fmt'\n"+optional))
		if err == nil || !strings.Contains(err.Error(), "conflicts with dependency alias") {
			t.Fatalf("library collided with declared dependency: %v", err)
		}
	}
}

func TestSelectLibraryAndExample(t *testing.T) {
	m := Manifest{
		Lib:      &Target{Name: "core", Path: "src", RequiredFeatures: []string{"library"}},
		Bins:     []Target{{Name: "app", Path: "cmd/app"}},
		Examples: []Target{{Name: "demo", Path: "examples/demo", RequiredFeatures: []string{"demo"}}},
	}
	library, err := Select(m, "", "", true, map[string]bool{"library": true})
	if err != nil || library.Name != "core" || library.Path != "src" {
		t.Fatalf("select library: %+v, %v", library, err)
	}
	example, err := Select(m, "", "demo", false, map[string]bool{"demo": true})
	if err != nil || example.Name != "demo" || example.Path != "examples/demo" {
		t.Fatalf("select example: %+v, %v", example, err)
	}
	for _, choice := range []struct {
		bin, example string
		lib          bool
		want         string
	}{
		{"app", "demo", false, "mutually exclusive"},
		{"app", "", true, "mutually exclusive"},
		{"", "demo", true, "mutually exclusive"},
		{"", "", true, "requires feature library"},
		{"", "demo", false, "requires feature demo"},
	} {
		_, err := Select(m, choice.bin, choice.example, choice.lib, nil)
		if err == nil || !strings.Contains(err.Error(), choice.want) {
			t.Fatalf("target selection %v: %v", choice, err)
		}
	}
}
