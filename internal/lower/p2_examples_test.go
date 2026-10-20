package lower_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

func TestP2ManualDefaultExample(t *testing.T) {
	module := prepareP2Example(t, "manual_default", "manualdefault.example/service")
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "service 8080 service 8080 service 9090 SCSC\n" {
		t.Fatalf("manual_default output = %q", got)
	}
}

func TestP2MacroPackagesExample(t *testing.T) {
	module := prepareP2Example(t, "macro_packages", "macros.example/app")
	runGo(t, module, "build", "./...")
	lines := strings.Split(strings.TrimSpace(runGo(t, module, "run", ".")), "\n")
	if len(lines) != 2 || lines[1] != "workers 4 <nil> 102" {
		t.Fatalf("macro_packages output = %q", lines)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatal(err)
	}
	if event["msg"] != "request" || event["request_id"] != float64(42) || event["workers"] != float64(4) {
		t.Fatalf("macro log event = %#v", event)
	}
}

func prepareP2Example(t *testing.T, example, modulePath string) string {
	t.Helper()
	source := filepath.Join("..", "..", "examples", example)
	var modules []string
	for range 2 {
		module, err := lower.PrepareModule([]string{source}, modulePath, testConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(module) })
		modules = append(modules, module)
	}
	err := filepath.WalkDir(modules[0], func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(modules[0], name)
		if err != nil {
			return err
		}
		first, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		second, err := os.ReadFile(filepath.Join(modules[1], relative))
		if err != nil {
			return err
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: repeated example generation changed %s", example, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return modules[0]
}
