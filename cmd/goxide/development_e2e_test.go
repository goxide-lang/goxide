package main

import (
	"bufio"
	"bytes"
	"context"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func developmentFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"goxide.toml":          "[go_mod]\nmodule='example.org/development'\ngo='1.27'\n[lib]\nname='app'\npath='.'\n[[bin]]\nname='main'\npath='cmd/main'\n[dependencies]\ntesting={go='testing'}\n",
		"lib.gox":              "package app\npub mod value\n",
		"value/value.gox":      "package value\npub func Answer() int { return 42 }\n",
		"value/value_test.gox": "package value\nuse testing\nfunc TestPass(t *testing::T) {\n    if Answer() != 42 { t.Fatal(\"wrong answer\") }\n}\nfunc TestFail(t *testing::T) {\n    t.Fatal(\"expected failure\")\n}\n",
		"cmd/main/main.gox":    "package main\nuse app::value\nfunc main() {\n    answer := value::Answer()\n    println(answer)\n}\n",
	}
	for name, text := range files {
		name = filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDevelopmentRunDiscoveryAndFailureLocation(t *testing.T) {
	root := developmentFixture(t)
	var out, stderr bytes.Buffer
	if err := run([]string{"run", "--bin", "main", root}, &out, &stderr); err != nil {
		t.Fatalf("run: %v\n%s", err, &stderr)
	}
	if strings.TrimSpace(stderr.String()) != "42" {
		t.Fatalf("multi-package run: stdout %q stderr %q", &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	if err := run([]string{"dev", "tests", "--bin", "main", root}, &out, &stderr); err != nil {
		t.Fatalf("discover: %v\n%s", err, &stderr)
	}
	var tests []developmentTest
	if err := json.Unmarshal(out.Bytes(), &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) != 2 || tests[0].Name != "TestFail" || tests[0].File != filepath.Join(root, "value/value_test.gox") || tests[0].Line != 6 {
		t.Fatalf("test discovery lost author positions: %+v", tests)
	}
	for _, name := range []string{"TestPass", "TestFail"} {
		out.Reset()
		stderr.Reset()
		err := run([]string{"dev", "test", "--bin", "main", "--package", tests[0].Package, "--run", "^" + name + "$", root}, &out, &stderr)
		if (err != nil) != (name == "TestFail") {
			t.Fatalf("%s: %v\n%s\n%s", name, err, &out, &stderr)
		}
		if name == "TestFail" {
			location := false
			decoder := json.NewDecoder(&out)
			for {
				var event struct {
					Output   string
					Location developmentLocation
				}
				if err := decoder.Decode(&event); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(event.Output, "expected failure") && event.Location.File == filepath.Join(root, "value/value_test.gox") && event.Location.Line == 7 {
					location = true
				}
			}
			if !location {
				t.Fatal("failure did not identify its original .gox line")
			}
		}
	}
}

func TestDevelopmentNativePrivateAssets(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(root, "application")
	files := map[string]string{
		"dependency/go.mod":                   "module example.org/dependency\ngo 1.27\n",
		"dependency/value.go":                 "package dependency\nfunc Value() int { return 21 }\n",
		"application/go.mod":                  "module example.org/native\ngo 1.27\nrequire example.org/dependency v0.0.0\nreplace example.org/dependency => ../dependency\n",
		"application/internal/value/value.go": "package value\nimport \"example.org/dependency\"\nfunc Answer() int { return dependency.Value()*2 }\n",
		"application/value.gox":               "package main\nfunc Answer() int { return 42 }\n",
		"application/main.go":                 "package main\nimport (\"embed\"; \"fmt\"; \"example.org/native/internal/value\")\n//go:embed data/message.txt\nvar resources embed.FS\nfunc main() { text,_:=resources.ReadFile(\"data/message.txt\"); fmt.Printf(\"%s %d %d\\n\", text, value.Answer(), Answer()) }\n",
		"application/data/message.txt":        "embedded",
	}
	for name, text := range files {
		name = filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	view, err := prepareDevelopment(t.Context(), developmentRequest{Input: module}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	private := view.Root
	command := view.command(t.Context(), "run", "-mod=readonly", view.Entry)
	output, runErr := command.CombinedOutput()
	if runErr != nil || string(output) != "embedded 42 42\n" {
		t.Fatalf("private native run: %v\n%s", runErr, output)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("private files retained: %v", err)
	}
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != want {
			t.Fatalf("author file changed: %s: %v", name, err)
		}
	}
}

func TestDevelopmentDiscoversRunnableGoTests(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":   "module example.org/native_tests\ngo 1.27\n",
		"value.go": "package value\nfunc Answer() int { return 42 }\n",
		"value_test.go": `package value
import "testing"
func TestMain(t *testing.T) {}
func TesticularHelper() {}
func Example() {
    // Output:
}
func Example_unchecked() {}
func BenchmarkAnswer(b *testing.B) {}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	view, err := prepareDevelopment(t.Context(), developmentRequest{Input: root}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	tests, err := view.Tests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 2 || tests[0].Name != "Example" || tests[1].Name != "TestMain" {
		t.Fatalf("discovery includes non-executable examples or misses an ordinary TestMain: %+v", tests)
	}
	var output, stderr bytes.Buffer
	if err := view.Test(t.Context(), tests[0].Package, "^(Example|TestMain)$", &output, &stderr); err != nil {
		t.Fatalf("discovered tests did not run: %v\nstdout:\n%s\nstderr:\n%s", err, &output, &stderr)
	}
	passed := map[string]bool{}
	decoder := json.NewDecoder(&output)
	for {
		var event struct{ Action, Test string }
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("test stdout is not a JSON event stream: %v\nstderr:\n%s", err, &stderr)
		}
		if event.Action == "pass" && event.Test != "" {
			passed[event.Test] = true
		}
	}
	if len(passed) != 2 || !passed["Example"] || !passed["TestMain"] {
		t.Fatalf("discovered tests did not both report PASS: %v\nstderr:\n%s", passed, &stderr)
	}
}

func TestDevelopmentDebugSourceLines(t *testing.T) {
	root := developmentFixture(t)
	view, err := prepareDevelopment(t.Context(), developmentRequest{Input: root}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	binary := filepath.Join(t.TempDir(), "program")
	var log bytes.Buffer
	if err := view.buildDebug(t.Context(), "", false, binary, &log); err != nil {
		t.Fatalf("debug build: %v\n%s", err, &log)
	}
	var data *dwarf.Data
	switch runtime.GOOS {
	case "darwin":
		file, openErr := macho.Open(binary)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		data, err = file.DWARF()
	case "windows":
		file, openErr := pe.Open(binary)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		data, err = file.DWARF()
	default:
		file, openErr := elf.Open(binary)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		data, err = file.DWARF()
	}
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]int{filepath.Join(root, "cmd/main/main.gox"): 5, filepath.Join(root, "value/value.gox"): 2}
	reader := data.Reader()
	for {
		unit, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if unit == nil {
			break
		}
		if unit.Tag != dwarf.TagCompileUnit {
			continue
		}
		lines, err := data.LineReader(unit)
		if err != nil {
			t.Fatal(err)
		}
		if lines == nil {
			continue
		}
		for {
			var line dwarf.LineEntry
			if err := lines.Next(&line); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if line.File != nil && line.Line == wanted[line.File.Name] && line.Address != 0 {
				delete(wanted, line.File.Name)
			}
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("debug executable lacks original .gox instruction locations: %v", wanted)
	}
}

func TestDevelopmentNamedFileKeepsAssetsAndExcludesSiblings(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%v", native), func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"goxide.roots.toml": "[roots.embed]\npackage='embed'\nsource='toolchain'\n",
				"main.gox":          "package main\nuse embed as _\n//go:embed message.txt\nvar message string\nfunc main() { println(message) }\n",
				"message.txt":       "selected asset",
				"sibling.gox":       "invalid sibling must remain unselected",
				"sibling.go":        "invalid native sibling must remain unselected",
			}
			if native {
				files["go.mod"] = "module example.org/selected\ngo 1.27\n"
			}
			for name, text := range files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			view, err := prepareDevelopment(t.Context(), developmentRequest{Input: filepath.Join(root, "main.gox")}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			binary := filepath.Join(t.TempDir(), "program")
			var log bytes.Buffer
			if err := view.buildDebug(t.Context(), "", false, binary, &log); err != nil {
				t.Fatalf("named-file debug build: %v\n%s", err, &log)
			}
			output, err := exec.CommandContext(t.Context(), binary).CombinedOutput()
			if err != nil || string(output) != "selected asset\n" {
				t.Fatalf("named-file binary: %v\n%s", err, output)
			}
			if _, err := os.Stat(view.Virtual); !os.IsNotExist(err) {
				t.Fatalf("wrote virtual source into author's directory: %v", err)
			}
			for name, expected := range files {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(data) != expected {
					t.Fatalf("changed %s: %v", name, err)
				}
			}
		})
	}
}

func TestDevelopmentDelveBreakpoint(t *testing.T) {
	testDevelopmentDelve(t, false)
}

func TestDevelopmentDelveNoDebug(t *testing.T) {
	testDevelopmentDelve(t, true)
}

func testDevelopmentDelve(t *testing.T, noDebug bool) {
	t.Helper()
	dlv := os.Getenv("GOXIDE_TEST_DLV")
	if dlv == "" {
		t.Skip("set GOXIDE_TEST_DLV to run a real Delve DAP breakpoint")
	}
	root := developmentFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	input, send := io.Pipe()
	receive, output := io.Pipe()
	defer input.Close()
	defer send.Close()
	defer receive.Close()
	defer output.Close()
	finished := make(chan error, 1)
	go func() {
		err := debugDevelopment(ctx, developmentRequest{Input: root, Delve: dlv}, input, output, io.Discard)
		_ = output.CloseWithError(err)
		finished <- err
	}()
	frames := make(chan map[string]any, 16)
	errors := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(receive)
		for {
			data, err := developmentReadDAP(reader)
			if err != nil {
				errors <- err
				return
			}
			var frame map[string]any
			if err := json.Unmarshal(data, &frame); err != nil {
				errors <- err
				return
			}
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	seq := 0
	request := func(command string, args any) int {
		t.Helper()
		seq++
		t.Logf("DAP request %d %s", seq, command)
		data, err := json.Marshal(map[string]any{"seq": seq, "type": "request", "command": command, "arguments": args})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(send, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	var observed []map[string]any
	await := func(kind, value string) map[string]any {
		t.Helper()
		for {
			select {
			case frame := <-frames:
				observed = append(observed, frame)
				t.Logf("DAP received: %+v", frame)
				if frame["type"] == "response" && frame["success"] == false {
					t.Fatalf("DAP rejected request: %+v", frame)
				}
				if frame[kind] == value {
					return frame
				}
			case err := <-errors:
				t.Fatalf("DAP ended: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	request("initialize", map[string]any{"adapterID": "goxide", "pathFormat": "path", "linesStartAt1": true, "columnsStartAt1": true})
	await("command", "initialize")
	request("launch", map[string]any{"stopOnEntry": false, "noDebug": noDebug})
	if noDebug {
		await("command", "launch")
		exited := await("event", "exited")
		var programOutput strings.Builder
		for _, event := range observed {
			if event["event"] == "output" {
				body := event["body"].(map[string]any)
				if body["category"] == "stdout" || body["category"] == "stderr" {
					programOutput.WriteString(body["output"].(string))
				}
			}
		}
		if programOutput.String() != "42\n" {
			t.Fatalf("debug adapter output = %q", &programOutput)
		}
		if exited["body"].(map[string]any)["exitCode"] != float64(0) {
			t.Fatalf("debug adapter program failed: %+v", exited)
		}
		request("disconnect", map[string]any{})
		await("command", "disconnect")
		_ = send.Close()
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return
	}
	await("event", "initialized")
	request("setBreakpoints", map[string]any{"source": map[string]any{"path": filepath.Join(root, "cmd/main/main.gox")}, "breakpoints": []any{map[string]any{"line": 5}}})
	response := await("command", "setBreakpoints")
	breakpoints := response["body"].(map[string]any)["breakpoints"].([]any)
	if len(breakpoints) != 1 || breakpoints[0].(map[string]any)["verified"] != true {
		t.Fatalf(".gox breakpoint not verified: %+v", response)
	}
	request("configurationDone", map[string]any{})
	stopped := await("event", "stopped")
	thread := stopped["body"].(map[string]any)["threadId"]
	request("stackTrace", map[string]any{"threadId": thread})
	stack := await("command", "stackTrace")
	frame := stack["body"].(map[string]any)["stackFrames"].([]any)[0].(map[string]any)
	if frame["source"].(map[string]any)["path"] != filepath.Join(root, "cmd/main/main.gox") || frame["line"] != float64(5) {
		t.Fatalf("stack did not map to original source: %+v", frame)
	}
	request("evaluate", map[string]any{"expression": "answer", "frameId": frame["id"], "context": "watch"})
	evaluated := await("command", "evaluate")
	if evaluated["body"].(map[string]any)["result"] != "42" {
		t.Fatalf("debug variable = %+v", evaluated)
	}
	request("disconnect", map[string]any{"terminateDebuggee": true})
	await("command", "disconnect")
	_ = send.Close()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
