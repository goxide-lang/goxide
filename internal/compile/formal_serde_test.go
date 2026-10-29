package compile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func TestFormalSerdePublishedProtocols(t *testing.T) {
	source, dependencies := formalSerdePins(t)
	formalSerdeAcceptance(t, source, dependencies)
}

func TestFormalSerdePublishedRejections(t *testing.T) {
	_, dependencies := formalSerdePins(t)
	formalSerdeRejections(t, dependencies)
}

func formalSerdePins(t *testing.T) (string, string) {
	t.Helper()

	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{}
	for _, dependency := range module.Require {
		versions[dependency.Path] = dependency.Version
	}
	const serde = "github.com/goxide-lang/serde"
	const standard = "github.com/goxide-lang/std"
	if versions[serde] == "" || versions[standard] == "" {
		t.Fatal("formal serde acceptance requires compiler std and serde pins")
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "mod", "download", "-json", serde+"@"+versions[serde])
	command.Env = project.GoEnv(true)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("load pinned serde source: %v", err)
	}
	var downloaded struct{ Dir string }
	if err := json.Unmarshal(output, &downloaded); err != nil || downloaded.Dir == "" {
		t.Fatalf("pinned serde source directory: %v\n%s", err, output)
	}
	dependencies := fmt.Sprintf("[dependencies.serde]\ngo=%q\nversion=%q\n[dependencies.standard]\ngo=%q\nversion=%q\n", serde, versions[serde], standard, versions[standard])
	return downloaded.Dir, dependencies
}

func formalSerdeAcceptance(t *testing.T, source, dependencies string) {
	t.Helper()
	compilerSDKForTest(t)
	root := t.TempDir()
	fixture := filepath.Join(source, "testdata", "formal")
	if err := filepath.WalkDir(fixture, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(fixture, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeProjectFile(t, root, relative, string(data))
		return nil
	}); err != nil {
		t.Fatalf("formal serde fixtures from pinned source: %v", err)
	}
	for _, module := range []string{"leaf", "model", "app"} {
		kind := "[lib]"
		if module == "app" {
			kind = "[[bin]]"
		}
		manifest := fmt.Sprintf("[go_mod]\nmodule=\"example.org/formal-serde-%s\"\ngo=\"1.27\"\n%s\nname=%q\npath=\".\"\n", module, kind, module) + dependencies
		if module == "model" {
			manifest += "[dependencies.leaf]\npath=\"../leaf\"\n"
		}
		if module == "app" {
			manifest += "[dependencies.model]\npath=\"../model\"\n"
		}
		writeProjectFile(t, filepath.Join(root, module), project.ManifestName, manifest)
	}
	deriveCalls := 0
	run := testWASI(t, func(request pm.Request) {
		if request.Kind() == pm.Derive {
			deriveCalls++
		}
	})
	app := filepath.Join(root, "app")
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if deriveCalls != 41 {
		t.Fatalf("formal serde WASI derives = %d, want 41", deriveCalls)
	}
	configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "consumer"), first.Entry)
	if got := configuredGo(t, first, "run", "-mod=readonly", first.Entry); got != "formal serde protocols passed\n" {
		t.Fatalf("formal serde Go consumer output = %q", got)
	}
	configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
	before := projectSnapshot(t, filepath.Dir(first.Root))
	lock, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("locked replay changed formal serde artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed formal serde lock: %v", err)
	}
}

func formalSerdeRejections(t *testing.T, dependencies string) {
	t.Helper()
	compilerSDKForTest(t)
	run := testWASI(t, nil)
	rejections := []struct{ name, source, diagnostic string }{
		{"missing-codec", `struct NoCodec{}; #[derive(serde::derive::Serialize)] struct MissingCodec{Value NoCodec}`, "Serialize"},
		{"wrong-direction", `#[derive(serde::derive::Deserialize)] struct Inner{}; #[derive(serde::derive::Serialize)] struct WrongDirection{Value Inner}`, "Serialize"},
		{"missing-default", `struct NoDefault{}; #[derive(serde::derive::Deserialize)] struct MissingDefault{#[serde(skip)]Value NoDefault}`, "Default"},
		{"non-string-key", `#[derive(serde::derive::Serialize)] struct NonStringKey{Value map[int]int}`, "Serialize"},
	}
	for _, test := range rejections {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeProjectFile(t, directory, project.ManifestName, "[go_mod]\nmodule=\"example.org/formal-serde-negative\"\ngo=\"1.27\"\n[lib]\nname=\"negative\"\npath=\".\"\n"+dependencies)
			writeProjectFile(t, directory, "lib.gox", "package negative\nuse serde;\n"+test.source+"\n")
			_, err := PrepareProject(directory, Options{Lib: true, Offline: true, RunProcMacro: run})
			if err == nil || !strings.Contains(err.Error(), "lib.gox:3:") || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("expected source-located %s protocol failure: %v", test.diagnostic, err)
			}
		})
	}
}
