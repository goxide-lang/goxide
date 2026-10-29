package lsp

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
)

// The workload and budgets are fixed independently of observed results. It uses
// a warm local toolchain, 16 functions and real gopls; dependency downloads are
// excluded. Enable the timing gates on the measurement host, not shared CI.
func TestLanguageServerInteractionBudget(t *testing.T) {
	if os.Getenv("GOXIDE_LSP_PERF") != "1" {
		t.Skip("set GOXIDE_LSP_PERF=1 for fixed-host timing and resource acceptance")
	}
	tool := realGopls(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool, backendPID := measuredGopls(t, tool)
	var source strings.Builder
	source.WriteString("package app\n")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&source, "func Value%d() int { return %d }\n", i, i)
	}
	source.WriteString("func Use() int { return Value15() }\n")
	text := source.String()
	writeLanguageFile(t, root, "lib.gox", text)
	writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/budget'\ngo='1.27'\n[lib]\nname='app'\npath='lib.gox'\n")
	started := time.Now()
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), text)
	preview := func() { peer.call(t, "goxide/generatedSource", map[string]string{"uri": uri}) }
	preview()
	cold := time.Since(started)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, text, "Value15()")}
	methods := []string{"textDocument/hover", "textDocument/definition", "textDocument/completion"}
	for i := 0; i < 12; i++ {
		peer.call(t, methods[i%len(methods)], params)
	}
	directoryCount := func() int {
		count := 0
		if err := filepath.WalkDir(root, func(_ string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				count++
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return count
	}
	beforeDirs, beforeTasks := directoryCount(), runtime.NumGoroutine()
	beforeRSS, beforeGoRSS := processRSS(t, os.Getpid()), processRSS(t, backendPID())
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var queries []time.Duration
	for i := 0; i < 150; i++ {
		start := time.Now()
		peer.call(t, methods[i%len(methods)], params)
		queries = append(queries, time.Since(start))
	}
	var edits, full []time.Duration
	version := 1
	for batch := 0; batch < 10; batch++ {
		for n := 0; n < 100; n++ {
			version++
			changeLanguageDocument(t, peer, uri, strings.Replace(text, "return 15", fmt.Sprintf("return %d", version), 1), version)
		}
		start := time.Now()
		peer.call(t, "textDocument/hover", params)
		edits = append(edits, time.Since(start))
		preview()
		full = append(full, time.Since(start))
	}
	// Exercise completed generations as well as the coalesced burst above.
	// Retaining even one prior analysis per finished edit must be detectable.
	for n := 0; n < 1000; n++ {
		version++
		start := time.Now()
		changeLanguageDocument(t, peer, uri, strings.Replace(text, "return 15", fmt.Sprintf("return %d", version), 1), version)
		peer.call(t, "textDocument/hover", params)
		edits = append(edits, time.Since(start))
		preview()
		full = append(full, time.Since(start))
		if n == 499 {
			t.Logf("500 completed edits: serverRSS=%dKiB goplsRSS=%dKiB", processRSS(t, os.Getpid()), processRSS(t, backendPID()))
		}
	}
	// A backend-only failure must also finish within the full-diagnostic budget.
	version++
	peer.notifications = nil
	start := time.Now()
	changeLanguageDocument(t, peer, uri, strings.Replace(text, "return 15", "", 1), version)
	preview()
	backendFailure := time.Since(start)
	found := false
	for _, message := range peer.notifications {
		if message.Method == "textDocument/publishDiagnostics" && bytes.Contains(message.Params, []byte("missing return")) {
			found = true
		}
	}
	if !found {
		t.Fatal("full Go diagnostic omitted backend-only error")
	}
	if after := directoryCount(); after != beforeDirs {
		t.Fatalf("1000 same-configuration edits grew directories: %d -> %d", beforeDirs, after)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if tasks := runtime.NumGoroutine(); tasks > beforeTasks+16 {
		t.Fatalf("finished edits retained tasks: %d -> %d", beforeTasks, tasks)
	}
	if after.HeapAlloc > before.HeapAlloc+32<<20 {
		t.Fatalf("finished edits retained heap: %d -> %d", before.HeapAlloc, after.HeapAlloc)
	}
	p95 := func(values []time.Duration) time.Duration {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		return values[(len(values)-1)*95/100]
	}
	if value := p95(queries); value > 100*time.Millisecond {
		t.Errorf("hot query p95 %v exceeds 100ms", value)
	}
	if value := p95(edits); value > time.Second {
		t.Errorf("source edit p95 %v exceeds 1s", value)
	}
	if value := p95(full); value > 5*time.Second {
		t.Errorf("complete Go p95 %v exceeds 5s", value)
	}
	if backendFailure > 5*time.Second {
		t.Errorf("backend-only diagnostic %v exceeds 5s", backendFailure)
	}
	t.Logf("startup=%v hot-query-p95=%v edit-source-p95=%v full-go-p95=%v backend-error=%v heap=%d->%d directories=%d", cold, p95(queries), p95(edits), p95(full), backendFailure, before.HeapAlloc, after.HeapAlloc, beforeDirs)
	t.Logf("1000 completed edits: serverRSS=%d->%dKiB goplsRSS=%d->%dKiB", beforeRSS, processRSS(t, os.Getpid()), beforeGoRSS, processRSS(t, backendPID()))
}

func measuredGopls(t *testing.T, tool string) (string, func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return tool, func() int { return 0 }
	}
	directory := t.TempDir()
	pidFile := filepath.Join(directory, "pid")
	wrapper := filepath.Join(directory, "gopls")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" > " + quote(pidFile) + "\nexec " + quote(tool) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return wrapper, func() int {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		return pid
	}
}

func processRSS(t *testing.T, pid int) int64 {
	t.Helper()
	if pid == 0 {
		return 0
	}
	output, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatal(err)
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
