package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestStandaloneSourceMacrosUseSelectedSupportGraph(t *testing.T) {
	provider := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "go.mod" {
			data = bytes.Replace(data, []byte("module github.com/goxide-lang/goxide"), []byte("module support.example/selection"), 1)
		}
		if err := os.WriteFile(filepath.Join(provider, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The provider supplies dependency selections, never application source.
	singleFileWrite(t, provider, "unrelated.go", "this is not valid Go\n")
	providerBefore := singleFileSnapshot(t, provider)
	source := t.TempDir()
	fixtureRootsFile(t, source, "[roots.std]\nmodule='github.com/goxide-lang/std'\n")
	singleFileWrite(t, source, "ignored.gox", "this sibling must not be selected\n")
	input := filepath.Join(source, "input.gox")
	singleFileWrite(t, source, "input.gox", `package main
use std;
#[derive(std::derive::Default)]
struct Settings { Value int }
func main(){value:=std::default_value[Settings]();std::println!("value={}",value.Value)}
`)
	cli, tool := formalTestCLI(t), filepath.Join(runtime.GOROOT(), "bin", "go")
	temporary := t.TempDir()
	invoke := func(success bool, args ...string) string {
		t.Helper()
		command := exec.Command(cli, args...)
		command.Dir, command.Env = source, append(localEnv(), "TMPDIR="+temporary)
		output, err := command.CombinedOutput()
		if (err == nil) != success {
			t.Fatalf("goxide %v: %v\n%s", args, err, output)
		}
		entries, err := os.ReadDir(temporary)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("standalone compilation left temporary files: %v", entries)
		}
		return string(output)
	}
	sourceBefore := singleFileSnapshot(t, source)
	if output := invoke(true, "run", "--runtime", provider, "--go", tool, input); output != "value=0\n" {
		t.Fatalf("selected support output = %q", output)
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		output := filepath.Join(t.TempDir(), "output.go")
		expanded := filepath.Join(t.TempDir(), "expanded.gox")
		invoke(true, "transpile", "--runtime", provider, "--go", tool, "-o", output, "--expanded", expanded, input)
		generated, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if attempt > 0 && !bytes.Equal(previous, generated) {
			t.Fatal("temporary support module changed generated single-file bytes")
		}
		previous = generated
		expansion, err := os.ReadFile(expanded)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(expansion, []byte("struct Settings")) || !bytes.Contains(expansion, []byte("func main")) || bytes.Contains(expansion, []byte("#[derive")) {
			t.Fatalf("standalone expanded output did not use the original input key:\n%s", expansion)
		}
	}
	if !reflect.DeepEqual(sourceBefore, singleFileSnapshot(t, source)) {
		t.Fatal("standalone compilation changed the source directory")
	}
	for _, test := range []struct{ name, source, location, message string }{
		{"macro", "package main\nuse std;\nfunc main(){std::println!(42)}\n", ":3:", "print format must be a string literal"},
		{"type", "package main\nuse std;\n#[derive(std::derive::Default)]\nstruct Settings { Value int }\nfunc main(){var value int=\"bad\";_=value}\n", ":5:", "cannot use"},
	} {
		t.Run(test.name, func(t *testing.T) {
			singleFileWrite(t, source, "input.gox", test.source)
			before := singleFileSnapshot(t, source)
			output := invoke(false, "run", "--runtime", provider, "--go", tool, input)
			if !strings.Contains(output, input+test.location) || !strings.Contains(output, test.message) || strings.Contains(output, "goxide-standalone-") {
				t.Fatalf("standalone diagnostic lost original input: %s", output)
			}
			if !reflect.DeepEqual(before, singleFileSnapshot(t, source)) {
				t.Fatal("failed standalone compilation changed the source directory")
			}
		})
	}
	if !reflect.DeepEqual(providerBefore, singleFileSnapshot(t, provider)) {
		t.Fatal("standalone compilation changed the support manifest provider")
	}
}
