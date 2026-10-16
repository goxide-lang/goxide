package acceptance_test

import (
	"strings"
	"testing"
)

func TestReadonlyRebindingFlowRace(t *testing.T) {
	macroRaceModule(t, `package fixture
type Cache struct{Items []int}
func snapshot(r &Cache,b bool)int{
 items:=r.Items
 items=make([]int,len(items));copy(items,r.Items);items[0]++
 branch:=r.Items
 if b {branch=[]int{10}}else{branch=[]int{20}}
 branch[0]++
 local:=func()int{x:=r.Items;x=append([]int(nil),x...);x[0]+=2;return x[0]}
 return items[0]+branch[0]+local()
}
func run(b bool)(int,int){c:=Cache{Items:[]int{3}};return snapshot(&c,b),c.Items[0]}
`, `package fixture
import "testing"
func TestFreshCopies(t *testing.T){for _,b:=range []bool{true,false}{want:=20;if !b{want=30};got,old:=run(b);if got!=want||old!=3{t.Fatalf("got=%d old=%d",got,old)}}}
`)
}
func TestReadonlyRebindingRetainsAliases(t *testing.T) {
	for name, body := range map[string]string{
		"old-alias":    `x:=r.Items;old:=x;x=[]int{1};old[0]++`,
		"one-branch":   `x:=r.Items;if b{x=[]int{1}};x[0]++`,
		"swap":         `x:=r.Items;old:=[]int{1};old,x=x,old;old[0]++`,
		"capture":      `x:=[]int{1};g:=func(){x[0]++};x=r.Items;g()`,
		"loop":         `x:=[]int{1};for b{x[0]++;x=r.Items}`,
		"pointer-copy": `x:=append([]*int(nil),r.Pointers...);*x[0]++`,
		"struct-copy":  `x:=append([]Node(nil),r.Nodes...);*x[0].P++`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("flow.gox", []byte("package fixture\ntype Node struct{P *int};type Cache struct{Items []int;Pointers []*int;Nodes []Node};func f(r &Cache,b bool){"+body+";_ = b}\n"))
			if err == nil || !strings.Contains(err.Error(), "read-only") {
				t.Fatalf("bad rejection: %v", err)
			}
		})
	}
}
