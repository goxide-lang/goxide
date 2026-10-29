package playground

import (
	"encoding/json"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/typecheck"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

// The fixed exports deliberately have no previously generated std/serde root
// package or native relay. Every edit must derive those from the same sources.
func TestPackageSourceGraphFollowsEdits(t *testing.T) {
	const program = `package main
use fmt;
use std;
use serde;
use serde::json;
#[derive(std::derive::Clone, std::derive::Debug, serde::derive::Serialize, serde::derive::Deserialize)]
struct Item { Value FIELD }
func main() {
 item := Item{Value: VALUE}
 data, err := json::Marshal(&item, serde::Write[Item])
 if err != nil { panic(err) }
 decoded, err := json::Unmarshal(data, serde::Read[Item])
 if err != nil { panic(err) }
 copy := std::Clone::clone(&decoded)
 fmt::Println(string(data), std::Debug::debug(&copy))
}
`
	variants := []struct{ field, value, want string }{
		{"int", "21", "{\"Value\":21} Item { Value: 21 }\n"},
		{"[]uint16", "[]uint16{3, 5}", "{\"Value\":[3,5]} Item { Value: [3, 5] }\n"},
	}
	files := make([][]File, len(variants))
	for index, variant := range variants {
		files[index] = []File{{"main.gox", strings.NewReplacer("FIELD", variant.field, "VALUE", variant.value).Replace(program)}}
	}
	input := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(input, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("goxide.toml", strings.Replace(testdeps.FormalManifest(t, PackagePath, "playground"), "[lib]", "[[bin]]", 1))
	write("main.gox", files[0][0].Source)
	var outputs [][]File
	options := Options{}
	first, _, err := testdeps.PrepareFormalBinary(t, input, compile.Options{
		PrepareNativePackage: func(modules []analysis.Module) error {
			options.Config = modules[0].Config
			// This importer is rooted in the compiler's pinned raw modules, not
			// the generated fixture whose export data describe only the first edit.
			options.Importer = &sourceGraphExports{Importer: typecheck.NewImporter("source_graph_test.go", typecheck.ImporterOptions{Config: options.Config})}
			return nil
		},
		OnPreparedSources: func(prepared compile.PreparedSources) error {
			options.Roots, options.RuntimeRoots = prepared.Roots, prepared.RuntimeRoots
			data, err := json.Marshal(prepared.SourcePackages)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &options.SourcePackages); err != nil {
				return err
			}
			for _, artifact := range prepared.Macros {
				options.ProcLibraries = append(options.ProcLibraries, artifact.Library)
			}
			for _, edit := range files {
				output, err := CompilePackage(edit, options)
				if err != nil {
					return err
				}
				if len(output) != 1 || output[0].Path != "main.go" {
					t.Fatalf("dependency files escaped into user output: %v", output)
				}
				outputs = append(outputs, output)
			}
			denied := []File{{"denied.gox", "package main\nuse net::http;\nfunc main(){_=http::MethodGet}\n"}}
			if output, err := CompilePackage(denied, options); err == nil || output != nil || !strings.Contains(err.Error(), "outside the fixed export set") {
				t.Fatalf("unknown export bypassed the fixed importer: output=%v error=%v", output, err)
			}
			for index, edit := range files {
				again, err := CompilePackage(edit, options)
				if err != nil || !reflect.DeepEqual(outputs[index], again) {
					t.Fatalf("request graph leaked across edits: %v", err)
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, variant := range variants {
		project := first
		if index != 0 {
			write("main.gox", files[index][0].Source)
			project, _, err = testdeps.PrepareFormalBinary(t, input)
			if err != nil {
				t.Fatal(err)
			}
		}
		native, err := os.ReadFile(filepath.Join(project.Root, "main.go"))
		if err != nil || string(native) != outputs[index][0].Source {
			t.Fatalf("native and in-memory Go differ for %s: %v", variant.field, err)
		}
		testdeps.RunFormalGo(t, project, "build", "-mod=readonly", "-o", filepath.Join(t.TempDir(), "program"), ".")
		if stdout := testdeps.RunFormalGo(t, project, "run", "-mod=readonly", "."); stdout != variant.want {
			t.Fatalf("%s output = %q, want %q", variant.field, stdout, variant.want)
		}
	}
}

type sourceGraphExports struct{ types.Importer }

func (i *sourceGraphExports) Import(path string) (*types.Package, error) {
	switch path {
	case "github.com/goxide-lang/std", "github.com/goxide-lang/serde", "github.com/goxide-lang/serde/json", "net/http":
		return nil, fmt.Errorf("%s is outside the fixed export set", path)
	}
	return i.Importer.Import(path)
}

func (i *sourceGraphExports) PackageIndex(source, module string) ([]string, error) {
	return i.Importer.(interface {
		PackageIndex(string, string) ([]string, error)
	}).PackageIndex(source, module)
}

func (i *sourceGraphExports) PackageOrigin(path string) (string, bool, error) {
	if _, err := i.Import(path); err != nil {
		return "", false, err
	}
	return i.Importer.(interface {
		PackageOrigin(string) (string, bool, error)
	}).PackageOrigin(path)
}
