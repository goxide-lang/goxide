package compile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestEditorPrepareIgnoresWriterLeases(t *testing.T) {
	root := editorCanonicalPath(t, smallProject(t))
	shared, locks := editorHoldWriterLeases(t, root)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	prepared, err := Prepare(ctx, root, Options{Offline: true, Editor: true})
	if err != nil {
		t.Fatalf("editor preparation competed with a CLI writer: %v", err)
	}
	defer prepared.Close()
	private := prepared.layoutRoot
	if private == shared || within(shared, private) || within(root, private) {
		t.Fatalf("editor preparation did not use an independent temporary root: %s", private)
	}
	snapshot, err := prepared.Snapshot(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := analysis.Analyze(snapshot.Context, snapshot.Input)
	if err != nil {
		snapshot.Close()
		t.Fatal(err)
	}
	generated, err := lower.Generate(snapshot.Context, result, lower.GenerationOptions{})
	snapshot.Close()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, data := range generated {
		found = found || strings.Contains(string(data), `Println("file target")`)
	}
	if !found {
		t.Fatalf("prepared editor input did not generate the selected program: %q", generated)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	editorAssertLeases(t, locks)
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("editor Close retained its private root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("editor preparation published the dependency lock: %v", err)
	}
}

func TestEditorPlanningDoesNotBlockCLIPublish(t *testing.T) {
	root := editorCanonicalPath(t, smallProject(t))
	temporary := editorCanonicalPath(t, t.TempDir())
	t.Setenv("TMPDIR", temporary)
	tool := editorNewPlanningTool(t)
	running := editorStartPrepare(t, root, tool.path)
	report := tool.wait(t)
	if !within(temporary, report.Directory) || !strings.Contains(report.Directory, "goxide-editor-") {
		t.Fatalf("editor Go planning did not run in its private root: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(root, "goxide.lock.guard")); !os.IsNotExist(err) {
		t.Fatalf("blocked editor planning acquired the CLI project guard: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cli, err := Prepare(ctx, root, Options{Offline: true})
	if err != nil {
		t.Fatalf("CLI preparation failed while editor planning was blocked: %v", err)
	}
	defer cli.Close()
	published, err := cli.buildAndPublish(ctx)
	if err != nil {
		t.Fatalf("CLI publication failed while editor planning was blocked: %v", err)
	}
	command := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", published.Entry)
	command.Dir, command.Env = published.Root, project.GoEnv(true)
	if output, err := command.CombinedOutput(); err != nil || string(output) != "file target\n" {
		t.Fatalf("concurrent CLI result: %v\n%s", err, output)
	}
	lock, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	files := projectSnapshot(t, filepath.Dir(published.Root))
	select {
	case outcome := <-running.done:
		running.consumed = true
		if outcome.project != nil {
			outcome.project.Close()
		}
		t.Fatalf("editor left its controlled planning barrier before release: %v", outcome.err)
	default:
	}
	tool.resume(t)
	editor, err := running.wait(t)
	if err != nil {
		t.Fatal(err)
	}
	private := editor.layoutRoot
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("completed editor retained its private root: %v", err)
	}
	if current, err := os.ReadFile(filepath.Join(root, "goxide.lock")); err != nil || string(current) != string(lock) {
		t.Fatalf("editor changed the CLI dependency lock: %v", err)
	}
	if !reflect.DeepEqual(files, projectSnapshot(t, filepath.Dir(published.Root))) {
		t.Fatal("editor preparation changed published CLI files")
	}
}

func TestEditorPlanningCancellationPreservesWriterLeases(t *testing.T) {
	root := editorCanonicalPath(t, smallProject(t))
	_, locks := editorHoldWriterLeases(t, root)
	temporary := editorCanonicalPath(t, t.TempDir())
	t.Setenv("TMPDIR", temporary)
	tool := editorNewPlanningTool(t)
	running := editorStartPrepare(t, root, tool.path)
	tool.wait(t)
	private, err := filepath.Glob(filepath.Join(temporary, "goxide-editor-*"))
	if err != nil || len(private) != 1 {
		t.Fatalf("active editor private roots = %v, %v", private, err)
	}
	running.cancel()
	prepared, err := running.wait(t)
	if prepared != nil {
		prepared.Close()
		t.Fatal("canceled editor returned a prepared project")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("editor cancellation lost its cause: %v", err)
	}
	if err := tool.connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.connection.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("canceled Go planning helper remained alive: %v", err)
	}
	for _, name := range private {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("canceled preparation retained its private root %s: %v", name, err)
		}
	}
	editorAssertLeases(t, locks)
	if _, err := os.Stat(filepath.Join(root, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("canceled editor wrote the dependency lock: %v", err)
	}
}

func editorCanonicalPath(t *testing.T, name string) string {
	t.Helper()
	name, err := filepath.EvalSymlinks(name)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func editorHoldWriterLeases(t *testing.T, root string) (string, map[string]os.FileInfo) {
	t.Helper()
	release, err := project.LockProject(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	layout, err := project.OpenLayout(root, "example.org/small", "target")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { layout.Close() })
	leases := map[string]os.FileInfo{}
	for _, name := range []string{filepath.Join(root, "goxide.lock.guard"), filepath.Join(layout.Root, ".layout-lock")} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		leases[name] = info
	}
	return layout.Root, leases
}

func editorAssertLeases(t *testing.T, leases map[string]os.FileInfo) {
	t.Helper()
	for name, previous := range leases {
		current, err := os.Stat(name)
		if err != nil || !os.SameFile(previous, current) {
			t.Fatalf("editor deleted or replaced another writer's lease %s: %v", name, err)
		}
	}
}

type editorPrepareOutcome struct {
	project *Project
	err     error
}

type editorRunningPreparation struct {
	done     chan editorPrepareOutcome
	cancel   context.CancelFunc
	consumed bool
}

func editorStartPrepare(t *testing.T, root, tool string) *editorRunningPreparation {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	running := &editorRunningPreparation{done: make(chan editorPrepareOutcome, 1), cancel: cancel}
	go func() {
		prepared, err := Prepare(ctx, root, Options{Offline: true, Editor: true, GoTool: tool})
		running.done <- editorPrepareOutcome{prepared, err}
	}()
	t.Cleanup(func() {
		cancel()
		if !running.consumed {
			prepared, _ := running.wait(t)
			if prepared != nil {
				prepared.Close()
			}
		}
	})
	return running
}

func (running *editorRunningPreparation) wait(t *testing.T) (*Project, error) {
	t.Helper()
	select {
	case outcome := <-running.done:
		running.consumed = true
		return outcome.project, outcome.err
	case <-time.After(30 * time.Second):
		t.Fatal("editor preparation did not finish")
		return nil, nil
	}
}

type editorPlanningReport struct {
	Directory string
	Arguments []string
}

type editorPlanningTool struct {
	path       string
	listener   *net.TCPListener
	connection net.Conn
}

func editorNewPlanningTool(t *testing.T) *editorPlanningTool {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("controlled Go wrapper uses a POSIX shell")
	}
	address, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	tool := &editorPlanningTool{listener: listener}
	t.Cleanup(func() {
		listener.Close()
		if tool.connection != nil {
			tool.connection.Close()
		}
	})
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	tool.path = filepath.Join(directory, "controlled-go")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nGORACE=" + quote(os.Getenv("GORACE")+" atexit_sleep_ms=0") + " exec " + quote(binary) + " -test.run=^TestEditorPrepareToolProcess$ -- \"$@\"\n"
	if err := os.WriteFile(tool.path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOXIDE_TEST_EDITOR_GO_ADDRESS", listener.Addr().String())
	t.Setenv("GOXIDE_TEST_EDITOR_GO_CLAIM", filepath.Join(directory, "claimed"))
	t.Setenv("GOXIDE_TEST_EDITOR_GO_REAL", filepath.Join(runtime.GOROOT(), "bin", "go"))
	return tool
}

func (tool *editorPlanningTool) wait(t *testing.T) editorPlanningReport {
	t.Helper()
	if err := tool.listener.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := tool.listener.Accept()
	if err != nil {
		t.Fatalf("editor did not reach its Go planning command: %v", err)
	}
	tool.connection = connection
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var report editorPlanningReport
	if err := json.NewDecoder(connection).Decode(&report); err != nil {
		t.Fatal(err)
	}
	return report
}

func (tool *editorPlanningTool) resume(t *testing.T) {
	t.Helper()
	if _, err := tool.connection.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
}

// This child test acts as the Go tool only for the controlled subprocesses above.
// It forwards all ordinary commands, and blocks once at an actual planning call.
func TestEditorPrepareToolProcess(t *testing.T) {
	address := os.Getenv("GOXIDE_TEST_EDITOR_GO_ADDRESS")
	if address == "" {
		return
	}
	var arguments []string
	for index, argument := range os.Args {
		if argument == "--" {
			arguments = os.Args[index+1:]
			break
		}
	}
	if len(arguments) >= 2 && arguments[0] == "mod" && arguments[1] == "edit" {
		claim, err := os.OpenFile(os.Getenv("GOXIDE_TEST_EDITOR_GO_CLAIM"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			claim.Close()
			connection, err := net.DialTimeout("tcp", address, 5*time.Second)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			directory, err := os.Getwd()
			if err == nil {
				err = json.NewEncoder(connection).Encode(editorPlanningReport{directory, arguments})
			}
			if err == nil {
				_, err = io.ReadFull(connection, make([]byte, 1))
			}
			connection.Close()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		} else if !os.IsExist(err) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	command := exec.Command(os.Getenv("GOXIDE_TEST_EDITOR_GO_REAL"), arguments...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			os.Exit(exited.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}
