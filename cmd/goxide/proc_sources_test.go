package main

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

	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestNativeSourceDependenciesSelectedVersion(t *testing.T) {
	proxy, cache := versionProxy(t)
	for _, version := range []string{"v1.0.0", "v1.1.0"} {
		libraryPath := "src"
		if version == "v1.0.0" {
			libraryPath = "oldsrc"
		}
		publishVersion(t, proxy, "example.org/native-discovery", version, map[string]string{
			"go.mod":                 "module example.org/native-discovery\ngo 1.27\n",
			"goxide.toml":            "[go_mod]\n[lib]\nname='library'\npath='" + libraryPath + "'\n",
			libraryPath + "/lib.gox": "package library\npub func Value() int { return 42 }\n",
		})
	}
	publishVersion(t, proxy, "example.org/native-bridge", "v1.0.0", map[string]string{
		"go.mod":    "module example.org/native-bridge\ngo 1.27\nrequire example.org/native-discovery v1.1.0\n",
		"bridge.go": "package bridge\n",
	})
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"go.mod":   "module example.org/discovery-consumer\ngo 1.27\nrequire (\nexample.org/native-discovery v1.0.0\nexample.org/native-bridge v1.0.0\n)\n",
		"main.gox": "package main\nfunc main() {}\n",
	})
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "mod", "download", "example.org/native-discovery@v1.0.0", "example.org/native-discovery@v1.1.0", "example.org/native-bridge@v1.0.0")
	command.Dir, command.Env = root, append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("prime source cache: %v\n%s", err, output)
	}
	before := singleFileSnapshot(t, root)
	cfg := testdeps.Configuration()
	owner, err := prepareSingleFileModule(filepath.Join(root, "main.gox"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	dependencies, err := nativeSourceDependencies(root, owner.modFile, paths.Roots{"library": {ModulePath: "example.org/native-discovery", GoPrefix: "example.org/native-discovery", Source: "module"}}, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest, roots := dependencies.manifest, dependencies.roots
	if selected := manifest.Dependencies["library"]; selected.Go != "example.org/native-discovery" || selected.Version != "v1.1.0" {
		t.Fatalf("selected source = %+v", selected)
	}
	if root := roots["library"]; root.PackagePath != "example.org/native-discovery/src" || root.GoPrefix != "" {
		t.Fatalf("source root = %+v", root)
	}
	if after := singleFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("module query changed authored files")
	}
	t.Run("authored_replacement_and_selected_published_root", func(t *testing.T) {
		mixed := t.TempDir()
		projectWrite(t, mixed, map[string]string{
			"go.mod":               "module example.org/mixed-consumer\ngo 1.27\nrequire (\nexample.org/native-discovery v1.0.0\nexample.org/native-bridge v1.0.0\nexample.org/authored v0.0.0\n)\nreplace example.org/authored => ./authored\n",
			"main.gox":             "package main\nfunc main() {}\n",
			"authored/goxide.toml": "[go_mod]\nmodule='example.org/authored'\ngo='1.27'\n[lib]\nname='authored'\npath='.'\n",
			"authored/lib.gox":     "package authored\npub func Value() int { return 7 }\n",
		})
		before := singleFileSnapshot(t, mixed)
		owner, err := prepareSingleFileModule(filepath.Join(mixed, "main.gox"), cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		dependencies, err := nativeSourceDependencies(mixed, owner.modFile, paths.Roots{
			"library":  {ModulePath: "example.org/native-discovery", Source: "module"},
			"authored": {ModulePath: "example.org/authored", Source: "module"},
		}, cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		manifest, roots := dependencies.manifest, dependencies.roots
		if manifest.Dependencies["library"].Version != "v1.1.0" || roots["library"].PackagePath != "example.org/native-discovery/src" {
			t.Fatalf("lost selected published root: dependency=%+v root=%+v", manifest.Dependencies["library"], roots["library"])
		}
		if manifest.Dependencies["authored"].Version != "v0.0.0" || roots["authored"].PackagePath != "example.org/authored" {
			t.Fatalf("lost authored root: dependency=%+v root=%+v", manifest.Dependencies["authored"], roots["authored"])
		}
		if after := singleFileSnapshot(t, mixed); !reflect.DeepEqual(before, after) {
			t.Fatal("mixed source discovery changed authored files")
		}
	})
	t.Run("local_versioned_replacements", func(t *testing.T) {
		replaced := t.TempDir()
		projectWrite(t, replaced, map[string]string{
			"go.mod":   fmt.Sprintf("module example.org/replaced-consumer\ngo 1.27\nrequire (\nexample.org/native-discovery v1.0.0\nexample.org/native-bridge v1.0.0\n)\nreplace example.org/native-discovery v1.0.0 => %s\nreplace example.org/native-discovery v1.1.0 => %s\n", filepath.Join(cache, "example.org/native-discovery@v1.0.0"), filepath.Join(cache, "example.org/native-discovery@v1.1.0")),
			"main.gox": "package main\nfunc main() {}\n",
		})
		before := singleFileSnapshot(t, replaced)
		owner, err := prepareSingleFileModule(filepath.Join(replaced, "main.gox"), cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		dependencies, err := nativeSourceDependencies(replaced, owner.modFile, paths.Roots{"library": {ModulePath: "example.org/native-discovery", Source: "module"}}, cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		manifest, roots := dependencies.manifest, dependencies.roots
		if manifest.Dependencies["library"].Version != "v1.1.0" || roots["library"].PackagePath != "example.org/native-discovery/src" {
			t.Fatalf("wrong selected replacement: dependency=%+v root=%+v", manifest.Dependencies["library"], roots["library"])
		}
		if after := singleFileSnapshot(t, replaced); !reflect.DeepEqual(before, after) {
			t.Fatal("replacement query changed authored files")
		}
	})
}

func TestNativeExplicitFileDiscovery(t *testing.T) {
	base := t.TempDir()
	write := func(name, source string) { singleFileWrite(t, base, name, source) }
	write("dep/goxide.toml", "[go_mod]\nmodule='example.org/explicit-dependency'\ngo='1.27'\n[lib]\nname='dependency'\npath='.'\n")
	write("dep/lib.gox", "package dependency\npub func Value() int { return 42 }\n")
	write("app/go.mod", "module example.org/explicit-consumer\ngo 1.27\nrequire example.org/explicit-dependency v0.0.0\nreplace example.org/explicit-dependency => ../dep\n")
	write("app/goxide.roots.toml", "[roots.dep]\nmodule='example.org/explicit-dependency'\n")
	for _, name := range []string{"testdata/poison.gox", ".hidden/poison.gox", "_dir/poison.gox", ".poison.gox", "_poison.gox"} {
		write("dep/"+name, "invalid unselected dependency source\n")
	}
	write("app/testdata/unselected.gox", "invalid unselected sibling\n")
	for _, name := range []string{"main.gox", "testdata/main.gox", ".hidden/main.gox", "_dir/main.gox", ".name.gox", "_name.gox"} {
		t.Run(name, func(t *testing.T) {
			write("app/"+name, "package main\nfunc main() { println(42) }\n")
			before := singleFileSnapshot(t, base)
			var stdout, stderr bytes.Buffer
			input := filepath.Join(base, "app", filepath.FromSlash(name))
			err := executeCompile(CompileRequest{Mode: "expand", Input: input, GoTool: filepath.Join(runtime.GOROOT(), "bin", "go")}, &stdout, &stderr)
			if err != nil || !strings.Contains(stdout.String(), "println(42)") {
				t.Fatalf("explicit input %s: %v\n%s\n%s", name, err, stdout.String(), stderr.String())
			}
			if after := singleFileSnapshot(t, base); !reflect.DeepEqual(before, after) {
				t.Fatal("explicit source compilation changed authored files")
			}
		})
	}
}

func TestNativeProceduralMacroSources(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	query := exec.Command(tool, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", "github.com/goxide-lang/syntax")
	query.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "NATIVE_PROC_CFG=yes")
	sdk, err := query.Output()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "goxide")
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command(tool, "run", "./scripts/build.go", "-o", binary)
	build.Dir, build.Env = repo, query.Env
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	base := t.TempDir()
	write := func(name, source string) { singleFileWrite(t, base, name, source) }
	write("macros/goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/native-macros"
go="1.27"
[lib]
name="macros"
path="src"
proc_macro=true
[dependencies.syntax]
path=%q
`, strings.TrimSpace(string(sdk))))
	write("macros/src/lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func echo(ctx pm::Context, input token::TokenStream) pm::Output {
 var output = pm::Success(input)
 output.Diagnostics = append(output.Diagnostics, pm::Diagnostic{Severity: pm::Warning, Message: "native warning", Span: ctx.CallSpan()})
 return output
}
#[proc_macro]
pub func answer(ctx pm::Context, input token::TokenStream) pm::Output {
 var root, err = ctx.RuntimeRoot("example.org/native-facade")
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 var tail, parseErr = token::ParseText("::Value()", ctx.DefSite())
 if parseErr != nil { return pm::Failure(parseErr.Error(), ctx.CallSpan()) }
 return pm::Success(root.Append(tail.Tokens()...))
}
#[proc_macro]
pub func gated(ctx pm::Context, input token::TokenStream) pm::Output {
 var parsed, err = token::ParseText("#[cfg(all())] func Extra() int { return 1 }", ctx.CallSite())
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 return pm::Success(parsed)
}
#[proc_macro]
pub func native_answer(ctx pm::Context, input token::TokenStream) pm::Output {
 var root, err = ctx.RuntimeRoot("example.org/native-runtime")
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 var tail, parseErr = token::ParseText("::Value()", ctx.DefSite())
 if parseErr != nil { return pm::Failure(parseErr.Error(), ctx.CallSpan()) }
 return pm::Success(root.Append(tail.Tokens()...))
}
`)
	write("app/go.mod", "module example.org/native-consumer\ngo 1.27\nrequire example.org/native-macros v0.0.0\nreplace example.org/native-macros => ../macros\n")
	write("app/goxide.roots.toml", `[roots.macros]
module="example.org/native-macros"
[roots.fmt]
package="fmt"
source="toolchain"
`)
	write("app/main.gox", `package main
use macros;
use fmt;
func main() { fmt::Println(macros::echo!(6 * 7)) }
`)
	before := singleFileSnapshot(t, base)
	input := filepath.Join(base, "app/main.gox")
	verifyPublishedGo := false
	invoke := func(args ...string) (string, string, error) {
		// Generation consumes a published Go library; build and run continue to
		// exercise the authoring-only Goxide dependency below.
		if args[0] == "gen" || args[0] == "transpile" {
			published := map[string]string{
				"macros/go.mod":     "module example.org/native-macros\ngo 1.27\n",
				"macros/src/api.go": "package macros\n",
			}
			if _, err := os.Stat(filepath.Join(base, "facade/goxide.toml")); err == nil {
				published = map[string]string{}
				published["facade/go.mod"] = "module example.org/native-facade\ngo 1.27\n"
				published["facade/lib_gox.go"] = generatedHeader + "package facade\nfunc Value() int { return 42 }\n"
			}
			for name, source := range published {
				write(name, source)
			}
			defer func() {
				for name, source := range published {
					path := filepath.Join(base, name)
					data, err := os.ReadFile(path)
					if err != nil || string(data) != source {
						t.Fatalf("generation changed published dependency %s: %v", name, err)
					}
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			}()
		}
		var stdout, stderr bytes.Buffer
		command := exec.Command(binary, args...)
		command.Env, command.Stdout, command.Stderr = query.Env, &stdout, &stderr
		err := command.Run()
		if err == nil && verifyPublishedGo && args[0] == "gen" && stdout.Len() != 0 {
			// The compiler has exited and removed its private preparation. The
			// generated consumer must run using the library's published Go API.
			check := filepath.Join(base, "app/publication_check.go")
			if err := os.WriteFile(check, stdout.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(check)
			goRun := exec.Command(tool, "run", "-mod=readonly", check)
			goRun.Dir, goRun.Env = filepath.Dir(input), query.Env
			if output, err := goRun.CombinedOutput(); err != nil || string(output) != "42\n" {
				t.Fatalf("published Go file entry: %v\n%s", err, output)
			}
		}
		return stdout.String(), stderr.String(), err
	}
	var first string
	for n := 0; n < 2; n++ {
		stdout, stderr, err := invoke("gen", "-o", "-", input)
		if err != nil || !strings.Contains(stdout, "6 * 7") || strings.Count(stderr, "warning: native warning") != 1 {
			t.Fatalf("gen: %v\n%s\n%s", err, stdout, stderr)
		}
		if !strings.Contains(stderr, "example.org/native-macros/src") || strings.Contains(stderr, "goxide_proc_") {
			t.Fatalf("tools advice lost the published library entry: %s", stderr)
		}
		if n == 0 {
			first = stdout
		} else if first != stdout {
			t.Fatal("generation changed bytes")
		}
	}
	for _, args := range [][]string{{"run", input}, {"run", filepath.Dir(input)}} {
		stdout, stderr, err := invoke(args...)
		if err != nil || stdout != "42\n" || strings.Count(stderr, "warning: native warning") != 1 {
			t.Fatalf("%v: %v\n%s\n%s", args, err, stdout, stderr)
		}
	}
	stdout, stderr, err := invoke("expand", input)
	if err != nil || !strings.Contains(stdout, "6 * 7") || strings.Contains(stdout, "echo!") || strings.Count(stderr, "warning: native warning") != 1 {
		t.Fatalf("expand: %v\n%s\n%s", err, stdout, stderr)
	}
	output := filepath.Join(t.TempDir(), "main.go")
	if _, stderr, err := invoke("transpile", "-o", output, input); err != nil {
		t.Fatalf("transpile: %v\n%s", err, stderr)
	}
	if after := singleFileSnapshot(t, base); !reflect.DeepEqual(before, after) {
		t.Fatalf("native source changed: before=%v after=%v", before, after)
	}
	t.Run("explicit_single_file_selection", func(t *testing.T) {
		foreignOS := "windows"
		if runtime.GOOS == foreignOS {
			foreignOS = "linux"
		}
		nativeSource, err := os.ReadFile(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"selected_" + foreignOS + ".gox", "extensionless"} {
			selected := filepath.Join(filepath.Dir(input), name)
			if err := os.WriteFile(selected, append([]byte("//go:build "+foreignOS+"\n\n"), nativeSource...), 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"expand", selected}, {"run", selected}} {
				stdout, stderr, err := invoke(args...)
				if err != nil || args[0] == "run" && stdout != "42\n" || args[0] == "expand" && !strings.Contains(stdout, "6 * 7") {
					t.Fatalf("%v: %v\n%s\n%s", args, err, stdout, stderr)
				}
			}
			if filepath.Ext(name) == ".gox" {
				stdout, stderr, err := invoke("gen", "-o", "-", selected)
				if err != nil || !strings.Contains(stdout, "6 * 7") {
					t.Fatalf("explicit gen: %v\n%s\n%s", err, stdout, stderr)
				}
			}
			if err := os.Remove(selected); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("exact_root_package", func(t *testing.T) {
		roots := filepath.Join(t.TempDir(), "roots.toml")
		if err := os.WriteFile(roots, []byte("[roots.macros]\nmodule=\"example.org/native-macros\"\npackage=\"example.org/native-macros\"\n[roots.fmt]\nsource=\"toolchain\"\npackage=\"fmt\"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := invoke("gen", "--roots", roots, "-o", "-", input)
		if err == nil || stdout != "" {
			t.Fatalf("exact package silently redirected: %v\n%s\n%s", err, stdout, stderr)
		}
	})
	t.Run("partial_generated_configuration", func(t *testing.T) {
		original, err := os.ReadFile(input)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.WriteFile(input, original, 0600); err != nil {
				t.Fatal(err)
			}
		}()
		write("app/main.gox", string(original)+"macros::gated!();\n")
		before := singleFileSnapshot(t, base)
		stdout, stderr, err := invoke("gen", input)
		if err == nil || stdout != "" || !strings.Contains(stderr, "whole-package generation") {
			t.Fatalf("partial generated configuration: %v\n%s\n%s", err, stdout, stderr)
		}
		if after := singleFileSnapshot(t, base); !reflect.DeepEqual(before, after) {
			t.Fatal("failed generation changed authored files")
		}
	})
	t.Run("ordinary_go_runtime_root", func(t *testing.T) {
		write("plain/go.mod", "module example.org/native-runtime\ngo 1.27\n")
		write("plain/value.go", "package runtime\nfunc Value() int { return 42 }\n")
		write("app/go.mod", "module example.org/native-consumer\ngo 1.27\nrequire (\nexample.org/native-macros v0.0.0\nexample.org/native-runtime v0.0.0\n)\nreplace example.org/native-macros => ../macros\nreplace example.org/native-runtime => ../plain\n")
		write("app/goxide.roots.toml", `[roots.macros]
module="example.org/native-macros"
[roots.support]
module="example.org/native-runtime"
[roots.fmt]
package="fmt"
source="toolchain"
`)
		write("app/main.gox", `package main
use macros;
use fmt;
func Answer() int { return macros::native_answer!() }
func main() { fmt::Println(Answer()) }
`)
		write("app/main_test.go", "package main\nimport \"testing\"\nfunc TestAnswer(t *testing.T) { if Answer() != 42 { t.Fatal(Answer()) } }\n")
		before := singleFileSnapshot(t, base)
		for _, args := range [][]string{{"gen", "-o", "-", input}, {"run", input}, {"run", filepath.Dir(input)}, {"build", "-o", filepath.Join(t.TempDir(), "app"), input}, {"build", "-o", filepath.Join(t.TempDir(), "app"), filepath.Dir(input)}, {"test", filepath.Dir(input)}} {
			stdout, stderr, err := invoke(args...)
			if err != nil || args[0] == "run" && stdout != "42\n" {
				t.Fatalf("%v: %v\n%s\n%s", args, err, stdout, stderr)
			}
		}
		if after := singleFileSnapshot(t, base); !reflect.DeepEqual(before, after) {
			t.Fatal("ordinary Go runtime root changed authored files")
		}
		if err := os.Remove(filepath.Join(base, "app/main_test.go")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("runtime_reexport_and_file_entry", func(t *testing.T) {
		verifyPublishedGo = true
		defer func() { verifyPublishedGo = false }()
		write("facade/goxide.toml", `[go_mod]
module="example.org/native-facade"
go="1.27"
[lib]
name="facade"
path="src/lib.gox"
[dependencies.macros]
path="../macros"
`)
		write("facade/src/lib.gox", `package facade
pub use macros::answer;
use macros;
pub mod part;
macro_rules! selected {
 () => { #[cfg(env("NATIVE_PROC_CFG"))] pub func Value() int { return macros::echo!(part::Value()) } };
}
selected!();
`)
		write("facade/src/part/lib.gox", "package part\npub func Value() int { return 42 }\n")
		write("app/go.mod", "module example.org/native-consumer\ngo 1.27\nrequire example.org/native-facade v0.0.0\nreplace example.org/native-facade => ../facade\n")
		write("app/goxide.roots.toml", `[roots.facade]
module="example.org/native-facade"
[roots.fmt]
package="fmt"
source="toolchain"
`)
		write("app/main.gox", `package main
use facade;
use fmt;
func main() { fmt::Println(facade::answer!()) }
`)
		before := singleFileSnapshot(t, base)
		var first string
		for _, args := range [][]string{{"gen", "-o", "-", input}, {"gen", "-o", "-", input}, {"run", input}, {"run", filepath.Dir(input)}} {
			stdout, stderr, err := invoke(args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s\n%s", args, err, stdout, stderr)
			}
			if strings.Count(stderr, "warning: native warning") != 1 {
				t.Fatalf("dependency macro must execute once: %v\n%s", args, stderr)
			}
			if args[0] == "run" && stdout != "42\n" {
				t.Fatalf("run output %q", stdout)
			}
			if args[0] == "gen" {
				if first == "" {
					first = stdout
				} else if first != stdout {
					t.Fatal("file-entry generation changed bytes")
				}
			}
		}
		if after := singleFileSnapshot(t, base); !reflect.DeepEqual(before, after) {
			t.Fatal("runtime reexport changed authored files")
		}
		if stdout, stderr, err := invoke("gen", input); err != nil || stdout != "" || strings.Count(stderr, "warning: native warning") != 1 {
			t.Fatalf("dependency configuration rejected native partial gen: %v\n%s\n%s", err, stdout, stderr)
		}
	})
}
