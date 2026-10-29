package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fieldFlowAPI = `package model
type Leaf struct{Items []int;Counts map[string]int;P *int}
type Pair struct{Left Leaf;Right Leaf}
type Embedded struct{Leaf}
type Indirect struct{*Leaf}
func View(p &Pair)&Pair{return p}
func LeafView(p &Leaf)&Leaf{return p}
func EmbeddedView(p &Embedded)&Embedded{return p}
func IndirectView(p &Indirect)&Indirect{return p}
`

func TestReadonlyFieldReplacementModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := acceptanceCLI(t)
	sourceFor := func(body string) string {
		dir := filepath.Join(t.TempDir(), "source")
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.gox", fieldFlowAPI)
		packagesWrite(t, dir, "app.gox", "package app\nuse model as m;\n"+body)
		return dir
	}
	t.Run("local-fields-race", func(t *testing.T) {
		source := sourceFor(`
func touch(v m::Leaf){v.Items[0]++;v.Counts["x"]++;*v.P++}
func All(r &m::Leaf)int{
 v:=*r;old:=v; n:=20
 v.Items=append([]int(nil),v.Items...);v.Counts=map[string]int{"x":10};v.P=&n
 touch(v)
 return v.Items[0]+v.Counts["x"]+*v.P+old.Items[0]+old.Counts["x"]+*old.P
}
func Nested(r &m::Pair)int{
 v:=*r;old:=v.Left;v.Left.Items=[]int{10};v.Left.Items[0]++
 v.Right.Counts=map[string]int{"x":20};v.Right.Counts["x"]++
 return v.Left.Items[0]+v.Right.Counts["x"]+old.Items[0]+v.Left.Counts["x"]+v.Right.Items[0]
}
func TaintedThenClean(r &m::Leaf)int{v:=m::Leaf{};v.Items=r.Items;v.Items=[]int{10};v.Items[0]++;return v.Items[0]}
func Embedded(r &m::Embedded)int{v:=*r;v.Items=[]int{10};v.Items[0]++;return v.Items[0]+r.Items[0]}
func BranchLoop(r &m::Pair,b bool)int{
 v:=*r
 if b{v.Left.Items=[]int{10}}else{v.Left.Items=[]int{20}}
 v.Left.Items[0]++
 sum:=v.Left.Items[0]
 for i:=0;i<2;i++{v.Right.Items=[]int{i};v.Right.Items[0]++;sum+=v.Right.Items[0];v.Right.Items=r.Right.Items}
 return sum+v.Right.Items[0]
}
func Order(r &m::Pair)int{
 v:=*r;v.Left.Items,v.Left=r.Left.Items,m::Leaf{Items:[]int{10}}
 v.Left.Items[0]++
 old:=v.Left;v.Left,v.Left.Items=m::Leaf{},r.Left.Items
 v.Left.Items=[]int{20};v.Left.Items[0]++
 return old.Items[0]+v.Left.Items[0]
}
func Range(r &m::Pair)int{
 v:=*r;sum:=0
 for _,v.Left.Items=range [][]int{{10},{20}}{v.Left.Items[0]++;sum+=v.Left.Items[0]}
 return sum
}
func RangeQualified(r &m::Pair)int{
 v:=*r;rows:=[][]int{{1},{2}};var rr &[][]int=&rows;sum:=0
 for _,v.Left.Items=range *rr{sum+=v.Left.Items[0];v.Left.Items=[]int{10};v.Left.Items[0]++;sum+=v.Left.Items[0]}
 return sum
}
func IndependentCalls(r &m::Leaf)int{
 v:=*r;v.Items=nil;v.Counts=nil;v.P=nil
 clearLeaf(v)
 return len(r.Items)
}
func clearLeaf(v m::Leaf){if v.Items!=nil||v.Counts!=nil||v.P!=nil{panic("not replaced")}}
// An existing pointer receiver does not expose this independent local header.
type Native struct{N int}
func(n *Native)Inc(){n.N++}
func NativePointers(r &m::Leaf)int{n:=Native{};p:=&n;p.Inc();v:=*r;v.Items=[]int{1};v.Items[0]++;return n.N+v.Items[0]}
func Run(which int,b bool)int{
 n:=7;leaf:=m::Leaf{Items:[]int{3},Counts:map[string]int{"x":5},P:&n};pair:=m::Pair{Left:leaf,Right:leaf};embedded:=m::Embedded{Leaf:leaf}
 var out int
 switch which{case 0:out=All(m::LeafView(&leaf));case 1:out=Nested(m::View(&pair));case 2:out=TaintedThenClean(m::LeafView(&leaf));case 3:out=Embedded(m::EmbeddedView(&embedded));case 4:out=BranchLoop(m::View(&pair),b);case 5:out=Order(m::View(&pair));case 6:out=Range(m::View(&pair));case 7:out=IndependentCalls(m::LeafView(&leaf));case 8:out=NativePointers(m::LeafView(&leaf));case 9:out=RangeQualified(m::View(&pair))}
 if leaf.Items[0]!=3||leaf.Counts["x"]!=5||n!=7{panic("old source changed")}
 return out
}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "fields_test.go", `package app
import "testing"
func TestReplacementAndOldCopies(t *testing.T){for _,x:=range []struct{k,w int}{{0,51},{1,43},{2,11},{3,14},{5,32},{7,1},{8,3}}{if got:=Run(x.k,false);got!=x.w{t.Fatalf("case %d got %d want %d",x.k,got,x.w)}}}
func TestFieldControlFlow(t *testing.T){if Run(4,false)!=27||Run(4,true)!=17||Run(6,false)!=32||Run(9,false)!=25{t.Fatal("branch/loop/range")}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	cases := map[string]string{
		"old struct copy":      `v:=*r;old:=v;v.Left.Items=[]int{1};old.Left.Items[0]++`,
		"old field alias":      `v:=*r;old:=v.Left.Items;v.Left.Items=[]int{1};old[0]++`,
		"sibling field":        `v:=*r;v.Left.Items=[]int{1};v.Right.Items[0]++`,
		"sibling map":          `v:=*r;v.Left.Items=[]int{1};v.Left.Counts["x"]++`,
		"sibling pointer":      `v:=*r;v.Left.Items=[]int{1};*v.Left.P++`,
		"requalified field":    `v:=m::Pair{};v.Left.Items=r.Left.Items;v.Left.Items[0]++`,
		"whole any":            `v:=*r;v.Left.Items=[]int{1};var x any=v;_=x`,
		"field any":            `v:=m::Pair{};v.Left.Items=r.Left.Items;var x any=v.Left.Items;_=x`,
		"field interface slot": `v:=struct{X any}{};v.X=r.Left.Items`,
		"one branch":           `v:=*r;if b{v.Left.Items=[]int{1}};v.Left.Items[0]++`,
		"loop carried field":   `v:=m::Pair{Left:m::Leaf{Items:[]int{1}}};for i:=0;i<2;i++{v.Left.Items[0]++;v.Left.Items=r.Left.Items}`,
		"zero trip field":      `v:=*r;for b{v.Left.Items=[]int{1};b=false};v.Left.Items[0]++`,
		"assignment order":     `v:=*r;v.Left,v.Left.Items=m::Leaf{},r.Left.Items;v.Left.Items[0]++`,
		"captured field":       `v:=m::Pair{};f:=func(){v.Left.Items[0]++};v.Left.Items=r.Left.Items;f()`,
		"address of local":     `v:=m::Pair{};p:=&v;v.Left.Items=r.Left.Items;p.Left.Items[0]++`,
		"readonly root":        `r.Left.Items=[]int{1}`,
		"pointer selection":    `v:=*r;p:=&v.Left;p.Items=[]int{1};v.Left.Items[0]++`,
		"shared pointer path":  `n:=1;leaf:=m::Leaf{P:&n};ind:=m::Indirect{Leaf:&leaf};iv:=m::IndirectView(&ind);v:=*iv;v.Items=[]int{1}`,
		"slice storage":        `v:=*r;v.Left.Items[0]=1`,
		"map storage":          `v:=*r;v.Left.Counts["x"]=1`,
		"range tainted field":  `v:=m::Pair{};rows:=[][]int{{1}};var rr &[][]int=&rows;for _,v.Left.Items=range *rr{v.Left.Items[0]++}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			source := sourceFor("func Bad(r &m::Pair,b bool){" + body + "}\n")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "read-only") {
				t.Fatalf("not a permission rejection: %s", out)
			}
		})
	}
	for name, call := range map[string]string{"method call": "v.Save()", "method value": "f:=v.Save;f()", "promoted method": "e:=Envelope{};e.Save();e.Local.Items=r.Left.Items;saved.Items[0]++;return"} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(`type Local m::Leaf
var saved *Local
func(v *Local)Save(){saved=v}
type Envelope struct{Local}
func Bad(r &m::Pair){v:=Local{};` + call + `;v.Items=r.Left.Items;saved.Items[0]++}
`)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "read-only") {
				t.Fatalf("not a permission rejection: %s", out)
			}
		})
	}
}
