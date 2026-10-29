package compile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// The macro and runtime traits are ordinary dependencies. This exercises the
// migration path with pinned serde/JSON backends, rather than builtin derives.
func TestProceduralSerdeLibraryProtocolsGoConsumer(t *testing.T) {
	sdk := compilerSDKForTest(t)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	module, err := project.ReadMod(tool, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{}
	for _, dependency := range module.Require {
		versions[dependency.Path] = dependency.Version
	}
	if versions["github.com/goxide-lang/serde"] == "" || versions["github.com/goxide-lang/std"] == "" {
		t.Fatal("compiler must pin serde and std")
	}
	root := t.TempDir()
	macros, codecs := filepath.Join(root, "macros"), filepath.Join(root, "codecs")
	leaf, model, app := filepath.Join(root, "leaf"), filepath.Join(root, "model"), filepath.Join(root, "app")
	fixture := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "proc_serde_protocols", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro_derive(LibrarySerialize, attributes(serde))]
pub func librarySerialize(ctx pm::Context, input token::TokenStream) pm::Output { return deriveSerde(ctx,input,true) }
#[proc_macro_derive(LibraryDeserialize, attributes(serde))]
pub func libraryDeserialize(ctx pm::Context, input token::TokenStream) pm::Output { return deriveSerde(ctx,input,false) }
`)
	writeProjectFile(t, macros, "derive.go", fixture("derive.go"))
	writeProjectFile(t, codecs, project.ManifestName, fmt.Sprintf(`[go_mod]
module="example.org/serde-codecs"
go="1.27"
[lib]
name="codecs"
path="."
[dependencies.serde]
go="github.com/goxide-lang/serde"
version=%q
[dependencies.standard]
go="github.com/goxide-lang/std"
version=%q
`, versions["github.com/goxide-lang/serde"], versions["github.com/goxide-lang/std"]))
	writeProjectFile(t, codecs, "lib.gox", fixture("protocol.gox"))
	for _, fixture := range []struct{ root, name string }{{leaf, "leaf"}, {model, "model"}} {
		extra := ""
		if fixture.name == "model" {
			extra = "[dependencies.leaf]\npath=\"../leaf\"\n"
		}
		writeProjectFile(t, fixture.root, project.ManifestName, fmt.Sprintf(`[go_mod]
module="example.org/serde-%s"
go="1.27"
[lib]
name=%q
path="."
[dependencies.macros]
path="../macros"
[dependencies.codecs]
path="../codecs"
[dependencies.serde]
go="github.com/goxide-lang/serde"
version=%q
[dependencies.standard]
go="github.com/goxide-lang/std"
version=%q
%s`, fixture.name, fixture.name, versions["github.com/goxide-lang/serde"], versions["github.com/goxide-lang/std"], extra))
	}
	writeProjectFile(t, leaf, "lib.gox", `package leaf
use macros;
use codecs;
#[derive(macros::LibrarySerialize,macros::LibraryDeserialize)]
pub struct Packet[T any] { pub Values []T; pub Pair [2]T }
`)
	writeProjectFile(t, model, "lib.gox", `package model
use macros;
use codecs;
use leaf;
type Amount = int
type Optional = Option[int]
#[derive(macros::LibrarySerialize,macros::LibraryDeserialize)]
pub enum Event { Idle, Counts([3]int) }
#[derive(macros::LibrarySerialize,macros::LibraryDeserialize)]
pub struct Envelope {
 #[serde(rename="packet")]
 pub Packet leaf::Packet[int]
 pub Event Event
 pub Optional Optional
 pub Amount Amount
}
`)
	writeProjectFile(t, model, "defaults.gox", fixture("defaults.gox"))
	writeProjectFile(t, app, project.ManifestName, fmt.Sprintf(`[go_mod]
module="example.org/serde-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.model]
path="../model"
[dependencies.serde]
go="github.com/goxide-lang/serde"
version=%q
`, versions["github.com/goxide-lang/serde"]))
	writeProjectFile(t, app, "main.go", fixture("consumer.go"))
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
	if deriveCalls != 11 {
		t.Fatalf("ordinary library derive calls = %d, want 11", deriveCalls)
	}
	for _, operation := range []string{"build", "run"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "ordinary serde protocols passed\n" {
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
		t.Fatal("locked replay changed ordinary serde artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed dependency lock: %v", err)
	}
}
