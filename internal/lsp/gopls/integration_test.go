package gopls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type diagnostic struct {
	Message string `json:"message"`
}

type publishedDiagnostics struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

func startGopls(t *testing.T, files map[string]string) (*Client, string, <-chan Notification) {
	t.Helper()
	path := os.Getenv("GOXIDE_TEST_GOPLS")
	if path == "" {
		var err error
		path, err = exec.LookPath("gopls")
		if err != nil {
			t.Skip("gopls not found in PATH; set GOXIDE_TEST_GOPLS to run protocol integration tests")
		}
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = "module example.test/lsp\n\ngo 1.27\n"
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	events := make(chan Notification, 256)
	lifetime, stop := context.WithCancel(context.Background())
	c, err := Start(lifetime, Options{
		Path: path, Dir: root,
		Env: []string{"GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off"},
		Notification: func(ctx context.Context, n Notification) {
			select {
			case events <- n:
			case <-ctx.Done():
			}
		},
	})
	if err != nil {
		stop()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("gopls cleanup: %v; stderr: %s", err, c.Stderr())
		}
		select {
		case <-c.Done():
		case <-ctx.Done():
			t.Error("gopls process or notification worker was not reaped")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	result, err := c.Initialize(ctx, map[string]any{
		"processId": os.Getpid(),
		"rootUri":   fileURI(root),
		"workspaceFolders": []any{
			map[string]any{"uri": fileURI(root), "name": "contract"},
		},
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true},
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{"versionSupport": true},
			},
			"window": map[string]any{"workDoneProgress": true},
		},
		"initializationOptions": map[string]any{
			"pullDiagnostics": true, "fileWatcher": "off", "diagnosticsDelay": "0s",
			"diagnosticsTrigger": "Edit", "verboseWorkDoneProgress": true,
		},
	})
	if err != nil {
		t.Fatalf("initialize gopls: %v; stderr: %s", err, c.Stderr())
	}
	var initialized struct {
		Capabilities struct {
			DiagnosticProvider *struct {
				InterFileDependencies bool `json:"interFileDependencies"`
				WorkspaceDiagnostics  bool `json:"workspaceDiagnostics"`
			} `json:"diagnosticProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initialized); err != nil {
		t.Fatal(err)
	}
	provider := initialized.Capabilities.DiagnosticProvider
	if provider == nil || !provider.InterFileDependencies || provider.WorkspaceDiagnostics {
		t.Fatalf("unexpected pull diagnostic capability: %s", result)
	}
	return c, root, events
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func openGo(t *testing.T, c *Client, path, text string, version int) {
	t.Helper()
	if err := c.Notify(t.Context(), "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(path), "languageId": "go", "version": version, "text": text},
	}); err != nil {
		t.Fatal(err)
	}
}

func changeGo(t *testing.T, c *Client, path, text string, version int) {
	t.Helper()
	if err := c.Notify(t.Context(), "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": fileURI(path), "version": version},
		"contentChanges": []any{map[string]any{"text": text}},
	}); err != nil {
		t.Fatal(err)
	}
}

func pull(t *testing.T, c *Client, path string) []diagnostic {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	result, err := c.Request(ctx, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(path)},
	})
	if err != nil {
		t.Fatalf("pull diagnostics: %v; stderr: %s", err, c.Stderr())
	}
	var report struct {
		Kind             string                     `json:"kind"`
		Items            []diagnostic               `json:"items"`
		ResultID         string                     `json:"resultId"`
		RelatedDocuments map[string]json.RawMessage `json:"relatedDocuments"`
	}
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}
	// v0.24.0 leaves kind empty. The coordinator normalizes this experimental
	// response; a low-level transport must preserve it without inventing data.
	if report.Kind != "" && report.Kind != "full" {
		t.Fatalf("unsupported diagnostic report: %s", result)
	}
	if report.ResultID != "" || len(report.RelatedDocuments) != 0 {
		t.Fatalf("gopls pull contract changed; review generation coordination: %s", result)
	}
	return report.Items
}

func diagnosticContains(diagnostics []diagnostic, text string) bool {
	for _, d := range diagnostics {
		if strings.Contains(d.Message, text) {
			return true
		}
	}
	return false
}

func TestGoplsVirtualPackageAndClose(t *testing.T) {
	main := "package main\nimport \"example.test/lsp/virtual\"\nvar Value int = virtual.Value\nfunc main() {}\n"
	c, root, _ := startGopls(t, map[string]string{"main.go": main})
	mainPath := filepath.Join(root, "main.go")
	virtualPath := filepath.Join(root, "virtual", "generated.go")
	openGo(t, c, mainPath, main, 1)
	openGo(t, c, virtualPath, "package virtual\nconst Value = 7\n", 1)
	if diagnostics := pull(t, c, mainPath); len(diagnostics) != 0 {
		t.Fatalf("new overlay-only package was not loaded: %+v", diagnostics)
	}
	if _, err := os.Stat(filepath.Dir(virtualPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("virtual package unexpectedly exists on disk: %v", err)
	}
	result, err := c.Request(t.Context(), "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(mainPath)},
		"position":     map[string]int{"line": 2, "character": 25},
	})
	if err != nil || !strings.Contains(string(result), fileURI(virtualPath)) {
		t.Fatalf("definition in virtual package: %s, %v", result, err)
	}
	if err := c.Notify(t.Context(), "textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(virtualPath)},
	}); err != nil {
		t.Fatal(err)
	}
	if diagnostics := pull(t, c, mainPath); !diagnosticContains(diagnostics, "could not import") {
		t.Fatalf("closing virtual package did not remove it: %+v", diagnostics)
	}
}

func TestGoplsTombstoneMasksDiskAndLastPackageFile(t *testing.T) {
	main := "package main\nimport \"example.test/lsp/p\"\nvar _ = p.A\nvar _ = p.B\nfunc main() {}\n"
	a, b := "package p\nconst A = 1\n", "package p\nconst B = 2\n"
	c, root, _ := startGopls(t, map[string]string{"main.go": main, "p/a_gox.go": a, "p/b_gox.go": b})
	mainPath, aPath, bPath := filepath.Join(root, "main.go"), filepath.Join(root, "p/a_gox.go"), filepath.Join(root, "p/b_gox.go")
	openGo(t, c, mainPath, main, 1)
	const tombstone = "//go:build ignore && !ignore\n\npackage p\n"
	openGo(t, c, aPath, tombstone, 1)
	if diagnostics := pull(t, c, mainPath); !diagnosticContains(diagnostics, "undefined: p.A") || diagnosticContains(diagnostics, "p.B") {
		t.Fatalf("one masked file: %+v", diagnostics)
	}
	if err := c.Notify(t.Context(), "textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(aPath)},
	}); err != nil {
		t.Fatal(err)
	}
	if diagnostics := pull(t, c, mainPath); len(diagnostics) != 0 {
		t.Fatalf("didClose should restore disk declarations: %+v", diagnostics)
	}
	openGo(t, c, aPath, tombstone, 2)
	openGo(t, c, bPath, tombstone, 1)
	if diagnostics := pull(t, c, mainPath); !diagnosticContains(diagnostics, "could not import") {
		t.Fatalf("masking the last package file kept stale disk declarations: %+v", diagnostics)
	}
	for path, want := range map[string]string{aPath: a, bPath: b} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("disk output changed: %s, %v", path, err)
		}
	}
}

func TestGoplsUnchangedVersionPushAndOrderedPull(t *testing.T) {
	a := "package p\nfunc F(x int) int { return 0 }\n"
	b := "package p\nfunc G() int { return 0 }\n"
	unchanged := "package p\nvar Value = F(G())\n"
	c, root, events := startGopls(t, map[string]string{"a.go": a, "b.go": b, "c.go": unchanged})
	aPath, bPath, cPath := filepath.Join(root, "a.go"), filepath.Join(root, "b.go"), filepath.Join(root, "c.go")
	openGo(t, c, aPath, a, 1)
	openGo(t, c, bPath, b, 1)
	openGo(t, c, cPath, unchanged, 1)
	if diagnostics := pull(t, c, cPath); len(diagnostics) != 0 {
		t.Fatalf("initial diagnostics: %+v", diagnostics)
	}
	changeGo(t, c, aPath, "package p\nfunc F(x string) int { return 0 }\n", 2)
	// The diagnostic itself, not an elapsed sleep or progress completion,
	// proves that gopls published the inconsistent dependency state.
	badPush := awaitNotification(t, events, func(n Notification) bool {
		var p publishedDiagnostics
		_ = json.Unmarshal(n.Params, &p)
		return n.Method == "textDocument/publishDiagnostics" && p.URI == fileURI(cPath) && diagnosticContains(p.Diagnostics, "cannot use")
	})
	var bad publishedDiagnostics
	_ = json.Unmarshal(badPush.Params, &bad)
	if bad.Version != 1 {
		t.Fatalf("unchanged C version: %d", bad.Version)
	}
	if diagnostics := pull(t, c, cPath); !diagnosticContains(diagnostics, "cannot use") {
		t.Fatalf("pull of first input generation: %+v", diagnostics)
	}
	changeGo(t, c, bPath, "package p\nfunc G() string { return \"\" }\n", 2)
	if diagnostics := pull(t, c, cPath); len(diagnostics) != 0 {
		t.Fatalf("pull of repaired input generation: %+v", diagnostics)
	}
	goodPush := awaitNotification(t, events, func(n Notification) bool {
		var p publishedDiagnostics
		_ = json.Unmarshal(n.Params, &p)
		return n.Method == "textDocument/publishDiagnostics" && p.URI == fileURI(cPath) && len(p.Diagnostics) == 0
	})
	var good publishedDiagnostics
	_ = json.Unmarshal(goodPush.Params, &good)
	if good.Version != bad.Version {
		t.Fatalf("the push version unexpectedly distinguishes dependency states: %d != %d", good.Version, bad.Version)
	}
	changeGo(t, c, aPath, "package p\nfunc F(x bool) int { return 0 }\n", 3)
	changeGo(t, c, bPath, "package p\nfunc G() bool { return false }\n", 3)
	if diagnostics := pull(t, c, cPath); len(diagnostics) != 0 {
		t.Fatalf("pull after consecutive A/B updates saw an intermediate state: %+v", diagnostics)
	}
}

func TestGoplsCancellationKeepsSessionUsable(t *testing.T) {
	c, root, _ := startGopls(t, map[string]string{"p.go": "package p\n"})
	path := filepath.Join(root, "large.go")
	var source strings.Builder
	source.WriteString("package p\n")
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&source, "var Item%d = struct { Value int }{Value: %d}\n", i, i)
	}
	openGo(t, c, path, source.String(), 1)
	// A cold type check gives the real server work to cancel. Exact cancellation
	// ID delivery is asserted separately by the controlled subprocess test.
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := c.Request(ctx, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(path)},
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("diagnostic deadline: %v", err)
	}
	if diagnostics := pull(t, c, path); len(diagnostics) != 0 {
		t.Fatalf("request cancellation damaged the session: %+v", diagnostics)
	}
}
