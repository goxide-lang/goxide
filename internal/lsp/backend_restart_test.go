package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/source"
)

type restartEvent struct {
	Kind       string   `json:"kind"`
	PID        int      `json:"pid"`
	WrapperPID int      `json:"wrapperPID,omitempty"`
	Directory  string   `json:"directory,omitempty"`
	RootURI    string   `json:"rootUri,omitempty"`
	BuildFlags []string `json:"buildFlags,omitempty"`
}

// The wrapper runs an unmodified installed gopls. It can hold one real hover
// response after gopls has computed it, establishing a deterministic crash window
// without sleeps, fake semantic answers, or production hooks.
func TestLSPGoplsRestartHelper(t *testing.T) {
	tool := os.Getenv("GOXIDE_LSP_RESTART_GOPLS")
	if tool == "" {
		return
	}
	if err := runGoplsRestartHelper(tool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runGoplsRestartHelper(tool string) error {
	control, err := net.DialTimeout("tcp", os.Getenv("GOXIDE_LSP_RESTART_CONTROL"), 5*time.Second)
	if err != nil {
		return err
	}
	defer control.Close()
	if os.Getenv("GOXIDE_LSP_RESTART_DIRECT") == "1" {
		directory, _ := os.Getwd()
		if err := json.NewEncoder(control).Encode(restartEvent{Kind: "started", PID: os.Getpid(), WrapperPID: os.Getpid(), Directory: directory}); err != nil {
			return err
		}
		control.Close()
		// Keep the wrapper PID: the LSP client now owns gopls itself, not a
		// forwarding process with a separate child lifetime.
		return syscall.Exec(tool, []string{tool}, os.Environ())
	}
	command := exec.Command(tool)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		return err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return err
	}
	if err := command.Start(); err != nil {
		input.Close()
		output.Close()
		return err
	}
	var eventMu sync.Mutex
	encoder := json.NewEncoder(control)
	event := func(value restartEvent) {
		value.PID = command.Process.Pid
		eventMu.Lock()
		defer eventMu.Unlock()
		_ = encoder.Encode(value)
	}
	directory, _ := os.Getwd()
	event(restartEvent{Kind: "started", Directory: directory, WrapperPID: os.Getpid()})
	var stateMu sync.Mutex
	holdNext := false
	hovers := map[string]bool{}
	stopped := make(chan struct{})
	go func() {
		decoder := json.NewDecoder(control)
		for {
			var action struct{ Action string }
			if err := decoder.Decode(&action); err != nil {
				_ = command.Process.Kill()
				return
			}
			if action.Action == "hold" {
				stateMu.Lock()
				holdNext = true
				stateMu.Unlock()
				event(restartEvent{Kind: "armed"})
			}
		}
	}()
	go func() {
		defer input.Close()
		reader := bufio.NewReader(os.Stdin)
		for {
			frame, err := readRPCFrame(reader)
			if err != nil {
				return
			}
			var message rpcMessage
			if json.Unmarshal(frame, &message) != nil {
				return
			}
			if message.Method == "initialize" {
				var params struct {
					RootURI               string `json:"rootUri"`
					InitializationOptions struct {
						BuildFlags []string `json:"buildFlags"`
					} `json:"initializationOptions"`
				}
				_ = json.Unmarshal(message.Params, &params)
				event(restartEvent{Kind: "initialize", RootURI: params.RootURI, BuildFlags: params.InitializationOptions.BuildFlags})
			}
			if message.Method == "textDocument/hover" {
				stateMu.Lock()
				hovers[string(message.ID)], holdNext = holdNext, false
				stateMu.Unlock()
			}
			if _, err := fmt.Fprintf(input, "Content-Length: %d\r\n\r\n%s", len(frame), frame); err != nil {
				return
			}
		}
	}()
	go func() {
		reader := bufio.NewReader(output)
		for {
			frame, err := readRPCFrame(reader)
			if err != nil {
				return
			}
			var message rpcMessage
			if json.Unmarshal(frame, &message) != nil {
				return
			}
			stateMu.Lock()
			hold, hover := hovers[string(message.ID)]
			if message.Method == "" {
				delete(hovers, string(message.ID))
			}
			stateMu.Unlock()
			hover = hover && message.Method == ""
			if hover && hold {
				event(restartEvent{Kind: "held"})
				<-stopped
				return // The dead process's buffered response must never escape.
			}
			if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(frame), frame); err != nil {
				_ = command.Process.Kill()
				return
			}
			if hover {
				event(restartEvent{Kind: "answered"})
			}
		}
	}()
	_ = command.Wait()
	close(stopped)
	event(restartEvent{Kind: "exited"})
	return nil
}

type restartProcess struct {
	connection net.Conn
	started    restartEvent
	initialize restartEvent
	exited     bool
}

type restartMonitor struct {
	listener net.Listener
	events   chan restartEvent
	mu       sync.Mutex
	process  map[int]*restartProcess
	clients  []net.Conn
	workers  sync.WaitGroup
	closed   bool
	seen     []restartEvent
}

func newRestartMonitor(t *testing.T) *restartMonitor {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &restartMonitor{listener: listener, events: make(chan restartEvent, 64), process: map[int]*restartProcess{}}
	m.workers.Go(func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				connection.Close()
				return
			}
			m.clients = append(m.clients, connection)
			m.mu.Unlock()
			m.workers.Go(func() {
				decoder := json.NewDecoder(connection)
				for {
					var value restartEvent
					if err := decoder.Decode(&value); err != nil {
						return
					}
					m.mu.Lock()
					m.seen = append(m.seen, value)
					record := m.process[value.PID]
					if record == nil {
						record = &restartProcess{connection: connection}
						m.process[value.PID] = record
					}
					switch value.Kind {
					case "started":
						record.started = value
					case "initialize":
						record.initialize = value
					case "exited":
						record.exited = true
					}
					m.mu.Unlock()
					select {
					case m.events <- value:
					default:
						connection.Close()
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		listener.Close()
		m.mu.Lock()
		m.closed = true
		for _, connection := range m.clients {
			connection.Close()
		}
		for pid, record := range m.process {
			if process, err := os.FindProcess(pid); err == nil && !record.exited {
				_ = process.Kill()
			}
		}
		m.mu.Unlock()
		m.workers.Wait()
	})
	return m
}

func (m *restartMonitor) wait(t *testing.T, kind string, pid int) restartEvent {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-m.events:
			if event.Kind == kind && (pid == 0 || event.PID == pid) {
				return event
			}
		case <-deadline.C:
			m.mu.Lock()
			seen := append([]restartEvent(nil), m.seen...)
			m.mu.Unlock()
			t.Fatalf("gopls event %s for PID %d did not arrive: %+v", kind, pid, seen)
		}
	}
}

func (m *restartMonitor) assertNativeRoot(t *testing.T, pid int, root string) {
	t.Helper()
	m.mu.Lock()
	record := *m.process[pid]
	m.mu.Unlock()
	uri, _ := PathToURI(root)
	if record.started.Directory != root || record.initialize.RootURI != uri {
		t.Fatalf("native gopls moved away from authorRoot: started=%+v initialize=%+v", record.started, record.initialize)
	}
	var modFile string
	hasTags := false
	for _, flag := range record.initialize.BuildFlags {
		if strings.HasPrefix(flag, "-modfile=") {
			modFile = strings.TrimPrefix(flag, "-modfile=")
		}
		hasTags = hasTags || flag == "-tags=lsp_restart"
	}
	if !hasTags || !filepath.IsAbs(modFile) || modFile == filepath.Join(root, "go.mod") {
		t.Fatalf("native gopls did not preserve tags and private modfile: %+v", record.initialize)
	}
	if _, err := os.Stat(modFile); err != nil {
		t.Fatalf("private modfile missing while gopls is alive: %v", err)
	}
}

func restartGoplsWrapper(t *testing.T, monitor *restartMonitor, tool string, direct bool) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(t.TempDir(), "gopls-wrapper")
	mode := "0"
	if direct {
		mode = "1"
	}
	script := "#!/bin/sh\nGOXIDE_LSP_RESTART_GOPLS=" + quote(tool) + " GOXIDE_LSP_RESTART_CONTROL=" + quote(monitor.listener.Addr().String()) + " GOXIDE_LSP_RESTART_DIRECT=" + mode + " GORACE=atexit_sleep_ms=0 exec " + quote(executable) + " -test.run='^TestLSPGoplsRestartHelper$'\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func (m *restartMonitor) assertGone(t *testing.T, pid int) {
	t.Helper()
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Release()
	if err := process.Signal(syscall.Signal(0)); err == nil {
		t.Errorf("gopls PID %d survived process death or client close", pid)
		return
	}
	m.mu.Lock()
	m.process[pid].exited = true
	m.mu.Unlock()
}

func TestLanguageServerGoplsCrashRestartsCurrentProjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process wrapper and signal-zero exit check require POSIX")
	}
	tool := realGopls(t)
	monitor := newRestartMonitor(t)
	wrapper := restartGoplsWrapper(t, monitor, tool, false)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "author")
	gox := "package app\nfunc Answer() int { return 41 }\n"
	native := "package app\nimport ( _ \"embed\"; \"example.org/local\" )\n//go:embed asset.txt\nvar asset string\nfunc Native() int { _ = asset; _ = local.Value; return Answer() }\n"
	files := map[string]string{
		"go.mod":     "module example.org/restart\n\ngo 1.27\nrequire example.org/local v0.0.0\nreplace example.org/local => ../local\n",
		"go.sum":     "",
		"lib.gox":    gox,
		"native.go":  native,
		"asset.txt":  "embedded author resource\n",
		"lib_gox.go": source.GeneratedHeader + "package app\nfunc Answer() bool { return false }\n",
	}
	for name, text := range files {
		writeLanguageFile(t, root, name, text)
	}
	writeLanguageFile(t, base, "local/go.mod", "module example.org/local\n\ngo 1.27\n")
	writeLanguageFile(t, base, "local/local.go", "package local\nconst Value = 1\n")
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: wrapper, Compiler: compile.Options{Offline: true, Tags: []string{"lsp_restart"}}})
	goxURI := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
	goURI := openLanguageDocument(t, peer, filepath.Join(root, "native.go"), native)
	params := map[string]any{"textDocument": map[string]string{"uri": goURI}, "position": languagePosition(t, native, "Answer()")}
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("int")) || bytes.Contains(hover, []byte("bool")) {
		t.Fatalf("initial real gopls hover: %s", hover)
	}
	first := monitor.wait(t, "answered", 0)
	monitor.assertNativeRoot(t, first.PID, root)
	monitor.mu.Lock()
	connection := monitor.process[first.PID].connection
	monitor.mu.Unlock()
	if err := json.NewEncoder(connection).Encode(map[string]string{"action": "hold"}); err != nil {
		t.Fatal(err)
	}
	monitor.wait(t, "armed", first.PID)
	peer.next++
	oldID := peer.next
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": oldID, "method": "textDocument/hover", "params": params})
	peer.send(t, string(request))
	monitor.wait(t, "held", first.PID)
	newGox := strings.Replace(gox, "int { return 41 }", "string { return \"current\" }", 1)
	newGo := strings.Replace(native, "Native() int", "Native() string", 1)
	changeLanguageDocument(t, peer, goxURI, newGox, 2)
	changeLanguageDocument(t, peer, goURI, newGo, 2)
	process, err := os.FindProcess(first.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	for {
		response := peer.receive(t)
		if len(response.ID) == 0 {
			peer.notifications = append(peer.notifications, response)
			continue
		}
		if string(response.ID) != fmt.Sprint(oldID) || response.Error == nil || len(response.Result) != 0 {
			t.Fatalf("old request published a result after its process died: %+v", response)
		}
		break
	}
	monitor.wait(t, "exited", first.PID)
	params["position"] = languagePosition(t, newGo, "Answer()")
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("string")) || bytes.Contains(hover, []byte("int")) || bytes.Contains(hover, []byte("bool")) {
		t.Fatalf("first query after gopls crash did not use current overlays: %s; notifications=%v", hover, peer.notifications)
	}
	second := monitor.wait(t, "answered", 0)
	if first.PID == second.PID {
		t.Fatal("gopls process did not restart")
	}
	monitor.assertNativeRoot(t, second.PID, root)
	peer.call(t, "shutdown", nil)
	// Reap the proxy's child before closing the forwarding process. Direct
	// client ownership and close behavior are exercised by the exec test below.
	process, err = os.FindProcess(second.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	monitor.wait(t, "exited", second.PID)
	peer.notify(t, "exit", nil)
	awaitRPC(t, peer.done)
	for _, pid := range []int{first.PID, second.PID} {
		monitor.assertGone(t, pid)
	}
	for name, expected := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != expected {
			t.Errorf("author file %s changed: %v\n%s", name, err, data)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != len(files) {
		t.Errorf("language server changed author directory entries: %v, %v", entries, err)
	}
}

func TestLanguageServerGoplsClientCloseReapsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec wrapper and signal-zero exit check require POSIX")
	}
	monitor := newRestartMonitor(t)
	wrapper := restartGoplsWrapper(t, monitor, realGopls(t), true)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gox := "package app\nfunc Answer() int { return 41 }\n"
	native := "package app\nfunc Native() int { return Answer() }\n"
	writeLanguageFile(t, root, "go.mod", "module example.org/close\n\ngo 1.27\n")
	writeLanguageFile(t, root, "lib.gox", gox)
	writeLanguageFile(t, root, "native.go", native)
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: wrapper, Compiler: compile.Options{Offline: true}})
	openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
	uri := openLanguageDocument(t, peer, filepath.Join(root, "native.go"), native)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, native, "Answer()")}
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("int")) {
		t.Fatalf("real gopls hover before close: %s", hover)
	}
	var running restartEvent
	for {
		running = monitor.wait(t, "started", 0)
		process, err := os.FindProcess(running.PID)
		if err != nil {
			t.Fatal(err)
		}
		err = process.Signal(syscall.Signal(0))
		process.Release()
		if err == nil {
			break
		}
	}
	if running.PID != running.WrapperPID || running.Directory != root {
		t.Fatalf("exec wrapper changed process identity or author root: %+v", running)
	}
	peer.call(t, "shutdown", nil)
	peer.notify(t, "exit", nil)
	awaitRPC(t, peer.done)
	monitor.assertGone(t, running.PID)
}
