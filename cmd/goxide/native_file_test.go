package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeGoSingleFileCLI(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	const source = "package main\nimport (\"fmt\"; \"os\")\nvar use = 7\nfunc main(){fmt.Printf(\"%d %q\\n\",use,os.Args[1:])}\n"
	want := "7 [\"--flag\" \"\" \"x y\" \"$(literal)\"]\n"
	arguments := []string{"--flag", "", "x y", "$(literal)"}
	for _, configured := range []bool{false, true} {
		name := "standalone"
		if configured {
			name = "module"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "args.go")
			singleFileWrite(t, dir, "args.go", source)
			// Native Go does not interpret Goxide dependency roots, and explicit
			// file compilation must not select either sibling source language.
			singleFileWrite(t, dir, "goxide.roots.toml", "invalid roots [[[\n")
			singleFileWrite(t, dir, "poison.go", "package main\ninvalid sibling\n")
			singleFileWrite(t, dir, "poison.gox", "invalid sibling\n")
			if configured {
				singleFileWrite(t, dir, "go.mod", "module example.org/nativefile\ngo 1.27\n")
			}
			for _, mode := range []string{"transpile", "expand"} {
				var out, diagnostic bytes.Buffer
				mapFile := filepath.Join(t.TempDir(), "source.map.json")
				if err := run([]string{mode, "--source-map", mapFile, input}, &out, &diagnostic); err != nil {
					t.Fatalf("%s: %v: %s", mode, err, &diagnostic)
				}
				if out.String() != source {
					t.Fatalf("native source changed: %q", out.String())
				}
				data, err := os.ReadFile(mapFile)
				if err != nil || !json.Valid(data) || !bytes.Contains(data, []byte(`"Origin"`)) {
					t.Fatalf("native source map: %v: %s", err, data)
				}
			}
			var out, diagnostic bytes.Buffer
			argv := []string{"run", "--go", tool, "--runtime", repo, input, "--"}
			if err := run(append(argv, arguments...), &out, &diagnostic); err != nil || out.String() != want {
				t.Fatalf("run: %v stdout=%q stderr=%s", err, out.String(), &diagnostic)
			}
			binary := filepath.Join(t.TempDir(), "program")
			out.Reset()
			diagnostic.Reset()
			if err := run([]string{"build", "--go", tool, "--runtime", repo, "-o", binary, input}, &out, &diagnostic); err != nil {
				t.Fatalf("build: %v: %s", err, &diagnostic)
			}
			actual, err := exec.Command(binary, arguments...).CombinedOutput()
			if err != nil || string(actual) != want {
				t.Fatalf("built program: %v: %q", err, actual)
			}
			if err := run([]string{"transpile", "-o", input, input}, &out, &diagnostic); err == nil || !strings.Contains(err.Error(), "overwrite input") {
				t.Fatalf("native output protection: %v", err)
			}
		})
	}
}

func TestNativeGoSingleFileSyntaxBoundary(t *testing.T) {
	for _, tc := range []struct{ name, source, diagnostic string }{
		{"old.gox", "package main\nimport \"fmt\"\nfunc main(){fmt.Println(1)}\n", "use"},
		{"use.go", "package main\nuse fmt;\nfunc main(){fmt::Println(1)}\n", "expected declaration"},
		{"enum.go", "package main\nenum State{A,B,}\nfunc main(){}\n", "expected declaration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			singleFileWrite(t, dir, tc.name, tc.source)
			var out bytes.Buffer
			err := run([]string{"transpile", filepath.Join(dir, tc.name)}, &out, &out)
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("got %v, want %q", err, tc.diagnostic)
			}
		})
	}
	dir := t.TempDir()
	singleFileWrite(t, dir, "main.go", "package main\nfunc main(){}\n")
	var out bytes.Buffer
	err := run([]string{"transpile", "--roots", filepath.Join(dir, "missing.toml"), filepath.Join(dir, "main.go")}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "native .go uses Go imports") {
		t.Fatalf("explicit roots on native Go: %v", err)
	}
}
