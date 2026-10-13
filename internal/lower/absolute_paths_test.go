package lower_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/resolve"
)

func TestLowerAbsolutePathsPreserveImportedMemberNames(t *testing.T) {
	sourceDirectory := t.TempDir()
	sources := map[string]string{
		"lib/lib.gox": `package lib
pub type item struct { pub value int }
pub func (v *item) next() *item { return v }
pub func makeItem() *item { return &item{value: 7} }
`,
		"main.gox": `package main
use fmt;
use fixture::lib;
func main() {
	fmt::Println(lib::makeItem().next().value, lib::item{value: 8}.value)
}
`,
	}
	absoluteSources := make(map[string]string, len(sources))
	for name, source := range sources {
		writeDiagnosticModuleFile(t, sourceDirectory, name, []byte(source))
		absoluteSources[filepath.Join(sourceDirectory, filepath.FromSlash(name))] = source
	}
	files := parseDiagnosticSources(t, absoluteSources)
	initial := resolve.Resolve(files, nil)
	res := resolve.Resolve(files, absolutePathImporter{root: sourceDirectory, result: initial})
	if len(res.Diagnostics) != 0 {
		t.Fatalf("resolve: %v", res.Diagnostics)
	}
	output, err := lower.Lower(files, res)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != len(files) {
		t.Fatalf("output has %d files, want %d", len(output), len(files))
	}
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module fixture\n\ngo 1.27\n"))
	for name := range files {
		data, ok := output[name]
		if !ok {
			t.Fatalf("output dropped absolute file key %s", name)
		}
		rel, err := filepath.Rel(sourceDirectory, name)
		if err != nil {
			t.Fatal(err)
		}
		writeDiagnosticModuleFile(t, directory, strings.TrimSuffix(rel, ".gox")+".go", data)
	}
	runGo(t, directory, "build", "./...")
	if got := runGo(t, directory, "run", "."); got != "7 8\n" {
		t.Fatalf("imported member output = %q, want %q", got, "7 8\n")
	}
}

type absolutePathImporter struct {
	root   string
	result *resolve.Result
}

func (i absolutePathImporter) Lookup(parts []string, name resolve.Key) []resolve.External {
	if len(parts) > 0 && parts[0] == "fixture" {
		directory := filepath.Join(append([]string{i.root}, parts[1:]...)...)
		for _, pkg := range i.result.Packages {
			if pkg.Dir == directory {
				if symbol := pkg.Scope.Bindings[name]; symbol != nil && symbol.Visibility == resolve.Public {
					return []resolve.External{{Name: name, Kind: symbol.Kind, GoName: symbol.GoName, Visibility: symbol.Visibility}}
				}
				return nil
			}
		}
	}
	return (fixtureImporter{result: i.result}).Lookup(parts, name)
}
