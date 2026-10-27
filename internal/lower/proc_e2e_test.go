package lower_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/typecheck"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// The native command is a test transport for the source/SDK/expansion contract.
// Production compilation must supply the separately implemented WASI host.
func TestRegisteredProceduralLibraryProgram(t *testing.T) {
	directory := t.TempDir()
	sdk := strings.TrimSpace(runGo(t, ".", "list", "-m", "-f", "{{.Dir}}", "github.com/goxide-lang/syntax"))
	producer := filepath.Join(directory, "producer")
	writeDiagnosticModuleFile(t, producer, "go.mod", []byte(fmt.Sprintf("module example.org/answer_macros\n\ngo 1.27\n\nrequire github.com/goxide-lang/syntax v0.0.0\nreplace github.com/goxide-lang/syntax => %q\n", filepath.ToSlash(sdk))))
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "procedural", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	sourceName := filepath.Join(producer, "macros.gox")
	configuration := testConfig(t)
	importer := typecheck.NewImporter(sourceName, typecheck.ImporterOptions{Config: configuration})
	roots := paths.Roots{"syntax": {GoPrefix: "github.com/goxide-lang/syntax", ModulePath: "github.com/goxide-lang/syntax", Source: "module"}}
	sources := map[string][]byte{sourceName: read("macros.gox")}
	generated, library, err := lower.CompileProcLibrary(sources, "example.org/answer_macros", importer, roots, lower.PackageOptions{Config: configuration})
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := lower.CompileProcLibrary(sources, "example.org/answer_macros", importer, roots, lower.PackageOptions{Config: configuration})
	if err != nil || !reflect.DeepEqual(generated, again) {
		t.Fatalf("producer generation is not stable: %v", err)
	}
	writeDiagnosticModuleFile(t, producer, "macros_gox.go", generated[sourceName])
	var launcher strings.Builder
	fmt.Fprintln(&launcher, `package main
import (
  "fmt"
  "os"
  macros "example.org/answer_macros"
  pm "github.com/goxide-lang/syntax/proc_macro"
)
func run() error {
  request, err := pm.ReadRequest(os.Stdin)
  if err != nil { return err }
  output, err := pm.Dispatch(request, []pm.Entry{`)
	fields := map[pm.Kind]string{pm.Function: "Function", pm.Attribute: "Attribute", pm.Derive: "Derive"}
	for _, entry := range library.Entries {
		fmt.Fprintf(&launcher, "{Name:%q,%s:macros.%s},\n", entry.Name, fields[entry.Kind], library.GoNames[entry.Name])
	}
	fmt.Fprintln(&launcher, `})
  if err != nil { return err }
  return pm.WriteResponse(os.Stdout, request, output)
}
func main() { if err := run(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) } }`)
	writeDiagnosticModuleFile(t, producer, "cmd/guest/main.go", []byte(launcher.String()))
	guest := filepath.Join(directory, "macro-command")
	runGo(t, producer, "build", "-o", guest, "./cmd/guest")
	var requests, responses [][]byte
	library.Run = func(request pm.Request) ([]byte, error) {
		var input bytes.Buffer
		if err := pm.WriteRequest(&input, request); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, guest)
		cmd.Dir, cmd.Stdin = producer, bytes.NewReader(input.Bytes())
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("test macro command: %w: %s", err, stderr.String())
		}
		requests = append(requests, append([]byte(nil), input.Bytes()...))
		responses = append(responses, append([]byte(nil), output...))
		return output, nil
	}
	consumer := filepath.Join(directory, "consumer")
	writeDiagnosticModuleFile(t, consumer, "go.mod", []byte("module example.org/consumer\n\ngo 1.27\n"))
	mainName := filepath.Join(consumer, "main.gox")
	consumerImporter := typecheck.NewImporter(mainName, typecheck.ImporterOptions{Config: configuration})
	consumerRoots := paths.Roots{"fmt": {GoPrefix: "fmt", PackagePath: "fmt", Source: "toolchain"}, "macros": {PackagePath: library.PackagePath, ModulePath: library.PackagePath, Source: "path"}}
	generate := func() map[string][]byte {
		t.Helper()
		output, err := lower.CompilePackage(map[string][]byte{mainName: read("main.gox")}, "example.org/consumer", consumerImporter, consumerRoots, lower.PackageOptions{Config: configuration, ProcLibraries: []*analysis.ProcLibrary{library}})
		if err != nil {
			t.Fatal(err)
		}
		return output
	}
	output, second := generate(), generate()
	if !reflect.DeepEqual(output, second) {
		t.Fatal("consumer generation is not stable")
	}
	if len(requests) != 6 || !reflect.DeepEqual(requests[:3], requests[3:]) || !reflect.DeepEqual(responses[:3], responses[3:]) {
		t.Fatalf("three-entry request/response sequence differs across runs: %d", len(requests))
	}
	if len(output) != 1 {
		t.Fatalf("producer leaked into consumer outputs: %v", output)
	}
	if bytes.Contains(output[mainName], []byte("github.com/goxide-lang/syntax")) || bytes.Contains(output[mainName], []byte(library.PackagePath)) {
		t.Fatalf("macro runtime leaked into generated program:\n%s", output[mainName])
	}
	t.Logf("generated consumer:\n%s", output[mainName])
	writeDiagnosticModuleFile(t, consumer, "main_gox.go", output[mainName])
	writeDiagnosticModuleFile(t, consumer, "main_test.go", []byte(`package main
import "testing"
func TestPublicGeneratedAPI(t *testing.T) {
  record := Record{Value: Answer()}
  if record.Value != 42 { t.Fatal(record.Value) }
}
`))
	runGo(t, consumer, "build", "-o", filepath.Join(directory, "program"), ".")
	if got := runGo(t, consumer, "run", "."); got != "42 1 42\n" {
		t.Fatalf("got %q", got)
	}
	runGo(t, consumer, "test", "./...")
}
