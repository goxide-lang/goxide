package compile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestFileTargetsKeepSelectedEntriesAndChildPackageIdentity(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, project.ManifestName, `[go_mod]
module = "example.org/filetargets"
go = "1.27"
[lib]
name = "core"
path = "src/lib.gox"
[[bin]]
name = "one"
path = "src/one.gox"
[[bin]]
name = "two"
path = "src/two.gox"
`)
	writeProjectFile(t, root, "src/lib.gox", `package core
pub mod model;
use crate::model;
func Make() model::Item { return model::Item{Value: 42} }
`)
	writeProjectFile(t, root, "src/model/model.gox", "package model\ntype Item struct { Value int }\n")
	// This file shares the source directory, but is not any target entry.
	writeProjectFile(t, root, "src/unselected.gox", "invalid unselected source")
	for _, name := range []string{"one", "two"} {
		value := "1"
		if name == "two" {
			value = "2"
		}
		child := name + "part"
		writeProjectFile(t, root, "src/"+child+"/value.gox", "package helper\nfunc Value() int { return "+value+" }\n")
		writeProjectFile(t, root, "src/"+name+".gox", "package main\nmod "+child+";\nuse crate::"+child+" as helper;\nuse core;\nuse core::model;\nuse fmt;\nfunc main() { var item model::Item = core::Make(); fmt::Println(item.Value + helper::Value()) }\n")
	}
	var library []byte
	for _, name := range []string{"one", "two"} {
		t.Run(name, func(t *testing.T) {
			first, err := PrepareProject(root, Options{Bin: name, Offline: true})
			if err != nil {
				t.Fatal(err)
			}
			if first.Entry != "./goxide_targets/bin/"+name {
				t.Fatalf("selected file target = %s", first.Entry)
			}
			want := "43\n"
			if name == "two" {
				want = "44\n"
			}
			if got := featureGo(t, first.Root, "run", first.Entry); got != want {
				t.Fatalf("selected entry = %q, want %q", got, want)
			}
			output, err := os.ReadFile(filepath.Join(first.Root, "goxide_targets/bin", name, name+".go"))
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range []string{"example.org/filetargets", "example.org/filetargets/src/model", "example.org/filetargets/src/" + name + "part"} {
				if !strings.Contains(string(output), `"`+imported+`"`) {
					t.Fatalf("missing canonical import %s:\n%s", imported, output)
				}
			}
			contents, err := os.ReadFile(filepath.Join(first.Root, "lib.go"))
			if err != nil {
				t.Fatal(err)
			}
			if library == nil {
				library = contents
			} else if !bytes.Equal(library, contents) {
				t.Fatal("selecting another file target changed the shared library")
			}
			snapshot := projectSnapshot(t, first.Root)
			second, err := PrepareProject(root, Options{Bin: name, Offline: true, Locked: true})
			if err != nil {
				t.Fatal(err)
			}
			if first.Root != second.Root || !reflect.DeepEqual(snapshot, projectSnapshot(t, second.Root)) {
				t.Fatal("same file target changed generated path or bytes")
			}
		})
	}
	if t.Failed() {
		return
	}
	result, err := PrepareProject(root, Options{Lib: true, Offline: true, Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entry != "." && result.Entry != "./" {
		t.Fatalf("file library package = %s", result.Entry)
	}
	featureGo(t, result.Root, "build", result.Entry)
}
