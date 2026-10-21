package compile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// These runtime traits use ordinary names deliberately: this checks the library
// path without the builtin derive registry. The existing Default recursion and
// derived Eq qualification rules remain outside this migration proof.
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
func main(){fmt.Println(model.Evaluate())}
`)
	writeProjectFile(t, app, "main_test.go", `package main
import("testing";model "example.org/library-protocol-model")
func TestOrdinaryProtocolEntry(t *testing.T) {
 const want="Record { first: 1, second: 2 } | Containers { values: [9, 2], fixed: [8, 4] } | true true"
 if got:=model.Evaluate();got!=want {t.Fatalf("protocol entry = %q, want %q",got,want)}
}
`)
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
	if deriveCalls != 9 {
		t.Fatalf("derive calls = %d, want 9", deriveCalls)
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
}
