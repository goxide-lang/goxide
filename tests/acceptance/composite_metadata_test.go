package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const compositeMetadataAPI = `package model
type State struct{N int;A [2]int;S []int;M map[string]int;P *int}
func(s *State)Touch(){s.N++}
func View(p &State)&State{return p}
func Edit(p &mut State)&mut State{return p}
func Read(p &State)int{return p.N+p.A[0]+p.S[0]+p.M["x"]+*p.P}
func Native(p *State){p.N++;p.S[0]++}
func Slice(p &[]int)&[]int{return p}
func SliceEdit(p &mut []int)&mut []int{return p}
func Map(p &map[string]int)&map[string]int{return p}
func Array(p &[2]int)&[2]int{return p}
type hidden struct{N int}
func Hidden()&hidden{v:=hidden{N:9};return &v}
`

func TestCompositeReferenceMetadataModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	sourceFor := func(body, ext string) string {
		dir := filepath.Join(t.TempDir(), "source")
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceSerdeRoots(t, dir, "packages.example/app", "model", "mid", "other")
		packagesWrite(t, dir, "model/model.gox", compositeMetadataAPI)
		packagesWrite(t, dir, "model/again.gox", "package model\nfunc Again(r &State)&State{return View(r)}\n")
		packagesWrite(t, dir, "mid/mid.gox", `package mid
use library::model as a;
type State=a::State
func View(r &State)&State{return a::Again(r)}
func Edit(r &mut State)&mut State{return a::Edit(r)}
`)
		packagesWrite(t, dir, "other/other.gox", `package model
type State struct{N int;A [2]int;S []int;M map[string]int;P *int}
func View(p &State)&State{return p}
`)
		packagesWrite(t, dir, "app."+ext, body)
		return dir
	}
	t.Run("business-shapes-aliases-race", func(t *testing.T) {
		source := sourceFor(`package app
use library::model as a;
use library::model as b;
use library::mid as m;
type Alias=a::State
func forwarded(r &Alias)&b::State{return m::View(r)}
func Run()int{n:=5;s:=a::State{N:1,A:[2]int{2,0},S:[]int{3},M:map[string]int{"x":4},P:&n};w:=&mut s;r:=forwarded(&*w);copy:=*r;copy.N++;copy.A[0]++;m::Edit(w).N++;a::Native(&s);s.Touch();if r.N!=4||r.S[0]!=4||copy.N!=2||copy.A[0]!=3{panic("identity/copy changed")};return b::Read(r)}
func Containers()int{s:=[]int{1};w:=&mut s;(*a::SliceEdit(w))[0]++;r:=a::Slice(&*w);m:=map[string]int{"x":3};mr:=a::Map(&m);v:=[2]int{4,5};ar:=a::Array(&v);copy:=*ar;copy[0]++;return (*r)[0]+(*mr)["x"]+copy[0]}
func Private()int{return a::Hidden().N}
`, "gox")
		packagesWrite(t, source, "left.gox", `package app
use library::model as a;
func left(r &a::State)int{return r.N}
func Left()int{s:=a::State{N:10};return left(&s)}
`)
		packagesWrite(t, source, "right.gox", `package app
use library::other as a;
func right(r &a::State)int{return r.N}
func Right()int{s:=a::State{N:20};return right(&s)}
`)
		packagesWrite(t, source, "native.go", `package app
import a "packages.example/app/model"
type any = int
// A source alias with the compiler helper prefix remains a source declaration.
type __goxide_permission_element_1 = a.State
func NativeRead()int{s:=a.State{N:7};return a.View(&s).N}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		twice := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", twice, source)
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
			b, e := os.ReadFile(filepath.Join(twice, rel))
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
		packagesWrite(t, generated, "app_test.go", `package app
import "testing"
func TestStateAndCopies(t *testing.T){if Run()!=19||Private()!=9{t.Fatal("structure identity")}}
func TestContainers(t *testing.T){if Containers()!=10{t.Fatal("container permissions")}}
func TestPathsAndNative(t *testing.T){if Left()!=10||Right()!=20||NativeRead()!=7{t.Fatal("path identity")}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	cases := map[string]string{
		"field write":          `n:=1;s:=a.State{P:&n};r:=a.View(&s);r.N=2`,
		"slice write":          `s:=a.State{S:[]int{1}};r:=a.View(&s);r.S[0]=2`,
		"map write":            `s:=a.State{M:map[string]int{}};r:=a.View(&s);r.M["x"]=2`,
		"array write":          `s:=a.State{};r:=a.View(&s);r.A[0]=2`,
		"pointer write":        `n:=1;s:=a.State{P:&n};r:=a.View(&s);*r.P=2`,
		"copy shared":          `s:=a.State{S:[]int{1}};r:=a.View(&s);copy:=*r;copy.S[0]=2`,
		"interface":            `s:=a.State{};r:=a.View(&s);var x interface{}=r;_=x`,
		"native":               `s:=a.State{};r:=a.View(&s);a.Native(r)`,
		"method":               `s:=a.State{};r:=a.View(&s);r.Touch()`,
		"private return write": `a.Hidden().N=2`,
		"returned slice write": `s:=[]int{1};r:=a.Slice(&s);(*r)[0]=2`,
		"returned map clear":   `s:=map[string]int{};r:=a.Map(&s);clear(*r)`,
		"returned array write": `s:=[2]int{};r:=a.Array(&s);(*r)[0]=2`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			source := sourceFor("package app\nimport a \"packages.example/app/model\"\nfunc bad(){"+body+"}\n", "go")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "app.go") {
				t.Fatal(out)
			}
		})
	}
	t.Run("goxide-upgrade-and-defined-identity", func(t *testing.T) {
		for _, body := range []string{
			`func bad(){s:=a::State{};r:=a::View(&s);a::Edit(r)}`,
			`func bad(){s:=a::State{};r:=a::View(&s);_=&mut *r}`,
			`type Other a::State;func bad(r &a::State)&Other{return r}`,
			`func bad(){s:=a::State{};r:=a::View(&s);other::View(r)}`,
		} {
			imports := "use library::model as a;\n"
			if strings.Contains(body, "other::") {
				imports += "use library::other as other;\n"
			}
			source := sourceFor("package app\n"+imports+body+"\n", "gox")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "app.gox") {
				t.Fatal(out)
			}
		}
	})
}
