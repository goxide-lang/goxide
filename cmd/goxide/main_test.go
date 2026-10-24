package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOutputProtection(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "input.gox")
	original := []byte("package main\nfunc main() {}\n")
	if err := os.WriteFile(src, original, 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(d, "alias.go")
	if err := os.Symlink(src, symlink); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(d, "hard.go")
	if err := os.Link(src, hardlink); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{src, symlink, hardlink} {
		var out bytes.Buffer
		err := run([]string{"transpile", "-o", target, src}, &out, &out)
		if err == nil || !strings.Contains(err.Error(), "overwrite input") {
			t.Fatalf("target %s: %v", target, err)
		}
	}
	var out bytes.Buffer
	target := filepath.Join(d, "result.go")
	if err := run([]string{"transpile", "-o", target, "--source-map", target, src}, &out, &out); err == nil {
		t.Fatal("removed source-map flag accepted")
	}
	got, _ := os.ReadFile(src)
	if !bytes.Equal(got, original) {
		t.Fatal("input changed")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("wrote output before validation")
	}
}
func formalTestCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "goxide")
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "./scripts/build.go", "-o", binary)
	command.Dir, command.Env = "../..", localEnv()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build stamped CLI: %v\n%s", err, output)
	}
	return binary
}

func TestTranspileArtifacts(t *testing.T) {
	d := t.TempDir()
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if err := writeStandaloneSupportModule(tool, "../..", d); err != nil {
		t.Fatal(err)
	}
	fixtureRootsFile(t, d, "[roots.std]\nmodule='github.com/goxide-lang/std'\n")
	src := filepath.Join(d, "input.gox")
	out := filepath.Join(t.TempDir(), "out.go")
	source := `package sample
use std;
#[derive(std::derive::Debug)]
type Item struct { Name string }
impl Item { pub func inspect(self &Self) string { return std::Debug::debug(self) } }
`
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cli := formalTestCLI(t)
	command := exec.Command(cli, "transpile", "--go", tool, "-o", out, src)
	command.Env = localEnv()
	if log, err := command.CombinedOutput(); err != nil {
		t.Fatalf("transpile: %v\n%s", err, log)
	}
	first, err := os.ReadFile(out)
	if err != nil || !bytes.Contains(first, []byte("Inspect(")) {
		t.Fatalf("missing authored method: %s %v", first, err)
	}
	again := filepath.Join(t.TempDir(), "out.go")
	command = exec.Command(cli, "transpile", "--go", tool, "-o", again, src)
	command.Env = localEnv()
	if log, err := command.CombinedOutput(); err != nil {
		t.Fatalf("repeat transpile: %v\n%s", err, log)
	}
	second, err := os.ReadFile(again)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("transpile artifact changed: %v", err)
	}
}

func TestBuildAndRunGenerated(t *testing.T) {
	d := t.TempDir()
	fixtureRootsFile(t, d, "")
	src := filepath.Join(d, "input.gox")
	target := filepath.Join(d, "program")
	if err := os.WriteFile(src, []byte("package main\nuse fmt;\nfunc main(){fmt::Println(42)}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"build", "--runtime", root, "--go", filepath.Join(runtime.GOROOT(), "bin/go"), "-o", target, src}, &output, &output); err != nil {
		t.Fatalf("build: %v: %s", err, &output)
	}
	got, err := exec.Command(target).CombinedOutput()
	if err != nil || string(got) != "42\n" {
		t.Fatalf("run: %v: %s", err, got)
	}
}

func TestSingleFilePublicRuntimeBuildAndRun(t *testing.T) {
	dir := t.TempDir()
	fixtureRootsFile(t, dir, fixtureSerdeRoots)
	src := filepath.Join(dir, "input.gox")
	source := `package main
use serde;
use fmt;
use serde::json as serdejson;
#[derive(serde::derive::Deserialize)]
type Row struct{Value Option[int]}
func main(){
 row,err:=serdejson::Unmarshal([]byte("{\"Value\":42}"),serde::Read[Row])
 if err!=nil{panic(err)}
 match row.Value{Option::Some(n)=>{fmt::Println(n)},Option::None=>{panic("missing")}}
}
`
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runtimeRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := formalTestCLI(t)
	command := exec.Command(cli, "run", "--runtime", runtimeRoot, "--go", goTool, src)
	command.Env = localEnv()
	if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("run: %v: %s", err, output)
	}
	binary := filepath.Join(t.TempDir(), "program")
	command = exec.Command(cli, "build", "--runtime", runtimeRoot, "--go", goTool, "-o", binary, src)
	command.Env = localEnv()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	got, err := exec.Command(binary).CombinedOutput()
	if err != nil || string(got) != "42\n" {
		t.Fatalf("built program: %v: %s", err, got)
	}
}
