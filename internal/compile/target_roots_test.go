package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestDirectoryTargetsCannotSelectTheSamePackage(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, kinds := range [][2]string{{"bin", "bin"}, {"example", "example"}, {"lib", "bin"}, {"lib", "example"}, {"bin", "example"}} {
		t.Run(kinds[0]+"-"+kinds[1], func(t *testing.T) {
			node := &project.Node{Root: root, Module: "example.org/duplicate"}
			var selected project.Target
			for index, kind := range kinds {
				target := project.Target{Name: []string{"one", "two"}[index], Path: []string{"src", "./src"}[index]}
				switch kind {
				case "lib":
					node.Manifest.Lib = &target
				case "bin":
					node.Manifest.Bins = append(node.Manifest.Bins, target)
				case "example":
					node.Manifest.Examples = append(node.Manifest.Examples, target)
				}
				selected = target
			}
			_, _, err := targetCrates(node, selected, kinds[1])
			if err == nil || !strings.Contains(err.Error(), "select the same package example.org/duplicate/src") || !strings.Contains(err.Error(), "target one") || !strings.Contains(err.Error(), "target two") {
				t.Fatalf("duplicate target roots were not rejected: %v", err)
			}
		})
	}
}

func TestLibraryNamespaceNamesBuild(t *testing.T) {
	for _, name := range []string{"_library", "工具"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, project.ManifestName, "[go_mod]\nmodule='example.org/namedlib'\ngo='1.27'\n[lib]\nname='"+name+"'\npath='src'\n[[bin]]\nname='app'\npath='cmd/app'\n")
			writeProjectFile(t, root, "src/lib.gox", "package library\nfunc Value() int { return 42 }\n")
			writeProjectFile(t, root, "cmd/app/main.gox", "package main\nuse "+name+";\nuse fmt;\nfunc main() { fmt::Println("+name+"::Value()) }\n")
			library, err := PrepareProject(root, Options{Lib: true, Offline: true})
			if err != nil {
				t.Fatal(err)
			}
			featureGo(t, library.Root, "build", library.Entry)
			binary, err := PrepareProject(root, Options{Offline: true, Locked: true})
			if err != nil {
				t.Fatal(err)
			}
			if got := featureGo(t, binary.Root, "run", binary.Entry); got != "42\n" {
				t.Fatalf("library namespace %s returned %q", name, got)
			}
		})
	}
}

func TestProjectedLibraryCannotShareDirectoryTargetRoot(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "src/lib.gox", "package library\n")
	for _, kind := range []string{"bin", "example"} {
		t.Run(kind, func(t *testing.T) {
			target := project.Target{Name: "app", Path: "."}
			node := &project.Node{
				Root: root, Module: "example.org/duplicate",
				Manifest: project.Manifest{Lib: &project.Target{Name: "core", Path: "src/lib.gox"}},
			}
			if kind == "bin" {
				node.Manifest.Bins = []project.Target{target}
			} else {
				node.Manifest.Examples = []project.Target{target}
			}
			_, _, err := targetCrates(node, target, kind)
			if err == nil || !strings.Contains(err.Error(), "select the same package example.org/duplicate") {
				t.Fatalf("projected file library shared a target root: %v", err)
			}
		})
	}
}
