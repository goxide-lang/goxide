package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"weak"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/source"
)

// This peer runs the ordinary transport and analysis worker while exposing the
// server to assertions about immutable source-query and generation identity.
func startBudgetServer(t *testing.T, input, tool string) (*Server, *languagePeer) {
	t.Helper()
	in, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	peer := &languagePeer{rpcPeer: &rpcPeer{input: writer, output: reader, reader: bufio.NewReader(reader), done: make(chan struct{})}, log: new(bytes.Buffer)}
	peer.readTimeout = 30 * time.Second
	ctx, cancel := context.WithCancel(t.Context())
	server := &Server{options: ServerOptions{Directory: input, Version: "budget-test", Gopls: tool, Compiler: compile.Options{GoTool: "go", Offline: true}, Log: peer.log}, connection: NewConnection(in, out), ctx: ctx, cancel: cancel, documents: NewDocumentStore(source.UTF16), encoding: source.UTF16, work: make(chan struct{}, 1), changed: make(chan struct{}), publishedURIs: map[string]bool{}}
	server.workers.Go(server.analyze)
	go func() {
		peer.err = server.connection.Serve(ctx, server.request, server.notification)
		cancel()
		server.mu.Lock()
		if server.workCancel != nil {
			server.workCancel()
		}
		server.mu.Unlock()
		server.workers.Wait()
		server.backend.close()
		if server.goView != "" {
			_ = os.RemoveAll(server.goView)
		}
		close(peer.done)
	}()
	t.Cleanup(func() {
		writer.Close()
		reader.Close()
		awaitRPC(t, peer.done)
		if peer.err != nil && !errors.Is(peer.err, context.Canceled) {
			t.Errorf("server stopped: %v\n%s", peer.err, peer.log.String())
		}
	})
	peer.call(t, "initialize", map[string]any{"capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}}})
	peer.notify(t, "initialized", map[string]any{})
	return server, peer
}

func budgetSnapshot(t *testing.T, server *Server) *publishedAnalysis {
	t.Helper()
	server.mu.Lock()
	defer server.mu.Unlock()
	current := server.current
	if current == nil || !current.complete || current.epoch != server.epoch || current.goProjection == nil || current.backendError != nil {
		t.Fatalf("expected complete current Go view: %+v", current)
	}
	return current
}

type budgetFixture struct {
	server              *Server
	peer                *languagePeer
	root, goxURI, goURI string
	gox, native         string
	params              map[string]any
}

func newBudgetFixture(t *testing.T, tool string, broken bool) budgetFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gox := "package app\nfunc Answer() int { return 41 }\n"
	if broken {
		gox += "func Broken() int {}\n"
	}
	native := "package app\nfunc Native() int { return Answer() }\n"
	writeLanguageFile(t, root, "go.mod", "module example.org/backend-budget\n\ngo 1.27\n")
	writeLanguageFile(t, root, "lib.gox", gox)
	writeLanguageFile(t, root, "native.go", native)
	server, peer := startBudgetServer(t, root, tool)
	gox = strings.Replace(gox, "Answer() int { return 41 }", "Answer() string { return \"unsaved\" }", 1)
	native = strings.Replace(native, "Native() int", "Native() string", 1)
	goxURI := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
	goURI := openLanguageDocument(t, peer, filepath.Join(root, "native.go"), native)
	params := map[string]any{"textDocument": map[string]string{"uri": goURI}, "position": languagePosition(t, native, "Answer()")}
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("string")) {
		t.Fatalf("initial unsaved Go projection: %s", hover)
	}
	return budgetFixture{server, peer, root, goxURI, goURI, gox, native, params}
}

func fillBackendBudget(t *testing.T, fixture budgetFixture, backend *goBackend, count uint64) {
	t.Helper()
	for range goRequestLimit + 8 {
		if backend.client.RequestCount() >= count {
			if current := budgetSnapshot(t, fixture.server); current.goProjection.backend != backend {
				t.Fatal("backend was replaced before the requested boundary")
			}
			return
		}
		if hover := fixture.peer.call(t, "textDocument/hover", fixture.params); !bytes.Contains(hover, []byte("string")) {
			t.Fatalf("bulk query lost unsaved source: %s", hover)
		}
	}
	t.Fatal("backend RPC count did not reach the fixed production limit")
}

func TestLanguageServerBackendBudgetReusesAnalysis(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PID/reaping assertions use POSIX exec and signal zero")
	}
	monitor := newRestartMonitor(t)
	fixture := newBudgetFixture(t, restartGoplsWrapper(t, monitor, realGopls(t), true), true)
	first := monitor.wait(t, "started", 0)
	before := budgetSnapshot(t, fixture.server)
	backend := before.goProjection.backend
	if before.generateError == nil || !strings.Contains(before.generateError.Error(), "missing return") {
		t.Fatalf("fixture did not preserve a compiler Go-only diagnostic: %v", before.generateError)
	}
	fillBackendBudget(t, fixture, backend, goRequestLimit-1)
	started := time.Now()
	// Exercise the actual eight request workers across the threshold. Every
	// already admitted success must survive draining and process replacement.
	ids := map[string]bool{}
	for range 8 {
		fixture.peer.next++
		id := fixture.peer.next
		ids[fmt.Sprint(id)] = true
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "textDocument/hover", "params": fixture.params})
		fixture.peer.send(t, string(request))
	}
	for len(ids) != 0 {
		response := fixture.peer.receive(t)
		if len(response.ID) == 0 {
			fixture.peer.notifications = append(fixture.peer.notifications, response)
			continue
		}
		if !ids[string(response.ID)] || response.Error != nil || !bytes.Contains(response.Result, []byte("string")) {
			t.Fatalf("a boundary query failed or used old source: %+v", response)
		}
		delete(ids, string(response.ID))
	}
	if hover := fixture.peer.call(t, "textDocument/hover", fixture.params); !bytes.Contains(hover, []byte("string")) {
		t.Fatalf("query after the boundary: %s", hover)
	}
	if elapsed := time.Since(started); elapsed > goMaintenanceTimeout {
		t.Fatalf("drain, restart, synchronization, diagnostics and query took %v", elapsed)
	}
	after := budgetSnapshot(t, fixture.server)
	if after.goProjection.backend == backend || after.epoch != before.epoch || after.queries != before.queries || after.result != before.result {
		t.Fatal("budget maintenance did not replace only the Go backend")
	}
	if after.generateError == nil || after.generateError.Error() != before.generateError.Error() {
		t.Fatalf("maintenance lost compiler diagnostic: %v -> %v", before.generateError, after.generateError)
	}
	for key, mapping := range before.sourceMaps {
		if after.sourceMaps[key] != mapping || len(before.generated[key]) != 0 && &after.generated[key][0] != &before.generated[key][0] {
			t.Fatalf("maintenance rebuilt source mappings or generated bytes for %s", key)
		}
	}
	items := latestLanguageDiagnostics(t, fixture.peer, fixture.goxURI)
	found := false
	for _, item := range items {
		found = found || strings.Contains(item.Message, "missing return")
	}
	if !found {
		t.Fatalf("maintenance cleared the current compiler diagnostic: %+v", items)
	}
	if count := backend.client.RequestCount(); count < goRequestLimit || count > goRequestLimit+8 {
		t.Fatalf("retired requests=%d, expected limit plus at most seven admitted queries and shutdown", count)
	}
	select {
	case <-backend.client.Done():
	default:
		t.Fatal("replacement was published before the old process was reaped")
	}
	second := monitor.wait(t, "started", 0)
	if first.PID == second.PID {
		t.Fatal("budget did not replace the gopls PID")
	}
	monitor.assertGone(t, first.PID)
	t.Logf("fixed %d RPC boundary: retired=%d, full maintenance=%v, PID=%d -> %d", goRequestLimit, backend.client.RequestCount(), time.Since(started), first.PID, second.PID)

	// A real edit can be the next synchronization instead of a Go query. It
	// follows the normal analysis path and still retires the spent backend.
	backend = after.goProjection.backend
	fillBackendBudget(t, fixture, backend, goRequestLimit)
	changeLanguageDocument(t, fixture.peer, fixture.goxURI, strings.Replace(fixture.gox, "unsaved", "next edit", 1), 2)
	fixture.peer.call(t, "goxide/generatedSource", map[string]string{"uri": fixture.goxURI})
	next := budgetSnapshot(t, fixture.server)
	if next.goProjection.backend == backend || next.epoch == after.epoch || next.queries == after.queries {
		t.Fatal("a changed source did not use normal analysis and retire the spent backend before synchronization")
	}
	monitor.wait(t, "started", 0)
	monitor.assertGone(t, second.PID)
}

// The helper preserves the real gopls PID. A marker can pause the next launch
// before exec so a source/configuration notification deterministically races
// initialization; no fake protocol or semantic response is returned.
func TestLSPGoplsBudgetExecHelper(t *testing.T) {
	tool := os.Getenv("GOXIDE_LSP_BUDGET_GOPLS")
	if tool == "" {
		return
	}
	connection, err := net.DialTimeout("tcp", os.Getenv("GOXIDE_LSP_BUDGET_CONTROL"), 5*time.Second)
	if err == nil {
		directory, _ := os.Getwd()
		err = json.NewEncoder(connection).Encode(restartEvent{Kind: "started", PID: os.Getpid(), WrapperPID: os.Getpid(), Directory: directory})
		if _, statErr := os.Stat(os.Getenv("GOXIDE_LSP_BUDGET_HOLD")); err == nil && statErr == nil {
			var release any
			err = json.NewDecoder(connection).Decode(&release)
		}
		connection.Close()
	}
	if err == nil {
		err = syscall.Exec(tool, []string{tool}, os.Environ())
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func TestLanguageServerBackendBudgetInputRace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("controlled exec helper uses POSIX")
	}
	for _, kind := range []string{"edit", "configuration", "timeout", "synchronization_timeout"} {
		t.Run(kind, func(t *testing.T) {
			timeout := strings.HasSuffix(kind, "timeout")
			monitor := newRestartMonitor(t)
			tool := realGopls(t)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			controlDirectory := t.TempDir()
			hold := filepath.Join(controlDirectory, "hold")
			wrapper := filepath.Join(controlDirectory, "gopls")
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
			script := "#!/bin/sh\nGOXIDE_LSP_BUDGET_GOPLS=" + quote(tool) + " GOXIDE_LSP_BUDGET_CONTROL=" + quote(monitor.listener.Addr().String()) + " GOXIDE_LSP_BUDGET_HOLD=" + quote(hold) + " GORACE=atexit_sleep_ms=0 exec " + quote(executable) + " -test.run='^TestLSPGoplsBudgetExecHelper$'\n"
			if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			fixture := newBudgetFixture(t, wrapper, false)
			first := monitor.wait(t, "started", 0)
			before := budgetSnapshot(t, fixture.server)
			fillBackendBudget(t, fixture, before.goProjection.backend, goRequestLimit)
			if err := os.WriteFile(hold, nil, 0600); err != nil {
				t.Fatal(err)
			}
			fixture.peer.next++
			oldID := fixture.peer.next
			method, params := "textDocument/hover", any(fixture.params)
			if kind == "synchronization_timeout" {
				fixture.gox = strings.Replace(fixture.gox, "unsaved", "new source", 1)
				changeLanguageDocument(t, fixture.peer, fixture.goxURI, fixture.gox, 2)
				method, params = "goxide/generatedSource", map[string]string{"uri": fixture.goxURI}
			}
			request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": oldID, "method": method, "params": params})
			started := time.Now()
			fixture.peer.send(t, string(request))
			blocked := monitor.wait(t, "started", 0)
			if kind == "synchronization_timeout" {
				fixture.server.mu.Lock()
				before = fixture.server.current // New source analysis, before its Go view.
				fixture.server.mu.Unlock()
			}
			monitor.assertGone(t, first.PID)
			if err := os.Remove(hold); err != nil {
				t.Fatal(err)
			}
			want := "string"
			if kind == "edit" {
				gox := strings.Replace(fixture.gox, "string { return \"unsaved\" }", "bool { return true }", 1)
				native := strings.Replace(fixture.native, "Native() string", "Native() bool", 1)
				changeLanguageDocument(t, fixture.peer, fixture.goxURI, gox, 2)
				changeLanguageDocument(t, fixture.peer, fixture.goURI, native, 2)
				fixture.params["position"] = languagePosition(t, native, "Answer()")
				want = "bool"
			} else if kind == "configuration" {
				writeLanguageFile(t, fixture.root, "go.mod", "module example.org/backend-budget\n\ngo 1.27\n// changed while restarting\n")
				uri, _ := PathToURI(filepath.Join(fixture.root, "go.mod"))
				fixture.peer.notify(t, "workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": uri, "type": 2}}})
			} else {
				assertBudgetSourceQuery(t, fixture)
			}
			for {
				response := fixture.peer.receive(t)
				if len(response.ID) == 0 {
					fixture.peer.notifications = append(fixture.peer.notifications, response)
					continue
				}
				if kind == "synchronization_timeout" {
					if string(response.ID) != fmt.Sprint(oldID) || response.Error != nil || len(response.Result) == 0 {
						t.Fatalf("backend timeout blocked the available generated source: %+v", response)
					}
				} else if string(response.ID) != fmt.Sprint(oldID) || response.Error == nil || !timeout && response.Error.Code != CodeContentModified || len(response.Result) != 0 {
					t.Fatalf("maintenance published an old-input query: %+v", response)
				}
				break
			}
			if timeout {
				if elapsed := time.Since(started); elapsed < goMaintenanceTimeout-time.Second || elapsed > goMaintenanceTimeout+time.Second {
					t.Fatalf("initialization did not respect the complete maintenance deadline: %v", elapsed)
				}
				failed := awaitBudgetFailure(t, fixture.server)
				if failed.queries != before.queries || failed.result != before.result || failed.epoch != before.epoch {
					t.Fatal("failed initialization rebuilt the source analysis")
				}
				assertBudgetSourceQuery(t, fixture)
			}
			if hover := fixture.peer.call(t, "textDocument/hover", fixture.params); !bytes.Contains(hover, []byte(want)) {
				t.Fatalf("normal analysis after %s did not use current unsaved source: %s", kind, hover)
			}
			after := budgetSnapshot(t, fixture.server)
			if timeout && (after.epoch != before.epoch || after.queries != before.queries || after.result != before.result) {
				t.Fatal("retry after failed initialization rebuilt current source")
			}
			if !timeout && (after.epoch == before.epoch || after.queries == before.queries || after.result == before.result) {
				t.Fatal("changed input reused the maintenance snapshot instead of normal analysis")
			}
			monitor.wait(t, "started", 0)
			monitor.assertGone(t, blocked.PID)
		})
	}
}

func awaitBudgetFailure(t *testing.T, server *Server) *publishedAnalysis {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for {
		server.mu.Lock()
		current, changed := server.current, server.changed
		ready := current != nil && current.complete && current.epoch == server.epoch && current.backendError != nil && server.backendMaintenance == nil
		server.mu.Unlock()
		if ready {
			return current
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("maintenance failure was not published")
		}
	}
}

func assertBudgetSourceQuery(t *testing.T, fixture budgetFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	params, _ := json.Marshal(map[string]any{"textDocument": map[string]string{"uri": fixture.goxURI}, "position": languagePosition(t, fixture.gox, "Answer()")})
	answer, err := fixture.server.request(ctx, "textDocument/hover", params)
	data, _ := json.Marshal(answer)
	if err != nil || !bytes.Contains(data, []byte("string")) {
		t.Fatalf("backend maintenance blocked the current source query: %s (%v)", data, err)
	}
}

func TestLanguageServerBackendBudgetDrainTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("freezing the actual gopls process requires POSIX signals")
	}
	monitor := newRestartMonitor(t)
	fixture := newBudgetFixture(t, restartGoplsWrapper(t, monitor, realGopls(t), true), false)
	first := monitor.wait(t, "started", 0)
	before := budgetSnapshot(t, fixture.server)
	backend := before.goProjection.backend
	fillBackendBudget(t, fixture, backend, goRequestLimit-1)
	process, err := os.FindProcess(first.PID)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Release()
	if err := process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	send := func() {
		fixture.peer.next++
		id := fixture.peer.next
		ids[fmt.Sprint(id)] = true
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "textDocument/hover", "params": fixture.params})
		fixture.peer.send(t, string(request))
	}
	send()
	// Wait on the observable admission count, not on an assumed RPC latency.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for backend.client.RequestCount() < goRequestLimit {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("the stopped backend did not receive the boundary request")
		}
	}
	started := time.Now()
	send()
	assertBudgetSourceQuery(t, fixture)
	for len(ids) != 0 {
		response := fixture.peer.receive(t)
		if len(response.ID) == 0 {
			fixture.peer.notifications = append(fixture.peer.notifications, response)
			continue
		}
		if !ids[string(response.ID)] || response.Error == nil || len(response.Result) != 0 {
			t.Fatalf("stopped backend returned a result during forced retirement: %+v", response)
		}
		delete(ids, string(response.ID))
	}
	if elapsed := time.Since(started); elapsed < goMaintenanceTimeout-time.Second || elapsed > goMaintenanceTimeout+time.Second {
		t.Fatalf("drain did not respect the complete maintenance deadline: %v", elapsed)
	}
	failed := awaitBudgetFailure(t, fixture.server)
	if failed.epoch != before.epoch || failed.queries != before.queries || failed.result != before.result {
		t.Fatal("forced retirement was treated as an input change")
	}
	select {
	case <-backend.client.Done():
	case <-time.After(time.Second):
		t.Fatal("timed-out drain left the stopped gopls process alive")
	}
	monitor.assertGone(t, first.PID)
	assertBudgetSourceQuery(t, fixture)
	if hover := fixture.peer.call(t, "textDocument/hover", fixture.params); !bytes.Contains(hover, []byte("string")) {
		t.Fatalf("next query did not retry the current projection: %s", hover)
	}
	after := budgetSnapshot(t, fixture.server)
	if after.epoch != before.epoch || after.queries != before.queries || after.result != before.result || after.goProjection.backend == backend {
		t.Fatal("retry after timed-out drain failed to preserve current analysis")
	}
	monitor.wait(t, "started", 0)
}

// Opt in only on the measurement host after concurrent heavy regressions end.
// This is an actual LSP service workload with the fixed production budget, not
// a lower-cap test configuration or a timing benchmark.
func TestLanguageServerBackendBudgetResources(t *testing.T) {
	reportPath := os.Getenv("GOXIDE_LSP_BACKEND_RESOURCE_REPORT")
	if reportPath == "" {
		t.Skip("set GOXIDE_LSP_BACKEND_RESOURCE_REPORT to a new absolute JSON path for the isolated 12000-query resource observation")
	}
	if !filepath.IsAbs(reportPath) || runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Fatal("resource observation needs an absolute report path and macOS/Linux ps RSS")
	}
	file, err := os.OpenFile(reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	type resourceSample struct {
		Queries          int     `json:"completed_go_queries"`
		Cycle            int     `json:"cycle"`
		Elapsed          float64 `json:"elapsed_seconds"`
		ServerHeapBytes  uint64  `json:"server_heap_alloc_bytes"`
		ServerRSSKiB     int64   `json:"server_rss_kib"`
		Goroutines       int     `json:"server_goroutines"`
		PID              int     `json:"gopls_pid"`
		GoplsRSSKiB      int64   `json:"gopls_rss_kib"`
		RetiredPID       int     `json:"retired_pid,omitempty"`
		RetiredRPCs      uint64  `json:"retired_rpc_requests,omitempty"`
		RetiredRSSKiB    int64   `json:"retired_gopls_rss_kib_before_rebuild,omitempty"`
		RetainedBackends int     `json:"retained_old_backend_objects"`
	}
	report := struct {
		Scope        string           `json:"scope"`
		GoVersion    string           `json:"go_version"`
		GoplsVersion string           `json:"gopls_version"`
		RequestLimit int              `json:"request_limit"`
		Queries      int              `json:"completed_go_queries"`
		Reaped       []int            `json:"reaped_gopls_pids"`
		Samples      []resourceSample `json:"samples"`
		Complete     bool             `json:"complete"`
	}{Scope: "actual LSP transport + service + unmodified gopls; stable unsaved Go/Goxide source; three forced GCs in service before each sample; ps RSS is per process, not process-tree peak; weak references verify old backend objects are collectible", GoVersion: runtime.Version(), RequestLimit: goRequestLimit}
	save := func() {
		data, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			err = os.WriteFile(reportPath, append(data, '\n'), 0644)
		}
		if err != nil {
			t.Errorf("save backend resource report: %v", err)
		}
	}
	defer save()
	tool := realGopls(t)
	version, err := exec.CommandContext(t.Context(), tool, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("gopls version: %s, %v", version, err)
	}
	report.GoplsVersion = strings.TrimSpace(string(version))
	monitor := newRestartMonitor(t)
	fixture := newBudgetFixture(t, restartGoplsWrapper(t, monitor, tool, true), false)
	pid := monitor.wait(t, "started", 0).PID
	queries := budgetSnapshot(t, fixture.server).queries
	result := budgetSnapshot(t, fixture.server).result
	var retired []weak.Pointer[goBackend]
	started := time.Now()
	sample := func(oldPID int, oldRPCs uint64, oldRSS int64) {
		for range 3 {
			runtime.GC()
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		point := resourceSample{Queries: report.Queries, Cycle: len(retired), Elapsed: time.Since(started).Seconds(), ServerHeapBytes: memory.HeapAlloc, ServerRSSKiB: processRSS(t, os.Getpid()), Goroutines: runtime.NumGoroutine(), PID: pid, GoplsRSSKiB: processRSS(t, pid), RetiredPID: oldPID, RetiredRPCs: oldRPCs, RetiredRSSKiB: oldRSS}
		for _, reference := range retired {
			if reference.Value() != nil {
				point.RetainedBackends++
			}
		}
		report.Samples = append(report.Samples, point)
		save()
		t.Logf("queries=%d cycle=%d serviceHeap=%dB serviceRSS=%dKiB goplsPID=%d goplsRSS=%dKiB oldPID=%d oldRPCs=%d oldRSS=%dKiB retainedBackends=%d", point.Queries, point.Cycle, point.ServerHeapBytes, point.ServerRSSKiB, point.PID, point.GoplsRSSKiB, oldPID, oldRPCs, oldRSS, point.RetainedBackends)
	}
	sample(0, 0, 0)
	// Scope old snapshots and backend pointers inside this call so the
	// observation itself does not keep retired objects alive during GC.
	runQuery := func() (oldPID int, oldRPCs uint64, oldRSS int64) {
		before := budgetSnapshot(t, fixture.server)
		backend := before.goProjection.backend
		if backend.client.RequestCount() >= goRequestLimit {
			oldRSS = processRSS(t, pid)
		}
		if hover := fixture.peer.call(t, "textDocument/hover", fixture.params); !bytes.Contains(hover, []byte("string")) {
			t.Fatalf("resource workload lost current unsaved projection: %s", hover)
		}
		after := budgetSnapshot(t, fixture.server)
		if after.queries != queries || after.result != result {
			t.Fatal("repeated maintenance rebuilt or retained a different source analysis")
		}
		if after.goProjection.backend == backend {
			return 0, 0, 0
		}
		select {
		case <-backend.client.Done():
		default:
			t.Fatal("new cycle started before old gopls was reaped")
		}
		oldPID, oldRPCs = pid, backend.client.RequestCount()
		monitor.assertGone(t, oldPID)
		report.Reaped = append(report.Reaped, oldPID)
		retired = append(retired, weak.Make(backend))
		pid = monitor.wait(t, "started", 0).PID
		return oldPID, oldRPCs, oldRSS
	}
	for range 12000 {
		oldPID, oldRPCs, oldRSS := runQuery()
		report.Queries++
		if oldPID != 0 {
			sample(oldPID, oldRPCs, oldRSS)
		}
	}
	sample(0, 0, 0)
	first, last := report.Samples[0], report.Samples[len(report.Samples)-1]
	if len(retired) < 5 || last.RetainedBackends != 0 {
		t.Fatalf("recycles=%d retained old backend objects=%d", len(retired), last.RetainedBackends)
	}
	if last.ServerHeapBytes > first.ServerHeapBytes+32<<20 || last.Goroutines > first.Goroutines+16 {
		t.Fatalf("service resources grew across fixed-input cycles: before=%+v after=%+v", first, last)
	}
	fixture.peer.call(t, "shutdown", nil)
	fixture.peer.notify(t, "exit", nil)
	awaitRPC(t, fixture.peer.done)
	monitor.assertGone(t, pid)
	report.Reaped = append(report.Reaped, pid)
	report.Complete = true
	save()
	t.Logf("12000-query resource observation complete: %s", reportPath)
}
