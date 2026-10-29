package compile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

// Ordinary dependency macros format through ordinary explicit traits. Their
// generated statements retain the caller's argument expressions and frame.
func TestProceduralPrintLibraryProtocolsMigration(t *testing.T) {
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
		t.Fatal("compiler must pin std")
	}
	root := t.TempDir()
	macros, protocol := filepath.Join(root, "macros"), filepath.Join(root, "protocol")
	model, app := filepath.Join(root, "model"), filepath.Join(root, "app")
	fixture := func(destination, name, source string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "proc_print_protocols", source))
		if err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, destination, name, string(data))
	}
	writeProcBuildManifest(t, macros, sdk, "")
	fixture(macros, "lib.gox", "macros.gox")
	fixture(macros, "macro.go", "macro.go")
	writeProjectFile(t, protocol, project.ManifestName, fmt.Sprintf(`[go_mod]
module="example.org/printing-protocols"
go="1.27"
[lib]
name="protocol"
path="."
[dependencies.standard]
go="github.com/goxide-lang/std"
version=%q
`, stdVersion))
	fixture(protocol, "lib.gox", "protocol.gox")
	writeProjectFile(t, model, project.ManifestName, fmt.Sprintf(`[go_mod]
module="example.org/printing-model"
go="1.27"
[lib]
name="model"
path="."
[dependencies.macros]
path="../macros"
[dependencies.formatters]
path="../protocol"
[dependencies.standard]
go="github.com/goxide-lang/std"
version=%q
`, stdVersion))
	fixture(model, "lib.gox", "model.gox")
	writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/printing-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.model]
path="../model"
`)
	fixture(app, "main.go", "consumer.go")
	fixture(app, "main_test.go", "consumer_test.go")
	deriveCalls, functionCalls := 0, 0
	var artifact []byte
	wasi := testWASI(t, nil)
	run := func(wasm []byte, request pm.Request) ([]byte, error) {
		if request.Kind() == pm.Derive && request.Entry() == "LibraryDebug" {
			deriveCalls++
		}
		if request.Kind() == pm.Function && (request.Entry() == "LibraryPrint" || request.Entry() == "LibraryPrintln") {
			functionCalls++
			artifact = wasm
		}
		return wasi(wasm, request)
	}
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if deriveCalls != 2 || functionCalls != 20 {
		t.Fatalf("ordinary macro calls: derive = %d, function = %d, want 2 / 20", deriveCalls, functionCalls)
	}
	for _, operation := range []string{"build", "run", "test"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "prefix {7} \"quoted\" true\nordinary print protocols passed\n" {
			t.Fatalf("ordinary print consumer %s: %v\n%s", operation, err, output)
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
		t.Fatal("locked replay changed ordinary print artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed dependency lock: %v", err)
	}

	// Invalid grammar still executes the same compiled WASI artifact. It fails
	// before needing a runtime root, and never enters the compiler's print path.
	for _, tc := range []struct{ name, entry, source, want string }{
		{"missing-format", "LibraryPrint", ``, "requires a format string literal"},
		{"dynamic-format", "LibraryPrintln", `message, 1`, "format must be a string literal"},
		{"unmatched-open", "LibraryPrintln", `"{"`, "unsupported or unmatched format brace"},
		{"unmatched-close", "LibraryPrintln", `"}"`, "unsupported or unmatched format brace"},
		{"indexed", "LibraryPrintln", `"{0}", 1`, "unsupported or unmatched format brace"},
		{"precision", "LibraryPrintln", `"{:.2}", 1`, "unsupported or unmatched format brace"},
		{"named", "LibraryPrintln", `"{name}", 1`, "unsupported or unmatched format brace"},
		{"missing-value", "LibraryPrintln", `"{}"`, "1 placeholders but 0 arguments"},
		{"extra-value", "LibraryPrintln", `"", 1`, "0 placeholders but 1 arguments"},
		{"empty-value", "LibraryPrintln", `"{}", , 1`, "empty format argument"},
		{"variadic", "LibraryPrintln", `"{}", values...`, "variadic print arguments are unsupported"},
		{"semicolon", "LibraryPrintln", `"{}", 1; sideEffect()`, "top-level semicolons"},
		{"newline", "LibraryPrintln", "\"{}\", 1\nsideEffect()", "top-level semicolons"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := token.NewScope("print-" + tc.name)
			def, call := scope.NewContext(), scope.NewContext()
			span, err := scope.NewSpan(0, uint32(len(tc.source)))
			if err != nil {
				t.Fatal(err)
			}
			input, err := token.ParseText(tc.source, call)
			if err != nil {
				t.Fatal(err)
			}
			nodes := input.Tokens()
			// ParseText adds an EOF semicolon, outside a macro's argument delimiters.
			if len(nodes) > 0 && nodes[len(nodes)-1].Kind() == token.SemicolonKind && nodes[len(nodes)-1].Implicit() {
				nodes = nodes[:len(nodes)-1]
			}
			request, err := pm.NewRequest(scope, pm.RequestConfig{Entry: tc.entry, Kind: pm.Function, Position: pm.StatementsPosition, DefSite: def, CallSite: call, CallSpan: span, Target: pm.Target{OS: runtime.GOOS, Arch: runtime.GOARCH}, Input: token.New(nodes...)})
			if err != nil {
				t.Fatal(err)
			}
			response, err := wasi(artifact, request)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pm.ReadResponse(bytes.NewReader(response), request)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("format diagnostic = %v, want %q", err, tc.want)
			}
		})
	}

	// Reuse the producer and dependencies while changing the consumer source, so
	// these failures must come from ordinary checking or output-slot parsing.
	for _, tc := range []struct{ name, declarations, body, want string }{
		{"debug-no-stringer", `type OnlyStringer struct{}; func (OnlyStringer) String() string {return "fallback"}; func DebugOnlyStringer(*OnlyStringer) string {return "fake"}`, `macros::LibraryPrintln!("{:?}", OnlyStringer{})`, "does not explicitly implement"},
		{"display-no-stringer", `type OnlyStringer struct{}; func (OnlyStringer) String() string {return "fallback"}`, `macros::LibraryPrintln!("{}", OnlyStringer{})`, "does not explicitly implement"},
		{"debug-no-display", `type DisplayOnly int; impl formatters::LibraryDisplay for DisplayOnly {func display(self &Self) string {return "display"}}`, `macros::LibraryPrintln!("{:?}", DisplayOnly(2))`, "does not explicitly implement"},
		{"expression", "", `_ = macros::LibraryPrintln!("x")`, "expected expression"},
		{"go", "", `go macros::LibraryPrintln!("x")`, "expected expression"},
		{"defer", "", `defer macros::LibraryPrintln!("x")`, "expected expression"},
		{"if-init", "", `if macros::LibraryPrintln!("x"); true {}`, "exactly one statement"},
		{"for-post", "", `for ; false; macros::LibraryPrintln!("x") {}`, "exactly one statement"},
		{"label", "", `label: macros::LibraryPrintln!("x"); goto label`, "exactly one statement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeProjectFile(t, model, "lib.gox", "package model\nuse macros;\nuse formatters;\n"+tc.declarations+"\npub func Bad(){"+tc.body+"}\n")
			_, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ordinary print diagnostic = %v, want %q", err, tc.want)
			}
		})
	}
}
