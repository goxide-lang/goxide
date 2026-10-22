package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExportDirectoryModuleRetainsLocalDependencies(t *testing.T) {
	bundle := t.TempDir()
	projectWrite(t, bundle, map[string]string{
		"module/go.mod": "module example.org/exportapp\ngo 1.27\n" +
			"require (\nexample.org/exportlib v0.0.0\nexample.org/exportvalue v0.0.0\n)\n" +
			"replace example.org/exportlib => ../deps/lib\nreplace example.org/exportvalue => ../deps/value\n",
		"module/main.go": "package main\nimport (\"fmt\"; \"example.org/exportlib\")\nfunc main() { fmt.Println(exportlib.Value()) }\n",
		"deps/lib/go.mod": "module example.org/exportlib\ngo 1.27\n" +
			"require example.org/exportvalue v0.0.0\nreplace example.org/exportvalue => ../value\n",
		"deps/lib/lib.go":     "package exportlib\nimport \"example.org/exportvalue\"\nfunc Value() int { return exportvalue.Value + 1 }\n",
		"deps/value/go.mod":   "module example.org/exportvalue\ngo 1.27\n",
		"deps/value/value.go": "package exportvalue\nconst Value = 41\n",
	})
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	destination := filepath.Join(t.TempDir(), "export")
	if err := exportDirectoryModule(tool, filepath.Join(bundle, "module"), bundle, destination, os.Environ()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(bundle); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{destination, filepath.Join(destination, ".goxide-deps", "lib")} {
		command := exec.Command(tool, "build", "-mod=readonly", "./...")
		command.Dir, command.Env = root, localEnv()
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build relocated module %s: %v\n%s", root, err, output)
		}
	}
	command := exec.Command(tool, "run", "-mod=readonly", ".")
	command.Dir, command.Env = destination, localEnv()
	if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("run relocated module: %v\n%s", err, output)
	}
}
