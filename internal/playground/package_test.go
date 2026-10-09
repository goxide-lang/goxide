package playground

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/testdeps"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestPackageFilesScopeHelpersAndNativeBuild(t *testing.T) {
	files := []File{
		{"main.gox", `package p
use fmt as native;
func Value() string { native := Item{N: 6}; return native::Sprint(add!(native.N), ":", label()) }
`},
		{"model.gox", `package p
use serde;
#[derive(Clone,Debug,serde::Serialize)]
type Item struct { N int }
macro_rules! add { ($n:expr) => { $n + 1 }; }
`},
		{"label.gox", `package p
use strings as native;
func label() string { return native::ToUpper("ok") }
`},
	}
	options := frontend.PackageOptions{Importer: typecheck.NewImporter("playground.gox"), Roots: paths.Roots{
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
		if file.Path == "model.go" && strings.Contains(file.Source, "func CloneItem") {
			t.Fatal("helper remains embedded in user file")
		}
		if file.Path == "goxide_helpers_model.go" && !strings.Contains(file.Source, "func CloneItem") {
			t.Fatal("separate helper source incomplete")
		}
		if err := os.WriteFile(filepath.Join(dir, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(names, []string{"goxide_helpers_model.go", "goxide_project_config.go", "label.go", "main.go", "model.go"}) {
		t.Fatalf("outputs: %v", names)
	}
	// Directory CLI and in-memory WASM consume the same explicit context. Compare
	// every generated Go byte, including configuration and helper file indices.
	input := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module playground\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(input, file.Path), []byte(file.Source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	moduleOutput := filepath.Join(t.TempDir(), "module")
	if _, err := frontend.CompileModuleWithOptions(input, moduleOutput, frontend.ModuleOptions{Roots: options.Roots}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(moduleOutput)
	if err != nil {
		t.Fatal(err)
	}
	var moduleFiles []File
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") {
			data, err := os.ReadFile(filepath.Join(moduleOutput, entry.Name()))
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
	if _, err := CompilePackage(bad, options); err == nil || !strings.Contains(err.Error(), "label.gox") || !strings.Contains(err.Error(), "::") {
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
			if _, err := CompilePackage(tc.files, frontend.PackageOptions{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q: %v", tc.want, err)
			}
		})
	}
}

func TestPackageConflictsAndRecovery(t *testing.T) {
	options := frontend.PackageOptions{Importer: typecheck.NewImporter("playground.gox"), Roots: paths.Roots{
		"fmt":     {PackagePath: "fmt", Source: "toolchain"},
		"strings": {PackagePath: "strings", Source: "toolchain"},
	}}
	cases := []struct{ name, first, second, want string }{
		{"declaration", "func F(){}", "func F(){}", "redeclared"},
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
	}, frontend.PackageOptions{Importer: typecheck.NewImporter("playground.gox")})
	if err != nil {
		t.Fatal(err)
	}
	var helpers, aliases int
	for _, f := range files {
		if strings.HasPrefix(f.Path, "goxide_helpers_") {
			helpers++
		}
		if strings.Contains(f.Source, "type Option[") {
			aliases++
			if !strings.HasPrefix(f.Path, "goxide_helpers_") || !strings.Contains(f.Source, "= __goxide_enum_std.Option") {
				t.Fatalf("shared std alias misplaced or inlined: %s", f.Path)
			}
		}
	}
	if helpers == 0 || aliases != 1 {
		t.Fatalf("helpers=%d aliases=%d", helpers, aliases)
	}
}
