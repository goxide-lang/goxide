package compile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func TestFormalCFGDerivedProtocols(t *testing.T) {
	compilerSDKForTest(t)
	_, dependencies := formalSerdePins(t)
	dependencies = strings.Replace(dependencies, "[dependencies.standard]", "[dependencies.std]", 1)
	const source = `package main
use fmt;
#[cfg(feature("derive"))]
use std;
#[cfg(feature("derive"))]
use serde;
#[cfg(feature("derive"))]
use serde::json as json;
#[cfg_attr(feature("derive"), derive(std::derive::Default, std::derive::Debug, serde::derive::Serialize, serde::derive::Deserialize))]
type Record struct { Value int }
#[cfg(feature("derive"))]
func Exercise() {
    value := std::default_value[Record]()
    std::println!("{}", value.Value)
    value.Value = 7
    std::println!("{:?}", value)
    data, err := json::Marshal(&value, serde::Write[Record])
    std::println!("{} {}", string(data), err == nil)
    decoded, err := json::Unmarshal(data, serde::Read[Record])
    std::println!("{} {}", decoded.Value, err == nil)
}
#[cfg(not(feature("derive")))]
func Exercise() { fmt::Println("plain") }
func main() { Exercise() }
`
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			root := t.TempDir()
			manifest := "[go_mod]\nmodule='example.org/formal-cfg'\ngo='1.27'\n[[bin]]\nname='fixture'\npath='.'\n[features]\nderive=[]\n[dependencies.fmt]\ngo='fmt'\n" + dependencies
			writeProjectFile(t, root, project.ManifestName, manifest)
			writeProjectFile(t, root, "main.gox", source)
			writeProjectFile(t, root, "main_test.go", "package main\nimport \"testing\"\nfunc TestCFG(t *testing.T){Exercise()}\n")
			derives := 0
			options := Options{Offline: true, RunProcMacro: testWASI(t, func(request pm.Request) {
				if request.Kind() == pm.Derive {
					derives++
				}
			})}
			if enabled {
				options.Features = []string{"derive"}
			}
			first, err := PrepareProject(root, options)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 0
			want := "plain\n"
			if enabled {
				wantCalls = 4
				want = "0\nRecord { Value: 7 }\n{\"Value\":7} true\n7 true\n"
			}
			if derives != wantCalls {
				t.Fatalf("executed derives = %d, want %d", derives, wantCalls)
			}
			configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "program"), first.Entry)
			if got := configuredGo(t, first, "run", "-mod=readonly", first.Entry); got != want {
				t.Fatalf("conditional derive output = %q, want %q", got, want)
			}
			configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
			before, err := os.ReadFile(filepath.Join(first.Root, "main.go"))
			if err != nil {
				t.Fatal(err)
			}
			options.Locked = true
			second, err := PrepareProject(root, options)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(filepath.Join(second.Root, "main.go"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("conditional derive replay changed output: %v", err)
			}
		})
	}
}

func TestFormalCFGDeriveConflicts(t *testing.T) {
	compilerSDKForTest(t)
	_, dependencies := formalSerdePins(t)
	dependencies = strings.Replace(dependencies, "[dependencies.standard]", "[dependencies.std]", 1)
	for _, test := range []struct{ name, declaration string }{
		{"manual-default", `#[cfg_attr(feature("derive"), derive(std::derive::Default))]
type Record struct { Value int }
impl std::Default for Record { func Default() Record { return Record{Value:9} } }`},
		{"duplicate-debug", `#[derive(std::derive::Debug)]
#[cfg_attr(feature("derive"), derive(std::derive::Debug))]
type Record struct { Value int }`},
		{"duplicate-serde", `#[derive(serde::derive::Serialize)]
#[cfg_attr(feature("derive"), derive(serde::derive::Serialize))]
type Record struct { Value int }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, project.ManifestName, "[go_mod]\nmodule='example.org/formal-cfg-conflict'\ngo='1.27'\n[[bin]]\nname='fixture'\npath='.'\n[features]\nderive=[]\n"+dependencies)
			writeProjectFile(t, root, "main.gox", "package main\nuse std;\nuse serde;\n"+test.declaration+"\nfunc main(){}\n")
			options := Options{Offline: true, RunProcMacro: testWASI(t, nil)}
			first, err := PrepareProject(root, options)
			if err != nil {
				t.Fatalf("disabled derive produced conflict: %v", err)
			}
			configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "program"), first.Entry)
			options.Features = []string{"derive"}
			_, err = PrepareProject(root, options)
			if err == nil || !strings.Contains(err.Error(), "overlapping") {
				t.Fatalf("enabled derive conflict was not diagnosed: %v", err)
			}
		})
	}
}
