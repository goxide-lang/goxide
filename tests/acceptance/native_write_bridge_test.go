package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeWritableScalarArguments(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	sourceFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		packagesWrite(t, dir, "native/native.go", `package native
import "sync/atomic"
var saved *int64
func Write(p *int64,n int64){*p=n}
func Keep(p *int64){saved=p}
func Saved()*int64{return saved}
func Echo(p *int64)*int64{return p}
func Many(ps ...*int64){for _,p:=range ps{*p+=1}}
func Async(p *int64,done chan<-struct{}){atomic.AddInt64(p,4);close(done)}
type Count int64
type Other int64
type Alias=int64
type Writer func(*int64,int64)
func CountWrite(p *Count){*p+=3}
func OtherWrite(p *Other){*p+=3}
func AliasWrite(p *Alias){*p+=2}
func Sink(v any){}
func Composite(p *[]int){}
func Generic[T any](p *T){}
`)
		packagesWrite(t, dir, "views/views.gox", `package views
func Edit(p mut& int64)mut& int64{return p}
func View(p &int64)&int64{return p}
`)
		packagesWrite(t, dir, "app.gox", `package app
import("sync/atomic";"runtime";n "packages.example/app/native";v "packages.example/app/views")
var _=atomic.LoadInt64
var _=runtime.GC
var _=n.Saved
func importedView(){var x int64;_=v.Edit(mut& x)}
`+body)
		return dir
	}
	t.Run("original-storage", func(t *testing.T) {
		source := sourceFor(`
func Direct()int64{var x int64;w:=v.Edit(mut& x);r:=v.View(&(*w));atomic.AddInt64(w,3);n.Write(w,5);p:=n.Echo(w);*p+=2;n.Keep(w);*n.Saved()+=4;return *r}
func Local(p *int64){*p+=2}
func Forms()int64{var x int64;w:=mut& x;Local(w);f:=n.Write;f(w,3);var g n.Writer=n.Write;g(w,4);n.Many(w,w);n.AliasWrite(w);done:=make(chan struct{});go n.Async(w,done);<-done;return x}
func Deferred()int64{var x int64;w:=mut& x;func(){defer n.Write(w,9)}();return x}
func KeepLocal(){var x int64=12;n.Keep(mut& x)}
func Lifetime()int64{KeepLocal();runtime.GC();*n.Saved()+=1;return *n.Saved()}
func Named()n.Count{var x n.Count=2;n.CountWrite(mut& x);return x}
func Once(w mut& int64,c mut& int)mut& int64{*c+=1;return w}
func EvaluatedOnce()(int64,int){var x int64;count:=0;n.Write(Once(mut& x,mut& count),7);return x,count}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "native_test.go", `package app
import "testing"
func TestStorage(t *testing.T){if got:=Direct();got!=11{t.Fatal(got)};if got:=Lifetime();got!=13{t.Fatal(got)}}
func TestCalls(t *testing.T){if got:=Forms();got!=12{t.Fatal(got)};if got:=Deferred();got!=9{t.Fatal(got)};if got:=Named();got!=5{t.Fatal(got)}}
func TestEvaluation(t *testing.T){x,n:=EvaluatedOnce();if x!=7||n!=1{t.Fatal(x,n)}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	for name, body := range map[string]string{
		"readonly":         `func Bad(r &int64){n.Write(r,1)}`,
		"narrowed":         `func Bad(w mut& int64){n.Write(&(*w),1)}`,
		"readonly-upgrade": `func Bad(r &int64){n.Write(mut& (*r),1)}`,
		"interface":        `func Bad(w mut& int64){n.Sink(w)}`,
		"raw-variable":     `func Bad(w mut& int64){var p *int64=w;_=p}`,
		"raw-return":       `func Bad(w mut& int64)*int64{return w}`,
		"raw-conversion":   `func Bad(w mut& int64){_=(*int64)(w)}`,
		"wrong-named":      `func Bad(w mut& n.Count){n.Write(w,1)}`,
		"other-named":      `func Bad(w mut& n.Count){n.OtherWrite(w)}`,
		"composite":        `func Bad(w mut& []int){n.Composite(w)}`,
		"generic":          `func Bad(w mut& int64){n.Generic[int64](w)}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(body)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "reference") && !strings.Contains(out, "readonly") && !strings.Contains(out, "cannot use") {
				t.Fatal(out)
			}
		})
	}
}
