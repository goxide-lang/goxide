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
)

func TestDirectoryBuildProtectsSourceModule(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "cmd")
	os.Mkdir(src, 0755)
	mod := []byte("module example.org/app\ngo 1.27\n")
	os.WriteFile(filepath.Join(root, "go.mod"), mod, 0600)
	os.WriteFile(filepath.Join(src, "main.gox"), []byte("package main\nfunc main(){}\n"), 0600)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, dest := range []string{filepath.Join(src, ".hidden"), filepath.Join(root, "go.mod"), filepath.Join(root, "new-binary")} {
		var log bytes.Buffer
		err := run([]string{"build", "--go", tool, "-o", dest, src}, &log, &log)
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

func TestDirectoryTagsReferencesAndExplicitCompiler(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	os.MkdirAll(filepath.Join(src, "dep"), 0700)
	for name, source := range map[string]string{
		"go.mod":         "module example.org/app\ngo 1.27\n",
		"main.gox":       "package main\nuse fmt;\nuse dep as d;\nfunc main(){fmt::Println(Run(),d::Value())}\nfunc Run()int{n:=40;w:=&mut n;increment(w);return read(&*w)}\n",
		"read.gox":       "package main\nfunc read(r &int)int{return *r}\n",
		"write.gox":      "package main\nfunc increment(r &mut int){*r+=2}\n",
		"race.gox":       "//go:build race\n\npackage main\nfunc RaceSelected()bool{return true}\n",
		"notrace.gox":    "//go:build !race\n\npackage main\nfunc RaceSelected()bool{return false}\n",
		"main_test.go":   "package main\nimport \"testing\"\nfunc TestRun(t *testing.T){if Run()!=42{t.Fatal(Run())};if !RaceSelected(){t.Fatal(\"wrong race selection\")}}\n",
		"dep/base.gox":   "//go:build !feature\n\npackage dep\nfunc Value()string{return \"base\"}\n",
		"dep/tagged.gox": "//go:build feature\n\npackage dep\nfunc Value()string{return native()}\n",
		"dep/native.go":  "//go:build feature\n\npackage dep\nfunc native()string{return \"tagged\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixtureRootsFile(t, src, `[roots.dep]
module="example.org/app"
package="example.org/app/dep"
`)
	fake := filepath.Join(root, "bin")
	os.Mkdir(fake, 0700)
	os.WriteFile(filepath.Join(fake, "go"), []byte("#!/bin/sh\nexit 97\n"), 0700)
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, mode := range []string{"run", "test"} {
		var out bytes.Buffer
		if err := run([]string{mode, "--go", tool, "--tags", "feature", src}, &out, &out); err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, out.String())
		}
		if mode == "run" && !strings.Contains(out.String(), "42 tagged") {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	if err := run([]string{"run", "--go", tool, src}, &out, &out); err != nil || !strings.Contains(out.String(), "42 base") {
		t.Fatalf("%v: %s", err, out.String())
	}
}

func TestDirectoryTranspileKeepsSourcesAcrossLazyRoots(t *testing.T) {
	base := t.TempDir()
	for name, value := range map[string]int{"first": 40, "second": 2} {
		singleFileWrite(t, base, name+"/go.mod", "module example.org/"+name+"\ngo 1.27\n")
		singleFileWrite(t, base, name+"/goxide.toml", "[go_mod]\nmodule='example.org/"+name+"'\ngo='1.27'\n[lib]\nname='"+name+"'\npath='.'\n")
		singleFileWrite(t, base, name+"/api.go", "package "+name+"\n")
		singleFileWrite(t, base, name+"/lib.gox", fmt.Sprintf("package %s\npub macro_rules! value { () => { %d }; }\n", name, value))
	}
	singleFileWrite(t, base, "app/go.mod", "module example.org/app\ngo 1.27\nrequire (\nexample.org/first v0.0.0\nexample.org/second v0.0.0\n)\nreplace example.org/first => ../first\nreplace example.org/second => ../second\n")
	singleFileWrite(t, base, "app/main.gox", "package main\nuse fmt;\nuse first;\nuse second;\nfunc main(){fmt::Println(first::value!()+second::value!(), Message)}\n")
	singleFileWrite(t, base, "app/assets.go", "package main\nimport _ \"embed\"\n//go:embed data/message.txt\nvar Message string\n")
	singleFileWrite(t, base, "app/data/message.txt", "inventory")
	before := singleFileSnapshot(t, base)
	output := filepath.Join(t.TempDir(), "generated")
	var diagnostics bytes.Buffer
	if err := run([]string{"transpile", "-o", output, filepath.Join(base, "app")}, &diagnostics, &diagnostics); err != nil {
		t.Fatalf("directory generation: %v\n%s", err, &diagnostics)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	command.Dir, command.Env = output, append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if got, err := command.CombinedOutput(); err != nil || string(got) != "42 inventory\n" {
		t.Fatalf("generated native module: %v\n%s", err, got)
	}
	if !reflect.DeepEqual(before, singleFileSnapshot(t, base)) {
		t.Fatal("lazy root activation changed authored modules")
	}
}
