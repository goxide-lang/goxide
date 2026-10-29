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

func TestProceduralIdentityAttributeGenericFunctionGoConsumer(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	macros, app := filepath.Join(root, "macros"), filepath.Join(root, "app")
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro_attribute]
pub func identity(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output {
 return pm::Success(item)
}
`)
	writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/identity-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.macros]
path="../macros"
`)
	writeProjectFile(t, app, "lib.gox", `package main
use macros;
#[macros::identity]
#[macros::identity]
pub func Echo[T any](value T) T {
 local := value
 closure := func(inner T) T { return inner }
 return closure(local)
}
`)
	writeProjectFile(t, app, "main.go", `package main
import "fmt"
func main() { fmt.Println(Echo(7), Echo("ok")) }
`)
	attributeCalls := 0
	run := testWASI(t, func(request pm.Request) {
		if request.Kind() == pm.Attribute {
			attributeCalls++
		}
	})
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if attributeCalls != 2 {
		t.Fatalf("identity attribute executions = %d, want 2", attributeCalls)
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, operation := range []string{"build", "run"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "7 ok\n" {
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
		t.Fatal("locked replay changed the generated attribute function or procedural artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed dependency lock: %v", err)
	}
}
