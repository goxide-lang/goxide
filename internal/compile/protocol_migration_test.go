package compile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// These runtime traits use ordinary names deliberately: this checks the library
// path without the builtin derive registry. This covers concrete structs/enums, sums and
// containers. Default recursion, deep enum PartialEq and the derived Eq
// qualification rules remain outside this migration proof.
func TestProceduralLibraryProtocolsMigration(t *testing.T) {
	sdk := compilerSDKForTest(t)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	module, err := project.ReadMod(tool, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var stdVersion string
	for _, dependency := range module.Require {
		if dependency.Path == "github.com/goxide-lang/std" {
			stdVersion = dependency.Version
		}
	}
	if stdVersion == "" {
		t.Fatal("compiler go.mod must pin std")
	}
	root := t.TempDir()
	macros, protocol := filepath.Join(root, "macros"), filepath.Join(root, "protocol")
	model, app := filepath.Join(root, "model"), filepath.Join(root, "app")
	writeProcBuildManifest(t, macros, sdk, "")
	fixture := func(destination, name, source string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "proc_protocols", source))
		if err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, destination, name, string(data))
	}
	fixture(macros, "lib.gox", "macros.gox")
	fixture(macros, "derive.go", "derive.go")
	writeProjectFile(t, protocol, project.ManifestName, `[go_mod]
module="example.org/library-protocols"
go="1.27"
[lib]
name="protocol"
path="."
[dependencies.standard]
go="github.com/goxide-lang/std"
version="`+stdVersion+`"
`)
	fixture(protocol, "lib.gox", "protocol.gox")
	writeProjectFile(t, model, project.ManifestName, `[go_mod]
module="example.org/library-protocol-model"
go="1.27"
[lib]
name="model"
path="."
[dependencies.macros]
path="../macros"
[dependencies.api]
path="../protocol"
[dependencies.standard]
go="github.com/goxide-lang/std"
version="`+stdVersion+`"
`)
	fixture(model, "lib.gox", "model.gox")
	fixture(model, "extension.gox", "extension.gox")
	writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/library-protocol-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.model]
path="../model"
`)
	writeProjectFile(t, app, "main.go", `package main
import("fmt";model "example.org/library-protocol-model")
func main(){model.CheckExtended();fmt.Println(model.Evaluate())}
`)
	writeProjectFile(t, app, "main_test.go", `package main
import("testing";model "example.org/library-protocol-model")
func TestOrdinaryProtocolEntry(t *testing.T) {
 const want="Record { first: 1, second: 2 } | Containers { values: [9, 2], fixed: [8, 4] } | true true"
 if got:=model.Evaluate();got!=want {t.Fatalf("protocol entry = %q, want %q",got,want)}

}
`)
	fixture(app, "extension_test.go", "extension_test.go")
	deriveCalls := 0
	run := testWASI(t, func(request pm.Request) {
		if request.Kind() == pm.Derive {
			deriveCalls++
		}
	})
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if deriveCalls != 15 {
		t.Fatalf("derive calls = %d, want 15", deriveCalls)
	}
	for _, operation := range []string{"build", "run", "test"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		want := "Record { first: 1, second: 2 } | Containers { values: [9, 2], fixed: [8, 4] } | true true\n"
		if err != nil || operation == "run" && string(output) != want {
			t.Fatalf("ordinary Go consumer %s: %v\n%s", operation, err, output)
		}
	}
	snapshot := projectSnapshot(t, filepath.Dir(first.Root))
	lock, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(snapshot, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("locked replay changed generated protocol implementations or macro artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed dependency lock: %v", err)
	}
	// Ordinary constraints must reject missing protocols, including inactive sum
	// branches. No Result/container Default implementation is supplied by the library.
	if err := os.Remove(filepath.Join(model, "extension.gox")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, body, want string }{
		{"result-default", "struct Bad { Value Result[int,string] }", "implementation prerequisites are not satisfied"},
		{"slice-default", "struct Bad { Value []int }", "implementation prerequisites are not satisfied"},
		{"array-default", "struct Bad { Value [2]int }", "implementation prerequisites are not satisfied"},
		{"map-default", "struct Bad { Value map[string]int }", "implementation prerequisites are not satisfied"},
		{"selected-default", "enum Bad { #[library_default] Selected(chan int),Other, }", "implementation prerequisites are not satisfied"},
		{"missing-default", "enum Bad { A,B, }", "exactly one library_default"},
		{"duplicate-default", "enum Bad { #[library_default] A,#[library_default] B, }", "exactly one library_default"},
		{"option-clone", "struct Bad { Value Option[chan int] }", "no explicit implementation for qualified method clone"},
		{"result-clone-inactive", "struct Bad { Value Result[int,chan int] }", "no explicit implementation for qualified method clone"},
		{"result-debug-inactive", "struct Bad { Value Result[int,chan int] }", "no explicit implementation for qualified method debug"},
		{"map-clone-defined-key", "type Key string\n#[derive(macros::LibraryClone)]\nstruct Bad { Value map[Key]int }", "no explicit implementation for qualified method clone"},
		{"map-clone-complex-key", "struct Bad { Value map[complex64]int }", "no explicit implementation for qualified method clone"},
		{"map-debug-integer-key", "struct Bad { Value map[int]int }", "no explicit implementation for qualified method debug"},
		{"map-debug-defined-key", "type Key string\n#[derive(macros::LibraryDebug)]\nstruct Bad { Value map[Key]int }", "no explicit implementation for qualified method debug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			protocolName := "Default"
			if strings.Contains(tc.name, "clone") {
				protocolName = "Clone"
			} else if strings.Contains(tc.name, "debug") {
				protocolName = "Debug"
			}
			source := "package model\nuse macros;\nuse api;\n"
			if !strings.HasPrefix(tc.body, "type Key") {
				source += "#[derive(macros::Library" + protocolName + ")]\n"
			}
			source += tc.body + "\nfunc Reject() {"
			if protocolName == "Default" {
				source += "_=api::Make[Bad]()"
			} else {
				source += "var value Bad;_=api::Library" + protocolName + "::" + strings.ToLower(protocolName) + "(&value)"
			}
			source += "}\n"
			writeProjectFile(t, model, "lib.gox", source)
			rejected, err := PrepareProject(model, Options{Offline: true, Lib: true, RunProcMacro: run})
			if rejected != nil || err == nil || !strings.Contains(err.Error(), "example.org/library-protocol-model/lib.gox:") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("rejection = (%v, %v), want nil result and %q", rejected, err, tc.want)
			}
		})
	}
}
