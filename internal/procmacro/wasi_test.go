//go:build !js

package procmacro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

var wasiTestBudgetMultiplier time.Duration = 1

func TestWASIHostSDKCommand(t *testing.T) {
	t.Setenv("GOXIDE_HOST_SECRET", "must not reach the macro")
	wasm := buildWASIFixture(t, wasiSDKFixture)
	host := wasiHostForTest(t, context.Background())
	run := func(entry string) ([]byte, pm.Request, error) {
		t.Helper()
		request := wasiRequest(t, entry)
		output, err := host.Run(wasm, request)
		return output, request, err
	}
	for _, entry := range []string{"echo", "state"} {
		t.Run(entry, func(t *testing.T) {
			first, request, err := run(entry)
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := run(entry)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatalf("fresh instance changed deterministic output: %v\n%s\n%s", err, first, second)
			}
			output, err := pm.ReadResponse(bytes.NewReader(first), request)
			if err != nil {
				t.Fatal(err)
			}
			if entry == "echo" && strings.TrimSpace(output.Tokens.Display()) != "42" {
				t.Fatalf("echo = %s", output.Tokens.Display())
			}
		})
	}
	if len(host.compiled) != 1 {
		t.Fatalf("repeated invocations compiled %d artifacts", len(host.compiled))
	}
	t.Run("error_diagnostic_retains_wire_response", func(t *testing.T) {
		response, request, err := run("diagnostic")
		if err != nil {
			t.Fatal(err)
		}
		output, err := pm.ReadResponse(bytes.NewReader(response), request)
		var diagnostic *pm.DiagnosticError
		if !errors.As(err, &diagnostic) || len(output.Diagnostics) != 2 || output.Diagnostics[0].Severity != pm.Note || output.Diagnostics[1].Severity != pm.Error {
			t.Fatalf("diagnostics changed: %+v, %v", output.Diagnostics, err)
		}
	})
	for _, test := range []struct{ entry, message string }{
		{"stdout_limit", "stdout exceeds"},
		{"stderr_limit", "stderr exceeds"},
		{"diagnostic_stderr_limit", "diagnostics and stderr exceed"},
		{"trailing_output", "response"},
		{"unknown_reference", "response"},
		{"panic", "panic fixture"},
		{"exit", "exit_code(7)"},
	} {
		t.Run(test.entry, func(t *testing.T) {
			if _, _, err := run(test.entry); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Run(%s) error = %v; want %s", test.entry, err, test.message)
			}
			if _, _, err := run("echo"); err != nil {
				t.Fatalf("next invocation failed after rejected guest: %v", err)
			}
		})
	}
	t.Run("response_then_loop_is_not_success", func(t *testing.T) {
		previous := host.invocation
		host.invocation = 50 * time.Millisecond
		defer func() { host.invocation = previous }()
		if _, _, err := run("response_loop"); err == nil || !strings.Contains(err.Error(), "invocation time budget") {
			t.Fatalf("response followed by nontermination was accepted: %v", err)
		}
	})
}

func TestWASIHostAlternatingArtifactsReuseCompiledCode(t *testing.T) {
	request := wasiRequest(t, "echo")
	var response bytes.Buffer
	if err := pm.WriteResponse(&response, request, pm.Success(request.Input())); err != nil {
		t.Fatal(err)
	}
	// This tests cache identity and fresh instances without JIT-compiling a
	// second copy of the full Go SDK. The SDK command is covered separately.
	first := wasiResponseModule(response.Bytes())
	second := append(append([]byte(nil), first...), 0, 3, 1, 'x', 0)
	host := wasiHostForTest(t, context.Background())
	seen := map[[sha256.Size]byte]wasiCompiled{}
	for _, artifact := range [][]byte{first, second, first, second} {
		output, err := host.Run(artifact, request)
		if err != nil || !bytes.Equal(output, response.Bytes()) {
			t.Fatalf("fresh command response changed: %v\n%s", err, output)
		}
		prepared, err := PrepareWASM(artifact)
		if err != nil {
			t.Fatal(err)
		}
		id := sha256.Sum256(prepared)
		current, ok := host.compiled[id]
		if !ok {
			t.Fatal("successful artifact was not retained in the compiled cache")
		}
		if previous, ok := seen[id]; ok && previous.module != current.module {
			t.Fatal("alternating artifacts recompiled an already cached module")
		}
		seen[id] = current
	}
	if len(host.compiled) != 2 || len(host.order) != 2 || host.cacheBytes > MaxWASMBytes {
		t.Fatalf("alternating macro libraries lost compiled cache entries: %d", len(host.compiled))
	}
}

func TestWASIHostTimeoutCancellationAndCumulativeBudget(t *testing.T) {
	wasm := wasiLoopModule(false)
	request := wasiRequest(t, "loop")
	t.Run("invocation_and_cumulative", func(t *testing.T) {
		host := wasiHostForTest(t, context.Background())
		host.invocation, host.compilation = 30*time.Millisecond, 55*time.Millisecond
		if _, err := host.Run(wasm, request); err == nil || !strings.Contains(err.Error(), "invocation time budget") {
			t.Fatalf("first loop error = %v", err)
		}
		if _, err := host.Run(wasm, request); err == nil || !strings.Contains(err.Error(), "cumulative execution budget") {
			t.Fatalf("second loop error = %v", err)
		}
		if _, err := host.Run(wasm, request); err == nil || !strings.Contains(err.Error(), "cumulative execution budget") {
			t.Fatalf("exhausted budget accepted another instance: %v", err)
		}
	})
	t.Run("module_start", func(t *testing.T) {
		host := wasiHostForTest(t, context.Background())
		host.invocation = 20 * time.Millisecond
		if _, err := host.Run(wasiLoopModule(true), request); err == nil || !strings.Contains(err.Error(), "invocation time budget") {
			t.Fatalf("module initialization was outside the deadline: %v", err)
		}
	})
	t.Run("cancel_and_single_active_instance", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		host := wasiHostForTest(t, ctx)
		done := make(chan error, 1)
		go func() { _, err := host.Run(wasm, request); done <- err }()
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			host.mu.Lock()
			active := host.active
			host.mu.Unlock()
			if active {
				break
			}
			select {
			case err := <-done:
				t.Fatalf("loop returned before cancellation: %v", err)
			case <-deadline.C:
				t.Fatal("loop did not become active")
			case <-tick.C:
			}
		}
		if _, err := host.Run(wasm, request); err == nil || !strings.Contains(err.Error(), "active invocation") {
			t.Fatalf("concurrent invocation accepted: %v", err)
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost its cause: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancel did not stop guest execution")
		}
	})
}

func TestWASIHostRejectsImportBeforeInstantiation(t *testing.T) {
	data := []byte{2, 1}
	for _, name := range []string{"wasi_snapshot_preview1", "path_open"} {
		data = appendWASMU32(data, uint32(len(name)))
		data = append(data, name...)
	}
	data = append(data, 0, 0)
	wasm := wasmSections(data, []byte{4, 1, 0x70, 0, 0}, []byte{5, 1, 0, 1})
	host := wasiHostForTest(t, context.Background())
	if _, err := host.Run(wasm, wasiRequest(t, "bad")); err == nil || !strings.Contains(err.Error(), "path_open") {
		t.Fatalf("filesystem import error = %v", err)
	}
	if len(host.compiled) != 0 {
		t.Fatal("disallowed imports reached engine compilation")
	}
}

func TestWASIHostGoPackageInitIsBounded(t *testing.T) {
	wasm := buildWASIFixture(t, "package main\nfunc init() { for {} }\nfunc main() {}\n")
	host := wasiHostForTest(t, context.Background())
	// Allow cold engine compilation, but bound the Go runtime/package init too.
	host.invocation = 2 * time.Second
	if _, err := host.Run(wasm, wasiRequest(t, "init")); err == nil || !strings.Contains(err.Error(), "invocation time budget") {
		t.Fatalf("Go init was not bounded: %v", err)
	}
}

func wasiHostForTest(t *testing.T, ctx context.Context) *WASIHost {
	t.Helper()
	host, err := NewWASIHostWithBudgets(ctx, MaxInvocationTime*wasiTestBudgetMultiplier, MaxCompilationTime*wasiTestBudgetMultiplier)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	return host
}

func wasiRequest(t *testing.T, entry string) pm.Request {
	t.Helper()
	scope := token.NewScope("wasi-test-request")
	definition, call := scope.NewContext(), scope.NewContext()
	span, err := scope.NewSpan(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	input, err := token.ParseText("42", call)
	if err != nil {
		t.Fatal(err)
	}
	request, err := pm.NewRequest(scope, pm.RequestConfig{Entry: entry, Kind: pm.Function, Position: pm.ExpressionPosition, DefSite: definition, CallSite: call, DefSpan: span, CallSpan: span, Target: pm.Target{OS: "linux", Arch: "amd64"}, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func wasiLoopModule(start bool) []byte {
	sections := [][]byte{
		{1, 1, 0x60, 0, 0}, {3, 1, 0},
		{4, 1, 0x70, 0, 0}, {5, 1, 0, 1},
		{7, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0},
	}
	if start {
		sections = append(sections, []byte{8, 0})
	}
	sections = append(sections, []byte{10, 1, 7, 0, 3, 0x40, 0x0c, 0, 0x0b, 0x0b})
	return wasmSections(sections...)
}

func wasiResponseModule(response []byte) []byte {
	imports := []byte{2, 1}
	for _, name := range []string{"wasi_snapshot_preview1", "fd_write"} {
		imports = appendWASMU32(imports, uint32(len(name)))
		imports = append(imports, name...)
	}
	imports = append(imports, 0, 0)
	body := []byte{
		0,                            // No local variables.
		0x23, 0, 0x04, 0x40, 0, 0x0b, // Trap if the instance's global was already set.
		0x41, 1, 0x24, 0, // Set the mutable global, which must reset next invocation.
		0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, // fd_write(1, iovec=0, count=1, written=8).
		0x10, 0, 0x1a, 0x0b, // Call fd_write, discard errno and return.
	}
	code := appendWASMU32([]byte{10, 1}, uint32(len(body)))
	code = append(code, body...)
	payload := make([]byte, 16, 16+len(response))
	binary.LittleEndian.PutUint32(payload, 16)
	binary.LittleEndian.PutUint32(payload[4:], uint32(len(response)))
	payload = append(payload, response...)
	data := appendWASMU32([]byte{11, 1, 0, 0x41, 0, 0x0b}, uint32(len(payload)))
	data = append(data, payload...)
	return wasmSections(
		[]byte{1, 2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0},
		imports, []byte{3, 1, 1}, []byte{4, 1, 0x70, 0, 0}, []byte{5, 1, 0, 1},
		[]byte{6, 1, 0x7f, 1, 0x41, 0, 0x0b},
		[]byte{7, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1}, code, data,
	)
}

func buildWASIFixture(t *testing.T, source string) []byte {
	t.Helper()
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	command := exec.Command(tool, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", SDKModule)
	command.Env = wasiBuildEnvironment(false)
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manifest := fmt.Sprintf("module example.org/wasi-host-fixture\n\ngo 1.27\n\nrequire %s v0.0.0\nreplace %s => %s\n", SDKModule, SDKModule, filepath.ToSlash(strings.TrimSpace(string(data))))
	for name, source := range map[string]string{"go.mod": manifest, "main.go": source} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	artifact := filepath.Join(root, "guest.wasm")
	command = exec.Command(tool, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", artifact, ".")
	command.Dir, command.Env = root, wasiBuildEnvironment(true)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build SDK guest: %v\n%s", err, output)
	}
	data, err = os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wasiBuildEnvironment(guest bool) []string {
	var environment []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB":
			continue
		}
		environment = append(environment, value)
	}
	environment = append(environment, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off")
	if guest {
		environment = append(environment, "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	}
	return environment
}

const wasiSDKFixture = `package main
import (
 "bytes"
 "crypto/rand"
 "fmt"
 "os"
 "strconv"
 "strings"
 "time"
 pm "github.com/goxide-lang/syntax/proc_macro"
 "github.com/goxide-lang/syntax/token"
)
var initialized int
func init() { initialized++ }
func main() {
 request, err := pm.ReadRequest(os.Stdin)
 if err != nil { panic(err) }
 output := pm.Success(request.Input())
 switch request.Entry() {
 case "state":
  if initialized != 1 || len(os.Environ()) != 0 || len(os.Args) != 1 || os.Args[0] != "goxide-proc-macro" { panic("host state leaked") }
  var random [13]byte
  if _, err := rand.Read(random[:]); err != nil { panic(err) }
  before := time.Now()
  time.Sleep(time.Hour)
  if time.Since(before) < time.Hour { panic("virtual sleep did not advance clock") }
  text := strconv.Quote(fmt.Sprintf("%d/%x/%d", initialized, random, time.Now().UnixNano()))
  result, err := token.ParseText(text, request.Context().DefSite())
  if err != nil { panic(err) }; output = pm.Success(result)
 case "diagnostic":
  output.Diagnostics = []pm.Diagnostic{{Severity:pm.Note, Message:"before error"}, {Severity:pm.Error, Message:"expected error", Span:request.Context().CallSpan()}}
 case "stdout_limit":
  os.Stdout.Write(bytes.Repeat([]byte{'x'}, pm.MaxMessageBytes+1)); for {}
 case "stderr_limit":
  os.Stderr.Write(bytes.Repeat([]byte{'x'}, pm.MaxDiagnosticBytes+1)); for {}
 case "diagnostic_stderr_limit":
  os.Stderr.Write(bytes.Repeat([]byte{'x'}, 32768))
  output.Diagnostics = []pm.Diagnostic{{Severity:pm.Warning, Message:strings.Repeat("y", 40000)}}
 case "unknown_reference":
  fmt.Fprintf(os.Stdout, "{\"id\":%q,\"tokens\":[{\"ref\":4294967295}]}", request.ID()); return
 case "panic": panic("panic fixture")
 case "exit": os.Exit(7)
 }
 if err := pm.WriteResponse(os.Stdout, request, output); err != nil { panic(err) }
 if request.Entry() == "trailing_output" { fmt.Fprint(os.Stdout, "{}") }
 if request.Entry() == "response_loop" { for {} }
}
`
