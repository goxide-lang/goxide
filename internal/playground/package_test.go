package playground

import (
	"go/importer"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/typecheck"
	testdeps "github.com/hxsf/goxide/tests/testutil"
)

func TestPackageUsesExplicitExportTargetAndEmptyEnvironment(t *testing.T) {
	t.Setenv("GOARCH", "arm64")
	environment, err := config.NewEnvironment(config.Target{OS: "linux", Arch: "386"}, []string{})
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Config: config.Config{Environment: environment}, Importer: importer.Default()}
	files, err := CompilePackage([]File{{"main.gox", `package p
const Is32 = cfg!(target_pointer_width("32"))
const HasProcessArch = cfg!(env("GOARCH"))
`}, {"other.gox", "package p\nconst Other = 1\n"}}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !strings.Contains(files[0].Source, "Is32 = true") || !strings.Contains(files[0].Source, "HasProcessArch = false") {
		t.Fatalf("wrong in-memory configuration: %v", files)
	}
	for _, file := range files {
		if !strings.Contains(file.Source, "//go:build linux && !android && 386") {
			t.Fatalf("missing package target guard: %s", file.Source)
		}
	}
	if _, err := CompilePackage([]File{{"large.gox", "package p\nvar TooLarge int = 1 << 40\n"}}, options); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("32-bit type checking used host sizes: %v", err)
	}
	if _, err := CompilePackage([]File{{"missing.gox", "package p\n"}}, Options{Importer: importer.Default()}); err == nil || !strings.Contains(err.Error(), "explicit export-data target") {
		t.Fatalf("missing browser target was guessed: %v", err)
	}
}

func TestPackageImplicitToolchainTrees(t *testing.T) {
	files := []File{{"main.gox", `package main
use encoding::json;
use encoding::BinaryMarshaler;
use archive::tar;
use r#go::ast;
use fmt;
func marker(value BinaryMarshaler) {}
func main() {
    value, _ := json::Marshal(map[string]int{"answer": 42})
    header := tar::Header{Name: "entry"}
    _ = ast::File{}
    fmt::Println(string(value), header.Name)
}
`}}
	options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()})}
	output, err := CompilePackage(files, options)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := CompilePackage(files, options)
	if err != nil || !reflect.DeepEqual(output, repeated) {
		t.Fatalf("toolchain package generation is unstable: %v", err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module playground\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, file := range output {
		if err := os.WriteFile(filepath.Join(directory, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "run", ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	stdout, err := command.CombinedOutput()
	if err != nil || string(stdout) != "{\"answer\":42} entry\n" {
		t.Fatalf("generated toolchain program: %s (%v)", stdout, err)
	}
}

func TestPackageAliasMemberKeepsSelectedOrigin(t *testing.T) {
	options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
		"fmt":       {Unavailable: "optional dependency fmt is inactive"},
		"actualfmt": {Source: "toolchain", PackagePath: "fmt"},
	}}
	files := []File{{"main.gox", "package main\nuse actualfmt::Println;\nfunc main() { Println(42) }\n"}}
	output, err := CompilePackage(files, options)
	if err != nil || len(output) != 1 || !strings.Contains(output[0].Source, `"fmt"`) {
		t.Fatalf("canonical member rebound to an inactive source root: %v, %v", output, err)
	}
	_, err = CompilePackage([]File{{"main.gox", `package main
macro_rules! generated { () => { use strings as fmt; }; }
generated!()
use fmt::Println;
func main() { Println(42) }
`}}, options)
	if err == nil || !strings.Contains(err.Error(), "optional dependency fmt is inactive") {
		t.Fatalf("hygienic alias bypassed an inactive source root: %v", err)
	}
}

func TestPackageUseRootsFollowAliasesAndMacroScopes(t *testing.T) {
	tests := map[string][]File{
		"declaration-shadows-standard-root": {{"main.gox", `package main
enum fmt { Unit }
use fmt::Unit;
func main() { _ = Unit }
`}},
		"hygienic-alias-does-not-shadow-root": {{"main.gox", `package main
macro_rules! generated { () => { use strings as actualfmt; }; }
generated!()
use actualfmt::Println;
func main() { Println(42) }
`}},
		"alias-shadows-standard-root": {{"main.gox", `package main
use encoding::json as encoding;
use encoding::Marshal;
func main() { _, _ = Marshal(42) }
`}},
		"macro-definition-file": {
			{"helpers.gox", `package main
use actualfmt as output;
macro_rules! generated { ($name:ident) => {
    use output::Println as $name;
}; }
`},
			{"main.gox", `package main
use strings as output;
generated!(print)
func main() { print(42); _ = output::ToUpper("ok") }
`},
		},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
				"actualfmt": {Source: "toolchain", PackagePath: "fmt"},
			}}
			if _, err := CompilePackage(files, options); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPackageRejectsMissingUseTargets(t *testing.T) {
	for _, target := range []string{"encoding::missing", "encoding::json::missing", "encoding::missing::*", "std::missing"} {
		t.Run(target, func(t *testing.T) {
			options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
				"std": {Source: "module", ModulePath: "github.com/goxide-lang/std", GoPrefix: "github.com/goxide-lang/std"},
			}}
			_, err := CompilePackage([]File{{"main.gox", "package main\nuse " + target + ";\nfunc main() {}\n"}}, options)
			if err == nil || !strings.Contains(err.Error(), `use target "missing" is not a public item or namespace`) {
				t.Fatalf("missing use target was accepted: %v", err)
			}
		})
	}
}

func TestPackageFilesScopeHelpersAndNativeBuild(t *testing.T) {
	files := []File{
		{"main.gox", `package p
use fmt as native;
func Value() string { native := Item{N: 6}; return native::Sprint(add!(native.N), ":", label()) }
`},
		{"model.gox", `package p

// The public dependency keeps its source comment after namespace resolution.
use serde;
#[derive(Clone,Debug,serde::Serialize)]
type Item struct { N int }
macro_rules! add { ($n:expr) => { $n + 1 }; }
`},
		{"label.gox", `package p

// This alias has a different target from the main file's native alias.
use strings as native;
func label() string { return native::ToUpper("ok") }
`},
	}
	options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
		"fmt":     {PackagePath: "fmt", Source: "toolchain"},
		"strings": {PackagePath: "strings", Source: "toolchain"},
		"serde":   {PackagePath: "github.com/goxide-lang/serde", ModulePath: "github.com/goxide-lang/serde", Source: "module"},
	}}
	output, err := CompilePackage(files, options)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module playground\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range output {
		names = append(names, file.Path)
		if file.Path == "model.go" && !strings.Contains(file.Source, "func CloneItem") {
			t.Fatal("derived clone source is missing")
		}
		if err := os.WriteFile(filepath.Join(dir, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(names, []string{"label.go", "main.go", "model.go"}) {
		t.Fatalf("outputs: %v", names)
	}
	// Directory CLI and in-memory WASM consume the same explicit context. Compare
	// every generated Go byte; declarations remain in their source files.
	input := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module playground\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(input, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := lower.CompileModules([]lower.Module{{Path: PackagePath, Dir: input, Roots: options.Roots, Config: options.Config}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(input)
	if err != nil {
		t.Fatal(err)
	}
	var moduleFiles []File
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") {
			data, err := os.ReadFile(filepath.Join(input, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			moduleFiles = append(moduleFiles, File{Path: entry.Name(), Source: string(data)})
		}
	}
	if !reflect.DeepEqual(moduleFiles, output) {
		t.Fatal("directory and in-memory package outputs differ")
	}
	if err := os.WriteFile(filepath.Join(dir, "output_test.go"), []byte(`package p
import "testing"
func TestOutput(t *testing.T) {
 if Value() != "7:OK" { t.Fatal(Value()) }
 v := Item{N: 8}; c := CloneItem(&v)
 if c.N != 8 || DebugItem(&c) != "Item { N: 8 }" { t.Fatal(c) }
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-mod=readonly", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local")
	if log, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated package: %v\n%s", err, log)
	}
	// A failed edit cannot contaminate declarations or import bindings in the next call.
	bad := append([]File(nil), files...)
	bad[2].Source = "package p\nfunc label()string{return native::Sprint(\"bad\")}\n"
	if _, err := CompilePackage(bad, options); err == nil || !strings.Contains(err.Error(), "label.gox") || !strings.Contains(err.Error(), "unresolved name") {
		t.Fatalf("import leaked from sibling: %v", err)
	}
	again, err := CompilePackage(files, options)
	if err != nil || !reflect.DeepEqual(output, again) {
		t.Fatalf("recovery changed outputs: %v", err)
	}
}

func TestPackageRejectsInvalidCollections(t *testing.T) {
	valid := File{"main.gox", "package p\nfunc F(){}\n"}
	cases := []struct {
		name  string
		files []File
		want  string
	}{
		{"empty", nil, "1 to 16"},
		{"many", make([]File, MaxFiles+1), "1 to 16"},
		{"duplicate", []File{valid, valid}, "duplicate"},
		{"case duplicate", []File{valid, {"Main.gox", valid.Source}}, "duplicate"},
		{"traversal", []File{{"../main.gox", valid.Source}}, "flat .gox"},
		{"directory", []File{{"sub/main.gox", valid.Source}}, "flat .gox"},
		{"native", []File{{"main.go", valid.Source}}, "flat .gox"},
		{"test", []File{{"main_test.gox", valid.Source}}, "no test files"},
		{"helper", []File{{"goxide_helpers_main.gox", valid.Source}}, "reserved"},
		{"packages", []File{valid, {"other.gox", "package q\n"}}, "all source files"},
		{"aggregate", []File{{"a.gox", "package p\n//" + strings.Repeat("a", MaxSourceBytes/2)}, {"b.gox", "package p\n//" + strings.Repeat("b", MaxSourceBytes/2)}}, "32 KiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CompilePackage(tc.files, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q: %v", tc.want, err)
			}
		})
	}
}

func TestPackageConflictsAndRecovery(t *testing.T) {
	options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
		"fmt":     {PackagePath: "fmt", Source: "toolchain"},
		"strings": {PackagePath: "strings", Source: "toolchain"},
	}}
	cases := []struct{ name, first, second, want string }{
		{"declaration", "func F(){}", "func F(){}", "duplicate declaration"},
		{"macro", "macro_rules! item {()=>{1};}", "macro_rules! item {()=>{2};}", "duplicate macro"},
		{"derive", "#[derive(Clone)]\ntype Item struct{N int}", "func CloneItem(){}", "CloneItem"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompilePackage([]File{{"first.gox", "package p\n" + tc.first + "\n"}, {"second.gox", "package p\n" + tc.second + "\n"}}, options)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q: %v", tc.want, err)
			}
			if _, err := CompilePackage([]File{{"recovered.gox", "package p\nfunc F()int{return 42}\n"}}, options); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPackageBuiltinSumHelpersAreComplete(t *testing.T) {
	files, err := CompilePackage([]File{
		{"make.gox", "package p\nfunc Make()Option[int]{return Option::Some(7)}\n"},
		{"read.gox", "package p\nfunc Read()int{return match Make(){Option::Some(n)=>n,Option::None=>0,}}\n"},
	}, Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()})})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !strings.Contains(f.Source, `"github.com/goxide-lang/std/sum"`) || strings.Contains(f.Source, "type Option[") {
			t.Fatalf("sum types must use the shared std package directly: %s\n%s", f.Path, f.Source)
		}
	}
	if len(files) != 2 || files[0].Path != "make.go" || files[1].Path != "read.go" {
		t.Fatalf("unexpected sum output files: %v", files)
	}
}
