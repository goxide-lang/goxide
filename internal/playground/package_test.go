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

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/typecheck"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
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
func Value()string {native:=Item{N:6};return native::Sprint(add!(native.N),":",label())}
`},
		{"model.gox", `package p
// The public dependency keeps its source comment after namespace resolution.
use std;
use serde;
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize)]
type Item struct{N int}
impl Item {
 pub func copy(self &Self)Self{return std::Clone::clone(self)}
 pub func inspect(self &Self)string{return std::Debug::debug(self)}
}
macro_rules! add {($n:expr)=>{$n+1};}
`},
		{"label.gox", `package p
// This alias has a different target from the main file's native alias.
use strings as native;
func label()string{return native::ToUpper("ok")}
`},
	}
	input := t.TempDir()
	if err := os.WriteFile(filepath.Join(input, "goxide.toml"), []byte(testdeps.FormalManifest(t, PackagePath, "playground")), 0600); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(input, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output []File
	var options Options
	project, calls, err := testdeps.PrepareFormal(t, input, compile.Options{
		PrepareNativePackage: func(modules []analysis.Module) error {
			module := modules[0]
			options = Options{Config: module.Config, Importer: typecheck.NewImporter(filepath.Join(module.Dir, "model.gox"), typecheck.ImporterOptions{Config: module.Config})}
			return nil
		},
		OnPreparedSources: func(sources compile.PreparedSources) error {
			options.Roots, options.RuntimeRoots, options.SourcePackages = sources.Roots, sources.RuntimeRoots, sources.SourcePackages
			for _, artifact := range sources.Macros {
				options.ProcLibraries = append(options.ProcLibraries, artifact.Library)
			}
			var err error
			output, err = CompilePackage(files, options)
			if err != nil {
				return err
			}
			// A failed edit cannot contaminate declarations or sibling import bindings.
			bad := append([]File(nil), files...)
			bad[2].Source = "package p\nfunc label()string{return native::Sprint(\"bad\")}\n"
			if _, err := CompilePackage(bad, options); err == nil || !strings.Contains(err.Error(), "label.gox") || !strings.Contains(err.Error(), "unresolved name") {
				t.Fatalf("import leaked from sibling: %v", err)
			}
			// A procedural derive and a handwritten implementation share the same
			// ordinary coherence rules, including when they are in different files.
			conflict := append([]File(nil), files...)
			conflict[2].Source = "package p\nuse std;\nfunc label()string{return \"ok\"}\nimpl std::Clone for Item {func clone(self &Self)Self{return *self}}\n"
			if _, err := CompilePackage(conflict, options); err == nil || !strings.Contains(err.Error(), "overlapping") {
				t.Fatalf("cross-file derive/impl conflict accepted: %v", err)
			}
			again, err := CompilePackage(files, options)
			if err != nil || !reflect.DeepEqual(output, again) {
				t.Fatalf("recovery changed outputs: %v", err)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("formal procedural macros did not execute")
	}
	var names []string
	for _, file := range output {
		names = append(names, file.Path)
		actual, err := os.ReadFile(filepath.Join(project.Root, file.Path))
		if err != nil || string(actual) != file.Source {
			t.Fatalf("directory and in-memory output differ for %s: %v", file.Path, err)
		}
	}
	if !reflect.DeepEqual(names, []string{"label.go", "main.go", "model.go"}) {
		t.Fatalf("outputs: %v", names)
	}
	if err := os.WriteFile(filepath.Join(project.Root, "output_test.go"), []byte(`package p
import "testing"
func TestOutput(t *testing.T){
 if Value()!="7:OK"{t.Fatal(Value())}
 v:=Item{N:8};c:=v.Copy()
 if c.N!=8||c.Inspect()!="Item { N: 8 }"{t.Fatal(c)}
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	testdeps.RunFormalGo(t, project, "test", "-mod=readonly", "-count=1", "./...")
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

func TestPackageCanonicalSumConstructors(t *testing.T) {
	files := []File{{"main.gox", `package main
use fmt;
use std::sum as s;
use std::sum::Option as Maybe;
type LocalOption[T any] = Maybe[T]
func readOption(value s::Option[int])int {
    return match value {s::Option[int]::Some(n)=>n,s::Option[int]::None=>0}
}
func readMaybe(value Maybe[int])int {
    return match value {Maybe[int]::Some(n)=>n,Maybe[int]::None=>0}
}
func readLocal(value LocalOption[int])int {
    return match value {LocalOption[int]::Some(n)=>n,LocalOption[int]::None=>0}
}
func next(value s::Result[int,error])s::Result[int,error] {
    n := value?
    return s::Result[int,error]::Ok(n+1)
}
func readResult(value s::Result[int,error])int {
    return match value {s::Result[int,error]::Ok(n)=>n,s::Result[int,error]::Err(_)=>-1}
}
func main() {
    fmt::Println(readOption(s::Option[int]::Some(7)),readOption(s::Option[int]::None),
        readOption(s::NewSome(8)),readMaybe(Maybe[int]::Some(9)),readMaybe(Maybe[int]::None),
        readLocal(LocalOption[int]::Some(10)),readLocal(LocalOption[int]::None))
    fmt::Println(readResult(next(s::Result[int,error]::Ok(4))),
        readResult(next(s::Result[int,error]::Err(fmt::Errorf("bad")))),
        readResult(next(s::NewOk[int,error](5))),
        readResult(next(s::NewErr[int,error](fmt::Errorf("bad")))))
}
`}}
	options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
		"std": {Source: "module", ModulePath: "github.com/goxide-lang/std", GoPrefix: "github.com/goxide-lang/std"},
	}}
	output, err := CompilePackage(files, options)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := CompilePackage(files, options)
	if err != nil || !reflect.DeepEqual(output, repeated) {
		t.Fatalf("canonical sum generation is unstable: %v", err)
	}
	directory := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(directory, "go.mod"), []byte("module playground\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, file := range output {
		if err := os.WriteFile(filepath.Join(directory, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	stdout, err := command.CombinedOutput()
	if err != nil || string(stdout) != "7 0 8 9 0 10 0\n5 -1 6 -1\n" {
		t.Fatalf("generated canonical sum program: %s (%v)", stdout, err)
	}
	for _, prefix := range []string{"s::Option[int]", "Maybe[int]"} {
		_, err := CompilePackage([]File{{"invalid.gox", "package main\nuse std::sum as s;\nuse std::sum::Option as Maybe;\nfunc main(){_=" + prefix + "::Missing::Some(1)}\n"}}, options)
		if err == nil || !strings.Contains(err.Error(), "unknown associated type or member path") {
			t.Fatalf("deferred associated path %s was accepted as an enum constructor: %v", prefix, err)
		}
	}
}
