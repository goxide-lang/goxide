package check_test

import (
	"testing"

	"github.com/hxsf/goxide/internal/check"
)

func TestReferenceControlFlowPreservesReachablePermissions(t *testing.T) {
	for _, test := range []struct {
		name, body string
		reject     bool
	}{
		{"continue-post", `x:=[]int{1};for i:=0;i<2;x=*r {i++;continue};x[0]++`, true},
		{"labeled-break-replacement", `x:=*r;Outer:for {for {x=[]int{1};break Outer}};x[0]++`, false},
		{"labeled-break-readonly", `x:=[]int{1};Outer:for {for {x=*r;break Outer}};x[0]++`, true},
		{"range-source-snapshot", `x:=*r;for _,value:=range [][]int{x} {x=[]int{1};value[0]++}`, true},
		{"nonempty-range-replacement", `x:=*r;for range [1]int{} {x=[]int{1}};x[0]++`, false},
		{"forward-goto-replacement", `x:=*r;if b {goto Reset};x=[]int{2};Reset:x=[]int{1};x[0]++`, false},
		{"forward-goto-readonly", `x:=*r;if b {goto Write};x=[]int{1};Write:x[0]++`, true},
		{"backward-goto-readonly", `x:=[]int{1};Write:x[0]++;if b {x=*r;goto Write}`, true},
		{"switch-terminating-path", `x:=*r;switch {case b:return;default:x=[]int{1}};x[0]++`, false},
		{"switch-fallthrough", `x:=[]int{1};switch {case b:x=*r;fallthrough;default:x[0]++}`, true},
		{"select-join", `x:=[]int{1};select {case <-ch:x=*r;default:};x[0]++`, true},
		{"select-terminating-path", `x:=*r;select {case <-ch:return;default:x=[]int{1}};x[0]++`, false},
		{"match-guard-writeback", `x:=[]int{1};install:=func()bool{x=*r;return false};match E::A {E::A if install()=>{},E::A=>{x[0]++}}`, true},
		{"match-terminating-path", `x:=*r;match E::A {E::A=>{return}};x[0]++`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;enum E{A,};func f(r &[]int,b bool,ch chan int){"+test.body+"}")
			_, diagnostics := check.Check(files, resolved)
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Code != check.ReferencePermission {
					t.Fatalf("unexpected diagnostic: %v", diagnostic)
				}
				found = true
			}
			if found != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", found, test.reject, diagnostics)
			}
		})
	}
}
