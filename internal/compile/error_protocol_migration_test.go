package compile

import (
	"bytes"
	"fmt"
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

// This proof covers Error's struct methods using ordinary inherent impls.
// Enum-native error bridging and #[from] registration are separate contracts.
func TestProceduralErrorStructMigration(t *testing.T) {
	sdk := compilerSDKForTest(t)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	root := t.TempDir()
	macros, support := filepath.Join(root, "macros"), filepath.Join(root, "runtime")
	model, app := filepath.Join(root, "model"), filepath.Join(root, "app")
	fixture := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "proc_error_protocols", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", fixture("macros.gox"))
	writeProjectFile(t, macros, "derive.go", fixture("derive.go"))
	library := func(module, name, extra string) string {
		return fmt.Sprintf("[go_mod]\nmodule=%q\ngo=\"1.27\"\n[lib]\nname=%q\npath=\".\"\n%s", module, name, extra)
	}
	dependencies := "[dependencies.macros]\npath=\"../macros\"\n[dependencies.runtime]\npath=\"../runtime\"\n"
	writeProjectFile(t, support, project.ManifestName, library("example.org/error-runtime", "errorsupport", ""))
	writeProjectFile(t, support, "lib.gox", fixture("runtime.gox"))
	writeProjectFile(t, model, project.ManifestName, library("example.org/error-model", "model", dependencies))
	writeProjectFile(t, model, "lib.gox", fixture("model.gox"))
	writeProjectFile(t, app, project.ManifestName, "[go_mod]\nmodule=\"example.org/error-consumer\"\ngo=\"1.27\"\n[[bin]]\nname=\"app\"\npath=\".\"\n[dependencies.model]\npath=\"../model\"\n")
	writeProjectFile(t, app, "main.go", fixture("consumer.go"))
	writeProjectFile(t, app, "main_test.go", "package main\nimport \"testing\"\nfunc TestNativeErrorConsumer(t *testing.T){if err:=verify();err!=nil{t.Fatal(err)}}\n")
	calls := 0
	run := testWASI(t, func(request pm.Request) {
		if request.Kind() == pm.Derive {
			calls++
		}
	})
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("derive calls = %d, want 3", calls)
	}
	for _, operation := range []string{"build", "run", "test"} {
		args := []string{operation, "-mod=readonly"}
		if operation == "build" {
			args = append(args, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(args, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "ordinary Error struct migration passed\n" {
			t.Fatalf("ordinary error consumer %s: %v\n%s", operation, err, output)
		}
	}
	snapshot := projectSnapshot(t, filepath.Dir(first.Root))
	lock, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(snapshot, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("locked replay changed ordinary error implementations or macro artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed error dependency lock: %v", err)
	}
	for _, test := range []struct{ name, source, want string }{
		{"missing-message", "#[derive(macros::LibraryError)] pub struct E{}", "requires a message"},
		{"unknown-field", "#[derive(macros::LibraryError)] #[library_error(\"{missing}\")] pub struct E{value int}", "unknown error placeholder"},
		{"unclosed", "#[derive(macros::LibraryError)] #[library_error(\"{value\")] pub struct E{value int}", "unclosed error placeholder"},
		{"closing", "#[derive(macros::LibraryError)] #[library_error(\"value}\")] pub struct E{}", "unmatched closing brace"},
		{"duplicate-message", "#[derive(macros::LibraryError)] #[library_error(\"a\")] #[library_error(\"b\")] pub struct E{}", "duplicate library_error"},
		{"duplicate-source", "#[derive(macros::LibraryError)] #[library_error(\"a\")] pub struct E{#[library_source] a error;#[library_source] b error}", "at most one source"},
		{"source-position", "#[derive(macros::LibraryError)] #[library_error(\"a\")] #[library_source] pub struct E{cause error}", "source belongs to a field"},
		{"field-message", "#[derive(macros::LibraryError)] #[library_error(\"a\")] pub struct E{#[library_error(\"b\")] cause error}", "message belongs to the struct"},
		{"source-type", "#[derive(macros::LibraryError)] #[library_error(\"a\")] pub struct E{#[library_source] cause int}", "does not satisfy"},
		{"defined-source", "type Cause interface{Error() string};#[derive(macros::LibraryError)] #[library_error(\"a\")] pub struct E{#[library_source] cause Cause}", "does not satisfy"},
		{"defined-scalar", "type Count int;#[derive(macros::LibraryError)] #[library_error(\"{value}\")] pub struct E{value Count}", "does not satisfy"},
		{"complex", "#[derive(macros::LibraryError)] #[library_error(\"{value}\")] pub struct E{value complex128}", "does not satisfy"},
		{"method-collision", "#[derive(macros::LibraryError)] #[library_error(\"a\")] pub struct E{};func (E) Error() string{return \"manual\"}", `duplicate declaration of "Error"`},
		{"unwrap-collision", "#[derive(macros::LibraryError)] #[library_error(\"a\")] pub struct E{#[library_source] cause error};func (E) Unwrap() error{return nil}", `duplicate declaration of "Unwrap"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := filepath.Join(root, "bad-"+test.name)
			writeProjectFile(t, bad, project.ManifestName, library("example.org/error-"+test.name, "model", dependencies))
			writeProjectFile(t, bad, "lib.gox", "package model\nuse macros;\nuse runtime;\n"+test.source+"\n")
			result, err := PrepareProject(bad, Options{Offline: true, Lib: true, RunProcMacro: run})
			if err == nil || result != nil || !strings.Contains(err.Error(), "example.org/error-"+test.name+"/lib.gox:") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid ordinary error %s: result=%v err=%v; want %q", test.name, result, err, test.want)
			}
			if (test.name == "source-type" || test.name == "defined-source") && !strings.Contains(err.Error(), "*error") {
				t.Fatalf("source rejected for an unrelated constraint: %v", err)
			}
		})
	}
}
