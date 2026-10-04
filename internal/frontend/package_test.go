package frontend

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPackageEnumCycleUsesDeclaringFile(t *testing.T) {
	_, err := preparePackage(map[string][]byte{"a.gox": []byte("package p\npub enum A { V(B), }\n"), "b.gox": []byte("package p\n" + strings.Repeat("// padding\n", 80) + "pub enum B { V(B), }\n")})
	if err == nil || !strings.Contains(err.Error(), "b.gox:82:") || !strings.Contains(err.Error(), "recursive enum") {
		t.Fatalf("wrong error: %v", err)
	}
}
func TestModulePreservesResourcesAndQuotedReplacement(t *testing.T) {
	parent := t.TempDir()
	src := filepath.Join(parent, "src")
	os.MkdirAll(filepath.Join(src, "testdata"), 0755)
	os.MkdirAll(filepath.Join(src, ".assets"), 0755)
	testdeps.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.org/app\n\ngo 1.27\nreplace example.org/dependency => \"../dependency with spaces\"\n"), 0600)
	testdeps.WriteFile(filepath.Join(src, "main.gox"), []byte("package main\nfunc main() {}\n"), 0600)
	testdeps.WriteFile(filepath.Join(src, "testdata", "input.json"), []byte("resource"), 0600)
	testdeps.WriteFile(filepath.Join(src, ".assets", "data.txt"), []byte("hidden"), 0600)
	dest := filepath.Join(parent, "out")
	_, err := CompileModule(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	mod, _ := os.ReadFile(filepath.Join(dest, "go.mod"))
	if !strings.Contains(string(mod), filepath.ToSlash(filepath.Join(parent, "dependency with spaces"))) {
		t.Fatalf("replacement changed: %s", mod)
	}
	for _, p := range []string{"testdata/input.json", ".assets/data.txt"} {
		if _, e := os.Stat(filepath.Join(dest, p)); e != nil {
			t.Fatal(e)
		}
	}
}
func TestModuleOutputAndSelectionBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, filename, source, want string }{
		{"macro", "input.gox", "package p\nfunc f(){dep.log!()}\n", "explicit import"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			testdeps.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.org/p\ngo 1.27\n"), 0600)
			testdeps.WriteFile(filepath.Join(src, tc.filename), []byte(tc.source), 0600)
			dest := filepath.Join(t.TempDir(), "out")
			_, err := CompileModule(src, dest)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			if _, err = os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("failed output was retained")
			}
		})
	}
	t.Run("symlink-parent", func(t *testing.T) {
		parent := t.TempDir()
		src := filepath.Join(parent, "src")
		os.Mkdir(src, 0755)
		testdeps.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.org/p\ngo 1.27\n"), 0600)
		link := filepath.Join(parent, "link")
		if err := os.Symlink(src, link); err != nil {
			t.Skip(err)
		}
		_, err := CompileModule(src, filepath.Join(link, "out"))
		if err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("%v", err)
		}
	})
}

func TestPackageGroupedPayloadRetainsEnumBoundary(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		"go.mod":    "module grouped.example/p\ngo 1.27\n",
		"enum.gox":  "package p\npub enum Mode { Idle, }\n",
		"group.gox": "package p\ntype ( Container struct { Modes []Mode } )\n",
		"use.gox": `package p
func Count(v Result[Container,string])int {
 match v {Result[Container,string]::Ok(c)=>{return len(c.Modes)},Result[Container,string]::Err(_)=>{return -1}}
 return -2
}
`,
	}
	for name, text := range files {
		if err := testdeps.WriteFile(filepath.Join(source, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := CompileModule(source, out); err != nil {
		t.Fatal(err)
	}
	assertions := `package p
import "testing"
func TestGroupedValidation(t *testing.T){
 if Count(GoxideNewResultOk[Container,string](Container{Modes:[]Mode{NewModeIdle()}}))!=1{t.Fatal("valid grouped container")}
 var typed *GoxideCase4_Mode_Idle
 for _,bad:=range []Mode{nil,typed}{func(){defer func(){if recover()==nil{t.Fatal("invalid nested enum escaped validator")}}();Count(GoxideNewResultOk[Container,string](Container{Modes:[]Mode{bad}}))}()}
}
`
	if err := testdeps.WriteFile(filepath.Join(out, "grouped_test.go"), []byte(assertions), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"test", "-race", "-count=1", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), args...)
		cmd.Dir = out
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
		if log, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, log)
		}
	}
}
