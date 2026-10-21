package check_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestSoleMultiResultReferencePermissions(t *testing.T) {
	for _, test := range []struct {
		name, source string
		reject       bool
	}{
		{"read first", `func Pair[T any](v T,e error)T{return v};func vals(r &int)(&int,error){return r,nil};func f(r &int){p:=Pair(vals(r));_=*p}`, false},
		{"write first", `func Pair[T any](v T,e error)T{return v};func vals(r &int)(&int,error){return r,nil};func f(r &int){p:=Pair(vals(r));*p=9}`, true},
		{"mutable first", `func Pair[T any](v T,e error)T{return v};func vals(r &mut int)(&mut int,error){return r,nil};func f(r &mut int){p:=Pair(vals(r));*p=9}`, false},
		{"write second", `func Pick[T any](n int,v T)T{return v};func vals(r &int)(int,&int){return 0,r};func f(r &int){p:=Pick(vals(r));*p=9}`, true},
		{"generic erasure", `func Hide[T any](v T,e error)any{return v};func vals(r &int)(&int,error){return r,nil};func f(r &int){_=Hide(vals(r))}`, true},
		{"inner effects", `func sink(n int,e error){};func f(r &[]int){items:=[]int{1};native:=func()(int,error){items=*r;return 0,nil};sink(native());items[0]=9}`, true},
		{"inner effects replaced", `func sink(n int,e error){};func f(r &[]int){items:=[]int{1};native:=func()(int,error){items=*r;return 0,nil};sink(native());items=[]int{2};items[0]=9}`, false},
		{"function value stays single", `func Keep[T any](v T)T{return v};func native()func()(int,error){return func()(int,error){return 42,nil}};func f(){v:=Keep(native());_,_=v()}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code != check.ReferencePermission {
					t.Fatalf("unexpected diagnostic: %v", diagnostic)
				}
			}
			if (len(diagnostics) > 0) != test.reject {
				t.Fatalf("permission rejection=%v, want %v: %v", len(diagnostics) > 0, test.reject, diagnostics)
			}
		})
	}
}

func TestSoleMultiResultConstInference(t *testing.T) {
	demandProgram(t, `trait Read {func read(self Self)int};impl Read for int {func read(self Self)int{return self}};impl Read for string {func read(self Self)int{return len(self)}}
func Sum[T Read,U Read,const N int](first T,values [N]U)int{return first.read()+N+values[0].read()}
func vals()(int,[2]string){return 39,[2]string{"a","b"}}
func run()int{return Sum(vals())}`)
	files, resolved := parse(t, `package p;func Same[const N int](a,b [N]int){};func vals()([2]int,[3]int){return [2]int{},[3]int{}};func run(){Same(vals())}`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "consistent const array arguments") {
			return
		}
	}
	t.Fatalf("diagnostics=%v, want conflicting const lengths", diagnostics)
}
