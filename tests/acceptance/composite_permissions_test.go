package acceptance_test

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/refs"
)

// Independent acceptance: a copied value slot is writable, while the shared
// storage reached through a read view retains its permission after local copies.
func TestCompositePermissionValuesAndAliasesRace(t *testing.T) {
	macroRaceModule(t, `package fixture
type Leaf struct { N int }
type State struct { N int; Value Leaf; Values [2]int; Items []int; Index map[string]int; Pointer *int }
func inspect(r &State) int {
 copy := *r
 copy.N++
 copy.Value.N++
 copy.Values[0]++
 value := r.Value
 value.N++
 array := r.Values
 array[0]++
 items := r.Items
 index := r.Index
 pointer := r.Pointer
 return copy.N+copy.Value.N+copy.Values[0]+value.N+array[0]+items[0]+index["x"]+*pointer
}
func update(w mut& State) {
 w.N++
 w.Items[0]++
 w.Index["x"]++
 (*w.Pointer)++
}
func exercise() [3]int {
 n:=7
 state:=State{N:1,Value:Leaf{N:2},Values:[2]int{3,4},Items:[]int{5},Index:map[string]int{"x":6},Pointer:&n}
 var r &State = &state
 first:=inspect(r)
 w:=mut& state
 update(w)
 second:=inspect(r)
 return [3]int{first,second,state.N+state.Value.N+state.Values[0]+state.Items[0]+state.Index["x"]+n}
}
func arrayCopy(r &[2]int)int {copy:=*r;copy[0]++;return copy[0]}
func snapshots(r &State)int {
 dst:=make([]int,len(r.Items))
 copy(dst,r.Items)
 dst[0]++
 total:=dst[0]
 for _,n:=range r.Items {n++;total+=n}
 {items:=[]int{10};items[0]++;total+=items[0]}
 return total+len(r.Index)+cap(r.Items)
}
`, `package fixture
import "testing"
func TestValueCopiesAndSharedAliasObservation(t *testing.T){if got:=exercise();got!=([3]int{34,38,28}){t.Fatalf("copy or alias semantics changed: %v",got)}}
func TestArrayCopyDoesNotWriteSource(t *testing.T){value:=[2]int{8,9};if arrayCopy(&value)!=9||value[0]!=8{t.Fatal("array copy mutated read view")}}
func TestReadOperationsAndIndependentStorage(t *testing.T){state:=State{Items:[]int{5},Index:map[string]int{"x":7}};if snapshots(&state)!=25||state.Items[0]!=5{t.Fatal("read copy or local identity changed")}}
`)
}

func TestCompositePermissionConsumerRejections(t *testing.T) {
	const declarations = `package fixture
type Leaf struct { N int }
type State struct { N int; Value Leaf; Values [2]int; Items []int; Index map[string]int; Pointer *int; Links [1]*int; Nodes []Leaf; Pointers []*Leaf; PointerMap map[string]*Leaf; KeyMap map[*Leaf]int; Dynamic any; Channel chan int; Callback func() *Leaf }
`
	cases := map[string]string{
		"map-var-comma-ok":           `func f(r &State){var node,ok=r.PointerMap["x"];if ok{node.N=9}}`,
		"range-selector-destination": `func f(r &State){holder:=struct{P *Leaf}{};for _,holder.P=range r.Pointers{holder.P.N=9}}`,
		"promoted-shared-field":      `type Inner struct{Items []int};type Outer struct{Inner};func f(r &Outer){copy:=*r;copy.Items[0]=9}`,
		"promoted-pointer-field":     `type Inner struct{Pointer *int};type Outer struct{Inner};func f(r &Outer){copy:=*r;*copy.Pointer=9}`,

		"copy-write-ref-upgrade":  `func f(r &State){copy:=*r;w:=mut& copy;w.Items[0]=9}`,
		"map-comma-ok":            `func f(r &State){node,ok:=r.PointerMap["x"];if ok{node.N=9}}`,
		"range-pointer-key":       `func f(r &State){for node:=range r.KeyMap{node.N=9}}`,
		"dynamic-assertion":       `func f(r &State){r.Dynamic.(*Leaf).N=9}`,
		"dynamic-local-assertion": `func f(r &State){value:=r.Dynamic;value.(*Leaf).N=9}`,
		"channel-projection":      `func f(r &State){r.Channel<-9}`,
		"function-projection":     `func f(r &State){node:=r.Callback();node.N=9}`,
		"direct-field":            `func f(r &State){r.N=9}`,
		"nested-field":            `func f(r &State){r.Value.N++}`,
		"array-slot":              `func f(r &State){r.Values[0]++}`,
		"slice-alias":             `func f(r &State){items:=r.Items;items[0]++}`,
		"map-alias":               `func f(r &State){index:=r.Index;index["x"]=9}`,
		"pointer-alias":           `func f(r &State){pointer:=r.Pointer;*pointer=9}`,
		"copy-shared-slice":       `func f(r &State){copy:=*r;copy.Items[0]=9}`,
		"copy-shared-map":         `func f(r &State){copy:=*r;copy.Index["x"]=9}`,
		"copy-shared-pointer":     `func f(r &State){copy:=*r;*copy.Pointer=9}`,
		"array-shared-pointer":    `func f(r &State){copy:=r.Links;*copy[0]=9}`,
		"slice-struct-element":    `func f(r &State){items:=r.Nodes;items[0].N=9}`,
		"range-pointer-element":   `func f(r &State){for _,node:=range r.Pointers{node.N=9}}`,
		"pointer-address":         `func f(r &State){raw:=&r.N;*raw=9}`,
		"write-reborrow-field":    `func f(r &State){w:=mut& r.N;*w=9}`,
		"write-reborrow-slice":    `func f(r &State){w:=mut& r.Items[0];*w=9}`,
		"raw-return":              `func f(r &State)[]int{return r.Items}`,
		"any-box":                 `func f(r &State)any{return r.Items}`,
		"any-box-whole-copy":      `func f(r &State)any{return *r}`,
		"interface-conversion":    `func f(r &State){boxed:=any(r.Items);boxed.([]int)[0]=9}`,
		"generic-identity":        `func id[T any](x T)T{return x};func f(r &State){x:=id(r.Items);x[0]=9}`,
		"direct-native-call":      `func mutate(x []int){x[0]=9};func f(r &State){mutate(r.Items)}`,
		"function-value":          `func mutate(x []int){x[0]=9};func f(r &State){call:=mutate;call(r.Items)}`,
		"closure-return":          `func f(r &State)func()[]int{return func()[]int{return r.Items}}`,
		"closure-write":           `func f(r &State)func(){items:=r.Items;return func(){items[0]=9}}`,
		"append":                  `func f(r &State){x:=append(r.Items,9);_=x}`,
		"copy-destination":        `func f(r &State){copy(r.Items,[]int{9})}`,
		"delete-map":              `func f(r &State){delete(r.Index,"x")}`,
		"clear-map":               `func f(r &State){clear(r.Index)}`,
		"clear-slice":             `func f(r &State){clear(r.Items)}`,
		"tuple-alias":             `func f(r &State){a,b:=r.Items,r.Items;_=a;b[0]=9}`,
		"branch-alias":            `func f(r &State,yes bool){x:=[]int{1};if yes{x=r.Items};x[0]=9}`,
		"nested-literal":          `func f(r &State){x:=[][]int{r.Items};x[0][0]=9}`,
		"channel-send":            `func f(r &State,c chan []int){c<-r.Items}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("composite_consumer.gox", []byte(declarations+body+"\n"))
			if err == nil || !strings.Contains(err.Error(), "composite_consumer.gox") {
				t.Fatalf("expected located permission/boundary rejection, got %v", err)
			}
			t.Log(err)
		})
	}
}

// The enum frontend separately rejects user generic declarations. Check refs
// directly so that rejection cannot mask a lost projection qualification.
func TestCompositePermissionGenericCheckerBoundary(t *testing.T) {
	for name, body := range map[string]string{
		"identity-slice":   `func id[T any](x T)T{return x};func f(r &State){x:=id(r.Items);x[0]=9}`,
		"identity-pointer": `func id[T any](x T)T{return x};func f(r &State){x:=id(r.Pointer);*x=9}`,
		"boxed-value":      `type Box[T any]struct{Value T};func box[T any](x T)Box[T]{return Box[T]{Value:x}};func f(r &State)any{return box(*r)}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := refs.Lower("composite_generic.gox", []byte("package fixture\ntype State struct{Items []int;Pointer *int}\n"+body))
			if err == nil || !strings.Contains(err.Error(), "composite_generic.gox") {
				t.Fatalf("expected checker boundary rejection, got %v", err)
			}
		})
	}
}
