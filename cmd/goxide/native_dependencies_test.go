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

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestNativeAutomaticDependencySelection(t *testing.T) {
	proxy, cache := versionProxy(t)
	publishLibrary := func(module, version, name string) {
		publishVersion(t, proxy, module, version, map[string]string{
			"go.mod":      "module " + module + "\ngo 1.27\n",
			"goxide.toml": "[go_mod]\n[lib]\nname='" + name + "'\npath='src'\n",
			"src/lib.gox": "package " + name + "\n",
		})
	}
	publishLibrary("example.org/selected", "v1.0.0", "old")
	publishLibrary("example.org/selected", "v1.1.0", "selected")
	publishLibrary("example.org/transitive", "v1.0.0", "transitive")
	publishLibrary("example.org/indirect", "v1.0.0", "indirect")
	publishLibrary("example.org/fork", "v1.4.0", "forked")
	publishVersion(t, proxy, "example.org/bridge", "v1.0.0", map[string]string{
		"go.mod":    "module example.org/bridge\ngo 1.27\nrequire (\nexample.org/selected v1.1.0\nexample.org/transitive v1.0.0\n)\n",
		"bridge.go": "package bridge\n",
	})
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"go.mod":   "module example.org/consumer\ngo 1.27\nrequire (\nexample.org/selected v1.0.0\nexample.org/bridge v1.0.0\nexample.org/indirect v1.0.0 // indirect\nexample.org/replaced v1.0.0\n)\nreplace example.org/replaced => example.org/fork v1.4.0\nreplace example.org/replace-only => ../unselected\n",
		"main.gox": "package main\nfunc main() {}\n",
	})
	before := singleFileSnapshot(t, root)
	cfg := testdeps.Configuration()
	owner, err := prepareSingleFileModule(filepath.Join(root, "main.gox"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	plan, err := nativeSourceDependencies(root, owner.modFile, nil, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Automatic) != 3 || plan.Automatic["selected"][0].Dependency.Version != "v1.1.0" {
		t.Fatalf("wrong automatic candidates: %+v", plan.Automatic)
	}
	if fork := plan.Automatic["forked"][0]; fork.Dependency.Go != "example.org/replaced" || fork.Dependency.Version != "v1.0.0" || fork.Namespace.PackagePath != "example.org/replaced/src" {
		t.Fatalf("replacement lost logical identity: %+v", fork)
	}
	if len(plan.Manifest.Dependencies) != 0 {
		t.Fatal("candidate discovery activated source dependencies")
	}
	for _, name := range []string{"selected@v1.1.0", "indirect@v1.0.0", "fork@v1.4.0"} {
		if _, err := os.Stat(filepath.Join(cache, "example.org", name, "goxide.toml")); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"selected@v1.0.0", "transitive@v1.0.0"} {
		if _, err := os.Stat(filepath.Join(cache, "example.org", name)); !os.IsNotExist(err) {
			t.Fatalf("downloaded non-candidate source %s: %v", name, err)
		}
	}
	if !reflect.DeepEqual(before, singleFileSnapshot(t, root)) {
		t.Fatal("discovery changed authored files")
	}

	t.Run("explicit_root_suppresses_default_and_keeps_exact_package", func(t *testing.T) {
		owner, err := prepareSingleFileModule(filepath.Join(root, "main.gox"), cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		roots := paths.Roots{"renamed": {ModulePath: "example.org/selected", PackagePath: "example.org/selected/other", Source: "module"}, "indirect": {Source: "toolchain", PackagePath: "fmt"}}
		plan, err := nativeSourceDependencies(root, owner.modFile, roots, cfg, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := plan.Automatic["selected"]; exists {
			t.Fatal("explicit module gained its default name")
		}
		if _, exists := plan.Automatic["indirect"]; exists {
			t.Fatal("automatic alias replaced an explicit root")
		}
		if plan.Roots["renamed"] != roots["renamed"] || len(plan.Manifest.Dependencies) != 0 {
			t.Fatal("exact package acquired the library entry")
		}
	})
}

func TestNativeAutomaticDependencyChecksumFailure(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/checksum", "v1.0.0", map[string]string{
		"go.mod":      "module example.org/checksum\ngo 1.27\n",
		"goxide.toml": "[go_mod]\n[lib]\nname='checked'\npath='.'\n",
		"lib.gox":     "package checked\n",
	})
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"go.mod":   "module example.org/consumer\ngo 1.27\nrequire example.org/checksum v1.0.0\n",
		"go.sum":   "example.org/checksum v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n",
		"main.gox": "package main\nfunc main() {}\n",
	})
	before := singleFileSnapshot(t, root)
	cfg := testdeps.Configuration()
	owner, err := prepareSingleFileModule(filepath.Join(root, "main.gox"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	_, err = nativeSourceDependencies(root, owner.modFile, nil, cfg, true)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checksum failure lost: %v", err)
	}
	if !reflect.DeepEqual(before, singleFileSnapshot(t, root)) {
		t.Fatal("failed download changed authored files")
	}
}

func TestNativeAutomaticSourceOnlyReplacementSelection(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		t.Run(fmt.Sprintf("versioned_%t", versioned), func(t *testing.T) {
			proxy, _ := versionProxy(t)
			for _, version := range []string{"v1.0.0", "v1.2.0"} {
				publishVersion(t, proxy, "example.org/selected", version, map[string]string{
					"go.mod":      "module example.org/selected\ngo 1.27\n",
					"goxide.toml": "[go_mod]\n[lib]\nname='selected'\npath='.'\n",
					"lib.gox":     "package selected\n",
				})
			}
			publishVersion(t, proxy, "example.org/library", "v1.0.0", map[string]string{
				"go.mod": "module example.org/library\ngo 1.27\n", "api.go": "package library\n",
			})
			publishVersion(t, proxy, "example.org/bridge", "v1.0.0", map[string]string{
				"go.mod": "module example.org/bridge\ngo 1.27\nrequire example.org/library v1.1.0\n", "api.go": "package bridge\n",
			})
			base := canonicalTempDir(t)
			singleFileWrite(t, base, "library/goxide.toml", "[go_mod]\nmodule='example.org/library'\ngo='1.27'\n[lib]\nname='library'\npath='.'\n[dependencies]\nselected={go='example.org/selected',version='v1.2.0'}\n")
			singleFileWrite(t, base, "library/lib.gox", "unused source must not compile\n")
			replacement := "example.org/library"
			requires := "example.org/library v1.0.0\nexample.org/selected v1.0.0\n"
			wantVersion := "v1.0.0"
			if versioned {
				replacement += " v1.1.0"
				requires += "example.org/bridge v1.0.0\n"
				wantVersion = "v1.1.0"
			}
			singleFileWrite(t, base, "app/go.mod", "module example.org/app\ngo 1.27\nrequire (\n"+requires+")\nreplace "+replacement+" => ../library\n")
			singleFileWrite(t, base, "app/main.gox", "package main\nfunc main() {}\n")
			app := filepath.Join(base, "app")
			cfg := testdeps.Configuration()
			selectDependencies := func() (*compile.NativeDependencies, error) {
				owner, err := prepareSingleFileModule(filepath.Join(app, "main.gox"), cfg)
				if err != nil {
					return nil, err
				}
				defer owner.close()
				return nativeSourceDependencies(app, owner.modFile, nil, cfg, true)
			}
			before := singleFileSnapshot(t, base)
			plan, err := selectDependencies()
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Automatic["library"]) != 1 || plan.Automatic["library"][0].Dependency.Version != wantVersion {
				t.Fatalf("source-only replacement did not follow selected version: %+v", plan.Automatic)
			}
			if len(plan.Automatic["selected"]) != 1 || plan.Automatic["selected"][0].Dependency.Version != "v1.2.0" {
				t.Fatalf("TOML dependency constraints were lost: %+v", plan.Automatic)
			}
			if plan.Required["example.org/selected"] != "v1.0.0" || len(plan.Manifest.Dependencies) != 0 {
				t.Fatal("private selection changed authored candidates or activated source compilation")
			}
			if !reflect.DeepEqual(before, singleFileSnapshot(t, base)) {
				t.Fatal("source-only selection changed authored files")
			}
			singleFileWrite(t, app, "go.sum", "example.org/selected v1.2.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
			before = singleFileSnapshot(t, base)
			if _, err := selectDependencies(); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
				t.Fatalf("source-only projection bypassed authored checksum: %v", err)
			}
			if !reflect.DeepEqual(before, singleFileSnapshot(t, base)) {
				t.Fatal("checksum failure changed authored files")
			}
		})
	}
}

func TestNativeAutomaticTransitiveSourceOnlyReplacement(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/bridge", "v1.0.0", map[string]string{
		"go.mod": "module example.org/bridge\ngo 1.27\nrequire example.org/library v1.0.0\n", "api.go": "package bridge\n",
	})
	base := canonicalTempDir(t)
	singleFileWrite(t, base, "library/goxide.toml", "[go_mod]\nmodule='example.org/library'\ngo='1.27'\n[lib]\nname='library'\npath='.'\n")
	singleFileWrite(t, base, "library/lib.gox", "unused transitive source must not compile\n")
	singleFileWrite(t, base, "app/go.mod", "module example.org/app\ngo 1.27\nrequire example.org/bridge v1.0.0\nreplace example.org/library => ../library\n")
	singleFileWrite(t, base, "app/main.gox", "package main\nfunc main() {}\n")
	app := filepath.Join(base, "app")
	before := singleFileSnapshot(t, base)
	cfg := testdeps.Configuration()
	owner, err := prepareSingleFileModule(filepath.Join(app, "main.gox"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	plan, err := nativeSourceDependencies(app, owner.modFile, nil, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Automatic) != 0 || len(plan.Manifest.Dependencies) != 0 || plan.Required["example.org/library"] != "" {
		t.Fatal("transitive source-only replacement became an authored candidate")
	}
	if !reflect.DeepEqual(before, singleFileSnapshot(t, base)) {
		t.Fatal("transitive source-only selection changed authored files")
	}
}

func TestNativeAutomaticRejectsIncompleteSelectedGraph(t *testing.T) {
	for _, missing := range []string{"module_info", "module_manifest"} {
		t.Run(missing, func(t *testing.T) {
			proxy, _ := versionProxy(t)
			goVersion := "1.27"
			if missing == "module_manifest" {
				goVersion = "1.16" // Its unpruned graph needs the transitive manifest.
			}
			publishVersion(t, proxy, "example.org/library", "v1.0.0", map[string]string{
				"go.mod":      "module example.org/library\ngo " + goVersion + "\nrequire example.org/missing v1.0.0\n",
				"goxide.toml": "[go_mod]\n[lib]\nname='library'\npath='.'\n",
				"lib.gox":     "package library\npub func Value() int { return 42 }\n",
				"lib_gox.go":  generatedHeader + "package library\nfunc Value() int { return 42 }\n",
			})
			if missing == "module_manifest" {
				singleFileWrite(t, proxy, "example.org/missing/@v/v1.0.0.info", `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`)
			} else {
				singleFileWrite(t, proxy, "example.org/missing/@v/v1.0.0.mod", "module example.org/missing\ngo 1.27\n")
			}
			root := t.TempDir()
			projectWrite(t, root, map[string]string{
				"go.mod":    "module example.org/consumer\ngo 1.27\nrequire example.org/library v1.0.0\n",
				"value.gox": "package p\nfunc Value() int { return 42 }\n",
			})
			before := singleFileSnapshot(t, root)
			var stdout, stderr bytes.Buffer
			err := run([]string{"gen", "-o", "-", filepath.Join(root, "value.gox")}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "example.org/missing") || stdout.Len() != 0 {
				t.Fatalf("incomplete graph produced generation: %v\n%s\n%s", err, &stdout, &stderr)
			}
			if !reflect.DeepEqual(before, singleFileSnapshot(t, root)) {
				t.Fatal("failed module selection changed authored files")
			}
		})
	}
}

func TestNativeAutomaticRootsAreDemanded(t *testing.T) {
	base := t.TempDir()
	write := func(name, contents string) { singleFileWrite(t, base, name, contents) }
	write("live/goxide.toml", "[go_mod]\nmodule='example.org/live'\ngo='1.27'\n[lib]\nname='live'\npath='.'\n")
	write("live/go.mod", "module example.org/live\ngo 1.27\n")
	write("live/api.go", "package live\n")
	write("live/lib.gox", "package live\npub macro_rules! answer { () => { 42 }; }\n")
	write("second/goxide.toml", "[go_mod]\nmodule='example.org/second'\ngo='1.27'\n[lib]\nname='second'\npath='.'\n")
	write("second/go.mod", "module example.org/second\ngo 1.27\n")
	write("second/api.go", "package second\n")
	write("second/lib.gox", "package second\npub macro_rules! zero { () => { 0 }; }\n")
	for _, name := range []string{"unused", "duplicate", "toolchain"} {
		alias := "poison"
		if name == "toolchain" {
			alias = "fmt"
		}
		write(name+"/goxide.toml", "[go_mod]\nmodule='example.org/"+name+"'\ngo='1.27'\n[lib]\nname='"+alias+"'\npath='.'\nproc_macro=true\n")
		write(name+"/go.mod", "module example.org/"+name+"\ngo 1.27\n")
		write(name+"/lib.gox", "this unused source must never be compiled\n")
	}
	write("app/go.mod", "module example.org/app\ngo 1.27\nrequire (\nexample.org/live v0.0.0\nexample.org/second v0.0.0\nexample.org/unused v0.0.0\nexample.org/duplicate v0.0.0\nexample.org/toolchain v0.0.0\n)\nreplace example.org/live => ../live\nreplace example.org/second => ../second\nreplace example.org/unused => ../unused\nreplace example.org/duplicate => ../duplicate\nreplace example.org/toolchain => ../toolchain\n")
	write("app/value.gox", "package p\nuse fmt;\nuse second;\n#[cfg(feature(\"off\"))]\nuse poison;\nmacro_rules! unused { () => { use poison; }; }\nmacro_rules! include { ($name:ident) => { use live as $name; }; }\ninclude!(selected);\nfunc Value() int { _ = fmt::Sprint(42); return selected::answer!() + second::zero!() }\n")
	write("app/value_test.go", "package p\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=42 { t.Fatal(Value()) } }\n")
	app := filepath.Join(base, "app")
	before := singleFileSnapshot(t, base)
	var output bytes.Buffer
	if err := run([]string{"gen", app}, &output, &output); err != nil {
		t.Fatalf("automatic generation: %v\n%s", err, &output)
	}
	first, err := os.ReadFile(filepath.Join(app, "value_gox.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gen", app}, &output, &output); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(app, "value_gox.go"))
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("unstable generation: %v", err)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "./...")
	command.Dir, command.Env = app, append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent Go test: %v\n%s", err, result)
	}
	for name, contents := range before {
		if singleFileSnapshot(t, base)[name] != contents {
			t.Fatalf("generation changed authored %s", name)
		}
	}
	t.Run("dependency_file_collision_does_not_publish", func(t *testing.T) {
		name := filepath.Join(app, "goxide_tools.gox")
		write("app/goxide_tools.gox", "package p\nfunc Other() int { return 7 }\n")
		defer os.Remove(name)
		before := singleFileSnapshot(t, app)
		err := run([]string{"gen", app}, &output, &output)
		if err == nil || !strings.Contains(err.Error(), "generation dependency file conflicts with source output") {
			t.Fatalf("missing generated output collision: %v", err)
		}
		if !reflect.DeepEqual(before, singleFileSnapshot(t, app)) {
			t.Fatal("conflicting dependency artifact published partial output")
		}
	})
	write("app/value.gox", "package p\nuse poison;\nfunc Value() int { return 42 }\n")
	err = run([]string{"gen", app}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "ambiguous default dependency root \"poison\": example.org/duplicate, example.org/unused") {
		t.Fatalf("ambiguous root diagnostic: %v", err)
	}
}

func TestNativeAutomaticPublishedLibraryEntrypoints(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(base, "library")
	for name, contents := range map[string]string{
		"go.mod":      "module example.org/native-library\ngo 1.27\n",
		"goxide.toml": "[go_mod]\nmodule='example.org/native-library'\ngo='1.27'\n[lib]\nname='library'\npath='.'\n",
		"lib.gox":     "package library\npub func Value() int { return 42 }\n",
		"lib_gox.go":  generatedHeader + "package library\nfunc Value() int { return 42 }\n",
	} {
		singleFileWrite(t, library, name, contents)
	}
	for _, mode := range []string{"package", "file", "stdout", "custom_file", "directory"} {
		t.Run(mode, func(t *testing.T) {
			app := filepath.Join(base, mode)
			projectWrite(t, app, map[string]string{
				"go.mod":   fmt.Sprintf("module example.org/consumer\ngo 1.27\nrequire example.org/native-library v1.0.0\nreplace example.org/native-library => %s\n", library),
				"main.gox": "package main\nuse library;\nfunc main() { println(library::Value()) }\n",
			})
			before := singleFileSnapshot(t, base)
			var first []byte
			for attempt := range 2 {
				input, destination := app, ""
				if mode != "package" && mode != "directory" {
					input = filepath.Join(app, "main.gox")
				}
				if mode == "stdout" {
					destination = "-"
				} else if mode == "custom_file" {
					destination = filepath.Join(base, fmt.Sprintf("result-%d.go", attempt))
				} else if mode == "directory" {
					destination = filepath.Join(base, fmt.Sprintf("directory-output-%d", attempt))
				}
				arguments := []string{"gen"}
				if destination != "" {
					arguments = append(arguments, "-o", destination)
				}
				arguments = append(arguments, input)
				var stdout, stderr bytes.Buffer
				if err := run(arguments, &stdout, &stderr); err != nil {
					t.Fatalf("%s: %v\n%s", mode, err, &stderr)
				}
				generated := filepath.Join(app, "main_gox.go")
				command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
				command.Dir = app
				switch mode {
				case "stdout":
					if err := os.WriteFile(generated, stdout.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
				case "custom_file":
					generated = destination
					command.Args = []string{command.Path, "run", generated}
				case "directory":
					generated = filepath.Join(destination, "main.go")
					command.Dir = destination
				}
				data, err := os.ReadFile(generated)
				if err != nil {
					t.Fatal(err)
				}
				if attempt == 0 {
					first = data
				} else if !bytes.Equal(first, data) {
					t.Fatal("same input produced different generated Go")
				}
				command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
				if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
					t.Fatalf("independent Go run: %v\n%s", err, output)
				}
			}
			for name, contents := range before {
				if singleFileSnapshot(t, base)[name] != contents {
					t.Fatalf("generation changed authored %s", name)
				}
			}
		})
	}
}

func TestNativeAutomaticRemoteReplacement(t *testing.T) {
	for _, replacementModule := range []string{"example.org/library", "example.org/fork"} {
		t.Run(replacementModule, func(t *testing.T) {
			proxy, cache := versionProxy(t)
			cache, err := filepath.EvalSymlinks(cache)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOMODCACHE", cache)
			publishVersion(t, proxy, replacementModule, "v1.4.0", map[string]string{
				"go.mod":      "module " + replacementModule + "\ngo 1.27\n",
				"goxide.toml": "[go_mod]\n[lib]\nname='library'\npath='.'\n",
				"lib.gox":     "package library\npub macro_rules! answer { () => { 42 }; }\n",
				"api.go":      "package library\n",
			})
			root := t.TempDir()
			projectWrite(t, root, map[string]string{
				"go.mod":   fmt.Sprintf("module example.org/consumer\ngo 1.27\nrequire example.org/library v1.0.0\nreplace example.org/library => %s v1.4.0\n", replacementModule),
				"main.gox": "package main\nuse library;\nfunc main() { println(library::answer!()) }\n",
			})
			before := singleFileSnapshot(t, root)
			var stdout, stderr bytes.Buffer
			if err := run([]string{"gen", root}, &stdout, &stderr); err != nil {
				t.Fatalf("remote replacement generation: %v\n%s", err, &stderr)
			}
			command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
			command.Dir, command.Env = root, append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
			if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
				t.Fatalf("independent Go run: %v\n%s", err, output)
			}
			for name, contents := range before {
				if singleFileSnapshot(t, root)[name] != contents {
					t.Fatalf("generation changed authored %s", name)
				}
			}
		})
	}
}

func TestNativeGenerationInstallsOnlyEffectiveMissingRoots(t *testing.T) {
	proxy, cache := versionProxy(t)
	cache, err := filepath.EvalSymlinks(cache)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", cache)
	previousStd, previousSerde := stdSuggestedVersion, serdeSuggestedVersion
	stdSuggestedVersion, serdeSuggestedVersion = "v0.1.0", "v0.2.0"
	t.Cleanup(func() { stdSuggestedVersion, serdeSuggestedVersion = previousStd, previousSerde })
	for name, version := range map[string]string{"std": stdSuggestedVersion, "serde": serdeSuggestedVersion} {
		module := "github.com/goxide-lang/" + name
		publishVersion(t, proxy, module, version, map[string]string{
			"go.mod":      "module " + module + "\ngo 1.27\n",
			"goxide.toml": "[go_mod]\n[lib]\nname='" + name + "'\npath='.'\n",
			"lib.gox":     "package " + name + "\npub macro_rules! answer { () => { 42 }; }\n",
			"api.go":      "package " + name + "\n",
		})
	}
	for _, accept := range []bool{false, true} {
		t.Run(fmt.Sprint(accept), func(t *testing.T) {
			root := t.TempDir()
			singleFileWrite(t, root, "go.mod", "module example.org/install\ngo 1.27\n")
			singleFileWrite(t, root, "main.gox", "package main\n#[cfg(feature(\"off\"))] use serde;\nmacro_rules! unused { () => { use serde; }; }\nuse std;\nfunc main(){println(std::answer!())}\n")
			before := singleFileSnapshot(t, root)
			var stdout, stderr bytes.Buffer
			req := CompileRequest{Mode: "transpile", Input: root, Interactive: accept, PromptInput: strings.NewReader("yes\n")}
			err := executeCompile(req, &stdout, &stderr)
			if !accept {
				if err == nil || !strings.Contains(err.Error(), "go get github.com/goxide-lang/std@v0.1.0") || strings.Contains(err.Error(), "serde") {
					t.Fatalf("inactive roots leaked into diagnostic: %v", err)
				}
				if !reflect.DeepEqual(before, singleFileSnapshot(t, root)) {
					t.Fatal("unconfirmed generation changed authored files")
				}
				return
			}
			if err != nil {
				t.Fatalf("confirmed generation did not retry: %v\n%s", err, &stderr)
			}
			if strings.Count(stderr.String(), "[y/N]") != 1 || strings.Contains(stderr.String(), "serde") {
				t.Fatalf("wrong dependency prompt: %s", &stderr)
			}
			command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", ".")
			command.Dir, command.Env = root, append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
			if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
				t.Fatalf("independent Go run after install: %v\n%s", err, output)
			}
		})
	}
}
