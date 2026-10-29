package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Each case is compiled by the installed CLI with a real checked dependency;
// the generated module has its own race execution, separate from host tests.
func TestReadonlyControlFlowModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := acceptanceCLI(t)
	sourceFor := func(body string) string {
		dir := filepath.Join(t.TempDir(), "source")
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.gox", `package model
 type State struct { Items []int; Rows [][]int; P *int; A [1][]int }
 func View(s &State)&State{return s}
 `)
		packagesWrite(t, dir, "app.gox", `package app
 use model;
 func touch(v []int){v[0]++}
 `+body)
		return dir
	}
	t.Run("fresh-paths-race", func(t *testing.T) {
		source := sourceFor(`
func For(r &model::State)int{
 x:=r.Items; total:=0
 for i:=0;i<3;i++ { x=append([]int(nil),x...);touch(x);total+=x[0] }
 return total
}
func Range(r &model::State)int{
 x:=r.Items;total:=0
 for _,v:=range r.Items {x=[]int{v};touch(x);total+=x[0]}
 for _,row:=range r.Rows {row=append([]int(nil),row...);touch(row);total+=row[0]}
 return total
}
func Branch(r &model::State,b bool)int{
 x:=[]int{10};if b{x=r.Items;return len(x)};touch(x);return x[0]
}
func Address(r &model::State)int{
 x:=r.Items;old:=&x[0];x=[]int{10};touch(x)
 p:=r.P;oldp:=&*p;n:=20;p=&n;*p++
 return *old+x[0]+*oldp+*p
}
func Break(r &model::State,b bool)int{
 x:=r.Items
 for {if b{x=[]int{10};break};x=[]int{20};break}
 touch(x);return x[0]
}
func Continue(r &model::State)int{
 x:=r.Items;total:=0
 Outer:for i:=0;i<3;touch(x){
  x=[]int{i};i++
  for j:=0;j<1;j++{total+=x[0];continue Outer}
 }
 return total+x[0]
}
func LabelBreak(r &model::State)int{
 x:=r.Items
 Outer:for {for {x=[]int{30};break Outer}}
 touch(x);return x[0]
}
func LocalCopies(r &model::State)int{
 total:=0
 for i:=0;i<2;i++ {v:=*r;v.A=[1][]int{};_ = v; x:=r.Items;x=append([]int(nil),x...);touch(x);total+=x[0]}
 return total
}
func ZeroLocals(r &model::State)int{
 total:=0
 for i:=0;i<2;i++ {var x []int;if x!=nil{panic("nonzero fresh slot")};touchEmpty(x);x=r.Items;total+=len(x)}
 return total
}
func touchEmpty(x []int){if x!=nil{panic("expected nil")}}
func Run(which int,b bool)int{
 n:=4;s:=model::State{Items:[]int{3,5},Rows:[][]int{{7}},P:&n};r:=model::View(&s)
 var out int
 switch which{case 0:out=For(r);case 1:out=Range(r);case 2:out=Branch(r,b);case 3:out=Address(r);case 4:out=Break(r,b);case 5:out=Continue(r);case 6:out=LabelBreak(r);case 7:out=LocalCopies(r);case 8:out=ZeroLocals(r)}
 if s.Items[0]!=3||s.Items[1]!=5||s.Rows[0][0]!=7||n!=4{panic("readonly source changed")}
 return out
}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "flow_test.go", `package app
import "testing"
func TestLoopsAndCopies(t *testing.T){for _,x:=range []struct{k,w int}{{0,15},{1,18},{5,6},{6,31},{7,8},{8,4}}{if got:=Run(x.k,false);got!=x.w{t.Fatalf("case %d: got %d want %d",x.k,got,x.w)}}}
func TestExitingBranches(t *testing.T){if Run(2,false)!=11||Run(2,true)!=2||Run(4,false)!=21||Run(4,true)!=11{t.Fatal("branch/break")}}
func TestAddressIdentity(t *testing.T){if got:=Run(3,false);got!=39{t.Fatal(got)}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	cases := map[string]string{
		"loop carried":            `x:=[]int{1};for i:=0;i<2;i++{x[0]++;x=r.Items}`,
		"zero trips":              `x:=r.Items;for i:=0;i<n;i++{x=[]int{1}};x[0]++`,
		"range zero trips":        `x:=r.Items;for range n{x=[]int{1}};x[0]++`,
		"continue skips refresh":  `x:=[]int{1};for i:=0;i<2;i++{if i==0{x=r.Items;continue};x[0]++}`,
		"post write":              `x:=[]int{1};for i:=0;i<2;touch(x){i++;x=r.Items;continue}`,
		"break before refresh":    `x:=r.Items;for{if n>0{break};x=[]int{1};break};x[0]++`,
		"labelled break":          `x:=r.Items;Outer:for{for{if n>0{break Outer};x=[]int{1};break Outer}};x[0]++`,
		"labelled continue":       `x:=[]int{1};Outer:for i:=0;i<2;i++{for j:=0;j<1;j++{if i==0{x=r.Items;continue Outer};x[0]++}}`,
		"old element address":     `x:=r.Items;p:=&x[0];x=[]int{1};*p++`,
		"old dereference address": `p:=r.P;q:=&*p;v:=1;p=&v;*q++`,
		"address of header":       `x:=[]int{1};p:=&x;x=r.Items;(*p)[0]++`,
		"address of array slot":   `a:=[1][]int{{1}};p:=&a[0];a=r.A;(*p)[0]++`,
		"any after loop":          `x:=[]int{1};for i:=0;i<n;i++{x=r.Items};var erased any=x;_=erased`,
		"call after loop":         `x:=[]int{1};for i:=0;i<n;i++{x=r.Items};touch(x)`,
		"one branch":              `x:=r.Items;if n>0{x=[]int{1}};x[0]++`,
		"range frozen source":     `rows:=r.Rows;for _,row:=range rows{rows=[][]int{{1}};row[0]++}`,
		"range assignment":        `x:=[]int{1};for _,x=range r.Rows{x[0]++}`,
		"range map key":           `m:=map[*int]int{};_ = m;for p:=range rmap{*p++}`,
		"closure capture":         `x:=[]int{1};f:=func(){x[0]++};x=r.Items;f()`,
		"switch escape":           `x:=r.Items;for{switch n{case 1:break};if n==2{break};x=[]int{1};break};x[0]++`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			source := sourceFor("func Bad(r &model::State,n int,rmap &map[*int]int){" + strings.ReplaceAll(body, "range rmap", "range *rmap") + "}\n")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "read-only") {
				t.Fatalf("not a permission rejection: %s", out)
			}
		})
	}
}
