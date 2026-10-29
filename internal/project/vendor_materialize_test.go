package project

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSuppliedCrossPlatformLicensesAndCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "nested-licenses", true: "collision"}[collision], func(t *testing.T) {
			root := sourceTempDir(t)
			files := map[string]string{
				"go.mod":                              "module example.org/app\ngo 1.27\n",
				"vendor/modules.txt":                  "# example.org/a v1.0.0\n## explicit; go 1.27\nexample.org/a\n# example.org/a/sub v1.0.0\n## explicit; go 1.27\nexample.org/a/sub/pkg\n",
				"vendor/example.org/LICENSE":          "shared license",
				"vendor/example.org/a/a.go":           "package a\n",
				"vendor/example.org/a/a_js.go":        "//go:build js && wasm\npackage a\nimport _ \"syscall/js\"\n",
				"vendor/example.org/a/sub/LICENSE":    "nested license",
				"vendor/example.org/a/sub/pkg/pkg.go": "package pkg\n",
			}
			if collision {
				files["vendor/example.org/a/supplied-licenses/"+Digest("example.org/LICENSE")[:16]+"-LICENSE"] = "must not overwrite"
			}
			for p, b := range files {
				target := filepath.Join(root, p)
				if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(target, []byte(b), 0640); e != nil {
					t.Fatal(e)
				}
			}
			snapshot, e := InspectVendor(root, Manifest{GoVersion: "1.27"})
			if e != nil {
				t.Fatal(e)
			}
			out := t.TempDir()
			a, e := MaterializeSupplied(filepath.Join(runtime.GOROOT(), "bin", "go"), root, out, "1.27", snapshot, nil)
			if collision {
				if e == nil || !strings.Contains(e.Error(), "collision") {
					t.Fatalf("expected collision: %v", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			b, e := os.ReadFile(filepath.Join(out, a.Paths["example.org/a/sub"], "LICENSE"))
			if e != nil || string(b) != "nested license" {
				t.Fatalf("nested license: %s %v", b, e)
			}
			for rel, outputs := range a.Outputs {
				for _, p := range outputs {
					b, e := os.ReadFile(filepath.Join(out, p))
					if e != nil || string(b) != files["vendor/"+rel] {
						t.Fatalf("file loss %s: %v", rel, e)
					}
				}
			}
		})
	}
}

func TestOldVendorLocksRejected(t *testing.T) {
	for _, raw := range []string{`{"version":2,"vendor":null}`, `{"version":4,"native_workspace":{"vendor":{}}}`} {
		var lock Lock
		if err := lock.UnmarshalJSON([]byte(raw)); err == nil || !strings.Contains(err.Error(), "re-import") {
			t.Fatalf("old source policy accepted: %v", err)
		}
	}
}
