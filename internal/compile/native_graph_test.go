package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
)

func TestDisabledCgoImportsDoNotJoinCrateGraph(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	root := t.TempDir()
	writeProjectFile(t, root, project.ManifestName, `[go_mod]
module = "example.org/cgograph"
go = "1.27"
[[bin]]
name = "app"
path = "cmd/app"
`)
	writeProjectFile(t, root, "cmd/app/main.gox", "package main\nuse fmt;\nfunc main() { fmt::Println(42) }\n")
	const native = `package main
import "C"
import _ "example.org/cgograph/outside"
`
	writeProjectFile(t, root, "cmd/app/native.go", native)
	writeProjectFile(t, root, "outside/value.go", "package outside\n")
	result, err := PrepareProject(root, Options{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := featureGo(t, result.Root, "run", result.Entry); got != "42\n" {
		t.Fatalf("disabled cgo program output = %q", got)
	}
	data, err := os.ReadFile(filepath.Join(result.Root, "cmd/app/native.go"))
	if err != nil || string(data) != native {
		t.Fatalf("native cgo source changed: %v\n%s", err, data)
	}
	if _, err := os.Stat(filepath.Join(result.Root, "outside/value.go")); !os.IsNotExist(err) {
		t.Fatalf("disabled cgo import selected another crate package: %v", err)
	}
}

func TestFileTargetNativeImportKeepsPrivateHelper(t *testing.T) {
	for _, test := range []struct {
		name, library, wantError string
	}{
		{name: "without_library"},
		{name: "with_file_library", library: "func Value() int { return 1 }"},
		{name: "private_library_helper", library: "mod helper;", wantError: "not publicly reachable"},
		{name: "public_library_helper", library: "pub mod helper;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifest := `[go_mod]
module = "example.org/nativefile"
go = "1.27"
[[bin]]
name = "app"
path = "src/app.go"
`
			if test.library != "" {
				manifest += "[lib]\nname = \"core\"\npath = \"src/lib.gox\"\n"
				writeProjectFile(t, root, "src/lib.gox", "package core\n"+test.library+"\n")
			}
			writeProjectFile(t, root, project.ManifestName, manifest)
			const native = `package main
import "fmt"
import "example.org/nativefile/src/helper"
func main() { fmt.Println(helper.Value()) }
`
			writeProjectFile(t, root, "src/app.go", native)
			writeProjectFile(t, root, "src/helper/value.go", "package helper\nfunc Value() int { return 42 }\n")
			result, err := PrepareProject(root, Options{Offline: true})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("private library import error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := featureGo(t, result.Root, "run", result.Entry); got != "42\n" {
				t.Fatalf("native file target output = %q", got)
			}
			data, err := os.ReadFile(filepath.Join(result.Root, "goxide_targets/bin/app/app.go"))
			if err != nil || string(data) != native {
				t.Fatalf("native file target changed: %v\n%s", err, data)
			}
		})
	}
}
