package main

import (
	"bytes"
	"github.com/hxsf/goxide/internal/config"
	"go/importer"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateChecksNativeBodiesBeforePublishing(t *testing.T) {
	root := t.TempDir()
	singleFileWrite(t, root, "go.mod", "module example.org/gencheck\n\ngo 1.27\n")
	singleFileWrite(t, root, "go.sum", "")
	singleFileWrite(t, root, "api.gox", `package p
#[cfg(feature("on"))]
func Hidden() int { return 42 }
`)
	singleFileWrite(t, root, "state.gox", "package p\nconst Enabled = cfg!(feature(\"on\"))\n")
	singleFileWrite(t, root, "native.go", "package p\nfunc Native() int { return identity(Hidden()) }\n")
	singleFileWrite(t, root, "helper.go", `package p
import "fmt"
func identity[T any](value T) T { _ = fmt.Sprint(value); return value }
func init() { panic("generation must not run native bodies") }
`)
	before := singleFileSnapshot(t, root)
	var output bytes.Buffer
	if err := run([]string{"gen", "--features=on", root}, &output, &output); err != nil {
		t.Fatalf("enabled package generation: %v\n%s", err, &output)
	}
	build := func() {
		t.Helper()
		command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "./...")
		command.Dir = root
		command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build generated package: %v\n%s", err, output)
		}
	}
	build()
	enabled := singleFileSnapshot(t, root)
	for name, contents := range before {
		if enabled[name] != contents {
			t.Fatalf("generation changed original file %s", name)
		}
	}
	if len(enabled) != len(before)+2 {
		t.Fatalf("expected exactly two generated files, got %v", enabled)
	}
	output.Reset()
	err := run([]string{"gen", root}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "native.go:2:") || !strings.Contains(err.Error(), "undefined: Hidden") {
		t.Fatalf("disabled API reference lacks native body diagnostic: %v\n%s", err, &output)
	}
	if after := singleFileSnapshot(t, root); !reflect.DeepEqual(enabled, after) {
		t.Fatal("failed native body check changed package files or generated output")
	}
	build()
}

func TestCheckGeneratedPackageRetainsLineDirectives(t *testing.T) {
	root := t.TempDir()
	native := filepath.Join(root, "native.go")
	source := filepath.Join(root, "value.gox")
	err := checkGeneratedPackage("example.org/lines", map[string][]byte{
		native: []byte("package p\nfunc Native() int { return Value() }\n"),
		source: []byte("source syntax must not be parsed as Go"),
	}, map[string][]byte{
		source: []byte("//line value.gox:1\npackage p\n//line value.gox:23\nfunc Value() int { return \"wrong\" }\n"),
	}, importer.Default(), config.Config{}.Sizes())
	if err == nil || !strings.Contains(err.Error(), source+":23:") || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("generated Go diagnostic lost its source location: %v", err)
	}
}
