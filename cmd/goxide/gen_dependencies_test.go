package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestGenerationDependencyConfirmation(t *testing.T) {
	proxy, _ := versionProxy(t)
	oldStd, oldSerde := stdSuggestedVersion, serdeSuggestedVersion
	stdSuggestedVersion, serdeSuggestedVersion = "v0.1.0", "v0.2.0"
	t.Cleanup(func() { stdSuggestedVersion, serdeSuggestedVersion = oldStd, oldSerde })
	for module, version := range map[string]string{"github.com/goxide-lang/std": stdSuggestedVersion, "github.com/goxide-lang/serde": serdeSuggestedVersion} {
		publishVersion(t, proxy, module, version, map[string]string{
			"go.mod":   "module " + module + "\ngo 1.27\n",
			"value.go": "package lib\nfunc Value() int { return 42 }\n",
		})
	}
	for _, tc := range []struct {
		name, answer string
		interactive  bool
		accept       bool
	}{
		{"accept", "yes\n", true, true},
		{"reject", "n\n", true, false},
		{"empty", "\n", true, false},
		{"eof", "", true, false},
		{"eof_after_yes", "yes", true, false},
		{"nonterminal", "yes\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			singleFileWrite(t, root, "go.mod", "module example.org/confirmation\n\ngo 1.27\n")
			before := singleFileSnapshot(t, root)
			missing := &missingGenerationDependencies{Root: root, Modules: []string{"github.com/goxide-lang/std", "github.com/goxide-lang/serde"}, Config: testdeps.Configuration()}
			var stderr bytes.Buffer
			err := addGenerationDependencies(CompileRequest{Interactive: tc.interactive, PromptInput: strings.NewReader(tc.answer)}, missing, &stderr)
			if tc.accept {
				if err != nil {
					t.Fatalf("accept: %v\n%s", err, stderr.String())
				}
				mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
				if err != nil || !bytes.Contains(mod, []byte("github.com/goxide-lang/std v0.1.0")) || !bytes.Contains(mod, []byte("github.com/goxide-lang/serde v0.2.0")) {
					t.Fatalf("selected dependencies not published: %v\n%s", err, mod)
				}
				sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
				if err != nil || !bytes.Contains(sum, []byte("github.com/goxide-lang/std v0.1.0 h1:")) || !bytes.Contains(sum, []byte("github.com/goxide-lang/serde v0.2.0 h1:")) {
					t.Fatalf("download checksums not published: %v\n%s", err, sum)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "go get github.com/goxide-lang/serde@v0.2.0 github.com/goxide-lang/std@v0.1.0") {
					t.Fatalf("missing manual command: %v", err)
				}
				if after := singleFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
					t.Fatal("unconfirmed request changed module files")
				}
			}
			if got := strings.Contains(stderr.String(), "[y/N]"); got != tc.interactive {
				t.Fatalf("interactive prompt = %v, want %v: %s", got, tc.interactive, stderr.String())
			}
		})
	}
}

func TestGenerationDependencyFailurePreservesModule(t *testing.T) {
	proxy, _ := versionProxy(t)
	old := stdSuggestedVersion
	stdSuggestedVersion = "v0.1.0"
	t.Cleanup(func() { stdSuggestedVersion = old })
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "download_failure", true: "concurrent_edit"}[concurrent], func(t *testing.T) {
			root := t.TempDir()
			singleFileWrite(t, root, "go.mod", "module example.org/preserve\n\ngo 1.27\n")
			missing := &missingGenerationDependencies{Root: root, Modules: []string{"github.com/goxide-lang/std"}, Config: testdeps.Configuration()}
			before := singleFileSnapshot(t, root)
			var input io.Reader = strings.NewReader("y\n")
			if concurrent {
				publishVersion(t, proxy, "github.com/goxide-lang/std", "v0.1.0", map[string]string{"go.mod": "module github.com/goxide-lang/std\ngo 1.27\n", "value.go": "package std\n"})
				input = generationPromptReader(func() {
					singleFileWrite(t, root, "go.mod", "module example.org/preserve\n\ngo 1.27\n// concurrent edit\n")
					before = singleFileSnapshot(t, root)
				})
			}
			var stderr bytes.Buffer
			err := addGenerationDependencies(CompileRequest{Interactive: true, PromptInput: input}, missing, &stderr)
			if err == nil {
				t.Fatal("unexpected successful dependency update")
			}
			if concurrent && !strings.Contains(err.Error(), "configuration changed") {
				t.Fatalf("concurrent update diagnostic: %v", err)
			}
			if after := singleFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("failed update changed authored module files")
			}
		})
	}
}

type generationPromptReader func()

func (reader generationPromptReader) Read(p []byte) (int, error) {
	reader()
	return copy(p, "yes\n"), nil
}

func TestGenerationDependencyRequiresStampedVersion(t *testing.T) {
	old := stdSuggestedVersion
	stdSuggestedVersion = ""
	t.Cleanup(func() { stdSuggestedVersion = old })
	missing := &missingGenerationDependencies{Modules: []string{"github.com/goxide-lang/std"}}
	var stderr bytes.Buffer
	err := addGenerationDependencies(CompileRequest{Interactive: true, PromptInput: strings.NewReader("yes\n")}, missing, &stderr)
	if err == nil || !strings.Contains(err.Error(), "std@VERSION") || !strings.Contains(err.Error(), "scripts/build.go") || stderr.Len() != 0 {
		t.Fatalf("unstamped compiler attempted install: %v\n%s", err, stderr.String())
	}
}
