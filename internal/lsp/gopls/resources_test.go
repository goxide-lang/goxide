package gopls

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
)

type goplsMemorySample struct {
	Phase           string          `json:"phase"`
	CompletedEdits  int             `json:"completed_edits"`
	ElapsedSeconds  float64         `json:"elapsed_seconds"`
	IdleSeconds     float64         `json:"idle_seconds"`
	RSSBeforeGCKiB  int64           `json:"rss_before_gc_kib"`
	RSSAfterGCKiB   int64           `json:"rss_after_gc_kib"`
	GCQuerySeconds  float64         `json:"gc_query_seconds"`
	HeapAllocBytes  uint64          `json:"heap_alloc_bytes"`
	HeapInUseBytes  uint64          `json:"heap_in_use_bytes"`
	TotalAllocBytes uint64          `json:"total_alloc_bytes"`
	WorkspaceStats  json.RawMessage `json:"workspace_stats"`
	HeapProfile     string          `json:"heap_profile,omitempty"`
}

type goplsMemoryReport struct {
	Scope                string              `json:"scope"`
	Started              string              `json:"started_utc"`
	Platform             string              `json:"platform"`
	GoVersion            string              `json:"harness_go_version"`
	GoplsPath            string              `json:"gopls_path"`
	GoplsVersion         string              `json:"gopls_version"`
	GoplsPID             int                 `json:"gopls_pid"`
	DebugURL             string              `json:"debug_url,omitempty"`
	Initialization       map[string]any      `json:"initialization_options"`
	Environment          []string            `json:"environment_overrides"`
	WarmupEdits          int                 `json:"warmup_edits"`
	PlannedEdits         int                 `json:"planned_edits"`
	CompletedEdits       int                 `json:"completed_edits"`
	MinimumEditPeriodMS  int                 `json:"minimum_edit_period_ms"`
	SampleEveryEdits     int                 `json:"sample_every_edits"`
	Measurement          string              `json:"measurement"`
	EditingSeconds       float64             `json:"editing_seconds"`
	PlannedIdleSeconds   int                 `json:"planned_idle_seconds"`
	ProjectionFileCount  int                 `json:"projection_file_count"`
	FinalDocumentVersion int32               `json:"final_document_version"`
	ProcessReaped        bool                `json:"process_reaped"`
	Complete             bool                `json:"complete"`
	Failure              string              `json:"failure,omitempty"`
	Samples              []goplsMemorySample `json:"samples"`
}

// TestGoplsResourceRetention is an opt-in observation, not a timing CI gate or a
// claim that heap/RSS growth proves a leak. It isolates the production Client
// and Coordinator with real analysis/lowering, hover, and pull diagnostics. It
// does not run the LSP Server, DocumentStore, project loader, or editing debounce.
//
// Set GOXIDE_GOPLS_RESOURCE_REPORT to a new absolute JSON filename, then run only
// this test with -v -count=1 -timeout=6m on an otherwise idle host. The run takes
// at least 170 seconds by default. GOXIDE_GOPLS_RESOURCE_ROUNDS, _PERIOD_MS,
// _SAMPLE_EVERY, and _IDLE_SECONDS can vary the observation (idle must be a
// multiple of 20). _PROFILE_DIR optionally creates a new absolute directory
// with public pprof heap profiles at warmup, halfway, and the final edit.
// Regular tests skip it without starting a process.
func TestGoplsResourceRetention(t *testing.T) {
	reportPath := os.Getenv("GOXIDE_GOPLS_RESOURCE_REPORT")
	if reportPath == "" {
		t.Skip("set GOXIDE_GOPLS_RESOURCE_REPORT to a new absolute JSON file for the isolated retention observation")
	}
	if !filepath.IsAbs(reportPath) {
		t.Fatal("resource report path must be absolute")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Fatal("this observation requires ps RSS in KiB on macOS or Linux")
	}
	setting := func(suffix string, fallback, minimum int) int {
		text := os.Getenv("GOXIDE_GOPLS_RESOURCE_" + suffix)
		if text == "" {
			return fallback
		}
		value, err := strconv.Atoi(text)
		if err != nil || value < minimum {
			t.Fatalf("invalid GOXIDE_GOPLS_RESOURCE_%s=%q: need integer >= %d", suffix, text, minimum)
		}
		return value
	}
	const warmup = 25
	rounds := setting("ROUNDS", 3000, 1)
	editPeriodMS := setting("PERIOD_MS", 30, 0)
	sampleEvery := setting("SAMPLE_EVERY", 500, 1)
	idleSeconds := setting("IDLE_SECONDS", 80, 0)
	if idleSeconds%20 != 0 {
		t.Fatal("GOXIDE_GOPLS_RESOURCE_IDLE_SECONDS must be a multiple of 20")
	}
	started := time.Now()
	report := goplsMemoryReport{
		Scope:   "real gopls via Client + Coordinator; analysis/lower produces one stable virtual Go file; no LSP Server, DocumentStore, project loader, or debounce",
		Started: started.UTC().Format(time.RFC3339Nano), Platform: runtime.GOOS + "/" + runtime.GOARCH, GoVersion: runtime.Version(),
		WarmupEdits: warmup, PlannedEdits: rounds, MinimumEditPeriodMS: editPeriodMS, PlannedIdleSeconds: idleSeconds,
		SampleEveryEdits: sampleEvery,
		Measurement:      "each sample requests public workspace_stats (which may compute current metadata/diagnostics), then mem_stats (three forced GCs); RSS is sampled before and after mem_stats; cumulative TotalAlloc is not retained heap",
		Initialization:   map[string]any{"pullDiagnostics": true, "fileWatcher": "off", "diagnosticsTrigger": "Save", "diagnosticsDelay": "1s"},
		Environment:      []string{"GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off"},
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(reportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	save := func() error {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(reportPath, append(data, '\n'), 0644)
	}
	defer func() {
		if err := save(); err != nil {
			t.Errorf("persist resource observation: %v", err)
		}
	}()
	fail := func(stage string, err error) {
		report.Failure = fmt.Sprintf("%s: %v", stage, err)
		t.Fatal(report.Failure)
	}
	tool := os.Getenv("GOXIDE_TEST_GOPLS")
	if tool == "" {
		tool, err = exec.LookPath("gopls")
		if err != nil {
			fail("locate gopls", err)
		}
	}
	report.GoplsPath = tool
	version, err := exec.CommandContext(t.Context(), tool, "version").CombinedOutput()
	if err != nil {
		fail("gopls version", fmt.Errorf("%w: %s", err, version))
	}
	report.GoplsVersion = strings.TrimSpace(string(version))
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		fail("temporary module", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/gopls-resource\n\ngo 1.27\n"), 0644); err != nil {
		fail("write module", err)
	}
	client, err := Start(t.Context(), Options{Path: tool, Dir: root, Env: report.Environment})
	if err != nil {
		fail("start gopls", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Close(ctx); err != nil {
			t.Errorf("gopls cleanup: %v", err)
		}
	})
	report.GoplsPID = client.cmd.Process.Pid
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	initialized, err := client.Initialize(ctx, map[string]any{
		"processId": os.Getpid(), "rootUri": fileURI(root),
		"workspaceFolders": []any{map[string]any{"uri": fileURI(root), "name": "resource"}},
		"capabilities": map[string]any{
			"general":      map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace":    map[string]any{"configuration": true},
			"textDocument": map[string]any{"publishDiagnostics": map[string]any{"versionSupport": true}},
		},
		"initializationOptions": report.Initialization,
	})
	cancel()
	if err != nil {
		fail("initialize gopls", fmt.Errorf("%w; stderr: %s", err, client.Stderr()))
	}
	var capabilities struct {
		Capabilities struct {
			PositionEncoding   string          `json:"positionEncoding"`
			DiagnosticProvider json.RawMessage `json:"diagnosticProvider"`
			ExecuteCommand     struct {
				Commands []string `json:"commands"`
			} `json:"executeCommandProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(initialized, &capabilities); err != nil {
		fail("decode capabilities", err)
	}
	supportsMemory, supportsWorkspace := false, false
	for _, command := range capabilities.Capabilities.ExecuteCommand.Commands {
		supportsMemory = supportsMemory || command == "gopls.mem_stats"
		supportsWorkspace = supportsWorkspace || command == "gopls.workspace_stats"
	}
	if !supportsMemory || !supportsWorkspace || len(capabilities.Capabilities.DiagnosticProvider) == 0 || string(capabilities.Capabilities.DiagnosticProvider) == "null" || capabilities.Capabilities.PositionEncoding != "" && capabilities.Capabilities.PositionEncoding != "utf-16" {
		fail("required capabilities", fmt.Errorf("unsupported response: %s", initialized))
	}
	coordinator := NewCoordinator(client)
	uri := fileURI(filepath.Join(root, "lib_gox.go"))
	var epoch uint64
	profileDir := os.Getenv("GOXIDE_GOPLS_RESOURCE_PROFILE_DIR")
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{}}
	t.Cleanup(httpClient.CloseIdleConnections)
	if profileDir != "" {
		if !filepath.IsAbs(profileDir) {
			fail("profile directory", fmt.Errorf("must be absolute"))
		}
		if err := os.Mkdir(profileDir, 0755); err != nil {
			fail("create new profile directory", err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		raw, err := client.Request(ctx, "workspace/executeCommand", map[string]any{"command": "gopls.start_debugging", "arguments": []any{map[string]any{"Addr": "127.0.0.1:0"}}})
		cancel()
		if err != nil {
			fail("gopls.start_debugging", err)
		}
		var result struct{ URLs []string }
		if err := json.Unmarshal(raw, &result); err != nil || len(result.URLs) != 1 {
			fail("debug URL", fmt.Errorf("%s (%v)", raw, err))
		}
		debugURL, err := url.Parse(result.URLs[0])
		if err != nil || debugURL.Scheme != "http" || debugURL.Hostname() != "127.0.0.1" {
			fail("debug loopback URL", fmt.Errorf("%s (%v)", raw, err))
		}
		report.DebugURL = strings.TrimSuffix(result.URLs[0], "/")
		report.Measurement += "; optional public pprof heap profiles are allocation-stack samples, not GC root graphs; loopback debug server is enabled before warmup"
	}
	heapProfile := func() string {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, report.DebugURL+"/debug/pprof/heap", nil)
		if err != nil {
			fail("heap profile request", err)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			fail("heap profile GET", err)
		}
		defer response.Body.Close()
		const maxProfile = 32 << 20
		data, err := io.ReadAll(io.LimitReader(response.Body, maxProfile+1))
		if err != nil || response.StatusCode != http.StatusOK || len(data) == 0 || len(data) > maxProfile {
			fail("heap profile response", fmt.Errorf("status=%s bytes=%d error=%v", response.Status, len(data), err))
		}
		path := filepath.Join(profileDir, fmt.Sprintf("heap-%06d.pprof", report.CompletedEdits))
		if err := os.WriteFile(path, data, 0644); err != nil {
			fail("persist heap profile", err)
		}
		return path
	}
	exercise := func(value int) error {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		var text strings.Builder
		text.WriteString("package app\n")
		for i := 0; i < 16; i++ {
			literal := i
			if i == 15 {
				literal = value
			}
			fmt.Fprintf(&text, "func Value%d() int { return %d }\n", i, literal)
		}
		text.WriteString("func Use() int { return Value15() }\n")
		result, err := analysis.Analyze(ctx, analysis.Input{Sources: map[string][]byte{"lib.gox": []byte(text.String())}, PackagePath: "example.org/gopls-resource"})
		if err != nil {
			return err
		}
		generated, err := lower.Generate(ctx, result, lower.GenerationOptions{})
		if err != nil {
			return err
		}
		content := string(generated["example.org/gopls-resource/lib.gox"])
		if !strings.Contains(content, fmt.Sprintf("return %d", value)) {
			return fmt.Errorf("current body missing from generated output: %s", content)
		}
		epoch++
		if err := coordinator.Sync(ctx, Projection{Epoch: epoch, Files: map[string]string{uri: content}}); err != nil {
			return err
		}
		position, err := source.New(content).Position(strings.LastIndex(content, "Value15()")+1, source.UTF16)
		if err != nil {
			return err
		}
		hover, err := coordinator.Query(ctx, epoch, "textDocument/hover", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position})
		if err != nil {
			return err
		}
		if !bytes.Contains(hover, []byte("Value15")) || !bytes.Contains(hover, []byte("int")) {
			return fmt.Errorf("unexpected hover: %s", hover)
		}
		diagnostics, err := coordinator.Diagnostics(ctx, epoch, []string{uri})
		if err != nil {
			return err
		}
		var diagnosticReport struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(diagnostics[uri], &diagnosticReport); err != nil || len(diagnosticReport.Items) != 0 {
			return fmt.Errorf("unexpected diagnostics: %s (%v)", diagnostics[uri], err)
		}
		return nil
	}
	rss := func() (int64, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		data, err := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(report.GoplsPID)).Output()
		if err != nil {
			return 0, err
		}
		return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	}
	sample := func(phase string, idleFor time.Duration) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		workspace, err := coordinator.Query(ctx, epoch, "workspace/executeCommand", map[string]any{"command": "gopls.workspace_stats", "arguments": []any{}})
		cancel()
		if err != nil || !json.Valid(workspace) || string(workspace) == "null" {
			fail("gopls.workspace_stats", fmt.Errorf("%s (%v)", workspace, err))
		}
		before, err := rss()
		if err != nil {
			fail("RSS before GC", err)
		}
		ctx, cancel = context.WithTimeout(t.Context(), 30*time.Second)
		gcStart := time.Now()
		raw, err := coordinator.Query(ctx, epoch, "workspace/executeCommand", map[string]any{"command": "gopls.mem_stats", "arguments": []any{}})
		gcDuration := time.Since(gcStart)
		cancel()
		if err != nil {
			fail("gopls.mem_stats", err)
		}
		var memory struct{ HeapAlloc, HeapInUse, TotalAlloc uint64 }
		if err := json.Unmarshal(raw, &memory); err != nil || memory.HeapAlloc == 0 || memory.HeapInUse < memory.HeapAlloc || memory.TotalAlloc < memory.HeapAlloc {
			fail("memory response", fmt.Errorf("%s (%v)", raw, err))
		}
		after, err := rss()
		if err != nil {
			fail("RSS after GC", err)
		}
		point := goplsMemorySample{Phase: phase, CompletedEdits: report.CompletedEdits, ElapsedSeconds: time.Since(started).Seconds(), IdleSeconds: idleFor.Seconds(), RSSBeforeGCKiB: before, RSSAfterGCKiB: after, GCQuerySeconds: gcDuration.Seconds(), HeapAllocBytes: memory.HeapAlloc, HeapInUseBytes: memory.HeapInUse, TotalAllocBytes: memory.TotalAlloc, WorkspaceStats: workspace}
		if profileDir != "" && (phase == "warm" || phase == "editing" && (report.CompletedEdits == rounds/2 || report.CompletedEdits == rounds)) {
			point.HeapProfile = heapProfile()
		}
		report.Samples = append(report.Samples, point)
		if err := save(); err != nil {
			fail("save memory sample", err)
		}
		t.Logf("resource phase=%s edits=%d elapsed=%.1fs idle=%.1fs heap=%d inuse=%d totalalloc=%d RSS=%d->%dKiB", phase, point.CompletedEdits, point.ElapsedSeconds, point.IdleSeconds, point.HeapAllocBytes, point.HeapInUseBytes, point.TotalAllocBytes, before, after)
	}
	waitUntil := func(deadline time.Time) {
		delay := time.Until(deadline)
		if delay <= 0 {
			return
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-t.Context().Done():
			fail("observation canceled", t.Context().Err())
		case <-timer.C:
		}
	}
	for i := 0; i < warmup; i++ {
		if err := exercise(100000 + i); err != nil {
			fail("warmup", err)
		}
	}
	sample("warm", 0)
	editingStarted := time.Now()
	for i := 1; i <= rounds; i++ {
		waitUntil(editingStarted.Add(time.Duration(i) * time.Duration(editPeriodMS) * time.Millisecond))
		if err := exercise(100000 + warmup + i); err != nil {
			fail(fmt.Sprintf("edit %d", i), err)
		}
		report.CompletedEdits = i
		if i%sampleEvery == 0 || i == rounds {
			sample("editing", 0)
		}
	}
	report.EditingSeconds = time.Since(editingStarted).Seconds()
	report.ProjectionFileCount = len(coordinator.files)
	report.FinalDocumentVersion = coordinator.version
	if report.ProjectionFileCount != 1 || report.FinalDocumentVersion != int32(warmup+rounds) {
		fail("projection accounting", fmt.Errorf("files=%d version=%d", report.ProjectionFileCount, report.FinalDocumentVersion))
	}
	idleStarted := time.Now()
	for seconds := 20; seconds <= idleSeconds; seconds += 20 {
		// Each wait is at most 20 seconds; go test -v reports progress after it.
		// Memory commands force GC but do not parse another source version.
		waitUntil(idleStarted.Add(time.Duration(seconds) * time.Second))
		sample("idle", time.Since(idleStarted))
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Close(ctx)
	cancel()
	if err != nil {
		fail("close gopls", err)
	}
	select {
	case <-client.Done():
		report.ProcessReaped = true
	default:
		fail("close gopls", fmt.Errorf("process workers still running"))
	}
	report.Complete = true
	if err := save(); err != nil {
		fail("final report", err)
	}
	t.Logf("resource observation complete: %s; scope is gopls Client/Coordinator, not the complete LSP service", reportPath)
}
