package acceptance_test

import (
	"fmt"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCrossModuleReferenceArtifacts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	module := func(path, library string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", path)
		if library != "" {
			s += fmt.Sprintf("require library.example/refs v0.0.0\nreplace library.example/refs => %s\n", filepath.ToSlash(library))
		}
		return s
	}
	writeRoots := func(dir string) {
		writeAcceptanceRoots(t, dir, map[string]rootconfig.Mapping{
			"api":   {Module: "library.example/refs", Package: "library.example/refs/api"},
			"relay": {Module: "library.example/refs", Package: "library.example/refs/relay"},
			"model": {Module: "library.example/refs", Package: "library.example/refs/model"},
			"app":   {Module: "consumer.example/app", Package: "consumer.example/app"},
		})
	}
	const api = `package api
type Cell struct{N int}
func Read(p &int)int{return *p}
var global int=6
func Global()&int{return &global}
func View(p &int)&int{return p}
func Edit(p &mut int)&mut int{return p}
func Inc(p &mut int){*p+=1}
func CellView(p &Cell)&Cell{return p}
`
	makeLibrary := func(relay, drift bool) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("library.example/refs", ""))
		writeRoots(dir)
		src := api
		if drift {
			src = strings.Replace(src, "func Read(p &int)int{return *p}", "func Read(p &mut int)int{*p+=1;return *p}", 1)
		}
		packagesWrite(t, dir, "api/api.gox", src)
		packagesWrite(t, dir, "api/native_default.go", "//go:build !extra\n\npackage api\nfunc Native()int{return 1}\n")
		packagesWrite(t, dir, "api/native_extra.go", "//go:build extra\n\npackage api\nfunc Native()int{return 2}\n")
		if relay {
			packagesWrite(t, dir, "relay/relay.gox", `package relay
use api as a;
type Cell=a::Cell
func Global()&int{return a::Global()}
func Read(p &int)int{return a::Read(p)}
func View(p &int)&int{return a::View(p)}
func Edit(p &mut int)&mut int{return a::Edit(p)}
func CellView(p &Cell)&Cell{return a::CellView(p)}
`)
		}
		return dir
	}
	source := makeLibrary(true, false)
	library := filepath.Join(t.TempDir(), "library")
	packagesCommand(t, root, cli, true, "transpile", "-o", library, source)
	compare := func(a, b string) {
		t.Helper()
		e := filepath.WalkDir(a, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(a, path)
			x, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			y, e := os.ReadFile(filepath.Join(b, rel))
			if e != nil {
				return e
			}
			if string(x) != string(y) {
				t.Fatalf("nondeterministic %s", rel)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	twice := filepath.Join(t.TempDir(), "library")
	packagesCommand(t, root, cli, true, "transpile", "-o", twice, source)
	compare(library, twice)
	makeApp := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("consumer.example/app", library))
		writeRoots(dir)
		packagesWrite(t, dir, "app.gox", "package app\nuse relay as r;\n"+body)
		return dir
	}
	body := `use api as a;
func Run()int{n:=1;w:=r::Edit(&mut n);*w+=2;a::Inc(w);c:=a::Cell{N:5};v:=r::CellView(&c);c.N=7;return r::Read(&n)+v.N}
func Again(p &int)&int{return r::View(p)}
`
	appSource := makeApp(body)
	packagesWrite(t, appSource, "extra.gox", "package app\nuse api as renamed;\nfunc Extra()int{n:=2;renamed::Inc(&mut n);return renamed::Read(&n)}\n")
	packagesWrite(t, appSource, "client/client.gox", "package client\nuse app;\nfunc Read()int{n:=8;return *app::Again(&n)}\n")
	app := filepath.Join(t.TempDir(), "app")
	compileAcceptanceGraph(t, true, appSource, app, source)
	appTwice := filepath.Join(t.TempDir(), "app")
	compileAcceptanceGraph(t, true, appSource, appTwice, source)
	compare(app, appTwice)
	packagesWrite(t, library, "api/api_test.go", `package api
import "testing"
func TestNativeView(t *testing.T){n:=1;p:=View(&n);*p=4;if Read(&n)!=4{t.Fatal(n)}}
`)
	packagesWrite(t, app, "app_test.go", `package app_test
import("testing";a "consumer.example/app";c "consumer.example/app/client")
func TestCrossModule(t *testing.T){if a.Run()!=11||a.Extra()!=3||c.Read()!=8{t.Fatal("cross-module identity or reference behavior")}}
`)
	for _, dir := range []string{library, app} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
		graph := packagesCommand(t, dir, tool, true, "list", "-mod=readonly", "-m", "all")
		if strings.Contains(graph, "github.com/goxide-lang/goxide") {
			t.Fatalf("generated reference module depends on compiler: %s", graph)
		}
	}
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", module("plain.example/app", library))
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";r "library.example/refs/relay")
func TestGoIgnoresGoxidePermissions(t *testing.T){n:=1;p:=r.View(&n);*p=9;if r.Read(&n)!=9{t.Fatal(n)}}
`)
	packagesCommand(t, plain, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, plain, tool, true, "vet", "./...")
	for name, bad := range map[string]string{
		"readonly-return-only": `func Bad(){p:=r::Global();*p=9}`,
		"readonly-write":       `func Bad(){n:=1;p:=r::View(&n);*p=9}`,
		"readonly-upgrade":     `func Bad(){n:=1;p:=r::View(&n);_=r::Edit(p)}`,
		"readonly-field":       `func Bad(){c:=r::Cell{N:1};p:=r::CellView(&c);p.N=9}`,
		"readonly-interface":   `func Bad(){n:=1;p:=r::View(&n);var x any=p;_=x}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := makeApp(bad)
			out := compileAcceptanceGraph(t, false, dir, filepath.Join(t.TempDir(), "out"), source)
			if !strings.Contains(out, "read-only") && !strings.Contains(out, "reference") && !strings.Contains(out, "cannot use") {
				t.Fatal(out)
			}
		})
	}
	t.Run("single-file-api-and-source-permissions", func(t *testing.T) {
		good := makeApp("func Read()int{return *r::Global()}")
		out := filepath.Join(t.TempDir(), "read.go")
		packagesCommand(t, root, cli, true, "transpile", "-o", out, filepath.Join(good, "app.gox"))
		b, e := os.ReadFile(out)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "__goxide_permission_call_") {
			t.Fatal("binding stub leaked")
		}
		bad := makeApp("func Bad(){p:=r::Global();*p=9}")
		diagnostic := compileAcceptanceGraph(t, false, bad, filepath.Join(t.TempDir(), "bad"), source)
		if !strings.Contains(diagnostic, "read-only") {
			t.Fatal(diagnostic)
		}
	})
}
