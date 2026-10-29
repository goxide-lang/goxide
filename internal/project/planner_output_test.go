package project

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestModulePlanningReportsOfflineDownloadFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/app\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "GOMODCACHE="+t.TempDir(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
	_, err := goCommand(filepath.Join(runtime.GOROOT(), "bin", "go"), root, true, environment,
		"mod", "download", "-json", "example.org/unavailable@v1.0.0")
	for _, want := range []string{"example.org/unavailable", "v1.0.0", "module lookup disabled by GOPROXY=off"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("download failure must retain %q: %v", want, err)
		}
	}
}
