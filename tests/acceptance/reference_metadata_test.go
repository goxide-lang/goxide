package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const referenceAPI = `package api
use time;
type hidden struct{N int}
func Hidden()hidden{return hidden{9}}
func Now()time::Time{return time::Unix(0,0)}
func Context(r &int,t time::Time,h hidden)int{return *r+t.Second()+h.N}
type Value struct{}
func(Value)Read()int{return 11}
func Read(r &int)int{return *r}
func Inc(r mut& int){*r+=1}
func View(r &int)&int{return r}
func Edit(r mut& int)mut& int{return r}
`

func TestReferenceMetadataModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	makeSource := func(body, ext string) string {
		source := filepath.Join(t.TempDir(), "source")
		packagesWrite(t, source, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, source, "packages.example/app", map[string]string{"api": "packages.example/app/api", "mid": "packages.example/app/mid", "other": "packages.example/app/other"})
		packagesWrite(t, source, "api/read.gox", referenceAPI)
		packagesWrite(t, source, "api/other.gox", "package api\nfunc Again(r &int)&int{return View(r)}\n")
		packagesWrite(t, source, "mid/mid.gox", `package mid
use api as x;
func View(r &int)&int{return x::Again(r)}
func Edit(r mut& int)mut& int{return x::Edit(r)}
`)
		packagesWrite(t, source, "other/other.gox", `package api
func View(r mut& int)mut& int{return r}
`)
		packagesWrite(t, source, "app."+ext, body)
		return source
	}
	t.Run("business-race-determinism", func(t *testing.T) {
		source := makeSource(`package app
use api as a;
use mid as m;
func Run()int{n:=40;w:=mut& n;a::Inc(w);r:=m::View(&*w);a::Inc(w);if a::Read(r)!=42{panic("alias visibility")};*m::Edit(w)+=1;return n}
func Deferred()int{n:=0;w:=mut& n;func(){defer a::Inc(w)}();return n}
func Goroutine()int{n:=0;w:=mut& n;done:=make(chan bool);go func(){a::Inc(w);done<-true}();<-done;return n}
func Native()int{n:=4;p:=&n;native(p);return n}
func native(p *int){*p+=1}
`, "gox")
		packagesWrite(t, source, "consumer.go", `package app
import alias "packages.example/app/api"
import _ "packages.example/app/api"
import __goxide_permission_import_1 "fmt"
func ImportedHygiene()string{n:=1;return __goxide_permission_import_1.Sprint(alias.Read(&n))}
func GoRead()int{n:=7;return alias.Read(&n)}
func Context()int{n:=2;v:=alias.Value{};return alias.Context(&n,alias.Now(),alias.Hidden())+v.Read()}
// Same spelling as a possible generated helper must never capture a shim.
func Hygiene()int{n:=8;__goxide_permission_call_1:=func(p *int)int{return 0};_=__goxide_permission_call_1;return alias.Read(&n)}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		second := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", second, source)
		err := filepath.WalkDir(generated, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(generated, path)
			a, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			b, e := os.ReadFile(filepath.Join(second, rel))
			if e != nil {
				return e
			}
			if string(a) != string(b) {
				t.Errorf("nondeterministic %s", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		data, e := os.ReadFile(filepath.Join(generated, "mid/.goxide.refs.json"))
		if e != nil || !strings.Contains(string(data), `"Permission": "read"`) {
			t.Fatalf("metadata %s %v", data, e)
		}
		packagesWrite(t, generated, "app_test.go", `package app
import "testing"
func TestBusiness(t *testing.T){if Run()!=43||GoRead()!=7||Hygiene()!=8||Native()!=5||Context()!=22||Deferred()!=1||Goroutine()!=1||ImportedHygiene()!="1"{t.Fatal("permissions changed identity or native semantics")}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	cases := map[string]string{
		"return write":     `n:=1;r:=m.View(&n);*r=2`,
		"write upgrade":    `n:=1;r:=a.View(&n);a.Inc(r)`,
		"raw escape":       `n:=1;r:=m.View(&n);raw(r)`,
		"interface escape": `n:=1;r:=m.View(&n);var erased any=r;_=erased`,
		"closure escape":   `n:=1;r:=m.View(&n);f:=func()*int{return r};_=f`,
		"function value":   `f:=a::Read;_=f`,
		"wrong path":       `n:=1;r:=a.View(&n);other.View(r)`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			imports := ""
			if strings.Contains(body, "a.") {
				imports += "import a \"packages.example/app/api\"\n"
			}
			if strings.Contains(body, "m.") {
				imports += "import m \"packages.example/app/mid\"\n"
			}
			if strings.Contains(body, "other.") {
				imports += "import other \"packages.example/app/other\"\n"
			}
			ext := "go"
			if name == "function value" {
				ext = "gox"
				imports = "use api as a;\n"
			}
			source := makeSource("package app\n"+imports+"func bad(){"+body+"}\nfunc raw(p *int){}\n", ext)
			generated := filepath.Join(t.TempDir(), "generated")
			output := packagesCommand(t, root, cli, false, "transpile", "-o", generated, source)
			if !strings.Contains(output, "app."+ext) {
				t.Fatalf("source file lost: %s", output)
			}
			if name == "function value" && (!strings.Contains(output, "app.gox:3:") || strings.Contains(output, "generated/unmapped")) {
				t.Fatalf("selector origin lost: %s", output)
			}
			if _, e := os.Stat(generated); !os.IsNotExist(e) {
				t.Fatal("failed output not cleaned")
			}
		})
	}
	t.Run("single-file-boundaries", func(t *testing.T) {
		source := makeSource("package app\nfunc Run(){}\n", "gox")
		single := filepath.Join(t.TempDir(), "api.go")
		packagesCommand(t, root, cli, true, "transpile", "-o", single, filepath.Join(source, "api/read.gox"))
		data, e := os.ReadFile(single)
		if e != nil || !strings.Contains(string(data), "GoxideReferenceMetadata") {
			t.Fatalf("single export marker: %s %v", data, e)
		}
		copied := filepath.Join(t.TempDir(), "copied")
		packagesWrite(t, copied, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, copied, "packages.example/app", map[string]string{"api": "packages.example/app/api", "mid": "packages.example/app/mid", "other": "packages.example/app/other"})
		packagesWrite(t, copied, "api/api.go", string(data))
		consumer := "package app\nuse api as x;\nfunc Run(){n:=1;*x::View(&n)=7}\n"
		packagesWrite(t, copied, "app.gox", consumer)
		output := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), copied)
		if !strings.Contains(output, "metadata missing") {
			t.Fatal(output)
		}
		output = packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "single.go"), filepath.Join(copied, "app.gox"))
		if !strings.Contains(output, "metadata missing") || !strings.Contains(output, "app.gox:2:") {
			t.Fatal(output)
		}
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "consumer.gox", consumer)
		output = packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "single.go"), filepath.Join(generated, "consumer.gox"))
		if !strings.Contains(output, "readonly") {
			t.Fatal(output)
		}
	})
	t.Run("missing-metadata-native-distinction", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "source")
		packagesWrite(t, source, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, source, "packages.example/app", map[string]string{"api": "packages.example/app/api", "mid": "packages.example/app/mid", "other": "packages.example/app/other"})
		packagesWrite(t, source, "api/api.go", `package api
const GoxideReferenceMetadata="v1:missing"
func View(p *int)*int{return p}
`)
		packagesWrite(t, source, "app.gox", "package app\nuse api as x;\nfunc Run(){n:=1;*x::View(&n)=2}\n")
		output := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), source)
		if !strings.Contains(output, "metadata missing") || !strings.Contains(output, "app.gox:2:") {
			t.Fatal(output)
		}
		packagesWrite(t, source, "api/api.go", "package api\nconst GoxideReferenceMetadata=1\nfunc View(p *int)*int{return p}\n")
		output = packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), source)
		if strings.Contains(output, "panic:") || !strings.Contains(output, "metadata missing") {
			t.Fatal(output)
		}
		packagesWrite(t, source, "api/api.go", "package api\nfunc View(p *int)*int{return p}\n")
		packagesCommand(t, root, cli, true, "transpile", "-o", filepath.Join(t.TempDir(), "out"), source)
	})
}
