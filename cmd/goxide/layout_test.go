package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/project"
)

func layoutFixture(t *testing.T, features bool) (string, string) {
	base := t.TempDir()
	root := filepath.Join(base, "app")
	dep := filepath.Join(base, "dep")
	projectWrite(t, dep, map[string]string{"go.mod": "module example.org/layoutdep\n\ngo 1.27\n", "lib.go": "package dep\nfunc Value()string{return \"dependency\"}\n"})
	files := map[string]string{
		"go.mod": "module example.org/layout\n\ngo 1.27\n",
		"goxide.toml": `[[bin]]
name = 'one'
path = 'src/one.gox'

[[bin]]
name = 'two'
path = 'src/two.gox'

[[bin]]
name = 'web'
path = 'cmd/web'

[dependencies]
[dependencies.dep]
module = 'example.org/layoutdep'
path = '../dep'

[[example]]
name = 'demo'
path = 'examples/demo.gox'

[features]
default = ['extra']
extra = []

[lib]
name = 'lib'
path = 'src/lib.gox'

[go_mod]
`,
		"src/lib.gox":         "package lib\nuse dep as d;\nfunc Value()string{if cfg!(feature(\"extra\")){return \"on \"+d::Value()};return \"off \"+d::Value()}\n",
		"src/one.gox":         "package main\nuse fmt;\nuse lib as l;\nfunc main(){fmt::Println(l::Value())}\n",
		"src/two.gox":         "package main\nuse fmt;\nfunc main(){fmt::Println(\"two\")}\n",
		"examples/demo.gox":   "package main\nuse fmt;\nfunc main(){fmt::Println(\"example\")}\n",
		"cmd/web/main.go":     "package main\nimport \"fmt\"\nimport _ \"embed\"\n//go:embed message.txt\nvar message string\nfunc main(){fmt.Println(message)}\n",
		"cmd/web/message.txt": "embedded asset",
	}
	if !features {
		files["goxide.toml"] = "[go_mod]\n[[bin]]\nname='one'\npath='src/one.gox'\n[[bin]]\nname='two'\npath='src/two.gox'\n[[bin]]\nname='web'\npath='cmd/web'\n[dependencies.dep]\nmodule='example.org/layoutdep'\npath='../dep'\n"
		files["src/one.gox"] = "package main\nuse fmt;use dep as d;\nfunc main(){fmt::Println(d::Value())}\n"
		delete(files, "src/lib.gox")
		delete(files, "examples/demo.gox")
	}
	projectWrite(t, root, files)

	return root, dep
}
func runLayoutBinary(t *testing.T, path, want string) {
	t.Helper()
	b, e := exec.Command(path).CombinedOutput()
	if e != nil || strings.TrimSpace(string(b)) != want {
		t.Fatalf("%s: %s %v", path, b, e)
	}
}
func TestProjectManagedLayoutEndToEnd(t *testing.T) {
	root, dep := layoutFixture(t, true)
	build := func(args ...string) string {
		t.Helper()
		b, e := projectRun(t, root, append([]string{"build", "--offline"}, args...)...)
		if e != nil {
			t.Fatalf("%s %v", b, e)
		}
		return strings.TrimSpace(b)
	}
	first := build("--bin", "one")
	if !strings.HasPrefix(first, filepath.Join(root, "target", "bin")+string(filepath.Separator)) {
		t.Fatal(first)
	}
	runLayoutBinary(t, first, "on dependency")
	again := build("--bin", "one", "--locked")
	if again != first {
		t.Fatalf("stable final path changed: %s %s", first, again)
	}
	off := build("--bin", "one", "--no-default-features", "--locked")
	if off == first {
		t.Fatal("feature artifacts collided")
	}
	runLayoutBinary(t, off, "off dependency")
	if b, e := projectRun(t, root, "transpile", "--bin", "one", "--offline", "--locked"); e != nil {
		t.Fatalf("%s %v", b, e)
	} else {
		generated := strings.TrimSpace(b)
		oldMod, e := os.ReadFile(filepath.Join(generated, "go.mod"))
		if e != nil {
			t.Fatal(e)
		}
		module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), generated)
		if err != nil {
			t.Fatal(err)
		}
		if len(module.Replace) != 1 || module.Replace[0].Old.Path != "example.org/layoutdep" || !within(filepath.Join(filepath.Dir(generated), "deps"), filepath.Join(generated, module.Replace[0].New.Path)) {
			t.Fatalf("replace not generation-local: %+v", module.Replace)
		}
		projectWrite(t, dep, map[string]string{"lib.go": "package dep\nfunc Value()string{return \"new dependency\"}\n"})
		latest := build("--bin", "one", "--locked")
		if latest != first {
			t.Fatal("source-only change should retain configuration output path")
		}
		runLayoutBinary(t, latest, "on new dependency")
		// The stable bundle now reflects the current dependency and remains Go-buildable.
		artifact := filepath.Join(t.TempDir(), "stable-bundle")
		c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-buildvcs=false", "-mod=readonly", "-o", artifact, "./goxide_targets/bin/one")
		c.Dir = generated
		c.Env = project.GoEnv(true)
		if out, e := c.CombinedOutput(); e != nil {
			t.Fatalf("retained bundle: %s %v", out, e)
		}
		runLayoutBinary(t, artifact, "on new dependency")
		if b, e := os.ReadFile(filepath.Join(generated, "go.mod")); e != nil || !bytes.Equal(b, oldMod) {
			t.Fatal("stable go.mod unnecessarily changed", e)
		}
	}
	runLayoutBinary(t, build("--bin", "two"), "two")
	runLayoutBinary(t, build("--bin", "web"), "embedded asset")
	runLayoutBinary(t, build("--example", "demo"), "example")
	if out := build("--lib"); out != "" {
		t.Fatal(out)
	}
	// Switching output roots excludes both the old owned root and the new one.
	t.Setenv("GOXIDE_TARGET_DIR", "environment-output")
	envbin := build("--bin", "two")
	if !strings.Contains(envbin, filepath.Join(root, "environment-output")) {
		t.Fatal(envbin)
	}
	explicit := build("--bin", "two", "--target-dir", "chosen-output")
	if !strings.Contains(explicit, filepath.Join(root, "chosen-output")) {
		t.Fatal(explicit)
	}
	var markers int
	filepath.Walk(filepath.Join(root, "target"), func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.Name() == project.LayoutMarker {
			markers++
		}
		return nil
	})
	if markers != 1 {
		t.Fatalf("recursive copying detected: %d layout markers", markers)
	}
}
func TestProjectLayoutRefusesOutputBypasses(t *testing.T) {
	root, _ := layoutFixture(t, false)
	other := t.TempDir()
	l, e := project.OpenLayout(other, "example.org/other", "")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	for _, mode := range []string{"build", "transpile"} {
		output := filepath.Join(l.Root, "foreign-"+mode)
		if b, e := projectRun(t, root, mode, "--bin", "one", "--offline", "-o", output); e == nil {
			t.Fatalf("%s wrote into foreign active layout: %s", mode, b)
		}
		if _, e = os.Lstat(output); !os.IsNotExist(e) {
			t.Fatal("foreign output created")
		}
	}
	b, e := projectRun(t, root, "build", "--bin", "one", "--offline")
	if e != nil {
		t.Fatalf("%s %v", b, e)
	}
	path := strings.TrimSpace(b)
	if e = os.WriteFile(path, []byte("user edit"), 0755); e != nil {
		t.Fatal(e)
	}
	if _, e = projectRun(t, root, "build", "--bin", "one", "--offline"); e == nil || !strings.Contains(e.Error(), "modified binary") {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(path); string(b) != "user edit" {
		t.Fatal("edited artifact lost")
	}
	// Explicit transpile keeps supporting fresh nested output paths.
	nested := filepath.Join(t.TempDir(), "new", "nested", "bundle")
	if b, e = projectRun(t, root, "transpile", "--bin", "one", "--offline", "-o", nested); e != nil {
		t.Fatalf("%s %v", b, e)
	}
}
func TestProjectLayoutHostBoundaryAndUnknownTarget(t *testing.T) {
	root, _ := layoutFixture(t, false)
	if e := os.Mkdir(filepath.Join(root, "target"), 0755); e != nil {
		t.Fatal(e)
	}
	keep := filepath.Join(root, "target", "user-data")
	os.WriteFile(keep, []byte("safe"), 0644)
	if _, e := projectRun(t, root, "build", "--bin", "one", "--offline"); e == nil || !strings.Contains(e.Error(), "unmanaged target") {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(keep); string(b) != "safe" {
		t.Fatal("unknown data changed")
	}
	otherOS := "windows"
	if runtime.GOOS == otherOS {
		otherOS = "linux"
	}
	t.Setenv("GOOS", otherOS)
	t.Setenv("CGO_ENABLED", "0")
	output, e := projectRun(t, root, "build", "--bin", "one", "--offline", "--target-dir", "cross-output")
	if e != nil {
		t.Fatalf("toolchain default must ignore ambient GOOS: %v: %s", e, output)
	}
	if _, e := os.Stat(strings.TrimSpace(output)); e != nil {
		t.Fatalf("missing host binary: %v", e)
	}

}

func TestProjectLayoutConfigurationAndGoCaches(t *testing.T) {
	t.Setenv("CGO_CFLAGS", "-O2")
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"goxide.toml": "[go_mod]\nmodule = 'example.org/cache'\ngo = '1.27'\n[[bin]]\nname = 'app'\npath = '.'\n",
		"main.gox":    "package main\nfunc main() {}\n",
	})
	prepare := func() string {
		t.Helper()
		result, err := compile.PrepareProject(root, compile.Options{GoTool: filepath.Join(runtime.GOROOT(), "bin", "go"), Offline: true})
		if err != nil {
			t.Fatal(err)
		}
		return result.Root
	}
	ordinary := prepare()
	if ordinary != prepare() {
		t.Fatal("same configuration changed the prepared module path")
	}
	t.Setenv("CGO_CFLAGS", "-O1")
	if ordinary == prepare() {
		t.Fatal("C tool configuration not isolated")
	}
	// Artifact placement must not replace the caller's native Go cache paths.
	t.Setenv("GOCACHE", filepath.Join(t.TempDir(), "build-cache"))
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "module-cache"))
	env := strings.Join(project.GoEnv(true), "\n")
	for _, key := range []string{"GOCACHE", "GOMODCACHE"} {
		if !strings.Contains(env, key+"="+os.Getenv(key)+"\n") && !strings.HasSuffix(env, key+"="+os.Getenv(key)) {
			t.Fatal("Go cache overridden", key)
		}
	}
}
