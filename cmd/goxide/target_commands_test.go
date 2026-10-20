package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestProjectLibraryBinaryAndExampleCommands(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"goxide.toml": `[go_mod]
module = "example.org/targets"
go = "1.27"
[lib]
name = "core"
path = "src"
[[bin]]
name = "one"
path = "cmd/one"
[[bin]]
name = "two"
path = "cmd/two"
[[example]]
name = "demo"
path = "examples/demo"
[dependencies]
fmt = { go = "fmt" }
`,
		"src/lib.gox":         "package core\npub mod model;\nuse model;\nfunc Make() model::Item { return model::Item{Value: 42} }\n",
		"src/model/model.gox": "package model\ntype Item struct { Value int }\n",
		"src/lib_test.go": `package core
import "testing"
func TestLibrary(t *testing.T) {
	if Make().Value != 42 { t.Fatal("wrong library value") }
}
`,
	}
	for _, name := range []string{"cmd/one", "cmd/two", "examples/demo"} {
		files[name+"/main.gox"] = `package main
use core;
use core::model;
use fmt;
func main() { var item model::Item = core::Make(); fmt::Println(item.Value) }
`
		files[name+"/main_test.go"] = `package main
import (
	"testing"
	core "example.org/targets/src"
	"example.org/targets/src/model"
)
func TestSharedIdentity(t *testing.T) {
	var item model.Item = core.Make()
	if item.Value != 42 { t.Fatal("wrong shared value") }
}
`
	}
	writeRootFixture(t, root, files)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	var libraryBytes []byte
	for _, choice := range []struct {
		name, kind, entry string
		flags             []string
	}{
		{"library", "lib", "src", []string{"--lib"}},
		{"first binary", "bin", "cmd/one", []string{"--bin", "one"}},
		{"second binary", "bin", "cmd/two", []string{"--bin", "two"}},
		{"example", "example", "examples/demo", []string{"--example", "demo"}},
	} {
		t.Run(choice.name, func(t *testing.T) {
			invoke := func(mode string) string {
				t.Helper()
				var output bytes.Buffer
				args := append([]string{mode, "--go", tool, "--offline"}, choice.flags...)
				if err := run(append(args, root), &output, &output); err != nil {
					t.Fatalf("%s %v: %v\n%s", mode, choice.flags, err, &output)
				}
				return strings.TrimSpace(output.String())
			}
			generated := invoke("transpile")
			contents, err := os.ReadFile(filepath.Join(generated, "src/lib.go"))
			if err != nil {
				t.Fatal(err)
			}
			if libraryBytes == nil {
				libraryBytes = contents
			} else if !bytes.Equal(libraryBytes, contents) {
				t.Fatal("selecting another target changed the shared library")
			}
			first := singleFileSnapshot(t, generated)
			if second := invoke("transpile"); second != generated || !reflect.DeepEqual(first, singleFileSnapshot(t, second)) {
				t.Fatal("same target changed generated path or bytes")
			}
			built := invoke("build")
			if choice.kind == "lib" {
				if built != "" {
					t.Fatalf("library build published a binary: %s", built)
				}
			} else {
				if info, err := os.Stat(built); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("binary output %q: %v", built, err)
				}
				if got := invoke("run"); got != "42" {
					t.Fatalf("run = %q", got)
				}
			}
			if output := invoke("test"); !strings.Contains(output, "example.org/targets/"+choice.entry) {
				t.Fatalf("selected target test did not run: %s", output)
			}
		})
	}
	var output bytes.Buffer
	if err := run([]string{"run", "--lib", root}, &output, &output); err == nil || !strings.Contains(err.Error(), "cannot be run") {
		t.Fatalf("run library: %v", err)
	}
}
