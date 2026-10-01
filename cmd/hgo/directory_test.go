package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirectoryBuildProtectsSourceModule(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cmd")
	os.Mkdir(src, 0755)
	mod := []byte("module example.org/app\ngo 1.27\n")
	os.WriteFile(filepath.Join(root, "go.mod"), mod, 0600)
	os.WriteFile(filepath.Join(src, "main.hgo"), []byte("package main\nfunc main(){}\n"), 0600)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, dest := range []string{filepath.Join(src, ".hidden"), filepath.Join(root, "go.mod"), filepath.Join(root, "new-binary")} {
		var log bytes.Buffer
		err := run([]string{"build", "-go", tool, "-o", dest, src}, &log, &log)
		if err == nil || (!strings.Contains(err.Error(), "outside") && !strings.Contains(err.Error(), "overwrite")) {
			t.Fatalf("%s: %v (%s)", dest, err, log.String())
		}
	}
	got, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	if !bytes.Equal(got, mod) {
		t.Fatal("source module changed")
	}
	if _, err := os.Stat(filepath.Join(src, ".hidden")); !os.IsNotExist(err) {
		t.Fatal("created hidden source output")
	}
}
