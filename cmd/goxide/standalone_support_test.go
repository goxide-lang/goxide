package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestStandaloneSupportModule(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	provider := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		// Any module may select matching public support dependencies. The
		// compiler's source/module identity is not a requirement.
		if name == "go.mod" {
			data = bytes.Replace(data, []byte("module github.com/hxsf/goxide"), []byte("module support.example/selection"), 1)
		}
		if err := os.WriteFile(filepath.Join(provider, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	generated := t.TempDir()
	if err := writeStandaloneSupportModule(tool, provider, generated); err != nil {
		t.Fatal(err)
	}
	// The generated module remains buildable after its manifest provider goes
	// away. Its public dependencies are verified remote pins, not local copies.
	if err := os.RemoveAll(provider); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(generated, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"github.com/hxsf/goxide", "github.com/spf13", "github.com/pelletier", "replace", provider} {
		if bytes.Contains(mod, []byte(forbidden)) {
			t.Fatalf("non-support dependency leaked into generated module: %s", mod)
		}
	}
	compiled, err := frontend.Compile(filepath.Join(generated, "main.gox"), []byte(`package main
import rt "github.com/goxide-lang/std/runtime"
func fail()(result int){
 guard:=rt.Assignment[int]{}
 defer guard.Catch(&result)
 guard.Fail(42)
 return 0
}
func main(){println!("value={}",fail())}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compiled.Go, []byte("github.com/goxide-lang/std/formatting")) {
		t.Fatalf("generated printing did not import std/formatting: %s", compiled.Go)
	}
	if err := os.WriteFile(filepath.Join(generated, "main.go"), compiled.Go, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list", "-mod=readonly", "-m", "all"}, {"list", "-buildvcs=false", "-mod=readonly", "-deps", "./..."}, {"run", "-buildvcs=false", "-mod=readonly", "."}} {
		cmd := exec.Command(tool, args...)
		cmd.Dir, cmd.Env = generated, localEnv()
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, output)
		}
		if strings.Contains(string(output), "github.com/hxsf/goxide") {
			t.Fatalf("generated module depends on compiler: %s", output)
		}
		if args[0] == "run" && string(output) != "value=42\n" {
			t.Fatalf("program output = %q", output)
		}
	}
}

func TestStandaloneSupportManifestSelection(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, tc := range []struct {
		name, manifest, want string
	}{
		{"missing-serde", "module selector.example\ngo 1.27\nrequire github.com/goxide-lang/std v0.0.0\n", "must explicitly require pinned"},
		{"wrong-owner", "module github.com/hxsf/goxide\ngo 1.27\n", "must explicitly require pinned"},
		{"relative-replace", "module selector.example\ngo 1.27\nrequire (\ngithub.com/goxide-lang/std v0.0.0\ngithub.com/goxide-lang/serde v0.0.0\n)\nreplace github.com/goxide-lang/std => ./std\nreplace github.com/goxide-lang/serde => ./serde\nreplace unrelated.example/cli => ./not-present\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, generated := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(provider, "go.mod"), []byte(tc.manifest), 0600); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"std", "serde"} {
				directory := filepath.Join(provider, name)
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module github.com/goxide-lang/"+name+"\ngo 1.27\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := writeStandaloneSupportModule(tool, provider, generated)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			mod, err := os.ReadFile(filepath.Join(generated, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(mod, []byte(filepath.ToSlash(filepath.Join(provider, "std")))) || bytes.Contains(mod, []byte("unrelated.example")) {
				t.Fatalf("wrong replacement selection: %s", mod)
			}
		})
	}
}

func TestStandaloneSupportForkGraph(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	provider, generated := t.TempDir(), t.TempDir()
	files := map[string]string{
		"go.mod": `module selector.example
go 1.27
require (
 github.com/goxide-lang/std v0.0.0
 github.com/goxide-lang/serde v0.0.0
 example.org/supportwriter v1.1.0
 unrelated.example/cli v1.0.0
)
replace github.com/goxide-lang/std => ./std
replace github.com/goxide-lang/serde => ./serde
replace example.org/supportwriter v1.0.0 => ./writer-old
replace example.org/supportwriter v1.1.0 => ./writer-new
replace unrelated.example/cli => ./does-not-exist
exclude unrelated.example/cli v0.9.0
`,
		"std/go.mod":          "module github.com/goxide-lang/std\ngo 1.27\nrequire example.org/supportwriter v1.0.0\n",
		"std/value.go":        "package std\nimport w \"example.org/supportwriter\"\nfunc Value()int{return w.Value()}\n",
		"serde/go.mod":        "module github.com/goxide-lang/serde\ngo 1.27\n",
		"writer-old/go.mod":   "module example.org/supportwriter\ngo 1.27\n",
		"writer-old/value.go": "package supportwriter\nfunc Value()int{return 1}\n",
		"writer-new/go.mod":   "module example.org/supportwriter\ngo 1.27\n",
		"writer-new/value.go": "package supportwriter\nfunc Value()int{return 2}\n",
	}
	for name, source := range files {
		path := filepath.Join(provider, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeStandaloneSupportModule(tool, provider, generated); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(generated, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(mod, []byte("example.org/supportwriter v1.1.0")) || bytes.Contains(mod, []byte("unrelated.example")) {
		t.Fatalf("lost reachable selection or retained unrelated CLI module: %s", mod)
	}
	if err := os.WriteFile(filepath.Join(generated, "main.go"), []byte("package main\nimport(\"fmt\";s \"github.com/goxide-lang/std\")\nfunc main(){fmt.Println(s.Value())}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tool, "run", "-buildvcs=false", "-mod=readonly", ".")
	cmd.Dir, cmd.Env = generated, localEnv()
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "2\n" {
		t.Fatalf("fork graph program: %v: %s", err, output)
	}
	// A real support -> compiler edge is a cycle in the intended ownership
	// graph, not an unrelated provider requirement that can simply be dropped.
	files["std/go.mod"] += "require github.com/hxsf/goxide v0.0.0\n"
	files["go.mod"] += "replace github.com/hxsf/goxide => ./compiler\n"
	files["compiler/go.mod"] = "module github.com/hxsf/goxide\ngo 1.27\n"
	for _, name := range []string{"go.mod", "std/go.mod", "compiler/go.mod"} {
		path := filepath.Join(provider, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[name]), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeStandaloneSupportModule(tool, provider, t.TempDir()); err == nil || !strings.Contains(err.Error(), "must not depend on the compiler") {
		t.Fatalf("support -> compiler edge accepted: %v", err)
	}
}
