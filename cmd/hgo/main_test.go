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
	src := filepath.Join(d, "input.hgo")
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
		t.Fatal("colliding outputs accepted")
	}
	got, _ := os.ReadFile(src)
	if !bytes.Equal(got, original) {
		t.Fatal("input changed")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("wrote output before validation")
	}
}
func TestTranspileArtifacts(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "input.hgo")
	out := filepath.Join(d, "out.go")
	sm := filepath.Join(d, "map.json")
	os.WriteFile(src, []byte("package sample\n#[derive(Debug)]\ntype Item struct { Name string }\n"), 0600)
	var log bytes.Buffer
	if err := run([]string{"transpile", "-o", out, "--source-map", sm, src}, &log, &log); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || !bytes.Contains(b, []byte("DebugItem")) {
		t.Fatalf("missing helper: %s %v", b, err)
	}
	b, err = os.ReadFile(sm)
	if err != nil || !bytes.Contains(b, []byte("input.hgo")) {
		t.Fatalf("missing map: %s %v", b, err)
	}
}

func TestBuildAndRunGenerated(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "input.hgo")
	target := filepath.Join(d, "program")
	if err := os.WriteFile(src, []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(42)}\n"), 0600); err != nil {
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
	src := filepath.Join(dir, "input.hgo")
	source := `package main
import("fmt";"github.com/goxide-lang/serde/json")
#[derive(Deserialize)]
type Row struct{Value Option[int]}
func main(){
 row,err:=serdejson.Unmarshal([]byte("{\"Value\":42}"),DeserializeRow)
 if err!=nil{panic(err)}
 match row.Value{Option::Some(n)=>{fmt.Println(n)},Option::None=>{panic("missing")}}
}
`
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin/go")
	var output bytes.Buffer
	if err := run([]string{"run", "--runtime", root, "--go", goTool, src}, &output, &output); err != nil || output.String() != "42\n" {
		t.Fatalf("run: %v: %s", err, &output)
	}
	output.Reset()
	binary := filepath.Join(dir, "program")
	if err := run([]string{"build", "--runtime", root, "--go", goTool, "-o", binary, src}, &output, &output); err != nil {
		t.Fatalf("build: %v: %s", err, &output)
	}
	got, err := exec.Command(binary).CombinedOutput()
	if err != nil || string(got) != "42\n" {
		t.Fatalf("built program: %v: %s", err, got)
	}
}
