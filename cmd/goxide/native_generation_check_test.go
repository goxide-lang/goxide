package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeGenerationRequiresPublishedGoArtifacts(t *testing.T) {
	for _, name := range []string{"missing_stable_api", "missing_macro_anchor", "missing_runtime_package"} {
		module := "example.org/library"
		librarySource := "package library\npub func Value() int { return 42 }\n"
		caller := "library::Value()"
		want := "stable Go API"
		if name == "missing_macro_anchor" {
			want = "no loadable Go package"
			librarySource = "package library\npub macro_rules! answer { () => { 42 }; }\n"
			caller = "library::answer!()"
		} else if name == "missing_runtime_package" {
			module = "github.com/goxide-lang/std"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			library, app := filepath.Join(base, "library"), filepath.Join(base, "app")
			singleFileWrite(t, library, "go.mod", "module "+module+"\ngo 1.27\n")
			singleFileWrite(t, library, "goxide.toml", "[go_mod]\nmodule='"+module+"'\ngo='1.27'\n[lib]\nname='library'\npath='.'\n")
			singleFileWrite(t, library, "lib.gox", librarySource)
			if name == "missing_stable_api" {
				singleFileWrite(t, library, "api.go", "package library\n")
			}
			singleFileWrite(t, app, "go.mod", "module example.org/app\ngo 1.27\nrequire "+module+" v1.0.0\nreplace "+module+" => ../library\n")
			singleFileWrite(t, app, "main.gox", fmt.Sprintf("package main\nuse library;\nfunc main(){println(%s)}\n", caller))
			for _, mode := range []string{"package", "file", "stdout", "custom_file"} {
				t.Run(mode, func(t *testing.T) {
					arguments := []string{"gen"}
					input := filepath.Join(app, "main.gox")
					switch mode {
					case "package":
						input = app
					case "stdout":
						arguments = append(arguments, "-o", "-")
					case "custom_file":
						arguments = append(arguments, "-o", filepath.Join(base, "result.go"))
					}
					before := singleFileSnapshot(t, base)
					var stdout, stderr bytes.Buffer
					err := run(append(arguments, input), &stdout, &stderr)
					if mode == "stdout" || mode == "custom_file" {
						if err != nil {
							t.Fatalf("prepared-graph generation: %v\n%s", err, &stderr)
						}
						readOutput := func() []byte {
							if mode == "stdout" {
								return bytes.Clone(stdout.Bytes())
							}
							data, err := os.ReadFile(filepath.Join(base, "result.go"))
							if err != nil {
								t.Fatal(err)
							}
							return data
						}
						first := readOutput()
						if !bytes.Contains(first, []byte("package main")) {
							t.Fatalf("missing Go output: %s", first)
						}
						if name == "missing_macro_anchor" && (!strings.Contains(stderr.String(), "goxide_tools") || !strings.Contains(stderr.String(), module)) {
							t.Fatalf("missing generation dependency advice: %s", &stderr)
						}
						stdout.Reset()
						stderr.Reset()
						if mode == "custom_file" {
							if err := os.Remove(filepath.Join(base, "result.go")); err != nil {
								t.Fatal(err)
							}
						}
						if err := run(append(arguments, input), &stdout, &stderr); err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(first, readOutput()) {
							t.Fatal("generation output changed between identical invocations")
						}
						if mode == "custom_file" {
							if err := os.Remove(filepath.Join(base, "result.go")); err != nil {
								t.Fatal(err)
							}
						}
						if !reflect.DeepEqual(before, singleFileSnapshot(t, base)) {
							t.Fatal("prepared-graph generation changed authored files")
						}
						return
					}
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("missing publication diagnostic: %v\n%s", err, &stderr)
					}
					if strings.Contains(err.Error(), "which is not in this module") || strings.Contains(err.Error(), "go get ") {
						t.Fatalf("declared library was mistaken for a missing dependency: %v", err)
					}
					if stdout.Len() != 0 || !reflect.DeepEqual(before, singleFileSnapshot(t, base)) {
						t.Fatal("failed original-graph check published output or changed authored files")
					}
				})
			}
		})
	}
}
