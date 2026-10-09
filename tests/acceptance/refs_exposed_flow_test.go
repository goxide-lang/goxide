package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadonlyExposedReplacementModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	sourceFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.gox", `package model
func View(r &[]int)&[]int{return r}
`)
		packagesWrite(t, dir, "app.gox", "package app\nuse model as m;\n"+body+"\nfunc Imported(v []int)int{return Capture(m::View(&v))}\n")
		return dir
	}
	const capture = `func Capture(r &[]int)int{x:=*r;f:=func()int{return x[0]};x=[]int{1};x[0]=2;return f()}`
	t.Run("fresh-current-values", func(t *testing.T) {
		source := sourceFor(capture + `
func Discard(r &[]int)int{x:=*r;_=func(){_=x};x=[]int{1};x[0]=2;return x[0]}
func Address(r &[]int)int{x:=*r;p:=&x;x=[]int{1};x[0]=3;return (*p)[0]}
func DiscardAddress(r &[]int)int{x:=*r;_=&x;x=[]int{1};x[0]=4;return x[0]}
func Copy(r &[]int)int{x:=*r;old:=x;_=func(){_=x};x=append([]int(nil),x...);x[0]++;return x[0]+old[0]}
func Branch(r &[]int,b bool)int{x:=*r;_=func(){_=x};if b{x=[]int{5}}else{x=[]int{6}};x[0]++;return x[0]}
func Loop(r &[]int)int{x:=*r;_=func(){_=x};sum:=0;for i:=0;i<2;i++{x=[]int{i};x[0]++;sum+=x[0];x=*r};return sum}
func Closure(r &[]int)int{x:=*r;f:=func()int{x=[]int{8};x[0]++;return x[0]};return f()}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "exposed_test.go", `package app
import "testing"
func TestFreshAndOldStorage(t *testing.T){v:=[]int{10};if Capture(&v)!=2||Discard(&v)!=2||Address(&v)!=3||DiscardAddress(&v)!=4||Copy(&v)!=21||Imported(v)!=2||v[0]!=10{t.Fatal("replacement/storage")}}
func TestControlAndClosure(t *testing.T){v:=[]int{10};if Branch(&v,true)!=6||Branch(&v,false)!=7||Loop(&v)!=3||Closure(&v)!=9||v[0]!=10{t.Fatal("control/closure")}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	for name, body := range map[string]string{
		"old-value":           `x:=*r;old:=x;_=func(){_=x};x=[]int{1};old[0]=9`,
		"old-address":         `x:=[]int{1};p:=&x;x=*r;(*p)[0]=9`,
		"address-after-clean": `x:=*r;x=[]int{1};p:=&x;x=*r;(*p)[0]=9`,
		"closure-later":       `x:=[]int{1};f:=func(){x[0]=9};x=*r;f()`,
		"call-retaint":        `x:=*r;f:=func(){x=*r};x=[]int{1};f();x[0]=9`,
		"same-expression":     `x:=*r;f:=func()int{x=*r;return 0};x=[]int{1};_,y:=f(),x;y[0]=9`,
		"write-order":         `x:=*r;p:=&x;x,*p=[]int{2},*r;x[0]=9`,
		"indirect-then-write": `x:=*r;p:=&x;x=[]int{2};*p=*r;x[0]=9`,
		"interface-old":       `x:=*r;old:=x;_=func(){_=x};x=[]int{1};var a any=old;_=a`,
		"conditional":         `x:=*r;_=func(){_=x};if b{x=[]int{1}};x[0]=9`,
		"zero-trip":           `x:=*r;_=func(){_=x};for b{x=[]int{1};b=false};x[0]=9`,
		"channel-sync":        `x:=*r;start:=make(chan bool);done:=make(chan bool);go func(){<-start;x=*r;done<-true}();x=[]int{1};start<-true;<-done;x[0]=9`,
		"channel-range":       `x:=*r;start:=make(chan bool);done:=make(chan bool);go func(){<-start;x=*r;done<-true;close(done)}();x=[]int{1};close(start);for range done{x[0]=9}`,
		"iterator-callback":   `x:=*r;iter:=func(yield func(int)bool){x=*r;yield(1)};x=[]int{1};for range iter{x[0]=9}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(capture + "\nfunc Bad(r &[]int,b bool){" + body + "}\n")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "readonly") {
				t.Fatal(out)
			}
		})
	}
}
