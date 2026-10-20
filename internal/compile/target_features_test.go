package compile

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestUnavailableLibraryDoesNotLoadOrFallBack(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, project.ManifestName, `[go_mod]
module = "example.org/unavailable"
go = "1.27"
[lib]
name = "fmt"
path = "src"
required_features = ["library"]
[[bin]]
name = "app"
path = "cmd/app"
[features]
library = []
`)
	writeProjectFile(t, root, "src/lib.gox", "invalid source in an unavailable library")
	writeProjectFile(t, root, "cmd/app/main.gox", "package main\nfunc main() {}\n")
	result, err := PrepareProject(root, Options{Offline: true})
	if err != nil {
		t.Fatalf("unused unavailable library was loaded: %v", err)
	}
	featureGo(t, result.Root, "build", result.Entry)
	writeProjectFile(t, root, "cmd/app/main.gox", "package main\nuse fmt;\nfunc main() { fmt::Println(42) }\n")
	if _, err := PrepareProject(root, Options{Offline: true, Locked: true}); err == nil || !strings.Contains(err.Error(), "requires feature library") {
		t.Fatalf("unavailable library fell back to standard fmt: %v", err)
	}
}

func TestInactiveDependencyReservesStandardLibraryName(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, project.ManifestName, `[go_mod]
module = "example.org/inactive"
go = "1.27"
[[bin]]
name = "app"
path = "cmd/app"
[dependencies.fmt]
path = "not-present"
optional = true
`)
	writeProjectFile(t, root, "cmd/app/main.gox", "package main\nuse fmt;\nfunc main() { fmt::Println(42) }\n")
	if _, err := PrepareProject(root, Options{Offline: true}); err == nil || !strings.Contains(err.Error(), `dependency root "fmt" is not activated`) {
		t.Fatalf("inactive dependency did not reserve standard-library root: %v", err)
	}
}
