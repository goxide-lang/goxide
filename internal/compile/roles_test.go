package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func compilerSDKForTest(t *testing.T) string {
	t.Helper()
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	command := exec.Command(tool, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", procmacro.SDKModule)
	command.Env = project.GoEnv(true)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := procmacro.ReadSDKIdentity(context.Background(), tool, ".", project.GoEnv(true))
	if err != nil {
		t.Fatal(err)
	}
	previous := procmacro.CompilerSDKIdentity
	procmacro.CompilerSDKIdentity = identity
	t.Cleanup(func() { procmacro.CompilerSDKIdentity = previous })
	return strings.TrimSpace(string(output))
}

// Node is only a test transport for trusted fixtures. Production expansion must
// supply the separately implemented restricted WASI host from RFC 0030.
func testWASI(t *testing.T, inspect func(pm.Request)) func([]byte, pm.Request) ([]byte, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to execute the WASI fixture")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "run.mjs")
	writeProjectFile(t, directory, "run.mjs", `import { readFile } from 'node:fs/promises';
import { WASI } from 'node:wasi';
const wasi = new WASI({version:'preview1', args:['goxide-proc-macro'], env:{}, preopens:{}, returnOnExit:true});
const module = await WebAssembly.compile(await readFile(process.argv[2]));
const instance = await WebAssembly.instantiate(module, wasi.getImportObject());
process.exitCode = wasi.start(instance);
`)
	return func(wasm []byte, request pm.Request) ([]byte, error) {
		if len(wasm) < 8 || !bytes.Equal(wasm[:8], []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}) {
			return nil, fmt.Errorf("macro artifact is not a WASM module")
		}
		if inspect != nil {
			inspect(request)
		}
		artifact := filepath.Join(directory, "macro.wasm")
		if err := os.WriteFile(artifact, wasm, 0o600); err != nil {
			return nil, err
		}
		var input, stderr bytes.Buffer
		if err := pm.WriteRequest(&input, request); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, node, "--no-warnings", script, artifact)
		command.Dir, command.Stdin, command.Stderr = directory, &input, &stderr
		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("test WASI command: %w: %s", err, stderr.String())
		}
		return output, nil
	}
}

func TestProceduralRoleBuildRuntimeRootAndLockedReplay(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	app := filepath.Join(root, "app")
	macros := filepath.Join(root, "macros")
	upstream := filepath.Join(root, "upstream")
	helpers := filepath.Join(root, "helpers")
	runtimeLib := filepath.Join(root, "runtime")
	writeProjectFile(t, upstream, "goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/upstream"
go="1.27"
[lib]
name="upstream"
path="."
proc_macro=true
[dependencies]
syntax={path=%q}
`, filepath.ToSlash(sdk)))
	writeProjectFile(t, upstream, "lib.gox", `package upstream
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
 if ctx.Target().OS != "wasip1" || ctx.Target().Arch != "wasm" {
  return pm::Failure("upstream received wrong target", ctx.CallSpan())
 }
 return pm::Success(input)
}

`)
	writeProjectFile(t, helpers, "goxide.toml", `[go_mod]
module="example.org/helpers"
go="1.27"
[lib]
name="helpers"
path="."
[features]
macro_mode=[]
`)
	writeProjectFile(t, helpers, "lib.gox", `package helpers
#[cfg(all(target_os("wasip1"),feature("macro_mode")))]
pub func Tail() string { return "::Value()" }
#[cfg(not(feature("macro_mode")))]
pub func Value() int { return 42 }
`)
	writeProjectFile(t, macros, "goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/macros"
go="1.27"
[lib]
name="macros"
path="src"
proc_macro=true
[dependencies]
syntax={path=%q}
helpers={path="../helpers",features=["macro_mode"]}
upstream={path="../upstream"}
`, filepath.ToSlash(sdk)))
	writeProjectFile(t, macros, "src/lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
use helpers;
use upstream;
mod local;
#[proc_macro]
pub func answer(ctx pm::Context, input token::TokenStream) pm::Output {
 if ctx.Target().OS == "wasip1" { return pm::Failure("consumer target was replaced by macro target", ctx.CallSpan()) }
 var root, err = ctx.RuntimeRoot("example.org/runtime")
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 var tail, parseErr = token::ParseText(local::Tail(), ctx.DefSite())
 if parseErr != nil { return pm::Failure(parseErr.Error(), ctx.CallSpan()) }
 return pm::Success(root.Append(tail.Tokens()...))
}
pub func Tail() string { return upstream::identity!(helpers::Tail()) }
`)
	writeProjectFile(t, macros, "src/local/lib.gox", `package local
use helpers;
pub func Tail() string { return helpers::Tail() }
`)
	writeProjectFile(t, runtimeLib, "goxide.toml", `[go_mod]
module="example.org/runtime"
go="1.27"
[lib]
name="runtime"
path="."
[dependencies]
macros={path="../macros"}
helpers={path="../helpers"}
`)
	writeProjectFile(t, runtimeLib, "lib.gox", `package runtime
pub use macros as macros;
use helpers;
pub func Value() int { return helpers::Value() }
`)
	writeProjectFile(t, app, "goxide.toml", `[go_mod]
module="example.org/app"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies]
api={path="../runtime"}
fmt={go="fmt"}
`)
	writeProjectFile(t, app, "main.gox", `package main
use api;
use fmt;
func main() { fmt::Println(api::macros::answer!()) }
`)
	writeProjectFile(t, app, "main_test.go", "package main\nimport \"testing\"\nfunc TestGenerated(t *testing.T) { main() }\n")
	var targets []pm.Target
	run := testWASI(t, func(request pm.Request) { targets = append(targets, request.Context().Target()) })
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	wasi, native := false, false
	for _, target := range targets {
		wasi = wasi || target == (pm.Target{OS: "wasip1", Arch: "wasm"})
		native = native || target == (pm.Target{OS: runtime.GOOS, Arch: runtime.GOARCH})
	}
	if !wasi || !native {
		t.Fatalf("role target snapshots = %#v", targets)
	}
	for _, operation := range []string{"build", "run", "test"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "program"))
		}
		command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "42\n" {
			t.Fatalf("generated %s: %v\n%s", operation, err, output)
		}
	}
	mod, err := os.ReadFile(filepath.Join(first.Root, "go.mod"))
	if err != nil || bytes.Contains(mod, []byte("github.com/goxide-lang/syntax")) || bytes.Contains(mod, []byte("example.org/macros")) || bytes.Contains(mod, []byte("example.org/upstream")) {
		t.Fatalf("compile-time dependency leaked into target module: %v\n%s", err, mod)
	}
	files := projectSnapshot(t, filepath.Dir(first.Root))
	lock, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(lock, &decoded); err != nil || len(decoded["roles"]) == 0 {
		t.Fatalf("role records missing from owner lock: %v\n%s", err, lock)
	}
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(files, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("same source changed the generated bundle")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed owner lock: %v", err)
	}
	for _, source := range []string{macros, upstream, helpers, runtimeLib} {
		for _, generated := range []string{"go.mod", "go.sum", "goxide.lock"} {
			if _, err := os.Stat(filepath.Join(source, generated)); !os.IsNotExist(err) {
				t.Fatalf("author dependency was modified: %s/%s: %v", source, generated, err)
			}
		}
	}
}

func TestSameProjectExampleUsesProcLib(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	writeProjectFile(t, root, "goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/self"
go="1.27"
[lib]
name="macros"
path="src"
proc_macro=true
[[example]]
name="demo"
path="examples/demo"
[dependencies]
syntax={path=%q}
fmt={go="fmt"}
`, filepath.ToSlash(sdk)))
	writeProjectFile(t, root, "src/lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output { return pm::Success(input) }
`)
	writeProjectFile(t, root, "examples/demo/main.gox", `package main
use macros;
use fmt;
func main() { fmt::Println(macros::identity!(42)) }
`)
	result, err := PrepareProject(root, Options{Example: "demo", Offline: true, RunProcMacro: testWASI(t, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProcMacro != "" || result.Kind != "example" || result.Config.Environment.Target.OS != runtime.GOOS {
		t.Fatalf("example acquired macro target identity: %#v", result)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", result.Entry)
	command.Dir, command.Env = result.Root, project.GoEnv(true)
	if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("same-project example: %v\n%s", err, output)
	}
}

func TestMacroBuildUsesAnotherSourceOfSameModule(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	first, second, app := filepath.Join(root, "first"), filepath.Join(root, "second"), filepath.Join(root, "app")
	writeProcBuildManifest(t, first, sdk, "")
	writeProcBuildManifest(t, second, sdk, `upstream={path="../first"}`)
	writeProjectFile(t, first, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output { return pm::Success(input) }
`)
	writeProjectFile(t, second, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
use upstream;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
 return upstream::identity!(pm::Success(input))
}
`)
	writeProjectFile(t, app, "goxide.toml", `[go_mod]
module="example.org/app"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies]
macros={path="../second"}
fmt={go="fmt"}
`)
	writeProjectFile(t, app, "main.gox", `package main
use macros;
use fmt;
func main() { fmt::Println(macros::identity!(42)) }
`)
	result, err := PrepareProject(app, Options{Offline: true, RunProcMacro: testWASI(t, nil)})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", result.Entry)
	command.Dir, command.Env = result.Root, project.GoEnv(true)
	if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("same module in independent plans: %v\n%s", err, output)
	}
}
